package arpscan

import (
	"context"
	"net/netip"
	"os"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// TestIntegrationScan runs a real arp-scan (NETSCOPE_INTEGRATION=1, root/NET_RAW).
// NETSCOPE_TEST_SUBNET, NETSCOPE_TEST_IFACE, NETSCOPE_TEST_EXPECT (comma separated)
// override the defaults of the homelab.
func TestIntegrationScan(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("NETSCOPE_INTEGRATION=1 not set")
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, nil)
	rc.Targets = plugin.Targets{Subnets: []plugin.SubnetTarget{{
		CIDR:      netip.MustParsePrefix(envOr("NETSCOPE_TEST_SUBNET", "192.168.8.0/24")),
		Interface: os.Getenv("NETSCOPE_TEST_IFACE"),
	}}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	start := time.Now()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	found := map[string]plugin.Observation{}
	for _, o := range sink.All() {
		found[o.IP] = o
		t.Logf("%-15s %v %-45s host=%q attrs=%v", o.IP, o.MACs, o.Vendor, o.Hostname, o.Attrs)
	}
	t.Logf("%d hosts in %s", len(found), time.Since(start).Round(time.Millisecond))
	for _, ip := range []string{envOr("NETSCOPE_TEST_GATEWAY", "192.168.8.1"), envOr("NETSCOPE_TEST_SELF", "192.168.8.123")} {
		if _, ok := found[ip]; !ok {
			t.Errorf("%s not found", ip)
		}
	}
	if self := found[envOr("NETSCOPE_TEST_SELF", "192.168.8.123")]; self.Attrs["netscope.self"] != "true" || len(self.MACs) != 1 {
		t.Errorf("self observation incomplete: %+v", self)
	}
}
