package netbios

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// TestIntegrationNBSTAT queries real hosts (NETSCOPE_INTEGRATION=1). NETSCOPE_TEST_NETBIOS
// lists IPs (comma separated) of which at least one must answer; default: the Windows PC
// of the homelab.
func TestIntegrationNBSTAT(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("NETSCOPE_INTEGRATION=1 not set")
	}
	ips := os.Getenv("NETSCOPE_TEST_NETBIOS")
	if ips == "" {
		ips = "192.168.8.11,192.168.8.1,192.168.8.123"
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, nil)
	for i, ip := range strings.Split(ips, ",") {
		rc.Targets.Devices = append(rc.Targets.Devices, plugin.DeviceInfo{ID: int64(i + 1), PrimaryIP: strings.TrimSpace(ip)})
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	for _, o := range sink.All() {
		t.Logf("%s hostname=%q attrs=%v\n%s", o.IP, o.Hostname, o.Attrs, o.Raw)
	}
	if len(sink.All()) == 0 {
		t.Fatal("no NetBIOS answer")
	}
}
