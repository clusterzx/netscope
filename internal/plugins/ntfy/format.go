package ntfy

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"netscope/internal/plugin"
)

const (
	// maxMessageBytes keeps the message below ntfy's default message size limit of
	// 4096 bytes (larger messages would be turned into an attachment).
	maxMessageBytes = 3900
	// footerReserve is kept free for the "… und N weitere" line.
	footerReserve = 400
	maxEvents     = 20

	maxTitleRunes   = 200
	maxEventRunes   = 200
	maxMessageRunes = 500
	maxNameRunes    = 100
	maxURLLen       = 500
)

// build creates the ntfy publish message for a notification.
func build(n *plugin.Notification, topic string, md, tagsBySeverity bool, loc *time.Location) message {
	if loc == nil {
		loc = time.Local
	}
	m := message{
		Topic:    topic,
		Title:    title(n),
		Priority: priority(n.Priority),
		Tags:     tags(n, tagsBySeverity),
		Click:    click(n),
		Markdown: md,
	}
	m.Message = render(n, md, loc)
	return m
}

func title(n *plugin.Notification) string {
	t := clip(oneLine(n.Title), maxTitleRunes)
	if t == "" {
		t = "NetScope"
	}
	if n.Kind == plugin.NotifyEscalation {
		t = "Eskalation – nicht quittiert: " + t
	}
	return t
}

// priority maps NetScope priorities to ntfy priorities (1 = min … 5 = max).
func priority(p plugin.Priority) int {
	switch p {
	case plugin.PrioLow:
		return 2
	case plugin.PrioHigh:
		return 4
	case plugin.PrioUrgent:
		return 5
	}
	return 3
}

var severityTags = map[plugin.Severity]string{
	plugin.SevCritical: "rotating_light",
	plugin.SevHigh:     "warning",
	plugin.SevMedium:   "large_orange_diamond",
	plugin.SevLow:      "large_blue_diamond",
	plugin.SevInfo:     "information_source",
}

// tags returns ntfy tags; tags matching an emoji short code are shown as emoji.
func tags(n *plugin.Notification, bySeverity bool) []string {
	var out []string
	switch n.Kind {
	case plugin.NotifyEscalation:
		out = append(out, "alarm_clock")
	case plugin.NotifyTest:
		out = append(out, "test_tube")
	case plugin.NotifyReport:
		out = append(out, "bar_chart")
	}
	if bySeverity && len(n.Events) > 0 {
		if t, ok := severityTags[n.MaxSeverity()]; ok {
			out = append(out, t)
		}
	}
	return out
}

// click returns the URL opened when the notification is tapped: the event itself for a
// single event, otherwise the notification link, otherwise the first event link.
func click(n *plugin.Notification) string {
	if len(n.Events) == 1 && validURL(n.Events[0].Link) {
		return n.Events[0].Link
	}
	if validURL(n.Link) {
		return n.Link
	}
	for _, e := range n.Events {
		if validURL(e.Link) {
			return e.Link
		}
	}
	return ""
}

// render builds the message text within maxMessageBytes.
func render(n *plugin.Notification, md bool, loc *time.Location) string {
	var parts []string
	used := 0
	if body := strings.TrimSpace(strings.ReplaceAll(n.Body, "\r\n", "\n")); body != "" {
		// The body of reports and test messages is Markdown already; in plain mode it is
		// sent unchanged, Markdown is readable as text.
		body = clipBytes(body, maxMessageBytes-footerReserve)
		parts = append(parts, body)
		used = len(body)
	}
	shown := 0
	for _, e := range n.Events {
		if shown == maxEvents {
			break
		}
		blk := eventBlock(e, md, loc)
		if used+len(blk)+2 > maxMessageBytes-footerReserve {
			break
		}
		parts = append(parts, blk)
		used += len(blk) + 2
		shown++
	}
	if more := len(n.Events) - shown; more > 0 {
		parts = append(parts, moreLine(more, n.Link, md))
	} else if len(n.Events) > 1 {
		if l := linkLine("In NetScope öffnen", n.Link, md); l != "" {
			parts = append(parts, l)
		}
	}
	msg := strings.Join(parts, "\n\n")
	if msg == "" {
		msg = title(n)
		if md {
			msg = escapeMD(msg)
		}
	}
	return msg
}

func eventBlock(e plugin.EventView, md bool, loc *time.Location) string {
	t := clip(oneLine(e.Title), maxEventRunes)
	if t == "" {
		t = clip(oneLine(e.Label), maxEventRunes)
	}
	if t == "" {
		t = e.Type
	}
	var info []string
	if name := clip(oneLine(e.DeviceName), maxNameRunes); name != "" {
		info = append(info, name)
	}
	if ip := clip(oneLine(e.DeviceIP), 64); ip != "" && ip != e.DeviceName {
		info = append(info, ip)
	}
	if site := clip(oneLine(e.Site), maxNameRunes); site != "" {
		info = append(info, "Standort "+site)
	}
	if !e.At.IsZero() {
		info = append(info, e.At.In(loc).Format("02.01. 15:04"))
	}
	msg := clip(e.Message, maxMessageRunes)
	var marks []string
	if e.Escalated {
		marks = append(marks, "⏰ eskaliert")
	}
	if e.Acknowledged {
		marks = append(marks, "✅ quittiert")
	}

	var lines []string
	if md {
		lines = append(lines, "**"+escapeMD(t)+"** ("+escapeMD(e.Severity.Label())+")")
		if len(info) > 0 {
			lines = append(lines, escapeMD(strings.Join(info, " · ")))
		}
		if msg != "" {
			lines = append(lines, escapeMD(msg))
		}
		var tail []string
		if validURL(e.Link) {
			tail = append(tail, "[Öffnen]("+mdURL(e.Link)+")")
		}
		tail = append(tail, marks...)
		if len(tail) > 0 {
			lines = append(lines, strings.Join(tail, " · "))
		}
		// Two trailing spaces force a line break in Markdown.
		return strings.Join(lines, "  \n")
	}
	lines = append(lines, "["+e.Severity.Label()+"] "+clean(t))
	if len(info) > 0 {
		lines = append(lines, clean(strings.Join(info, " · ")))
	}
	if msg != "" {
		lines = append(lines, clean(msg))
	}
	if len(marks) > 0 {
		lines = append(lines, strings.Join(marks, " · "))
	}
	if validURL(e.Link) {
		lines = append(lines, e.Link)
	}
	return strings.Join(lines, "\n")
}

func moreLine(more int, link string, md bool) string {
	s := "… und 1 weiteres Ereignis"
	if more > 1 {
		s = fmt.Sprintf("… und %d weitere Ereignisse", more)
	}
	if !md {
		if validURL(link) {
			s += ": " + link
		}
		return s
	}
	s = escapeMD(s)
	if validURL(link) {
		s += " – [Alle anzeigen](" + mdURL(link) + ")"
	}
	return s
}

func linkLine(text, link string, md bool) string {
	if !validURL(link) {
		return ""
	}
	if md {
		return "[" + escapeMD(text) + "](" + mdURL(link) + ")"
	}
	return text + ": " + link
}

// escapeMD escapes text for CommonMark/GFM so that event data never turns into
// formatting: inline markers are backslash-escaped everywhere, block markers at the
// start of a line; line breaks become hard breaks.
func escapeMD(s string) string {
	lines := strings.Split(clean(s), "\n")
	for i, line := range lines {
		lines[i] = escapeMDLine(strings.TrimLeft(line, " \t"))
	}
	return strings.Join(lines, "  \n")
}

func escapeMDLine(line string) string {
	var b strings.Builder
	for i, r := range line {
		switch {
		case strings.ContainsRune("\\`*_[]<>~|#", r):
			b.WriteByte('\\')
		case i == 0 && strings.ContainsRune("-+=", r):
			b.WriteByte('\\')
		case (r == '.' || r == ')') && allDigits(line[:i]) &&
			(i+1 == len(line) || line[i+1] == ' ' || line[i+1] == '\t'):
			// "1. " or "1) " would start an ordered list
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

// mdURL makes a URL safe as a Markdown link destination.
func mdURL(u string) string {
	return strings.NewReplacer("(", "%28", ")", "%29", "<", "%3C", ">", "%3E").Replace(u)
}

func validURL(s string) bool {
	if s == "" || len(s) > maxURLLen || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// clean removes invalid UTF-8 and control characters except newline and tab.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, strings.ToValidUTF8(s, ""))
}

// oneLine collapses all whitespace (including line breaks) to single spaces.
func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

// clip shortens s to at most n runes (adding "…").
func clip(s string, n int) string {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	return strings.TrimSpace(string([]rune(s)[:n-1])) + "…"
}

// clipBytes shortens s to at most n bytes without splitting a rune (adding "…").
func clipBytes(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n - len("…")
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}
