package reports

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"time"

	"github.com/go-pdf/fpdf"

	"netscope/internal/plugin"
)

// PDF output in the NetScope design: navy header band with the radar logo, key figures,
// tables with severity chips, "Seite x von y" footer. Core fonts with cp1252 translation
// (German umlauts, "–", "…", "•"); characters outside cp1252 must not be used.

type rgb struct{ r, g, b int }

func hex(s string) rgb {
	v, _ := strconv.ParseUint(s[1:], 16, 32)
	return rgb{int(v >> 16), int(v >> 8 & 0xff), int(v & 0xff)}
}

var (
	pNavy    = hex("#0f172a")
	pSky     = hex("#38bdf8")
	pAmber   = hex("#f59e0b")
	pFg      = hex(cFg)
	pMuted   = hex(cMuted)
	pSubtle  = hex(cSubtle)
	pBorder  = hex(cBorder)
	pLine    = hex(cLine)
	pSurface = hex(cSurface)
	pZebra   = hex("#fbfcfd")
	pAccent  = hex(cAccent)
	pOK      = hex(cOK)
	pViolet  = hex(cViolet)
)

func pTone(t tone) (fg, bg rgb) { return hex(t.fg), hex(t.bg) }

// doc wraps fpdf with the NetScope layout helpers.
type doc struct {
	pdf     *fpdf.Fpdf
	tr      func(string) string
	w, h    float64 // page size
	m       float64 // side margin
	bottom  float64 // lowest y for content
	title   string
	period  string
	created string
}

func newDoc(orientation, title, period string, created time.Time, loc *time.Location) *doc {
	pdf := fpdf.New(orientation, "mm", "A4", "")
	d := &doc{pdf: pdf, tr: pdf.UnicodeTranslatorFromDescriptor(""), m: 14, title: title, period: period,
		created: created.In(loc).Format("02.01.2006 15:04")}
	d.w, d.h = pdf.GetPageSize()
	d.bottom = d.h - 18
	pdf.SetTitle("NetScope "+title, true)
	pdf.SetCreator("NetScope", true)
	pdf.SetMargins(d.m, 16, d.m)
	pdf.SetAutoPageBreak(false, 0)
	pdf.AliasNbPages("{nb}")
	pdf.SetHeaderFunc(d.header)
	pdf.SetFooterFunc(d.footer)
	pdf.AddPage()
	return d
}

func (d *doc) fill(c rgb)  { d.pdf.SetFillColor(c.r, c.g, c.b) }
func (d *doc) draw(c rgb)  { d.pdf.SetDrawColor(c.r, c.g, c.b) }
func (d *doc) color(c rgb) { d.pdf.SetTextColor(c.r, c.g, c.b) }

func (d *doc) font(style string, size float64) { d.pdf.SetFont("Helvetica", style, size) }

// text writes s at x/baseline y; align "L", "R" or "C" relative to x (width w for R/C).
func (d *doc) text(x, y, w float64, s, align string) {
	s = d.tr(s)
	switch align {
	case "R":
		x = x + w - d.pdf.GetStringWidth(s)
	case "C":
		x = x + (w-d.pdf.GetStringWidth(s))/2
	}
	d.pdf.Text(x, y, s)
}

// fit shortens s (already in the current font) to width w with "…".
func (d *doc) fit(s string, w float64) string {
	if d.pdf.GetStringWidth(d.tr(s)) <= w {
		return s
	}
	r := []rune(s)
	for len(r) > 1 && d.pdf.GetStringWidth(d.tr(string(r)+"…")) > w {
		r = r[:len(r)-1]
	}
	return string(r) + "…"
}

// logo draws the radar logo (size = edge length) at x/y.
func (d *doc) logo(x, y, size float64) {
	k := size / 64
	d.fill(hex("#111c33"))
	d.draw(pSky)
	d.pdf.SetLineWidth(0.25)
	d.pdf.RoundedRect(x, y, size, size, 14*k, "1234", "FD")
	cx, cy := x+32*k, y+32*k
	d.pdf.SetLineWidth(4 * k)
	d.pdf.Circle(cx, cy, 18*k, "D")
	d.pdf.SetLineWidth(3 * k)
	d.pdf.SetAlpha(0.7, "Normal")
	d.pdf.Circle(cx, cy, 8*k, "D")
	d.pdf.SetAlpha(1, "Normal")
	d.draw(pAmber)
	d.fill(pAmber)
	d.pdf.SetLineWidth(4 * k)
	d.pdf.SetLineCapStyle("round")
	d.pdf.Line(cx, cy, x+46*k, y+18*k)
	d.pdf.Circle(x+46*k, y+18*k, 4*k, "F")
	d.pdf.SetLineCapStyle("butt")
}

func (d *doc) header() {
	if d.pdf.PageNo() == 1 {
		d.fill(pNavy)
		d.pdf.Rect(0, 0, d.w, 36, "F")
		d.fill(pSky)
		d.pdf.Rect(0, 36, d.w, 0.9, "F")
		d.logo(d.m, 9, 18)
		d.color(rgb{255, 255, 255})
		d.font("B", 19)
		d.text(d.m+23, 17.5, 0, "NetScope", "L")
		d.color(pSky)
		d.font("B", 8.5)
		d.text(d.m+23.3, 24.5, 0, d.title, "L")
		d.color(rgb{255, 255, 255})
		d.font("B", 11)
		d.text(d.w/2, 17, d.w/2-d.m, d.period, "R")
		d.color(hex("#94a3b8"))
		d.font("", 8)
		d.text(d.w/2, 23.5, d.w/2-d.m, "erstellt "+d.created, "R")
		d.pdf.SetY(44)
		return
	}
	// running header on the following pages
	d.logo(d.m, 8, 6)
	d.color(pFg)
	d.font("B", 9)
	d.text(d.m+8.5, 12.4, 0, "NetScope · "+d.title, "L")
	d.color(pSubtle)
	d.font("", 8)
	d.text(d.w/2, 12.4, d.w/2-d.m, d.period, "R")
	d.draw(pBorder)
	d.pdf.SetLineWidth(0.2)
	d.pdf.Line(d.m, 16.5, d.w-d.m, 16.5)
	d.pdf.SetY(22)
}

func (d *doc) footer() {
	y := d.h - 11
	d.draw(pBorder)
	d.pdf.SetLineWidth(0.2)
	d.pdf.Line(d.m, y-4, d.w-d.m, y-4)
	d.color(pSubtle)
	d.font("", 7.5)
	d.text(d.m, y, 0, "NetScope · "+d.title+" · "+d.period, "L")
	d.text(d.w/2, y, d.w/2-d.m, fmt.Sprintf("Seite %d von {nb}", d.pdf.PageNo()), "R")
}

// need starts a new page unless h mm are left.
func (d *doc) need(h float64) {
	if d.pdf.GetY()+h > d.bottom {
		d.pdf.AddPage()
	}
}

// chip draws a rounded label at x/y (top) and returns its width.
func (d *doc) chip(x, y float64, label string, t tone, size float64) float64 {
	fg, bg := pTone(t)
	d.font("B", size)
	w := d.pdf.GetStringWidth(d.tr(label)) + 4
	h := size*0.353 + 2.2
	d.fill(bg)
	d.pdf.RoundedRect(x, y, w, h, h/2, "1234", "F")
	d.color(fg)
	d.text(x+2, y+h/2+size*0.353*0.36, 0, label, "L")
	return w
}

// card draws a key figure tile.
func (d *doc) card(x, y, w, h float64, label, value, sub string, valueColor rgb) {
	d.fill(pSurface)
	d.draw(pBorder)
	d.pdf.SetLineWidth(0.25)
	d.pdf.RoundedRect(x, y, w, h, 2.5, "1234", "FD")
	d.color(pSubtle)
	d.font("B", 7)
	d.text(x+4, y+6, 0, label, "L")
	d.color(valueColor)
	d.font("B", 20)
	d.text(x+4, y+15.5, 0, value, "L")
	d.color(pMuted)
	d.font("", 7.5)
	d.text(x+4, y+20.5, 0, d.fit(sub, w-8), "L")
}

// section writes a heading with an accent bar.
func (d *doc) section(title string, count int) {
	d.need(24)
	y := d.pdf.GetY() + 4
	d.fill(pAccent)
	d.pdf.RoundedRect(d.m, y, 1.3, 6, 0.6, "1234", "F")
	d.color(pFg)
	d.font("B", 12)
	d.text(d.m+4, y+4.9, 0, title, "L")
	if count > 0 {
		tw := d.pdf.GetStringWidth(d.tr(title))
		d.color(pSubtle)
		d.font("", 10)
		d.text(d.m+4+tw+2, y+4.9, 0, fmt.Sprintf("(%d)", count), "L")
	}
	d.pdf.SetY(y + 9)
}

// cell kinds of a table row
type pcell struct {
	text  string
	mono  bool
	bold  bool
	muted bool
	chip  *tone
	bar   float64 // 0..1 = horizontal bar
}

type pcol struct {
	title string
	w     float64
	align string // "L" (default), "R", "C"
}

// table draws a table with a repeated header after page breaks.
func (d *doc) table(cols []pcol, rows [][]pcell) {
	const hh, rh = 7.0, 6.6
	header := func() {
		y := d.pdf.GetY()
		d.fill(pSurface)
		d.pdf.Rect(d.m, y, d.w-2*d.m, hh, "F")
		d.draw(pBorder)
		d.pdf.SetLineWidth(0.2)
		d.pdf.Line(d.m, y+hh, d.w-d.m, y+hh)
		d.color(pSubtle)
		d.font("B", 6.8)
		x := d.m
		for _, c := range cols {
			d.text(x+2, y+4.6, c.w-4, d.fit(upper(c.title), c.w-4), alignOr(c.align))
			x += c.w
		}
		d.pdf.SetY(y + hh)
	}
	d.need(hh + rh*2)
	header()
	for i, r := range rows {
		if d.pdf.GetY()+rh > d.bottom {
			d.pdf.AddPage()
			header()
		}
		y := d.pdf.GetY()
		if i%2 == 1 {
			d.fill(pZebra)
			d.pdf.Rect(d.m, y, d.w-2*d.m, rh, "F")
		}
		d.draw(pLine)
		d.pdf.SetLineWidth(0.15)
		d.pdf.Line(d.m, y+rh, d.w-d.m, y+rh)
		x := d.m
		for j, c := range r {
			col := cols[j]
			inner := col.w - 4
			switch {
			case c.chip != nil:
				d.font("B", 7)
				cw := d.pdf.GetStringWidth(d.tr(c.text)) + 4
				cx := x + 2
				switch col.align {
				case "R":
					cx = x + col.w - 2 - cw
				case "C":
					cx = x + (col.w-cw)/2
				}
				d.chip(cx, y+1.35, c.text, *c.chip, 7)
			case c.bar > 0:
				d.fill(pLine)
				d.pdf.RoundedRect(x+2, y+2.3, inner, 2, 1, "1234", "F")
				d.fill(pAccent)
				d.pdf.RoundedRect(x+2, y+2.3, max(2, inner*c.bar), 2, 1, "1234", "F")
			default:
				switch {
				case c.mono:
					d.pdf.SetFont("Courier", "", 7.6)
					d.color(pMuted)
				case c.bold:
					d.font("B", 8)
					d.color(pFg)
				case c.muted:
					d.font("", 8)
					d.color(pSubtle)
				default:
					d.font("", 8)
					d.color(pFg)
				}
				d.text(x+2, y+4.35, inner, d.fit(c.text, inner), alignOr(col.align))
			}
			x += col.w
		}
		d.pdf.SetY(y + rh)
	}
}

func alignOr(a string) string {
	if a == "" {
		return "L"
	}
	return a
}

func upper(s string) string {
	b := []rune(s)
	for i, r := range b {
		switch r {
		case 'ä':
			b[i] = 'Ä'
		case 'ö':
			b[i] = 'Ö'
		case 'ü':
			b[i] = 'Ü'
		default:
			if r >= 'a' && r <= 'z' {
				b[i] = r - 32
			}
		}
	}
	return string(b)
}

func tonePtr(t tone) *tone { return &t }

// ---------------------------------------------------------------- change report

// PDF renders the change report.
func (r *ChangeReport) PDF(w io.Writer, loc *time.Location) error {
	if loc == nil {
		loc = time.Local
	}
	period := r.From.In(loc).Format("02.01.2006") + " – " + r.To.In(loc).Format("02.01.2006")
	d := newDoc("P", "Änderungsbericht", period, r.GeneratedAt, loc)
	pdf := d.pdf
	df := func(s string) string {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return s
		}
		return t.In(loc).Format("02.01.2006 15:04")
	}

	// key figures
	cw := (d.w - 2*d.m - 3*4) / 4
	y := pdf.GetY()
	total, online := r.Devices["total"], r.Devices["online"]
	pct := "–"
	if total > 0 {
		pct = fmt.Sprintf("%d %% erreichbar", online*100/total)
	}
	d.card(d.m, y, cw, 24, "GERÄTE", strconv.Itoa(total), "im Inventar", pFg)
	d.card(d.m+(cw+4), y, cw, 24, "ONLINE", strconv.Itoa(online), pct, pOK)
	d.card(d.m+2*(cw+4), y, cw, 24, "NEU", strconv.Itoa(r.Devices["new"]), "im Zeitraum", pAccent)
	d.card(d.m+3*(cw+4), y, cw, 24, "UNBEKANNT", strconv.Itoa(r.Devices["unknown"]), "nicht bestätigt", pViolet)
	y += 28

	// open events and vulnerabilities
	pw := (d.w - 2*d.m - 4) / 2
	panel := func(x float64, label string, counts map[string]int, keys []string, zero string) {
		d.fill(rgb{255, 255, 255})
		d.draw(pBorder)
		pdf.SetLineWidth(0.25)
		pdf.RoundedRect(x, y, pw, 16, 2.5, "1234", "FD")
		d.color(pSubtle)
		d.font("B", 7)
		d.text(x+4, y+5.8, 0, label, "L")
		cx := x + 4
		shown := false
		for _, k := range keys {
			if n := counts[k]; n > 0 {
				cx += d.chip(cx, y+8.2, fmt.Sprintf("%d %s", n, lower(sevLabel(k))), sevTone(k), 7.5) + 2
				shown = true
			}
		}
		if !shown {
			d.chip(cx, y+8.2, zero, toneOK, 7.5)
		}
	}
	panel(d.m, "OFFENE EVENTS", map[string]int{"critical": r.OpenCritical, "high": r.OpenHigh}, []string{"critical", "high"}, "keine kritischen")
	panel(d.m+pw+4, "AKTIVE SCHWACHSTELLEN", r.CVEBySeverity, []string{"critical", "high", "medium", "low"}, "keine")
	pdf.SetY(y + 18)

	empty := len(r.NewDevices) == 0 && len(r.EventCounts) == 0 && len(r.Important) == 0 && len(r.NewCVEs) == 0 &&
		len(r.ExpiringCerts) == 0 && len(r.Outages) == 0 && len(r.FailedRuns) == 0
	if empty {
		y := pdf.GetY() + 6
		fg, bg := pTone(toneOK)
		d.fill(bg)
		pdf.RoundedRect(d.m, y, d.w-2*d.m, 12, 2.5, "1234", "F")
		d.color(fg)
		d.font("B", 10)
		d.text(d.m+5, y+7.6, 0, "Keine Änderungen im Zeitraum – alles ruhig.", "L")
		pdf.SetY(y + 14)
	}

	if len(r.Important) > 0 {
		d.section("Wichtige Ereignisse", len(r.Important))
		var rows [][]pcell
		for _, e := range r.Important {
			title := e.Title
			if e.Device != "" && !containsFold(e.Title, e.Device) {
				title += " – " + e.Device
			}
			state := pcell{text: "offen", muted: true}
			if e.Acked {
				state = pcell{text: "quittiert", chip: tonePtr(toneOK)}
			}
			rows = append(rows, []pcell{{text: sevLabel(e.Severity), chip: tonePtr(sevTone(e.Severity))}, {text: df(e.TS), mono: true},
				{text: title}, state})
		}
		d.table([]pcol{{"Schwere", 22, ""}, {"Zeit", 32, ""}, {"Ereignis", 104, ""}, {"Status", 24, "R"}}, rows)
	}

	if len(r.NewDevices) > 0 {
		d.section("Neue Geräte", len(r.NewDevices))
		var rows [][]pcell
		for _, dv := range r.NewDevices {
			vendor := pcell{text: dv.Vendor}
			if dv.Vendor == "" {
				vendor = pcell{text: "–", muted: true}
			}
			rows = append(rows, []pcell{{text: dv.Name, bold: true}, {text: dv.IP, mono: true}, vendor, {text: df(dv.At), mono: true}})
		}
		d.table([]pcol{{"Gerät", 60, ""}, {"IP", 30, ""}, {"Hersteller", 60, ""}, {"Erstmals", 32, ""}}, rows)
	}

	if len(r.NewCVEs) > 0 {
		d.section("Neue Schwachstellen", len(r.NewCVEs))
		var rows [][]pcell
		for _, c := range r.NewCVEs {
			rows = append(rows, []pcell{{text: c.CVE, mono: true}, {text: fmt.Sprintf("%.1f", c.CVSS), chip: tonePtr(sevTone(string(plugin.SeverityFromCVSS(c.CVSS))))},
				{text: c.Device}, {text: c.Detail, muted: true}})
		}
		d.table([]pcol{{"CVE", 36, ""}, {"CVSS", 16, "C"}, {"Gerät", 60, ""}, {"Produkt", 70, ""}}, rows)
	}

	if len(r.ExpiringCerts) > 0 {
		d.section("Ablaufende Zertifikate", len(r.ExpiringCerts))
		var rows [][]pcell
		for _, c := range r.ExpiringCerts {
			left := pcell{text: fmt.Sprintf("%d Tage", c.DaysLeft), chip: tonePtr(certTone(c.DaysLeft))}
			if c.DaysLeft < 0 {
				left = pcell{text: "abgelaufen", chip: tonePtr(toneCritical)}
			}
			rows = append(rows, []pcell{{text: c.Subject, bold: true}, {text: c.Endpoint, mono: true}, {text: c.Device}, left})
		}
		d.table([]pcol{{"Zertifikat", 58, ""}, {"Endpunkt", 42, ""}, {"Gerät", 56, ""}, {"Restlaufzeit", 26, "R"}}, rows)
	}

	if len(r.Outages) > 0 {
		d.section("Ausfälle", len(r.Outages))
		var rows [][]pcell
		for _, o := range r.Outages {
			state := pcell{text: "Ausfall", chip: tonePtr(toneCritical)}
			if o.State == "degraded" {
				state = pcell{text: "Beeinträchtigt", chip: tonePtr(toneMedium)}
			}
			dur := humanDuration(time.Duration(o.Seconds) * time.Second)
			if o.Ongoing {
				dur += " (andauernd)"
			}
			rows = append(rows, []pcell{{text: o.Check, bold: true}, state, {text: df(o.Started), mono: true}, {text: dur}})
		}
		d.table([]pcol{{"Check", 70, ""}, {"Zustand", 32, ""}, {"Beginn", 36, ""}, {"Dauer", 44, "R"}}, rows)
	}

	if len(r.EventCounts) > 0 {
		d.section("Ereignisse nach Typ", 0)
		keys := make([]string, 0, len(r.EventCounts))
		maxV := 1
		for k, v := range r.EventCounts {
			keys = append(keys, k)
			maxV = max(maxV, v)
		}
		sort.Slice(keys, func(i, j int) bool {
			a, b := r.EventCounts[keys[i]], r.EventCounts[keys[j]]
			return a > b || (a == b && keys[i] < keys[j])
		})
		var rows [][]pcell
		for _, k := range keys {
			v := r.EventCounts[k]
			rows = append(rows, []pcell{{text: eventLabel(k)}, {bar: float64(v) / float64(maxV)}, {text: strconv.Itoa(v), bold: true}})
		}
		d.table([]pcol{{"Ereignis", 72, ""}, {"Verteilung", 90, ""}, {"Anzahl", 20, "R"}}, rows)
	}

	if len(r.FailedRuns) > 0 {
		d.section("Fehlgeschlagene Plugin-Läufe", 0)
		ids := make([]string, 0, len(r.FailedRuns))
		for id := range r.FailedRuns {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		var rows [][]pcell
		for _, id := range ids {
			name := id
			if p, ok := plugin.Get(id); ok {
				name = p.Info().Name
			}
			rows = append(rows, []pcell{{text: name, bold: true}, {text: id, mono: true},
				{text: fmt.Sprintf("%d×", r.FailedRuns[id]), chip: tonePtr(toneHigh)}})
		}
		d.table([]pcol{{"Plugin", 90, ""}, {"ID", 62, ""}, {"Fehlschläge", 30, "R"}}, rows)
	}
	return pdf.Output(w)
}

func lower(s string) string {
	b := []rune(s)
	for i, r := range b {
		switch {
		case r >= 'A' && r <= 'Z':
			b[i] = r + 32
		case r == 'Ä':
			b[i] = 'ä'
		case r == 'Ö':
			b[i] = 'ö'
		case r == 'Ü':
			b[i] = 'ü'
		}
	}
	return string(b)
}

func containsFold(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && indexFold(s, sub) >= 0
}

func indexFold(s, sub string) int {
	ls, lsub := lower(s), lower(sub)
	for i := 0; i+len(lsub) <= len(ls); i++ {
		if ls[i:i+len(lsub)] == lsub {
			return i
		}
	}
	return -1
}

// ---------------------------------------------------------------- inventory

// InventoryPDF renders the inventory as a table (A4 landscape).
func InventoryPDF(w io.Writer, devs []InventoryDevice, loc *time.Location) error {
	if loc == nil {
		loc = time.Local
	}
	now := time.Now()
	d := newDoc("L", "Inventar", fmt.Sprintf("%d Geräte · Stand %s", len(devs), now.In(loc).Format("02.01.2006")), now, loc)
	online, unknown := 0, 0
	for _, dv := range devs {
		if dv.Online {
			online++
		}
		if dv.State == "unknown" {
			unknown++
		}
	}
	cw := (d.w - 2*d.m - 3*4) / 4
	y := d.pdf.GetY()
	d.card(d.m, y, cw, 24, "GERÄTE", strconv.Itoa(len(devs)), "im Export", pFg)
	d.card(d.m+(cw+4), y, cw, 24, "ONLINE", strconv.Itoa(online), "zuletzt erreichbar", pOK)
	d.card(d.m+2*(cw+4), y, cw, 24, "OFFLINE", strconv.Itoa(len(devs)-online), "nicht erreichbar", pSubtle)
	d.card(d.m+3*(cw+4), y, cw, 24, "UNBEKANNT", strconv.Itoa(unknown), "nicht bestätigt", pViolet)
	d.pdf.SetY(y + 30)

	states := map[string]string{"known": "bekannt", "unknown": "unbekannt", "ignored": "ignoriert"}
	stateTone := map[string]tone{"known": toneOK, "unknown": toneViolet, "ignored": toneInfo}
	var rows [][]pcell
	for _, dv := range devs {
		on := pcell{text: "online", chip: tonePtr(toneOK)}
		if !dv.Online {
			on = pcell{text: "offline", chip: tonePtr(toneInfo)}
		}
		last := ""
		if dv.LastSeen != nil {
			last = dv.LastSeen.In(loc).Format("02.01.06 15:04")
		}
		name := dv.Name
		if name == "" {
			name = dv.IP
		}
		st := stateTone[dv.State]
		if st.fg == "" {
			st = toneInfo
		}
		rows = append(rows, []pcell{{text: name, bold: true}, {text: dv.IP, mono: true}, {text: dv.MAC, mono: true}, {text: dv.Vendor},
			{text: dv.Type, muted: dv.Type == ""}, {text: dv.OS}, {text: states[dv.State], chip: tonePtr(st)}, on, {text: last, mono: true}})
	}
	d.table([]pcol{{"Name", 46, ""}, {"IP", 27, ""}, {"MAC", 33, ""}, {"Hersteller", 40, ""}, {"Typ", 22, ""}, {"Betriebssystem", 46, ""},
		{"Zustand", 20, ""}, {"Status", 16, ""}, {"Zuletzt gesehen", 19, "R"}}, rows)
	return d.pdf.Output(w)
}
