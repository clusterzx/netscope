package cve

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"netscope/internal/plugin"
)

// kevDoc builds a KEV catalog; pairs are CVE ID and date added.
func kevDoc(pairs ...string) []byte {
	var vulns []map[string]any
	for i := 0; i+1 < len(pairs); i += 2 {
		vulns = append(vulns, map[string]any{"cveID": pairs[i], "vendorProject": "Vendor", "product": "Produkt",
			"vulnerabilityName": "Name " + pairs[i], "dateAdded": pairs[i+1], "shortDescription": "Beschreibung",
			"requiredAction": "Apply updates per vendor instructions.", "dueDate": "2026-10-16",
			"knownRansomwareCampaignUse": "Unknown", "notes": "https://example.org/advisory"})
	}
	b, _ := json.Marshal(map[string]any{"title": "CISA Catalog of Known Exploited Vulnerabilities", "catalogVersion": "2026.09.25",
		"dateReleased": "2026-09-25T18:58:16Z", "count": len(vulns), "vulnerabilities": vulns})
	return b
}

// epssDoc builds an EPSS CSV body from "cve,score,percentile" rows.
func epssDoc(rows ...string) string {
	return "#model_version:v2026.06.15,score_date:2026-09-26T12:00:22Z\ncve,epss,percentile\n" + strings.Join(rows, "\n") + "\n"
}

func gz(s string) []byte {
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	zw.Write([]byte(s))
	zw.Close()
	return b.Bytes()
}

func TestParseKEVAndEPSS(t *testing.T) {
	cat, err := parseKEV(kevDoc("CVE-2024-6387", "2026-09-20", "not-a-cve", "2026-09-21", "cve-2021-44228", "2021-12-10"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cat.Vulnerabilities) != 2 || cat.Vulnerabilities[1].CVE != "CVE-2021-44228" || cat.CatalogVersion != "2026.09.25" {
		t.Fatalf("kev: %+v", cat)
	}
	if _, err := parseKEV(kevDoc()); err == nil {
		t.Error("an empty catalog must be rejected")
	}
	if _, err := parseKEV([]byte("<html>")); err == nil {
		t.Error("garbage accepted")
	}

	var rows []epssRow
	version, n, err := parseEPSS(gz(epssDoc("CVE-2024-6387,0.93,0.99", "CVE-2021-44228,0.94432,0.99962", "bogus,1,1",
		"CVE-2020-0001,1.5,0.2", "CVE-2020-0002,x,0.2")), func(r epssRow) error {
		rows = append(rows, r)
		return nil
	})
	if err != nil || n != 2 || version != "v2026.06.15 · 2026-09-26" {
		t.Fatalf("epss: %q %d %v", version, n, err)
	}
	if rows[1].cve != "CVE-2021-44228" || rows[1].score != 0.94432 || rows[1].percentile != 0.99962 {
		t.Errorf("rows %+v", rows)
	}
	// without comment line, other column order
	_, n, err = parseEPSS(gz("percentile,cve,epss\n0.5,CVE-2024-0001,0.2\n"), func(epssRow) error { return nil })
	if err != nil || n != 1 {
		t.Errorf("no comment: %d %v", n, err)
	}
	if _, _, err := parseEPSS(gz(epssDoc()), func(epssRow) error { return nil }); err == nil {
		t.Error("an empty score file must be rejected")
	}
	if _, _, err := parseEPSS([]byte("plain"), func(epssRow) error { return nil }); err == nil {
		t.Error("non-gzip accepted")
	}
}

func TestPrioritySyncAndEvents(t *testing.T) {
	ctx := context.Background()
	d := newTestDB(t)
	loadFixtures(t, d)
	c := newClock()
	t0 := c.Ms()
	nas := addDevice(t, d, "nas", t0)
	addPort(t, d, nas, 22, "OpenSSH", "9.6p1", []string{"cpe:/a:openbsd:openssh:9.6p1"}, t0)
	web := addDevice(t, d, "dashboard", t0)
	addHTTPApp(t, d, web, 3000, []plugin.DetectedApp{{Name: "Grafana", Version: "10.2.3", CPE: "cpe:2.3:a:grafana:grafana", Confidence: "high"}}, t0)
	ignoredDev := addDevice(t, d, "gateway", t0)
	addPort(t, d, ignoredDev, 22, "OpenSSH", "9.6p1", []string{"cpe:/a:openbsd:openssh:9.6p1"}, t0)

	fs := newFeedServer(t)
	fs.setPrio(kevDoc("CVE-2021-44228", "2021-12-10"), epssDoc("CVE-2024-6387,0.93,0.99", "CVE-2024-1442,0.02,0.4"))
	p := newTestPlugin(c)
	rc, evs := testRC(t, p, d, fs.settings(nil))
	cfg := loadConfig(rc.Settings)

	c.Advance(time.Minute)
	if _, err := p.match(ctx, rc, cfg, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := SetIgnored(ctx, d, ignoredDev, "CVE-2024-6387", true, "", "admin"); err != nil {
		t.Fatal(err)
	}

	// 1. first load: history, no "added" CVEs
	st, err := p.syncPriority(ctx, rc, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	if st.KEV != 1 || len(st.KEVAdded) != 0 || st.EPSS != 2 || len(st.Updated) != 2 {
		t.Fatalf("first sync: %+v", st)
	}
	if row := feedRow(t, rc, feedEPSS); row.Status != "ok" || row.CVECount != 2 || row.LastModified != "v2026.06.15 · 2026-09-26" {
		t.Errorf("epss state %+v", row)
	}

	// 2. unchanged downloads leave the tables alone
	if st, err = p.syncPriority(ctx, rc, cfg, false); err != nil || len(st.Updated) != 0 || st.KEV != 1 || st.EPSS != 2 {
		t.Fatalf("unchanged sync: %+v %v", st, err)
	}

	// 3. CISA adds the OpenSSH CVE: one cve.exploited for the matching device, none for the
	// device where it is marked irrelevant
	fs.setPrio(kevDoc("CVE-2021-44228", "2021-12-10", "CVE-2024-6387", "2026-09-25"), "")
	if st, err = p.syncPriority(ctx, rc, cfg, false); err != nil {
		t.Fatal(err)
	}
	if strings.Join(st.KEVAdded, ",") != "CVE-2024-6387" {
		t.Fatalf("added %v", st.KEVAdded)
	}
	n, err := p.exploitedEvents(ctx, rc, st.KEVAdded)
	if err != nil || n != 1 {
		t.Fatalf("exploited events: %d %v", n, err)
	}
	ex := eventsOf(evs, plugin.EvCVEExploited)
	if len(ex) != 1 || ex[0].DeviceID != nas || ex[0].Severity != plugin.SevCritical || ex[0].Title != "CVE-2024-6387 auf nas wird aktiv ausgenutzt" ||
		ex[0].Payload["date_added"] != "2026-09-25" || ex[0].Payload["epss"] != 0.93 || ex[0].Payload["cvss"] != 8.1 ||
		ex[0].DedupKey != fmt.Sprintf("cve.exploited:%d:CVE-2024-6387", nas) {
		t.Fatalf("event %+v", ex)
	}

	// 4. lists: exploited first, filters, detail
	rows, total, err := ListVulnerabilities(ctx, d, Filter{})
	if err != nil || total == 0 {
		t.Fatal(total, err)
	}
	if rows[0].CVE != "CVE-2024-6387" || !rows[0].Exploited || rows[0].KEVAdded != "2026-09-25" || rows[0].EPSS == nil || *rows[0].EPSS != 0.93 {
		t.Fatalf("first row %+v", rows[0])
	}
	for _, r := range rows[1:] {
		if r.Exploited {
			t.Errorf("exploited row %s not first", r.CVE)
		}
	}
	if rows, _, _ := ListVulnerabilities(ctx, d, Filter{Exploited: true}); len(rows) != 1 || rows[0].CVE != "CVE-2024-6387" {
		t.Errorf("exploited filter: %+v", rows)
	}
	if rows, _, _ := ListVulnerabilities(ctx, d, Filter{MinEPSS: 0.5}); len(rows) != 1 || rows[0].CVE != "CVE-2024-6387" {
		t.Errorf("epss filter: %+v", rows)
	}
	if rows, _, err := ListVulnerabilities(ctx, d, Filter{Sort: "-epss"}); err != nil || rows[0].CVE != "CVE-2024-6387" || rows[1].CVE != "CVE-2024-1442" {
		t.Errorf("epss sort: %v %v", rows, err)
	}
	devs, _ := DeviceCVEs(ctx, d, nas, false)
	if len(devs) == 0 || devs[0].CVE != "CVE-2024-6387" || !devs[0].Exploited || *devs[0].EPSSPercentile != 0.99 {
		t.Errorf("device rows %+v", devs)
	}
	info, _, err := CVEDetail(ctx, d, "CVE-2024-6387")
	if err != nil || info.KEV == nil || info.KEV.DueDate != "2026-10-16" || info.KEV.Action == "" || !strings.Contains(info.KEV.URL, "CVE-2024-6387") ||
		info.EPSS == nil || *info.EPSS != 0.93 {
		t.Fatalf("detail %+v %v", info, err)
	}
	if info, _, _ := CVEDetail(ctx, d, "CVE-2024-1442"); info.KEV != nil || *info.EPSS != 0.02 {
		t.Errorf("not exploited: %+v", info.KEV)
	}
	sum, _ := Summary(ctx, d)
	if sum["exploited"] != 1 || sum["exploitedDevices"] != 1 {
		t.Errorf("summary %v", sum)
	}

	// 5. a broken download keeps the old data and reports the feed
	fs.setPrio([]byte(`{"vulnerabilities":[]}`), "")
	if _, err := p.syncPriority(ctx, rc, cfg, false); err == nil || !strings.Contains(err.Error(), "KEV") {
		t.Fatalf("broken catalog: %v", err)
	}
	if row := feedRow(t, rc, feedKEV); row.Status != "error" {
		t.Errorf("kev state %+v", row)
	}
	if rows, _, _ := ListVulnerabilities(ctx, d, Filter{Exploited: true}); len(rows) != 1 {
		t.Error("KEV data lost after a broken download")
	}
	if len(eventsOf(evs, plugin.EvNVDSyncFailed)) != 1 {
		t.Errorf("sync failure event missing: %+v", evs.Events)
	}
}

// Exploited CVEs raise cve.new below the CVSS threshold, as critical, unless disabled.
func TestNewEventForExploitedCVE(t *testing.T) {
	for _, always := range []bool{true, false} {
		t.Run(fmt.Sprint(always), func(t *testing.T) {
			ctx := context.Background()
			d := newTestDB(t)
			loadFixtures(t, d)
			c := newClock()
			t0 := c.Ms()
			web := addDevice(t, d, "dashboard", t0)
			port := addPort(t, d, web, 22, "OpenSSH", "9.8p1", []string{"cpe:/a:openbsd:openssh:9.8p1"}, t0)
			fs := newFeedServer(t)
			fs.setPrio(kevDoc("CVE-2024-6387", "2026-09-25"), epssDoc("CVE-2024-6387,0.93,0.99"))
			p := newTestPlugin(c)
			rc, evs := testRC(t, p, d, fs.settings(map[string]any{"min_event_score": "9.0", "kev_always": always}))
			cfg := loadConfig(rc.Settings)
			if _, err := p.syncPriority(ctx, rc, cfg, false); err != nil {
				t.Fatal(err)
			}
			c.Advance(time.Minute)
			if _, err := p.match(ctx, rc, cfg, nil, nil); err != nil {
				t.Fatal(err)
			}
			// OpenSSH is downgraded to a vulnerable release: CVE-2024-6387 (CVSS 8.1 < 9.0) is new
			t1 := c.Advance(time.Hour).UnixMilli()
			closeRow(t, d, "ports", port, t1)
			addPort(t, d, web, 22, "OpenSSH", "9.6p1", []string{"cpe:/a:openbsd:openssh:9.6p1"}, t1)
			if err := p.HandleChanges(ctx, rc, []plugin.Change{{Type: plugin.ChangePortChanged, DeviceID: web, At: c.Now()}}); err != nil {
				t.Fatal(err)
			}
			news := eventsOf(evs, plugin.EvCVENew)
			if !always {
				if len(news) != 0 {
					t.Fatalf("events below threshold: %+v", news)
				}
				return
			}
			if len(news) != 1 || news[0].Severity != plugin.SevCritical || news[0].Payload["kev"] != true || news[0].Payload["epss"] != 0.93 ||
				!strings.Contains(news[0].Message, "aktiv ausgenutzt") {
				t.Fatalf("cve.new %+v", news)
			}
		})
	}
}
