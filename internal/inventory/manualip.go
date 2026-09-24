package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"netscope/internal/db"
	"netscope/internal/federation/wire"
	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

// AddManualIP assigns an address to a device by hand, e.g. a VM whose hypervisor does
// not report its addresses. The address is scanned like any other from then on. A device
// known only by this address (no MAC, no manual data) is merged into the device; an
// address of another device is refused. Addresses of site devices are kept at the site.
func (s *Store) AddManualIP(ctx context.Context, id int64, ip string) error {
	addr, ok := normalizeHostIP(ip)
	if !ok {
		return plugin.FieldErr("ip", fmt.Sprintf("ungültige IP-Adresse %q", ip))
	}
	var merged []int64
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var site sql.NullInt64
		if err := tx.QueryRowContext(ctx, "SELECT site_id FROM devices WHERE id = ?", id).Scan(&site); err != nil {
			return db.NotFound(err)
		}
		if site.Valid {
			return plugin.FieldErr("ip", "Das Gerät gehört zu einem NetScope-Standort – seine Adressen werden dort gepflegt")
		}
		has := func() (bool, error) {
			var n int
			err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_ips WHERE device_id = ? AND ip = ? AND gone_at IS NULL", id, addr).Scan(&n)
			return n > 0, err
		}
		if ok, err := has(); err != nil || ok {
			return err // already assigned
		}
		others, err := queryIDs(ctx, tx, `SELECT DISTINCT i.device_id FROM device_ips i JOIN devices d ON d.id = i.device_id
			WHERE i.ip = ? AND i.gone_at IS NULL AND i.device_id <> ? AND d.site_id IS NULL`, addr, id)
		if err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		for _, other := range others {
			var macs int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_macs WHERE device_id = ?", other).Scan(&macs); err != nil {
				return err
			}
			shadow, err := isMergeableShadow(ctx, tx, other)
			if err != nil {
				return err
			}
			if macs > 0 || !shadow {
				var name string
				_ = tx.QueryRowContext(ctx, "SELECT COALESCE(NULLIF(display_name, ''), NULLIF(hostname, ''), primary_ip) FROM devices WHERE id = ?", other).Scan(&name)
				return plugin.FieldErr("ip", fmt.Sprintf("%s gehört zu Gerät „%s“ (#%d) – die Geräte zusammenführen oder die Adresse dort entfernen", addr, name, other))
			}
			// the same host, found by a scan through its address only
			if err := s.mergeInto(ctx, tx, id, []int64{other}, now); err != nil {
				return err
			}
			if err := s.forwardOp(ctx, tx, wire.DeviceOp{Op: wire.OpMerged, Device: id, Sources: []int64{other}}); err != nil {
				return err
			}
			merged = append(merged, other)
		}
		if ok, err := has(); err != nil || ok {
			return err // taken over with the merged device
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_ips(device_id, ip, ip_key, subnet_id, source, first_seen, last_seen) VALUES (?,?,?,?,?,?,?)`,
			id, addr, netutil.IPKey(addr), s.subnetFor(addr), SourceManual, now, now); err != nil {
			return err
		}
		if err := updatePrimary(ctx, tx, id); err != nil {
			return err
		}
		if f := s.forwarder(); f != nil {
			return f.Enqueue(ctx, tx, wire.KindObservation, time.UnixMilli(now),
				wire.Observation{Plugin: SourceManual, Device: id, Obs: &plugin.Observation{IP: addr, Create: true}})
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publishDevice("updated", id)
	for _, m := range merged {
		s.publishMerged(m, id)
	}
	return nil
}

// RemoveManualIP removes an address assigned by hand. Addresses reported by scanners
// cannot be removed (the next scan would bring them back).
func (s *Store) RemoveManualIP(ctx context.Context, id int64, ip string) error {
	addr, ok := normalizeHostIP(ip)
	if !ok {
		return plugin.FieldErr("ip", fmt.Sprintf("ungültige IP-Adresse %q", ip))
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		now := time.Now()
		res, err := tx.ExecContext(ctx, "UPDATE device_ips SET gone_at = ? WHERE device_id = ? AND ip = ? AND gone_at IS NULL AND source = ?",
			now.UnixMilli(), id, addr, SourceManual)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			var source string
			err := tx.QueryRowContext(ctx, "SELECT source FROM device_ips WHERE device_id = ? AND ip = ? AND gone_at IS NULL", id, addr).Scan(&source)
			if errors.Is(err, sql.ErrNoRows) {
				return db.ErrNotFound
			}
			if err != nil {
				return err
			}
			return plugin.FieldErr("ip", fmt.Sprintf("%s wurde nicht von Hand vergeben, sondern von %s gemeldet – die Adresse verschwindet, wenn das Gerät sie nicht mehr nutzt", addr, source))
		}
		if err := updatePrimary(ctx, tx, id); err != nil {
			return err
		}
		if f := s.forwarder(); f != nil {
			return f.Enqueue(ctx, tx, wire.KindIPGone, now, wire.IPGone{Device: id, IP: addr})
		}
		return nil
	})
	if err == nil {
		s.publishDevice("updated", id)
	}
	return err
}
