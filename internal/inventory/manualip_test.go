package inventory

import (
	"context"
	"testing"

	"netscope/internal/plugin"
)

func TestManualIP(t *testing.T) {
	s, _ := newTestStore(t)
	ctx := context.Background()
	// a Proxmox guest without address (no guest agent)
	vm, err := s.Observe(ctx, "proxmox", 0, &plugin.Observation{Ref: &plugin.ExternalRef{Source: "proxmox", ID: "pve/qemu/101"},
		MACs: []string{"bc:24:11:00:01:01"}, Create: true, Hostname: "mailcow"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddManualIP(ctx, vm, "192.168.8.70"); err != nil {
		t.Fatal(err)
	}
	d, err := s.Get(ctx, vm)
	if err != nil || d.IP != "192.168.8.70" || len(d.IPHistory) != 1 || d.IPHistory[0].Source != SourceManual || d.IPHistory[0].SubnetID == 0 {
		t.Fatalf("manual IP: %+v %v", d, err)
	}
	// it is a scan target of its subnet now
	targets, err := s.ResolveTargets(ctx, plugin.Scope{AllSubnets: true}, plugin.TargetSubnets)
	if err != nil || len(targets.Devices) != 1 || targets.Devices[0].ID != vm {
		t.Fatalf("targets: %+v %v", targets.Devices, err)
	}
	// a scan finding it by MAC confirms the address (source changes, no second device)
	if id, err := s.Observe(ctx, "arpscan", 0, &plugin.Observation{IP: "192.168.8.70", MACs: []string{"bc:24:11:00:01:01"}, Present: true}); err != nil || id != vm {
		t.Fatalf("scan: %d %v", id, err)
	}

	// a device known only by its address (ping through a router) is merged
	shadow, err := s.Observe(ctx, "icmp", 0, &plugin.Observation{IP: "192.168.8.71", Present: true})
	if err != nil {
		t.Fatal(err)
	}
	vm2, err := s.Observe(ctx, "proxmox", 0, &plugin.Observation{Ref: &plugin.ExternalRef{Source: "proxmox", ID: "pve/qemu/102"},
		MACs: []string{"bc:24:11:00:01:02"}, Create: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AddManualIP(ctx, vm2, "192.168.8.71"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Row(ctx, shadow); err == nil {
		t.Fatal("IP-only device not merged")
	}
	if d, _ := s.Get(ctx, vm2); d.IP != "192.168.8.71" {
		t.Fatalf("merged address: %+v", d.IPHistory)
	}

	// an address of another real device is refused
	if err := s.AddManualIP(ctx, vm2, "192.168.8.70"); err == nil {
		t.Fatal("address of another device accepted")
	}
	if err := s.AddManualIP(ctx, vm2, "nonsense"); err == nil {
		t.Fatal("invalid address accepted")
	}

	// only manual addresses can be removed
	if err := s.RemoveManualIP(ctx, vm, "192.168.8.70"); err == nil {
		t.Fatal("scanned address removed")
	}
	if err := s.AddManualIP(ctx, vm2, "192.168.8.72"); err != nil {
		t.Fatal(err)
	}
	if err := s.RemoveManualIP(ctx, vm2, "192.168.8.72"); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.Get(ctx, vm2); len(d.IPs) != 1 {
		t.Fatalf("after remove: %v", d.IPs)
	}
}
