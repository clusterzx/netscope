package nmap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// fixture reads a file below testdata/.
func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

// TestRunIntegration runs a real nmap scan against the two allowed lab hosts. It requires
// nmap, root and NETSCOPE_INTEGRATION=1 and is skipped otherwise (so it never runs on
// Windows or in CI without the tooling).
func TestRunIntegration(t *testing.T) {
	if os.Getenv("NETSCOPE_INTEGRATION") != "1" {
		t.Skip("set NETSCOPE_INTEGRATION=1 to run against the lab")
	}
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{
		"ports":             "top-100",
		"service_detection": true,
		"os_detection":      true,
		"discovery":         "known",
		"host_timeout":      "120s",
	})
	rc.Targets = plugin.Targets{
		DeviceMode: true,
		Devices: []plugin.DeviceInfo{
			{ID: 1, PrimaryIP: "192.168.8.1"},
			{ID: 2, PrimaryIP: "192.168.8.123"},
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatalf("run: %v", err)
	}
	obs := sink.All()
	if len(obs) < 2 {
		t.Fatalf("observations = %d, want >= 2", len(obs))
	}
	seen := map[string]bool{}
	for _, o := range obs {
		seen[o.IP] = true
		if o.Ports != nil {
			t.Logf("%s: %d ports, os=%v", o.IP, len(o.Ports.Ports), o.OS)
		}
	}
	if !seen["192.168.8.1"] || !seen["192.168.8.123"] {
		t.Errorf("missing lab hosts: %v", seen)
	}
}
