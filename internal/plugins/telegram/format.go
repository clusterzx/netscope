package telegram

import (
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"netscope/internal/plugin"
)

const (
	// maxMessageLen is Telegram's limit for a text message. Telegram counts UTF-16 code
	// units after entity parsing; the raw MarkdownV2 text is always at least as long,
	// so measuring the raw text is a safe upper bound.
	maxMessageLen = 4096
	// partReserve is kept free in every message for the "Teil i/n" marker.
	partReserve = 32
	// maxEvents is the number of events listed per notification.
	maxEvents = 20

	maxTitleRunes   = 200
	maxMessageRunes = 800
	maxNameRunes    = 100
	maxURLLen       = 300
	// maxBodyPieceRunes bounds one piece of a report body; escaping at most doubles it.
	maxBodyPieceRunes = 1500
)

// mdSpecial are the characters that must be escaped in MarkdownV2 text.
const mdSpecial = "_*[]()~`>#+-=|{}.!\\"

// escape escapes text for MarkdownV2 (outside of links, code and pre entities). Invalid
// UTF-8 and control characters except newline and tab are removed.
func escape(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	for _, r := range clean(s) {
		if strings.ContainsRune(mdSpecial, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// escapeURL escapes the URL part of an inline link: only ")" and "\" are escaped.
func escapeURL(s string) string {
	return strings.NewReplacer(`\`, `\\`, `)`, `\)`).Replace(s)
}

// link returns an inline link or "" if u is not an absolute http(s) URL.
func link(text, u string) string {
	if !validURL(u) {
		return ""
	}
	return "[" + escape(text) + "](" + escapeURL(u) + ")"
}

func validURL(s string) bool {
	if s == "" || len(s) > maxURLLen || strings.ContainsAny(s, " \t\r\n") {
		return false
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

func clean(s string) string {
	s = strings.ToValidUTF8(s, "")
	return strings.Map(func(r rune) rune {
		if r == '\n' || r == '\t' {
			return r
		}
		if r == '\r' || unicode.IsControl(r) {
			return -1
		}
		return r
	}, s)
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

// utf16Len returns the length of s in UTF-16 code units (what Telegram counts).
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		n += utf16.RuneLen(r)
	}
	return n
}

func severityEmoji(s plugin.Severity) string {
	switch s {
	case plugin.SevCritical:
		return "🔴"
	case plugin.SevHigh:
		return "🟠"
	case plugin.SevMedium:
		return "🟡"
	case plugin.SevLow:
		return "🔵"
	}
	return "⚪"
}

func headerEmoji(n *plugin.Notification) string {
	switch n.Kind {
	case plugin.NotifyTest:
		return "🧪"
	case plugin.NotifyReport:
		return "📊"
	}
	if n.Priority == plugin.PrioUrgent {
		return "🚨"
	}
	if len(n.Events) > 0 {
		return severityEmoji(n.MaxSeverity())
	}
	return "🔔"
}

func title(n *plugin.Notification) string {
	if t := clip(oneLine(n.Title), maxTitleRunes); t != "" {
		return t
	}
	return "NetScope"
}

func countLabel(n int) string {
	if n == 1 {
		return "1 Ereignis"
	}
	return fmt.Sprintf("%d Ereignisse", n)
}

// segment is a part of a message that is never split; sep is written before it.
type segment struct {
	sep  string
	text string
}

// render formats a notification as one or more MarkdownV2 messages.
func render(n *plugin.Notification, loc *time.Location) []string {
	if loc == nil {
		loc = time.Local
	}
	var head strings.Builder
	if n.Kind == plugin.NotifyEscalation {
		head.WriteString("⏰ *" + escape("Eskalation – nicht quittiert") + "*\n")
	}
	head.WriteString(headerEmoji(n) + " *" + escape(title(n)) + "*")
	var meta []string
	if n.RuleName != "" {
		meta = append(meta, "Regel: "+clip(oneLine(n.RuleName), maxNameRunes))
	}
	if len(n.Events) > 1 {
		meta = append(meta, countLabel(len(n.Events)))
	}
	if len(meta) > 0 {
		head.WriteString("\n_" + escape(strings.Join(meta, " · ")) + "_")
	}
	cont := headerEmoji(n) + " *" + escape(clip(title(n), 80)) + "* _" + escape("(Fortsetzung)") + "_"

	var segs []segment
	for i, piece := range bodyPieces(n.Body) {
		sep := "\n"
		if i == 0 {
			sep = "\n\n"
		}
		segs = append(segs, segment{sep: sep, text: piece})
	}
	shown, more := n.Events, 0
	if len(shown) > maxEvents {
		shown, more = shown[:maxEvents], len(shown)-maxEvents
	}
	for _, e := range shown {
		segs = append(segs, segment{sep: "\n\n", text: eventBlock(e, loc)})
	}
	if f := footer(n, more); f != "" {
		segs = append(segs, segment{sep: "\n\n", text: f})
	}
	return pack(head.String(), cont, segs, maxMessageLen-partReserve)
}

// bodyPieces escapes a report/test body line by line; overlong lines are split so that
// every piece fits into a message.
func bodyPieces(body string) []string {
	body = strings.Trim(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	if strings.TrimSpace(body) == "" {
		return nil
	}
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimRight(line, " \t")
		rs := []rune(line)
		for len(rs) > maxBodyPieceRunes {
			out = append(out, escape(string(rs[:maxBodyPieceRunes])))
			rs = rs[maxBodyPieceRunes:]
		}
		out = append(out, escape(string(rs)))
	}
	return out
}

func eventBlock(e plugin.EventView, loc *time.Location) string {
	var b strings.Builder
	t := clip(oneLine(e.Title), maxTitleRunes)
	if t == "" {
		t = clip(oneLine(e.Label), maxTitleRunes)
	}
	if t == "" {
		t = e.Type
	}
	b.WriteString(severityEmoji(e.Severity) + " *" + escape(t) + "*")

	var meta []string
	if e.Label != "" && e.Label != t {
		meta = append(meta, clip(oneLine(e.Label), maxNameRunes))
	}
	meta = append(meta, e.Severity.Label())
	if !e.At.IsZero() {
		meta = append(meta, e.At.In(loc).Format("02.01. 15:04"))
	}
	b.WriteString("\n_" + escape(strings.Join(meta, " · ")) + "_")

	if dev := deviceLine(e); dev != "" {
		b.WriteString("\n🖥 " + dev)
	}
	if msg := clip(e.Message, maxMessageRunes); msg != "" {
		b.WriteString("\n" + escape(msg))
	}
	var tail []string
	if l := link("Öffnen", e.Link); l != "" {
		tail = append(tail, l)
	}
	if e.Escalated {
		tail = append(tail, "⏰ "+escape("eskaliert"))
	}
	if e.Acknowledged {
		tail = append(tail, "✅ "+escape("quittiert"))
	}
	if len(tail) > 0 {
		b.WriteString("\n" + strings.Join(tail, " · "))
	}
	return b.String()
}

func deviceLine(e plugin.EventView) string {
	name := clip(oneLine(e.DeviceName), maxNameRunes)
	ip := clip(oneLine(e.DeviceIP), 64)
	var parts []string
	switch {
	case name != "":
		if l := link(name, e.DeviceLink); l != "" {
			parts = append(parts, l)
		} else {
			parts = append(parts, escape(name))
		}
		if ip != "" && ip != name {
			parts = append(parts, escape(ip))
		}
	case ip != "":
		if l := link(ip, e.DeviceLink); l != "" {
			parts = append(parts, l)
		} else {
			parts = append(parts, escape(ip))
		}
	}
	return strings.Join(parts, " · ")
}

func footer(n *plugin.Notification, more int) string {
	if more > 0 {
		s := "… und 1 weiteres Ereignis"
		if more > 1 {
			s = fmt.Sprintf("… und %d weitere Ereignisse", more)
		}
		out := escape(s)
		if l := link("Alle anzeigen", n.Link); l != "" {
			out += " – " + l
		}
		return out
	}
	if len(n.Events) != 1 {
		return link("In NetScope öffnen", n.Link)
	}
	return ""
}

// pack distributes the segments over as few messages as possible without splitting a
// segment. Continuation messages start with cont; with more than one message every
// message gets a "Teil i/n" marker (space for it is reserved by the caller's limit).
func pack(head, cont string, segs []segment, limit int) []string {
	var msgs []string
	cur, curLen := head, utf16Len(head)
	for _, s := range segs {
		add := utf16Len(s.sep) + utf16Len(s.text)
		if curLen+add <= limit {
			cur += s.sep + s.text
			curLen += add
			continue
		}
		msgs = append(msgs, cur)
		cur = cont + "\n\n" + s.text
		curLen = utf16Len(cur)
	}
	msgs = append(msgs, cur)
	if len(msgs) > 1 {
		for i := range msgs {
			msgs[i] += "\n\n_" + escape(fmt.Sprintf("Teil %d/%d", i+1, len(msgs))) + "_"
		}
	}
	return msgs
}
