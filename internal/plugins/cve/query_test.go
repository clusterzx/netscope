package cve

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"netscope/internal/db"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/settings"
)

// queryFixture: two SSH hosts (one with the CVE ignored), a Grafana host and an Ubuntu
// host with packages, matched against the real fixtures.
func queryFixture(t *testing.T) (*db.DB, map[string]int64) {
	t.Helper()
	ctx := context.Background()
	d := newTestDB(t)
	loadFixtures(t, d)
	c := newClock()
	t0 := c.Ms()
	ids := map[string]int64{}
	for _, name := range []string{"nas", "pi"} {
		ids[name] = addDevice(t, d, name, t0)
		addPort(t, d, ids[name], 22, "OpenSSH", "9.6p1", []string{"cpe:/a:openbsd:openssh:9.6p1"}, t0)
	}
	ids["grafana"] = addDevice(t, d, "grafana", t0)
	addHTTPApp(t, d, ids["grafana"], 3000, []plugin.DetectedApp{{Name: "Grafana", Version: "11.0.0", CPE: "cpe:2.3:a:grafana:grafana", Confidence: "high"}}, t0)
	ids["ubuntu"] = addDevice(t, d, "ubuntu", t0)
	addPackage(t, d, ids["ubuntu"], "dpkg", "curl", "8.5.0-2ubuntu10.6", t0)
	addPackage(t, d, ids["ubuntu"], "dpkg", "openssh-server", "1:9.6p1-3ubuntu13.5", t0)
	p := newTestPlugin(c)
	rc, _ := testRC(t, p, d, nil)
	if _, err := p.match(ctx, rc, loadConfig(rc.Settings), nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := SetIgnored(ctx, d, ids["pi"], "CVE-2024-6387", true, "nur intern erreichbar", "admin"); err != nil {
		t.Fatal(err)
	}
	return d, ids
}

func TestListVulnerabilities(t *testing.T) {
	ctx := context.Background()
	d, ids := queryFixture(t)
	rows, total, err := ListVulnerabilities(ctx, d, Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if total != len(rows) || total != 3 { // CVE-2024-6387, CVE-2024-9264, CVE-2024-2398
		t.Fatalf("total %d rows %d", total, len(rows))
	}
	byID := map[string]VulnRow{}
	for i, r := range rows {
		byID[r.CVE] = r
		if i > 0 && rows[i-1].CVSS != nil && r.CVSS != nil && *rows[i-1].CVSS < *r.CVSS {
			t.Errorf("not sorted by score: %v before %v", *rows[i-1].CVSS, *r.CVSS)
		}
	}
	ssh := byID["CVE-2024-6387"]
	// nas (range) + ubuntu (package); pi is ignored
	if ssh.Devices != 2 || ssh.IgnoredDevices != 0 || ssh.AllIgnored || ssh.CVSS == nil || *ssh.CVSS != 8.1 ||
		ssh.Severity != "high" || ssh.Published == nil || !strings.Contains(ssh.Description, "OpenSSH") ||
		strings.Join(ssh.MatchTypes, ",") != "heuristic,range" || strings.Join(ssh.Products, ",") != "OpenSSH,openssh-server" {
		t.Errorf("CVE-2024-6387 row %+v", ssh)
	}
	withIgnored, _, _ := ListVulnerabilities(ctx, d, Filter{IncludeIgnored: true, Text: "cve-2024-6387"})
	if len(withIgnored) != 1 || withIgnored[0].Devices != 3 || withIgnored[0].IgnoredDevices != 1 || withIgnored[0].AllIgnored {
		t.Errorf("with ignored %+v", withIgnored)
	}
	onlyPi, _, _ := ListVulnerabilities(ctx, d, Filter{IncludeIgnored: true, DeviceID: ids["pi"]})
	if len(onlyPi) != 1 || !onlyPi[0].AllIgnored {
		t.Errorf("pi %+v", onlyPi)
	}
	if hidden, _, _ := ListVulnerabilities(ctx, d, Filter{DeviceID: ids["pi"]}); len(hidden) != 0 {
		t.Errorf("ignored CVE listed: %+v", hidden)
	}
	crit, _, _ := ListVulnerabilities(ctx, d, Filter{MinScore: 8.5})
	for _, r := range crit {
		if r.CVSS == nil || *r.CVSS < 8.5 {
			t.Errorf("min score: %+v", r)
		}
	}
	if len(crit) == 0 || len(crit) >= len(rows) {
		t.Errorf("min score filter: %d of %d", len(crit), len(rows))
	}
	graf, _, _ := ListVulnerabilities(ctx, d, Filter{Product: "grafana"})
	if len(graf) != 1 || graf[0].CVE != "CVE-2024-9264" {
		t.Errorf("product filter %+v", graf)
	}
	race, _, _ := ListVulnerabilities(ctx, d, Filter{Text: "race condition"})
	if len(race) != 1 || race[0].CVE != "CVE-2024-6387" {
		t.Errorf("text filter %+v", race)
	}
	page, total2, _ := ListVulnerabilities(ctx, d, Filter{Sort: "cve", Limit: 2, Offset: 1})
	if total2 != total || len(page) != 2 || page[0].CVE > page[1].CVE {
		t.Errorf("paging %d %+v", total2, page)
	}
	byDevices, _, _ := ListVulnerabilities(ctx, d, Filter{Sort: "-devices"})
	if byDevices[0].CVE != "CVE-2024-6387" {
		t.Errorf("sort by devices: %s", byDevices[0].CVE)
	}
	if _, _, err := ListVulnerabilities(ctx, d, Filter{Sort: "name"}); err == nil {
		t.Error("unknown sort accepted")
	}
	b, _ := json.Marshal(rows[0])
	for _, key := range []string{`"cve":`, `"cvss":`, `"matchTypes":`, `"allIgnored":`, `"firstSeen":`} {
		if !strings.Contains(string(b), key) {
			t.Errorf("json %s lacks %s", b, key)
		}
	}
}

func TestDeviceCVEsAndDetail(t *testing.T) {
	ctx := context.Background()
	d, ids := queryFixture(t)
	list, err := DeviceCVEs(ctx, d, ids["nas"], false)
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("nas CVEs %+v", list)
	}
	r := list[0]
	if r.CVE != "CVE-2024-6387" || r.DeviceName != "nas" || r.MatchType != MatchRange || r.Product != "OpenSSH" || r.Version != "9.6p1" ||
		r.CPE != "cpe:2.3:a:openbsd:openssh:9.6p1:*:*:*:*:*:*:*" || r.Source != "nmap" || r.Vector == "" || len(r.Refs) == 0 ||
		len(r.CWEs) != 2 || r.Ignored || r.FirstSeen.IsZero() || r.CVSSVersion != "3.1" {
		t.Errorf("device CVE %+v", r)
	}
	pi, _ := DeviceCVEs(ctx, d, ids["pi"], true)
	if len(pi) != 1 || !pi[0].Ignored || pi[0].IgnoreNote != "nur intern erreichbar" || pi[0].IgnoredBy != "admin" {
		t.Errorf("pi %+v", pi)
	}
	info, devs, err := CVEDetail(ctx, d, " cve-2024-6387 ")
	if err != nil {
		t.Fatal(err)
	}
	if info.ID != "CVE-2024-6387" || !info.InMirror || info.Status != "Modified" || info.CVSS == nil || *info.CVSS != 8.1 ||
		info.CPEMatchesTotal < 80 || len(info.CPEMatches) != info.CPEMatchesTotal || info.URL != "https://nvd.nist.gov/vuln/detail/CVE-2024-6387" {
		t.Errorf("detail %+v", info)
	}
	var sawRange bool
	for _, m := range info.CPEMatches {
		if m.Product == "openssh" && m.VersionStartIncluding == "8.6" && m.VersionEndIncluding == "9.8" &&
			m.Criteria == "cpe:2.3:a:openbsd:openssh:*:*:*:*:*:*:*:*" {
			sawRange = true
		}
	}
	if !sawRange {
		t.Error("openssh range missing in detail")
	}
	if len(devs) != 3 {
		t.Errorf("affected devices %d", len(devs))
	}
	if _, _, err := CVEDetail(ctx, d, "CVE-2099-9999"); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("unknown CVE: %v", err)
	}
	if _, _, err := CVEDetail(ctx, d, "not-a-cve"); err == nil || errors.Is(err, db.ErrNotFound) {
		t.Errorf("invalid id: %v", err)
	}
	if err := SetIgnored(ctx, d, 99999, "CVE-2024-6387", true, "", "admin"); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("unknown device: %v", err)
	}
	if err := SetIgnored(ctx, d, ids["nas"], "CVE-24-1", true, "", "admin"); err == nil {
		t.Error("invalid CVE accepted")
	}
	// updating an existing mark keeps one row
	if err := SetIgnored(ctx, d, ids["pi"], "CVE-2024-6387", true, "neu", "bob"); err != nil {
		t.Fatal(err)
	}
	pi, _ = DeviceCVEs(ctx, d, ids["pi"], true)
	if pi[0].IgnoreNote != "neu" || pi[0].IgnoredBy != "bob" {
		t.Errorf("updated mark %+v", pi[0])
	}
}

func TestSummary(t *testing.T) {
	ctx := context.Background()
	d, _ := queryFixture(t)
	s, err := Summary(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	// CVE-2024-6387 on nas + ubuntu (pi ignored), CVE-2024-9264 on grafana (high 8.8),
	// CVE-2024-2398 via curl on ubuntu (NVD: high 8.6)
	if s["high"] < 4 || s["devices"] != 3 || s["total"] != s["critical"]+s["high"]+s["medium"]+s["low"]+s["none"]+s["unknown"] {
		t.Errorf("summary %v", s)
	}
	for _, k := range []string{"critical", "high", "medium", "low", "unknown", "total", "devices"} {
		if _, ok := s[k]; !ok {
			t.Errorf("missing key %s", k)
		}
	}
	empty := newTestDB(t)
	s, _ = Summary(ctx, empty)
	if s["total"] != 0 || s["critical"] != 0 {
		t.Errorf("empty summary %v", s)
	}
	st, err := SyncStatus(ctx, empty)
	if err != nil || !st.Empty || st.LastMatch != nil || len(st.Feeds) != 0 {
		t.Errorf("empty status %+v %v", st, err)
	}
}

// TestInventoryIntegration feeds real inventory changes (from the core's ingest) into
// the change handler.
func TestInventoryIntegration(t *testing.T) {
	ctx := context.Background()
	d := newTestDB(t)
	loadFixtures(t, d)
	st, err := settings.Load(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	store, err := inventory.New(ctx, d, nil, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	var (
		mu      sync.Mutex
		changes []plugin.Change
	)
	store.OnChanges(func(c []plugin.Change) {
		mu.Lock()
		changes = append(changes, c...)
		mu.Unlock()
	})
	take := func() []plugin.Change {
		mu.Lock()
		defer mu.Unlock()
		out := changes
		changes = nil
		return out
	}
	// the core stamps inventory rows with the wall clock, so the plugin must use it too
	p := New()
	p.retryDelay = time.Millisecond
	rc, evs := testRC(t, p, d, nil)
	// seed a full match so later changes are not the device's first evaluation
	if _, err := p.match(ctx, rc, loadConfig(rc.Settings), nil, nil); err != nil {
		t.Fatal(err)
	}
	obs := &plugin.Observation{MACs: []string{"aa:bb:cc:00:00:10"}, IP: "192.168.8.10", Present: true, Hostname: "nas",
		Ports: &plugin.PortScan{Protocol: "tcp", Scanned: []plugin.PortRange{{From: 1, To: 1024}}, Ports: []plugin.Port{
			{Port: 22, State: "open", Service: "ssh", Product: "OpenSSH", Version: "9.8p1", ExtraInfo: "protocol 2.0", CPEs: []string{"cpe:/a:openbsd:openssh:9.8p1"}},
		}}}
	dev, err := store.Observe(ctx, "nmap", 0, obs)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.HandleChanges(ctx, rc, take()); err != nil {
		t.Fatal(err)
	}
	if got := activeCVEs(t, d, dev); len(got) != 0 {
		t.Fatalf("9.8p1: %v", got)
	}
	// the next scan sees a vulnerable version (e.g. a container with an old image)
	time.Sleep(5 * time.Millisecond)
	obs.Ports.Ports[0].Version = "9.6p1"
	obs.Ports.Ports[0].CPEs = []string{"cpe:/a:openbsd:openssh:9.6p1"}
	if _, err := store.Observe(ctx, "nmap", 0, obs); err != nil {
		t.Fatal(err)
	}
	ch := take()
	if len(ch) != 1 || ch[0].Type != plugin.ChangePortChanged {
		t.Fatalf("changes %+v", ch)
	}
	if err := p.HandleChanges(ctx, rc, ch); err != nil {
		t.Fatal(err)
	}
	if got := activeCVEs(t, d, dev); got["CVE-2024-6387"] != MatchRange {
		t.Fatalf("9.6p1: %v", got)
	}
	news := eventsOf(evs, plugin.EvCVENew)
	if len(news) != 1 || news[0].Title != "CVE-2024-6387 (CVSS 8.1) auf nas" {
		t.Fatalf("events %+v", evs.Events)
	}
	// the core's device list sees the match (cve>=7)
	ids, err := store.MatchingIDs(ctx, "cve>=7")
	if err != nil || len(ids) != 1 || ids[0] != dev {
		t.Fatalf("query cve>=7: %v %v", ids, err)
	}
}
