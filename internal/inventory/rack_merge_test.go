package inventory

import (
	"context"
	"testing"

	"netscope/internal/plugin"
)

// Merging devices keeps their places in the racks: the target takes the place of the
// source; when both are mounted, the source's place stays as a passive element.
func TestMergeKeepsRackPlaces(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	obs := func(mac, ip string) int64 {
		id, err := s.Observe(ctx, "arpscan", 0, &plugin.Observation{MACs: []string{mac}, IP: ip, Present: true})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b, c, pc := obs("aa:00:00:00:00:01", "192.168.8.1"), obs("aa:00:00:00:00:02", "192.168.8.2"),
		obs("aa:00:00:00:00:03", "192.168.8.3"), obs("aa:00:00:00:00:04", "192.168.8.4")
	w := s.db.W
	w.Exec("INSERT INTO racks(id, name, created_at, updated_at) VALUES (1, 'R', 1, 1)")
	w.Exec("INSERT INTO rack_items(id, rack_id, kind, device_id, device_name, position, created_at, updated_at) VALUES (1, 1, 'device', ?, 'old-b', 1, 1, 1)", b)
	w.Exec("INSERT INTO rack_items(id, rack_id, kind, device_id, device_name, position, created_at, updated_at) VALUES (2, 1, 'device', ?, 'c', 2, 1, 1)", c)
	w.Exec("INSERT INTO rack_items(id, rack_id, kind, device_id, device_name, position, created_at, updated_at) VALUES (3, 1, 'device', ?, 'pc', 3, 1, 1)", a)
	w.Exec("INSERT INTO rack_ports(item_id, port, device_id) VALUES (2, '1', ?)", pc)

	// a is mounted itself (item 3): b's place becomes a passive element
	if err := s.Merge(ctx, a, []int64{b}); err != nil {
		t.Fatal(err)
	}
	var kind, label string
	var dev *int64
	s.db.R.QueryRow("SELECT kind, label, device_id FROM rack_items WHERE id = 1").Scan(&kind, &label, &dev)
	if kind != "other" || label != "old-b" || dev != nil {
		t.Fatalf("source place: %s %q %v", kind, label, dev)
	}
	// c is merged into pc: pc takes c's place, and the port data follows
	if err := s.Merge(ctx, pc, []int64{c}); err != nil {
		t.Fatal(err)
	}
	var d2 int64
	s.db.R.QueryRow("SELECT device_id FROM rack_items WHERE id = 2").Scan(&d2)
	if d2 != pc {
		t.Fatalf("target place: %d", d2)
	}
}
