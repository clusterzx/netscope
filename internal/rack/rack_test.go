package rack

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

func newTest(t *testing.T) (*Service, *db.DB) {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "rack.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return New(d, nil), d
}

func device(t *testing.T, d *db.DB, name, typ string) int64 {
	t.Helper()
	res, err := d.W.Exec("INSERT INTO devices(display_name, type, online, created_at, updated_at) VALUES (?,?,1,1,1)", name, typ)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func mustRack(t *testing.T, s *Service, name string, height int) *Rack {
	t.Helper()
	r, err := s.Create(context.Background(), RackInput{Name: name, Height: height})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func mustItem(t *testing.T, s *Service, rack int64, in ItemInput) int64 {
	t.Helper()
	id, err := s.AddItem(context.Background(), rack, in)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func fieldOf(err error) string {
	var ve *plugin.ValidationError
	if errors.As(err, &ve) && len(ve.Errors) > 0 {
		return ve.Errors[0].Field
	}
	return ""
}

type rel struct {
	parent, child         int64
	parentPort, childPort string
}

func rackRelations(t *testing.T, d *db.DB) []rel {
	t.Helper()
	rows, err := d.R.Query("SELECT parent_id, child_id, parent_port, child_port FROM relations WHERE source = 'rack' AND kind = 'switch_port' AND protected = 1 ORDER BY parent_id, child_id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []rel
	for rows.Next() {
		var r rel
		if err := rows.Scan(&r.parent, &r.child, &r.parentPort, &r.childPort); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

func portOf(t *testing.T, v *View, item int64, name string) Port {
	t.Helper()
	for _, it := range v.Items {
		if it.ID != item {
			continue
		}
		for _, p := range it.Ports {
			if p.Name == name {
				return p
			}
		}
		var names []string
		for _, p := range it.Ports {
			names = append(names, p.Name)
		}
		t.Fatalf("port %q not in %v", name, names)
	}
	t.Fatalf("item %d not in rack", item)
	return Port{}
}

func TestRackValidation(t *testing.T) {
	s, _ := newTest(t)
	ctx := context.Background()
	if _, err := s.Create(ctx, RackInput{Name: " ", Width: "21", Height: 99, Numbering: "x"}); err == nil {
		t.Fatal("invalid rack accepted")
	} else if ve := (*plugin.ValidationError)(nil); !errors.As(err, &ve) || len(ve.Errors) != 4 {
		t.Fatalf("want 4 field errors, got %v", err)
	}
	r := mustRack(t, s, "Keller", 0)
	if r.Height != 42 || r.Width != "19" || r.Numbering != "bottom" {
		t.Fatalf("defaults: %+v", r)
	}
	mustItem(t, s, r.ID, ItemInput{Kind: KindShelf, Position: 20, Height: 2})
	if _, err := s.Update(ctx, r.ID, RackInput{Name: "Keller", Height: 20}); fieldOf(err) != "height" {
		t.Fatalf("shrinking below items: %v", err)
	}
	if _, err := s.Update(ctx, r.ID, RackInput{Name: "Keller", Height: 21, Width: "10", Numbering: "top"}); err != nil {
		t.Fatal(err)
	}
	list, err := s.List(ctx)
	if err != nil || len(list) != 1 || list[0].Items != 1 || list[0].UsedUnits != 2 {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestPlacement(t *testing.T) {
	s, d := newTest(t)
	ctx := context.Background()
	r := mustRack(t, s, "R1", 10)
	sw := device(t, d, "core-sw", "switch")

	mustItem(t, s, r.ID, ItemInput{Kind: KindDevice, DeviceID: sw, Position: 10})
	cases := []struct {
		name  string
		in    ItemInput
		field string
	}{
		{"above the rack", ItemInput{Kind: KindBlank, Position: 10, Height: 2}, "position"},
		{"occupied", ItemInput{Kind: KindBlank, Position: 10}, "position"},
		{"device twice", ItemInput{Kind: KindDevice, DeviceID: sw, Position: 1}, "deviceId"},
		{"device missing", ItemInput{Kind: KindDevice, Position: 1}, "deviceId"},
		{"unknown device", ItemInput{Kind: KindDevice, DeviceID: 999, Position: 1}, "deviceId"},
		{"bad width", ItemInput{Kind: KindBlank, Position: 1, Cols: 4}, "cols"},
		{"half in the middle", ItemInput{Kind: KindBlank, Position: 1, Cols: 3, Col: 2}, "col"},
		{"unknown kind", ItemInput{Kind: "rocket", Position: 1}, "kind"},
		{"too many ports", ItemInput{Kind: KindPatchPanel, Position: 1, PortCount: 500}, "portCount"},
	}
	for _, c := range cases {
		if _, err := s.AddItem(ctx, r.ID, c.in); fieldOf(err) != c.field {
			t.Errorf("%s: want error on %s, got %v", c.name, c.field, err)
		}
	}

	// the rear face is free, unless an item is full depth
	rear := mustItem(t, s, r.ID, ItemInput{Kind: KindPDU, Position: 10, Face: "rear"})
	if _, err := s.AddItem(ctx, r.ID, ItemInput{Kind: KindBlank, Position: 9, Height: 2, FullDepth: true}); fieldOf(err) != "position" {
		t.Fatalf("full depth over a rear item: %v", err)
	}

	// half and third widths share a unit
	mustItem(t, s, r.ID, ItemInput{Kind: KindOther, Label: "Pi A", Position: 5, Cols: 3, Col: 0})
	mustItem(t, s, r.ID, ItemInput{Kind: KindOther, Label: "Pi B", Position: 5, Cols: 3, Col: 3})
	if _, err := s.AddItem(ctx, r.ID, ItemInput{Kind: KindOther, Position: 5, Cols: 2, Col: 2}); fieldOf(err) != "position" {
		t.Fatalf("third over two halves: %v", err)
	}
	mustItem(t, s, r.ID, ItemInput{Kind: KindOther, Position: 4, Cols: 2, Col: 0})
	mustItem(t, s, r.ID, ItemInput{Kind: KindOther, Position: 4, Cols: 2, Col: 2})
	mustItem(t, s, r.ID, ItemInput{Kind: KindOther, Position: 4, Cols: 2, Col: 4})

	// moving an item: it does not collide with itself, but with others
	if err := s.UpdateItem(ctx, rear, ItemInput{Kind: KindPDU, Position: 9, Height: 2, Face: "rear"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateItem(ctx, rear, ItemInput{Kind: KindPDU, Position: 5, Face: "front"}); fieldOf(err) != "position" {
		t.Fatalf("move onto others: %v", err)
	}
	// into another rack
	r2 := mustRack(t, s, "R2", 4)
	if err := s.UpdateItem(ctx, rear, ItemInput{RackID: r2.ID, Kind: KindPDU, Position: 1}); err != nil {
		t.Fatal(err)
	}
	v, err := s.View(ctx, r2.ID)
	if err != nil || len(v.Items) != 1 || v.Items[0].ID != rear {
		t.Fatalf("moved item: %+v %v", v, err)
	}
}

// A switch with SNMP interfaces: ports come from the inventory, connections other sources
// see appear as detected, a device set by hand becomes a protected relation.
func TestSwitchPortsAndRelations(t *testing.T) {
	s, d := newTest(t)
	ctx := context.Background()
	r := mustRack(t, s, "R1", 12)
	sw := device(t, d, "core-sw", "switch")
	nas := device(t, d, "nas", "nas")
	pc := device(t, d, "pc", "desktop")
	ap := device(t, d, "ap", "access-point")
	if _, err := d.W.Exec(`INSERT INTO device_inventory(device_id, source, data, collected_at) VALUES (?, 'snmp', ?, 1)`, sw,
		`{"interfaces":[{"index":1,"name":"lo","type":24},{"index":3,"name":"Port 2","type":6,"operStatus":"down"},
		{"index":2,"name":"Port 1","type":6,"operStatus":"up","speedMbps":1000},{"index":4,"name":"SFP+ 1","type":6,"speedMbps":10000},
		{"index":5,"name":"br0","type":6},{"index":6,"name":"vlan10","type":135}]}`); err != nil {
		t.Fatal(err)
	}
	// UniFi names the port "2", the topology "Port 1"
	for _, q := range []struct {
		parent, child int64
		source, port  string
	}{{sw, nas, "topology", "Port 1"}, {sw, pc, "unifi", "2"}, {sw, ap, "unifi", "2"}} {
		if _, err := d.W.Exec(`INSERT INTO relations(parent_id, child_id, kind, source, parent_port, first_seen, last_seen) VALUES (?,?,'switch_port',?,?,1,1)`,
			q.parent, q.child, q.source, q.port); err != nil {
			t.Fatal(err)
		}
	}
	item := mustItem(t, s, r.ID, ItemInput{Kind: KindDevice, DeviceID: sw, Position: 12})
	v, err := s.View(ctx, r.ID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range v.Items[0].Ports {
		names = append(names, p.Name+"/"+p.Source+"/"+p.Media)
	}
	if got := strings.Join(names, " "); got != "Port 1/snmp/copper Port 2/snmp/copper SFP+ 1/snmp/sfp" {
		t.Fatalf("ports: %s", got)
	}
	p1 := portOf(t, v, item, "Port 1")
	if p1.Status != "up" || p1.SpeedMbps != 1000 || len(p1.Detected) != 1 || p1.Detected[0].Device.ID != nas {
		t.Fatalf("port 1: %+v", p1)
	}
	if p2 := portOf(t, v, item, "Port 2"); len(p2.Detected) != 2 {
		t.Fatalf("port 2: %+v", p2)
	}

	// adopting takes ports with exactly one detected device
	n, err := s.Adopt(ctx, item)
	if err != nil || n != 1 {
		t.Fatalf("adopt: %d %v", n, err)
	}
	// set by hand: the pc on port 2
	if err := s.SetPort(ctx, item, PortInput{Port: "port 2", DeviceID: pc, Label: "Büro"}); err != nil {
		t.Fatal(err)
	}
	got := rackRelations(t, d)
	want := []rel{{sw, nas, "Port 1", ""}, {sw, pc, "Port 2", ""}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("relations: %+v", got)
	}
	// the pc seen elsewhere is flagged
	if err := s.SetPort(ctx, item, PortInput{Port: "Port 2"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPort(ctx, item, PortInput{Port: "SFP+ 1", DeviceID: pc}); err != nil {
		t.Fatal(err)
	}
	v, _ = s.View(ctx, r.ID)
	for _, det := range portOf(t, v, item, "Port 2").Detected {
		if det.Device.ID == pc && det.Elsewhere != "core-sw SFP+ 1" {
			t.Fatalf("elsewhere: %+v", det)
		}
	}
	if err := s.SetPort(ctx, item, PortInput{Port: "Port 9", DeviceID: pc}); fieldOf(err) != "port" {
		t.Fatalf("unknown port: %v", err)
	}
	if err := s.SetPort(ctx, item, PortInput{Port: "Port 1", DeviceID: sw}); fieldOf(err) != "deviceId" {
		t.Fatalf("own port: %v", err)
	}

	// removing the switch removes its relations; the relations of other sources stay
	if _, err := s.DeleteItem(ctx, item); err != nil {
		t.Fatal(err)
	}
	if got := rackRelations(t, d); len(got) != 0 {
		t.Fatalf("relations after removal: %+v", got)
	}
	var others int
	d.R.QueryRow("SELECT COUNT(*) FROM relations WHERE source <> 'rack'").Scan(&others)
	if others != 3 {
		t.Fatalf("other relations: %d", others)
	}
}

// switch port → patch panel (front) → patch panel (back, other rack) → device on the socket
func TestCablesThroughPatchPanels(t *testing.T) {
	s, d := newTest(t)
	ctx := context.Background()
	r1 := mustRack(t, s, "Keller", 12)
	r2 := mustRack(t, s, "Büro", 6)
	sw := device(t, d, "sw", "switch")
	router := device(t, d, "router", "router")
	pc := device(t, d, "pc", "desktop")
	swItem := mustItem(t, s, r1.ID, ItemInput{Kind: KindDevice, DeviceID: sw, Position: 10, PortCount: 8})
	rtItem := mustItem(t, s, r1.ID, ItemInput{Kind: KindDevice, DeviceID: router, Position: 12, PortCount: 4, PortPrefix: "eth"})
	pp1 := mustItem(t, s, r1.ID, ItemInput{Kind: KindPatchPanel, Label: "PP Keller", Position: 11, PortCount: 24})
	pp2 := mustItem(t, s, r2.ID, ItemInput{Kind: KindPatchPanel, Label: "PP Büro", Position: 6, PortCount: 12})

	cable := func(a, b End) int64 {
		t.Helper()
		id, err := s.AddCable(ctx, CableInput{A: a, B: b, Color: "blue"})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	c1 := cable(End{swItem, "3"}, End{pp1, "5"})
	cable(End{pp1, "5"}, End{pp2, "7"})
	if err := s.SetPort(ctx, pp2, PortInput{Port: "7", DeviceID: pc, Label: "Dose 1.07"}); err != nil {
		t.Fatal(err)
	}
	// router uplink directly to the switch: the router is the parent
	cable(End{swItem, "1"}, End{rtItem, "eth2"})

	got := rackRelations(t, d)
	want := []rel{{sw, pc, "3", ""}, {router, sw, "eth2", "1"}}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("relations: %+v", got)
	}

	v, _ := s.View(ctx, r1.ID)
	p3 := portOf(t, v, swItem, "3")
	if len(p3.Cables) != 1 || p3.Cables[0].Peer.ItemName != "PP Keller" || p3.Cables[0].End == nil ||
		p3.Cables[0].End.RackName != "Büro" || p3.Cables[0].End.Label != "Dose 1.07" || p3.Cables[0].End.Device.ID != pc {
		t.Fatalf("path: %+v", p3.Cables)
	}

	// limits: a device port takes one cable, a panel port two – one when a device is plugged in
	if _, err := s.AddCable(ctx, CableInput{A: End{swItem, "3"}, B: End{pp1, "6"}}); fieldOf(err) != "a" {
		t.Fatalf("second cable at a device port: %v", err)
	}
	if _, err := s.AddCable(ctx, CableInput{A: End{swItem, "4"}, B: End{pp1, "5"}}); fieldOf(err) != "b" {
		t.Fatalf("third cable at a panel port: %v", err)
	}
	if _, err := s.AddCable(ctx, CableInput{A: End{swItem, "4"}, B: End{pp2, "7"}}); fieldOf(err) != "b" {
		t.Fatalf("second cable at a panel port with a device: %v", err)
	}
	if err := s.SetPort(ctx, swItem, PortInput{Port: "3", DeviceID: pc}); fieldOf(err) != "deviceId" {
		t.Fatalf("device on a cabled switch port: %v", err)
	}
	if _, err := s.AddCable(ctx, CableInput{A: End{swItem, "4"}, B: End{swItem, "4"}}); err == nil {
		t.Fatal("loop accepted")
	}
	if _, err := s.AddCable(ctx, CableInput{A: End{swItem, "4"}, B: End{pp1, "99"}}); fieldOf(err) != "b" {
		t.Fatalf("unknown port: %v", err)
	}
	if _, err := s.AddCable(ctx, CableInput{A: End{swItem, "4"}, B: End{pp1, "8"}, Color: "plaid"}); fieldOf(err) != "color" {
		t.Fatalf("unknown color: %v", err)
	}

	info, err := s.Device(ctx, pc)
	if err != nil || info.Mount != nil || len(info.Links) != 1 || info.Links[0].Port != "3" || info.Links[0].RackName != "Keller" {
		t.Fatalf("device info: %+v %v", info, err)
	}
	info, _ = s.Device(ctx, sw)
	if info.Mount == nil || info.Mount.Unit != 10 {
		t.Fatalf("mount: %+v", info.Mount)
	}

	// pulling the first cable ends the connection to the pc
	if _, err := s.DeleteCable(ctx, c1); err != nil {
		t.Fatal(err)
	}
	if got := rackRelations(t, d); len(got) != 1 || got[0].child != sw {
		t.Fatalf("after unplugging: %+v", got)
	}
	// deleting the rack with the panel on the other side leaves the rest
	if _, err := s.Delete(ctx, r2.ID); err != nil {
		t.Fatal(err)
	}
	var cables int
	d.R.QueryRow("SELECT COUNT(*) FROM rack_cables").Scan(&cables)
	if cables != 1 {
		t.Fatalf("cables: %d", cables)
	}
}

// A device deleted from the inventory leaves its place, with its name.
func TestDeletedDeviceKeepsPlace(t *testing.T) {
	s, d := newTest(t)
	ctx := context.Background()
	r := mustRack(t, s, "R", 4)
	srv := device(t, d, "srv-01", "server")
	mustItem(t, s, r.ID, ItemInput{Kind: KindDevice, DeviceID: srv, Position: 1, Height: 2})
	if _, err := d.W.Exec("DELETE FROM devices WHERE id = ?", srv); err != nil {
		t.Fatal(err)
	}
	v, err := s.View(ctx, r.ID)
	if err != nil || len(v.Items) != 1 || v.Items[0].Device != nil || v.Items[0].DeviceName != "srv-01" {
		t.Fatalf("view: %+v %v", v, err)
	}
	info, _ := s.Device(ctx, srv)
	if info.Mount != nil {
		t.Fatal("deleted device still mounted")
	}
}

func TestMatch(t *testing.T) {
	ps := []Port{{Name: "ether1"}, {Name: "ether2"}, {Name: "sfp-sfpplus1"}, {Name: "1/0/5"}, {Name: "1/1/5"}}
	for _, c := range []struct {
		in   string
		want int
		ok   bool
	}{{"ether2", 1, true}, {"ETHER1", 0, true}, {"2", 1, true}, {"5", 0, false}, {"1/1/5", 4, true}, {"", 0, false}, {"x", 0, false}} {
		got, ok := match(ps, c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("match(%q) = %d %v", c.in, got, ok)
		}
	}
	if !natLess("port2", "port10") || natLess("port10", "port2") {
		t.Error("natural order")
	}
}
