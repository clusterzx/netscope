package email

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"html"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"netscope/internal/plugin"
)

const (
	maxSubjectRunes = 200
	// maxHTMLEvents bounds the HTML table; the text part always lists all events.
	maxHTMLEvents = 200
)

var severityColors = map[plugin.Severity]string{
	plugin.SevCritical: "#b91c1c",
	plugin.SevHigh:     "#c2410c",
	plugin.SevMedium:   "#a16207",
	plugin.SevLow:      "#1d4ed8",
	plugin.SevInfo:     "#52525b",
}

var priorityLabels = map[plugin.Priority]string{
	plugin.PrioLow:    "Niedrig",
	plugin.PrioNormal: "Normal",
	plugin.PrioHigh:   "Hoch",
	plugin.PrioUrgent: "Dringend",
}

var kindLabels = map[string]string{
	plugin.NotifyEvent:      "Ereignisse",
	plugin.NotifyEscalation: "Eskalation",
	plugin.NotifyTest:       "Testnachricht",
	plugin.NotifyReport:     "Bericht",
}

// envelope holds everything needed to render a message.
type envelope struct {
	From   *mail.Address
	To     []*mail.Address
	Prefix string // subject prefix, e.g. "[NetScope]"
	Now    time.Time
	Loc    *time.Location
	Mailer string
}

// subject builds the (unencoded) subject line.
func subject(n *plugin.Notification, prefix string) string {
	t := oneLine(n.Title)
	if t == "" {
		t = "NetScope"
	}
	if n.Kind == plugin.NotifyEscalation {
		t = "Eskalation – nicht quittiert: " + t
	}
	if p := oneLine(prefix); p != "" {
		t = p + " " + t
	}
	return clip(t, maxSubjectRunes)
}

// encodeHeader RFC 2047-encodes a header value if needed and folds long encoded values.
func encodeHeader(s string) string {
	enc := mime.QEncoding.Encode("utf-8", s)
	if strings.HasPrefix(enc, "=?") {
		enc = strings.ReplaceAll(enc, "?= =?", "?=\r\n =?")
	}
	return enc
}

// plainText renders the text/plain part.
func plainText(n *plugin.Notification) string {
	var b strings.Builder
	if n.Kind == plugin.NotifyEscalation {
		b.WriteString("⏰ ESKALATION – nicht quittiert\n\n")
	}
	b.WriteString(n.PlainText())
	b.WriteString("\n-- \nDiese Nachricht wurde automatisch von NetScope erzeugt.\n")
	return b.String()
}

// buildMessage renders the complete MIME message with CRLF line endings.
func buildMessage(n *plugin.Notification, env envelope) ([]byte, error) {
	if env.Loc == nil {
		env.Loc = time.Local
	}
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	h := func(name, value string) {
		buf.WriteString(name + ": " + value + "\r\n")
	}
	to := make([]string, len(env.To))
	for i, a := range env.To {
		to[i] = a.String()
	}
	h("From", env.From.String())
	h("To", strings.Join(to, ",\r\n "))
	h("Subject", encodeHeader(subject(n, env.Prefix)))
	h("Date", env.Now.In(env.Loc).Format(time.RFC1123Z))
	h("Message-ID", messageID(env.From, env.Now))
	h("MIME-Version", "1.0")
	h("Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": mw.Boundary()}))
	h("Auto-Submitted", "auto-generated")
	if env.Mailer != "" {
		h("X-Mailer", env.Mailer)
	}
	h("X-NetScope-Kind", strings.Map(asciiHeader, n.Kind))
	if n.ID > 0 {
		h("X-NetScope-Notification-Id", strconv.FormatInt(n.ID, 10))
	}
	switch n.Priority {
	case plugin.PrioUrgent:
		h("X-Priority", "1 (Highest)")
		h("Importance", "high")
	case plugin.PrioHigh:
		h("X-Priority", "2 (High)")
		h("Importance", "high")
	case plugin.PrioLow:
		h("X-Priority", "5 (Lowest)")
		h("Importance", "low")
	}
	buf.WriteString("\r\n")

	if err := writePart(mw, "text/plain; charset=utf-8", plainText(n)); err != nil {
		return nil, err
	}
	if err := writePart(mw, "text/html; charset=utf-8", htmlBody(n, env.Loc)); err != nil {
		return nil, err
	}
	if err := mw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func writePart(mw *multipart.Writer, contentType, body string) error {
	pw, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {contentType},
		"Content-Transfer-Encoding": {"quoted-printable"},
	})
	if err != nil {
		return err
	}
	// Binary=false: line breaks are encoded as CRLF.
	qp := quotedprintable.NewWriter(pw)
	if _, err := qp.Write([]byte(strings.ReplaceAll(body, "\r\n", "\n"))); err != nil {
		return err
	}
	return qp.Close()
}

func messageID(from *mail.Address, now time.Time) string {
	var rnd [12]byte
	_, _ = rand.Read(rnd[:])
	domain := "netscope.invalid"
	if at := strings.LastIndexByte(from.Address, '@'); at >= 0 && at < len(from.Address)-1 {
		if d := strings.Map(asciiHeader, strings.ToLower(from.Address[at+1:])); d != "" {
			domain = d
		}
	}
	return "<" + strconv.FormatInt(now.UnixNano(), 36) + "." + hex.EncodeToString(rnd[:]) + "@" + domain + ">"
}

// asciiHeader drops everything that must not appear in a raw header value.
func asciiHeader(r rune) rune {
	if r <= ' ' || r >= 0x7f || r == '<' || r == '>' {
		return -1
	}
	return r
}

// htmlBody renders the text/html part: inline styles only (mail clients ignore
// <style> blocks), a table of events with severity colours and links into the UI.
func htmlBody(n *plugin.Notification, loc *time.Location) string {
	esc := html.EscapeString
	var b strings.Builder
	title := oneLine(n.Title)
	if title == "" {
		title = "NetScope"
	}
	b.WriteString("<!DOCTYPE html>\n<html lang=\"de\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<title>" + esc(title) + "</title>\n</head>\n")
	b.WriteString(`<body style="margin:0;padding:0;background-color:#f4f4f5;">` + "\n")
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background-color:#f4f4f5;">` + "\n")
	b.WriteString(`<tr><td align="center" style="padding:24px 12px;">` + "\n")
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:720px;background-color:#ffffff;border:1px solid #e4e4e7;border-radius:8px;font-family:-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif;color:#18181b;">` + "\n")

	if n.Kind == plugin.NotifyEscalation {
		b.WriteString(`<tr><td style="padding:12px 24px;background-color:#b91c1c;color:#ffffff;font-size:15px;font-weight:700;border-radius:8px 8px 0 0;">` +
			"&#9200; Eskalation – nicht quittiert</td></tr>\n")
	}

	// Header
	kind := kindLabels[n.Kind]
	if kind == "" {
		kind = "Benachrichtigung"
	}
	b.WriteString(`<tr><td style="padding:20px 24px 8px 24px;">` + "\n")
	b.WriteString(`<div style="font-size:12px;color:#71717a;text-transform:uppercase;letter-spacing:0.05em;">NetScope · ` + esc(kind) + "</div>\n")
	b.WriteString(`<h1 style="margin:4px 0 0 0;font-size:20px;line-height:1.3;font-weight:700;color:#18181b;">` + esc(title) + "</h1>\n")
	var meta []string
	if p, ok := priorityLabels[n.Priority]; ok {
		meta = append(meta, "Priorität: "+p)
	}
	if n.RuleName != "" {
		meta = append(meta, "Regel: "+oneLine(n.RuleName))
	}
	if len(n.Events) > 0 {
		meta = append(meta, countLabel(len(n.Events)))
	}
	if !n.CreatedAt.IsZero() {
		meta = append(meta, n.CreatedAt.In(loc).Format("02.01.2006 15:04"))
	}
	if len(meta) > 0 {
		b.WriteString(`<div style="margin-top:6px;font-size:13px;color:#52525b;">` + esc(strings.Join(meta, " · ")) + "</div>\n")
	}
	b.WriteString("</td></tr>\n")

	// Body (reports, test messages): Markdown shown as preformatted text.
	if body := strings.TrimSpace(strings.ReplaceAll(n.Body, "\r\n", "\n")); body != "" {
		b.WriteString(`<tr><td style="padding:8px 24px 8px 24px;">` + "\n")
		b.WriteString(`<div style="white-space:pre-wrap;font-size:14px;line-height:1.5;color:#27272a;">` + esc(body) + "</div>\n")
		b.WriteString("</td></tr>\n")
	}

	if len(n.Events) > 0 {
		b.WriteString(eventTable(n.Events, loc))
	}

	// Footer
	b.WriteString(`<tr><td style="padding:16px 24px;border-top:1px solid #e4e4e7;font-size:12px;line-height:1.5;color:#71717a;">`)
	if u, ok := safeURL(n.Link); ok {
		b.WriteString(`<a href="` + esc(u) + `" style="color:#1d4ed8;text-decoration:none;font-weight:600;">In NetScope öffnen</a> · `)
	}
	b.WriteString("Diese Nachricht wurde automatisch von NetScope erzeugt.</td></tr>\n")
	b.WriteString("</table>\n</td></tr>\n</table>\n</body>\n</html>\n")
	return b.String()
}

func eventTable(events []plugin.EventView, loc *time.Location) string {
	esc := html.EscapeString
	const (
		th   = `<th align="left" style="padding:8px 8px 8px 0;border-bottom:2px solid #e4e4e7;font-size:12px;font-weight:600;color:#71717a;">`
		cell = `<td valign="top" style="padding:10px 8px 10px 0;border-bottom:1px solid #f4f4f5;`
	)
	var b strings.Builder
	b.WriteString(`<tr><td style="padding:8px 24px 16px 24px;">` + "\n")
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="border-collapse:collapse;font-size:14px;line-height:1.4;">` + "\n")
	b.WriteString("<tr>" + th + "Schweregrad</th>" + th + "Ereignis</th>" + th + "Gerät</th>" + th + "Zeit</th></tr>\n")
	shown := events
	if len(shown) > maxHTMLEvents {
		shown = shown[:maxHTMLEvents]
	}
	for _, e := range shown {
		color, ok := severityColors[e.Severity]
		if !ok {
			color = severityColors[plugin.SevInfo]
		}
		b.WriteString("<tr>")
		// Severity badge
		b.WriteString(cell + `white-space:nowrap;"><span style="display:inline-block;padding:2px 8px;border-radius:4px;background-color:` +
			color + `;color:#ffffff;font-size:12px;font-weight:600;">` + esc(e.Severity.Label()) + "</span></td>")
		// Event
		t := oneLine(e.Title)
		if t == "" {
			t = oneLine(e.Label)
		}
		if t == "" {
			t = e.Type
		}
		b.WriteString(cell + `">`)
		if u, ok := safeURL(e.Link); ok {
			b.WriteString(`<a href="` + esc(u) + `" style="color:#1d4ed8;font-weight:600;text-decoration:none;">` + esc(t) + "</a>")
		} else {
			b.WriteString(`<span style="font-weight:600;">` + esc(t) + "</span>")
		}
		if e.Label != "" && e.Label != t {
			b.WriteString(`<div style="font-size:12px;color:#71717a;">` + esc(e.Label) + "</div>")
		}
		if msg := strings.TrimSpace(e.Message); msg != "" {
			b.WriteString(`<div style="margin-top:4px;color:#3f3f46;">` + strings.ReplaceAll(esc(msg), "\n", "<br>") + "</div>")
		}
		var marks []string
		if e.Escalated {
			marks = append(marks, "&#9200; eskaliert")
		}
		if e.Acknowledged {
			marks = append(marks, "&#9989; quittiert")
		}
		if len(marks) > 0 {
			b.WriteString(`<div style="margin-top:4px;font-size:12px;color:#71717a;">` + strings.Join(marks, " · ") + "</div>")
		}
		b.WriteString("</td>")
		// Device
		b.WriteString(cell + `">`)
		name := oneLine(e.DeviceName)
		if name == "" {
			name = e.DeviceIP
		}
		switch u, ok := safeURL(e.DeviceLink); {
		case name != "" && ok:
			b.WriteString(`<a href="` + esc(u) + `" style="color:#1d4ed8;text-decoration:none;">` + esc(name) + "</a>")
		case name != "":
			b.WriteString(esc(name))
		default:
			b.WriteString(`<span style="color:#a1a1aa;">–</span>`)
		}
		if e.DeviceIP != "" && e.DeviceIP != name {
			b.WriteString(`<div style="font-size:12px;color:#71717a;">` + esc(e.DeviceIP) + "</div>")
		}
		b.WriteString("</td>")
		// Time
		at := ""
		if !e.At.IsZero() {
			at = e.At.In(loc).Format("02.01.2006 15:04")
		}
		b.WriteString(cell + `white-space:nowrap;color:#52525b;">` + esc(at) + "</td>")
		b.WriteString("</tr>\n")
	}
	b.WriteString("</table>\n")
	if more := len(events) - len(shown); more > 0 {
		b.WriteString(`<div style="margin-top:8px;font-size:13px;color:#52525b;">… und ` + strconv.Itoa(more) +
			" weitere Ereignisse (vollständige Liste im Textteil)</div>\n")
	}
	b.WriteString("</td></tr>\n")
	return b.String()
}

// safeURL accepts only absolute http(s) URLs for links.
func safeURL(s string) (string, bool) {
	if s == "" {
		return "", false
	}
	u, err := url.Parse(s)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", false
	}
	return u.String(), true
}

func countLabel(n int) string {
	if n == 1 {
		return "1 Ereignis"
	}
	return fmt.Sprintf("%d Ereignisse", n)
}

// oneLine collapses whitespace and removes control characters.
func oneLine(s string) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, ""))
	return strings.Join(strings.Fields(s), " ")
}

// clip shortens s to at most n runes (adding "…").
func clip(s string, n int) string {
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n-1])) + "…"
}
