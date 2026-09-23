package icmp

import (
	"context"
	"os"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// TestIntegrationPing pings real hosts (NETSCOPE_INTEGRATION=1, root/NET_RAW).
func TestIntegrationPing(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("NETSCOPE_INTEGRATION=1 not set")
	}
	online := os.Getenv("NETSCOPE_TEST_GATEWAY")
	if online == "" {
		online = "192.168.8.1"
	}
	offline := os.Getenv("NETSCOPE_TEST_UNUSED_IP")
	if offline == "" {
		offline = "192.168.8.254"
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, nil)
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 1, PrimaryIP: online}, {ID: 2, PrimaryIP: offline}}}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	byID := map[int64]plugin.Observation{}
	for _, o := range sink.All() {
		byID[o.DeviceID] = o
		t.Logf("%s present=%v metrics=%+v", o.IP, o.Present, o.Metrics)
	}
	if o := byID[1]; !o.Present || len(o.Metrics) != 2 || o.Metrics[0].Avg <= 0 {
		t.Errorf("%s: %+v", online, o)
	}
	if o := byID[2]; o.Present || len(o.Metrics) != 1 || o.Metrics[0].Avg != 100 {
		t.Errorf("%s: %+v", offline, o)
	}
}
