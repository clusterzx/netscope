package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"netscope/internal/db"
	"netscope/internal/federation/wire"
	"netscope/internal/plugin"
)

// scopeFilter returns a subquery selecting the device ids covered by a finished run and
// whether the run covered whole subnets (needed for IP change detection).
func scopeFilter(t plugin.Targets) (string, []any, bool) {
	if len(t.Subnets) > 0 && !t.DeviceMode {
		ids := make([]int64, 0, len(t.Subnets))
		for _, s := range t.Subnets {
			ids = append(ids, s.ID)
		}
		return fmt.Sprintf("SELECT device_id FROM device_ips WHERE gone_at IS NULL AND subnet_id IN (%s)", db.Placeholders(len(ids))),
			db.Int64Args(ids), true
	}
	if len(t.Devices) == 0 {
		return "", nil, false
	}
	ids := make([]int64, 0, len(t.Devices))
	for _, d := range t.Devices {
		ids = append(ids, d.ID)
	}
	return fmt.Sprintf("SELECT id FROM devices WHERE id IN (%s)", db.Placeholders(len(ids))), db.Int64Args(ids), false
}

// RunFinished evaluates presence after a successful run of a presence plugin: devices the
// plugin knows but did not see count as missed; after OfflineAfterMissed misses (and if no
// other plugin saw them more recently) they go offline. For subnet scans, addresses that
// were replaced by another address of the same device in the same subnet are closed
// (IP change).
func (s *Store) RunFinished(ctx context.Context, run plugin.RunSummary) error {
	if !run.Presence || run.Status != "success" || run.RunID == 0 {
		return nil
	}
	sub, args, subnetMode := scopeFilter(run.Targets)
	if sub == "" {
		return nil
	}
	p := presenceRun{run: run, sub: sub, args: args, subnetMode: subnetMode,
		threshold: s.settings.System().OfflineAfterMissed, now: time.Now()}
	fwd := s.forwarder()
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		if err := p.evaluate(ctx, tx); err != nil {
			return err
		}
		if fwd == nil {
			return nil
		}
		// the transitions go to the central instance in the same transaction
		for _, c := range p.changes {
			if err := forwardPresence(ctx, tx, fwd, c); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, id := range p.touched {
		s.publishDevice("updated", id)
	}
	s.emit(p.changes)
	return nil
}

// forwardPresence hands an offline transition or a closed address to the central instance.
func forwardPresence(ctx context.Context, tx *sql.Tx, f Forwarder, c plugin.Change) error {
	switch c.Type {
	case plugin.ChangeDeviceOffline:
		at := c.At
		p := wire.Presence{Device: c.DeviceID, Online: false, Since: &at}
		if t, ok := c.Old.(time.Time); ok {
			p.LastSeen = &t
		}
		return f.Enqueue(ctx, tx, wire.KindPresence, c.At, p)
	case plugin.ChangeIPChanged:
		if old, ok := c.Old.(string); ok {
			return f.Enqueue(ctx, tx, wire.KindIPGone, c.At, wire.IPGone{Device: c.DeviceID, IP: old})
		}
	}
	return nil
}

// presenceRun is the presence evaluation of one finished run.
type presenceRun struct {
	run        plugin.RunSummary
	sub        string
	args       []any
	subnetMode bool
	threshold  int
	now        time.Time
	changes    []plugin.Change
	touched    []int64
}

func (p *presenceRun) evaluate(ctx context.Context, tx *sql.Tx) error {
	run, now := p.run, p.now
	q := fmt.Sprintf(`UPDATE device_presence SET missed = missed + 1 WHERE plugin_id = ?
		AND (last_run_id IS NULL OR last_run_id <> ?) AND device_id IN (%s)`, p.sub)
	if _, err := tx.ExecContext(ctx, q, append([]any{run.PluginID, run.RunID}, p.args...)...); err != nil {
		return err
	}
	q = fmt.Sprintf(`SELECT d.id, d.last_seen FROM devices d JOIN device_presence p ON p.device_id = d.id AND p.plugin_id = ?
		WHERE d.online = 1 AND p.missed >= ? AND IFNULL(d.last_seen, 0) <= p.last_seen AND d.id IN (%s)`, p.sub)
	rows, err := tx.QueryContext(ctx, q, append([]any{run.PluginID, p.threshold}, p.args...)...)
	if err != nil {
		return err
	}
	type off struct {
		id       int64
		lastSeen sql.NullInt64
	}
	var offline []off
	for rows.Next() {
		var o off
		if err := rows.Scan(&o.id, &o.lastSeen); err != nil {
			rows.Close()
			return err
		}
		offline = append(offline, o)
	}
	rows.Close()
	for _, o := range offline {
		if _, err := tx.ExecContext(ctx, "UPDATE devices SET online = 0, online_changed_at = ?, updated_at = ? WHERE id = ?",
			now.UnixMilli(), now.UnixMilli(), o.id); err != nil {
			return err
		}
		var last any
		if t := db.NullTime(o.lastSeen); t != nil {
			last = *t
		}
		p.changes = append(p.changes, plugin.Change{Type: plugin.ChangeDeviceOffline, DeviceID: o.id, PluginID: run.PluginID,
			RunID: run.RunID, At: now, Old: last})
		p.touched = append(p.touched, o.id)
	}
	if !p.subnetMode {
		return nil
	}
	ids := make([]int64, 0, len(run.Targets.Subnets))
	for _, sn := range run.Targets.Subnets {
		ids = append(ids, sn.ID)
	}
	q = fmt.Sprintf(`SELECT o.id, o.device_id, o.ip, n.ip FROM device_ips o
		JOIN device_ips n ON n.device_id = o.device_id AND n.gone_at IS NULL AND n.last_run_id = ? AND n.id <> o.id
			AND n.subnet_id = o.subnet_id
		WHERE o.gone_at IS NULL AND o.subnet_id IN (%s) AND (o.last_run_id IS NULL OR o.last_run_id <> ?)`, db.Placeholders(len(ids)))
	qargs := append([]any{run.RunID}, db.Int64Args(ids)...)
	qargs = append(qargs, run.RunID)
	rows, err = tx.QueryContext(ctx, q, qargs...)
	if err != nil {
		return err
	}
	type ipc struct {
		rowID, dev int64
		oldIP, new string
	}
	var list []ipc
	seen := map[int64]bool{}
	for rows.Next() {
		var c ipc
		if err := rows.Scan(&c.rowID, &c.dev, &c.oldIP, &c.new); err != nil {
			rows.Close()
			return err
		}
		if seen[c.rowID] {
			continue
		}
		seen[c.rowID] = true
		list = append(list, c)
	}
	rows.Close()
	for _, c := range list {
		if _, err := tx.ExecContext(ctx, "UPDATE device_ips SET gone_at = ? WHERE id = ?", now.UnixMilli(), c.rowID); err != nil {
			return err
		}
		p.changes = append(p.changes, plugin.Change{Type: plugin.ChangeIPChanged, DeviceID: c.dev, PluginID: run.PluginID,
			RunID: run.RunID, At: now, Key: c.new, Old: c.oldIP, New: c.new})
		if err := updatePrimary(ctx, tx, c.dev); err != nil {
			return err
		}
		p.touched = append(p.touched, c.dev)
	}
	return nil
}
