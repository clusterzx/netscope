package reports

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/go-pdf/fpdf"
)

// newPDF creates a document with cp1252 translation (German umlauts in core fonts).
func newPDF(orientation, title string) (*fpdf.Fpdf, func(string) string) {
	pdf := fpdf.New(orientation, "mm", "A4", "")
	tr := pdf.UnicodeTranslatorFromDescriptor("")
	pdf.SetTitle(title, true)
	pdf.SetCreator("NetScope", true)
	pdf.SetMargins(12, 12, 12)
	pdf.SetAutoPageBreak(true, 14)
	pdf.SetFooterFunc(func() {
		pdf.SetY(-10)
		pdf.SetFont("Helvetica", "", 7)
		pdf.SetTextColor(120, 120, 120)
		pdf.CellFormat(0, 5, tr(fmt.Sprintf("NetScope – %s – Seite %d", title, pdf.PageNo())), "", 0, "C", false, 0, "")
	})
	pdf.AddPage()
	return pdf, tr
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// InventoryPDF renders the inventory as a table (A4 landscape).
func InventoryPDF(w io.Writer, devs []InventoryDevice, loc *time.Location) error {
	if loc == nil {
		loc = time.Local
	}
	pdf, tr := newPDF("L", "Inventar")
	pdf.SetFont("Helvetica", "B", 15)
	pdf.CellFormat(0, 8, tr("NetScope Inventar"), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	pdf.CellFormat(0, 5, tr(fmt.Sprintf("Stand %s – %d Geräte", time.Now().In(loc).Format("02.01.2006 15:04"), len(devs))), "", 1, "L", false, 0, "")
	pdf.Ln(3)
	cols := []struct {
		title string
		w     float64
		max   int
	}{{"Name", 50, 32}, {"IP", 28, 18}, {"MAC", 32, 17}, {"Hersteller", 42, 26}, {"Typ", 24, 14}, {"OS", 45, 30},
		{"Zustand", 18, 10}, {"Online", 13, 5}, {"Zuletzt gesehen", 21, 16}}
	header := func() {
		pdf.SetFont("Helvetica", "B", 8)
		pdf.SetFillColor(30, 41, 59)
		pdf.SetTextColor(255, 255, 255)
		for _, c := range cols {
			pdf.CellFormat(c.w, 6, tr(c.title), "", 0, "L", true, 0, "")
		}
		pdf.Ln(-1)
		pdf.SetTextColor(0, 0, 0)
		pdf.SetFont("Helvetica", "", 7.5)
	}
	header()
	states := map[string]string{"known": "bekannt", "unknown": "unbekannt", "ignored": "ignoriert"}
	for i, d := range devs {
		if pdf.GetY() > 190 {
			pdf.AddPage()
			header()
		}
		online := "nein"
		if d.Online {
			online = "ja"
		}
		last := ""
		if d.LastSeen != nil {
			last = d.LastSeen.In(loc).Format("02.01.06 15:04")
		}
		vals := []string{d.Name, d.IP, d.MAC, d.Vendor, d.Type, d.OS, states[d.State], online, last}
		fill := i%2 == 1
		pdf.SetFillColor(241, 245, 249)
		for j, c := range cols {
			pdf.CellFormat(c.w, 5, tr(clip(vals[j], c.max)), "", 0, "L", fill, 0, "")
		}
		pdf.Ln(-1)
	}
	return pdf.Output(w)
}

// PDF renders the change report.
func (r *ChangeReport) PDF(w io.Writer, loc *time.Location) error {
	if loc == nil {
		loc = time.Local
	}
	pdf, tr := newPDF("P", "Änderungsbericht")
	h1 := func(s string) {
		pdf.Ln(2)
		pdf.SetFont("Helvetica", "B", 12)
		pdf.SetTextColor(30, 41, 59)
		pdf.CellFormat(0, 7, tr(s), "", 1, "L", false, 0, "")
		pdf.SetTextColor(0, 0, 0)
		pdf.SetFont("Helvetica", "", 9)
	}
	line := func(s string) { pdf.MultiCell(0, 4.6, tr(s), "", "L", false) }
	df := func(s string) string {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return s
		}
		return t.In(loc).Format("02.01.2006 15:04")
	}
	pdf.SetFont("Helvetica", "B", 16)
	pdf.CellFormat(0, 9, tr("NetScope Änderungsbericht"), "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 9)
	line(fmt.Sprintf("Zeitraum %s – %s, erstellt %s", r.From.In(loc).Format("02.01.2006 15:04"), r.To.In(loc).Format("02.01.2006 15:04"),
		r.GeneratedAt.In(loc).Format("02.01.2006 15:04")))
	h1("Überblick")
	line(fmt.Sprintf("Geräte: %d gesamt, %d online, %d neu im Zeitraum, %d unbekannt", r.Devices["total"], r.Devices["online"], r.Devices["new"], r.Devices["unknown"]))
	line(fmt.Sprintf("Offene Events: %d kritisch, %d hoch", r.OpenCritical, r.OpenHigh))
	line(fmt.Sprintf("Aktive Schwachstellen: %d kritisch, %d hoch, %d mittel, %d niedrig", r.CVEBySeverity["critical"], r.CVEBySeverity["high"],
		r.CVEBySeverity["medium"], r.CVEBySeverity["low"]))
	if len(r.NewDevices) > 0 {
		h1(fmt.Sprintf("Neue Geräte (%d)", len(r.NewDevices)))
		for _, d := range r.NewDevices {
			s := fmt.Sprintf("• %s (%s)", d.Name, d.IP)
			if d.Vendor != "" {
				s += " – " + d.Vendor
			}
			line(s + ", seit " + df(d.At))
		}
	}
	if len(r.EventCounts) > 0 {
		h1("Ereignisse nach Typ")
		keys := make([]string, 0, len(r.EventCounts))
		for k := range r.EventCounts {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return r.EventCounts[keys[i]] > r.EventCounts[keys[j]] })
		for _, k := range keys {
			line(fmt.Sprintf("• %s: %d", eventLabel(k), r.EventCounts[k]))
		}
	}
	if len(r.Important) > 0 {
		h1("Wichtige Ereignisse")
		for _, e := range r.Important {
			ack := ""
			if e.Acked {
				ack = " (quittiert)"
			}
			line(fmt.Sprintf("• [%s] %s – %s%s", sevLabel(e.Severity), df(e.TS), e.Title, ack))
		}
	}
	if len(r.NewCVEs) > 0 {
		h1("Neue Schwachstellen")
		for _, c := range r.NewCVEs {
			line(fmt.Sprintf("• %s (CVSS %.1f) auf %s – %s", c.CVE, c.CVSS, c.Device, c.Detail))
		}
	}
	if len(r.ExpiringCerts) > 0 {
		h1("Zertifikate mit baldigem Ablauf")
		for _, c := range r.ExpiringCerts {
			line(fmt.Sprintf("• %s (%s, %s): %d Tage", c.Subject, c.Endpoint, c.Device, c.DaysLeft))
		}
	}
	if len(r.Outages) > 0 {
		h1("Ausfälle")
		for _, o := range r.Outages {
			line(fmt.Sprintf("• %s: %s ab %s, Dauer %s", o.Check, strings.ReplaceAll(o.State, "down", "Ausfall"), df(o.Started), o.Duration))
		}
	}
	if len(r.FailedRuns) > 0 {
		h1("Fehlgeschlagene Plugin-Läufe")
		for id, n := range r.FailedRuns {
			line(fmt.Sprintf("• %s: %d", id, n))
		}
	}
	return pdf.Output(w)
}
