package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"netscope/internal/db"
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
	threshold := s.settings.System().OfflineAfterMissed
	now := time.Now()
	var changes []plugin.Change
	var touched []int64
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		q := fmt.Sprintf(`UPDATE device_presence SET missed = missed + 1 WHERE plugin_id = ?
			AND (last_run_id IS NULL OR last_run_id <> ?) AND device_id IN (%s)`, sub)
		if _, err := tx.ExecContext(ctx, q, append([]any{run.PluginID, run.RunID}, args...)...); err != nil {
			return err
		}
		q = fmt.Sprintf(`SELECT d.id, d.last_seen FROM devices d JOIN device_presence p ON p.device_id = d.id AND p.plugin_id = ?
			WHERE d.online = 1 AND p.missed >= ? AND IFNULL(d.last_seen, 0) <= p.last_seen AND d.id IN (%s)`, sub)
		rows, err := tx.QueryContext(ctx, q, append([]any{run.PluginID, threshold}, args...)...)
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
			changes = append(changes, plugin.Change{Type: plugin.ChangeDeviceOffline, DeviceID: o.id, PluginID: run.PluginID,
				RunID: run.RunID, At: now, Old: last})
			touched = append(touched, o.id)
		}
		if !subnetMode {
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
			changes = append(changes, plugin.Change{Type: plugin.ChangeIPChanged, DeviceID: c.dev, PluginID: run.PluginID,
				RunID: run.RunID, At: now, Key: c.new, Old: c.oldIP, New: c.new})
			if err := updatePrimary(ctx, tx, c.dev); err != nil {
				return err
			}
			touched = append(touched, c.dev)
		}
		return nil
	})
	if err != nil {
		return err
	}
	for _, id := range touched {
		s.publishDevice("updated", id)
	}
	s.emit(changes)
	return nil
}
