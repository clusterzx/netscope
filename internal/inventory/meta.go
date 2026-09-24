package inventory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"strconv"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

// ---------------------------------------------------------------- subnets

// Access modes of a subnet.
const (
	AccessDirect    = "direct"    // attached to a local interface (ARP, broadcasts)
	AccessRouted    = "routed"    // reached through a router, layer 3 only
	AccessWireGuard = "wireguard" // reached through NetScope's own WireGuard tunnel
)

// Subnet is a configured network.
type Subnet struct {
	ID        int64  `json:"id"`
	CIDR      string `json:"cidr"`
	Name      string `json:"name"`
	Interface string `json:"interface"`
	VLAN      *int   `json:"vlan,omitempty"`
	Gateway   string `json:"gateway"`
	Enabled   bool   `json:"enabled"`
	Notes     string `json:"notes"`
	// Access says how the subnet is reached: direct | routed | wireguard.
	Access string `json:"access"`
	// TunnelCredentialID is the WireGuard credential of the tunnel (access wireguard).
	TunnelCredentialID *int64    `json:"tunnelCredentialId,omitempty"`
	DeviceCount        int       `json:"deviceCount"`
	CreatedAt          time.Time `json:"createdAt"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

// Routed reports whether the subnet is reached through a router or tunnel.
func (s *Subnet) Routed() bool { return s.Access == AccessRouted || s.Access == AccessWireGuard }

func (s *Subnet) validate() error {
	p, err := netip.ParsePrefix(strings.TrimSpace(s.CIDR))
	if err != nil {
		return plugin.FieldErr("cidr", fmt.Sprintf("ungültiges CIDR %q", s.CIDR))
	}
	p = p.Masked()
	if p.Addr().Is4() && p.Bits() < 16 {
		return plugin.FieldErr("cidr", fmt.Sprintf("Subnetz %s ist zu groß (maximal /16)", p))
	}
	s.CIDR = p.String()
	if s.Gateway != "" {
		a, err := netip.ParseAddr(s.Gateway)
		if err != nil || !p.Contains(a) {
			return plugin.FieldErr("gateway", fmt.Sprintf("Gateway %q liegt nicht im Subnetz", s.Gateway))
		}
	}
	if s.VLAN != nil && (*s.VLAN < 1 || *s.VLAN > 4094) {
		return plugin.FieldErr("vlan", "VLAN muss zwischen 1 und 4094 liegen")
	}
	if s.Interface != "" && !regexp.MustCompile(`^[A-Za-z0-9_.@:-]{1,32}$`).MatchString(s.Interface) {
		return plugin.FieldErr("interface", fmt.Sprintf("ungültiger Interface-Name %q", s.Interface))
	}
	switch s.Access {
	case "":
		s.Access = AccessDirect
	case AccessDirect, AccessRouted, AccessWireGuard:
	default:
		return plugin.FieldErr("access", fmt.Sprintf("unbekannte Erreichbarkeit %q", s.Access))
	}
	if s.Access == AccessWireGuard {
		if s.TunnelCredentialID == nil || *s.TunnelCredentialID <= 0 {
			return plugin.FieldErr("tunnelCredentialId", "Tunnel wählen oder eine WireGuard-Konfiguration hochladen")
		}
		s.Interface = "" // the tunnel interface is managed by NetScope
	} else {
		s.TunnelCredentialID = nil
	}
	return nil
}

// Subnets lists all subnets.
func (s *Store) ListSubnets(ctx context.Context) ([]Subnet, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT s.id, s.cidr, s.name, s.interface, s.vlan, s.gateway, s.enabled, s.notes, s.access, s.tunnel_credential_id, s.created_at, s.updated_at,
		(SELECT COUNT(DISTINCT i.device_id) FROM device_ips i WHERE i.subnet_id = s.id AND i.gone_at IS NULL)
		FROM subnets s ORDER BY s.cidr`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Subnet{}
	for rows.Next() {
		var (
			sn       Subnet
			vlan     sql.NullInt64
			tunnel   sql.NullInt64
			cre, upd int64
		)
		if err := rows.Scan(&sn.ID, &sn.CIDR, &sn.Name, &sn.Interface, &vlan, &sn.Gateway, &sn.Enabled, &sn.Notes, &sn.Access, &tunnel, &cre, &upd, &sn.DeviceCount); err != nil {
			return nil, err
		}
		if tunnel.Valid {
			sn.TunnelCredentialID = &tunnel.Int64
		}
		if vlan.Valid {
			v := int(vlan.Int64)
			sn.VLAN = &v
		}
		sn.CreatedAt, sn.UpdatedAt = db.Time(cre), db.Time(upd)
		out = append(out, sn)
	}
	return out, rows.Err()
}

// SaveSubnet creates (ID 0) or updates a subnet and re-assigns addresses to subnets.
func (s *Store) SaveSubnet(ctx context.Context, sn *Subnet) error {
	if err := sn.validate(); err != nil {
		return err
	}
	var vlan, tunnel any
	if sn.VLAN != nil {
		vlan = *sn.VLAN
	}
	if sn.TunnelCredentialID != nil {
		tunnel = *sn.TunnelCredentialID
	}
	now := db.Now()
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		if sn.ID == 0 {
			res, err := tx.ExecContext(ctx, `INSERT INTO subnets(cidr, name, interface, vlan, gateway, enabled, notes, access, tunnel_credential_id, created_at, updated_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?)`, sn.CIDR, sn.Name, sn.Interface, vlan, sn.Gateway, db.Bool(sn.Enabled), sn.Notes, sn.Access, tunnel, now, now)
			if err != nil {
				return uniqueErr(err, "cidr", "Subnetz existiert bereits")
			}
			sn.ID, _ = res.LastInsertId()
			return nil
		}
		res, err := tx.ExecContext(ctx, `UPDATE subnets SET cidr = ?, name = ?, interface = ?, vlan = ?, gateway = ?, enabled = ?, notes = ?, access = ?,
			tunnel_credential_id = ?, updated_at = ? WHERE id = ?`, sn.CIDR, sn.Name, sn.Interface, vlan, sn.Gateway, db.Bool(sn.Enabled), sn.Notes,
			sn.Access, tunnel, now, sn.ID)
		if err != nil {
			return uniqueErr(err, "cidr", "Subnetz existiert bereits")
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return db.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}
	return s.reassignSubnets(ctx)
}

// DeleteSubnet removes a subnet.
func (s *Store) DeleteSubnet(ctx context.Context, id int64) error {
	res, err := s.db.W.ExecContext(ctx, "DELETE FROM subnets WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return s.reassignSubnets(ctx)
}

func (s *Store) reassignSubnets(ctx context.Context) error {
	if err := s.reloadSubnets(ctx); err != nil {
		return err
	}
	// addresses of site devices belong to the site's subnets, not to ours
	rows, err := s.db.R.QueryContext(ctx, `SELECT i.id, i.ip FROM device_ips i JOIN devices d ON d.id = i.device_id
		WHERE i.gone_at IS NULL AND d.site_id IS NULL`)
	if err != nil {
		return err
	}
	type row struct {
		id int64
		ip string
	}
	var list []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.ip); err != nil {
			rows.Close()
			return err
		}
		list = append(list, r)
	}
	rows.Close()
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		for _, r := range list {
			if _, err := tx.ExecContext(ctx, "UPDATE device_ips SET subnet_id = ? WHERE id = ?", s.subnetFor(r.ip), r.id); err != nil {
				return err
			}
		}
		return nil
	})
}

// SeedSubnets adds the locally attached networks when no subnet is configured yet.
func (s *Store) SeedSubnets(ctx context.Context) ([]string, error) {
	var n int
	if err := s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM subnets").Scan(&n); err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, nil
	}
	local, err := netutil.LocalSubnets()
	if err != nil {
		return nil, err
	}
	gateways := netutil.DefaultGateways()
	var added []string
	for _, l := range local {
		sn := &Subnet{CIDR: l.Prefix.String(), Name: l.Interface, Interface: l.Interface, Enabled: true,
			Notes: "automatisch beim ersten Start erkannt"}
		if gw, ok := gateways[l.Interface]; ok && l.Prefix.Contains(gw) {
			sn.Gateway = gw.String()
		}
		if err := s.SaveSubnet(ctx, sn); err != nil {
			continue
		}
		added = append(added, sn.CIDR)
	}
	return added, nil
}

// FillGateways sets the gateway of subnets that have none from the host's default routes
// (the gateway is needed for the layer 3 topology). It returns the updated subnets.
func (s *Store) FillGateways(ctx context.Context) ([]string, error) {
	gateways := netutil.DefaultGateways()
	if len(gateways) == 0 {
		return nil, nil
	}
	list, err := s.ListSubnets(ctx)
	if err != nil {
		return nil, err
	}
	var updated []string
	for _, sn := range list {
		if sn.Gateway != "" {
			continue
		}
		p, err := netip.ParsePrefix(sn.CIDR)
		if err != nil {
			continue
		}
		for _, gw := range gateways {
			if p.Contains(gw) {
				if _, err := s.db.W.ExecContext(ctx, "UPDATE subnets SET gateway = ?, updated_at = ? WHERE id = ? AND gateway = ''",
					gw.String(), db.Now(), sn.ID); err != nil {
					return updated, err
				}
				updated = append(updated, sn.CIDR+" → "+gw.String())
				break
			}
		}
	}
	return updated, nil
}

// Subnets implements plugin.InventoryReader (enabled subnets only).
func (s *Store) Subnets(ctx context.Context) ([]plugin.SubnetTarget, error) {
	list, err := s.ListSubnets(ctx)
	if err != nil {
		return nil, err
	}
	var out []plugin.SubnetTarget
	for _, sn := range list {
		if !sn.Enabled {
			continue
		}
		p, err := netip.ParsePrefix(sn.CIDR)
		if err != nil {
			continue
		}
		out = append(out, plugin.SubnetTarget{ID: sn.ID, CIDR: p, Name: sn.Name, Interface: sn.Interface, Gateway: sn.Gateway, Routed: sn.Routed()})
	}
	return out, nil
}

func uniqueErr(err error, field, msg string) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		if field == "" {
			return errors.New(msg)
		}
		return plugin.FieldErr(field, msg)
	}
	return err
}

// ResolveTargets turns a plugin scope into concrete subnets and devices.
func (s *Store) ResolveTargets(ctx context.Context, scope plugin.Scope, mode plugin.TargetMode) (plugin.Targets, error) {
	var t plugin.Targets
	if mode == plugin.TargetNone {
		return t, nil
	}
	all, err := s.Subnets(ctx)
	if err != nil {
		return t, err
	}
	var subnets []plugin.SubnetTarget
	if scope.AllSubnets || (len(scope.Subnets) == 0 && !scope.DeviceRestricted()) {
		subnets = all
	} else {
		want := map[string]bool{}
		for _, c := range scope.Subnets {
			if p, err := plugin.ParsePrefix(c); err == nil {
				want[p.String()] = true
			}
		}
		for _, sn := range all {
			if want[sn.CIDR.String()] {
				subnets = append(subnets, sn)
			}
		}
	}
	var ids []int64
	if scope.DeviceRestricted() {
		t.DeviceMode = true
		idSet := map[int64]bool{}
		add := func(list []int64) {
			for _, id := range list {
				idSet[id] = true
			}
		}
		add(scope.Devices)
		for _, g := range scope.Groups {
			members, err := s.GroupMemberIDs(ctx, g)
			if err != nil {
				return t, err
			}
			add(members)
		}
		if len(scope.Tags) > 0 {
			tagIDs, err := queryIDs(ctx, s.db.R, "SELECT DISTINCT device_id FROM device_tags WHERE tag IN ("+db.Placeholders(len(scope.Tags))+")",
				db.StringArgs(scope.Tags)...)
			if err != nil {
				return t, err
			}
			add(tagIDs)
		}
		onlyQuery := len(scope.Devices) == 0 && len(scope.Groups) == 0 && len(scope.Tags) == 0
		if scope.Query != "" {
			qIDs, err := s.MatchingIDs(ctx, scope.Query)
			if err != nil {
				return t, fmt.Errorf("Scope-Filter: %w", err)
			}
			if onlyQuery {
				add(qIDs)
			} else {
				keep := map[int64]bool{}
				for _, id := range qIDs {
					keep[id] = true
				}
				for id := range idSet {
					if !keep[id] {
						delete(idSet, id)
					}
				}
			}
		}
		for id := range idSet {
			ids = append(ids, id)
		}
		// explicitly selected devices are always included, the rest only when not ignored
		if len(scope.Devices) == 0 && len(ids) > 0 {
			ids, err = queryIDs(ctx, s.db.R, "SELECT id FROM devices WHERE state <> 'ignored' AND id IN ("+db.Placeholders(len(ids))+")", db.Int64Args(ids)...)
			if err != nil {
				return t, err
			}
		}
		if len(scope.Subnets) > 0 {
			t.Subnets = subnets
		}
	} else {
		t.Subnets = subnets
		if len(subnets) > 0 {
			sids := make([]int64, 0, len(subnets))
			for _, sn := range subnets {
				sids = append(sids, sn.ID)
			}
			ids, err = queryIDs(ctx, s.db.R, `SELECT DISTINCT d.id FROM devices d JOIN device_ips i ON i.device_id = d.id AND i.gone_at IS NULL
				WHERE d.state <> 'ignored' AND i.subnet_id IN (`+db.Placeholders(len(sids))+`)`, db.Int64Args(sids)...)
			if err != nil {
				return t, err
			}
		}
	}
	// devices of sites are scanned there, never from here (their addresses are not ours)
	if len(ids) > 0 {
		if ids, err = s.localIDs(ctx, ids); err != nil {
			return t, err
		}
	}
	devs, err := s.DeviceInfos(ctx, ids)
	if err != nil {
		return t, err
	}
	// in device mode with subnet restriction keep only devices inside those subnets
	if t.DeviceMode && len(scope.Subnets) > 0 {
		var kept []plugin.DeviceInfo
		for _, d := range devs {
			if deviceInSubnets(d, subnets) {
				kept = append(kept, d)
			}
		}
		devs = kept
	}
	t.Devices = devs
	if t.Devices == nil {
		t.Devices = []plugin.DeviceInfo{}
	}
	return t, nil
}

// localIDs keeps the ids of devices of this instance (not delivered by a site).
func (s *Store) localIDs(ctx context.Context, ids []int64) ([]int64, error) {
	var sited int
	if err := s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE site_id IS NOT NULL").Scan(&sited); err != nil || sited == 0 {
		return ids, err
	}
	var out []int64
	for start := 0; start < len(ids); start += 500 {
		chunk := ids[start:min(start+500, len(ids))]
		list, err := queryIDs(ctx, s.db.R, "SELECT id FROM devices WHERE site_id IS NULL AND id IN ("+db.Placeholders(len(chunk))+")", db.Int64Args(chunk)...)
		if err != nil {
			return nil, err
		}
		out = append(out, list...)
	}
	return out, nil
}

func deviceInSubnets(d plugin.DeviceInfo, subnets []plugin.SubnetTarget) bool {
	for _, ip := range d.IPs {
		a, err := netip.ParseAddr(ip)
		if err != nil {
			continue
		}
		for _, sn := range subnets {
			if sn.CIDR.Contains(a) {
				return true
			}
		}
	}
	return false
}

// ---------------------------------------------------------------- groups

// Group is a device group (manual membership or filter query).
type Group struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Kind        string    `json:"kind"` // manual | query
	Query       string    `json:"query"`
	Color       string    `json:"color"`
	MemberCount int       `json:"memberCount"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

var colorRe = regexp.MustCompile(`^(#[0-9a-fA-F]{6}|)$`)

func (s *Store) queryGroups(ctx context.Context) ([]Group, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT id, name, query, color FROM groups WHERE kind = 'query' ORDER BY name")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Group
	for rows.Next() {
		g := Group{Kind: "query"}
		if err := rows.Scan(&g.ID, &g.Name, &g.Query, &g.Color); err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, rows.Err()
}

// ListGroups returns all groups with member counts.
func (s *Store) ListGroups(ctx context.Context) ([]Group, error) {
	rows, err := s.db.R.QueryContext(ctx, `SELECT id, name, description, kind, query, color, created_at, updated_at,
		(SELECT COUNT(*) FROM group_members m WHERE m.group_id = groups.id) FROM groups ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	var out []Group
	for rows.Next() {
		var (
			g        Group
			cre, upd int64
		)
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.Kind, &g.Query, &g.Color, &cre, &upd, &g.MemberCount); err != nil {
			rows.Close()
			return nil, err
		}
		g.CreatedAt, g.UpdatedAt = db.Time(cre), db.Time(upd)
		out = append(out, g)
	}
	rows.Close()
	for i := range out {
		if out[i].Kind == "query" {
			ids, err := s.MatchingIDs(ctx, out[i].Query)
			if err == nil {
				out[i].MemberCount = len(ids)
			}
		}
	}
	if out == nil {
		out = []Group{}
	}
	return out, nil
}

// SaveGroup creates (ID 0) or updates a group.
func (s *Store) SaveGroup(ctx context.Context, g *Group) error {
	g.Name = strings.TrimSpace(g.Name)
	if g.Name == "" {
		return plugin.FieldErr("name", "Name erforderlich")
	}
	if g.Kind != "manual" && g.Kind != "query" {
		return plugin.FieldErr("kind", "Art muss manual oder query sein")
	}
	if !colorRe.MatchString(g.Color) {
		return plugin.FieldErr("color", "Farbe als #rrggbb angeben")
	}
	if g.Kind == "query" {
		if strings.TrimSpace(g.Query) == "" {
			return plugin.FieldErr("query", "regelbasierte Gruppen brauchen einen Filter")
		}
		if strings.Contains(strings.ToLower(g.Query), "group:"+strings.ToLower(g.Name)) {
			return plugin.FieldErr("query", "eine Gruppe kann sich nicht selbst referenzieren")
		}
		if _, _, err := s.CompileQuery(ctx, g.Query); err != nil {
			return plugin.FieldErr("query", "Filter: "+err.Error())
		}
	} else {
		g.Query = ""
	}
	now := db.Now()
	if g.ID == 0 {
		res, err := s.db.W.ExecContext(ctx, `INSERT INTO groups(name, description, kind, query, color, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			g.Name, g.Description, g.Kind, g.Query, g.Color, now, now)
		if err != nil {
			return uniqueErr(err, "name", "Name bereits vergeben")
		}
		g.ID, _ = res.LastInsertId()
		return nil
	}
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE groups SET name = ?, description = ?, kind = ?, query = ?, color = ?, updated_at = ? WHERE id = ?`,
			g.Name, g.Description, g.Kind, g.Query, g.Color, now, g.ID)
		if err != nil {
			return uniqueErr(err, "name", "Name bereits vergeben")
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return db.ErrNotFound
		}
		if g.Kind == "query" {
			_, err = tx.ExecContext(ctx, "DELETE FROM group_members WHERE group_id = ?", g.ID)
		}
		return err
	})
}

// DeleteGroup removes a group.
func (s *Store) DeleteGroup(ctx context.Context, id int64) error {
	res, err := s.db.W.ExecContext(ctx, "DELETE FROM groups WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return nil
}

// GroupMemberIDs returns the members of a group.
func (s *Store) GroupMemberIDs(ctx context.Context, id int64) ([]int64, error) {
	var kind, q string
	if err := s.db.R.QueryRowContext(ctx, "SELECT kind, query FROM groups WHERE id = ?", id).Scan(&kind, &q); err != nil {
		return nil, fmt.Errorf("Gruppe %d: %w", id, db.NotFound(err))
	}
	if kind == "query" {
		return s.MatchingIDs(ctx, q)
	}
	return queryIDs(ctx, s.db.R, "SELECT device_id FROM group_members WHERE group_id = ?", id)
}

// DeviceInGroup reports whether a device belongs to a group.
func (s *Store) DeviceInGroup(ctx context.Context, deviceID, groupID int64) (bool, error) {
	var kind, q string
	if err := s.db.R.QueryRowContext(ctx, "SELECT kind, query FROM groups WHERE id = ?", groupID).Scan(&kind, &q); err != nil {
		return false, db.NotFound(err)
	}
	if kind == "manual" {
		var n int
		err := s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM group_members WHERE group_id = ? AND device_id = ?", groupID, deviceID).Scan(&n)
		return n > 0, err
	}
	where, args, err := s.CompileQuery(ctx, q)
	if err != nil {
		return false, err
	}
	var n int
	err = s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices d WHERE d.id = ? AND ("+where+")", append([]any{deviceID}, args...)...).Scan(&n)
	return n > 0, err
}

// DeviceMatches reports whether a device matches a filter query.
func (s *Store) DeviceMatches(ctx context.Context, deviceID int64, query string) (bool, error) {
	where, args, err := s.CompileQuery(ctx, query)
	if err != nil {
		return false, err
	}
	var n int
	err = s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices d WHERE d.id = ? AND ("+where+")", append([]any{deviceID}, args...)...).Scan(&n)
	return n > 0, err
}

// ---------------------------------------------------------------- custom fields

// CustomField defines a user-defined device attribute.
type CustomField struct {
	ID          int64     `json:"id"`
	Key         string    `json:"key"`
	Label       string    `json:"label"`
	Type        string    `json:"type"` // text | number | date | url | bool
	Description string    `json:"description"`
	SortOrder   int       `json:"sortOrder"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

var cfKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

func (c CustomField) check(v any) error {
	switch c.Type {
	case "text":
		if _, ok := v.(string); !ok {
			return errors.New("Text erwartet")
		}
	case "number":
		switch x := v.(type) {
		case float64, int, int64:
		case string:
			if _, err := strconv.ParseFloat(x, 64); err != nil {
				return errors.New("Zahl erwartet")
			}
		default:
			return errors.New("Zahl erwartet")
		}
	case "date":
		str, ok := v.(string)
		if !ok {
			return errors.New("Datum erwartet (YYYY-MM-DD)")
		}
		if _, err := time.Parse("2006-01-02", str); err != nil {
			return errors.New("Datum erwartet (YYYY-MM-DD)")
		}
	case "url":
		str, ok := v.(string)
		if !ok || (str != "" && !(strings.HasPrefix(str, "http://") || strings.HasPrefix(str, "https://"))) {
			return errors.New("URL erwartet (http/https)")
		}
	case "bool":
		if _, ok := v.(bool); !ok {
			return errors.New("Ja/Nein erwartet")
		}
	}
	return nil
}

// CustomFields lists all custom field definitions.
func (s *Store) CustomFields(ctx context.Context) ([]CustomField, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT id, key, label, type, description, sort_order, created_at, updated_at FROM custom_fields ORDER BY sort_order, label")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []CustomField{}
	for rows.Next() {
		var (
			c        CustomField
			cre, upd int64
		)
		if err := rows.Scan(&c.ID, &c.Key, &c.Label, &c.Type, &c.Description, &c.SortOrder, &cre, &upd); err != nil {
			return nil, err
		}
		c.CreatedAt, c.UpdatedAt = db.Time(cre), db.Time(upd)
		out = append(out, c)
	}
	return out, rows.Err()
}

// SaveCustomField creates (ID 0) or updates a custom field. The key cannot change.
func (s *Store) SaveCustomField(ctx context.Context, c *CustomField) error {
	c.Label = strings.TrimSpace(c.Label)
	if c.Label == "" {
		return plugin.FieldErr("label", "Bezeichnung erforderlich")
	}
	switch c.Type {
	case "text", "number", "date", "url", "bool":
	default:
		return plugin.FieldErr("type", "Typ: text, number, date, url oder bool")
	}
	now := db.Now()
	if c.ID == 0 {
		if !cfKeyRe.MatchString(c.Key) {
			return plugin.FieldErr("key", "Schlüssel: Kleinbuchstaben, Ziffern und _ (beginnt mit Buchstabe)")
		}
		res, err := s.db.W.ExecContext(ctx, `INSERT INTO custom_fields(key, label, type, description, sort_order, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?)`, c.Key, c.Label, c.Type, c.Description, c.SortOrder, now, now)
		if err != nil {
			return uniqueErr(err, "key", "Schlüssel bereits vergeben")
		}
		c.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.db.W.ExecContext(ctx, "UPDATE custom_fields SET label = ?, type = ?, description = ?, sort_order = ?, updated_at = ? WHERE id = ?",
		c.Label, c.Type, c.Description, c.SortOrder, now, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return nil
}

// DeleteCustomField removes a definition and its values from all devices.
func (s *Store) DeleteCustomField(ctx context.Context, id int64) error {
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		var key string
		if err := tx.QueryRowContext(ctx, "SELECT key FROM custom_fields WHERE id = ?", id).Scan(&key); err != nil {
			return db.NotFound(err)
		}
		if _, err := tx.ExecContext(ctx, `UPDATE devices SET custom = json_remove(custom, ?) WHERE json_extract(custom, ?) IS NOT NULL`,
			`$."`+key+`"`, `$."`+key+`"`); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM custom_fields WHERE id = ?", id)
		return err
	})
}

// ---------------------------------------------------------------- saved views

// SavedView is a stored device list configuration.
type SavedView struct {
	ID        int64     `json:"id"`
	Name      string    `json:"name"`
	Query     string    `json:"query"`
	Columns   []string  `json:"columns"`
	Sort      string    `json:"sort"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ListViews returns all saved views.
func (s *Store) ListViews(ctx context.Context) ([]SavedView, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT id, name, query, columns, sort, created_at, updated_at FROM saved_views ORDER BY name COLLATE NOCASE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SavedView{}
	for rows.Next() {
		var (
			v        SavedView
			cols     string
			cre, upd int64
		)
		if err := rows.Scan(&v.ID, &v.Name, &v.Query, &cols, &v.Sort, &cre, &upd); err != nil {
			return nil, err
		}
		_ = db.Unmarshal(cols, &v.Columns)
		if v.Columns == nil {
			v.Columns = []string{}
		}
		v.CreatedAt, v.UpdatedAt = db.Time(cre), db.Time(upd)
		out = append(out, v)
	}
	return out, rows.Err()
}

// SaveView creates (ID 0) or updates a saved view.
func (s *Store) SaveView(ctx context.Context, v *SavedView) error {
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		return plugin.FieldErr("name", "Name erforderlich")
	}
	if _, _, err := s.CompileQuery(ctx, v.Query); err != nil {
		return plugin.FieldErr("query", "Filter: "+err.Error())
	}
	if key := strings.TrimPrefix(v.Sort, "-"); key != "" {
		if _, ok := sortColumns[key]; !ok {
			return plugin.FieldErr("sort", fmt.Sprintf("unbekanntes Sortierfeld %q", key))
		}
	}
	if v.Columns == nil {
		v.Columns = []string{}
	}
	now := db.Now()
	if v.ID == 0 {
		res, err := s.db.W.ExecContext(ctx, "INSERT INTO saved_views(name, query, columns, sort, created_at, updated_at) VALUES (?,?,?,?,?,?)",
			v.Name, v.Query, db.JSON(v.Columns), v.Sort, now, now)
		if err != nil {
			return uniqueErr(err, "name", "Name bereits vergeben")
		}
		v.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := s.db.W.ExecContext(ctx, "UPDATE saved_views SET name = ?, query = ?, columns = ?, sort = ?, updated_at = ? WHERE id = ?",
		v.Name, v.Query, db.JSON(v.Columns), v.Sort, now, v.ID)
	if err != nil {
		return uniqueErr(err, "name", "Name bereits vergeben")
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return nil
}

// DeleteView removes a saved view.
func (s *Store) DeleteView(ctx context.Context, id int64) error {
	res, err := s.db.W.ExecContext(ctx, "DELETE FROM saved_views WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return nil
}
