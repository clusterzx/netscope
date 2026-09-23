package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

// GroupRef is a group membership.
type GroupRef struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color,omitempty"`
}

// DeviceRow is one row of the device list.
type DeviceRow struct {
	ID              int64          `json:"id"`
	Name            string         `json:"name"`
	DisplayName     string         `json:"displayName"`
	Hostname        string         `json:"hostname"`
	HostnameSource  string         `json:"hostnameSource"`
	IP              string         `json:"ip"`
	IPs             []string       `json:"ips"`
	MAC             string         `json:"mac"`
	MACs            []string       `json:"macs"`
	Vendor          string         `json:"vendor"`
	Model           string         `json:"model"`
	Type            string         `json:"type"`
	OS              string         `json:"os"`
	OSSource        string         `json:"osSource"`
	Location        string         `json:"location"`
	Owner           string         `json:"owner"`
	State           string         `json:"state"`
	Criticality     string         `json:"criticality"`
	Online          bool           `json:"online"`
	OnlineChangedAt *time.Time     `json:"onlineChangedAt,omitempty"`
	FirstSeen       *time.Time     `json:"firstSeen,omitempty"`
	LastSeen        *time.Time     `json:"lastSeen,omitempty"`
	Tags            []string       `json:"tags"`
	Groups          []GroupRef     `json:"groups"`
	PortCount       int            `json:"portCount"`
	Ports           []string       `json:"ports,omitempty"`
	MaxCVSS         *float64       `json:"maxCvss,omitempty"`
	CVECount        int            `json:"cveCount"`
	CertExpiry      *time.Time     `json:"certExpiry,omitempty"`
	HealthState     string         `json:"healthState,omitempty"`
	ParentID        int64          `json:"parentId,omitempty"`
	ParentName      string         `json:"parentName,omitempty"`
	Custom          map[string]any `json:"custom"`
	HasNotes        bool           `json:"hasNotes"`
	CreatedSource   string         `json:"createdSource"`
}

// ListOptions controls List.
type ListOptions struct {
	Query     string
	Sort      string // field, "-" prefix for descending
	Limit     int
	Offset    int
	WithPorts bool
}

// ListResult is a page of devices.
type ListResult struct {
	Total int         `json:"total"`
	Items []DeviceRow `json:"items"`
}

var sortColumns = map[string]string{
	"name":        "LOWER(COALESCE(NULLIF(d.display_name, ''), NULLIF(d.hostname, ''), d.primary_ip))",
	"ip":          "d.ip_key",
	"mac":         "d.primary_mac",
	"vendor":      "LOWER(d.vendor)",
	"model":       "LOWER(d.model)",
	"type":        "d.type",
	"os":          "LOWER(d.os)",
	"location":    "LOWER(d.location)",
	"owner":       "LOWER(d.owner)",
	"state":       "d.state",
	"criticality": critRankSQL,
	"online":      "d.online",
	"lastSeen":    "d.last_seen",
	"firstSeen":   "d.first_seen",
	"ports":       "port_count",
	"cve":         "max_cvss",
	"certExpiry":  "cert_expiry",
	"id":          "d.id",
}

// SortFields returns the accepted sort keys.
func SortFields() []string {
	out := make([]string, 0, len(sortColumns))
	for k := range sortColumns {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (s *Store) groupResolver(ctx context.Context) GroupResolver {
	return func(name string) (int64, string, error) {
		var (
			id         int64
			kind, qstr string
		)
		err := s.db.R.QueryRowContext(ctx, "SELECT id, kind, query FROM groups WHERE name = ? COLLATE NOCASE", name).Scan(&id, &kind, &qstr)
		if errors.Is(err, sql.ErrNoRows) {
			return 0, "", fmt.Errorf("Gruppe %q existiert nicht", name)
		}
		if err != nil {
			return 0, "", err
		}
		if kind == "manual" {
			return id, "", nil
		}
		return 0, qstr, nil
	}
}

// CompileQuery parses and compiles a filter query.
func (s *Store) CompileQuery(ctx context.Context, query string) (string, []any, error) {
	q, err := ParseQuery(query)
	if err != nil {
		return "", nil, err
	}
	return q.Compile(time.Now(), s.groupResolver(ctx))
}

// MatchingIDs returns the ids of all devices matching a query.
func (s *Store) MatchingIDs(ctx context.Context, query string) ([]int64, error) {
	where, args, err := s.CompileQuery(ctx, query)
	if err != nil {
		return nil, err
	}
	return queryIDs(ctx, s.db.R, "SELECT d.id FROM devices d WHERE "+where, args...)
}

func queryIDs(ctx context.Context, q db.Querier, query string, args ...any) ([]int64, error) {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

const deviceSelect = `SELECT d.id, d.display_name, d.hostname, d.hostname_source, d.primary_ip, d.primary_mac, d.vendor, d.model, d.type,
	d.os, d.os_source, d.location, d.owner, d.state, d.criticality, d.online, d.online_changed_at, d.first_seen, d.last_seen,
	d.custom, d.notes <> '', d.created_source,
	(SELECT COUNT(*) FROM ports p WHERE p.device_id = d.id AND p.gone_at IS NULL) AS port_count,
	(` + activeCVE + `) AS max_cvss,
	(SELECT COUNT(*) FROM device_cves c WHERE c.device_id = d.id AND c.gone_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM cve_ignores i WHERE i.device_id = c.device_id AND i.cve_id = c.cve_id)) AS cve_count,
	(SELECT MIN(x.not_after) FROM certificates x WHERE x.device_id = d.id AND x.gone_at IS NULL) AS cert_expiry,
	(SELECT CASE MAX(CASE h.state WHEN 'down' THEN 3 WHEN 'degraded' THEN 2 WHEN 'up' THEN 1 ELSE 0 END)
		WHEN 3 THEN 'down' WHEN 2 THEN 'degraded' WHEN 1 THEN 'up' WHEN 0 THEN 'unknown' END
		FROM health_checks h WHERE h.device_id = d.id AND h.enabled = 1) AS health_state,
	(SELECT r.parent_id FROM relations r WHERE r.child_id = d.id ORDER BY (r.kind = 'runs_on') DESC, r.last_seen DESC LIMIT 1) AS parent_id
	FROM devices d`

// portCountSQL is the port count column of deviceSelect.
const portCountSQL = "(SELECT COUNT(*) FROM ports p WHERE p.device_id = d.id AND p.gone_at IS NULL) AS port_count"

// deviceSelectNoPortCount skips the per-device port count; List fills it from the
// loaded port lists instead (cheaper when all ports are loaded anyway).
var deviceSelectNoPortCount = strings.Replace(deviceSelect, portCountSQL, "0 AS port_count", 1)

func scanDeviceRow(rows *sql.Rows) (DeviceRow, error) {
	var (
		r                          DeviceRow
		onlineChanged, first, last sql.NullInt64
		custom                     string
		maxCVSS                    sql.NullFloat64
		certExp                    sql.NullInt64
		health                     sql.NullString
		parent                     sql.NullInt64
	)
	err := rows.Scan(&r.ID, &r.DisplayName, &r.Hostname, &r.HostnameSource, &r.IP, &r.MAC, &r.Vendor, &r.Model, &r.Type,
		&r.OS, &r.OSSource, &r.Location, &r.Owner, &r.State, &r.Criticality, &r.Online, &onlineChanged, &first, &last,
		&custom, &r.HasNotes, &r.CreatedSource, &r.PortCount, &maxCVSS, &r.CVECount, &certExp, &health, &parent)
	if err != nil {
		return r, err
	}
	r.OnlineChangedAt, r.FirstSeen, r.LastSeen = db.NullTime(onlineChanged), db.NullTime(first), db.NullTime(last)
	r.Custom = map[string]any{}
	_ = json.Unmarshal([]byte(custom), &r.Custom)
	if maxCVSS.Valid {
		v := maxCVSS.Float64
		r.MaxCVSS = &v
	}
	r.CertExpiry = db.NullTime(certExp)
	r.HealthState = health.String
	r.ParentID = parent.Int64
	r.Name = displayName(r.DisplayName, r.Hostname, r.IP, r.MAC, r.ID)
	r.Tags, r.IPs, r.MACs, r.Groups = []string{}, []string{}, []string{}, []GroupRef{}
	return r, nil
}

func displayName(display, hostname, ip, mac string, id int64) string {
	switch {
	case display != "":
		return display
	case hostname != "":
		return hostname
	case ip != "":
		return ip
	case mac != "":
		return mac
	}
	return fmt.Sprintf("Gerät %d", id)
}

// List returns a filtered, sorted page of devices.
func (s *Store) List(ctx context.Context, opts ListOptions) (*ListResult, error) {
	where, args, err := s.CompileQuery(ctx, opts.Query)
	if err != nil {
		return nil, err
	}
	var total int
	if err := s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices d WHERE "+where, args...).Scan(&total); err != nil {
		return nil, err
	}
	sortKey, desc := strings.TrimPrefix(opts.Sort, "-"), strings.HasPrefix(opts.Sort, "-")
	if sortKey == "" {
		sortKey = "ip"
	}
	col, ok := sortColumns[sortKey]
	if !ok {
		return nil, fmt.Errorf("unbekanntes Sortierfeld %q", sortKey)
	}
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	limit := opts.Limit
	if limit <= 0 || limit > 5000 {
		limit = 5000
	}
	sel := deviceSelect
	countFromList := opts.WithPorts && sortKey != "ports"
	if countFromList {
		sel = deviceSelectNoPortCount
	}
	q := fmt.Sprintf("%s WHERE %s ORDER BY %s %s NULLS LAST, d.id LIMIT ? OFFSET ?", sel, where, col, dir)
	rows, err := s.db.R.QueryContext(ctx, q, append(args, limit, opts.Offset)...)
	if err != nil {
		return nil, err
	}
	items := []DeviceRow{}
	for rows.Next() {
		r, err := scanDeviceRow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if err := s.enrich(ctx, items, opts.WithPorts); err != nil {
		return nil, err
	}
	if countFromList {
		for i := range items {
			items[i].PortCount = len(items[i].Ports)
		}
	}
	return &ListResult{Total: total, Items: items}, nil
}

// sortPortList orders "port/proto" entries by protocol, then numerically by port.
func sortPortList(list []string) []string {
	type entry struct {
		proto string
		port  int
		s     string
	}
	es := make([]entry, len(list))
	for i, s := range list {
		p, proto, _ := strings.Cut(s, "/")
		n, _ := strconv.Atoi(p)
		es[i] = entry{proto, n, s}
	}
	sort.Slice(es, func(i, j int) bool {
		if es[i].proto != es[j].proto {
			return es[i].proto < es[j].proto
		}
		return es[i].port < es[j].port
	})
	for i := range es {
		list[i] = es[i].s
	}
	return list
}

// enrich batch-loads IPs, MACs, tags, groups, parent names and (optionally) ports.
func (s *Store) enrich(ctx context.Context, items []DeviceRow, withPorts bool) error {
	if len(items) == 0 {
		return nil
	}
	idx := map[int64]*DeviceRow{}
	ids := make([]int64, 0, len(items))
	var parentIDs []int64
	for i := range items {
		idx[items[i].ID] = &items[i]
		ids = append(ids, items[i].ID)
		if items[i].ParentID > 0 {
			parentIDs = append(parentIDs, items[i].ParentID)
		}
	}
	// Chunk to stay below SQLite's parameter limit.
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		in := db.Placeholders(len(chunk))
		args := db.Int64Args(chunk)
		load := func(q string, fn func(*DeviceRow, string)) error {
			rows, err := s.db.R.QueryContext(ctx, q, args...)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var (
					id int64
					v  string
				)
				if err := rows.Scan(&id, &v); err != nil {
					return err
				}
				if r := idx[id]; r != nil {
					fn(r, v)
				}
			}
			return rows.Err()
		}
		if err := load("SELECT device_id, ip FROM device_ips WHERE gone_at IS NULL AND device_id IN ("+in+") ORDER BY ip_key",
			func(r *DeviceRow, v string) { r.IPs = append(r.IPs, v) }); err != nil {
			return err
		}
		if err := load("SELECT device_id, mac FROM device_macs WHERE device_id IN ("+in+") ORDER BY last_seen DESC",
			func(r *DeviceRow, v string) { r.MACs = append(r.MACs, v) }); err != nil {
			return err
		}
		if err := load("SELECT device_id, tag FROM device_tags WHERE device_id IN ("+in+") ORDER BY tag",
			func(r *DeviceRow, v string) { r.Tags = append(r.Tags, v) }); err != nil {
			return err
		}
		if withPorts {
			// one row per device (group_concat) is several times faster than one row per
			// port; the per-device lists are sorted in Go
			if err := load("SELECT device_id, group_concat(port || '/' || proto, ',') FROM ports WHERE gone_at IS NULL AND device_id IN ("+in+") GROUP BY device_id",
				func(r *DeviceRow, v string) { r.Ports = sortPortList(strings.Split(v, ",")) }); err != nil {
				return err
			}
		}
		rows, err := s.db.R.QueryContext(ctx, `SELECT m.device_id, g.id, g.name, g.color FROM group_members m JOIN groups g ON g.id = m.group_id
			WHERE m.device_id IN (`+in+`) ORDER BY g.name`, args...)
		if err != nil {
			return err
		}
		for rows.Next() {
			var (
				id int64
				g  GroupRef
			)
			if err := rows.Scan(&id, &g.ID, &g.Name, &g.Color); err != nil {
				rows.Close()
				return err
			}
			if r := idx[id]; r != nil {
				r.Groups = append(r.Groups, g)
			}
		}
		rows.Close()
	}
	// query groups
	qgroups, err := s.queryGroups(ctx)
	if err != nil {
		return err
	}
	for _, g := range qgroups {
		members, err := s.MatchingIDs(ctx, g.Query)
		if err != nil {
			continue // an invalid group query must not break the device list
		}
		for _, id := range members {
			if r := idx[id]; r != nil {
				r.Groups = append(r.Groups, GroupRef{ID: g.ID, Name: g.Name, Color: g.Color})
			}
		}
	}
	if len(parentIDs) > 0 {
		names, err := s.names(ctx, parentIDs)
		if err != nil {
			return err
		}
		for i := range items {
			if items[i].ParentID > 0 {
				items[i].ParentName = names[items[i].ParentID]
			}
		}
	}
	return nil
}

// names returns display names for device ids.
func (s *Store) names(ctx context.Context, ids []int64) (map[int64]string, error) {
	out := map[int64]string{}
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		rows, err := s.db.R.QueryContext(ctx, "SELECT id, display_name, hostname, primary_ip, primary_mac FROM devices WHERE id IN ("+db.Placeholders(len(chunk))+")",
			db.Int64Args(chunk)...)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var (
				id                     int64
				display, host, ip, mac string
			)
			if err := rows.Scan(&id, &display, &host, &ip, &mac); err != nil {
				rows.Close()
				return nil, err
			}
			out[id] = displayName(display, host, ip, mac, id)
		}
		rows.Close()
	}
	return out, nil
}

// Name returns the display name of one device.
func (s *Store) Name(ctx context.Context, id int64) string {
	m, err := s.names(ctx, []int64{id})
	if err != nil {
		return fmt.Sprintf("Gerät %d", id)
	}
	if n, ok := m[id]; ok {
		return n
	}
	return fmt.Sprintf("Gerät %d", id)
}

// Row returns the list row of one device.
func (s *Store) Row(ctx context.Context, id int64) (*DeviceRow, error) {
	rows, err := s.db.R.QueryContext(ctx, deviceSelect+" WHERE d.id = ?", id)
	if err != nil {
		return nil, err
	}
	var items []DeviceRow
	for rows.Next() {
		r, err := scanDeviceRow(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, r)
	}
	rows.Close()
	if len(items) == 0 {
		return nil, db.ErrNotFound
	}
	if err := s.enrich(ctx, items, true); err != nil {
		return nil, err
	}
	return &items[0], nil
}

// ---------------------------------------------------------------- detail

// FactView is a scalar fact of one source.
type FactView struct {
	Kind      string    `json:"kind"`
	Source    string    `json:"source"`
	Value     string    `json:"value"`
	Extra     any       `json:"extra,omitempty"`
	FirstSeen time.Time `json:"firstSeen"`
	LastSeen  time.Time `json:"lastSeen"`
}

// IPView is an address (current or historic).
type IPView struct {
	IP        string     `json:"ip"`
	MAC       string     `json:"mac"`
	SubnetID  int64      `json:"subnetId,omitempty"`
	Source    string     `json:"source"`
	FirstSeen time.Time  `json:"firstSeen"`
	LastSeen  time.Time  `json:"lastSeen"`
	GoneAt    *time.Time `json:"goneAt,omitempty"`
}

// MACView is a MAC address of a device.
type MACView struct {
	MAC        string    `json:"mac"`
	Vendor     string    `json:"vendor"`
	Randomized bool      `json:"randomized"`
	Source     string    `json:"source"`
	FirstSeen  time.Time `json:"firstSeen"`
	LastSeen   time.Time `json:"lastSeen"`
}

// PresenceView is the presence state per plugin.
type PresenceView struct {
	Plugin   string    `json:"plugin"`
	LastSeen time.Time `json:"lastSeen"`
	Missed   int       `json:"missed"`
}

// RefView is an external reference.
type RefView struct {
	Source   string    `json:"source"`
	Ref      string    `json:"ref"`
	Data     any       `json:"data,omitempty"`
	LastSeen time.Time `json:"lastSeen"`
}

// DeviceDetail is the full device view.
type DeviceDetail struct {
	DeviceRow
	Notes     string            `json:"notes"`
	Manual    map[string]string `json:"manual"` // manual overrides of hostname/vendor/model/type/os
	Facts     []FactView        `json:"facts"`
	IPHistory []IPView          `json:"ipHistory"`
	MACList   []MACView         `json:"macList"`
	Presence  []PresenceView    `json:"presence"`
	Refs      []RefView         `json:"refs"`
	Counts    map[string]int    `json:"counts"`
	// EffectiveSources names the source whose fact won per kind (hostname, vendor,
	// model, type, os), following the configured priorities.
	EffectiveSources map[string]string `json:"effectiveSources"`
}

// Get returns the full detail of a device.
func (s *Store) Get(ctx context.Context, id int64) (*DeviceDetail, error) {
	row, err := s.Row(ctx, id)
	if err != nil {
		return nil, err
	}
	d := &DeviceDetail{DeviceRow: *row, Manual: map[string]string{}, Facts: []FactView{}, IPHistory: []IPView{},
		MACList: []MACView{}, Presence: []PresenceView{}, Refs: []RefView{}, Counts: map[string]int{}}
	if err := s.db.R.QueryRowContext(ctx, "SELECT notes FROM devices WHERE id = ?", id).Scan(&d.Notes); err != nil {
		return nil, err
	}
	eff, err := s.effectiveValues(ctx, s.db.R, id)
	if err != nil {
		return nil, err
	}
	d.EffectiveSources = make(map[string]string, len(eff))
	for kind, f := range eff {
		d.EffectiveSources[kind] = f.source
	}
	rows, err := s.db.R.QueryContext(ctx, `SELECT kind, source, value, extra, first_seen, last_seen FROM device_facts
		WHERE device_id = ? AND gone_at IS NULL ORDER BY kind, source`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			f           FactView
			extra       string
			first, last int64
		)
		if err := rows.Scan(&f.Kind, &f.Source, &f.Value, &extra, &first, &last); err != nil {
			rows.Close()
			return nil, err
		}
		f.FirstSeen, f.LastSeen = db.Time(first), db.Time(last)
		if extra != "" && extra != "{}" {
			var x any
			if json.Unmarshal([]byte(extra), &x) == nil {
				f.Extra = x
			}
		}
		if f.Source == SourceManual {
			d.Manual[f.Kind] = f.Value
		}
		d.Facts = append(d.Facts, f)
	}
	rows.Close()
	rows, err = s.db.R.QueryContext(ctx, `SELECT ip, mac, IFNULL(subnet_id, 0), source, first_seen, last_seen, gone_at FROM device_ips
		WHERE device_id = ? ORDER BY gone_at IS NOT NULL, last_seen DESC LIMIT 200`, id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			v           IPView
			first, last int64
			gone        sql.NullInt64
		)
		if err := rows.Scan(&v.IP, &v.MAC, &v.SubnetID, &v.Source, &first, &last, &gone); err != nil {
			rows.Close()
			return nil, err
		}
		v.FirstSeen, v.LastSeen, v.GoneAt = db.Time(first), db.Time(last), db.NullTime(gone)
		d.IPHistory = append(d.IPHistory, v)
	}
	rows.Close()
	rows, err = s.db.R.QueryContext(ctx, "SELECT mac, vendor, randomized, source, first_seen, last_seen FROM device_macs WHERE device_id = ? ORDER BY last_seen DESC", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			m           MACView
			first, last int64
		)
		if err := rows.Scan(&m.MAC, &m.Vendor, &m.Randomized, &m.Source, &first, &last); err != nil {
			rows.Close()
			return nil, err
		}
		m.FirstSeen, m.LastSeen = db.Time(first), db.Time(last)
		d.MACList = append(d.MACList, m)
	}
	rows.Close()
	rows, err = s.db.R.QueryContext(ctx, "SELECT plugin_id, last_seen, missed FROM device_presence WHERE device_id = ? ORDER BY last_seen DESC", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			p    PresenceView
			last int64
		)
		if err := rows.Scan(&p.Plugin, &last, &p.Missed); err != nil {
			rows.Close()
			return nil, err
		}
		p.LastSeen = db.Time(last)
		d.Presence = append(d.Presence, p)
	}
	rows.Close()
	rows, err = s.db.R.QueryContext(ctx, "SELECT source, ref, data, last_seen FROM external_refs WHERE device_id = ? ORDER BY source, ref", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			r    RefView
			data string
			last int64
		)
		if err := rows.Scan(&r.Source, &r.Ref, &data, &last); err != nil {
			rows.Close()
			return nil, err
		}
		r.LastSeen = db.Time(last)
		var x any
		if json.Unmarshal([]byte(data), &x) == nil {
			r.Data = x
		}
		d.Refs = append(d.Refs, r)
	}
	rows.Close()
	counts := map[string]string{
		"ports":        "SELECT COUNT(*) FROM ports WHERE device_id = ? AND gone_at IS NULL",
		"http":         "SELECT COUNT(*) FROM http_services WHERE device_id = ? AND gone_at IS NULL",
		"certificates": "SELECT COUNT(*) FROM certificates WHERE device_id = ? AND gone_at IS NULL",
		"packages":     "SELECT COUNT(*) FROM packages WHERE device_id = ? AND gone_at IS NULL",
		"containers":   "SELECT COUNT(*) FROM containers WHERE device_id = ? AND gone_at IS NULL",
		"cves":         "SELECT COUNT(*) FROM device_cves c WHERE c.device_id = ? AND c.gone_at IS NULL",
		"health":       "SELECT COUNT(*) FROM health_checks WHERE device_id = ?",
		"events":       "SELECT COUNT(*) FROM events WHERE device_id = ?",
		"relations":    "SELECT COUNT(*) FROM relations WHERE parent_id = ?1 OR child_id = ?1",
		"inventory":    "SELECT COUNT(*) FROM device_inventory WHERE device_id = ?",
		"observations": "SELECT COUNT(*) FROM observations WHERE device_id = ?",
	}
	for k, q := range counts {
		var n int
		if err := s.db.R.QueryRowContext(ctx, q, id).Scan(&n); err != nil {
			return nil, err
		}
		d.Counts[k] = n
	}
	return d, nil
}

// ---------------------------------------------------------------- manual updates

// DeviceUpdate holds manual changes; nil fields stay unchanged.
type DeviceUpdate struct {
	DisplayName *string        `json:"displayName,omitempty"`
	Hostname    *string        `json:"hostname,omitempty"` // manual override, "" removes it
	Vendor      *string        `json:"vendor,omitempty"`
	Model       *string        `json:"model,omitempty"`
	Type        *string        `json:"type,omitempty"`
	OS          *string        `json:"os,omitempty"`
	Location    *string        `json:"location,omitempty"`
	Owner       *string        `json:"owner,omitempty"`
	Notes       *string        `json:"notes,omitempty"`
	Criticality *string        `json:"criticality,omitempty"`
	State       *string        `json:"state,omitempty"`
	Tags        *[]string      `json:"tags,omitempty"`
	Custom      map[string]any `json:"custom,omitempty"` // merged; null deletes a key
}

// AuditSnapshot is the manual state of a device, used for audit before/after.
type AuditSnapshot struct {
	DisplayName string            `json:"displayName"`
	Location    string            `json:"location"`
	Owner       string            `json:"owner"`
	Notes       string            `json:"notes"`
	Criticality string            `json:"criticality"`
	State       string            `json:"state"`
	Tags        []string          `json:"tags"`
	Custom      map[string]any    `json:"custom"`
	Manual      map[string]string `json:"manual"`
}

// ManualSnapshot returns the manual state of a device.
func (s *Store) ManualSnapshot(ctx context.Context, q db.Querier, id int64) (*AuditSnapshot, error) {
	a := &AuditSnapshot{Tags: []string{}, Custom: map[string]any{}, Manual: map[string]string{}}
	var custom string
	if err := q.QueryRowContext(ctx, "SELECT display_name, location, owner, notes, criticality, state, custom FROM devices WHERE id = ?", id).
		Scan(&a.DisplayName, &a.Location, &a.Owner, &a.Notes, &a.Criticality, &a.State, &custom); err != nil {
		return nil, db.NotFound(err)
	}
	_ = json.Unmarshal([]byte(custom), &a.Custom)
	rows, err := q.QueryContext(ctx, "SELECT tag FROM device_tags WHERE device_id = ? ORDER BY tag", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			rows.Close()
			return nil, err
		}
		a.Tags = append(a.Tags, t)
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, "SELECT kind, value FROM device_facts WHERE device_id = ? AND source = 'manual' AND gone_at IS NULL", id)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			rows.Close()
			return nil, err
		}
		a.Manual[k] = v
	}
	rows.Close()
	return a, nil
}

// ValidateCustom checks custom field values against their definitions.
func (s *Store) ValidateCustom(ctx context.Context, values map[string]any) error {
	defs, err := s.CustomFields(ctx)
	if err != nil {
		return err
	}
	byKey := map[string]CustomField{}
	for _, d := range defs {
		byKey[d.Key] = d
	}
	for k, v := range values {
		def, ok := byKey[k]
		if !ok {
			return fmt.Errorf("unbekanntes Custom Field %q", k)
		}
		if v == nil {
			continue
		}
		if err := def.check(v); err != nil {
			return fmt.Errorf("%s: %w", def.Label, err)
		}
	}
	return nil
}

// Update applies manual changes and returns the snapshots before and after.
func (s *Store) Update(ctx context.Context, id int64, u DeviceUpdate) (before, after *AuditSnapshot, err error) {
	if u.Criticality != nil {
		if _, ok := critRanks[*u.Criticality]; !ok {
			return nil, nil, fmt.Errorf("Kritikalität: low, normal, high oder critical")
		}
	}
	if u.State != nil {
		switch *u.State {
		case "known", "unknown", "ignored":
		default:
			return nil, nil, fmt.Errorf("Zustand: known, unknown oder ignored")
		}
	}
	if u.Custom != nil {
		if err := s.ValidateCustom(ctx, u.Custom); err != nil {
			return nil, nil, err
		}
	}
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		var err error
		if before, err = s.ManualSnapshot(ctx, tx, id); err != nil {
			return err
		}
		set := []string{}
		args := []any{}
		str := func(col string, v *string) {
			if v != nil {
				set = append(set, col+" = ?")
				args = append(args, strings.TrimSpace(*v))
			}
		}
		str("display_name", u.DisplayName)
		str("location", u.Location)
		str("owner", u.Owner)
		if u.Notes != nil {
			set = append(set, "notes = ?")
			args = append(args, *u.Notes)
		}
		str("criticality", u.Criticality)
		str("state", u.State)
		if u.Custom != nil {
			custom := before.Custom
			for k, v := range u.Custom {
				if v == nil {
					delete(custom, k)
				} else {
					custom[k] = v
				}
			}
			set = append(set, "custom = ?")
			args = append(args, db.JSON(custom))
		}
		set = append(set, "updated_at = ?")
		args = append(args, db.Now(), id)
		if _, err := tx.ExecContext(ctx, "UPDATE devices SET "+strings.Join(set, ", ")+" WHERE id = ?", args...); err != nil {
			return err
		}
		for kind, v := range map[string]*string{FactHostname: u.Hostname, FactVendor: u.Vendor, FactModel: u.Model, FactType: u.Type, FactOS: u.OS} {
			if v != nil {
				if err := s.setManualFact(ctx, tx, id, kind, *v); err != nil {
					return err
				}
			}
		}
		if u.Tags != nil {
			if _, err := tx.ExecContext(ctx, "DELETE FROM device_tags WHERE device_id = ?", id); err != nil {
				return err
			}
			for _, t := range *u.Tags {
				if t = normalizeTag(t); t != "" {
					if _, err := tx.ExecContext(ctx, "INSERT OR IGNORE INTO device_tags(device_id, tag) VALUES (?,?)", id, t); err != nil {
						return err
					}
				}
			}
		}
		after, err = s.ManualSnapshot(ctx, tx, id)
		return err
	})
	if err == nil {
		s.publishDevice("updated", id)
	}
	return before, after, err
}

// Delete removes a device and all its data (events are kept).
func (s *Store) Delete(ctx context.Context, id int64) error {
	name := s.Name(ctx, id)
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		// keep the device name in its events before the FK is nulled
		if _, err := tx.ExecContext(ctx, `UPDATE events SET payload = json_set(payload, '$.device_name', ?) WHERE device_id = ?
			AND json_extract(payload, '$.device_name') IS NULL`, name, id); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "DELETE FROM devices WHERE id = ?", id)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return db.ErrNotFound
		}
		return nil
	})
	if err == nil {
		s.publishDevice("deleted", id)
	}
	return err
}

// CreateManual creates a device by hand (e.g. for devices that are not scannable).
func (s *Store) CreateManual(ctx context.Context, name, ip, mac string) (int64, error) {
	var ipN, macN string
	if ip != "" {
		n, ok := normalizeHostIP(ip)
		if !ok {
			return 0, fmt.Errorf("ungültige IP %q", ip)
		}
		ipN = n
	}
	if mac != "" {
		n, ok := netutil.NormalizeMAC(mac)
		if !ok {
			return 0, fmt.Errorf("ungültige MAC %q", mac)
		}
		macN = n
	}
	if strings.TrimSpace(name) == "" && ipN == "" && macN == "" {
		return 0, errors.New("Name, IP oder MAC erforderlich")
	}
	var id int64
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		now := db.Now()
		if macN != "" {
			var n int
			_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_macs WHERE mac = ?", macN).Scan(&n)
			if n > 0 {
				return fmt.Errorf("MAC %s gehört bereits zu einem Gerät", macN)
			}
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO devices(display_name, created_source, first_seen, state, online_changed_at, created_at, updated_at)
			VALUES (?, 'manual', ?, 'known', ?, ?, ?)`, strings.TrimSpace(name), now, now, now, now)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		if macN != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO device_macs(mac, device_id, randomized, source, first_seen, last_seen)
				VALUES (?,?,?,?,?,?)`, macN, id, db.Bool(netutil.IsRandomizedMAC(macN)), SourceManual, now, now); err != nil {
				return err
			}
		}
		if ipN != "" {
			if _, err := tx.ExecContext(ctx, `INSERT INTO device_ips(device_id, ip, ip_key, mac, subnet_id, source, first_seen, last_seen)
				VALUES (?,?,?,?,?,?,?,?)`, id, ipN, netutil.IPKey(ipN), macN, s.subnetFor(ipN), SourceManual, now, now); err != nil {
				return err
			}
		}
		return updatePrimary(ctx, tx, id)
	})
	if err == nil {
		s.publishDevice("created", id)
	}
	return id, err
}

// BulkAction is a mass operation on devices.
type BulkAction struct {
	Action string   `json:"action"` // add_tags | remove_tags | set_state | set_criticality | add_group | remove_group | delete
	IDs    []int64  `json:"ids"`
	Tags   []string `json:"tags,omitempty"`
	Value  string   `json:"value,omitempty"`
	Group  int64    `json:"group,omitempty"`
}

// Bulk applies a mass operation and returns the number of affected devices.
func (s *Store) Bulk(ctx context.Context, b BulkAction) (int, error) {
	if len(b.IDs) == 0 {
		return 0, errors.New("keine Geräte ausgewählt")
	}
	if len(b.IDs) > 5000 {
		return 0, errors.New("zu viele Geräte (max. 5000)")
	}
	switch b.Action {
	case "set_state":
		if b.Value != "known" && b.Value != "unknown" && b.Value != "ignored" {
			return 0, errors.New("Zustand: known, unknown oder ignored")
		}
	case "set_criticality":
		if _, ok := critRanks[b.Value]; !ok {
			return 0, errors.New("Kritikalität: low, normal, high oder critical")
		}
	case "add_group", "remove_group":
		var kind string
		if err := s.db.R.QueryRowContext(ctx, "SELECT kind FROM groups WHERE id = ?", b.Group).Scan(&kind); err != nil {
			return 0, fmt.Errorf("Gruppe %d: %w", b.Group, db.NotFound(err))
		}
		if kind != "manual" {
			return 0, errors.New("Mitglieder können nur manuellen Gruppen zugeordnet werden")
		}
	case "add_tags", "remove_tags":
		if len(b.Tags) == 0 {
			return 0, errors.New("keine Tags angegeben")
		}
	case "delete":
	default:
		return 0, fmt.Errorf("unbekannte Aktion %q", b.Action)
	}
	n := 0
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		for _, id := range b.IDs {
			var exists int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE id = ?", id).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				continue
			}
			n++
			var err error
			switch b.Action {
			case "add_tags":
				for _, t := range b.Tags {
					if t = normalizeTag(t); t != "" {
						if _, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO device_tags(device_id, tag) VALUES (?,?)", id, t); err != nil {
							return err
						}
					}
				}
			case "remove_tags":
				for _, t := range b.Tags {
					if _, err = tx.ExecContext(ctx, "DELETE FROM device_tags WHERE device_id = ? AND tag = ?", id, normalizeTag(t)); err != nil {
						return err
					}
				}
			case "set_state":
				_, err = tx.ExecContext(ctx, "UPDATE devices SET state = ?, updated_at = ? WHERE id = ?", b.Value, db.Now(), id)
			case "set_criticality":
				_, err = tx.ExecContext(ctx, "UPDATE devices SET criticality = ?, updated_at = ? WHERE id = ?", b.Value, db.Now(), id)
			case "add_group":
				_, err = tx.ExecContext(ctx, "INSERT OR IGNORE INTO group_members(group_id, device_id) VALUES (?,?)", b.Group, id)
			case "remove_group":
				_, err = tx.ExecContext(ctx, "DELETE FROM group_members WHERE group_id = ? AND device_id = ?", b.Group, id)
			case "delete":
				_, err = tx.ExecContext(ctx, `UPDATE events SET payload = json_set(payload, '$.device_name',
					(SELECT COALESCE(NULLIF(display_name, ''), NULLIF(hostname, ''), primary_ip) FROM devices WHERE id = ?))
					WHERE device_id = ? AND json_extract(payload, '$.device_name') IS NULL`, id, id)
				if err == nil {
					_, err = tx.ExecContext(ctx, "DELETE FROM devices WHERE id = ?", id)
				}
			}
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		typ := "updated"
		if b.Action == "delete" {
			typ = "deleted"
		}
		for _, id := range b.IDs {
			s.publishDevice(typ, id)
		}
	}
	return n, err
}

// TagCount is a tag with its usage.
type TagCount struct {
	Tag   string `json:"tag"`
	Count int    `json:"count"`
}

// Tags lists all tags with their usage.
func (s *Store) Tags(ctx context.Context) ([]TagCount, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT tag, COUNT(*) FROM device_tags GROUP BY tag ORDER BY tag")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TagCount{}
	for rows.Next() {
		var t TagCount
		if err := rows.Scan(&t.Tag, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ---------------------------------------------------------------- plugin read access

// DeviceInfos returns plugin read models for the given device ids.
func (s *Store) DeviceInfos(ctx context.Context, ids []int64) ([]plugin.DeviceInfo, error) {
	out := make([]plugin.DeviceInfo, 0, len(ids))
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		in := db.Placeholders(len(chunk))
		args := db.Int64Args(chunk)
		rows, err := s.db.R.QueryContext(ctx, `SELECT id, display_name, hostname, vendor, model, type, os, primary_ip, primary_mac, state,
			criticality, online, last_seen FROM devices WHERE id IN (`+in+`) ORDER BY ip_key, id`, args...)
		if err != nil {
			return nil, err
		}
		idx := map[int64]int{}
		base := len(out)
		for rows.Next() {
			var (
				d       plugin.DeviceInfo
				display string
				last    sql.NullInt64
			)
			if err := rows.Scan(&d.ID, &display, &d.Hostname, &d.Vendor, &d.Model, &d.Type, &d.OS, &d.PrimaryIP, &d.PrimaryMAC,
				&d.State, &d.Criticality, &d.Online, &last); err != nil {
				rows.Close()
				return nil, err
			}
			d.Name = displayName(display, d.Hostname, d.PrimaryIP, d.PrimaryMAC, d.ID)
			if t := db.NullTime(last); t != nil {
				d.LastSeen = *t
			}
			d.IPs, d.MACs, d.Tags = []string{}, []string{}, []string{}
			idx[d.ID] = len(out)
			out = append(out, d)
		}
		rows.Close()
		scanPairs := func(q string, set func(d *plugin.DeviceInfo, v string)) error {
			r, err := s.db.R.QueryContext(ctx, q, args...)
			if err != nil {
				return err
			}
			defer r.Close()
			for r.Next() {
				var (
					id int64
					v  string
				)
				if err := r.Scan(&id, &v); err != nil {
					return err
				}
				if i, ok := idx[id]; ok && i >= base {
					set(&out[i], v)
				}
			}
			return r.Err()
		}
		if err := scanPairs("SELECT device_id, ip FROM device_ips WHERE gone_at IS NULL AND device_id IN ("+in+") ORDER BY ip_key",
			func(d *plugin.DeviceInfo, v string) { d.IPs = append(d.IPs, v) }); err != nil {
			return nil, err
		}
		if err := scanPairs("SELECT device_id, mac FROM device_macs WHERE device_id IN ("+in+")",
			func(d *plugin.DeviceInfo, v string) { d.MACs = append(d.MACs, v) }); err != nil {
			return nil, err
		}
		if err := scanPairs("SELECT device_id, tag FROM device_tags WHERE device_id IN ("+in+")",
			func(d *plugin.DeviceInfo, v string) { d.Tags = append(d.Tags, v) }); err != nil {
			return nil, err
		}
		prow, err := s.db.R.QueryContext(ctx, `SELECT device_id, ip, proto, port, service, product, version, tunnel FROM ports
			WHERE gone_at IS NULL AND device_id IN (`+in+`) ORDER BY proto, port`, args...)
		if err != nil {
			return nil, err
		}
		for prow.Next() {
			var (
				id int64
				p  plugin.PortRef
			)
			if err := prow.Scan(&id, &p.IP, &p.Proto, &p.Port, &p.Service, &p.Product, &p.Version, &p.Tunnel); err != nil {
				prow.Close()
				return nil, err
			}
			if i, ok := idx[id]; ok && i >= base {
				out[i].Ports = append(out[i].Ports, p)
			}
		}
		prow.Close()
	}
	return out, nil
}

// Device implements plugin.InventoryReader.
func (s *Store) Device(ctx context.Context, id int64) (*plugin.DeviceInfo, error) {
	list, err := s.DeviceInfos(ctx, []int64{id})
	if err != nil {
		return nil, err
	}
	if len(list) == 0 {
		return nil, db.ErrNotFound
	}
	return &list[0], nil
}

// Devices implements plugin.InventoryReader ("" = all devices not ignored).
func (s *Store) Devices(ctx context.Context, query string) ([]plugin.DeviceInfo, error) {
	var ids []int64
	var err error
	if strings.TrimSpace(query) == "" {
		ids, err = queryIDs(ctx, s.db.R, "SELECT id FROM devices WHERE state <> 'ignored'")
	} else {
		ids, err = s.MatchingIDs(ctx, query)
	}
	if err != nil {
		return nil, err
	}
	return s.DeviceInfos(ctx, ids)
}

// DeviceByMAC implements plugin.InventoryReader.
func (s *Store) DeviceByMAC(ctx context.Context, mac string) (*plugin.DeviceInfo, error) {
	n, ok := netutil.NormalizeMAC(mac)
	if !ok {
		return nil, db.ErrNotFound
	}
	var id int64
	if err := s.db.R.QueryRowContext(ctx, "SELECT device_id FROM device_macs WHERE mac = ?", n).Scan(&id); err != nil {
		return nil, db.NotFound(err)
	}
	return s.Device(ctx, id)
}

// DeviceByIP implements plugin.InventoryReader.
func (s *Store) DeviceByIP(ctx context.Context, ip string) (*plugin.DeviceInfo, error) {
	id, _, err := ipOwner(ctx, s.db.R, ip)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		return nil, db.ErrNotFound
	}
	return s.Device(ctx, id)
}
