package cve

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"netscope/internal/plugin"
)

// TestMatchKnownCases matches devices against the real NVD fixtures and checks known
// positive and negative results.
func TestMatchKnownCases(t *testing.T) {
	ctx := context.Background()
	d := newTestDB(t)
	loadFixtures(t, d)
	c := newClock()
	t0 := c.Ms()
	ssh := func(name, version string, extra ...string) int64 {
		id := addDevice(t, d, name, t0)
		pid := addPort(t, d, id, 22, "OpenSSH", version, []string{"cpe:/a:openbsd:openssh:" + serviceVersion(version)}, t0)
		if len(extra) > 0 {
			_, _ = d.W.Exec("UPDATE ports SET extra_info = ? WHERE id = ?", extra[0], pid)
		}
		return id
	}
	// CVE-2024-6387 (regreSSHion): NVD lists < 4.4, 4.4 (no update), 8.5p1 and 8.6 – 9.8
	sshVuln := ssh("ssh-9.6p1", "9.6p1")
	sshFixed := ssh("ssh-9.8p1", "9.8p1")
	// nmap may add the distribution; the NVD lists ubuntu_linux:24.04 for CVE-2024-6387 as a whole
	if _, err := d.W.Exec(`UPDATE ports SET cpes = '["cpe:/a:openbsd:openssh:9.8p1","cpe:/o:canonical:ubuntu_linux:24.04"]' WHERE device_id = ?`, sshFixed); err != nil {
		t.Fatal(err)
	}
	sshOld := ssh("ssh-4.4p1", "4.4p1")
	sshExact := ssh("ssh-8.5p1", "8.5p1")
	sshAncient := ssh("ssh-4.3p2", "4.3p2")
	sshBefore := ssh("ssh-8.4p1", "8.4p1")
	sshUbuntu := ssh("ssh-ubuntu", "9.6p1 Ubuntu 3ubuntu13.5", "Ubuntu Linux; protocol 2.0")
	// version only in the version field, CPE without version
	sshNoCPEVersion := addDevice(t, d, "ssh-cpe-without-version", t0)
	addPort(t, d, sshNoCPEVersion, 22, "OpenSSH", "9.7p1", []string{"cpe:/a:openbsd:openssh", "cpe:/o:linux:linux_kernel"}, t0)

	// Grafana web app detection (CVE-2024-1442 ranges, CVE-2024-9264 exact 11.0.0)
	grafana := func(name, version string) int64 {
		id := addDevice(t, d, name, t0)
		addHTTPApp(t, d, id, 3000, []plugin.DetectedApp{{Name: "Grafana", Version: version, Confidence: "high", CPE: "cpe:2.3:a:grafana:grafana"}}, t0)
		return id
	}
	graf1023 := grafana("grafana-10.2.3", "10.2.3")
	graf1025 := grafana("grafana-10.2.5", "10.2.5")
	graf1100 := grafana("grafana-11.0.0", "11.0.0")
	graf1101 := grafana("grafana-11.0.1", "11.0.1")

	// nginx via the old nmap name igor_sysoev (CVE-2021-23017 lists f5:nginx < 1.20.1,
	// CVE-2024-7347 lists f5:nginx_open_source 1.5.13 – 1.26.2)
	nginxOld := addDevice(t, d, "nginx-1.18.0", t0)
	addPort(t, d, nginxOld, 80, "nginx", "1.18.0", []string{"cpe:/a:igor_sysoev:nginx:1.18.0"}, t0)
	nginxNew := addDevice(t, d, "nginx-1.27.1", t0)
	addPort(t, d, nginxNew, 80, "nginx", "1.27.1", []string{"cpe:/a:igor_sysoev:nginx:1.27.1"}, t0)

	// Apache exact version (CVE-2021-41773: only 2.4.49)
	apache49 := addDevice(t, d, "apache-2.4.49", t0)
	addPort(t, d, apache49, 80, "Apache httpd", "2.4.49", []string{"cpe:/a:apache:http_server:2.4.49"}, t0)
	apache50 := addDevice(t, d, "apache-2.4.50", t0)
	addPort(t, d, apache50, 80, "Apache httpd", "2.4.50", []string{"cpe:/a:apache:http_server:2.4.50"}, t0)

	// dropbear via the nmap name (CVE-2021-36369: <= 2020.81)
	dropbear := addDevice(t, d, "router", t0)
	addPort(t, d, dropbear, 22, "Dropbear sshd", "2020.81", []string{"cpe:/a:matt_johnston:dropbear_ssh_server:2020.81", "cpe:/o:linux:linux_kernel"}, t0)
	dropbearNew := addDevice(t, d, "router-new", t0)
	addPort(t, d, dropbearNew, 22, "Dropbear sshd", "2022.83", []string{"cpe:/a:matt_johnston:dropbear_ssh_server:2022.83"}, t0)

	// "*" without version bounds is too broad for applications (CVE-2024-23332)
	notation := addDevice(t, d, "notation", t0)
	addPort(t, d, notation, 8080, "notation", "1.0.0", []string{"cpe:/a:notaryproject:notation-go:1.0.0"}, t0)

	// device firmware where the NVD lists all versions (CVE-2024-20287, cisco wap371)
	ap := addDevice(t, d, "wap371", t0)
	addOSFact(t, d, ap, "nmap", plugin.OSInfo{Name: "Cisco WAP371", Accuracy: 95, CPEs: []string{"cpe:/o:cisco:wap371_firmware:1.3.0.7"}}, t0)

	// imprecise OS guess: "6.1" could be any 6.1.x (CVE-2024-1086: 6.1 <= v < 6.1.76)
	guess := addDevice(t, d, "linux-guess", t0)
	addOSFact(t, d, guess, "nmap", plugin.OSInfo{Name: "Linux 6.1", Accuracy: 90, CPEs: []string{"cpe:/o:linux:linux_kernel:6.1"}}, t0)
	// nmap reports accuracy 100 even for candidate lists; a fingerprint kernel is never matched
	kernelFP := addDevice(t, d, "linux-fingerprint", t0)
	addOSFact(t, d, kernelFP, "nmap", plugin.OSInfo{Name: "Linux 6.1.69", Accuracy: 100, CPEs: []string{"cpe:/o:linux:linux_kernel:6.1.69"}}, t0)
	// generic OS guesses (CVE-2024-2398: macOS 13.0 <= v < 13.6.8) are not matched by default
	macRange := addDevice(t, d, "mac-range", t0)
	addOSFact(t, d, macRange, "nmap", plugin.OSInfo{Name: "Apple macOS 13.0 - 13.4 (Darwin 22.1.0 - 22.5.0)", Accuracy: 100, CPEs: []string{"cpe:/o:apple:macos:13"}}, t0)
	macSingle := addDevice(t, d, "mac-single", t0)
	addOSFact(t, d, macSingle, "nmap", plugin.OSInfo{Name: "Apple macOS 13.5", Accuracy: 100, CPEs: []string{"cpe:/o:apple:macos:13.5"}}, t0)
	bsd := addDevice(t, d, "freebsd-guess", t0)
	addOSFact(t, d, bsd, "nmap", plugin.OSInfo{Name: "FreeBSD 13.2", Accuracy: 100, CPEs: []string{"cpe:/o:freebsd:freebsd:13.2"}}, t0)
	// kernel facts: exact upstream versions are matched, ABI versions are not
	kernelFact := addDevice(t, d, "kernel-fact", t0)
	addOSFact(t, d, kernelFact, "ssh", plugin.OSInfo{Name: "Debian 12", Accuracy: 100, CPEs: []string{"cpe:/o:linux:linux_kernel:6.1.69"}}, t0)
	kernelABI := addDevice(t, d, "kernel-abi", t0)
	addOSFact(t, d, kernelABI, "ssh", plugin.OSInfo{Name: "Ubuntu 24.04", Accuracy: 100, CPEs: []string{"cpe:/o:linux:linux_kernel:6.8.0-45-generic"}}, t0)

	// SSH host with packages (Ubuntu 24.04 style versions) and a distribution CPE
	ubuntu := addDevice(t, d, "ubuntu-host", t0)
	addOSFact(t, d, ubuntu, "ssh", plugin.OSInfo{Name: "Fedora Linux 39", Accuracy: 100, CPEs: []string{"cpe:/o:fedoraproject:fedora:39"}}, t0)
	addPackage(t, d, ubuntu, "dpkg", "libssl3t64", "3.0.13-0ubuntu3.4", t0)
	addPackage(t, d, ubuntu, "dpkg", "openssl", "3.0.13-0ubuntu3.4", t0)
	addPackage(t, d, ubuntu, "dpkg", "curl", "8.5.0-2ubuntu10.6", t0)
	addPackage(t, d, ubuntu, "dpkg", "libc6", "2.39-0ubuntu8.3", t0)
	addPackage(t, d, ubuntu, "dpkg", "sudo", "1.9.15p5-3ubuntu5.24.04.1", t0)
	addPackage(t, d, ubuntu, "dpkg", "linux-image-6.8.0-45-generic", "6.8.0-45.45", t0)
	addPackage(t, d, ubuntu, "dpkg", "vim-common", "2:9.1.0016-1ubuntu7.3", t0)
	// old sudo (Baron Samedit CVE-2021-3156 < 1.8.32) and a fixed 1.9.5p2
	oldSudo := addDevice(t, d, "focal-host", t0)
	addPackage(t, d, oldSudo, "dpkg", "sudo", "1.8.31-1ubuntu1", t0)
	fixedSudo := addDevice(t, d, "bullseye-host", t0)
	addPackage(t, d, fixedSudo, "dpkg", "sudo", "1.9.5p2-3+deb11u1", t0)
	// Debian kernels: only the newest installed kernel counts
	debOld := addDevice(t, d, "debian-old-kernel", t0)
	addPackage(t, d, debOld, "dpkg", "linux-image-6.1.0-17-amd64", "6.1.69-1", t0)
	debNew := addDevice(t, d, "debian-new-kernel", t0)
	addPackage(t, d, debNew, "dpkg", "linux-image-6.1.0-17-amd64", "6.1.69-1", t0)
	addPackage(t, d, debNew, "dpkg", "linux-image-6.1.0-25-amd64", "6.1.106-3", t0)

	p := newTestPlugin(c)
	rc, _ := testRC(t, p, d, nil)
	ms, err := p.match(ctx, rc, loadConfig(rc.Settings), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if ms.Devices < 25 {
		t.Fatalf("matched %d devices", ms.Devices)
	}
	expect := func(dev int64, cve, typ string) {
		t.Helper()
		got := activeCVEs(t, d, dev)
		if typ == "" {
			if g, ok := got[cve]; ok {
				t.Errorf("device %d: %s must not match (got %s)", dev, cve, g)
			}
			return
		}
		if got[cve] != typ {
			t.Errorf("device %d: %s = %q, want %q (all: %v)", dev, cve, got[cve], typ, got)
		}
	}
	expect(sshVuln, "CVE-2024-6387", MatchRange)
	expect(sshFixed, "CVE-2024-6387", "")
	expect(sshOld, "CVE-2024-6387", "")
	expect(sshExact, "CVE-2024-6387", MatchExact)
	expect(sshAncient, "CVE-2024-6387", MatchRange)
	expect(sshBefore, "CVE-2024-6387", "")
	expect(sshUbuntu, "CVE-2024-6387", MatchHeuristic)
	expect(sshNoCPEVersion, "CVE-2024-6387", MatchRange)

	expect(graf1023, "CVE-2024-1442", MatchRange)
	expect(graf1025, "CVE-2024-1442", "")
	expect(graf1100, "CVE-2024-9264", MatchExact)
	expect(graf1101, "CVE-2024-9264", "")
	expect(graf1023, "CVE-2024-9264", "")

	expect(nginxOld, "CVE-2021-23017", MatchRange)
	expect(nginxOld, "CVE-2024-7347", MatchRange)
	expect(nginxNew, "CVE-2021-23017", "")
	expect(nginxNew, "CVE-2024-7347", "")

	expect(apache49, "CVE-2021-41773", MatchExact)
	expect(apache50, "CVE-2021-41773", "")

	expect(dropbear, "CVE-2021-36369", MatchRange)
	expect(dropbearNew, "CVE-2021-36369", "")

	expect(notation, "CVE-2024-23332", "")
	expect(ap, "CVE-2024-20287", MatchHeuristic)
	expect(guess, "CVE-2024-1086", "")
	expect(kernelFP, "CVE-2024-1086", "")
	expect(macRange, "CVE-2024-2398", "")
	expect(macSingle, "CVE-2024-2398", "")
	expect(bsd, "CVE-2024-6387", "")
	expect(kernelFact, "CVE-2024-1086", MatchRange)
	expect(kernelABI, "CVE-2024-1086", "")

	// packages are always heuristic
	expect(ubuntu, "CVE-2024-6119", MatchHeuristic) // openssl 3.0.0 <= 3.0.13 < 3.0.15
	expect(ubuntu, "CVE-2024-0727", "")             // < 3.0.13 only
	expect(ubuntu, "CVE-2024-2398", MatchHeuristic) // curl 7.44.0 <= 8.5.0 < 8.7.0
	expect(ubuntu, "CVE-2024-2961", MatchHeuristic) // glibc < 2.40
	expect(ubuntu, "CVE-2021-3156", "")             // sudo 1.9.15p5
	expect(ubuntu, "CVE-2024-1086", "")             // Ubuntu ABI kernel is not matched
	expect(oldSudo, "CVE-2021-3156", MatchHeuristic)
	expect(fixedSudo, "CVE-2021-3156", "")
	expect(debOld, "CVE-2024-1086", MatchHeuristic)
	expect(debNew, "CVE-2024-1086", "")

	// libssl3t64 and openssl map to the same CPE: one row per CVE
	var n int
	_ = d.R.QueryRow("SELECT COUNT(*) FROM device_cves WHERE device_id = ? AND cve_id = 'CVE-2024-6119'", ubuntu).Scan(&n)
	if n != 1 {
		t.Errorf("openssl rows: %d", n)
	}
	// the fedora distribution CPE of the curl CVE must not be matched via the OS
	var cpe, product, version, source string
	if err := d.R.QueryRow("SELECT cpe, product, version, source FROM device_cves WHERE device_id = ? AND cve_id = 'CVE-2024-2398'", ubuntu).
		Scan(&cpe, &product, &version, &source); err != nil {
		t.Fatal(err)
	}
	if cpe != "cpe:2.3:a:haxx:curl:8.5.0:*:*:*:*:*:*:*" || product != "curl" || version != "8.5.0-2ubuntu10.6" || source != "ssh" {
		t.Errorf("curl row: %s %s %s %s", cpe, product, version, source)
	}
	// device CPEs are stored canonically (igor_sysoev -> f5)
	if err := d.R.QueryRow("SELECT cpe, product, version FROM device_cves WHERE device_id = ? AND cve_id = 'CVE-2021-23017'", nginxOld).
		Scan(&cpe, &product, &version); err != nil {
		t.Fatal(err)
	}
	if cpe != "cpe:2.3:a:f5:nginx:1.18.0:*:*:*:*:*:*:*" || product != "nginx" || version != "1.18.0" {
		t.Errorf("nginx row: %s %s %s", cpe, product, version)
	}
	// scores are copied for the core's device list filters (cve>=7)
	var score float64
	_ = d.R.QueryRow("SELECT cvss_score FROM device_cves WHERE device_id = ? AND cve_id = 'CVE-2024-6387'", sshVuln).Scan(&score)
	if score != 8.1 {
		t.Errorf("score %v", score)
	}

	// settings: packages disabled -> package rows close
	rc2, _ := testRC(t, p, d, map[string]any{"include_packages": false})
	if _, err := p.match(ctx, rc2, loadConfig(rc2.Settings), []int64{ubuntu}, nil); err != nil {
		t.Fatal(err)
	}
	if got := activeCVEs(t, d, ubuntu); len(got) != 0 {
		t.Errorf("package matches with packages disabled: %v", got)
	}

	// opt-in: uncertain OS guesses are matched, always heuristically; the fingerprint
	// kernel stays excluded
	guesses := []int64{macRange, macSingle, bsd, kernelFP}
	rc3, _ := testRC(t, p, d, map[string]any{"include_os_guesses": true})
	if _, err := p.match(ctx, rc3, loadConfig(rc3.Settings), guesses, nil); err != nil {
		t.Fatal(err)
	}
	expect(macSingle, "CVE-2024-2398", MatchHeuristic)
	expect(bsd, "CVE-2024-6387", MatchHeuristic)
	expect(kernelFP, "CVE-2024-1086", "")
	// switching it off again closes the rows silently (no cve.resolved flood)
	rc4, evs := testRC(t, p, d, nil)
	if _, err := p.match(ctx, rc4, loadConfig(rc4.Settings), guesses, nil); err != nil {
		t.Fatal(err)
	}
	expect(macSingle, "CVE-2024-2398", "")
	for _, ev := range evs.Events {
		if ev.Type == plugin.EvCVEResolved {
			t.Errorf("resolved event after disabling guesses: %s", ev.Title)
		}
	}
}

// TestEventsAndReconcile follows one device through initial import, software change,
// ignore and update, checking device_cves history and events.
func TestEventsAndReconcile(t *testing.T) {
	ctx := context.Background()
	d := newTestDB(t)
	loadFixtures(t, d)
	c := newClock()
	t0 := c.Ms()
	host := addDevice(t, d, "nas", t0)
	port := addPort(t, d, host, 22, "OpenSSH", "9.8p1", []string{"cpe:/a:openbsd:openssh:9.8p1"}, t0)
	web := addDevice(t, d, "dashboard", t0)
	addHTTPApp(t, d, web, 3000, []plugin.DetectedApp{{Name: "Grafana", Version: "10.2.3", CPE: "cpe:2.3:a:grafana:grafana", Confidence: "high"}}, t0)

	p := newTestPlugin(c)
	rc, evs := testRC(t, p, d, nil)
	cfg := loadConfig(rc.Settings)

	// 1. initial full match: matches are stored, no events
	c.Advance(time.Minute)
	if _, err := p.match(ctx, rc, cfg, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := activeCVEs(t, d, web); got["CVE-2024-1442"] != MatchRange {
		t.Fatalf("grafana: %v", got)
	}
	if len(evs.Events) != 0 {
		t.Fatalf("initial match raised events: %+v", evs.Events)
	}

	// 2. the SSH version changes (downgrade to a vulnerable release): cve.new
	t2 := c.Advance(time.Hour).UnixMilli()
	closeRow(t, d, "ports", port, t2)
	port = addPort(t, d, host, 22, "OpenSSH", "9.6p1", []string{"cpe:/a:openbsd:openssh:9.6p1"}, t2)
	if err := p.HandleChanges(ctx, rc, []plugin.Change{{Type: plugin.ChangePortChanged, DeviceID: host, At: c.Now()}}); err != nil {
		t.Fatal(err)
	}
	news := eventsOf(evs, plugin.EvCVENew)
	if len(news) != 1 {
		t.Fatalf("cve.new events: %+v", evs.Events)
	}
	ev := news[0]
	if ev.Title != "CVE-2024-6387 (CVSS 8.1) auf nas" || ev.DeviceID != host || ev.Severity != plugin.SevHigh ||
		ev.DedupKey != fmt.Sprintf("cve:%d:CVE-2024-6387", host) {
		t.Errorf("event %+v", ev)
	}
	if ev.Payload["cve"] != "CVE-2024-6387" || ev.Payload["cvss"] != 8.1 || ev.Payload["match_type"] != MatchRange ||
		ev.Payload["product"] != "OpenSSH" || ev.Payload["version"] != "9.6p1" ||
		ev.Payload["cpe"] != "cpe:2.3:a:openbsd:openssh:9.6p1:*:*:*:*:*:*:*" ||
		ev.Payload["vector"] != "CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:H/A:H" {
		t.Errorf("payload %+v", ev.Payload)
	}
	if !strings.Contains(ev.Message, "Versionsbereich") || !strings.Contains(ev.Message, "race condition") {
		t.Errorf("message %q", ev.Message)
	}

	// 3. mark irrelevant: survives re-matching, hidden from lists
	if err := SetIgnored(ctx, d, host, "cve-2024-6387", true, "Backport eingespielt", "admin"); err != nil {
		t.Fatal(err)
	}
	c.Advance(time.Hour)
	if _, err := p.match(ctx, rc, cfg, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := activeCVEs(t, d, host); got["CVE-2024-6387"] != MatchRange {
		t.Fatalf("ignored CVE must stay matched: %v", got)
	}
	var ign int
	_ = d.R.QueryRow("SELECT COUNT(*) FROM cve_ignores WHERE device_id = ? AND cve_id = 'CVE-2024-6387'", host).Scan(&ign)
	if ign != 1 {
		t.Fatal("ignore mark lost")
	}
	list, _ := DeviceCVEs(ctx, d, host, false)
	for _, r := range list {
		if r.CVE == "CVE-2024-6387" {
			t.Error("ignored CVE listed")
		}
	}
	all, _ := DeviceCVEs(ctx, d, host, true)
	found := false
	for _, r := range all {
		if r.CVE == "CVE-2024-6387" {
			found = r.Ignored && r.IgnoreNote == "Backport eingespielt" && r.IgnoredBy == "admin" && r.IgnoredAt != nil
		}
	}
	if !found {
		t.Errorf("ignored row: %+v", all)
	}

	// 4. un-ignore, then update to a fixed release: row closed, cve.resolved
	if err := SetIgnored(ctx, d, host, "CVE-2024-6387", false, "", ""); err != nil {
		t.Fatal(err)
	}
	t4 := c.Advance(time.Hour).UnixMilli()
	closeRow(t, d, "ports", port, t4)
	addPort(t, d, host, 22, "OpenSSH", "9.9p1", []string{"cpe:/a:openbsd:openssh:9.9p1"}, t4)
	if err := p.HandleChanges(ctx, rc, []plugin.Change{{Type: plugin.ChangePortChanged, DeviceID: host, At: c.Now()}}); err != nil {
		t.Fatal(err)
	}
	if got := activeCVEs(t, d, host); len(got) != 0 {
		t.Errorf("after update: %v", got)
	}
	res := eventsOf(evs, plugin.EvCVEResolved)
	if len(res) != 1 || res[0].Payload["cve"] != "CVE-2024-6387" || res[0].Payload["cvss"] != 8.1 {
		t.Fatalf("cve.resolved: %+v", res)
	}
	var gone int
	_ = d.R.QueryRow("SELECT COUNT(*) FROM device_cves WHERE device_id = ? AND cve_id = 'CVE-2024-6387' AND gone_at = ?", host, t4).Scan(&gone)
	if gone != 1 {
		t.Errorf("history rows closed at update: %d", gone)
	}

	// 5. a CVE the NVD changed since the last sync is news for existing devices
	if _, err := d.W.Exec("DELETE FROM device_cves WHERE device_id = ?", web); err != nil {
		t.Fatal(err)
	}
	c.Advance(time.Hour)
	before := len(evs.Events)
	if _, err := p.match(ctx, rc, cfg, nil, map[string]struct{}{"CVE-2024-1442": {}}); err != nil {
		t.Fatal(err)
	}
	news = eventsOf(evs, plugin.EvCVENew)
	if len(evs.Events) != before+1 || news[len(news)-1].Payload["cve"] != "CVE-2024-1442" {
		t.Fatalf("NVD change event: %+v", evs.Events[before:])
	}

	// 6. matches that only appear because of new history in the mirror are silent
	if _, err := d.W.Exec("DELETE FROM device_cves WHERE device_id = ?", web); err != nil {
		t.Fatal(err)
	}
	c.Advance(time.Hour)
	before = len(evs.Events)
	if _, err := p.match(ctx, rc, cfg, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(evs.Events) != before {
		t.Errorf("unexpected events: %+v", evs.Events[before:])
	}
}

// TestFirstDataIsInitial: a new package inventory on a known device raises no events.
func TestFirstDataIsInitial(t *testing.T) {
	ctx := context.Background()
	d := newTestDB(t)
	loadFixtures(t, d)
	c := newClock()
	host := addDevice(t, d, "server", c.Ms())
	addPort(t, d, host, 22, "OpenSSH", "9.9p1", []string{"cpe:/a:openbsd:openssh:9.9p1"}, c.Ms())
	p := newTestPlugin(c)
	rc, evs := testRC(t, p, d, nil)
	cfg := loadConfig(rc.Settings)
	c.Advance(time.Minute)
	if _, err := p.match(ctx, rc, cfg, nil, nil); err != nil {
		t.Fatal(err)
	}
	// the SSH scanner delivers the first package list (the core emits no change for it)
	t1 := c.Advance(time.Hour).UnixMilli()
	addPackage(t, d, host, "dpkg", "curl", "8.5.0-2ubuntu10.6", t1)
	c.Advance(time.Minute)
	if _, err := p.match(ctx, rc, cfg, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got := activeCVEs(t, d, host); got["CVE-2024-2398"] != MatchHeuristic {
		t.Fatalf("curl not matched: %v", got)
	}
	if len(evs.Events) != 0 {
		t.Fatalf("first package inventory raised events: %+v", evs.Events)
	}
	// a later package update is news (heuristic events enabled by default) ...
	var pkgID int64
	_ = d.R.QueryRow("SELECT id FROM packages WHERE device_id = ?", host).Scan(&pkgID)
	t2 := c.Advance(time.Hour).UnixMilli()
	closeRow(t, d, "packages", pkgID, t2)
	addPackage(t, d, host, "dpkg", "curl", "8.6.0-1", t2)
	if _, err := d.W.Exec("DELETE FROM device_cves WHERE device_id = ?", host); err != nil { // pretend it was never matched
		t.Fatal(err)
	}
	c.Advance(time.Minute)
	if _, err := p.match(ctx, rc, cfg, nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(eventsOf(evs, plugin.EvCVENew)) != 1 {
		t.Fatalf("package update: %+v", evs.Events)
	}
	// ... unless heuristic events are disabled or the score is below the threshold
	rcQuiet, quiet := testRC(t, p, d, map[string]any{"heuristic_events": false})
	if _, err := d.W.Exec("DELETE FROM device_cves WHERE device_id = ?", host); err != nil {
		t.Fatal(err)
	}
	t3 := c.Advance(time.Hour).UnixMilli()
	_ = d.R.QueryRow("SELECT id FROM packages WHERE device_id = ? AND gone_at IS NULL", host).Scan(&pkgID)
	closeRow(t, d, "packages", pkgID, t3)
	addPackage(t, d, host, "dpkg", "curl", "8.6.0-2", t3)
	c.Advance(time.Minute)
	if _, err := p.match(ctx, rcQuiet, loadConfig(rcQuiet.Settings), nil, nil); err != nil {
		t.Fatal(err)
	}
	if len(quiet.Events) != 0 {
		t.Fatalf("heuristic events disabled: %+v", quiet.Events)
	}
}

// TestChangeBatching: changes of a running scan are queued until the run finishes.
func TestChangeBatching(t *testing.T) {
	ctx := context.Background()
	d := newTestDB(t)
	loadFixtures(t, d)
	c := newClock()
	p := newTestPlugin(c)
	rc, _ := testRC(t, p, d, nil)
	host := addDevice(t, d, "new-host", c.Ms())
	addPort(t, d, host, 22, "OpenSSH", "9.6p1", []string{"cpe:/a:openbsd:openssh:9.6p1"}, c.Ms())
	changes := []plugin.Change{
		{Type: plugin.ChangeDeviceCreated, DeviceID: host, RunID: 7},
		{Type: plugin.ChangePortOpened, DeviceID: host, RunID: 7, Initial: true},
		{Type: plugin.ChangeHostname, DeviceID: host + 100, RunID: 7}, // irrelevant
	}
	if err := p.HandleChanges(ctx, rc, changes); err != nil {
		t.Fatal(err)
	}
	if got := activeCVEs(t, d, host); len(got) != 0 {
		t.Fatalf("matched before the run finished: %v", got)
	}
	if len(p.pending) != 1 {
		t.Fatalf("pending %v", p.pending)
	}
	if err := p.HandleRunFinished(ctx, rc, plugin.RunSummary{RunID: 7, PluginID: "nmap", Kind: plugin.KindScanner, Status: "success",
		Started: c.Now().Add(-time.Minute), Finished: c.Now()}); err != nil {
		t.Fatal(err)
	}
	if got := activeCVEs(t, d, host); got["CVE-2024-6387"] != MatchRange {
		t.Fatalf("after run end: %v", got)
	}
	// a stale queue is flushed by the next change even without a run end
	host2 := addDevice(t, d, "other", c.Ms())
	addPort(t, d, host2, 22, "OpenSSH", "9.6p1", []string{"cpe:/a:openbsd:openssh:9.6p1"}, c.Ms())
	_ = p.HandleChanges(ctx, rc, []plugin.Change{{Type: plugin.ChangePortOpened, DeviceID: host2, RunID: 8}})
	c.Advance(3 * time.Minute)
	_ = p.HandleChanges(ctx, rc, []plugin.Change{{Type: plugin.ChangeHostname, DeviceID: host2, RunID: 8}})
	if got := activeCVEs(t, d, host2); got["CVE-2024-6387"] != MatchRange {
		t.Fatalf("stale queue not flushed: %v", got)
	}
	// web applications change without a Change: picked up from the finished run
	web := addDevice(t, d, "web", c.Ms())
	if _, err := d.W.Exec(`INSERT INTO http_services(device_id, ip, port, scheme, url, apps, source, run_id, first_seen, last_seen)
		VALUES (?, '192.168.8.20', 443, 'https', 'https://x', '[{"name":"Grafana","version":"11.0.0","confidence":"high","cpe":"cpe:2.3:a:grafana:grafana"}]', 'http', 9, ?, ?)`,
		web, c.Ms(), c.Ms()); err != nil {
		t.Fatal(err)
	}
	if err := p.HandleRunFinished(ctx, rc, plugin.RunSummary{RunID: 9, PluginID: "http", Kind: plugin.KindScanner, Status: "success",
		Started: c.Now().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	if got := activeCVEs(t, d, web); got["CVE-2024-9264"] != MatchExact {
		t.Fatalf("http run: %v", got)
	}
	// nothing happens while the mirror is empty
	empty := newTestDB(t)
	rcE, _ := testRC(t, p, empty, nil)
	dev := addDevice(t, empty, "x", c.Ms())
	if err := p.HandleChanges(ctx, rcE, []plugin.Change{{Type: plugin.ChangePortOpened, DeviceID: dev}}); err != nil {
		t.Fatal(err)
	}
}

// TestMatchPerformance: 500 devices against a few thousand criteria rows.
func TestMatchPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("performance test (make test runs it separately)")
	}
	ctx := context.Background()
	d := newTestDB(t)
	c := newClock()
	now := c.Ms()
	const products, cvesPerProduct, devices = 60, 50, 500
	var batch []*cveRecord
	for pi := 0; pi < products; pi++ {
		for ci := 0; ci < cvesPerProduct; ci++ {
			score := float64(ci%10) + 0.5
			rec := &cveRecord{ID: fmt.Sprintf("CVE-2030-%d", 10000+pi*cvesPerProduct+ci), LastModified: now, Status: "Analyzed",
				Score: &score, Severity: string(plugin.SeverityFromCVSS(score)), Description: "synthetic"}
			for k := 0; k < 3; k++ {
				rec.Matches = append(rec.Matches, cpeCriteria{
					CPE:       CPE{Part: "a", Vendor: fmt.Sprintf("vendor%d", pi), Product: fmt.Sprintf("product%d", pi), Version: "*", Update: "*"},
					StartIncl: fmt.Sprintf("%d.0", k), EndExcl: fmt.Sprintf("%d.%d", k, 5+ci%5)})
			}
			rec.Matches = append(rec.Matches, cpeCriteria{CPE: CPE{Part: "a", Vendor: fmt.Sprintf("vendor%d", pi), Product: fmt.Sprintf("product%d", pi),
				Version: fmt.Sprintf("1.%d", ci), Update: "*"}})
			batch = append(batch, rec)
		}
	}
	for i := 0; i < len(batch); i += batchSize {
		if _, err := writeBatch(ctx, d, batch[i:min(i+batchSize, len(batch))], false, false, map[string]struct{}{}); err != nil {
			t.Fatal(err)
		}
	}
	tx, err := d.W.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < devices; i++ {
		res, err := tx.Exec(`INSERT INTO devices(display_name, created_source, first_seen, state, created_at, updated_at) VALUES (?, 'arpscan', ?, 'known', ?, ?)`,
			fmt.Sprintf("dev%d", i), now, now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		for k := 0; k < 8; k++ {
			pi := (i + k*7) % products
			cpe := fmt.Sprintf(`["cpe:/a:vendor%d:product%d:%d.%d"]`, pi, pi, k%3, i%9)
			if _, err := tx.Exec(`INSERT INTO ports(device_id, ip, proto, port, state, product, version, cpes, source, first_seen, last_seen)
				VALUES (?, ?, 'tcp', ?, 'open', 'p', 'v', ?, 'nmap', ?, ?)`, id, fmt.Sprintf("10.0.%d.%d", i/250, i%250), 1000+k, cpe, now, now); err != nil {
				t.Fatal(err)
			}
		}
		for k := 0; k < 40; k++ {
			if _, err := tx.Exec(`INSERT INTO packages(device_id, manager, name, version, source, first_seen, last_seen) VALUES (?, 'dpkg', ?, '1.0-1', 'ssh', ?, ?)`,
				id, fmt.Sprintf("unmapped-pkg-%d", k), now, now); err != nil {
				t.Fatal(err)
			}
		}
		for _, pk := range [][2]string{{"openssl", "3.0.13-0ubuntu3.4"}, {"curl", "8.5.0-2ubuntu10.6"}, {"libc6", "2.39-0ubuntu8.3"}} {
			if _, err := tx.Exec(`INSERT INTO packages(device_id, manager, name, version, source, first_seen, last_seen) VALUES (?, 'dpkg', ?, ?, 'ssh', ?, ?)`,
				id, pk[0], pk[1], now, now); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	p := newTestPlugin(c)
	rc, _ := testRC(t, p, d, nil)
	start := time.Now()
	ms, err := p.match(ctx, rc, loadConfig(rc.Settings), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	first := time.Since(start)
	c.Advance(time.Hour)
	start = time.Now()
	if _, err := p.match(ctx, rc, loadConfig(rc.Settings), nil, nil); err != nil {
		t.Fatal(err)
	}
	second := time.Since(start)
	t.Logf("500 devices: first match %s (%d active CVEs, %d CPEs), re-match %s", first.Round(time.Millisecond), ms.Active, ms.CPEs,
		second.Round(time.Millisecond))
	if ms.Devices != devices || ms.Active == 0 {
		t.Fatalf("stats %+v", ms)
	}
	if first > 30*time.Second || second > 30*time.Second {
		t.Fatalf("too slow: %s / %s", first, second)
	}
}
