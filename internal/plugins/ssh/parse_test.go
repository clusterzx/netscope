package ssh

import (
	"net/netip"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// Fixtures in testdata/ are real captures:
//
//   - <host>.out / <host>.err: stdout/stderr of the inventory script (buildScript with
//     packages and docker enabled), captured with
//     `ssh host 'sh -s' < script > x.out 2> x.err` on the Ubuntu 24.04 LXC (with two
//     throwaway docker containers) and with `docker run --rm -i <image> sh -s` in
//     alpine:3.22, busybox:1.37 and rockylinux:9 containers.
//   - ubuntu2404-ip-addr.txt, ubuntu2404-ss-header.txt, ubuntu2404-lsblk-default.json:
//     the fallback variants (`ip addr`, `ss -tulpn`, `lsblk -J -b`) on the same LXC.
//   - alpine322-apk-info.txt (`apk info -v`), alpine322-netstat.txt (busybox
//     `netstat -tulpn` with nc listeners).
//
// Run `NETSCOPE_SSH_SCRIPT_OUT=/tmp/script.sh go test -run TestWriteScript` to get the
// script for new captures.

func TestWriteScript(t *testing.T) {
	path := os.Getenv("NETSCOPE_SSH_SCRIPT_OUT")
	if path == "" {
		t.Skip("NETSCOPE_SSH_SCRIPT_OUT not set")
	}
	s := buildScript(scriptOptions{Packages: true, Docker: true, CommandTimeout: 20 * time.Second})
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func fixtureSections(t *testing.T, host string) map[string]*sectionOutput {
	t.Helper()
	return splitOutput(plugintest.Fixture(t, host+".out"), plugintest.Fixture(t, host+".err"))
}

func sec(t *testing.T, secs map[string]*sectionOutput, name string) *sectionOutput {
	t.Helper()
	s, ok := secs[name]
	if !ok {
		t.Fatalf("section %s missing", name)
	}
	return s
}

func TestSplitOutput(t *testing.T) {
	secs := fixtureSections(t, "ubuntu2404")
	if len(secs) != len(commands) {
		t.Fatalf("sections = %d, want %d", len(secs), len(commands))
	}
	for name, s := range secs {
		if !s.Done {
			t.Errorf("%s not done", name)
		}
	}
	if s := sec(t, secs, secIPAddr); s.Variant != "ip" || !s.OK() {
		t.Errorf("ip_addr = %+v", s)
	}
	if s := sec(t, secs, secSockets); s.Variant != "ss" || !s.OK() {
		t.Errorf("sockets = %+v", s)
	}
	if s := sec(t, secs, secPkgRPM); !s.Missing || s.OK() {
		t.Errorf("pkg_rpm should be missing: %+v", s)
	}
	if got := sec(t, secs, secHostname).Out; got != "netscope" {
		t.Errorf("hostname = %q", got)
	}

	// busybox: stderr lines are attributed to their sections
	bb := fixtureSections(t, "busybox137")
	if s := sec(t, bb, secOSRelease); s.RC != 1 || !strings.Contains(s.Err, "/usr/lib/os-release") {
		t.Errorf("os_release = %+v", s)
	}
	if s := sec(t, bb, secPkgRPM); s.RC != 1 || !strings.HasPrefix(s.Err, "rpm: invalid option") {
		t.Errorf("pkg_rpm = %+v", s)
	}
	if s := sec(t, bb, secSockets); s.Variant != "netstat" {
		t.Errorf("sockets variant = %q", s.Variant)
	}
	// rocky: docker pull messages before the first marker are ignored
	rk := fixtureSections(t, "rocky9")
	if s := sec(t, rk, secDate); s.Err != "" {
		t.Errorf("date stderr = %q", s.Err)
	}
}

func TestSplitOutputTruncated(t *testing.T) {
	out := plugintest.Fixture(t, "ubuntu2404.out")
	i := strings.Index(string(out), "openssh-server\t")
	secs := splitOutput(out[:i], nil)
	s := sec(t, secs, secPkgDpkg)
	if s.Done || s.OK() {
		t.Fatalf("cut section must not be done: %+v", s)
	}
	if _, ok := secs[secServices]; ok {
		t.Fatal("sections after the cut must not exist")
	}
}

func TestParseDate(t *testing.T) {
	tm, loc, err := parseDate(sec(t, fixtureSections(t, "ubuntu2404"), secDate).Out)
	if err != nil {
		t.Fatal(err)
	}
	if tm.Unix() != 1790108554 {
		t.Errorf("time = %d", tm.Unix())
	}
	if _, off := time.Now().In(loc).Zone(); off != 0 {
		t.Errorf("offset = %d", off)
	}
	_, loc, err = parseDate("1790108554 +0200")
	if err != nil {
		t.Fatal(err)
	}
	if _, off := time.Now().In(loc).Zone(); off != 7200 {
		t.Errorf("offset = %d", off)
	}
	if _, _, err := parseDate(""); err == nil {
		t.Error("empty input must fail")
	}
}

func TestParseOSRelease(t *testing.T) {
	cases := []struct {
		host                     string
		pretty, id, version, cpe string
	}{
		{"ubuntu2404", "Ubuntu 24.04 LTS", "ubuntu", "24.04", ""},
		{"alpine322", "Alpine Linux v3.22", "alpine", "3.22.6", ""},
		{"rocky9", "Rocky Linux 9.3 (Blue Onyx)", "rocky", "9.3", "cpe:/o:rocky:rocky:9::baseos"},
	}
	for _, c := range cases {
		rel := parseOSRelease(sec(t, fixtureSections(t, c.host), secOSRelease).Out)
		if rel["PRETTY_NAME"] != c.pretty || rel["ID"] != c.id || rel["VERSION_ID"] != c.version || rel["CPE_NAME"] != c.cpe {
			t.Errorf("%s: %v", c.host, rel)
		}
		o := osInfo(rel)
		if o == nil || o.Name != c.pretty || o.Family != "Linux" || o.Vendor != c.id || o.Generation != c.version || o.Accuracy != 100 {
			t.Errorf("%s: os = %+v", c.host, o)
		}
		if (c.cpe != "") != (len(o.CPEs) == 1) {
			t.Errorf("%s: cpes = %v", c.host, o.CPEs)
		}
	}
	if got := unquoteShell(`"a \"quoted\" \$x"`); got != `a "quoted" $x` {
		t.Errorf("unquote = %q", got)
	}
	if osInfo(nil) != nil {
		t.Error("nil os-release must give nil")
	}
}

func TestParseCPUInfo(t *testing.T) {
	c := parseCPUInfo(sec(t, fixtureSections(t, "ubuntu2404"), secCPUInfo).Out)
	want := CPUInfo{Model: "Intel(R) Xeon(R) CPU E5-2697A v4 @ 2.60GHz", Vendor: "GenuineIntel", Sockets: 1, Cores: 16, Threads: 4}
	if c != want {
		t.Errorf("ubuntu (lxcfs) = %+v, want %+v", c, want)
	}
	c = parseCPUInfo(sec(t, fixtureSections(t, "alpine322"), secCPUInfo).Out)
	if c.Threads != 32 || c.Model != want.Model {
		t.Errorf("alpine = %+v", c)
	}
	arm := "processor\t: 0\nBogoMIPS\t: 108.00\nFeatures\t: fp asimd\nCPU implementer\t: 0x41\n\nHardware\t: BCM2835\nModel\t\t: Raspberry Pi 4 Model B Rev 1.4\n"
	if c := parseCPUInfo(arm); c.Threads != 1 || c.Hardware != "BCM2835" {
		t.Errorf("arm = %+v", c)
	}
}

func TestParseMeminfo(t *testing.T) {
	m, err := parseMeminfo(sec(t, fixtureSections(t, "ubuntu2404"), secMeminfo).Out)
	if err != nil {
		t.Fatal(err)
	}
	want := MemInfo{TotalBytes: 2097152 * 1024, AvailableBytes: 1923125 * 1024, SwapTotalBytes: 524288 * 1024, SwapFreeBytes: 524280 * 1024}
	if m != want {
		t.Errorf("got %+v, want %+v", m, want)
	}
	if _, err := parseMeminfo("garbage"); err == nil {
		t.Error("missing MemTotal must fail")
	}
}

func TestParseUptime(t *testing.T) {
	up, err := parseUptime(sec(t, fixtureSections(t, "ubuntu2404"), secUptime).Out)
	if err != nil || up != 4303.37 {
		t.Errorf("uptime = %v, %v", up, err)
	}
	if _, err := parseUptime("x"); err == nil {
		t.Error("garbage must fail")
	}
}

func TestParseDF(t *testing.T) {
	fs := parseDF(sec(t, fixtureSections(t, "ubuntu2404"), secDF).Out)
	want := []Filesystem{{Device: "/dev/mapper/pve-vm--105--disk--0", Type: "ext4", MountPoint: "/",
		SizeBytes: 15375304 * 1024, UsedBytes: 1893172 * 1024, AvailBytes: 12679316 * 1024, UsePercent: 13}}
	if !reflect.DeepEqual(fs, want) {
		t.Errorf("ubuntu = %+v", fs)
	}
	// busybox df inside a container: overlay/tmpfs/devtmpfs skipped, bind mounts once
	fs = parseDF(sec(t, fixtureSections(t, "alpine322"), secDF).Out)
	if len(fs) != 1 || fs[0].Type != "ext4" || fs[0].MountPoint != "/etc/hostname" || fs[0].UsePercent != 9 {
		t.Errorf("alpine = %+v", fs)
	}
	fs = parseDF("Filesystem Type 1024-blocks Used Available Capacity Mounted on\n/dev/sdb1 ext4 100 50 50 50% /mnt/my disk\n")
	if len(fs) != 1 || fs[0].MountPoint != "/mnt/my disk" {
		t.Errorf("spaces = %+v", fs)
	}
}

func TestParseLsblk(t *testing.T) {
	disks, err := parseLsblk(sec(t, fixtureSections(t, "ubuntu2404"), secLsblk).Out)
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]BlockDevice{}
	for _, d := range disks {
		byName[d.Name] = d
	}
	if _, ok := byName["loop0"]; ok {
		t.Error("loop devices must be skipped")
	}
	sda := byName["sda"]
	if sda.Type != "disk" || sda.SizeBytes != 250059350016 || sda.Model != "Crucial_CT250MX2" || sda.Transport != "sata" ||
		sda.Rotational == nil || *sda.Rotational || len(sda.Children) != 2 || sda.Children[0].Name != "sda1" {
		t.Errorf("sda = %+v", sda)
	}
	if nv := byName["nvme0n1"]; nv.Serial != "190456800335" || nv.Model != "SanDisk Extreme Pro 500GB" {
		t.Errorf("nvme = %+v", nv)
	}
	// default columns (fallback variant)
	def, err := parseLsblk(string(plugintest.Fixture(t, "ubuntu2404-lsblk-default.json")))
	if err != nil {
		t.Fatal(err)
	}
	if len(def) != len(disks) {
		t.Errorf("default variant: %d disks, want %d", len(def), len(disks))
	}
	for _, d := range def {
		if d.Name == "sda" && d.SizeBytes != 250059350016 {
			t.Errorf("default sda = %+v", d)
		}
	}
	// old lsblk versions print everything as strings
	old, err := parseLsblk(`{"blockdevices": [{"name": "sda", "size": "8589934592", "rota": "1", "type": "disk", "mountpoint": null,
		"children": [{"name": "sda1", "size": "8588886016", "rota": "1", "type": "part", "mountpoint": "/"}]}]}`)
	if err != nil || len(old) != 1 || old[0].SizeBytes != 8589934592 || !*old[0].Rotational || old[0].Children[0].MountPoint != "/" {
		t.Errorf("old = %+v, %v", old, err)
	}
	if _, err := parseLsblk("not json"); err == nil {
		t.Error("garbage must fail")
	}
}

func TestParseIPAddr(t *testing.T) {
	js, err := parseIPAddr(sec(t, fixtureSections(t, "ubuntu2404"), secIPAddr).Out)
	if err != nil {
		t.Fatal(err)
	}
	txt, err := parseIPAddr(string(plugintest.Fixture(t, "ubuntu2404-ip-addr.txt")))
	if err != nil {
		t.Fatal(err)
	}
	find := func(list []Interface, name string) Interface {
		for _, i := range list {
			if i.Name == name {
				return i
			}
		}
		t.Fatalf("interface %s missing", name)
		return Interface{}
	}
	for _, list := range [][]Interface{js, txt} {
		eth0 := find(list, "eth0")
		if eth0.MAC != "bc:24:11:27:22:b5" || eth0.MTU != 1500 || eth0.State != "UP" || eth0.Type != "ether" {
			t.Errorf("eth0 = %+v", eth0)
		}
		if len(eth0.Addresses) != 2 || eth0.Addresses[0] != (IfAddr{Address: "192.168.8.123", PrefixLen: 24, Family: "inet", Scope: "global", Dynamic: true}) ||
			eth0.Addresses[1].Scope != "link" || eth0.Addresses[1].Family != "inet6" {
			t.Errorf("eth0 addresses = %+v", eth0.Addresses)
		}
		if lo := find(list, "lo"); lo.MAC != "" || len(lo.Addresses) != 2 {
			t.Errorf("lo = %+v", lo)
		}
		if d := find(list, "docker0"); d.Addresses[0].Address != "172.17.0.1" {
			t.Errorf("docker0 = %+v", d)
		}
	}
	// busybox ip
	bb, err := parseIPAddr(sec(t, fixtureSections(t, "alpine322"), secIPAddr).Out)
	if err != nil {
		t.Fatal(err)
	}
	if eth0 := find(bb, "eth0"); eth0.MAC != "aa:af:ea:46:fc:99" || len(eth0.Addresses) != 1 || eth0.Addresses[0].Address != "172.17.0.3" ||
		eth0.Addresses[0].PrefixLen != 16 {
		t.Errorf("busybox eth0 = %+v", eth0)
	}
}

func TestHostAddresses(t *testing.T) {
	ifs, err := parseIPAddr(sec(t, fixtureSections(t, "ubuntu2404"), secIPAddr).Out)
	if err != nil {
		t.Fatal(err)
	}
	subnets := []netip.Prefix{netip.MustParsePrefix("192.168.8.0/24"), netip.MustParsePrefix("172.16.0.0/12")}
	ips, mac := hostAddresses(ifs, "192.168.8.123", subnets)
	if !reflect.DeepEqual(ips, []string{"192.168.8.123"}) || mac != "bc:24:11:27:22:b5" {
		t.Errorf("ips = %v, mac = %q (docker bridges must never be reported)", ips, mac)
	}
	ips, mac = hostAddresses(ifs, "10.0.0.5", nil)
	if len(ips) != 0 || mac != "" {
		t.Errorf("no subnets / unknown scanned ip: ips = %v, mac = %q", ips, mac)
	}
}

func TestParseServices(t *testing.T) {
	svcs := parseServices(sec(t, fixtureSections(t, "ubuntu2404"), secServices).Out)
	if len(svcs) != 16 {
		t.Fatalf("services = %d", len(svcs))
	}
	want := map[string]string{"ssh.service": "OpenBSD Secure Shell server", "postfix@-.service": "Postfix Mail Transport Agent (instance -)",
		"docker.service": "Docker Application Container Engine"}
	for _, s := range svcs {
		if d, ok := want[s.Unit]; ok && d != s.Description {
			t.Errorf("%s = %q", s.Unit, s.Description)
		}
		delete(want, s.Unit)
	}
	if len(want) > 0 {
		t.Errorf("missing %v", want)
	}
}

func TestParseSockets(t *testing.T) {
	socks := parseSockets(sec(t, fixtureSections(t, "ubuntu2404"), secSockets).Out, "ss")
	if len(socks) != 11 {
		t.Fatalf("sockets = %d: %+v", len(socks), socks)
	}
	var ssh, resolved, smtp6 *Socket
	for i := range socks {
		s := &socks[i]
		switch {
		case s.Proto == "tcp" && s.Port == 22:
			ssh = s
		case s.Proto == "udp" && s.Address == "127.0.0.53":
			resolved = s
		case s.Proto == "tcp" && s.Port == 25 && s.Address == "::1":
			smtp6 = s
		}
	}
	if ssh == nil || ssh.Address != "*" || !reflect.DeepEqual(ssh.Processes, []SocketProcess{{"sshd", 1166}, {"systemd", 1}}) {
		t.Errorf("ssh = %+v", ssh)
	}
	if resolved == nil || resolved.Interface != "lo" || resolved.Processes[0].Name != "systemd-resolve" {
		t.Errorf("resolved = %+v", resolved)
	}
	if smtp6 == nil || smtp6.Processes[0].Name != "master" {
		t.Errorf("smtp6 = %+v", smtp6)
	}
	// ss without -H support prints a header
	withHeader := parseSockets(string(plugintest.Fixture(t, "ubuntu2404-ss-header.txt")), "ss")
	if len(withHeader) == 0 || withHeader[0].Proto == "Netid" {
		t.Errorf("header variant = %+v", withHeader)
	}
	// busybox netstat
	ns := parseSockets(string(plugintest.Fixture(t, "alpine322-netstat.txt")), "netstat")
	want := []Socket{
		{Proto: "tcp", Address: "::", Port: 8080, Processes: []SocketProcess{{"nc", 7}}},
		{Proto: "tcp", Address: "::1", Port: 9000, Processes: []SocketProcess{{"nc", 9}}},
		{Proto: "udp", Address: "::", Port: 5353, Processes: []SocketProcess{{"nc", 8}}},
	}
	if !reflect.DeepEqual(ns, want) {
		t.Errorf("netstat = %+v", ns)
	}
	if got := parseSockets(sec(t, fixtureSections(t, "alpine322"), secSockets).Out, "netstat"); len(got) != 0 {
		t.Errorf("empty netstat = %+v", got)
	}
}

func pkgMap(list []plugin.Package) map[string]plugin.Package {
	m := map[string]plugin.Package{}
	for _, p := range list {
		m[p.Name] = p
	}
	return m
}

func TestParseDpkg(t *testing.T) {
	pkgs := parseDpkg(sec(t, fixtureSections(t, "ubuntu2404"), secPkgDpkg).Out)
	if len(pkgs) != 367 {
		t.Errorf("packages = %d, want 367", len(pkgs))
	}
	m := pkgMap(pkgs)
	for name, want := range map[string]plugin.Package{
		"openssh-server": {Name: "openssh-server", Version: "1:9.6p1-3ubuntu13", Arch: "amd64"},
		"docker-ce":      {Name: "docker-ce", Version: "5:29.8.1-1~ubuntu.24.04~noble", Arch: "amd64"},
		"adduser":        {Name: "adduser", Version: "3.137ubuntu1", Arch: "all"},
	} {
		if m[name] != want {
			t.Errorf("%s = %+v", name, m[name])
		}
	}
	// removed packages with config files left ("rc") are not installed
	got := parseDpkg("a\t1.0\tamd64\tii \nb\t2.0\tamd64\trc \nc\t3.0\tall\thi \nd\t4.0\tall\tinstall ok installed\ne\t5\tall\tdeinstall ok config-files\n")
	if len(got) != 3 || got[0].Name != "a" || got[1].Name != "c" || got[2].Name != "d" {
		t.Errorf("status filter = %+v", got)
	}
}

func TestParseRPM(t *testing.T) {
	pkgs := parseRPM(sec(t, fixtureSections(t, "rocky9"), secPkgRPM).Out)
	if len(pkgs) != 141 {
		t.Errorf("packages = %d, want 141", len(pkgs))
	}
	m := pkgMap(pkgs)
	for name, want := range map[string]plugin.Package{
		"bash":         {Name: "bash", Version: "5.1.8-6.el9_1", Arch: "x86_64"},
		"shadow-utils": {Name: "shadow-utils", Version: "2:4.9-8.el9", Arch: "x86_64"},
		"tzdata":       {Name: "tzdata", Version: "2023c-1.el9", Arch: "noarch"},
	} {
		if m[name] != want {
			t.Errorf("%s = %+v", name, m[name])
		}
	}
	if got := parseRPM("gpg-pubkey\t(none)\t8483c65d-5ccc5b19\t(none)\n"); len(got) != 0 {
		t.Errorf("gpg-pubkey = %+v", got)
	}
}

func TestParseApk(t *testing.T) {
	list := parseApk(sec(t, fixtureSections(t, "alpine322"), secPkgApk).Out)
	info := parseApk(string(plugintest.Fixture(t, "alpine322-apk-info.txt")))
	if len(list) != 16 || len(info) != 16 {
		t.Fatalf("apk list = %d, apk info = %d, want 16", len(list), len(info))
	}
	ml, mi := pkgMap(list), pkgMap(info)
	for name, want := range map[string]plugin.Package{
		"musl":                   {Name: "musl", Version: "1.2.5-r12", Arch: "x86_64"},
		"ca-certificates-bundle": {Name: "ca-certificates-bundle", Version: "20260909-r0", Arch: "x86_64"},
		"alpine-baselayout-data": {Name: "alpine-baselayout-data", Version: "3.7.0-r0", Arch: "x86_64"},
		"libcrypto3":             {Name: "libcrypto3", Version: "3.5.8-r0", Arch: "x86_64"},
	} {
		if ml[name] != want {
			t.Errorf("list %s = %+v", name, ml[name])
		}
		want.Arch = ""
		if mi[name] != want {
			t.Errorf("info %s = %+v", name, mi[name])
		}
	}
	for _, bad := range []string{"noversion", "x-1.0", "-1.0-r0"} {
		if _, _, ok := splitApkName(bad); ok {
			t.Errorf("splitApkName(%q) must fail", bad)
		}
	}
}

func TestParseDocker(t *testing.T) {
	secs := fixtureSections(t, "ubuntu2404")
	containers, bad := parseDockerPS(sec(t, secs, secDockerPS).Out)
	if bad != 0 || len(containers) != 2 {
		t.Fatalf("containers = %d, bad = %d", len(containers), bad)
	}
	images, refs, badImg := parseDockerImages(sec(t, secs, secDockerImages).Out)
	if badImg != 0 || len(images) != 2 {
		t.Fatalf("images = %d, bad = %d", len(images), badImg)
	}
	resolveImageIDs(containers, refs, images)
	const alpineID = "sha256:5291449c3df73caf6ed85e649dec1b9e818b39a5d8c871e97afc13e9cd5e8fa8"
	stopped, web := containers[0], containers[1]
	if stopped.Name != "ns-fixture-stopped" || stopped.State != "created" || stopped.Status != "Created" || len(stopped.Ports) != 0 ||
		stopped.Labels != nil || stopped.ImageID != alpineID {
		t.Errorf("stopped = %+v", stopped)
	}
	wantPorts := []plugin.ContainerPort{
		{IP: "127.0.0.1", PrivatePort: 80, PublicPort: 18081, Type: "tcp"},
		{IP: "127.0.0.1", PrivatePort: 8082, PublicPort: 18082, Type: "udp"},
		{IP: "127.0.0.1", PrivatePort: 8083, PublicPort: 18083, Type: "udp"},
	}
	if web.ID != "6431e200eb73c56520ae8704ced9bf41b117af37778fa4a5fabde0cb5209f67a" || web.Image != "alpine:3.22" || web.ImageID != alpineID ||
		web.State != "running" || web.ComposeProject != "nsfixture" || web.ComposeService != "web" ||
		web.Labels["org.example.note"] != "a,b" || !reflect.DeepEqual(web.Networks, []string{"bridge"}) ||
		!reflect.DeepEqual(web.Ports, wantPorts) || !web.Created.Equal(time.Date(2026, 9, 22, 20, 22, 27, 0, time.UTC)) {
		t.Errorf("web = %+v", web)
	}
	if images[1].ID != alpineID && images[0].ID != alpineID {
		t.Fatalf("images = %+v", images)
	}
	for _, im := range images {
		if im.ID == alpineID && (!reflect.DeepEqual(im.Tags, []string{"alpine:3.22"}) || im.Size != 12800000 ||
			!im.Created.Equal(time.Date(2026, 9, 17, 20, 37, 44, 0, time.UTC))) {
			t.Errorf("alpine image = %+v", im)
		}
	}
	// port formats of other docker versions
	got := parseDockerPorts("0.0.0.0:8000-8001->8000-8001/tcp, :::9000->9000/tcp, [::]:53->53/udp, 443/tcp")
	want := []plugin.ContainerPort{
		{IP: "0.0.0.0", PrivatePort: 8000, PublicPort: 8000, Type: "tcp"},
		{IP: "0.0.0.0", PrivatePort: 8001, PublicPort: 8001, Type: "tcp"},
		{IP: "::", PrivatePort: 9000, PublicPort: 9000, Type: "tcp"},
		{IP: "::", PrivatePort: 53, PublicPort: 53, Type: "udp"},
		{PrivatePort: 443, Type: "tcp"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ports = %+v", got)
	}
	if st := stateFromStatus("Exited (0) 2 hours ago"); st != "exited" {
		t.Errorf("state = %q", st)
	}
	if _, bad := parseDockerPS("{broken\n"); bad != 1 {
		t.Error("broken line must be counted")
	}
	for in, want := range map[string]int64{"12.8MB": 12800000, "4.1kB (virtual 8.99MB)": 4100, "1.2GB": 1200000000, "512B": 512} {
		if n, err := parseHumanSize(in); err != nil || n != want {
			t.Errorf("size %q = %d, %v", in, n, err)
		}
	}
}

func TestParseLastUpdate(t *testing.T) {
	ub := fixtureSections(t, "ubuntu2404")
	apt, err := parseAptHistory(sec(t, ub, secLastApt).Out, time.UTC)
	if err != nil || !apt.Equal(time.Date(2026, 9, 22, 19, 15, 14, 0, time.UTC)) {
		t.Errorf("apt = %v, %v", apt, err)
	}
	berlin := time.FixedZone("+0200", 7200)
	apt, _ = parseAptHistory(sec(t, ub, secLastApt).Out, berlin)
	if !apt.Equal(time.Date(2026, 9, 22, 17, 15, 14, 0, time.UTC)) {
		t.Errorf("apt with offset = %v", apt)
	}
	st, err := parseEpoch(sec(t, ub, secLastDpkg).Out)
	if err != nil || st.Unix() != 1790104516 {
		t.Errorf("dpkg status = %v, %v", st, err)
	}
	rpm, err := parseRPMLast(sec(t, fixtureSections(t, "rocky9"), secLastRPM).Out, time.UTC)
	if err != nil || !rpm.Equal(time.Date(2023, 11, 19, 22, 25, 22, 0, time.UTC)) {
		t.Errorf("rpm = %v, %v", rpm, err)
	}
	for _, line := range []string{
		"kernel-5.14.0-427.el9.x86_64    Mon 15 Jul 2024 10:11:12 AM UTC",
		"kernel-5.14.0-427.el9.x86_64    Mon Jul 15 10:11:12 2024",
	} {
		if tm, err := parseRPMLast(line, time.UTC); err != nil || !tm.Equal(time.Date(2024, 7, 15, 10, 11, 12, 0, time.UTC)) {
			t.Errorf("%q = %v, %v", line, tm, err)
		}
	}
	apk, err := parseEpoch(sec(t, fixtureSections(t, "alpine322"), secLastApk).Out)
	if err != nil || apk.IsZero() {
		t.Errorf("apk = %v, %v", apk, err)
	}
	if _, err := parseAptHistory("", time.UTC); err == nil {
		t.Error("empty history must fail")
	}
}

func TestParseResultUbuntu(t *testing.T) {
	r := parseResult(plugintest.Fixture(t, "ubuntu2404.out"), plugintest.Fixture(t, "ubuntu2404.err"), false)
	inv := r.Inventory
	if r.Hostname != "netscope" || r.OS == nil || r.OS.Name != "Ubuntu 24.04 LTS" || inv.Kernel != "Linux 7.0.14-8-pve x86_64 GNU/Linux" {
		t.Errorf("host = %q, os = %+v, kernel = %q", r.Hostname, r.OS, inv.Kernel)
	}
	if len(inv.Errors) != 0 {
		t.Errorf("errors = %v", inv.Errors)
	}
	if !reflect.DeepEqual(inv.Unavailable, []string{secLastApk, secLastRPM, secPkgApk, secPkgRPM}) {
		t.Errorf("unavailable = %v", inv.Unavailable)
	}
	if r.Packages == nil || r.Packages.Manager != "dpkg" || len(r.Packages.Packages) != 367 || inv.PackageCount != 367 {
		t.Errorf("packages = %+v", r.Packages)
	}
	if r.Containers == nil || len(r.Containers.Containers) != 2 || len(r.Containers.Images) != 2 ||
		inv.Docker == nil || *inv.Docker != (DockerInfo{Containers: 2, Running: 1, Images: 2}) {
		t.Errorf("containers = %+v, docker = %+v", r.Containers, inv.Docker)
	}
	if inv.LastUpdate == nil || inv.LastUpdateSource != "apt-history" || inv.PackageDBModified == nil {
		t.Errorf("last update = %v (%s)", inv.LastUpdate, inv.LastUpdateSource)
	}
	if inv.UptimeSeconds != 4303 || inv.BootTime == nil || inv.CPU == nil || inv.CPU.Usable != 4 || inv.Memory == nil ||
		len(inv.Filesystems) != 1 || len(inv.Disks) == 0 || len(inv.Interfaces) != 5 || len(inv.Services) != 16 || len(inv.Listening) != 11 {
		t.Errorf("inventory = %+v", inv)
	}

	obs := r.observation(7, "192.168.8.123", "root", []netip.Prefix{netip.MustParsePrefix("192.168.8.0/24")}, "raw")
	if obs.DeviceID != 7 || !obs.Present || obs.IP != "192.168.8.123" || !reflect.DeepEqual(obs.IPs, []string{"192.168.8.123"}) ||
		!reflect.DeepEqual(obs.MACs, []string{"bc:24:11:27:22:b5"}) || obs.Hostname != "netscope" ||
		obs.Attrs["os.kernel"] != inv.Kernel || len(obs.Attrs) != 1 || obs.Raw != "raw" {
		t.Errorf("observation = %+v", obs)
	}
	oi, ok := obs.Inventory.(Inventory)
	if !ok || oi.User != "root" {
		t.Errorf("inventory = %T %+v", obs.Inventory, obs.Inventory)
	}
}

func TestParseResultVariants(t *testing.T) {
	cases := []struct {
		host        string
		os          string
		manager     string
		packages    int
		unavailable []string
		errors      []string
		lastSource  string
	}{
		{"alpine322", "Alpine Linux v3.22", "apk", 16,
			[]string{secDockerImages, secDockerPS, secLastApt, secLastDpkg, secLastRPM, secLsblk, secPkgDpkg, secPkgRPM, secServices}, nil, "apk"},
		{"busybox137", "", "", 0,
			[]string{secDockerImages, secDockerPS, secLastApk, secLastApt, secLastDpkg, secLsblk, secPkgApk, secPkgDpkg, secServices},
			[]string{secOSRelease, secPkgRPM}, ""},
		{"rocky9", "Rocky Linux 9.3 (Blue Onyx)", "rpm", 141,
			[]string{secDockerImages, secDockerPS, secIPAddr, secLastApk, secLastApt, secLastDpkg, secPkgApk, secPkgDpkg, secServices, secSockets},
			nil, "rpm"},
	}
	for _, c := range cases {
		r := parseResult(plugintest.Fixture(t, c.host+".out"), plugintest.Fixture(t, c.host+".err"), false)
		inv := r.Inventory
		osName := ""
		if r.OS != nil {
			osName = r.OS.Name
		}
		if osName != c.os || r.Hostname == "" {
			t.Errorf("%s: os = %q, host = %q", c.host, osName, r.Hostname)
		}
		if c.manager == "" && r.Packages != nil || c.manager != "" && (r.Packages == nil || r.Packages.Manager != c.manager || len(r.Packages.Packages) != c.packages) {
			t.Errorf("%s: packages = %+v", c.host, r.Packages)
		}
		if !reflect.DeepEqual(inv.Unavailable, c.unavailable) {
			t.Errorf("%s: unavailable = %v", c.host, inv.Unavailable)
		}
		var errs []string
		for k := range inv.Errors {
			errs = append(errs, k)
		}
		sortStrings(errs)
		if !reflect.DeepEqual(errs, c.errors) {
			t.Errorf("%s: errors = %v", c.host, inv.Errors)
		}
		if inv.LastUpdateSource != c.lastSource {
			t.Errorf("%s: last update source = %q", c.host, inv.LastUpdateSource)
		}
		if r.Containers != nil {
			t.Errorf("%s: containers must be nil without docker", c.host)
		}
	}
}

func TestParseResultTruncated(t *testing.T) {
	out := plugintest.Fixture(t, "ubuntu2404.out")
	i := strings.Index(string(out), "openssh-server\t")
	r := parseResult(out[:i], nil, true)
	if r.Packages != nil {
		t.Error("an incomplete package list must not be reported")
	}
	if r.Inventory.Errors[secPkgDpkg] != "Ausgabe gekürzt (Limit erreicht)" {
		t.Errorf("errors = %v", r.Inventory.Errors)
	}
	if r.Containers != nil || r.Hostname != "netscope" {
		t.Errorf("containers = %+v, host = %q", r.Containers, r.Hostname)
	}
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
