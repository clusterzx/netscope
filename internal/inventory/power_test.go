package inventory

import (
	"context"
	"testing"

	"netscope/internal/plugin"
)

func TestPowerState(t *testing.T) {
	ctx := context.Background()
	s, rec := newTestStore(t)
	online := func(id int64) bool {
		t.Helper()
		var on bool
		if err := s.db.R.QueryRow("SELECT online FROM devices WHERE id = ?", id).Scan(&on); err != nil {
			t.Fatal(err)
		}
		return on
	}
	guest := func(p *plugin.PowerState) *plugin.Observation {
		return &plugin.Observation{MACs: []string{"bc:24:11:00:00:01"}, Ref: &plugin.ExternalRef{Source: "proxmox", ID: "pve/qemu/100"},
			Create: true, Hostname: "vm100", Power: p}
	}
	find := func(cs []plugin.Change, typ plugin.ChangeType) *plugin.Change {
		for i := range cs {
			if cs[i].Type == typ {
				return &cs[i]
			}
		}
		return nil
	}

	// a guest that no scanner reaches: running counts as online
	id, err := s.Observe(ctx, "proxmox", 0, guest(&plugin.PowerState{Running: true, Expected: true}))
	if err != nil || id == 0 || !online(id) {
		t.Fatalf("running guest: id=%d err=%v online=%v", id, err, online(id))
	}
	rec.take()

	// stopped: offline at once, the change says why
	if _, err := s.Observe(ctx, "proxmox", 0, guest(&plugin.PowerState{Running: false, Expected: true})); err != nil {
		t.Fatal(err)
	}
	c := find(rec.take(), plugin.ChangeDeviceOffline)
	pc, _ := c.New.(*plugin.PowerChange)
	if online(id) || c == nil || pc == nil || pc.Running || !pc.Expected || pc.Source != "proxmox" {
		t.Fatalf("stopped guest: online=%v change=%+v", online(id), c)
	}
	// started again
	if _, err := s.Observe(ctx, "proxmox", 0, guest(&plugin.PowerState{Running: true})); err != nil {
		t.Fatal(err)
	}
	c = find(rec.take(), plugin.ChangeDeviceOnline)
	if !online(id) || c == nil {
		t.Fatalf("restarted guest: online=%v change=%+v", online(id), c)
	}

	// once a presence scanner tracks the device, it decides reachability
	if _, err := s.Observe(ctx, "arpscan", 0, &plugin.Observation{MACs: []string{"bc:24:11:00:00:01"}, IP: "192.168.8.50", Present: true}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.W.Exec("UPDATE devices SET online = 0 WHERE id = ?", id); err != nil { // missed by the scanner
		t.Fatal(err)
	}
	if _, err := s.Observe(ctx, "proxmox", 0, guest(&plugin.PowerState{Running: true, Expected: true})); err != nil {
		t.Fatal(err)
	}
	if online(id) {
		t.Error("a running report must not override the presence scanner")
	}
	// … but a stopped guest is offline in any case
	if _, err := s.db.W.Exec("UPDATE devices SET online = 1 WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Observe(ctx, "proxmox", 0, guest(&plugin.PowerState{Running: false})); err != nil {
		t.Fatal(err)
	}
	if online(id) {
		t.Error("a stopped guest must go offline")
	}
}

func TestIPSeenOnlyOnAnswers(t *testing.T) {
	ctx := context.Background()
	s, _ := newTestStore(t)
	id, err := s.Observe(ctx, "arpscan", 0, &plugin.Observation{MACs: []string{"aa:00:00:00:00:51"}, IP: "192.168.8.51", Present: true})
	if err != nil {
		t.Fatal(err)
	}
	seen := func() (int64, string) {
		var (
			ts  int64
			src string
		)
		if err := s.db.R.QueryRow("SELECT last_seen, source FROM device_ips WHERE device_id = ? AND ip = ?", id, "192.168.8.51").Scan(&ts, &src); err != nil {
			t.Fatal(err)
		}
		return ts, src
	}
	if _, err := s.db.W.Exec("UPDATE device_ips SET last_seen = 1 WHERE device_id = ?", id); err != nil {
		t.Fatal(err)
	}
	// a ping without reply and a DNS lookup do not count as sighting
	for _, p := range []string{"icmp", "dns"} {
		if _, err := s.Observe(ctx, p, 0, &plugin.Observation{DeviceID: id, IP: "192.168.8.51"}); err != nil {
			t.Fatal(err)
		}
	}
	if ts, src := seen(); ts != 1 || src != "arpscan" {
		t.Fatalf("last_seen=%d source=%s, want unchanged", ts, src)
	}
	if _, err := s.Observe(ctx, "icmp", 0, &plugin.Observation{DeviceID: id, IP: "192.168.8.51", Present: true}); err != nil {
		t.Fatal(err)
	}
	if ts, src := seen(); ts == 1 || src != "icmp" {
		t.Fatalf("answer must refresh last_seen: %d %s", ts, src)
	}
}
