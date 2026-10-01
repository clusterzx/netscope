package rack

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// Port is a port of a rack item.
type Port struct {
	Name      string `json:"name"`
	Label     string `json:"label,omitempty"`
	Media     string `json:"media"`            // copper | sfp
	Status    string `json:"status,omitempty"` // up | down (SNMP)
	SpeedMbps int64  `json:"speedMbps,omitempty"`
	// Source tells where the port comes from: snmp (interface of the device), count (counted
	// by hand) or seen (named by a connection, a cable or port data).
	Source string `json:"source"`
	// Device is the device plugged in directly (set by hand).
	Device *DeviceRef  `json:"device,omitempty"`
	Cables []PortCable `json:"cables"`
	// Detected lists the devices other sources see on this port (SNMP FDB/LLDP, imports).
	Detected     []Detected `json:"detected"`
	DetectedMore int        `json:"detectedMore,omitempty"`
}

// PortCable is a cable at a port with the way it leads.
type PortCable struct {
	ID    int64  `json:"id"`
	Label string `json:"label,omitempty"`
	Color string `json:"color,omitempty"`
	Peer  Target `json:"peer"`
	// End is where the cable leads to through patch panels (nil when it ends at Peer).
	End *Target `json:"end,omitempty"`
}

// Target is a port somewhere in the racks.
type Target struct {
	ItemID   int64      `json:"itemId"`
	ItemName string     `json:"itemName"`
	RackID   int64      `json:"rackId"`
	RackName string     `json:"rackName"`
	Port     string     `json:"port"`
	Label    string     `json:"label,omitempty"`
	Device   *DeviceRef `json:"device,omitempty"`
}

// Detected is a device another source sees on a port.
type Detected struct {
	Device     DeviceRef `json:"device"`
	Kind       string    `json:"kind"`   // switch_port | lldp | manual
	Source     string    `json:"source"` // plugin, topology or manual
	RemotePort string    `json:"remotePort,omitempty"`
	// Elsewhere names the rack port the device is connected to by hand, when that is
	// another one.
	Elsewhere string `json:"elsewhere,omitempty"`
}

type portKey struct {
	item int64
	port string
}

type portData struct {
	label  string
	device int64
}

type ifc struct {
	Index       int    `json:"index"`
	Name        string `json:"name"`
	Type        int    `json:"type"`
	SpeedMbps   int64  `json:"speedMbps"`
	OperStatus  string `json:"operStatus"`
	AdminStatus string `json:"adminStatus"`
}

type relation struct {
	parent, child         int64
	kind, source          string
	parentPort, childPort string
}

// state is everything the racks show, loaded at once.
type state struct {
	racks   map[int64]*Rack
	items   []*Item
	byID    map[int64]*Item
	ports   map[portKey]*portData
	cables  []Cable
	devices map[int64]*DeviceRef
	ifaces  map[int64][]ifc // SNMP interfaces per device
	rels    []relation
	names   map[int64][]Port // port list per item (memo)
}

func (s *Service) load(ctx context.Context) (*state, error) {
	st := &state{racks: map[int64]*Rack{}, byID: map[int64]*Item{}, ports: map[portKey]*portData{}, devices: map[int64]*DeviceRef{},
		ifaces: map[int64][]ifc{}, names: map[int64][]Port{}}
	q := s.db.R
	each := func(query string, fn func(r *sql.Rows) error, args ...any) error {
		rows, err := q.QueryContext(ctx, query, args...)
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
	if err := each("SELECT "+rackCols+" FROM racks", func(r *sql.Rows) error {
		rk, err := scanRack(r)
		st.racks[rk.ID] = &rk
		return err
	}); err != nil {
		return nil, err
	}
	if err := each(`SELECT id, rack_id, kind, IFNULL(device_id, 0), device_name, label, position, height, face, full_depth, col, cols,
		port_count, port_prefix FROM rack_items ORDER BY rack_id, position DESC, col, id`, func(r *sql.Rows) error {
		it := &Item{}
		if err := r.Scan(&it.ID, &it.RackID, &it.Kind, &it.DeviceID, &it.DeviceName, &it.Label, &it.Position, &it.Height, &it.Face,
			&it.FullDepth, &it.Col, &it.Cols, &it.PortCount, &it.PortPrefix); err != nil {
			return err
		}
		st.items = append(st.items, it)
		st.byID[it.ID] = it
		return nil
	}); err != nil {
		return nil, err
	}
	if err := each("SELECT item_id, port, label, IFNULL(device_id, 0) FROM rack_ports", func(r *sql.Rows) error {
		var (
			k portKey
			d portData
		)
		if err := r.Scan(&k.item, &k.port, &d.label, &d.device); err != nil {
			return err
		}
		st.ports[k] = &d
		return nil
	}); err != nil {
		return nil, err
	}
	if err := each("SELECT id, a_item, a_port, b_item, b_port, label, color FROM rack_cables ORDER BY id", func(r *sql.Rows) error {
		var c Cable
		if err := r.Scan(&c.ID, &c.A.ItemID, &c.A.Port, &c.B.ItemID, &c.B.Port, &c.Label, &c.Color); err != nil {
			return err
		}
		st.cables = append(st.cables, c)
		return nil
	}); err != nil {
		return nil, err
	}
	if err := each(`SELECT id, display_name, hostname, primary_ip, primary_mac, type, vendor, model, online FROM devices`, func(r *sql.Rows) error {
		var (
			d         DeviceRef
			disp, mac string
			host      string
		)
		if err := r.Scan(&d.ID, &disp, &host, &d.IP, &mac, &d.Type, &d.Vendor, &d.Model, &d.Online); err != nil {
			return err
		}
		d.Name = firstNonEmpty(disp, host, d.IP, mac, fmt.Sprintf("Gerät %d", d.ID))
		st.devices[d.ID] = &d
		return nil
	}); err != nil {
		return nil, err
	}
	var mounted []int64
	for _, it := range st.items {
		if it.DeviceID > 0 {
			mounted = append(mounted, it.DeviceID)
		}
	}
	if len(mounted) > 0 {
		if err := each("SELECT device_id, data FROM device_inventory WHERE source = 'snmp' AND device_id IN ("+db.Placeholders(len(mounted))+")",
			func(r *sql.Rows) error {
				var (
					id   int64
					data string
					inv  struct {
						Interfaces []ifc `json:"interfaces"`
					}
				)
				if err := r.Scan(&id, &data); err != nil {
					return err
				}
				if json.Unmarshal([]byte(data), &inv) == nil {
					st.ifaces[id] = physical(inv.Interfaces)
				}
				return nil
			}, db.Int64Args(mounted)...); err != nil {
			return nil, err
		}
		if err := each(`SELECT parent_id, child_id, kind, source, parent_port, child_port FROM relations
			WHERE source <> 'rack' AND kind IN ('switch_port', 'lldp', 'manual')
			AND (parent_id IN (`+db.Placeholders(len(mounted))+`) OR child_id IN (`+db.Placeholders(len(mounted))+`))`,
			func(r *sql.Rows) error {
				var rel relation
				if err := r.Scan(&rel.parent, &rel.child, &rel.kind, &rel.source, &rel.parentPort, &rel.childPort); err != nil {
					return err
				}
				st.rels = append(st.rels, rel)
				return nil
			}, append(db.Int64Args(mounted), db.Int64Args(mounted)...)...); err != nil {
			return nil, err
		}
	}
	return st, nil
}

// virtualIfc matches interface names that are no physical ports (Linux and router software).
var virtualIfc = regexp.MustCompile(`(?i)^(lo|br|bridge|docker|veth|virbr|vmbr|vnet|tap|tun|wg|bond|team|vlan|wlan|wifi|ath|ifb|dummy|sit|gre|ip6|ppp|pppoe|l2tp|ovpn|zt|tailscale|cpu|null|mgmt|nve|port-channel|po\d)`)

// physical keeps the interfaces that are physical ports, in ifIndex order.
func physical(list []ifc) []ifc {
	var out []ifc
	for _, i := range list {
		switch i.Type {
		case 6, 62, 69, 117: // ethernetCsmacd, fastEther, fastEtherFX, gigabitEthernet
		default:
			continue
		}
		if i.Name == "" || virtualIfc.MatchString(i.Name) || strings.Contains(i.Name, ".") {
			continue
		}
		out = append(out, i)
	}
	sort.SliceStable(out, func(a, b int) bool { return out[a].Index < out[b].Index })
	return out
}

var sfpName = regexp.MustCompile(`(?i)sfp|qsfp|xe-|te\d|tengig|fortygig|hundredgig|x\d+$`)

func media(name string, speed int64) string {
	if sfpName.MatchString(name) || speed >= 10000 {
		return "sfp"
	}
	return "copper"
}

var trailingNum = regexp.MustCompile(`(\d+)$`)

// match finds the port of a list a name refers to: the same name (case does not matter),
// or – for names from other sources such as "5" or "Port 5" – the only port with the same
// trailing number.
func match(ports []Port, name string) (int, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, false
	}
	for i, p := range ports {
		if p.Name == name {
			return i, true
		}
	}
	for i, p := range ports {
		if strings.EqualFold(p.Name, name) {
			return i, true
		}
	}
	m := trailingNum.FindString(name)
	if m == "" {
		return 0, false
	}
	n, _ := strconv.Atoi(m)
	found := -1
	for i, p := range ports {
		pm := trailingNum.FindString(p.Name)
		if pm == "" {
			continue
		}
		if pn, _ := strconv.Atoi(pm); pn == n {
			if found >= 0 {
				return 0, false // ambiguous (1/0/5 and 1/1/5)
			}
			found = i
		}
	}
	return found, found >= 0
}

// portNames returns the ports of an item without connection data.
func (st *state) portNames(it *Item) []Port {
	if ps, ok := st.names[it.ID]; ok {
		return ps
	}
	var ps []Port
	snmp := st.ifaces[it.DeviceID]
	if it.PortCount > 0 || it.Kind != KindDevice {
		for i := 1; i <= it.PortCount; i++ {
			name := it.PortPrefix + strconv.Itoa(i)
			ps = append(ps, Port{Name: name, Media: media(name, 0), Source: "count"})
		}
		for _, f := range snmp {
			if j, ok := match(ps, f.Name); ok {
				ps[j].Status, ps[j].SpeedMbps = status(f), f.SpeedMbps
			}
		}
	} else {
		for _, f := range snmp {
			ps = append(ps, Port{Name: f.Name, Media: media(f.Name, f.SpeedMbps), Status: status(f), SpeedMbps: f.SpeedMbps, Source: "snmp"})
		}
	}
	// ports named elsewhere: port data, cables and connections of other sources
	var extra []string
	for k := range st.ports {
		if k.item == it.ID {
			extra = append(extra, k.port)
		}
	}
	for _, c := range st.cables {
		if c.A.ItemID == it.ID {
			extra = append(extra, c.A.Port)
		}
		if c.B.ItemID == it.ID {
			extra = append(extra, c.B.Port)
		}
	}
	if it.Kind == KindDevice && it.DeviceID > 0 {
		for _, r := range st.rels {
			if p, ok := r.port(it.DeviceID); ok {
				extra = append(extra, p)
			}
		}
	}
	sort.Slice(extra, func(a, b int) bool { return natLess(extra[a], extra[b]) })
	fixed := len(ps)
	for _, name := range extra {
		if _, ok := match(ps[:fixed], name); ok {
			continue
		}
		dup := false
		for _, p := range ps[fixed:] {
			dup = dup || p.Name == name
		}
		if !dup {
			ps = append(ps, Port{Name: name, Media: media(name, 0), Source: "seen"})
		}
	}
	if ps == nil {
		ps = []Port{}
	}
	st.names[it.ID] = ps
	return ps
}

// port returns the port of a device this relation names (the port on its own side).
func (r relation) port(device int64) (string, bool) {
	switch {
	case r.parent == device && r.parentPort != "":
		return r.parentPort, true
	case r.child == device && r.childPort != "" && r.kind == plugin.RelLLDP:
		return r.childPort, true
	}
	return "", false
}

func status(f ifc) string {
	switch {
	case f.AdminStatus == "down":
		return "disabled"
	case f.OperStatus == "up":
		return "up"
	case f.OperStatus != "":
		return "down"
	}
	return ""
}

// natLess sorts names with numbers naturally (port2 before port10).
func natLess(a, b string) bool {
	na, nb := trailingNum.FindString(a), trailingNum.FindString(b)
	pa, pb := strings.TrimSuffix(a, na), strings.TrimSuffix(b, nb)
	if pa != pb || na == "" || nb == "" {
		return a < b
	}
	x, _ := strconv.Atoi(na)
	y, _ := strconv.Atoi(nb)
	return x < y
}

// canonical returns the name of an item's port.
func (st *state) canonical(it *Item, name string) (string, bool) {
	ps := st.portNames(it)
	if i, ok := match(ps, name); ok {
		return ps[i].Name, true
	}
	return "", false
}

// cablesAt returns the cables at a port.
func (st *state) cablesAt(item int64, port string) []Cable {
	var out []Cable
	for _, c := range st.cables {
		if (c.A.ItemID == item && c.A.Port == port) || (c.B.ItemID == item && c.B.Port == port) {
			out = append(out, c)
		}
	}
	return out
}

func (st *state) itemName(it *Item) string {
	if d := st.devices[it.DeviceID]; d != nil && it.Label == "" {
		return d.Name
	}
	return firstNonEmpty(it.Label, it.DeviceName, kindName(it.Kind))
}

func (st *state) target(item int64, port string) Target {
	t := Target{ItemID: item, Port: port}
	if it := st.byID[item]; it != nil {
		t.ItemName, t.RackID = st.itemName(it), it.RackID
		if r := st.racks[it.RackID]; r != nil {
			t.RackName = r.Name
		}
		if it.Kind == KindDevice {
			t.Device = st.devices[it.DeviceID]
		}
	}
	if pd := st.ports[portKey{item, port}]; pd != nil {
		t.Label = pd.label
		if pd.device > 0 {
			if it := st.byID[item]; it == nil || it.Kind != KindDevice {
				t.Device = st.devices[pd.device]
			}
		}
	}
	return t
}

// trace follows a cable from one of its ends through patch panels to where it ends: a port
// of a device, a patch panel port with a device plugged in, or an open patch panel port.
func (st *state) trace(c Cable, from End) Target {
	seen := map[int64]bool{}
	for range 32 {
		seen[c.ID] = true
		at := c.B
		if c.B == from {
			at = c.A
		}
		it := st.byID[at.ItemID]
		if it == nil || it.Kind == KindDevice {
			return st.target(at.ItemID, at.Port)
		}
		if pd := st.ports[portKey{at.ItemID, at.Port}]; pd != nil && pd.device > 0 {
			return st.target(at.ItemID, at.Port)
		}
		next := false
		for _, o := range st.cablesAt(at.ItemID, at.Port) {
			if !seen[o.ID] {
				c, from, next = o, at, true
				break
			}
		}
		if !next {
			return st.target(at.ItemID, at.Port)
		}
	}
	return st.target(from.ItemID, from.Port)
}

// endpoint returns the device at the other end of a port of an item (plugged in directly
// or reached by cable) with the port on its side ("" for devices outside the racks).
func (st *state) endpoint(item int64, port string) *DeviceRef {
	d, _ := st.peer(item, port)
	return d
}

func (st *state) peer(item int64, port string) (*DeviceRef, string) {
	if pd := st.ports[portKey{item, port}]; pd != nil && pd.device > 0 {
		if cs := st.cablesAt(item, port); len(cs) == 0 || st.byID[item].Kind != KindDevice {
			return st.devices[pd.device], ""
		}
	}
	for _, c := range st.cablesAt(item, port) {
		t := st.trace(c, End{ItemID: item, Port: port})
		if t.Device != nil && !(t.ItemID == item && t.Port == port) {
			if it := st.byID[t.ItemID]; it != nil && it.Kind == KindDevice {
				return t.Device, t.Port
			}
			return t.Device, ""
		}
	}
	return nil, ""
}

// rackLinks maps devices to the rack ports connected to them by hand ("Rack / item port").
func (st *state) rackLinks() map[int64][]string {
	out := map[int64][]string{}
	for _, it := range st.items {
		if it.Kind != KindDevice || it.DeviceID == 0 {
			continue
		}
		for _, p := range st.portNames(it) {
			if d := st.endpoint(it.ID, p.Name); d != nil {
				out[d.ID] = append(out[d.ID], st.itemName(it)+" "+p.Name)
			}
		}
	}
	return out
}

const maxDetected = 8

// item returns an item with its ports and their connections.
func (st *state) item(it *Item) Item {
	out := *it
	out.Device = st.devices[it.DeviceID]
	base := st.portNames(it)
	ps := make([]Port, len(base))
	copy(ps, base)
	var links map[int64][]string
	for i := range ps {
		p := &ps[i]
		p.Cables, p.Detected = []PortCable{}, []Detected{}
		if pd := st.ports[portKey{it.ID, p.Name}]; pd != nil {
			p.Label = pd.label
			if pd.device > 0 {
				p.Device = st.devices[pd.device]
			}
		}
		for _, c := range st.cablesAt(it.ID, p.Name) {
			from := End{ItemID: it.ID, Port: p.Name}
			peer := c.B
			if c.B == from {
				peer = c.A
			}
			pc := PortCable{ID: c.ID, Label: c.Label, Color: c.Color, Peer: st.target(peer.ItemID, peer.Port)}
			if end := st.trace(c, from); end.ItemID != peer.ItemID || end.Port != peer.Port {
				pc.End = &end
			}
			p.Cables = append(p.Cables, pc)
		}
	}
	if it.Kind == KindDevice && it.DeviceID > 0 {
		for _, r := range st.rels {
			name, ok := r.port(it.DeviceID)
			if !ok {
				continue
			}
			j, ok := match(ps, name)
			if !ok {
				continue
			}
			other, remote := r.child, r.childPort
			if r.child == it.DeviceID {
				other, remote = r.parent, r.parentPort
			}
			d := st.devices[other]
			if d == nil {
				continue
			}
			p := &ps[j]
			dup := false
			for _, x := range p.Detected {
				dup = dup || x.Device.ID == other
			}
			if dup {
				continue
			}
			if len(p.Detected) >= maxDetected {
				p.DetectedMore++
				continue
			}
			det := Detected{Device: *d, Kind: r.kind, Source: r.source, RemotePort: remote}
			if links == nil {
				links = st.rackLinks()
			}
			here := st.itemName(it) + " " + p.Name
			if l := links[other]; len(l) > 0 && !contains(l, here) {
				det.Elsewhere = strings.Join(l, ", ")
			}
			p.Detected = append(p.Detected, det)
		}
	}
	out.Ports = ps
	return out
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
