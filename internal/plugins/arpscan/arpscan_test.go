package arpscan

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// TestMain lets the test binary act as a fake arp-scan: when ARPSCAN_FAKE_OUTPUT is set
// it prints that file (and ARPSCAN_FAKE_STDERR to stderr with exit code 1).
func TestMain(m *testing.M) {
	if out := os.Getenv("ARPSCAN_FAKE_OUTPUT"); out != "" {
		if f := os.Getenv("ARPSCAN_FAKE_ARGS"); f != "" {
			_ = os.WriteFile(f, []byte(strings.Join(os.Args[1:], "\n")), 0o600)
		}
		b, err := os.ReadFile(out)
		if err != nil {
			os.Exit(2)
		}
		_, _ = os.Stdout.Write(b)
		if e := os.Getenv("ARPSCAN_FAKE_STDERR"); e != "" {
			msg, _ := os.ReadFile(e)
			_, _ = os.Stderr.Write(msg)
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

// fakeArpScan installs the test binary as "arp-scan" in a temporary PATH.
func fakeArpScan(t *testing.T, fixture, stderrFixture string) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	name := "arp-scan"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	src, err := os.Open(exe)
	if err != nil {
		t.Fatal(err)
	}
	defer src.Close()
	dst, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY, 0o755)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(dst, src); err != nil {
		t.Fatal(err)
	}
	dst.Close()
	abs, _ := filepath.Abs(filepath.Join("testdata", fixture))
	t.Setenv("PATH", dir)
	t.Setenv("ARPSCAN_FAKE_OUTPUT", abs)
	argsFile := filepath.Join(dir, "args.txt")
	t.Setenv("ARPSCAN_FAKE_ARGS", argsFile)
	if stderrFixture != "" {
		e, _ := filepath.Abs(filepath.Join("testdata", stderrFixture))
		t.Setenv("ARPSCAN_FAKE_STDERR", e)
	} else {
		t.Setenv("ARPSCAN_FAKE_STDERR", "")
	}
	return argsFile
}

func TestParseSubnetScan(t *testing.T) {
	data := plugintest.Fixture(t, "arp-scan-192.168.8.0-24.txt")
	var replies []reply
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		r, ok := parseLine(sc.Text())
		if !ok {
			t.Fatalf("line not parsed: %q", sc.Text())
		}
		replies = append(replies, r)
	}
	if len(replies) != 25 {
		t.Fatalf("got %d replies, want 25", len(replies))
	}
	byIP := map[string]reply{}
	for _, r := range replies {
		byIP[r.IP.String()] = r
		if r.Dup != 0 || r.HdrMAC != "" || r.VLAN != 0 {
			t.Errorf("%s: unexpected annotations %+v", r.IP, r)
		}
	}
	cases := []struct{ ip, mac, vendor string }{
		{"192.168.8.1", "94:83:c4:a8:a4:0a", "GL Technologies (Hong Kong) Limited"},
		{"192.168.8.11", "a8:a1:59:77:91:0e", "ASRock Incorporation"},
		{"192.168.8.25", "12:d5:c5:c7:a3:40", ""}, // (Unknown: locally administered)
		{"192.168.8.32", "bc:24:11:13:ce:ce", ""}, // (Unknown)
		{"192.168.8.44", "dc:15:c8:22:c6:b3", "AVM Audiovisuelles Marketing und Computersysteme GmbH"},
	}
	for _, c := range cases {
		r, ok := byIP[c.ip]
		if !ok {
			t.Errorf("%s missing", c.ip)
			continue
		}
		if r.MAC != c.mac || r.Vendor != c.vendor {
			t.Errorf("%s: got mac=%q vendor=%q, want %q %q", c.ip, r.MAC, r.Vendor, c.mac, c.vendor)
		}
	}
}

func TestParseDuplicates(t *testing.T) {
	// Real IP conflict (two containers answering for 172.17.0.2) and a real duplicate
	// response of the same host (GL.iNet router answering twice).
	lines := strings.Split(strings.TrimSpace(string(plugintest.Fixture(t, "arp-scan-dup.txt"))), "\n")
	r, ok := parseLine(lines[2])
	if !ok || r.Dup != 2 || r.IP.String() != "172.17.0.2" || r.MAC != "de:5a:70:0c:33:e4" || r.Vendor != "" {
		t.Fatalf("conflict line: %+v ok=%v", r, ok)
	}
	lines = strings.Split(strings.TrimSpace(string(plugintest.Fixture(t, "arp-scan-devices.txt"))), "\n")
	r, ok = parseLine(lines[2])
	if !ok || r.Dup != 2 || r.Vendor != "GL Technologies (Hong Kong) Limited" || r.MAC != "94:83:c4:a8:a4:0a" {
		t.Fatalf("duplicate line: %+v ok=%v", r, ok)
	}
}

func TestParseAnnotations(t *testing.T) {
	// Format variants produced by arp-scan's display_packet (proxy ARP, VLAN, quiet mode).
	cases := []struct {
		line string
		want reply
	}{
		{"10.0.0.5\t00:11:22:33:44:55 (00:aa:bb:cc:dd:ee)\tCIMSYS Inc (802.1Q VLAN=20) (DUP: 3)\tRTT=1.234 ms",
			reply{MAC: "00:11:22:33:44:55", HdrMAC: "00:aa:bb:cc:dd:ee", Vendor: "CIMSYS Inc", VLAN: 20, Dup: 3}},
		{"10.0.0.6\t00:11:22:33:44:56 (DUP: 2)", reply{MAC: "00:11:22:33:44:56", Dup: 2}},
		{"10.0.0.7\t00:11:22:33:44:57\tFoo (802.2 LLC/SNAP) (ARP Proto=0x0806)", reply{MAC: "00:11:22:33:44:57", Vendor: "Foo"}},
	}
	for _, c := range cases {
		r, ok := parseLine(c.line)
		if !ok {
			t.Fatalf("%q not parsed", c.line)
		}
		if r.MAC != c.want.MAC || r.HdrMAC != c.want.HdrMAC || r.Vendor != c.want.Vendor || r.VLAN != c.want.VLAN || r.Dup != c.want.Dup {
			t.Errorf("%q: got %+v", c.line, r)
		}
	}
	for _, bad := range []string{"", "Interface: eth0, type: EN10MB, MAC: bc:24:11:27:22:b5, IPv4: 192.168.8.123",
		"Starting arp-scan 1.10.0 with 256 hosts (https://github.com/royhills/arp-scan)",
		"2 packets received by filter, 0 packets dropped by kernel", "192.168.8.1\tnot-a-mac\tX"} {
		if _, ok := parseLine(bad); ok {
			t.Errorf("%q must not parse", bad)
		}
	}
}

func TestClassifyPermissionError(t *testing.T) {
	stderr := strings.TrimSpace(string(plugintest.Fixture(t, "arp-scan-noperm.stderr")))
	err := classifyError(errors.New("arp-scan: exit status 1: "+stderr), "eth0")
	if !strings.Contains(err.Error(), "NET_RAW") || !strings.Contains(err.Error(), "keine Raw-Sockets") {
		t.Fatalf("unexpected message: %v", err)
	}
}

func TestBuildJobs(t *testing.T) {
	ifaceFor := func(a netip.Addr) string {
		if netip.MustParsePrefix("192.168.8.0/24").Contains(a) {
			return "eth0"
		}
		return ""
	}
	jobs, errs, skipped := buildJobs(plugin.Targets{Subnets: []plugin.SubnetTarget{
		{CIDR: netip.MustParsePrefix("192.168.8.0/24")},
		{CIDR: netip.MustParsePrefix("10.20.0.0/24"), Interface: "vlan20"},
		{CIDR: netip.MustParsePrefix("172.16.0.0/24")},
		{CIDR: netip.MustParsePrefix("10.0.0.0/8"), Interface: "eth1"},
		{CIDR: netip.MustParsePrefix("192.168.1.0/24"), Routed: true},
	}}, nil, ifaceFor)
	if len(jobs) != 2 || len(errs) != 2 || len(skipped) != 1 || skipped[0].String() != "192.168.1.0/24" {
		t.Fatalf("jobs=%+v errs=%v", jobs, errs)
	}
	if jobs[0].iface != "eth0" || jobs[0].targets[0] != "192.168.8.0/24" || jobs[1].iface != "vlan20" {
		t.Fatalf("jobs=%+v", jobs)
	}

	jobs, errs, skipped = buildJobs(plugin.Targets{DeviceMode: true,
		Subnets: []plugin.SubnetTarget{{CIDR: netip.MustParsePrefix("10.20.0.0/24"), Interface: "vlan20"}},
		Devices: []plugin.DeviceInfo{
			{ID: 1, PrimaryIP: "192.168.8.44", IPs: []string{"192.168.8.44", "10.20.0.5"}},
			{ID: 2, PrimaryIP: "192.168.8.1"},
			{ID: 3, PrimaryIP: "8.8.8.8"},
			{ID: 4, PrimaryIP: "fd00::1"},
			{ID: 5, PrimaryIP: "192.168.1.10"}, // behind a tunnel: skipped without error
		}}, []netip.Prefix{netip.MustParsePrefix("192.168.1.0/24")}, ifaceFor)
	if len(jobs) != 2 || len(errs) != 1 || len(skipped) != 1 {
		t.Fatalf("jobs=%+v errs=%v", jobs, errs)
	}
	if jobs[0].iface != "eth0" || strings.Join(jobs[0].targets, ",") != "192.168.8.1,192.168.8.44" {
		t.Fatalf("eth0 job: %+v", jobs[0])
	}
	if jobs[1].iface != "vlan20" || strings.Join(jobs[1].targets, ",") != "10.20.0.5" {
		t.Fatalf("vlan20 job: %+v", jobs[1])
	}
	if !jobs[0].inScope(netip.MustParseAddr("192.168.8.1")) || jobs[0].inScope(netip.MustParseAddr("192.168.8.2")) {
		t.Fatal("device mode scope")
	}
}

func TestRunStreamsObservations(t *testing.T) {
	argsFile := fakeArpScan(t, "arp-scan-devices.txt", "")
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"retries": 3, "timeout_ms": 250, "bandwidth": "1M",
		"ignore_macs": []any{"DC-15-C8-22-C6-B3"}})
	rc.Targets = plugin.Targets{Subnets: []plugin.SubnetTarget{{CIDR: netip.MustParsePrefix("192.168.8.0/24"), Interface: "nsfake0"}}}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	// 192.168.8.44 is ignored, the duplicate of 192.168.8.1 is dropped.
	if len(obs) != 1 {
		t.Fatalf("got %d observations: %+v", len(obs), obs)
	}
	o := obs[0]
	if o.IP != "192.168.8.1" || !o.Present || len(o.MACs) != 1 || o.MACs[0] != "94:83:c4:a8:a4:0a" ||
		o.Vendor != "GL Technologies (Hong Kong) Limited" {
		t.Fatalf("observation: %+v", o)
	}
	if rc.Stats()["hosts"] != 1 || rc.Stats()["duplicates"] != 1 {
		t.Fatalf("stats: %v", rc.Stats())
	}
	args, _ := os.ReadFile(argsFile)
	want := "--plain\n--numeric\n--interface=nsfake0\n--retry=3\n--timeout=250\n--bandwidth=1M\n192.168.8.0/24"
	if string(args) != want {
		t.Fatalf("args:\n%s\nwant:\n%s", args, want)
	}
}

func TestRunPermissionDenied(t *testing.T) {
	fakeArpScan(t, "empty.txt", "arp-scan-noperm.stderr")
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, nil)
	rc.Targets = plugin.Targets{Subnets: []plugin.SubnetTarget{{CIDR: netip.MustParsePrefix("192.168.8.0/24"), Interface: "nsfake0"}}}
	err := p.Run(context.Background(), rc)
	if err == nil || !strings.Contains(err.Error(), "NET_RAW") {
		t.Fatalf("expected permission error, got %v", err)
	}
	if len(sink.All()) != 0 {
		t.Fatal("no observations expected")
	}
}

func TestLocalHost(t *testing.T) {
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Skip(err)
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback == 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		obs, err := localHost(job{iface: ifc.Name, prefix: netip.MustParsePrefix("127.0.0.0/8")})
		if err != nil {
			t.Fatal(err)
		}
		if obs == nil {
			continue // loopback without IPv4 address
		}
		if obs.IP != "127.0.0.1" || !obs.Present || obs.Attrs["netscope.self"] != "true" || obs.Hostname == "" {
			t.Fatalf("self observation: %+v", obs)
		}
		obs, err = localHost(job{iface: ifc.Name, prefix: netip.MustParsePrefix("192.0.2.0/24")})
		if err != nil || obs != nil {
			t.Fatalf("out of scope: %+v %v", obs, err)
		}
		return
	}
	t.Skip("no loopback interface with IPv4")
}
