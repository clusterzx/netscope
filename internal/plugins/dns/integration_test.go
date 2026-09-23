package dns

import (
	"context"
	"os"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// TestIntegrationReverse queries the real resolver (NETSCOPE_INTEGRATION=1).
func TestIntegrationReverse(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("NETSCOPE_INTEGRATION=1 not set")
	}
	resolver := os.Getenv("NETSCOPE_TEST_RESOLVER")
	if resolver == "" {
		resolver = "192.168.8.1"
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"resolver": resolver})
	rc.Targets.Devices = []plugin.DeviceInfo{{ID: 1, PrimaryIP: resolver}, {ID: 2, PrimaryIP: "192.168.8.123"}, {ID: 3, PrimaryIP: "192.168.8.254"}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	names := 0
	for _, o := range sink.All() {
		t.Logf("device %d %s hostname=%q clear=%v", o.DeviceID, o.IP, o.Hostname, o.Clear)
		if o.Hostname != "" {
			names++
		}
	}
	if names == 0 {
		t.Fatal("no PTR record found")
	}
}
