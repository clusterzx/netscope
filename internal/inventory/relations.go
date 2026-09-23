package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// EventDevice implements events.DeviceLookup.
func (s *Store) EventDevice(ctx context.Context, id int64) (name, ip, mac, state string, err error) {
	var display, host string
	err = s.db.R.QueryRowContext(ctx, "SELECT display_name, hostname, primary_ip, primary_mac, state FROM devices WHERE id = ?", id).
		Scan(&display, &host, &ip, &mac, &state)
	if err != nil {
		return "", "", "", "", db.NotFound(err)
	}
	return displayName(display, host, ip, mac, id), ip, mac, state, nil
}

// Relation is an edge between two devices.
type Relation struct {
	ID         int64     `json:"id"`
	ParentID   int64     `json:"parentId"`
	ParentName string    `json:"parentName"`
	ChildID    int64     `json:"childId"`
	ChildName  string    `json:"childName"`
	Kind       string    `json:"kind"`
	Source     string    `json:"source"`
	ParentPort string    `json:"parentPort"`
	ChildPort  string    `json:"childPort"`
	Label      string    `json:"label"`
	Protected  bool      `json:"protected"`
	FirstSeen  time.Time `json:"firstSeen"`
	LastSeen   time.Time `json:"lastSeen"`
}

const relationSelect = `SELECT r.id, r.parent_id, r.child_id, r.kind, r.source, r.parent_port, r.child_port, r.label, r.protected,
	r.first_seen, r.last_seen FROM relations r`

func (s *Store) scanRelations(ctx context.Context, rows *sql.Rows) ([]Relation, error) {
	defer rows.Close()
	out := []Relation{}
	var ids []int64
	for rows.Next() {
		var (
			r           Relation
			first, last int64
		)
		if err := rows.Scan(&r.ID, &r.ParentID, &r.ChildID, &r.Kind, &r.Source, &r.ParentPort, &r.ChildPort, &r.Label, &r.Protected, &first, &last); err != nil {
			return nil, err
		}
		r.FirstSeen, r.LastSeen = db.Time(first), db.Time(last)
		out = append(out, r)
		ids = append(ids, r.ParentID, r.ChildID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	names, err := s.names(ctx, ids)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].ParentName, out[i].ChildName = names[out[i].ParentID], names[out[i].ChildID]
	}
	return out, nil
}

// DeviceRelations returns all edges touching a device.
func (s *Store) DeviceRelations(ctx context.Context, id int64) ([]Relation, error) {
	rows, err := s.db.R.QueryContext(ctx, relationSelect+" WHERE r.parent_id = ?1 OR r.child_id = ?1 ORDER BY r.kind, r.id", id)
	if err != nil {
		return nil, err
	}
	return s.scanRelations(ctx, rows)
}

// ManualRelations returns all manually created edges.
func (s *Store) ManualRelations(ctx context.Context) ([]Relation, error) {
	rows, err := s.db.R.QueryContext(ctx, relationSelect+" WHERE r.source = 'manual' ORDER BY r.id")
	if err != nil {
		return nil, err
	}
	return s.scanRelations(ctx, rows)
}

// AddManualRelation creates a protected manual edge.
func (s *Store) AddManualRelation(ctx context.Context, parent, child int64, kind, parentPort, childPort, label string) (int64, error) {
	if parent == child {
		return 0, errors.New("ein Gerät kann nicht mit sich selbst verbunden werden")
	}
	switch kind {
	case "":
		kind = plugin.RelManual
	case plugin.RelManual, plugin.RelRunsOn, plugin.RelSwitchPort, plugin.RelLLDP, plugin.RelL3, plugin.RelWireless:
	default:
		return 0, fmt.Errorf("unbekannte Art %q", kind)
	}
	var n int
	if err := s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE id IN (?, ?)", parent, child).Scan(&n); err != nil {
		return 0, err
	}
	if n != 2 {
		return 0, errors.New("Gerät existiert nicht")
	}
	now := db.Now()
	res, err := s.db.W.ExecContext(ctx, `INSERT INTO relations(parent_id, child_id, kind, source, parent_port, child_port, label, protected, first_seen, last_seen)
		VALUES (?,?,?,'manual',?,?,?,1,?,?)`, parent, child, kind, parentPort, childPort, label, now, now)
	if err != nil {
		return 0, uniqueErr(err, "", "diese Verbindung existiert bereits")
	}
	s.publishDevice("updated", parent)
	s.publishDevice("updated", child)
	return res.LastInsertId()
}

// DeleteRelation removes an edge. Automatically discovered edges come back on the next
// scan; manual edges are gone for good.
func (s *Store) DeleteRelation(ctx context.Context, id int64) (*Relation, error) {
	rows, err := s.db.R.QueryContext(ctx, relationSelect+" WHERE r.id = ?", id)
	if err != nil {
		return nil, err
	}
	list, err := s.scanRelations(ctx, rows)
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, db.ErrNotFound
	}
	if _, err := s.db.W.ExecContext(ctx, "DELETE FROM relations WHERE id = ?", id); err != nil {
		return nil, err
	}
	return &list[0], nil
}

// ---------------------------------------------------------------- topology graph

// GraphNode is a node of the topology graph.
type GraphNode struct {
	ID       string   `json:"id"` // d<id> for devices, c<id> for containers
	DeviceID int64    `json:"deviceId,omitempty"`
	Kind     string   `json:"kind"` // device | container
	Label    string   `json:"label"`
	IP       string   `json:"ip,omitempty"`
	Type     string   `json:"type,omitempty"`
	Vendor   string   `json:"vendor,omitempty"`
	Online   bool     `json:"online"`
	State    string   `json:"state,omitempty"`
	Subnet   string   `json:"subnet,omitempty"`
	Tags     []string `json:"tags,omitempty"`
	Image    string   `json:"image,omitempty"`
}

// GraphEdge is an edge of the topology graph (source = parent/upstream).
type GraphEdge struct {
	ID         string `json:"id"`
	RelationID int64  `json:"relationId,omitempty"`
	Source     string `json:"source"`
	Target     string `json:"target"`
	Kind       string `json:"kind"`
	Origin     string `json:"origin"`          // plugin or manual
	Label      string `json:"label,omitempty"` // label, or the parent port when there is none
	ParentPort string `json:"parentPort,omitempty"`
	ChildPort  string `json:"childPort,omitempty"`
	Protected  bool   `json:"protected"`
}

// Graph is the topology graph.
type Graph struct {
	Nodes  []GraphNode `json:"nodes"`
	Edges  []GraphEdge `json:"edges"`
	TookMs int64       `json:"tookMs"`
}

// GraphFilter restricts the graph.
type GraphFilter struct {
	Subnet            string // CIDR
	Tag               string
	Query             string
	IncludeContainers bool
	IncludeIgnored    bool
}

// Graph builds the topology graph from devices, relations and containers.
func (s *Store) Graph(ctx context.Context, f GraphFilter) (*Graph, error) {
	start := time.Now()
	var conds []string
	var args []any
	if !f.IncludeIgnored {
		conds = append(conds, "d.state <> 'ignored'")
	}
	if f.Query != "" {
		where, a, err := s.CompileQuery(ctx, f.Query)
		if err != nil {
			return nil, err
		}
		conds = append(conds, "("+where+")")
		args = append(args, a...)
	}
	if f.Tag != "" {
		conds = append(conds, "EXISTS (SELECT 1 FROM device_tags t WHERE t.device_id = d.id AND t.tag = ?)")
		args = append(args, normalizeTag(f.Tag))
	}
	if f.Subnet != "" {
		lo, hi, ok := cidrRange(f.Subnet)
		if !ok {
			return nil, fmt.Errorf("ungültiges Subnetz %q", f.Subnet)
		}
		conds = append(conds, "EXISTS (SELECT 1 FROM device_ips i WHERE i.device_id = d.id AND i.gone_at IS NULL AND i.ip_key BETWEEN ? AND ?)")
		args = append(args, lo, hi)
	}
	where := "1=1"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT d.id, d.display_name, d.hostname, d.primary_ip, d.primary_mac, d.type, d.vendor, d.online, d.state
		FROM devices d WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	g := &Graph{Nodes: []GraphNode{}, Edges: []GraphEdge{}}
	inGraph := map[int64]bool{}
	s.subMu.RLock()
	subnets := append([]subnetEntry(nil), s.subnets...)
	s.subMu.RUnlock()
	for rows.Next() {
		var (
			n                      GraphNode
			display, host, ip, mac string
		)
		if err := rows.Scan(&n.DeviceID, &display, &host, &ip, &mac, &n.Type, &n.Vendor, &n.Online, &n.State); err != nil {
			rows.Close()
			return nil, err
		}
		n.ID, n.Kind, n.IP = fmt.Sprintf("d%d", n.DeviceID), "device", ip
		n.Label = displayName(display, host, ip, mac, n.DeviceID)
		if a, err := netip.ParseAddr(ip); err == nil {
			for _, sn := range subnets {
				if sn.prefix.Contains(a) {
					n.Subnet = sn.prefix.String()
					break
				}
			}
		}
		inGraph[n.DeviceID] = true
		g.Nodes = append(g.Nodes, n)
	}
	rows.Close()
	idx := map[int64]int{}
	for i, n := range g.Nodes {
		idx[n.DeviceID] = i
	}
	trows, err := s.db.R.QueryContext(ctx, "SELECT device_id, tag FROM device_tags ORDER BY tag")
	if err != nil {
		return nil, err
	}
	for trows.Next() {
		var (
			id  int64
			tag string
		)
		if err := trows.Scan(&id, &tag); err != nil {
			trows.Close()
			return nil, err
		}
		if i, ok := idx[id]; ok {
			g.Nodes[i].Tags = append(g.Nodes[i].Tags, tag)
		}
	}
	trows.Close()
	rrows, err := s.db.R.QueryContext(ctx, "SELECT id, parent_id, child_id, kind, source, parent_port, child_port, label, protected FROM relations")
	if err != nil {
		return nil, err
	}
	for rrows.Next() {
		var (
			e             GraphEdge
			parent, child int64
			lbl           string
		)
		if err := rrows.Scan(&e.RelationID, &parent, &child, &e.Kind, &e.Origin, &e.ParentPort, &e.ChildPort, &lbl, &e.Protected); err != nil {
			rrows.Close()
			return nil, err
		}
		if !inGraph[parent] || !inGraph[child] {
			continue
		}
		e.ID = fmt.Sprintf("r%d", e.RelationID)
		e.Source, e.Target = fmt.Sprintf("d%d", parent), fmt.Sprintf("d%d", child)
		e.Label = lbl
		if e.Label == "" && e.ParentPort != "" {
			e.Label = e.ParentPort
		}
		g.Edges = append(g.Edges, e)
	}
	rrows.Close()
	if f.IncludeContainers {
		crows, err := s.db.R.QueryContext(ctx, "SELECT id, device_id, name, image, state FROM containers WHERE gone_at IS NULL")
		if err != nil {
			return nil, err
		}
		for crows.Next() {
			var (
				id, dev            int64
				name, image, state string
			)
			if err := crows.Scan(&id, &dev, &name, &image, &state); err != nil {
				crows.Close()
				return nil, err
			}
			if !inGraph[dev] {
				continue
			}
			nid := fmt.Sprintf("c%d", id)
			g.Nodes = append(g.Nodes, GraphNode{ID: nid, Kind: "container", Label: name, Image: image, Online: state == "running", Type: "container"})
			g.Edges = append(g.Edges, GraphEdge{ID: "e" + nid, Source: fmt.Sprintf("d%d", dev), Target: nid, Kind: "container", Origin: "docker"})
		}
		crows.Close()
	}
	g.TookMs = time.Since(start).Milliseconds()
	return g, nil
}

// ---------------------------------------------------------------- timeline

// TimelineEntry is one entry of a device history.
type TimelineEntry struct {
	TS       time.Time       `json:"ts"`
	Kind     string          `json:"kind"` // event | ip | port | cert | container | package | fact
	Change   string          `json:"change"`
	Text     string          `json:"text"`
	EventID  int64           `json:"eventId,omitempty"`
	Severity plugin.Severity `json:"severity,omitempty"`
	RunID    int64           `json:"runId,omitempty"`
}

// Timeline merges events and the temporal history of a device (newest first).
func (s *Store) Timeline(ctx context.Context, id int64, limit int) ([]TimelineEntry, error) {
	if limit <= 0 || limit > 2000 {
		limit = 300
	}
	var out []TimelineEntry
	add := func(ts int64, kind, change, text string) {
		out = append(out, TimelineEntry{TS: db.Time(ts), Kind: kind, Change: change, Text: text})
	}
	type q struct {
		sql string
		fn  func(r *sql.Rows) error
	}
	queries := []q{
		{"SELECT first_seen, gone_at, ip, mac, source FROM device_ips WHERE device_id = ?", func(r *sql.Rows) error {
			var (
				first       int64
				gone        sql.NullInt64
				ip, mac, sr string
			)
			if err := r.Scan(&first, &gone, &ip, &mac, &sr); err != nil {
				return err
			}
			add(first, "ip", "added", fmt.Sprintf("IP %s zugeordnet (%s)", ip, sr))
			if gone.Valid {
				add(gone.Int64, "ip", "removed", fmt.Sprintf("IP %s nicht mehr zugeordnet", ip))
			}
			return nil
		}},
		{"SELECT first_seen, gone_at, ip, proto, port, service, product, version FROM ports WHERE device_id = ?", func(r *sql.Rows) error {
			var (
				first                      int64
				gone                       sql.NullInt64
				ip, proto, svc, prod, vers string
				port                       int
			)
			if err := r.Scan(&first, &gone, &ip, &proto, &port, &svc, &prod, &vers); err != nil {
				return err
			}
			desc := strings.TrimSpace(fmt.Sprintf("%d/%s %s", port, proto, portValue(svc, prod, vers)))
			add(first, "port", "added", "Port "+desc+" auf "+ip)
			if gone.Valid {
				add(gone.Int64, "port", "removed", fmt.Sprintf("Port %d/%s auf %s nicht mehr in diesem Zustand", port, proto, ip))
			}
			return nil
		}},
		{"SELECT first_seen, gone_at, ip, port, subject_cn, not_after FROM certificates WHERE device_id = ?", func(r *sql.Rows) error {
			var (
				first, na int64
				gone      sql.NullInt64
				ip, cn    string
				port      int
			)
			if err := r.Scan(&first, &gone, &ip, &port, &cn, &na); err != nil {
				return err
			}
			add(first, "cert", "added", fmt.Sprintf("Zertifikat CN=%s auf %s:%d (gültig bis %s)", cn, ip, port, db.Time(na).Format("02.01.2006")))
			if gone.Valid {
				add(gone.Int64, "cert", "removed", fmt.Sprintf("Zertifikat CN=%s auf %s:%d ersetzt/entfernt", cn, ip, port))
			}
			return nil
		}},
		{"SELECT first_seen, gone_at, name, image FROM containers WHERE device_id = ?", func(r *sql.Rows) error {
			var (
				first       int64
				gone        sql.NullInt64
				name, image string
			)
			if err := r.Scan(&first, &gone, &name, &image); err != nil {
				return err
			}
			add(first, "container", "added", fmt.Sprintf("Container %s (%s)", name, image))
			if gone.Valid {
				add(gone.Int64, "container", "removed", fmt.Sprintf("Container %s (%s) entfernt/ersetzt", name, image))
			}
			return nil
		}},
		{"SELECT first_seen, COUNT(*) FROM packages WHERE device_id = ? GROUP BY first_seen", func(r *sql.Rows) error {
			var (
				first int64
				n     int
			)
			if err := r.Scan(&first, &n); err != nil {
				return err
			}
			add(first, "package", "added", fmt.Sprintf("%d Paket(e) installiert oder aktualisiert", n))
			return nil
		}},
		{"SELECT first_seen, kind, source, value FROM device_facts WHERE device_id = ? AND kind IN ('hostname','os','vendor','model','type')", func(r *sql.Rows) error {
			var (
				first             int64
				kind, source, val string
			)
			if err := r.Scan(&first, &kind, &source, &val); err != nil {
				return err
			}
			labels := map[string]string{"hostname": "Hostname", "os": "Betriebssystem", "vendor": "Hersteller", "model": "Modell", "type": "Typ"}
			add(first, "fact", "changed", fmt.Sprintf("%s (%s): %s", labels[kind], source, val))
			return nil
		}},
	}
	for _, qq := range queries {
		rows, err := s.db.R.QueryContext(ctx, qq.sql, id)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			if err := qq.fn(rows); err != nil {
				rows.Close()
				return nil, err
			}
		}
		rows.Close()
	}
	erows, err := s.db.R.QueryContext(ctx, "SELECT id, ts, title, severity, IFNULL(run_id, 0) FROM events WHERE device_id = ? ORDER BY ts DESC LIMIT ?", id, limit)
	if err != nil {
		return nil, err
	}
	for erows.Next() {
		var (
			e   TimelineEntry
			ts  int64
			sev string
		)
		if err := erows.Scan(&e.EventID, &ts, &e.Text, &sev, &e.RunID); err != nil {
			erows.Close()
			return nil, err
		}
		e.TS, e.Kind, e.Change, e.Severity = db.Time(ts), "event", "event", plugin.Severity(sev)
		out = append(out, e)
	}
	erows.Close()
	sort.SliceStable(out, func(i, j int) bool { return out[i].TS.After(out[j].TS) })
	if len(out) > limit {
		out = out[:limit]
	}
	if out == nil {
		out = []TimelineEntry{}
	}
	return out, nil
}

// Inventory returns the latest structured inventories of a device per source.
func (s *Store) Inventory(ctx context.Context, id int64) (map[string]any, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT source, data, collected_at FROM device_inventory WHERE device_id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]any{}
	for rows.Next() {
		var (
			src, data string
			at        int64
		)
		if err := rows.Scan(&src, &data, &at); err != nil {
			return nil, err
		}
		var v any
		if json.Unmarshal([]byte(data), &v) == nil {
			out[src] = map[string]any{"collectedAt": db.Time(at), "data": v}
		}
	}
	return out, rows.Err()
}
