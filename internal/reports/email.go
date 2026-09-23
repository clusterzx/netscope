package reports

import (
	"fmt"
	"html"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/plugin"
)

// Email rendering of the change report: an HTML fragment for the body of a notification
// mail (plugin.Notification.HTML). Many mail clients ignore <style> blocks and modern
// layout, so everything is tables with inline styles in the light NetScope palette. The
// classes ns-tile, ns-panel and ns-sm-hide are progressive enhancement for small screens;
// the mail frame of the email publisher defines them in a media query.

const (
	cFg      = "#141922"
	cMuted   = "#535c6c"
	cSubtle  = "#858e9e"
	cBorder  = "#e1e4ea"
	cLine    = "#eceef2"
	cSurface = "#f6f7f9"
	cAccent  = "#0673b0"
	cOK      = "#15803d"
	cViolet  = "#7c3aed"
	fontSans = "-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"
	fontMono = "SFMono-Regular,Consolas,'Liberation Mono',Menlo,monospace"
)

// tone is a text colour with its light background tint.
type tone struct{ fg, bg string }

var (
	toneCritical = tone{"#be123c", "#fdecef"}
	toneHigh     = tone{"#c2410c", "#fdf0e7"}
	toneMedium   = tone{"#a66300", "#fdf6e3"}
	toneLow      = tone{"#1f6fb8", "#e9f2fb"}
	toneInfo     = tone{"#5b6577", "#eef0f3"}
	toneOK       = tone{"#15803d", "#e8f6ed"}
	toneViolet   = tone{"#7c3aed", "#f1ebfe"}
)

func sevTone(s string) tone {
	switch plugin.Severity(s) {
	case plugin.SevCritical:
		return toneCritical
	case plugin.SevHigh:
		return toneHigh
	case plugin.SevMedium:
		return toneMedium
	case plugin.SevLow:
		return toneLow
	}
	return toneInfo
}

// certTone colours the remaining lifetime of a certificate.
func certTone(days int) tone {
	switch {
	case days <= 7:
		return toneCritical
	case days <= 14:
		return toneHigh
	}
	return toneMedium
}

// visible limits of the mail tables; the attached PDF lists everything
const (
	mailMaxDevices = 40
	mailMaxRows    = 25
)

type htmlWriter struct {
	b    strings.Builder
	base string // public UI root for links ("" = no links)
	loc  *time.Location
}

func esc(s string) string { return html.EscapeString(s) }

func (w *htmlWriter) raw(s string) { w.b.WriteString(s) }

func badge(label string, t tone) string {
	return `<span style="display:inline-block;padding:2px 9px;border-radius:10px;background-color:` + t.bg + `;color:` + t.fg +
		`;font-size:12px;line-height:18px;font-weight:600;white-space:nowrap;">` + esc(label) + `</span>`
}

func (w *htmlWriter) link(path, label string) string {
	if w.base == "" {
		return esc(label)
	}
	return `<a href="` + esc(w.base+path) + `" style="color:` + cAccent + `;text-decoration:none;font-weight:600;">` + esc(label) + `</a>`
}

func (w *htmlWriter) date(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.In(w.loc).Format("02.01. 15:04") // the year is in the period
}

func mono(s string) string {
	return `<span style="font-family:` + fontMono + `;font-size:12px;color:` + cMuted + `;">` + esc(s) + `</span>`
}

func muted(s string) string {
	return `<span style="color:` + cSubtle + `;">` + esc(s) + `</span>`
}

// section writes a heading with an accent bar and an optional count.
func (w *htmlWriter) section(title string, count int) {
	c := ""
	if count > 0 {
		c = ` <span style="color:` + cSubtle + `;font-weight:400;">(` + strconv.Itoa(count) + `)</span>`
	}
	w.raw(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin:28px 0 10px 0;"><tr>` +
		`<td width="4" style="background-color:` + cAccent + `;border-radius:2px;font-size:0;line-height:0;">&nbsp;</td>` +
		`<td style="padding-left:10px;font-family:` + fontSans + `;font-size:16px;font-weight:700;color:` + cFg + `;">` + esc(title) + c + `</td>` +
		`</tr></table>` + "\n")
}

type col struct {
	title  string
	align  string // "", "right", "center"
	width  string // e.g. "30%"
	nowrap bool
	small  bool // hidden on small screens (ns-sm-hide)
}

// table writes a data table; cells are HTML.
func (w *htmlWriter) table(cols []col, rows [][]string, more int, moreText string) {
	w.raw(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="border-collapse:collapse;font-family:` +
		fontSans + `;font-size:13px;line-height:1.45;color:` + cFg + `;border:1px solid ` + cBorder + `;">` + "\n<tr>")
	for _, c := range cols {
		align := c.align
		if align == "" {
			align = "left"
		}
		width := ""
		if c.width != "" {
			width = ` width="` + c.width + `"`
		}
		cls := ""
		if c.small {
			cls = ` class="ns-sm-hide"`
		}
		w.raw(`<th` + cls + ` align="` + align + `"` + width + ` style="padding:8px 10px;background-color:` + cSurface + `;border-bottom:1px solid ` + cBorder +
			`;font-size:11px;font-weight:600;color:` + cSubtle + `;text-transform:uppercase;letter-spacing:0.04em;white-space:nowrap;">` + esc(c.title) + `</th>`)
	}
	w.raw("</tr>\n")
	for i, r := range rows {
		bg := "#ffffff"
		if i%2 == 1 {
			bg = "#fbfcfd"
		}
		w.raw(`<tr style="background-color:` + bg + `;">`)
		for j, cell := range r {
			c := cols[j]
			align := c.align
			if align == "" {
				align = "left"
			}
			// long names without break points ("DESKTOP-FANI8MD.local") may wrap anywhere,
			// otherwise they widen the mail beyond a phone screen
			nw := "overflow-wrap:anywhere;word-break:break-word;"
			if c.nowrap {
				nw = "white-space:nowrap;"
			}
			cls := ""
			if c.small {
				cls = ` class="ns-sm-hide"`
			}
			w.raw(`<td` + cls + ` align="` + align + `" valign="top" style="padding:8px 10px;border-bottom:1px solid ` + cLine + `;` + nw + `">` + cell + `</td>`)
		}
		w.raw("</tr>\n")
	}
	w.raw("</table>\n")
	if more > 0 {
		w.raw(`<div style="margin-top:6px;font-family:` + fontSans + `;font-size:12px;color:` + cSubtle + `;">… und ` + strconv.Itoa(more) + " " + esc(moreText) + "</div>\n")
	}
}

// tile is one key figure.
func tile(label, value, sub, color string) string {
	return `<td class="ns-tile" width="25%" valign="top" style="padding:0 4px;">` +
		`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="border:1px solid ` + cBorder + `;border-radius:10px;background-color:#ffffff;">` +
		`<tr><td style="padding:12px 14px;font-family:` + fontSans + `;">` +
		`<div style="font-size:11px;color:` + cSubtle + `;text-transform:uppercase;letter-spacing:0.04em;white-space:nowrap;">` + esc(label) + `</div>` +
		`<div style="font-size:26px;line-height:32px;font-weight:700;color:` + color + `;">` + esc(value) + `</div>` +
		`<div style="font-size:12px;color:` + cMuted + `;">` + esc(sub) + `</div>` +
		`</td></tr></table></td>`
}

// panel is a box with a label and a row of badges.
func panel(label string, badges []string) string {
	return `<td class="ns-panel" width="50%" valign="top" style="padding:0 4px;">` +
		`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="border:1px solid ` + cBorder + `;border-radius:10px;background-color:` + cSurface + `;">` +
		`<tr><td style="padding:12px 14px;font-family:` + fontSans + `;">` +
		`<div style="font-size:11px;color:` + cSubtle + `;text-transform:uppercase;letter-spacing:0.04em;margin-bottom:6px;">` + esc(label) + `</div>` +
		strings.Join(badges, "&nbsp; ") + `</td></tr></table></td>`
}

func countBadges(counts map[string]int, keys []string, zeroLabel string) []string {
	var out []string
	for _, k := range keys {
		if n := counts[k]; n > 0 {
			out = append(out, badge(strconv.Itoa(n)+" "+strings.ToLower(sevLabel(k)), sevTone(k)))
		}
	}
	if len(out) == 0 {
		out = append(out, badge(zeroLabel, toneOK))
	}
	return out
}

// EmailHTML renders the report for a notification mail. root is the public URL of the UI
// (links are left out without it).
func (r *ChangeReport) EmailHTML(loc *time.Location, root string) string {
	if loc == nil {
		loc = time.Local
	}
	w := &htmlWriter{base: strings.TrimRight(root, "/"), loc: loc}

	w.raw(`<div style="font-family:` + fontSans + `;font-size:14px;color:` + cMuted + `;margin:0 0 16px 0;">Zeitraum <strong style="color:` + cFg + `;">` +
		esc(r.From.In(loc).Format("02.01.2006 15:04")+" – "+r.To.In(loc).Format("02.01.2006 15:04")) + `</strong></div>` + "\n")

	// key figures
	total, online := r.Devices["total"], r.Devices["online"]
	pct := "–"
	if total > 0 {
		pct = fmt.Sprintf("%d %% erreichbar", online*100/total)
	}
	w.raw(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>` +
		tile("Geräte", strconv.Itoa(total), "im Inventar", cFg) +
		tile("Online", strconv.Itoa(online), pct, cOK) +
		tile("Neu", strconv.Itoa(r.Devices["new"]), "im Zeitraum", cAccent) +
		tile("Unbekannt", strconv.Itoa(r.Devices["unknown"]), "nicht bestätigt", cViolet) +
		"</tr></table>\n")
	open := map[string]int{"critical": r.OpenCritical, "high": r.OpenHigh}
	w.raw(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="margin-top:8px;"><tr>` +
		panel("Offene Events", countBadges(open, []string{"critical", "high"}, "keine kritischen")) +
		panel("Aktive Schwachstellen", countBadges(r.CVEBySeverity, []string{"critical", "high", "medium", "low"}, "keine")) +
		"</tr></table>\n")

	empty := len(r.NewDevices) == 0 && len(r.EventCounts) == 0 && len(r.Important) == 0 && len(r.NewCVEs) == 0 &&
		len(r.ExpiringCerts) == 0 && len(r.Outages) == 0 && len(r.FailedRuns) == 0
	if empty {
		w.raw(`<div style="margin-top:24px;padding:16px;border-radius:10px;background-color:` + toneOK.bg + `;color:` + toneOK.fg +
			`;font-family:` + fontSans + `;font-size:14px;font-weight:600;">Keine Änderungen im Zeitraum – alles ruhig.</div>` + "\n")
	}

	if len(r.Important) > 0 {
		w.section("Wichtige Ereignisse", len(r.Important))
		var rows [][]string
		for i, e := range r.Important {
			if i >= mailMaxRows {
				break
			}
			title := esc(e.Title)
			if w.base != "" {
				title = w.link("/events?id="+strconv.FormatInt(e.ID, 10), e.Title)
			}
			if e.Device != "" && !strings.Contains(e.Title, e.Device) {
				title += `<div style="font-size:12px;color:` + cSubtle + `;">` + esc(e.Device) + `</div>`
			}
			state := muted("offen")
			if e.Acked {
				state = badge("quittiert", toneOK)
			}
			rows = append(rows, []string{badge(sevLabel(e.Severity), sevTone(e.Severity)), mono(w.date(e.TS)), title, state})
		}
		w.table([]col{{title: "Schwere", nowrap: true}, {title: "Zeit", nowrap: true, small: true}, {title: "Ereignis", width: "60%"}, {title: "Status", nowrap: true}},
			rows, len(r.Important)-len(rows), "weitere im PDF-Anhang")
	}

	if len(r.NewDevices) > 0 {
		w.section("Neue Geräte", len(r.NewDevices))
		var rows [][]string
		for i, d := range r.NewDevices {
			if i >= mailMaxDevices {
				break
			}
			name := d.Name
			cell := esc(name)
			if w.base != "" {
				cell = w.link("/devices/"+strconv.FormatInt(d.ID, 10), name)
			}
			vendor := esc(d.Vendor)
			if d.Vendor == "" {
				vendor = muted("–")
			}
			rows = append(rows, []string{cell, mono(d.IP), vendor, mono(w.date(d.At))})
		}
		w.table([]col{{title: "Gerät", width: "34%"}, {title: "IP", nowrap: true}, {title: "Hersteller"}, {title: "Erstmals", nowrap: true, small: true}},
			rows, len(r.NewDevices)-len(rows), "weitere im PDF-Anhang")
	}

	if len(r.NewCVEs) > 0 {
		w.section("Neue Schwachstellen", len(r.NewCVEs))
		var rows [][]string
		for i, c := range r.NewCVEs {
			if i >= mailMaxRows {
				break
			}
			id := esc(c.CVE)
			if w.base != "" {
				id = w.link("/vulnerabilities/"+c.CVE, c.CVE)
			}
			score := badge(fmt.Sprintf("%.1f", c.CVSS), sevTone(string(plugin.SeverityFromCVSS(c.CVSS))))
			rows = append(rows, []string{`<span style="white-space:nowrap;">` + id + `</span>`, score, esc(c.Device), esc(c.Detail)})
		}
		w.table([]col{{title: "CVE", nowrap: true}, {title: "CVSS", align: "center", nowrap: true}, {title: "Gerät"}, {title: "Produkt", small: true}},
			rows, len(r.NewCVEs)-len(rows), "weitere im PDF-Anhang")
	}

	if len(r.ExpiringCerts) > 0 {
		w.section("Ablaufende Zertifikate", len(r.ExpiringCerts))
		var rows [][]string
		for i, c := range r.ExpiringCerts {
			if i >= mailMaxRows {
				break
			}
			left := badge(fmt.Sprintf("%d Tage", c.DaysLeft), certTone(c.DaysLeft))
			if c.DaysLeft < 0 {
				left = badge("abgelaufen", toneCritical)
			}
			rows = append(rows, []string{esc(c.Subject), mono(c.Endpoint), esc(c.Device), left})
		}
		w.table([]col{{title: "Zertifikat"}, {title: "Endpunkt", nowrap: true, small: true}, {title: "Gerät"}, {title: "Restlaufzeit", align: "right", nowrap: true}},
			rows, len(r.ExpiringCerts)-len(rows), "weitere im PDF-Anhang")
	}

	if len(r.Outages) > 0 {
		w.section("Ausfälle", len(r.Outages))
		var rows [][]string
		for i, o := range r.Outages {
			if i >= mailMaxRows {
				break
			}
			state := badge("Ausfall", toneCritical)
			if o.State == "degraded" {
				state = badge("Beeinträchtigt", toneMedium)
			}
			dur := esc(humanDuration(time.Duration(o.Seconds) * time.Second))
			if o.Ongoing {
				dur += " " + badge("andauernd", toneCritical)
			}
			rows = append(rows, []string{esc(o.Check), state, mono(w.date(o.Started)), dur})
		}
		w.table([]col{{title: "Check"}, {title: "Zustand", nowrap: true}, {title: "Beginn", nowrap: true, small: true}, {title: "Dauer", nowrap: true}},
			rows, len(r.Outages)-len(rows), "weitere im PDF-Anhang")
	}

	if len(r.EventCounts) > 0 {
		w.section("Ereignisse nach Typ", 0)
		type kv struct {
			k string
			v int
		}
		var list []kv
		maxV := 1
		for k, v := range r.EventCounts {
			list = append(list, kv{k, v})
			maxV = max(maxV, v)
		}
		sort.Slice(list, func(i, j int) bool { return list[i].v > list[j].v || (list[i].v == list[j].v && list[i].k < list[j].k) })
		var rows [][]string
		for _, e := range list {
			pct := max(2, e.v*100/maxV)
			bar := `<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>` +
				`<td width="` + strconv.Itoa(pct) + `%" style="background-color:` + cAccent + `;height:8px;border-radius:4px;font-size:0;line-height:0;">&nbsp;</td>`
			if pct < 100 {
				bar += `<td style="font-size:0;line-height:0;">&nbsp;</td>`
			}
			bar += `</tr></table>`
			rows = append(rows, []string{esc(eventLabel(e.k)), bar, `<strong>` + strconv.Itoa(e.v) + `</strong>`})
		}
		w.table([]col{{title: "Ereignis", width: "46%"}, {title: "Verteilung", width: "40%", small: true}, {title: "Anzahl", align: "right", nowrap: true}}, rows, 0, "")
	}

	if len(r.FailedRuns) > 0 {
		w.section("Fehlgeschlagene Plugin-Läufe", 0)
		ids := make([]string, 0, len(r.FailedRuns))
		for id := range r.FailedRuns {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		var rows [][]string
		for _, id := range ids {
			name := id
			if p, ok := plugin.Get(id); ok {
				name = p.Info().Name
			}
			cell := esc(name)
			if w.base != "" {
				cell = w.link("/plugins/"+id, name)
			}
			rows = append(rows, []string{cell, badge(strconv.Itoa(r.FailedRuns[id])+"×", toneHigh)})
		}
		w.table([]col{{title: "Plugin"}, {title: "Fehlschläge", align: "right", nowrap: true}}, rows, 0, "")
	}
	return w.b.String()
}
