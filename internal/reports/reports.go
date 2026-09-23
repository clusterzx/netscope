// Package reports builds the inventory export (CSV/JSON/PDF) and the change report for a
// period (Markdown/JSON/PDF). The weekly report plugin uses the same change report.
package reports

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
)

// CSVColumns are the fixed inventory export columns (the csv importer reads them back).
var CSVColumns = []string{"id", "name", "hostname", "ip", "ips", "mac", "macs", "vendor", "model", "type", "os", "location",
	"owner", "state", "criticality", "online", "first_seen", "last_seen", "tags", "notes"}

// InventoryDevice is one exported device.
type InventoryDevice struct {
	inventory.DeviceRow
	Notes string `json:"notes"`
}

// Inventory loads all devices (optionally filtered by a query) for export.
func Inventory(ctx context.Context, inv *inventory.Store, query string) ([]InventoryDevice, error) {
	res, err := inv.List(ctx, inventory.ListOptions{Query: query, Sort: "ip", Limit: 100000, WithPorts: true})
	if err != nil {
		return nil, err
	}
	notes := map[int64]string{}
	rows, err := inv.DB().R.QueryContext(ctx, "SELECT id, notes FROM devices WHERE notes <> ''")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			id int64
			n  string
		)
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		notes[id] = n
	}
	rows.Close()
	out := make([]InventoryDevice, 0, len(res.Items))
	for _, d := range res.Items {
		out = append(out, InventoryDevice{DeviceRow: d, Notes: notes[d.ID]})
	}
	return out, nil
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// WriteCSV writes the inventory as CSV (custom fields as cf.<key> columns).
func WriteCSV(w io.Writer, devs []InventoryDevice, customKeys []string) error {
	cw := csv.NewWriter(w)
	header := append([]string{}, CSVColumns...)
	for _, k := range customKeys {
		header = append(header, "cf."+k)
	}
	if err := cw.Write(header); err != nil {
		return err
	}
	for _, d := range devs {
		online := "false"
		if d.Online {
			online = "true"
		}
		rec := []string{strconv.FormatInt(d.ID, 10), d.DisplayName, d.Hostname, d.IP, strings.Join(d.IPs, " "), d.MAC,
			strings.Join(d.MACs, " "), d.Vendor, d.Model, d.Type, d.OS, d.Location, d.Owner, d.State, d.Criticality, online,
			fmtTime(d.FirstSeen), fmtTime(d.LastSeen), strings.Join(d.Tags, ";"), d.Notes}
		for _, k := range customKeys {
			v := ""
			if x, ok := d.Custom[k]; ok && x != nil {
				v = fmt.Sprint(x)
			}
			rec = append(rec, v)
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteJSON writes the inventory as JSON.
func WriteJSON(w io.Writer, devs []InventoryDevice) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(map[string]any{"exportedAt": time.Now(), "count": len(devs), "devices": devs})
}

// ---------------------------------------------------------------- change report

// DeviceLine is a device mentioned in a report.
type DeviceLine struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	IP     string `json:"ip"`
	Vendor string `json:"vendor,omitempty"`
	At     string `json:"at,omitempty"`
}

// EventLine is an event mentioned in a report.
type EventLine struct {
	ID       int64  `json:"id"`
	TS       string `json:"ts"`
	Type     string `json:"type"`
	Severity string `json:"severity"`
	Title    string `json:"title"`
	Device   string `json:"device,omitempty"`
	Acked    bool   `json:"acked"`
}

// CertLine is an expiring certificate.
type CertLine struct {
	Device   string `json:"device"`
	Endpoint string `json:"endpoint"`
	Subject  string `json:"subject"`
	NotAfter string `json:"notAfter"`
	DaysLeft int    `json:"daysLeft"`
}

// OutageLine is a health outage.
type OutageLine struct {
	Check    string `json:"check"`
	State    string `json:"state"`
	Started  string `json:"started"`
	Seconds  int64  `json:"seconds"` // duration so far for ongoing outages
	Ongoing  bool   `json:"ongoing"`
	Duration string `json:"duration"` // human-readable, e.g. "2 h 5 min (andauernd)"
}

// humanDuration formats a duration for reports ("3 d 4 h", "2 h 5 min", "45 s").
func humanDuration(d time.Duration) string {
	d = d.Round(time.Second)
	days, hours, mins := int(d.Hours())/24, int(d.Hours())%24, int(d.Minutes())%60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%d d %d h", days, hours)
	case days > 0:
		return fmt.Sprintf("%d d", days)
	case hours > 0 && mins > 0:
		return fmt.Sprintf("%d h %d min", hours, mins)
	case hours > 0:
		return fmt.Sprintf("%d h", hours)
	case mins > 0:
		return fmt.Sprintf("%d min", mins)
	}
	return fmt.Sprintf("%d s", int(d.Seconds()))
}

// CVELine is a newly found vulnerability.
type CVELine struct {
	CVE    string  `json:"cve"`
	CVSS   float64 `json:"cvss"`
	Device string  `json:"device"`
	Detail string  `json:"detail"`
}

// ChangeReport summarizes a period.
type ChangeReport struct {
	From          time.Time      `json:"from"`
	To            time.Time      `json:"to"`
	GeneratedAt   time.Time      `json:"generatedAt"`
	Devices       map[string]int `json:"devices"` // total, online, new, unknown
	NewDevices    []DeviceLine   `json:"newDevices"`
	EventCounts   map[string]int `json:"eventCounts"` // by type
	Important     []EventLine    `json:"important"`   // high/critical events of the period
	OpenCritical  int            `json:"openCritical"`
	OpenHigh      int            `json:"openHigh"`
	CVEBySeverity map[string]int `json:"cveBySeverity"`
	NewCVEs       []CVELine      `json:"newCves"`
	ExpiringCerts []CertLine     `json:"expiringCerts"`
	Outages       []OutageLine   `json:"outages"`
	FailedRuns    map[string]int `json:"failedRuns"` // by plugin
}

// BuildChangeReport collects the data of a period.
func BuildChangeReport(ctx context.Context, d *db.DB, from, to time.Time) (*ChangeReport, error) {
	r := &ChangeReport{From: from, To: to, GeneratedAt: time.Now(), Devices: map[string]int{}, EventCounts: map[string]int{},
		CVEBySeverity: map[string]int{}, FailedRuns: map[string]int{}, NewDevices: []DeviceLine{}, Important: []EventLine{},
		NewCVEs: []CVELine{}, ExpiringCerts: []CertLine{}, Outages: []OutageLine{}}
	f, t := from.UnixMilli(), to.UnixMilli()
	q := d.R
	var total, online, unknown, nw int
	if err := q.QueryRowContext(ctx, `SELECT COUNT(*), IFNULL(SUM(online), 0), IFNULL(SUM(state = 'unknown'), 0),
		IFNULL(SUM(first_seen BETWEEN ? AND ?), 0) FROM devices WHERE state <> 'ignored'`, f, t).Scan(&total, &online, &unknown, &nw); err != nil {
		return nil, err
	}
	r.Devices["total"], r.Devices["online"], r.Devices["unknown"], r.Devices["new"] = total, online, unknown, nw
	name := `COALESCE(NULLIF(d.display_name, ''), NULLIF(d.hostname, ''), NULLIF(d.primary_ip, ''), d.primary_mac)`
	rows, err := q.QueryContext(ctx, `SELECT d.id, `+name+`, d.primary_ip, d.vendor, d.first_seen FROM devices d
		WHERE d.first_seen BETWEEN ? AND ? AND d.state <> 'ignored' ORDER BY d.first_seen LIMIT 200`, f, t)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			l  DeviceLine
			fs int64
		)
		if err := rows.Scan(&l.ID, &l.Name, &l.IP, &l.Vendor, &fs); err != nil {
			rows.Close()
			return nil, err
		}
		l.At = db.Time(fs).Format(time.RFC3339)
		r.NewDevices = append(r.NewDevices, l)
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, "SELECT type, COUNT(*) FROM events WHERE ts BETWEEN ? AND ? GROUP BY type ORDER BY COUNT(*) DESC", f, t)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			typ string
			n   int
		)
		if err := rows.Scan(&typ, &n); err != nil {
			rows.Close()
			return nil, err
		}
		r.EventCounts[typ] = n
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT e.id, e.ts, e.type, e.severity, e.title, IFNULL(`+name+`, ''), e.acked_at IS NOT NULL
		FROM events e LEFT JOIN devices d ON d.id = e.device_id WHERE e.ts BETWEEN ? AND ? AND e.severity IN ('high','critical')
		ORDER BY e.ts DESC LIMIT 100`, f, t)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			l  EventLine
			ts int64
		)
		if err := rows.Scan(&l.ID, &ts, &l.Type, &l.Severity, &l.Title, &l.Device, &l.Acked); err != nil {
			rows.Close()
			return nil, err
		}
		l.TS = db.Time(ts).Format(time.RFC3339)
		r.Important = append(r.Important, l)
	}
	rows.Close()
	if err := q.QueryRowContext(ctx, `SELECT IFNULL(SUM(severity = 'critical'), 0), IFNULL(SUM(severity = 'high'), 0) FROM events WHERE acked_at IS NULL`).
		Scan(&r.OpenCritical, &r.OpenHigh); err != nil {
		return nil, err
	}
	rows, err = q.QueryContext(ctx, `SELECT c.cvss_score FROM device_cves c WHERE c.gone_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM cve_ignores i WHERE i.device_id = c.device_id AND i.cve_id = c.cve_id)`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var score *float64
		if err := rows.Scan(&score); err != nil {
			rows.Close()
			return nil, err
		}
		s := 0.0
		if score != nil {
			s = *score
		}
		r.CVEBySeverity[string(plugin.SeverityFromCVSS(s))]++
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT c.cve_id, IFNULL(c.cvss_score, 0), `+name+`, c.product || ' ' || c.version FROM device_cves c
		JOIN devices d ON d.id = c.device_id WHERE c.first_seen BETWEEN ? AND ? AND c.gone_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM cve_ignores i WHERE i.device_id = c.device_id AND i.cve_id = c.cve_id)
		ORDER BY c.cvss_score DESC LIMIT 50`, f, t)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var l CVELine
		if err := rows.Scan(&l.CVE, &l.CVSS, &l.Device, &l.Detail); err != nil {
			rows.Close()
			return nil, err
		}
		l.Detail = strings.TrimSpace(l.Detail)
		r.NewCVEs = append(r.NewCVEs, l)
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT `+name+`, x.ip || ':' || x.port, x.subject_cn, x.not_after FROM certificates x
		JOIN devices d ON d.id = x.device_id WHERE x.gone_at IS NULL AND x.not_after < ? ORDER BY x.not_after LIMIT 100`,
		to.Add(30*24*time.Hour).UnixMilli())
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			l  CertLine
			na int64
		)
		if err := rows.Scan(&l.Device, &l.Endpoint, &l.Subject, &na); err != nil {
			rows.Close()
			return nil, err
		}
		l.NotAfter = db.Time(na).Format(time.RFC3339)
		l.DaysLeft = int(time.Until(db.Time(na)).Hours() / 24)
		r.ExpiringCerts = append(r.ExpiringCerts, l)
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT c.name, o.state, o.started_at, o.ended_at FROM health_outages o JOIN health_checks c ON c.id = o.check_id
		WHERE o.started_at <= ? AND (o.ended_at IS NULL OR o.ended_at >= ?) ORDER BY o.started_at DESC LIMIT 100`, t, f)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			l     OutageLine
			start int64
			end   *int64
		)
		if err := rows.Scan(&l.Check, &l.State, &start, &end); err != nil {
			rows.Close()
			return nil, err
		}
		stop := time.Now()
		if end != nil {
			stop = time.UnixMilli(*end)
		}
		l.Started = db.Time(start).Format(time.RFC3339)
		d := stop.Sub(db.Time(start))
		l.Seconds, l.Ongoing = int64(d.Seconds()), end == nil
		l.Duration = humanDuration(d)
		if l.Ongoing {
			l.Duration += " (andauernd)"
		}
		r.Outages = append(r.Outages, l)
	}
	rows.Close()
	rows, err = q.QueryContext(ctx, `SELECT plugin_id, COUNT(*) FROM runs WHERE status IN ('failed','timeout') AND created_at BETWEEN ? AND ?
		GROUP BY plugin_id`, f, t)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var (
			id string
			n  int
		)
		if err := rows.Scan(&id, &n); err != nil {
			rows.Close()
			return nil, err
		}
		r.FailedRuns[id] = n
	}
	rows.Close()
	return r, nil
}

func sevLabel(s string) string { return plugin.Severity(s).Label() }

func eventLabel(t string) string {
	if spec, ok := plugin.LookupEvent(t); ok {
		return spec.Label
	}
	return t
}

// Markdown renders the report as Markdown (German).
func (r *ChangeReport) Markdown(loc *time.Location, baseURL string) string {
	if loc == nil {
		loc = time.Local
	}
	df := func(s string) string {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return s
		}
		return t.In(loc).Format("02.01.2006 15:04")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Zeitraum:** %s – %s\n\n", r.From.In(loc).Format("02.01.2006 15:04"), r.To.In(loc).Format("02.01.2006 15:04"))
	fmt.Fprintf(&b, "**Geräte:** %d gesamt, %d online, %d neu, %d unbekannt\n", r.Devices["total"], r.Devices["online"], r.Devices["new"], r.Devices["unknown"])
	fmt.Fprintf(&b, "**Offene Events:** %d kritisch, %d hoch\n", r.OpenCritical, r.OpenHigh)
	fmt.Fprintf(&b, "**Schwachstellen (aktiv):** %d kritisch, %d hoch, %d mittel, %d niedrig\n\n",
		r.CVEBySeverity["critical"], r.CVEBySeverity["high"], r.CVEBySeverity["medium"], r.CVEBySeverity["low"])
	if len(r.NewDevices) > 0 {
		b.WriteString("## Neue Geräte\n")
		for _, d := range r.NewDevices {
			fmt.Fprintf(&b, "- %s (%s)", d.Name, d.IP)
			if d.Vendor != "" {
				fmt.Fprintf(&b, " – %s", d.Vendor)
			}
			fmt.Fprintf(&b, ", seit %s\n", df(d.At))
		}
		b.WriteString("\n")
	}
	if len(r.EventCounts) > 0 {
		b.WriteString("## Ereignisse\n")
		type kv struct {
			k string
			v int
		}
		var list []kv
		for k, v := range r.EventCounts {
			list = append(list, kv{k, v})
		}
		sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v })
		for _, e := range list {
			fmt.Fprintf(&b, "- %s: %d\n", eventLabel(e.k), e.v)
		}
		b.WriteString("\n")
	}
	if len(r.Important) > 0 {
		b.WriteString("## Wichtige Ereignisse (hoch/kritisch)\n")
		for i, e := range r.Important {
			if i >= 25 {
				fmt.Fprintf(&b, "- … und %d weitere\n", len(r.Important)-25)
				break
			}
			ack := ""
			if e.Acked {
				ack = " ✓"
			}
			fmt.Fprintf(&b, "- [%s] %s – %s%s\n", sevLabel(e.Severity), df(e.TS), e.Title, ack)
		}
		b.WriteString("\n")
	}
	if len(r.NewCVEs) > 0 {
		b.WriteString("## Neue Schwachstellen\n")
		for _, c := range r.NewCVEs {
			fmt.Fprintf(&b, "- %s (CVSS %.1f) auf %s – %s\n", c.CVE, c.CVSS, c.Device, c.Detail)
		}
		b.WriteString("\n")
	}
	if len(r.ExpiringCerts) > 0 {
		b.WriteString("## Zertifikate mit baldigem Ablauf\n")
		for _, c := range r.ExpiringCerts {
			fmt.Fprintf(&b, "- %s (%s, %s): %d Tage\n", c.Subject, c.Endpoint, c.Device, c.DaysLeft)
		}
		b.WriteString("\n")
	}
	if len(r.Outages) > 0 {
		b.WriteString("## Ausfälle\n")
		for _, o := range r.Outages {
			state := "Ausfall"
			if o.State == "degraded" {
				state = "Beeinträchtigt"
			}
			fmt.Fprintf(&b, "- %s: %s ab %s, Dauer %s\n", o.Check, state, df(o.Started), o.Duration)
		}
		b.WriteString("\n")
	}
	if len(r.FailedRuns) > 0 {
		b.WriteString("## Fehlgeschlagene Plugin-Läufe\n")
		ids := make([]string, 0, len(r.FailedRuns))
		for id := range r.FailedRuns {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			fmt.Fprintf(&b, "- %s: %d\n", id, r.FailedRuns[id])
		}
		b.WriteString("\n")
	}
	if baseURL != "" {
		fmt.Fprintf(&b, "Details: %s\n", baseURL)
	}
	return strings.TrimSpace(b.String())
}

// ToBytes renders a report in the given format.
func (r *ChangeReport) ToBytes(format string, loc *time.Location, baseURL string) ([]byte, string, error) {
	switch format {
	case "json":
		b, err := json.MarshalIndent(r, "", "  ")
		return b, "application/json", err
	case "md", "markdown":
		return []byte("# NetScope Änderungsbericht\n\n" + r.Markdown(loc, baseURL) + "\n"), "text/markdown; charset=utf-8", nil
	case "pdf":
		var buf bytes.Buffer
		err := r.PDF(&buf, loc)
		return buf.Bytes(), "application/pdf", err
	}
	return nil, "", fmt.Errorf("unbekanntes Format %q", format)
}
