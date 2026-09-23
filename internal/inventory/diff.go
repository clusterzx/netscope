package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// DiffSide describes one side of a comparison.
type DiffSide struct {
	Kind       string     `json:"kind"` // run | time
	RunID      int64      `json:"runId,omitempty"`
	PluginID   string     `json:"pluginId,omitempty"`
	PluginName string     `json:"pluginName,omitempty"`
	Partial    bool       `json:"partial,omitempty"` // the run covered selected devices only
	Time       time.Time  `json:"time"`
	Finished   *time.Time `json:"finished,omitempty"`
}

// DiffItem is one difference.
type DiffItem struct {
	DeviceID   int64  `json:"deviceId"`
	DeviceName string `json:"deviceName"`
	Kind       string `json:"kind"` // device | ip | mac | port | cert | http | package | container | hostname | os | vendor | type
	Key        string `json:"key"`
	Change     string `json:"change"` // added | removed | changed
	Old        string `json:"old,omitempty"`
	New        string `json:"new,omitempty"`
}

// DiffResult is the outcome of a comparison.
type DiffResult struct {
	A       DiffSide       `json:"a"`
	B       DiffSide       `json:"b"`
	Items   []DiffItem     `json:"items"`
	Summary map[string]int `json:"summary"`
}

// facts: device id -> "kind\x00key" -> value
type factSet map[int64]map[string]string

func (f factSet) put(dev int64, kind, key, value string) {
	m := f[dev]
	if m == nil {
		m = map[string]string{}
		f[dev] = m
	}
	m[kind+"\x00"+key] = value
}

func portValue(service, product, version string) string {
	v := strings.TrimSpace(strings.Join([]string{service, product, version}, " "))
	if v == "" {
		return "offen"
	}
	return v
}

func certValue(fp, cn string, notAfter time.Time) string {
	short := fp
	if len(short) > 16 {
		short = short[:16]
	}
	return fmt.Sprintf("%s (CN=%s, gültig bis %s)", short, cn, notAfter.Format("2006-01-02"))
}

// snapshotAt reconstructs the state of all (or one) devices at time t from the temporal tables.
func (s *Store) snapshotAt(ctx context.Context, t time.Time, deviceID int64) (factSet, error) {
	ms := t.UnixMilli()
	fs := factSet{}
	devFilter := ""
	var devArgs []any
	if deviceID > 0 {
		devFilter = " AND device_id = ?"
		devArgs = []any{deviceID}
	}
	active := "first_seen <= ? AND (gone_at IS NULL OR gone_at > ?)"
	query := func(q string, fn func(rows *sql.Rows) error) error {
		rows, err := s.db.R.QueryContext(ctx, q, append([]any{ms, ms}, devArgs...)...)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			if err := fn(rows); err != nil {
				return err
			}
		}
		return rows.Err()
	}
	// devices that existed
	devQ := "SELECT id FROM devices WHERE created_at <= ?"
	args := []any{ms}
	if deviceID > 0 {
		devQ += " AND id = ?"
		args = append(args, deviceID)
	}
	rows, err := s.db.R.QueryContext(ctx, devQ, args...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		fs.put(id, "device", "", "vorhanden")
	}
	rows.Close()
	steps := []struct {
		q  string
		fn func(rows *sql.Rows) error
	}{
		{"SELECT device_id, ip, mac FROM device_ips WHERE " + active + devFilter, func(r *sql.Rows) error {
			var (
				id      int64
				ip, mac string
			)
			if err := r.Scan(&id, &ip, &mac); err != nil {
				return err
			}
			fs.put(id, "ip", ip, mac)
			return nil
		}},
		{"SELECT device_id, ip, proto, port, service, product, version FROM ports WHERE " + active + devFilter, func(r *sql.Rows) error {
			var (
				id                            int64
				ip, proto, svc, product, vers string
				port                          int
			)
			if err := r.Scan(&id, &ip, &proto, &port, &svc, &product, &vers); err != nil {
				return err
			}
			fs.put(id, "port", fmt.Sprintf("%s %d/%s", ip, port, proto), portValue(svc, product, vers))
			return nil
		}},
		{"SELECT device_id, ip, port, fingerprint, subject_cn, not_after FROM certificates WHERE " + active + devFilter, func(r *sql.Rows) error {
			var (
				id         int64
				ip, fp, cn string
				port       int
				notAfter   int64
			)
			if err := r.Scan(&id, &ip, &port, &fp, &cn, &notAfter); err != nil {
				return err
			}
			fs.put(id, "cert", fmt.Sprintf("%s:%d", ip, port), certValue(fp, cn, db.Time(notAfter)))
			return nil
		}},
		{"SELECT device_id, manager, name, arch, version FROM packages WHERE " + active + devFilter, func(r *sql.Rows) error {
			var (
				id                       int64
				mgr, name, arch, version string
			)
			if err := r.Scan(&id, &mgr, &name, &arch, &version); err != nil {
				return err
			}
			key := name
			if arch != "" {
				key += ":" + arch
			}
			fs.put(id, "package", key, version)
			return nil
		}},
		{"SELECT device_id, name, image FROM containers WHERE " + active + devFilter, func(r *sql.Rows) error {
			var (
				id          int64
				name, image string
			)
			if err := r.Scan(&id, &name, &image); err != nil {
				return err
			}
			fs.put(id, "container", name, image)
			return nil
		}},
	}
	for _, st := range steps {
		if err := query(st.q, st.fn); err != nil {
			return nil, err
		}
	}
	// effective scalar facts at time t
	byDev := map[int64]map[string][]factRow{}
	err = query(`SELECT device_id, kind, source, value, extra, last_seen FROM device_facts WHERE `+active+devFilter+`
		AND kind IN ('hostname','vendor','type','os')`, func(r *sql.Rows) error {
		var (
			id int64
			f  factRow
		)
		if err := r.Scan(&id, &f.kind, &f.source, &f.value, &f.extra, &f.lastSeen); err != nil {
			return err
		}
		if byDev[id] == nil {
			byDev[id] = map[string][]factRow{}
		}
		byDev[id][f.kind] = append(byDev[id][f.kind], f)
		return nil
	})
	if err != nil {
		return nil, err
	}
	hostPrio := append([]string{SourceManual}, s.settings.System().HostnamePriority...)
	prios := map[string][]string{FactHostname: hostPrio, FactVendor: vendorPriority, FactType: typePriority, FactOS: osPriority}
	for id, kinds := range byDev {
		for kind, list := range kinds {
			if f, ok := pickFact(list, prios[kind]); ok {
				fs.put(id, kind, "", f.value)
			}
		}
	}
	return fs, nil
}

// snapshotRun builds the fact set of one run from its stored observations.
func (s *Store) snapshotRun(ctx context.Context, runID, deviceID int64) (factSet, error) {
	q := "SELECT device_id, data FROM observations WHERE run_id = ? AND device_id IS NOT NULL"
	args := []any{runID}
	if deviceID > 0 {
		q += " AND device_id = ?"
		args = append(args, deviceID)
	}
	q += " ORDER BY id"
	rows, err := s.db.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	fs := factSet{}
	for rows.Next() {
		var (
			id   int64
			data string
			o    plugin.Observation
		)
		if err := rows.Scan(&id, &data); err != nil {
			return nil, err
		}
		if err := json.Unmarshal([]byte(data), &o); err != nil {
			continue
		}
		fs.put(id, "device", "", "gesehen")
		if o.IP != "" {
			mac := ""
			if len(o.MACs) > 0 {
				mac = o.MACs[0]
			}
			fs.put(id, "ip", o.IP, mac)
		}
		for _, m := range o.MACs {
			fs.put(id, "mac", m, "")
		}
		if o.Hostname != "" {
			fs.put(id, "hostname", "", o.Hostname)
		}
		if o.Vendor != "" {
			fs.put(id, "vendor", "", o.Vendor)
		}
		if o.OS != nil && o.OS.Name != "" {
			fs.put(id, "os", "", o.OS.Name)
		}
		if o.Ports != nil {
			for _, p := range o.Ports.Ports {
				proto := p.Proto
				if proto == "" {
					proto = o.Ports.Protocol
				}
				fs.put(id, "port", fmt.Sprintf("%s %d/%s", o.IP, p.Port, proto), portValue(p.Service, p.Product, p.Version))
			}
		}
		if o.TLS != nil {
			for _, c := range o.TLS.Certs {
				fs.put(id, "cert", fmt.Sprintf("%s:%d", o.IP, c.Port), certValue(c.Fingerprint, c.SubjectCN, c.NotAfter))
			}
		}
		if o.HTTP != nil {
			for _, h := range o.HTTP.Services {
				var apps []string
				for _, a := range h.Apps {
					apps = append(apps, a.Name)
				}
				fs.put(id, "http", fmt.Sprintf("%s:%d", o.IP, h.Port), strings.TrimSpace(fmt.Sprintf("%d %s %s", h.StatusCode, h.Title, strings.Join(apps, ","))))
			}
		}
		if o.Packages != nil {
			for _, p := range o.Packages.Packages {
				key := p.Name
				if p.Arch != "" {
					key += ":" + p.Arch
				}
				fs.put(id, "package", key, p.Version)
			}
		}
		if o.Containers != nil {
			for _, c := range o.Containers.Containers {
				fs.put(id, "container", strings.TrimPrefix(c.Name, "/"), c.Image)
			}
		}
	}
	return fs, rows.Err()
}

func (s *Store) compare(ctx context.Context, a, b factSet) ([]DiffItem, error) {
	devs := map[int64]bool{}
	for id := range a {
		devs[id] = true
	}
	for id := range b {
		devs[id] = true
	}
	ids := make([]int64, 0, len(devs))
	for id := range devs {
		ids = append(ids, id)
	}
	names, err := s.names(ctx, ids)
	if err != nil {
		return nil, err
	}
	var items []DiffItem
	for _, id := range ids {
		am, bm := a[id], b[id]
		keys := map[string]bool{}
		for k := range am {
			keys[k] = true
		}
		for k := range bm {
			keys[k] = true
		}
		for k := range keys {
			kind, key, _ := strings.Cut(k, "\x00")
			av, inA := am[k]
			bv, inB := bm[k]
			it := DiffItem{DeviceID: id, DeviceName: names[id], Kind: kind, Key: key}
			if it.DeviceName == "" {
				it.DeviceName = fmt.Sprintf("Gerät %d (gelöscht)", id)
			}
			switch {
			case inA && !inB:
				it.Change, it.Old = "removed", av
			case !inA && inB:
				it.Change, it.New = "added", bv
			case av != bv:
				it.Change, it.Old, it.New = "changed", av, bv
			default:
				continue
			}
			items = append(items, it)
		}
	}
	order := map[string]int{"device": 0, "ip": 1, "mac": 2, "hostname": 3, "os": 4, "vendor": 5, "type": 6, "port": 7,
		"cert": 8, "http": 9, "container": 10, "package": 11}
	sort.Slice(items, func(i, j int) bool {
		if items[i].DeviceName != items[j].DeviceName {
			return items[i].DeviceName < items[j].DeviceName
		}
		if order[items[i].Kind] != order[items[j].Kind] {
			return order[items[i].Kind] < order[items[j].Kind]
		}
		return items[i].Key < items[j].Key
	})
	if items == nil {
		items = []DiffItem{}
	}
	return items, nil
}

func summarize(items []DiffItem) map[string]int {
	out := map[string]int{}
	for _, it := range items {
		out[it.Kind+"."+it.Change]++
	}
	return out
}

func (s *Store) runSide(ctx context.Context, runID int64) (DiffSide, error) {
	var (
		side         DiffSide
		created      int64
		started, fin sql.NullInt64
		scopeRaw     string
	)
	err := s.db.R.QueryRowContext(ctx, "SELECT plugin_id, created_at, started_at, finished_at, scope FROM runs WHERE id = ?", runID).
		Scan(&side.PluginID, &created, &started, &fin, &scopeRaw)
	if err != nil {
		return side, fmt.Errorf("Lauf %d: %w", runID, db.NotFound(err))
	}
	side.Kind, side.RunID = "run", runID
	if p, ok := plugin.Get(side.PluginID); ok {
		side.PluginName = p.Info().Name
	}
	var sc plugin.Scope
	if json.Unmarshal([]byte(scopeRaw), &sc) == nil {
		side.Partial = sc.DeviceRestricted()
	}
	side.Time = db.Time(created)
	if started.Valid {
		side.Time = db.Time(started.Int64)
	}
	side.Finished = db.NullTime(fin)
	return side, nil
}

// DiffRuns compares the observations of two runs (optionally for one device).
func (s *Store) DiffRuns(ctx context.Context, runA, runB, deviceID int64) (*DiffResult, error) {
	sa, err := s.runSide(ctx, runA)
	if err != nil {
		return nil, err
	}
	sb, err := s.runSide(ctx, runB)
	if err != nil {
		return nil, err
	}
	fa, err := s.snapshotRun(ctx, runA, deviceID)
	if err != nil {
		return nil, err
	}
	fb, err := s.snapshotRun(ctx, runB, deviceID)
	if err != nil {
		return nil, err
	}
	items, err := s.compare(ctx, fa, fb)
	if err != nil {
		return nil, err
	}
	return &DiffResult{A: sa, B: sb, Items: items, Summary: summarize(items)}, nil
}

// DiffTimes compares the inventory state at two points in time.
func (s *Store) DiffTimes(ctx context.Context, a, b time.Time, deviceID int64) (*DiffResult, error) {
	fa, err := s.snapshotAt(ctx, a, deviceID)
	if err != nil {
		return nil, err
	}
	fb, err := s.snapshotAt(ctx, b, deviceID)
	if err != nil {
		return nil, err
	}
	items, err := s.compare(ctx, fa, fb)
	if err != nil {
		return nil, err
	}
	return &DiffResult{A: DiffSide{Kind: "time", Time: a}, B: DiffSide{Kind: "time", Time: b}, Items: items, Summary: summarize(items)}, nil
}

// PreviousRun returns the run of the same plugin before runID (0 if none).
func (s *Store) PreviousRun(ctx context.Context, runID int64) (int64, error) {
	var prev sql.NullInt64
	err := s.db.R.QueryRowContext(ctx, `SELECT MAX(r2.id) FROM runs r1 JOIN runs r2 ON r2.plugin_id = r1.plugin_id AND r2.id < r1.id
		AND r2.status = 'success' WHERE r1.id = ?`, runID).Scan(&prev)
	return prev.Int64, err
}
