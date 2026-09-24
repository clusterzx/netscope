package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"netscope/internal/db"
	"netscope/internal/federation/wire"
	"netscope/internal/plugin"
)

// Forwarder hands the changes of this instance to the delivery to a central instance
// (site role). Enqueue runs inside the transaction that made the change, so an item
// exists exactly when the change was committed.
type Forwarder interface {
	Active() bool
	Enqueue(ctx context.Context, q db.Querier, kind string, at time.Time, data any) error
}

// SetForwarder registers the delivery to a central instance (nil = none).
func (s *Store) SetForwarder(f Forwarder) {
	s.fwdMu.Lock()
	s.fwd = f
	s.fwdMu.Unlock()
}

// forwarder returns the active forwarder or nil.
func (s *Store) forwarder() Forwarder {
	s.fwdMu.RLock()
	f := s.fwd
	s.fwdMu.RUnlock()
	if f == nil || !f.Active() {
		return nil
	}
	return f
}

// forward queues the applied observation (and automatic merges it caused) for the
// central instance.
func (g *ingest) forward(f Forwarder) error {
	if len(g.merged) > 0 {
		if err := f.Enqueue(g.ctx, g.tx, wire.KindDevice, g.now, wire.DeviceOp{Op: wire.OpMerged, Device: g.devID, Sources: g.merged}); err != nil {
			return err
		}
	}
	o := *g.obs
	o.DeviceID = 0 // the resolved device travels as Device
	return f.Enqueue(g.ctx, g.tx, wire.KindObservation, g.now, wire.Observation{Plugin: g.plugin, Device: g.devID, Obs: &o})
}

// forwardOp queues a device operation inside tx (no-op without an active forwarder).
func (s *Store) forwardOp(ctx context.Context, tx *sql.Tx, op wire.DeviceOp) error {
	f := s.forwarder()
	if f == nil {
		return nil
	}
	return f.Enqueue(ctx, tx, wire.KindDevice, time.Now(), op)
}

// siteDevice returns the device a site's device id is mapped to (0 = none).
func siteDevice(ctx context.Context, q db.Querier, site, remote int64) (int64, error) {
	var id int64
	err := q.QueryRowContext(ctx, "SELECT device_id FROM site_devices WHERE site_id = ? AND remote_id = ?", site, remote).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// ---------------------------------------------------------------- central instance

// SiteRef names the site a device belongs to.
type SiteRef struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Slug     string `json:"slug"`
	URL      string `json:"url,omitempty"`      // web UI of the site
	RemoteID int64  `json:"remoteId,omitempty"` // device id at the site
}

// DeviceSite returns the site of a device (nil = this instance).
func (s *Store) DeviceSite(ctx context.Context, id int64) (*SiteRef, error) {
	var (
		ref    SiteRef
		url    string
		status string
		remote sql.NullInt64
	)
	err := s.db.R.QueryRowContext(ctx, `SELECT s.id, s.name, s.slug, s.url, s.status,
		(SELECT MIN(x.remote_id) FROM site_devices x WHERE x.site_id = s.id AND x.device_id = d.id)
		FROM devices d JOIN sites s ON s.id = d.site_id WHERE d.id = ?`, id).Scan(&ref.ID, &ref.Name, &ref.Slug, &url, &status, &remote)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	ref.URL = siteURL(url, status)
	ref.RemoteID = remote.Int64
	return &ref, nil
}

// siteURL is the configured URL of a site or, without one, the URL the site reports.
func siteURL(configured, status string) string {
	if configured != "" {
		return configured
	}
	var st struct {
		Instance struct {
			URL string `json:"url"`
		} `json:"instance"`
	}
	_ = json.Unmarshal([]byte(status), &st)
	return st.Instance.URL
}

// SiteURL is siteURL for the API (configured URL or the one the site reports).
func SiteURL(configured, status string) string { return siteURL(configured, status) }

// RemoteObserve applies an observation delivered by a site: the site's device id is
// mapped to a device here (created if unknown), addresses are matched within the site
// only, and device ids in relations are translated. at is the time of the observation
// at the site.
func (s *Store) RemoteObserve(ctx context.Context, site int64, item wire.Observation, at time.Time) (int64, error) {
	if item.Obs == nil || item.Device <= 0 || item.Plugin == "" {
		return 0, errors.New("unvollständige Beobachtung")
	}
	o := *item.Obs
	o.DeviceID = 0
	o.Create = true // the site has accepted the device
	if len(o.Relations) > 0 {
		rels := make([]plugin.Relation, 0, len(o.Relations))
		for _, r := range o.Relations {
			if r.Other.DeviceID > 0 {
				id, err := siteDevice(ctx, s.db.R, site, r.Other.DeviceID)
				if err != nil {
					return 0, err
				}
				r.Other.DeviceID = id
				if id == 0 && r.Other.MAC == "" && r.Other.IP == "" && r.Other.Ref == nil {
					continue // other side not delivered yet
				}
			}
			rels = append(rels, r)
		}
		o.Relations = rels
	}
	return s.observe(ctx, item.Plugin, 0, &o, observeOpts{site: site, remote: item.Device, at: at})
}

// RemotePresence applies the online state of a site's device.
func (s *Store) RemotePresence(ctx context.Context, site int64, p wire.Presence, at time.Time) error {
	var id int64
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if id, err = siteDevice(ctx, tx, site, p.Device); err != nil || id == 0 {
			return err
		}
		since, exact := at, false
		if p.Since != nil && !p.Since.IsZero() {
			since, exact = *p.Since, true
		}
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET online = ?1,
			online_changed_at = CASE WHEN online <> ?1 OR ?2 = 1 THEN ?3 ELSE online_changed_at END, updated_at = ?4 WHERE id = ?5`,
			db.Bool(p.Online), db.Bool(exact), since.UnixMilli(), time.Now().UnixMilli(), id); err != nil {
			return err
		}
		if p.LastSeen != nil && !p.LastSeen.IsZero() {
			if _, err := tx.ExecContext(ctx, "UPDATE devices SET last_seen = ?1 WHERE id = ?2 AND (last_seen IS NULL OR last_seen < ?1)",
				p.LastSeen.UnixMilli(), id); err != nil {
				return err
			}
		}
		if p.FirstSeen != nil && !p.FirstSeen.IsZero() {
			if _, err := tx.ExecContext(ctx, "UPDATE devices SET first_seen = ? WHERE id = ? AND (first_seen IS NULL OR first_seen > ?)",
				p.FirstSeen.UnixMilli(), id, p.FirstSeen.UnixMilli()); err != nil {
				return err
			}
		}
		for _, sc := range p.Scanners {
			if _, err := tx.ExecContext(ctx, `INSERT INTO device_presence(device_id, plugin_id, last_seen, missed) VALUES (?,?,?,?)
				ON CONFLICT(device_id, plugin_id) DO UPDATE SET last_seen = MAX(last_seen, excluded.last_seen), missed = excluded.missed`,
				id, sc.Plugin, sc.LastSeen.UnixMilli(), sc.Missed); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil && id > 0 {
		s.publishDevice("updated", id)
	}
	return err
}

// RemoteIPGone closes an address a site's device no longer uses.
func (s *Store) RemoteIPGone(ctx context.Context, site int64, g wire.IPGone, at time.Time) error {
	var id int64
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if id, err = siteDevice(ctx, tx, site, g.Device); err != nil || id == 0 {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE device_ips SET gone_at = ? WHERE device_id = ? AND ip = ? AND gone_at IS NULL",
			at.UnixMilli(), id, g.IP); err != nil {
			return err
		}
		return updatePrimary(ctx, tx, id)
	})
	if err == nil && id > 0 {
		s.publishDevice("updated", id)
	}
	return err
}

// RemoteDeviceOp mirrors a deletion, merge or split done at a site.
func (s *Store) RemoteDeviceOp(ctx context.Context, site int64, op wire.DeviceOp) error {
	switch op.Op {
	case wire.OpDeleted:
		id, err := siteDevice(ctx, s.db.R, site, op.Device)
		if err != nil || id == 0 {
			return err
		}
		if _, err := s.db.W.ExecContext(ctx, "DELETE FROM site_devices WHERE site_id = ? AND remote_id = ?", site, op.Device); err != nil {
			return err
		}
		// a device of another site or of this instance only loses the link
		if keep, err := s.keepAfterUnlink(ctx, id, site); err != nil || keep {
			return err
		}
		return s.Delete(ctx, id)
	case wire.OpMerged:
		target, err := siteDevice(ctx, s.db.R, site, op.Device)
		if err != nil || target == 0 {
			return err
		}
		var sources []int64
		for _, src := range op.Sources {
			id, err := siteDevice(ctx, s.db.R, site, src)
			if err != nil {
				return err
			}
			if id > 0 && id != target {
				sources = append(sources, id)
			}
		}
		if _, err := s.db.W.ExecContext(ctx, fmt.Sprintf("DELETE FROM site_devices WHERE site_id = ? AND remote_id IN (%s)", db.Placeholders(len(op.Sources))),
			append([]any{site}, db.Int64Args(op.Sources)...)...); err != nil {
			return err
		}
		if len(sources) == 0 {
			return nil
		}
		return s.Merge(ctx, target, sources)
	case wire.OpSplit:
		id, err := siteDevice(ctx, s.db.R, site, op.Device)
		if err != nil || id == 0 || op.New <= 0 {
			return err
		}
		var macs []string
		for _, m := range op.MACs {
			var n int
			if err := s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_macs WHERE mac = ? AND device_id = ?", m, id).Scan(&n); err != nil {
				return err
			}
			if n > 0 {
				macs = append(macs, m)
			}
		}
		if len(macs) == 0 {
			return nil // the new device is created by its next observation
		}
		newID, err := s.Split(ctx, id, macs)
		if err != nil {
			return err
		}
		return s.db.Tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, "UPDATE devices SET site_id = (SELECT site_id FROM devices WHERE id = ?) WHERE id = ?", id, newID); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, `INSERT INTO site_devices(site_id, remote_id, device_id) VALUES (?,?,?)
				ON CONFLICT(site_id, remote_id) DO UPDATE SET device_id = excluded.device_id`, site, op.New, newID)
			return err
		})
	}
	return fmt.Errorf("unbekannte Geräte-Operation %q", op.Op)
}

// keepAfterUnlink reports whether a device stays after a site dropped it: it belongs to
// another site or to this instance, or another site still delivers it.
func (s *Store) keepAfterUnlink(ctx context.Context, id, site int64) (bool, error) {
	var owned, linked int
	err := s.db.R.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM devices WHERE id = ?1 AND site_id = ?2),
		(SELECT COUNT(*) FROM site_devices WHERE device_id = ?1)`, id, site).Scan(&owned, &linked)
	return owned == 0 || linked > 0, err
}

// ResetSiteMapping forgets which devices a site delivered (its device ids are no longer
// valid, e.g. after the site restored a backup); the next synchronisation re-matches
// them by MAC, reference and address.
func (s *Store) ResetSiteMapping(ctx context.Context, site int64) error {
	_, err := s.db.W.ExecContext(ctx, "DELETE FROM site_devices WHERE site_id = ?", site)
	return err
}

// FinishSiteSync removes the devices of a site the site no longer has: after a full
// synchronisation, devices is the complete list of its device ids. It returns the
// number of removed devices.
func (s *Store) FinishSiteSync(ctx context.Context, site int64, devices []int64) (int, error) {
	keep := make(map[int64]bool, len(devices))
	for _, id := range devices {
		keep[id] = true
	}
	type mapped struct{ remote, dev int64 }
	var stale []mapped
	rows, err := s.db.R.QueryContext(ctx, "SELECT remote_id, device_id FROM site_devices WHERE site_id = ?", site)
	if err != nil {
		return 0, err
	}
	keptDev := map[int64]bool{}
	for rows.Next() {
		var m mapped
		if err := rows.Scan(&m.remote, &m.dev); err != nil {
			rows.Close()
			return 0, err
		}
		if keep[m.remote] {
			keptDev[m.dev] = true
		} else {
			stale = append(stale, m)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for _, m := range stale {
		if _, err := s.db.W.ExecContext(ctx, "DELETE FROM site_devices WHERE site_id = ? AND remote_id = ?", site, m.remote); err != nil {
			return 0, err
		}
	}
	// devices owned by the site that no current site device maps to
	owned, err := queryIDs(ctx, s.db.R, "SELECT id FROM devices WHERE site_id = ?", site)
	if err != nil {
		return 0, err
	}
	removed := 0
	for _, id := range owned {
		if keptDev[id] {
			continue
		}
		var n int
		if err := s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM site_devices WHERE device_id = ?", id).Scan(&n); err != nil {
			return removed, err
		}
		if n > 0 {
			continue // still delivered (by another site)
		}
		if err := s.Delete(ctx, id); err != nil && !errors.Is(err, db.ErrNotFound) {
			return removed, err
		}
		removed++
	}
	return removed, nil
}

// SiteDeviceIDs maps device ids here to the ids at their site (for deep links).
func (s *Store) SiteDeviceIDs(ctx context.Context, site int64, remote []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	for start := 0; start < len(remote); start += 500 {
		chunk := remote[start:min(start+500, len(remote))]
		rows, err := s.db.R.QueryContext(ctx, fmt.Sprintf("SELECT remote_id, device_id FROM site_devices WHERE site_id = ? AND remote_id IN (%s)",
			db.Placeholders(len(chunk))), append([]any{site}, db.Int64Args(chunk)...)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var r, d int64
			if err := rows.Scan(&r, &d); err != nil {
				rows.Close()
				return nil, err
			}
			out[r] = d
		}
		rows.Close()
	}
	return out, nil
}
