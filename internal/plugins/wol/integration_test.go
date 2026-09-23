package wol

import (
	"context"
	"net/netip"
	"os"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// TestIntegrationWake sends real magic packets for a locally administered MAC that no
// NIC uses (NETSCOPE_INTEGRATION=1), proving that broadcast sending works.
func TestIntegrationWake(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("NETSCOPE_INTEGRATION=1 not set")
	}
	p := &Plugin{}
	rc, _, _ := plugintest.RunContext(t, p, map[string]any{"repeat": 1})
	rc.Inventory = &plugintest.Inventory{SubnetsList: []plugin.SubnetTarget{{CIDR: netip.MustParsePrefix("192.168.8.0/24")}}}
	rc.Targets.Devices = []plugin.DeviceInfo{{ID: 1, PrimaryIP: "192.168.8.200", PrimaryMAC: "02:4e:53:00:00:01"}}
	res, err := p.RunAction(context.Background(), rc, "wake", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(res.Message)
}
