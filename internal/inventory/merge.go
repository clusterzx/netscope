package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/netutil"
)

// temporal tables and the columns that identify an active row per device
var temporalKeys = []struct {
	table string
	key   []string
}{
	{"ports", []string{"ip", "proto", "port"}},
	{"certificates", []string{"ip", "port"}},
	{"http_services", []string{"ip", "port"}},
	{"packages", []string{"manager", "name", "arch"}},
	{"containers", []string{"engine", "name"}},
	{"container_images", []string{"engine", "image_id"}},
	{"device_cves", []string{"cve_id", "cpe"}},
}

// mergeInto moves everything from sources into target and deletes the sources.
// Where both devices have an active row for the same key, the target's row wins.
func (s *Store) mergeInto(ctx context.Context, tx *sql.Tx, target int64, sources []int64, now int64) error {
	exec := func(q string, args ...any) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}
	for _, src := range sources {
		if src == target {
			return errors.New("ein Gerät kann nicht mit sich selbst zusammengeführt werden")
		}
		var n int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE id IN (?, ?)", target, src).Scan(&n); err != nil {
			return err
		}
		if n != 2 {
			return fmt.Errorf("Gerät %d oder %d existiert nicht", target, src)
		}
		steps := []struct {
			q    string
			args []any
		}{
			{"UPDATE device_macs SET device_id = ? WHERE device_id = ?", []any{target, src}},
			{`UPDATE device_ips SET gone_at = ? WHERE device_id = ? AND gone_at IS NULL
				AND ip IN (SELECT ip FROM device_ips WHERE device_id = ? AND gone_at IS NULL)`, []any{now, src, target}},
			{"UPDATE device_ips SET device_id = ? WHERE device_id = ?", []any{target, src}},
			{`UPDATE device_facts SET gone_at = ? WHERE device_id = ? AND gone_at IS NULL AND EXISTS (
				SELECT 1 FROM device_facts t WHERE t.device_id = ? AND t.kind = device_facts.kind
				AND t.source = device_facts.source AND t.gone_at IS NULL)`, []any{now, src, target}},
			{"UPDATE device_facts SET device_id = ? WHERE device_id = ?", []any{target, src}},
			{`INSERT INTO device_presence(device_id, plugin_id, last_seen, last_run_id, missed)
				SELECT ?, plugin_id, last_seen, last_run_id, missed FROM device_presence WHERE device_id = ?
				ON CONFLICT(device_id, plugin_id) DO UPDATE SET last_seen = MAX(last_seen, excluded.last_seen),
				missed = MIN(missed, excluded.missed)`, []any{target, src}},
			{"UPDATE external_refs SET device_id = ? WHERE device_id = ?", []any{target, src}},
			{`INSERT OR IGNORE INTO device_inventory(device_id, source, data, run_id, collected_at)
				SELECT ?, source, data, run_id, collected_at FROM device_inventory WHERE device_id = ?`, []any{target, src}},
			{"INSERT OR IGNORE INTO device_tags(device_id, tag) SELECT ?, tag FROM device_tags WHERE device_id = ?", []any{target, src}},
			{"INSERT OR IGNORE INTO group_members(group_id, device_id) SELECT group_id, ? FROM group_members WHERE device_id = ?", []any{target, src}},
			{"UPDATE OR IGNORE relations SET parent_id = ? WHERE parent_id = ? AND child_id <> ?", []any{target, src, target}},
			{"UPDATE OR IGNORE relations SET child_id = ? WHERE child_id = ? AND parent_id <> ?", []any{target, src, target}},
			{"UPDATE observations SET device_id = ? WHERE device_id = ?", []any{target, src}},
			{"INSERT OR IGNORE INTO cve_ignores(device_id, cve_id, note, created_by, created_at) SELECT ?, cve_id, note, created_by, created_at FROM cve_ignores WHERE device_id = ?", []any{target, src}},
			{"UPDATE events SET device_id = ? WHERE device_id = ?", []any{target, src}},
			{"UPDATE health_checks SET device_id = ? WHERE device_id = ?", []any{target, src}},
		}
		for _, st := range steps {
			if err := exec(st.q, st.args...); err != nil {
				return fmt.Errorf("merge: %w", err)
			}
		}
		for _, tk := range temporalKeys {
			var conds []string
			for _, k := range tk.key {
				conds = append(conds, fmt.Sprintf("t.%s = %s.%s", k, tk.table, k))
			}
			q := fmt.Sprintf(`UPDATE %s SET gone_at = ? WHERE device_id = ? AND gone_at IS NULL AND EXISTS (
				SELECT 1 FROM %s t WHERE t.device_id = ? AND t.gone_at IS NULL AND %s)`, tk.table, tk.table, strings.Join(conds, " AND "))
			if err := exec(q, now, src, target); err != nil {
				return fmt.Errorf("merge %s: %w", tk.table, err)
			}
			if err := exec(fmt.Sprintf("UPDATE %s SET device_id = ? WHERE device_id = ?", tk.table), target, src); err != nil {
				return fmt.Errorf("merge %s: %w", tk.table, err)
			}
		}
		if err := mergeSeries(ctx, tx, target, src); err != nil {
			return err
		}
		if err := mergeDeviceRow(ctx, tx, target, src); err != nil {
			return err
		}
		if err := exec("DELETE FROM devices WHERE id = ?", src); err != nil {
			return err
		}
	}
	if _, _, err := s.applyEffective(ctx, tx, target); err != nil {
		return err
	}
	return updatePrimary(ctx, tx, target)
}

func mergeSeries(ctx context.Context, tx *sql.Tx, target, src int64) error {
	rows, err := tx.QueryContext(ctx, `SELECT s.id, t.id FROM ts_series s LEFT JOIN ts_series t
		ON t.device_id = ? AND t.metric = s.metric AND t.key = s.key WHERE s.device_id = ?`, target, src)
	if err != nil {
		return err
	}
	type pair struct {
		src int64
		dst sql.NullInt64
	}
	var pairs []pair
	for rows.Next() {
		var p pair
		if err := rows.Scan(&p.src, &p.dst); err != nil {
			rows.Close()
			return err
		}
		pairs = append(pairs, p)
	}
	rows.Close()
	for _, p := range pairs {
		if !p.dst.Valid {
			if _, err := tx.ExecContext(ctx, "UPDATE ts_series SET device_id = ? WHERE id = ?", target, p.src); err != nil {
				return err
			}
			continue
		}
		for _, q := range []string{
			"INSERT OR IGNORE INTO ts_raw(series_id, ts, min, avg, max) SELECT ?, ts, min, avg, max FROM ts_raw WHERE series_id = ?",
			"INSERT OR IGNORE INTO ts_5m(series_id, bucket, min, avg, max, count) SELECT ?, bucket, min, avg, max, count FROM ts_5m WHERE series_id = ?",
			"INSERT OR IGNORE INTO ts_1h(series_id, bucket, min, avg, max, count) SELECT ?, bucket, min, avg, max, count FROM ts_1h WHERE series_id = ?",
		} {
			if _, err := tx.ExecContext(ctx, q, p.dst.Int64, p.src); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM ts_series WHERE id = ?", p.src); err != nil {
			return err
		}
	}
	return nil
}

func mergeDeviceRow(ctx context.Context, tx *sql.Tx, target, src int64) error {
	type row struct {
		display, location, owner, notes, crit, state, custom string
		online                                               bool
		first, last                                          sql.NullInt64
	}
	load := func(id int64) (row, error) {
		var r row
		err := tx.QueryRowContext(ctx, `SELECT display_name, location, owner, notes, criticality, state, custom, online, first_seen, last_seen
			FROM devices WHERE id = ?`, id).Scan(&r.display, &r.location, &r.owner, &r.notes, &r.crit, &r.state, &r.custom, &r.online, &r.first, &r.last)
		return r, err
	}
	t, err := load(target)
	if err != nil {
		return err
	}
	s, err := load(src)
	if err != nil {
		return err
	}
	fill := func(a, b string) string {
		if a == "" {
			return b
		}
		return a
	}
	notes := t.notes
	if s.notes != "" && s.notes != t.notes {
		if notes != "" {
			notes += "\n\n"
		}
		notes += s.notes
	}
	critRank := map[string]int{"low": 0, "normal": 1, "high": 2, "critical": 3}
	crit := t.crit
	if critRank[s.crit] > critRank[t.crit] {
		crit = s.crit
	}
	state := t.state
	if state == "unknown" {
		state = s.state
	}
	custom := map[string]any{}
	_ = json.Unmarshal([]byte(s.custom), &custom)
	tc := map[string]any{}
	_ = json.Unmarshal([]byte(t.custom), &tc)
	for k, v := range tc {
		custom[k] = v
	}
	minNull := func(a, b sql.NullInt64) any {
		switch {
		case a.Valid && b.Valid:
			return min(a.Int64, b.Int64)
		case a.Valid:
			return a.Int64
		case b.Valid:
			return b.Int64
		}
		return nil
	}
	maxNull := func(a, b sql.NullInt64) any {
		switch {
		case a.Valid && b.Valid:
			return max(a.Int64, b.Int64)
		case a.Valid:
			return a.Int64
		case b.Valid:
			return b.Int64
		}
		return nil
	}
	_, err = tx.ExecContext(ctx, `UPDATE devices SET display_name = ?, location = ?, owner = ?, notes = ?, criticality = ?, state = ?,
		custom = ?, online = ?, first_seen = ?, last_seen = ?, updated_at = ? WHERE id = ?`,
		fill(t.display, s.display), fill(t.location, s.location), fill(t.owner, s.owner), notes, crit, state, db.JSON(custom),
		db.Bool(t.online || s.online), minNull(t.first, s.first), maxNull(t.last, s.last), db.Now(), target)
	return err
}

// Merge merges the source devices into target (manual action).
func (s *Store) Merge(ctx context.Context, target int64, sources []int64) error {
	if len(sources) == 0 {
		return errors.New("keine Quellgeräte angegeben")
	}
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		return s.mergeInto(ctx, tx, target, sources, time.Now().UnixMilli())
	})
	if err != nil {
		return err
	}
	s.publishDevice("updated", target)
	for _, id := range sources {
		s.publishMerged(id, target)
	}
	return nil
}

// Split moves the given MACs (and the addresses/ports seen with them) of a device to a
// new device and returns its id.
func (s *Store) Split(ctx context.Context, id int64, macs []string) (int64, error) {
	var norm []string
	for _, m := range macs {
		n, ok := netutil.NormalizeMAC(m)
		if !ok {
			return 0, fmt.Errorf("ungültige MAC %q", m)
		}
		norm = append(norm, n)
	}
	if len(norm) == 0 {
		return 0, errors.New("keine MAC-Adressen angegeben")
	}
	var newID int64
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		var total, selected int
		if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_macs WHERE device_id = ?", id).Scan(&total); err != nil {
			return err
		}
		args := append([]any{id}, db.StringArgs(norm)...)
		if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT COUNT(*) FROM device_macs WHERE device_id = ? AND mac IN (%s)", db.Placeholders(len(norm))), args...).Scan(&selected); err != nil {
			return err
		}
		if selected != len(norm) {
			return errors.New("nicht alle MAC-Adressen gehören zu diesem Gerät")
		}
		if selected == total {
			return errors.New("mindestens eine MAC-Adresse muss beim Gerät verbleiben")
		}
		var first, last sql.NullInt64
		if err := tx.QueryRowContext(ctx, fmt.Sprintf("SELECT MIN(first_seen), MAX(last_seen) FROM device_macs WHERE mac IN (%s)", db.Placeholders(len(norm))),
			db.StringArgs(norm)...).Scan(&first, &last); err != nil {
			return err
		}
		now := db.Now()
		res, err := tx.ExecContext(ctx, `INSERT INTO devices(created_source, first_seen, last_seen, online, online_changed_at, created_at, updated_at)
			VALUES ('split', ?, ?, 0, ?, ?, ?)`, first, last, now, now, now)
		if err != nil {
			return err
		}
		newID, _ = res.LastInsertId()
		in := db.Placeholders(len(norm))
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("UPDATE device_macs SET device_id = ? WHERE mac IN (%s)", in), append([]any{newID}, db.StringArgs(norm)...)...); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("UPDATE device_ips SET device_id = ? WHERE device_id = ? AND mac IN (%s)", in),
			append([]any{newID, id}, db.StringArgs(norm)...)...); err != nil {
			return err
		}
		for _, table := range []string{"ports", "certificates", "http_services"} {
			if _, err := tx.ExecContext(ctx, fmt.Sprintf(`UPDATE %s SET device_id = ? WHERE device_id = ? AND ip IN (
				SELECT ip FROM device_ips WHERE device_id = ?)`, table), newID, id, newID); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_presence(device_id, plugin_id, last_seen, last_run_id, missed)
			SELECT ?, plugin_id, last_seen, last_run_id, missed FROM device_presence WHERE device_id = ?`, newID, id); err != nil {
			return err
		}
		for _, dev := range []int64{id, newID} {
			if err := updatePrimary(ctx, tx, dev); err != nil {
				return err
			}
			if _, _, err := s.applyEffective(ctx, tx, dev); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	s.publishDevice("updated", id)
	s.publishDevice("created", newID)
	return newID, nil
}
