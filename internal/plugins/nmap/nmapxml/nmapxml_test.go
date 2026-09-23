package nmapxml

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func load(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}

func parseAll(t *testing.T, name string) (*Run, []*Host) {
	t.Helper()
	var hosts []*Host
	run, err := Parse(bytes.NewReader(load(t, name)), func(_ *Run, h *Host) error {
		hh := *h
		hosts = append(hosts, &hh)
		return nil
	})
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return run, hosts
}

func TestParseTCPRouter(t *testing.T) {
	run, hosts := parseAll(t, "nmap-tcp-192.168.8.1.xml")
	if len(hosts) != 1 {
		t.Fatalf("hosts = %d, want 1", len(hosts))
	}
	h := hosts[0]
	if !h.Up() {
		t.Fatal("host not up")
	}
	if got := h.IPv4(); got != "192.168.8.1" {
		t.Errorf("ipv4 = %q", got)
	}
	mac, vendor := h.MAC()
	if mac != "94:83:C4:A8:A4:0A" || vendor == "" {
		t.Errorf("mac/vendor = %q/%q", mac, vendor)
	}
	// scaninfo → 1000 tcp services, first range starts at port 1.
	scanned := run.Scanned("tcp")
	if len(scanned) == 0 || scanned[0].From != 1 {
		t.Fatalf("scanned tcp = %+v", scanned)
	}
	// Open ports present with services and CPEs.
	byPort := map[int]Port{}
	for _, p := range h.Ports {
		byPort[p.PortID] = p
	}
	ssh, ok := byPort[22]
	if !ok || ssh.State.State != "open" || ssh.Service == nil || ssh.Service.Name != "ssh" {
		t.Fatalf("port 22 = %+v", ssh)
	}
	if ssh.Service.Product != "Dropbear sshd" {
		t.Errorf("ssh product = %q", ssh.Service.Product)
	}
	if len(ssh.Service.CPEs) == 0 {
		t.Errorf("ssh cpes empty")
	}
	https, ok := byPort[443]
	if !ok || https.Service == nil || https.Service.Tunnel != "ssl" {
		t.Fatalf("port 443 tunnel = %+v", https.Service)
	}
	wap, ok := byPort[8080]
	if !ok || wap.Service == nil || wap.Service.DeviceType != "WAP" {
		t.Fatalf("port 8080 devicetype = %+v", wap.Service)
	}
	// OS match.
	if h.OS == nil || len(h.OS.Matches) == 0 {
		t.Fatal("no os matches")
	}
	best := h.OS.Matches[0]
	if best.Accuracy != 100 || len(best.Classes) == 0 || best.Classes[0].OSFamily != "Linux" {
		t.Errorf("os match = %+v", best)
	}
	if h.Uptime == nil || h.Uptime.Seconds == 0 {
		t.Errorf("uptime = %+v", h.Uptime)
	}
	if !bytes.Contains([]byte(h.Raw), []byte("<port protocol=\"tcp\" portid=\"22\"")) {
		t.Errorf("raw fragment missing port 22: %.80q", h.Raw)
	}
	if bytes.Contains([]byte(h.Raw), []byte("<runstats")) {
		t.Errorf("raw fragment leaked past </host>")
	}
}

func TestParseUDP(t *testing.T) {
	run, hosts := parseAll(t, "nmap-udp-192.168.8.1.xml")
	if len(hosts) != 1 {
		t.Fatalf("hosts = %d", len(hosts))
	}
	scanned := run.Scanned("udp")
	if len(scanned) == 0 {
		t.Fatal("no udp scanned ranges")
	}
	var open []int
	for _, p := range hosts[0].Ports {
		if p.State.State == "open" {
			open = append(open, p.PortID)
		}
	}
	// 53, 67, 123 answered as open.
	want := map[int]bool{53: true, 67: true, 123: true}
	for _, p := range open {
		delete(want, p)
	}
	if len(want) != 0 {
		t.Errorf("missing open udp ports: %v (open=%v)", want, open)
	}
}

func TestParseDiscovery(t *testing.T) {
	_, hosts := parseAll(t, "nmap-sn-192.168.8.0_24.xml")
	if len(hosts) < 10 {
		t.Fatalf("discovery hosts = %d, want many", len(hosts))
	}
	var withMAC int
	for _, h := range hosts {
		if !h.Up() {
			t.Errorf("host %s not up", h.IPv4())
		}
		if mac, _ := h.MAC(); mac != "" {
			withMAC++
		}
	}
	if withMAC == 0 {
		t.Error("no host reported a MAC")
	}
}

func TestParseTimeout(t *testing.T) {
	_, hosts := parseAll(t, "nmap-tcp-timeout-192.168.8.1.xml")
	if len(hosts) != 1 {
		t.Fatalf("hosts = %d", len(hosts))
	}
	if !hosts[0].TimedOut {
		t.Error("expected timedout host")
	}
}

func TestParseError(t *testing.T) {
	run, hosts := parseAll(t, "nmap-error-nse.xml")
	if len(hosts) != 0 {
		t.Fatalf("hosts = %d, want 0", len(hosts))
	}
	if !run.Finished || run.Exit != "error" || run.ErrorMsg == "" {
		t.Errorf("run = %+v", run)
	}
}

func TestParseRanges(t *testing.T) {
	got := ParseRanges("1,3-4,22,8000-8100,bad,70000")
	want := []struct{ from, to int }{{1, 1}, {3, 4}, {22, 22}, {8000, 8100}}
	if len(got) != len(want) {
		t.Fatalf("ranges = %+v", got)
	}
	for i, w := range want {
		if got[i].From != w.from || got[i].To != w.to {
			t.Errorf("range %d = %+v, want %v", i, got[i], w)
		}
	}
}
