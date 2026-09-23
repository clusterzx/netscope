package email

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"net/textproto"
	"net/url"
	"regexp"
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
	for _, a := range n.Attachments {
		b.WriteString("\nAnhang: " + a.Name + "\n")
	}
	b.WriteString("\n-- \nDiese Nachricht wurde automatisch von NetScope erzeugt.\n")
	return b.String()
}

// buildMessage renders the complete MIME message with CRLF line endings.
func buildMessage(n *plugin.Notification, env envelope) ([]byte, error) {
	if env.Loc == nil {
		env.Loc = time.Local
	}
	var buf bytes.Buffer
	// with attachments: multipart/mixed { multipart/alternative { text, html }, files… }
	mw := multipart.NewWriter(&buf)
	outer := mw
	if len(n.Attachments) > 0 {
		outer = multipart.NewWriter(&buf)
	}

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
	if outer != mw {
		h("Content-Type", mime.FormatMediaType("multipart/mixed", map[string]string{"boundary": outer.Boundary()}))
	} else {
		h("Content-Type", mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": mw.Boundary()}))
	}
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

	if outer != mw {
		// the alternative part lives inside the mixed container
		var alt bytes.Buffer
		mw = multipart.NewWriter(&alt)
		if err := writeAlternative(mw, n, env.Loc); err != nil {
			return nil, err
		}
		pw, err := outer.CreatePart(textproto.MIMEHeader{
			"Content-Type": {mime.FormatMediaType("multipart/alternative", map[string]string{"boundary": mw.Boundary()})},
		})
		if err != nil {
			return nil, err
		}
		if _, err := pw.Write(alt.Bytes()); err != nil {
			return nil, err
		}
		for _, a := range n.Attachments {
			if err := writeAttachment(outer, a); err != nil {
				return nil, err
			}
		}
		if err := outer.Close(); err != nil {
			return nil, err
		}
		return buf.Bytes(), nil
	}
	if err := writeAlternative(mw, n, env.Loc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// writeAlternative writes the text and HTML parts and closes mw.
func writeAlternative(mw *multipart.Writer, n *plugin.Notification, loc *time.Location) error {
	if err := writePart(mw, "text/plain; charset=utf-8", plainText(n)); err != nil {
		return err
	}
	if err := writePart(mw, "text/html; charset=utf-8", htmlBody(n, loc)); err != nil {
		return err
	}
	return mw.Close()
}

// writeAttachment adds a file as base64 part (lines of 76 characters).
func writeAttachment(mw *multipart.Writer, a plugin.Attachment) error {
	name := strings.Map(func(r rune) rune {
		if r < ' ' || r == '"' || r == '/' || r == '\\' {
			return '_'
		}
		return r
	}, a.Name)
	if name == "" {
		name = "anhang"
	}
	ct := a.ContentType
	if ct == "" {
		ct = "application/octet-stream"
	}
	pw, err := mw.CreatePart(textproto.MIMEHeader{
		"Content-Type":              {mime.FormatMediaType(ct, map[string]string{"name": name})},
		"Content-Disposition":       {mime.FormatMediaType("attachment", map[string]string{"filename": name})},
		"Content-Transfer-Encoding": {"base64"},
	})
	if err != nil {
		return err
	}
	enc := base64.StdEncoding.EncodeToString(a.Data)
	for len(enc) > 76 {
		if _, err := io.WriteString(pw, enc[:76]+"\r\n"); err != nil {
			return err
		}
		enc = enc[76:]
	}
	_, err = io.WriteString(pw, enc+"\r\n")
	return err
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

// Mail palette (light NetScope theme; many clients do not support dark mode styles).
const (
	mFont    = "-apple-system,'Segoe UI',Roboto,Helvetica,Arial,sans-serif"
	mMono    = "SFMono-Regular,Consolas,'Liberation Mono',Menlo,monospace"
	mFg      = "#141922"
	mMuted   = "#535c6c"
	mSubtle  = "#858e9e"
	mBorder  = "#e1e4ea"
	mLine    = "#eceef2"
	mSurface = "#f6f7f9"
	mAccent  = "#0673b0"
	mNavy    = "#0f172a"
	mSky     = "#38bdf8"
)

// htmlBody renders the text/html part: inline styles only (mail clients ignore <style>
// blocks), tables for layout. Reports bring their own HTML (n.HTML); other bodies are
// rendered from Markdown; events become a table with severity badges.
func htmlBody(n *plugin.Notification, loc *time.Location) string {
	esc := html.EscapeString
	var b strings.Builder
	title := oneLine(n.Title)
	if title == "" {
		title = "NetScope"
	}
	kind := kindLabels[n.Kind]
	if kind == "" {
		kind = "Benachrichtigung"
	}
	side := `border-left:1px solid ` + mBorder + `;border-right:1px solid ` + mBorder + `;`
	b.WriteString("<!DOCTYPE html>\n<html lang=\"de\">\n<head>\n<meta charset=\"utf-8\">\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n")
	b.WriteString("<meta name=\"color-scheme\" content=\"light\">\n<meta name=\"supported-color-schemes\" content=\"light\">\n")
	b.WriteString("<title>" + esc(title) + "</title>\n")
	// progressive enhancement for phones (Apple Mail, Gmail, Outlook apps); every layout
	// property is also set inline for clients without <style> support
	b.WriteString("<style>\n@media only screen and (max-width:620px){\n" +
		".ns-pad{padding-left:16px !important;padding-right:16px !important}\n" +
		".ns-title{font-size:19px !important}\n" +
		".ns-tile{display:inline-block !important;width:50% !important;box-sizing:border-box;padding:4px !important}\n" +
		".ns-panel{display:block !important;width:100% !important;box-sizing:border-box;padding:4px !important}\n" +
		".ns-sm-hide{display:none !important}\n}\n</style>\n</head>\n")
	b.WriteString(`<body style="margin:0;padding:0;background-color:#f3f4f7;">` + "\n")
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="background-color:#f3f4f7;">` + "\n")
	b.WriteString(`<tr><td align="center" style="padding:24px 12px;">` + "\n")
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="max-width:720px;font-family:` + mFont + `;color:` + mFg + `;">` + "\n")

	// brand band
	b.WriteString(`<tr><td class="ns-pad" style="background-color:` + mNavy + `;padding:18px 28px;border-radius:12px 12px 0 0;border-bottom:3px solid ` + mSky + `;">`)
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0"><tr>`)
	b.WriteString(`<td style="font-family:` + mFont + `;font-size:19px;font-weight:700;color:#ffffff;white-space:nowrap;"><span style="color:` + mSky + `;">&#9673;</span>&nbsp;NetScope</td>`)
	b.WriteString(`<td align="right" style="font-family:` + mFont + `;font-size:11px;font-weight:600;color:#94a3b8;text-transform:uppercase;letter-spacing:0.08em;">` + esc(kind) + `</td>`)
	b.WriteString("</tr></table></td></tr>\n")

	if n.Kind == plugin.NotifyEscalation {
		b.WriteString(`<tr><td style="padding:12px 28px;background-color:#be123c;color:#ffffff;font-size:15px;font-weight:700;">` +
			"&#9200; Eskalation – nicht quittiert</td></tr>\n")
	}

	// title
	b.WriteString(`<tr><td class="ns-pad" style="background-color:#ffffff;padding:24px 28px 4px 28px;` + side + `">` + "\n")
	b.WriteString(`<h1 class="ns-title" style="margin:0;font-size:22px;line-height:1.3;font-weight:700;color:` + mFg + `;">` + esc(title) + "</h1>\n")
	var meta []string
	if p, ok := priorityLabels[n.Priority]; ok && n.Priority != plugin.PrioNormal && n.Priority != plugin.PrioLow {
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
		b.WriteString(`<div style="margin-top:6px;font-size:13px;color:` + mSubtle + `;">` + esc(strings.Join(meta, " · ")) + "</div>\n")
	}
	b.WriteString("</td></tr>\n")

	// content
	b.WriteString(`<tr><td class="ns-pad" style="background-color:#ffffff;padding:16px 28px 24px 28px;` + side + `">` + "\n")
	switch body := strings.TrimSpace(strings.ReplaceAll(n.Body, "\r\n", "\n")); {
	case n.HTML != "":
		b.WriteString(n.HTML)
	case body != "":
		b.WriteString(markdownHTML(body))
	}
	if len(n.Events) > 0 {
		b.WriteString(eventTable(n.Events, loc))
	}
	if len(n.Attachments) > 0 {
		names := make([]string, len(n.Attachments))
		for i, a := range n.Attachments {
			names[i] = esc(a.Name)
		}
		b.WriteString(`<div style="margin-top:20px;padding:10px 14px;border-radius:8px;background-color:` + mSurface + `;font-size:13px;color:` + mMuted +
			`;">&#128206; Im Anhang: <strong style="color:` + mFg + `;">` + strings.Join(names, ", ") + "</strong></div>\n")
	}
	b.WriteString("</td></tr>\n")

	// footer
	b.WriteString(`<tr><td class="ns-pad" style="background-color:` + mSurface + `;padding:18px 28px;border:1px solid ` + mBorder + `;border-radius:0 0 12px 12px;font-size:12px;line-height:1.5;color:` + mSubtle + `;">`)
	if u, ok := safeURL(n.Link); ok {
		b.WriteString(`<a href="` + esc(u) + `" style="display:inline-block;margin-bottom:10px;padding:9px 16px;border-radius:8px;background-color:` + mAccent +
			`;color:#ffffff;font-size:13px;font-weight:600;text-decoration:none;">In NetScope öffnen</a><br>`)
	}
	b.WriteString("Diese Nachricht wurde automatisch von NetScope erzeugt.</td></tr>\n")
	b.WriteString("</table>\n</td></tr>\n</table>\n</body>\n</html>\n")
	return b.String()
}

// severity badge colours: text on a light tint
var severityTones = map[plugin.Severity][2]string{
	plugin.SevCritical: {"#be123c", "#fdecef"},
	plugin.SevHigh:     {"#c2410c", "#fdf0e7"},
	plugin.SevMedium:   {"#a66300", "#fdf6e3"},
	plugin.SevLow:      {"#1f6fb8", "#e9f2fb"},
	plugin.SevInfo:     {"#5b6577", "#eef0f3"},
}

func eventTable(events []plugin.EventView, loc *time.Location) string {
	esc := html.EscapeString
	const (
		th   = `<th align="left" style="padding:8px 10px;background-color:` + mSurface + `;border-bottom:1px solid ` + mBorder + `;font-size:11px;font-weight:600;color:` + mSubtle + `;text-transform:uppercase;letter-spacing:0.04em;white-space:nowrap;">`
		cell = `<td valign="top" style="padding:10px;border-bottom:1px solid ` + mLine + `;overflow-wrap:anywhere;word-break:break-word;`
	)
	var b strings.Builder
	b.WriteString(`<table role="presentation" width="100%" cellpadding="0" cellspacing="0" border="0" style="border-collapse:collapse;font-size:14px;line-height:1.45;border:1px solid ` + mBorder + `;">` + "\n")
	b.WriteString("<tr>" + th + "Schwere</th>" + th + "Ereignis</th>" + th + "Gerät</th>" +
		strings.Replace(th, "<th ", `<th class="ns-sm-hide" `, 1) + "Zeit</th></tr>\n")
	shown := events
	if len(shown) > maxHTMLEvents {
		shown = shown[:maxHTMLEvents]
	}
	for _, e := range shown {
		tn, ok := severityTones[e.Severity]
		if !ok {
			tn = severityTones[plugin.SevInfo]
		}
		b.WriteString("<tr>")
		b.WriteString(cell + `white-space:nowrap;"><span style="display:inline-block;padding:2px 9px;border-radius:10px;background-color:` + tn[1] +
			`;color:` + tn[0] + `;font-size:12px;font-weight:600;">` + esc(e.Severity.Label()) + "</span></td>")
		t := oneLine(e.Title)
		if t == "" {
			t = oneLine(e.Label)
		}
		if t == "" {
			t = e.Type
		}
		b.WriteString(cell + `">`)
		if u, ok := safeURL(e.Link); ok {
			b.WriteString(`<a href="` + esc(u) + `" style="color:` + mAccent + `;font-weight:600;text-decoration:none;">` + esc(t) + "</a>")
		} else {
			b.WriteString(`<span style="font-weight:600;">` + esc(t) + "</span>")
		}
		if e.Label != "" && e.Label != t {
			b.WriteString(`<div style="font-size:12px;color:` + mSubtle + `;">` + esc(e.Label) + "</div>")
		}
		if msg := strings.TrimSpace(e.Message); msg != "" {
			b.WriteString(`<div style="margin-top:4px;color:` + mMuted + `;">` + strings.ReplaceAll(esc(msg), "\n", "<br>") + "</div>")
		}
		var marks []string
		if e.Escalated {
			marks = append(marks, "&#9200; eskaliert")
		}
		if e.Acknowledged {
			marks = append(marks, "&#9989; quittiert")
		}
		if len(marks) > 0 {
			b.WriteString(`<div style="margin-top:4px;font-size:12px;color:` + mSubtle + `;">` + strings.Join(marks, " · ") + "</div>")
		}
		b.WriteString("</td>")
		b.WriteString(cell + `">`)
		name := oneLine(e.DeviceName)
		if name == "" {
			name = e.DeviceIP
		}
		switch u, ok := safeURL(e.DeviceLink); {
		case name != "" && ok:
			b.WriteString(`<a href="` + esc(u) + `" style="color:` + mAccent + `;text-decoration:none;">` + esc(name) + "</a>")
		case name != "":
			b.WriteString(esc(name))
		default:
			b.WriteString(`<span style="color:#a1a1aa;">–</span>`)
		}
		if e.DeviceIP != "" && e.DeviceIP != name {
			b.WriteString(`<div style="font-family:` + mMono + `;font-size:12px;color:` + mSubtle + `;">` + esc(e.DeviceIP) + "</div>")
		}
		b.WriteString("</td>")
		at := ""
		if !e.At.IsZero() {
			at = e.At.In(loc).Format("02.01.2006 15:04")
		}
		b.WriteString(strings.Replace(cell, "<td ", `<td class="ns-sm-hide" `, 1) + `white-space:nowrap;color:` + mMuted + `;font-size:13px;">` + esc(at) + "</td>")
		b.WriteString("</tr>\n")
	}
	b.WriteString("</table>\n")
	if more := len(events) - len(shown); more > 0 {
		b.WriteString(`<div style="margin-top:8px;font-size:13px;color:` + mMuted + `;">… und ` + strconv.Itoa(more) +
			" weitere Ereignisse (vollständige Liste im Textteil)</div>\n")
	}
	return b.String()
}

var (
	mdBold = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdCode = regexp.MustCompile("`([^`]+)`")
	mdLink = regexp.MustCompile(`\[([^\]]+)\]\((https?://[^\s)]+)\)`)
	mdURL  = regexp.MustCompile(`(^|[\s(])(https?://[^\s<)]*[^\s<).,;:!?])`) // no trailing punctuation
)

// mdInline escapes a line and renders **bold**, `code`, [links](url) and bare URLs.
func mdInline(s string) string {
	s = html.EscapeString(s)
	s = mdCode.ReplaceAllString(s, `<code style="font-family:`+mMono+`;font-size:12px;background-color:`+mSurface+`;padding:1px 5px;border-radius:4px;">$1</code>`)
	s = mdBold.ReplaceAllString(s, `<strong style="color:`+mFg+`;">$1</strong>`)
	if mdLink.MatchString(s) {
		return mdLink.ReplaceAllString(s, `<a href="$2" style="color:`+mAccent+`;text-decoration:none;font-weight:600;">$1</a>`)
	}
	return mdURL.ReplaceAllString(s, `$1<a href="$2" style="color:`+mAccent+`;text-decoration:none;">$2</a>`)
}

// markdownHTML renders the small Markdown subset used in notification bodies: headings,
// lists, paragraphs and inline formatting. Everything is escaped first.
func markdownHTML(src string) string {
	var b strings.Builder
	var para []string
	inList := false
	flushPara := func() {
		if len(para) > 0 {
			b.WriteString(`<p style="margin:0 0 12px 0;font-size:14px;line-height:1.55;color:` + mMuted + `;">` + strings.Join(para, "<br>") + "</p>\n")
			para = nil
		}
	}
	closeList := func() {
		if inList {
			b.WriteString("</ul>\n")
			inList = false
		}
	}
	for _, line := range strings.Split(src, "\n") {
		t := strings.TrimSpace(line)
		switch {
		case t == "":
			flushPara()
			closeList()
		case strings.HasPrefix(t, "#"):
			flushPara()
			closeList()
			text := strings.TrimSpace(strings.TrimLeft(t, "#"))
			b.WriteString(`<h2 style="margin:20px 0 8px 0;font-size:16px;line-height:1.3;font-weight:700;color:` + mFg + `;">` + mdInline(text) + "</h2>\n")
		case strings.HasPrefix(t, "- ") || strings.HasPrefix(t, "* ") || strings.HasPrefix(t, "• "):
			flushPara()
			if !inList {
				b.WriteString(`<ul style="margin:0 0 12px 0;padding-left:20px;font-size:14px;line-height:1.55;color:` + mMuted + `;">` + "\n")
				inList = true
			}
			item := strings.TrimSpace(t[strings.Index(t, " "):])
			b.WriteString(`<li style="margin:0 0 4px 0;">` + mdInline(item) + "</li>\n")
		default:
			closeList()
			para = append(para, mdInline(t))
		}
	}
	flushPara()
	closeList()
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
