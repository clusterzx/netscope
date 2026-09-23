package oui

import (
	"context"
	"os"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// TestIntegrationUpdate downloads the real IEEE registries (NETSCOPE_INTEGRATION=1) and
// looks up devices of the homelab with them.
func TestIntegrationUpdate(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("NETSCOPE_INTEGRATION=1 not set")
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	res, err := p.RunAction(ctx, rc, "update", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(res.Message)
	rc.Targets.Devices = []plugin.DeviceInfo{
		{ID: 1, PrimaryMAC: "94:83:c4:a8:a4:0a"},
		{ID: 2, PrimaryMAC: "bc:24:11:27:22:b5"},
	}
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	for _, o := range obs {
		t.Logf("device %d %v → %s", o.DeviceID, o.MACs, o.Vendor)
	}
	if len(obs) != 2 || obs[0].Vendor != "GL Technologies (Hong Kong) Limited" || obs[1].Vendor != "Proxmox Server Solutions GmbH" {
		t.Fatalf("unexpected vendors: %+v", obs)
	}
}
