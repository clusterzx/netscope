package ssh

import (
	"context"
	"net/netip"
	"os"
	"path/filepath"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// TestIntegrationLXC scans the development LXC with the dev machine's key.
//
//	NETSCOPE_INTEGRATION=1 go test ./internal/plugins/ssh -run Integration -v
//
// Overrides: NETSCOPE_SSH_HOST (default 192.168.8.123), NETSCOPE_SSH_USER (root),
// NETSCOPE_SSH_KEY (~/.ssh/netscope_lxc).
func TestIntegrationLXC(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("NETSCOPE_INTEGRATION=1 not set")
	}
	host := envOr("NETSCOPE_SSH_HOST", "192.168.8.123")
	user := envOr("NETSCOPE_SSH_USER", "root")
	keyPath := os.Getenv("NETSCOPE_SSH_KEY")
	if keyPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		keyPath = filepath.Join(home, ".ssh", "netscope_lxc")
	}
	key, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"credentials": []any{1}})
	rc.Creds = plugintest.Creds{1: {ID: 1, Name: "lxc-key", Type: plugin.CredSSH,
		Public: map[string]string{"username": user}, Secret: map[string]string{"private_key": string(key)}}}
	rc.Inventory = &plugintest.Inventory{SubnetsList: []plugin.SubnetTarget{{ID: 1, CIDR: netip.MustParsePrefix("192.168.8.0/24")}}}
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 1, Name: "lxc", PrimaryIP: host}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	start := time.Now()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) != 1 {
		t.Fatalf("observations = %d", len(obs))
	}
	o := obs[0]
	inv := o.Inventory.(Inventory)
	t.Logf("scan took %v: host=%q os=%q kernel=%q packages=%d containers=%v ips=%v macs=%v errors=%v",
		time.Since(start), o.Hostname, o.OS.Name, inv.Kernel, len(o.Packages.Packages), inv.Docker, o.IPs, o.MACs, inv.Errors)
	if o.Hostname == "" || o.OS == nil || o.OS.Family != "Linux" || o.OS.Accuracy != 100 {
		t.Errorf("hostname = %q, os = %+v", o.Hostname, o.OS)
	}
	if o.Packages == nil || len(o.Packages.Packages) < 100 {
		t.Fatalf("packages = %+v", o.Packages)
	}
	found := false
	for _, pk := range o.Packages.Packages {
		if pk.Name == "openssh-server" || pk.Name == "openssh" {
			found = true
		}
	}
	if !found {
		t.Error("openssh package not found")
	}
	if o.Containers == nil || o.Containers.Engine != "docker" || o.Containers.Images == nil {
		t.Errorf("containers = %+v", o.Containers)
	}
	for _, c := range o.Containers.Containers {
		if c.ID == "" || c.Name == "" || c.Image == "" || c.State == "" {
			t.Errorf("container = %+v", c)
		}
	}
	if len(o.IPs) == 0 || o.IPs[0] != host || len(o.MACs) != 1 {
		t.Errorf("ips = %v, macs = %v", o.IPs, o.MACs)
	}
	if len(inv.Errors) > 0 {
		t.Errorf("collection errors: %v", inv.Errors)
	}
	if inv.Memory == nil || inv.CPU == nil || inv.UptimeSeconds <= 0 || len(inv.Listening) == 0 || len(inv.Services) == 0 {
		t.Errorf("inventory incomplete: %+v", inv)
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
