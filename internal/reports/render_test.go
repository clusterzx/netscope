package reports

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"netscope/internal/i18n"
	"netscope/internal/plugin"
)

func sampleReport(devices int) *ChangeReport {
	to := time.Date(2026, 9, 23, 13, 33, 0, 0, time.UTC)
	r := &ChangeReport{From: to.Add(-7 * 24 * time.Hour), To: to, GeneratedAt: to,
		Devices:       map[string]int{"total": 34, "online": 30, "new": devices, "unknown": 3},
		EventCounts:   map[string]int{plugin.EvDeviceNew: 33, plugin.EvDeviceOffline: 34, plugin.EvPortOpened: 5},
		OpenHigh:      1,
		CVEBySeverity: map[string]int{"critical": 35, "high": 225, "medium": 245, "low": 56},
		FailedRuns:    map[string]int{"nmap": 1},
	}
	for i := 0; i < devices; i++ {
		r.NewDevices = append(r.NewDevices, DeviceLine{ID: int64(i + 1), Name: fmt.Sprintf("gerät-%d <script>", i), IP: fmt.Sprintf("192.168.8.%d", i+2),
			Vendor: "Espressif Inc.", At: to.Add(-time.Hour).Format(time.RFC3339)})
	}
	r.Important = []EventLine{{ID: 7, TS: to.Format(time.RFC3339), Type: plugin.EvHealthDown, Severity: "high", Title: "Check ausgefallen: TCP 22", Device: "nas"}}
	r.NewCVEs = []CVELine{{CVE: "CVE-2024-6387", CVSS: 8.1, Device: "netscope", Detail: "OpenSSH 9.6p1"}}
	r.ExpiringCerts = []CertLine{{Device: "coolify", Endpoint: "192.168.8.204:443", Subject: "home.example.net", NotAfter: to.Format(time.RFC3339), DaysLeft: 5}}
	r.Outages = []OutageLine{{Check: "TCP 22 · nas", State: "down", Started: to.Format(time.RFC3339), Seconds: 7500, Ongoing: true}}
	return r
}

func TestEmailHTML(t *testing.T) {
	r := sampleReport(60)
	out := r.EmailHTML(i18n.DE, time.UTC, "https://ns.example.lan/")
	for _, want := range []string{
		"gerät-0 &lt;script&gt;",                  // escaped
		`href="https://ns.example.lan/devices/1"`, // device link without double slash
		`href="https://ns.example.lan/vulnerabilities/CVE-2024-6387"`,
		">35 kritisch</span>", ">1 hoch</span>", // badges
		"… und 20 weitere im PDF-Anhang", // 60 devices, 40 shown
		"2 h 5 min", ">andauernd</span>", ">5 Tage</span>",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("email HTML lacks %q", want)
		}
	}
	if strings.Contains(out, "<script>") || strings.Contains(out, "**") || strings.Contains(out, "##") {
		t.Error("unescaped HTML or raw Markdown in the email")
	}
	// without a public URL there are no links
	if strings.Contains(r.EmailHTML(i18n.DE, time.UTC, ""), "<a ") {
		t.Error("links without public URL")
	}
	// an empty period says so
	empty := &ChangeReport{From: r.From, To: r.To, Devices: map[string]int{}, EventCounts: map[string]int{}, CVEBySeverity: map[string]int{}, FailedRuns: map[string]int{}}
	if !strings.Contains(empty.EmailHTML(i18n.DE, time.UTC, ""), "Keine Änderungen im Zeitraum") {
		t.Error("empty report message missing")
	}
}

func TestReportPDF(t *testing.T) {
	for _, n := range []int{0, 5, 150} { // 150 devices: several pages with repeated table headers
		var buf bytes.Buffer
		if err := sampleReport(n).PDF(&buf, i18n.DE, time.UTC); err != nil {
			t.Fatal(err)
		}
		if !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) || buf.Len() < 2000 {
			t.Fatalf("%d devices: invalid PDF (%d bytes)", n, buf.Len())
		}
		pages := bytes.Count(buf.Bytes(), []byte("/Type /Page\n"))
		if n == 150 && pages < 3 {
			t.Errorf("150 devices on %d pages", pages)
		}
	}
	var buf bytes.Buffer
	devs := []InventoryDevice{{}}
	devs[0].Name, devs[0].IP, devs[0].State, devs[0].Online = "nas", "192.168.8.5", "known", true
	if err := InventoryPDF(&buf, devs, i18n.DE, time.UTC); err != nil || !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("inventory PDF: %v", err)
	}
}

func TestNotificationExtra(t *testing.T) {
	s, err := sampleReport(3).NotificationExtra(i18n.DE, time.UTC, "")
	if err != nil {
		t.Fatal(err)
	}
	var x plugin.NotificationExtra
	if err := json.Unmarshal([]byte(s), &x); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(x.HTML, "Neue Geräte") || len(x.Attachments) != 1 ||
		x.Attachments[0].Name != "netscope-bericht_2026-09-16_2026-09-23.pdf" || !bytes.HasPrefix(x.Attachments[0].Data, []byte("%PDF-")) {
		t.Errorf("extra: html=%d attachments=%+v", len(x.HTML), len(x.Attachments))
	}
}

// TestChangeReportEnglish renders the change report in English: headings, labels and
// dates follow the language, no German headings remain.
func TestChangeReportEnglish(t *testing.T) {
	r := sampleReport(60)
	md := r.Markdown(i18n.EN, time.UTC, "https://ns.example.lan/reports")
	for _, want := range []string{
		"**Period:** 16 Sep 2026 13:33 – 23 Sep 2026 13:33",
		"**Devices:** 34 total, 30 online, 60 new, 3 unknown",
		"**Open events:** 0 critical, 1 high",
		"**Vulnerabilities (active):** 35 critical, 225 high, 245 medium, 56 low",
		"## New devices", "– Espressif Inc., first seen 23 Sep 2026 12:33",
		"## Important events (high/critical)", "- [High] 23 Sep 2026 13:33",
		"## New vulnerabilities", "CVE-2024-6387 (CVSS 8.1) on netscope – OpenSSH 9.6p1",
		"## Certificates expiring soon", "(192.168.8.204:443, coolify): 5 days",
		"## Outages", "TCP 22 · nas: Outage since 23 Sep 2026 13:33",
		"## Failed plugin runs",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("English Markdown lacks %q", want)
		}
	}
	for _, de := range []string{"Zeitraum", "Geräte:", "## Neue", "Schwachstellen", "Ausfälle", "Fehlgeschlagene", " Tage", " ab ", "Dauer", ", seit", "kritisch"} {
		if strings.Contains(md, de) {
			t.Errorf("English Markdown contains German %q", de)
		}
	}
	if b, _, err := r.ToBytes("md", i18n.EN, time.UTC, ""); err != nil || !strings.HasPrefix(string(b), "# NetScope change report\n") {
		t.Errorf("English Markdown download: %v %.40q", err, b)
	}

	html := r.EmailHTML(i18n.EN, time.UTC, "")
	for _, want := range []string{
		"Period <strong", "16 Sep 2026 13:33 – 23 Sep 2026 13:33", ">35 critical</span>", ">1 high</span>",
		">Important events", ">New devices", ">New vulnerabilities", ">Expiring certificates", ">Outages", ">Events by type", ">Failed plugin runs",
		">Vendor</th>", ">First seen</th>", ">Remaining validity</th>", "… and 20 more in the PDF attachment",
		">5 days</span>", ">ongoing</span>", ">Outage</span>", ">open</span>", "23 Sep 13:33",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("English email HTML lacks %q", want)
		}
	}
	for _, de := range []string{"Zeitraum", "Neue Geräte", "Ablaufende", "Wichtige", "PDF-Anhang", "Tage", "andauernd", "kritisch", "Hersteller", "Erstmals", "Restlaufzeit", "offen"} {
		if strings.Contains(html, de) {
			t.Errorf("English email HTML contains German %q", de)
		}
	}
	empty := &ChangeReport{From: r.From, To: r.To, Devices: map[string]int{}, EventCounts: map[string]int{}, CVEBySeverity: map[string]int{}, FailedRuns: map[string]int{}}
	if out := empty.EmailHTML(i18n.EN, time.UTC, ""); !strings.Contains(out, "No changes in the period – all quiet.") || !strings.Contains(out, ">none</span>") {
		t.Error("English empty report message missing")
	}

	// PDF and attachment render in English as well
	s, err := r.NotificationExtra(i18n.EN, time.UTC, "")
	if err != nil {
		t.Fatal(err)
	}
	var x plugin.NotificationExtra
	if err := json.Unmarshal([]byte(s), &x); err != nil {
		t.Fatal(err)
	}
	if len(x.Attachments) != 1 || x.Attachments[0].Name != "netscope-report_2026-09-16_2026-09-23.pdf" || !bytes.HasPrefix(x.Attachments[0].Data, []byte("%PDF-")) {
		t.Errorf("English attachment: %+v", len(x.Attachments))
	}
	var buf bytes.Buffer
	devs := []InventoryDevice{{}}
	devs[0].Name, devs[0].IP, devs[0].State = "nas", "192.168.8.5", "unknown"
	if err := InventoryPDF(&buf, devs, i18n.EN, time.UTC); err != nil || !bytes.HasPrefix(buf.Bytes(), []byte("%PDF-")) {
		t.Fatalf("English inventory PDF: %v", err)
	}
}
