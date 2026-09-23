package mdns

import (
	"context"
	"net/netip"
	"os"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// TestIntegrationDiscover runs a real discovery on the LAN (NETSCOPE_INTEGRATION=1).
func TestIntegrationDiscover(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("NETSCOPE_INTEGRATION=1 not set")
	}
	subnet := os.Getenv("NETSCOPE_TEST_SUBNET")
	if subnet == "" {
		subnet = "192.168.8.0/24"
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"listen": "6s"})
	rc.Targets = plugin.Targets{
		Subnets: []plugin.SubnetTarget{{CIDR: netip.MustParsePrefix(subnet), Interface: os.Getenv("NETSCOPE_TEST_IFACE")}},
		Devices: []plugin.DeviceInfo{{ID: 1, PrimaryIP: "192.168.8.1"}, {ID: 2, PrimaryIP: "192.168.8.135"}},
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	named := 0
	for _, o := range sink.All() {
		t.Logf("%-15s present=%v host=%q type=%q model=%q services=%q", o.IP, o.Present, o.Hostname, o.DeviceType, o.Model, o.Attrs["mdns.services"])
		if o.Hostname != "" {
			named++
		}
	}
	t.Logf("stats: %v", rc.Stats())
	if named == 0 {
		t.Fatal("no host names discovered")
	}
}
