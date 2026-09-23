package nmap

import (
	"bytes"
	"reflect"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugins/nmap/nmapxml"
)

func TestInfoAndSchema(t *testing.T) {
	p := &Plugin{}
	info := p.Info()
	if info.ID != "nmap" || info.Kind != plugin.KindScanner || !info.Presence {
		t.Fatalf("info = %+v", info)
	}
	if err := p.Schema().Check(); err != nil {
		t.Fatalf("schema: %v", err)
	}
	// Defaults must validate.
	if _, err := p.Schema().Validate(nil, nil, nil); err != nil {
		t.Fatalf("defaults invalid: %v", err)
	}
}

func TestPortsToArgs(t *testing.T) {
	cases := []struct {
		in   string
		want []string
		bad  bool
	}{
		{"top-1000", []string{"--top-ports", "1000"}, false},
		{"TOP-50", []string{"--top-ports", "50"}, false},
		{"all", []string{"-p-"}, false},
		{"22,80,443,8000-8100", []string{"-p", "22,80,443,8000-8100"}, false},
		{"22", []string{"-p", "22"}, false},
		{"", nil, true},
		{"top-0", nil, true},
		{"top-99999", nil, true},
		{"22;rm -rf", nil, true},
		{"-sV", nil, true},
		{"80-20", nil, true},
		{"70000", nil, true},
		{"80,,443", nil, true},
	}
	for _, c := range cases {
		got, err := portsToArgs(c.in)
		if c.bad {
			if err == nil {
				t.Errorf("portsToArgs(%q) = %v, want error", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("portsToArgs(%q): %v", c.in, err)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("portsToArgs(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestValidateSettings(t *testing.T) {
	p := &Plugin{}
	ok, _ := p.Schema().Validate(map[string]any{"ports": "top-100"}, nil, nil)
	if err := p.ValidateSettings(plugin.NewSettings(ok)); err != nil {
		t.Errorf("valid settings rejected: %v", err)
	}
	bad, _ := p.Schema().Validate(map[string]any{"ports": "bogus"}, nil, nil)
	if err := p.ValidateSettings(plugin.NewSettings(bad)); err == nil {
		t.Error("expected error for bogus ports")
	}
}

func TestMapDeviceType(t *testing.T) {
	cases := map[string]string{
		"router":           "router",
		"broadband router": "router",
		"WAP":              "access-point",
		"webcam":           "camera",
		"storage-misc":     "nas",
		"media device":     "media-player",
		"general purpose":  "",
		"":                 "",
		"nonsense":         "",
	}
	for in, want := range cases {
		if got := mapDeviceType(in); got != want {
			t.Errorf("mapDeviceType(%q) = %q, want %q", in, got, want)
		}
	}
}

func buildFromFixture(t *testing.T, name string, minOSAcc int) *plugin.Observation {
	t.Helper()
	b := fixture(t, name)
	var obs *plugin.Observation
	if _, err := nmapxml.Parse(bytes.NewReader(b), func(run *nmapxml.Run, h *nmapxml.Host) error {
		obs = buildObservation(run, h, minOSAcc, hostPresent(h))
		return nil
	}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if obs == nil {
		t.Fatal("no observation built")
	}
	return obs
}

func TestBuildObservationRouter(t *testing.T) {
	obs := buildFromFixture(t, "nmap-tcp-192.168.8.1.xml", 85)
	if obs.IP != "192.168.8.1" || !obs.Present {
		t.Fatalf("ip/present = %q/%v", obs.IP, obs.Present)
	}
	if len(obs.MACs) != 1 || obs.Vendor == "" {
		t.Fatalf("macs/vendor = %v/%q", obs.MACs, obs.Vendor)
	}
	if obs.Ports == nil || obs.Ports.Protocol != "tcp" {
		t.Fatal("no tcp port scan")
	}
	if len(obs.Ports.Scanned) == 0 {
		t.Error("scanned ranges empty")
	}
	got := map[int]plugin.Port{}
	for _, p := range obs.Ports.Ports {
		got[p.Port] = p
	}
	if len(got) != 7 {
		t.Errorf("open ports = %d, want 7 (%v)", len(got), keys(got))
	}
	if p := got[443]; p.Tunnel != "ssl" {
		t.Errorf("443 tunnel = %q", p.Tunnel)
	}
	if p := got[80]; p.Product != "nginx" || p.Version != "1.26.1" || len(p.CPEs) == 0 {
		t.Errorf("80 = %+v", p)
	}
	// OS + device type.
	if obs.OS == nil || obs.OS.Accuracy != 100 || obs.OS.Family != "Linux" {
		t.Errorf("os = %+v", obs.OS)
	}
	if len(obs.OS.CPEs) == 0 {
		t.Error("os cpes empty")
	}
	// osclass type is "general purpose" (ignored), but service devicetype WAP → access-point.
	if obs.DeviceType != "access-point" {
		t.Errorf("deviceType = %q, want access-point", obs.DeviceType)
	}
	if obs.Raw == "" {
		t.Error("raw empty")
	}
}

func TestBuildObservationTimeoutNoClose(t *testing.T) {
	obs := buildFromFixture(t, "nmap-tcp-timeout-192.168.8.1.xml", 85)
	if obs.Ports == nil {
		t.Fatal("no port scan")
	}
	if len(obs.Ports.Scanned) != 0 {
		t.Errorf("timed-out host must not report scanned ranges, got %v", obs.Ports.Scanned)
	}
}

func TestBuildObservationMinOSAccuracy(t *testing.T) {
	// The LXC's best match is 97%; a threshold above it drops the OS guess.
	obs := buildFromFixture(t, "nmap-tcp-192.168.8.123.xml", 99)
	if obs.OS != nil {
		t.Errorf("os should be dropped above threshold, got %+v", obs.OS)
	}
	obs = buildFromFixture(t, "nmap-tcp-192.168.8.123.xml", 90)
	if obs.OS == nil || obs.OS.Accuracy < 90 {
		t.Errorf("os = %+v", obs.OS)
	}
}

func keys(m map[int]plugin.Port) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
