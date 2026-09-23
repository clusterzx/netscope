package nmapudp

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/plugins/nmap/nmapxml"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func TestInfoAndSchema(t *testing.T) {
	p := &Plugin{}
	info := p.Info()
	if info.ID != "nmap_udp" || info.Targets != plugin.TargetDevices || info.Presence {
		t.Fatalf("info = %+v", info)
	}
	if err := p.Schema().Check(); err != nil {
		t.Fatalf("schema: %v", err)
	}
	if _, err := p.Schema().Validate(nil, nil, nil); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

func build(t *testing.T, includeOF bool) (*plugin.Observation, int) {
	t.Helper()
	var obs *plugin.Observation
	var n int
	if _, err := nmapxml.Parse(bytes.NewReader(fixture(t, "nmap-udp-192.168.8.1.xml")), func(run *nmapxml.Run, h *nmapxml.Host) error {
		obs, n = buildUDPObservation(run, h, 7, "192.168.8.1", includeOF)
		return nil
	}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return obs, n
}

func TestBuildUDPObservation(t *testing.T) {
	obs, n := build(t, false)
	if obs.DeviceID != 7 || obs.IP != "192.168.8.1" {
		t.Fatalf("identity = %d/%q", obs.DeviceID, obs.IP)
	}
	if obs.Present {
		t.Error("UDP scan should not set presence")
	}
	if obs.Ports == nil || obs.Ports.Protocol != "udp" {
		t.Fatal("no udp port scan")
	}
	if len(obs.Ports.Scanned) == 0 {
		t.Error("scanned ranges empty")
	}
	// Without open|filtered only the three responding open ports remain.
	if n != 3 {
		t.Errorf("open udp ports = %d, want 3", n)
	}
	for _, p := range obs.Ports.Ports {
		if p.State != "open" {
			t.Errorf("unexpected state %q for port %d", p.State, p.Port)
		}
	}
}

func TestBuildUDPObservationIncludeOpenFiltered(t *testing.T) {
	_, n := build(t, true)
	// The 34 no-response ports are aggregated in <extraports>, not per-port, so they are
	// not emitted; only explicitly listed open / open|filtered ports count.
	if n < 3 {
		t.Errorf("open ports with open|filtered = %d, want >= 3", n)
	}
}

func TestRunIntegration(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("set NETSCOPE_INTEGRATION=1 to run against the lab")
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"top_ports": 30, "host_timeout": "120s"})
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 1, PrimaryIP: "192.168.8.1"}}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatalf("run: %v", err)
	}
	obs := sink.All()
	if len(obs) != 1 || obs[0].Ports == nil {
		t.Fatalf("observations = %+v", obs)
	}
	t.Logf("udp ports on 192.168.8.1: %d", len(obs[0].Ports.Ports))
}
