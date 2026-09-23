package telegram

import (
	"fmt"
	"net/url"
	"strings"
	"testing"
	"time"

	"netscope/internal/plugin"
)

// parseMarkdownV2 checks text against Telegram's MarkdownV2 rules for the entities this
// publisher uses (bold, italic, inline links) and returns the visible text and links.
// Every special character outside of markup must be escaped; inside link URLs only ")"
// and "\" may be escaped.
func parseMarkdownV2(s string) (string, []string, error) {
	var out strings.Builder
	var links []string
	rs := []rune(s)
	bold, italic := false, false
	ctx := func(i int) string {
		lo, hi := max(i-20, 0), min(i+20, len(rs))
		return string(rs[lo:hi])
	}
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch r {
		case '\\':
			if i+1 >= len(rs) {
				return "", nil, fmt.Errorf("dangling backslash at end")
			}
			if rs[i+1] < 1 || rs[i+1] > 126 {
				return "", nil, fmt.Errorf("escaped non-ASCII %q near %q", rs[i+1], ctx(i))
			}
			out.WriteRune(rs[i+1])
			i++
		case '*':
			bold = !bold
		case '_':
			if i+1 < len(rs) && rs[i+1] == '_' {
				return "", nil, fmt.Errorf("underline marker near %q", ctx(i))
			}
			italic = !italic
		case '[':
			var text strings.Builder
			j := i + 1
			for ; j < len(rs) && rs[j] != ']'; j++ {
				if rs[j] == '\\' {
					if j+1 >= len(rs) {
						return "", nil, fmt.Errorf("dangling backslash in link text")
					}
					text.WriteRune(rs[j+1])
					j++
					continue
				}
				if strings.ContainsRune(mdSpecial, rs[j]) {
					return "", nil, fmt.Errorf("unescaped %q in link text near %q", rs[j], ctx(j))
				}
				text.WriteRune(rs[j])
			}
			if j+1 >= len(rs) || rs[j+1] != '(' {
				return "", nil, fmt.Errorf("link without URL near %q", ctx(i))
			}
			var u strings.Builder
			k := j + 2
			for ; k < len(rs) && rs[k] != ')'; k++ {
				if rs[k] == '\\' {
					if k+1 >= len(rs) || (rs[k+1] != ')' && rs[k+1] != '\\') {
						return "", nil, fmt.Errorf("invalid escape in URL near %q", ctx(k))
					}
					u.WriteRune(rs[k+1])
					k++
					continue
				}
				u.WriteRune(rs[k])
			}
			if k >= len(rs) {
				return "", nil, fmt.Errorf("unterminated link URL near %q", ctx(i))
			}
			if text.Len() == 0 {
				return "", nil, fmt.Errorf("empty link text near %q", ctx(i))
			}
			if pu, err := url.Parse(u.String()); err != nil || pu.Host == "" {
				return "", nil, fmt.Errorf("invalid link URL %q", u.String())
			}
			out.WriteString(text.String())
			links = append(links, u.String())
			i = k
		default:
			if strings.ContainsRune(mdSpecial, r) {
				return "", nil, fmt.Errorf("unescaped %q near %q", r, ctx(i))
			}
			out.WriteRune(r)
		}
	}
	if bold || italic {
		return "", nil, fmt.Errorf("unbalanced bold/italic (bold=%v italic=%v)", bold, italic)
	}
	return out.String(), links, nil
}

func mustValid(t *testing.T, msg string) (string, []string) {
	t.Helper()
	plain, links, err := parseMarkdownV2(msg)
	if err != nil {
		t.Fatalf("invalid MarkdownV2: %v\n%s", err, msg)
	}
	if n := utf16Len(msg); n > maxMessageLen {
		t.Fatalf("message has %d UTF-16 units (> %d)", n, maxMessageLen)
	}
	return plain, links
}

func TestEscape(t *testing.T) {
	cases := map[string]string{
		"":                                "",
		"plain text äöü ß":                "plain text äöü ß",
		`_*[]()~` + "`" + `>#+-=|{}.!\`:   `\_\*\[\]\(\)\~\` + "`" + `\>\#\+\-\=\|\{\}\.\!\\`,
		"192.168.8.1":                     `192\.168\.8\.1`,
		"Port 8080/tcp (http-alt) offen!": `Port 8080/tcp \(http\-alt\) offen\!`,
		`C:\temp\new`:                     `C:\\temp\\new`,
		"my_host_name":                    `my\_host\_name`,
		"a\\_b":                           `a\\\_b`,
		"CVSS 9.8 > 9":                    `CVSS 9\.8 \> 9`,
		"Zeile1\nZeile2\tTab":             "Zeile1\nZeile2\tTab",
		"ctrl\x00\x07\r\x1b[31mred":       `ctrl\[31mred`,
		"invalid \xff utf8":               "invalid  utf8",
		"emoji 🔴 & <b>":                   `emoji 🔴 & <b\>`,
		"–—… „Anführung“ · ° € % $ @ : / ?": "–—… „Anführung“ · ° € % $ @ : / ?",
	}
	for in, want := range cases {
		got := escape(in)
		if got != want {
			t.Errorf("escape(%q) = %q, want %q", in, got, want)
			continue
		}
		plain, _, err := parseMarkdownV2(got)
		if err != nil {
			t.Errorf("escape(%q) is not valid MarkdownV2: %v", in, err)
		}
		if want := clean(in); plain != want {
			t.Errorf("round trip of %q = %q, want %q", in, plain, want)
		}
	}
}

func TestEscapeURLAndLink(t *testing.T) {
	cases := map[string]string{
		"http://h/a":            "http://h/a",
		"http://h/a_(b)":        `http://h/a_(b\)`,
		`http://h/a\b`:          `http://h/a\\b`,
		"http://h/?q=a*b&c=[d]": "http://h/?q=a*b&c=[d]",
	}
	for in, want := range cases {
		if got := escapeURL(in); got != want {
			t.Errorf("escapeURL(%q) = %q, want %q", in, got, want)
		}
	}
	l := link("nas_(alt) [1]", "http://h/devices/1_(x)")
	if want := `[nas\_\(alt\) \[1\]](http://h/devices/1_(x\))`; l != want {
		t.Fatalf("link = %q, want %q", l, want)
	}
	plain, links, err := parseMarkdownV2(l)
	if err != nil || plain != "nas_(alt) [1]" || len(links) != 1 || links[0] != "http://h/devices/1_(x)" {
		t.Fatalf("parsed %q %v %v", plain, links, err)
	}
	for _, bad := range []string{"", "javascript:alert(1)", "ftp://h/x", "/relative", "http://h/with space", "http://" + strings.Repeat("a", maxURLLen)} {
		if l := link("x", bad); l != "" {
			t.Errorf("link for %q = %q, want none", bad, l)
		}
	}
}

func TestUTF16Len(t *testing.T) {
	if n := utf16Len("a🔴ä"); n != 4 {
		t.Fatalf("utf16Len = %d, want 4", n)
	}
}

func sample() *plugin.Notification {
	at := time.Date(2026, 9, 22, 12, 3, 0, 0, time.UTC)
	return &plugin.Notification{
		ID: 5, Kind: plugin.NotifyEvent, Priority: plugin.PrioHigh,
		Title:    "2 Ereignisse: Port & CVE auf nas-01",
		RuleName: "Ports (bekannt) + CVE>=9",
		Link:     "http://192.168.8.123:8080/events?n=5",
		Events: []plugin.EventView{
			{
				ID: 1, Type: plugin.EvPortOpened, Label: "Port neu", Severity: plugin.SevMedium,
				Title:   "Neuer Port 8080/tcp (http-alt) auf nas_01!",
				Message: "Server: nginx/1.25 [beta] {x} ~ > # + - = | . !", At: at,
				DeviceName: "nas_01", DeviceIP: "192.168.8.10",
				Link: "http://192.168.8.123:8080/events/1_(a)", DeviceLink: "http://192.168.8.123:8080/devices/17",
			},
			{
				ID: 2, Type: plugin.EvCVENew, Label: "Neue Schwachstelle", Severity: plugin.SevCritical,
				Title: "CVE-2024-6387 auf nas_01", Message: `C:\temp\x`, At: at.Add(time.Minute),
				DeviceName: "nas_01", DeviceIP: "192.168.8.10",
				Link: "http://192.168.8.123:8080/events/2", DeviceLink: "http://192.168.8.123:8080/devices/17",
				Escalated: true, Acknowledged: true,
			},
		},
	}
}

func TestRenderEvents(t *testing.T) {
	msgs := render(sample(), time.UTC)
	if len(msgs) != 1 {
		t.Fatalf("%d messages", len(msgs))
	}
	want := `🔴 *2 Ereignisse: Port & CVE auf nas\-01*
_Regel: Ports \(bekannt\) \+ CVE\>\=9 · 2 Ereignisse_

🟡 *Neuer Port 8080/tcp \(http\-alt\) auf nas\_01\!*
_Port neu · Mittel · 22\.09\. 12:03_
🖥 [nas\_01](http://192.168.8.123:8080/devices/17) · 192\.168\.8\.10
Server: nginx/1\.25 \[beta\] \{x\} \~ \> \# \+ \- \= \| \. \!
[Öffnen](http://192.168.8.123:8080/events/1_(a\))

🔴 *CVE\-2024\-6387 auf nas\_01*
_Neue Schwachstelle · Kritisch · 22\.09\. 12:04_
🖥 [nas\_01](http://192.168.8.123:8080/devices/17) · 192\.168\.8\.10
C:\\temp\\x
[Öffnen](http://192.168.8.123:8080/events/2) · ⏰ eskaliert · ✅ quittiert

[In NetScope öffnen](http://192.168.8.123:8080/events?n=5)`
	if msgs[0] != want {
		t.Fatalf("render mismatch:\n--- got ---\n%s\n--- want ---\n%s", msgs[0], want)
	}
	plain, links := mustValid(t, msgs[0])
	if !strings.Contains(plain, "Server: nginx/1.25 [beta] {x} ~ > # + - = | . !") || len(links) != 5 {
		t.Fatalf("plain %q links %v", plain, links)
	}
}

func TestRenderSingleEventAndEscalation(t *testing.T) {
	n := sample()
	n.Kind = plugin.NotifyEscalation
	n.Priority = plugin.PrioUrgent
	n.RuleName = ""
	n.Events = n.Events[:1]
	n.Title = "Neuer Port auf nas_01"
	msgs := render(n, time.UTC)
	if len(msgs) != 1 {
		t.Fatalf("%d messages", len(msgs))
	}
	if !strings.HasPrefix(msgs[0], "⏰ *Eskalation – nicht quittiert*\n🚨 *Neuer Port auf nas\\_01*\n\n🟡 ") {
		t.Fatalf("escalation header:\n%s", msgs[0])
	}
	// A single event links to itself only (no extra "In NetScope öffnen").
	if strings.Contains(msgs[0], "In NetScope öffnen") {
		t.Fatalf("unexpected footer:\n%s", msgs[0])
	}
	mustValid(t, msgs[0])
}

func TestRenderReportAndTest(t *testing.T) {
	n := &plugin.Notification{
		Kind: plugin.NotifyReport, Priority: plugin.PrioLow, Title: "Wochenbericht KW 38",
		Body: "# Änderungen\r\n\r\n- **3** neue Geräte (IoT)\n- 1 CVE >= 9.0: `CVE-2024-6387`\n| a | b |",
		Link: "https://netscope.example/reports/38",
	}
	msgs := render(n, time.UTC)
	want := "📊 *Wochenbericht KW 38*\n\n*Änderungen*\n\n• *3* neue Geräte \\(IoT\\)\n" +
		"• 1 CVE \\>\\= 9\\.0: \\`CVE\\-2024\\-6387\\`\n\\| a \\| b \\|\n\n" +
		"[In NetScope öffnen](https://netscope.example/reports/38)"
	if len(msgs) != 1 || msgs[0] != want {
		t.Fatalf("report:\n%q\nwant\n%q", msgs, want)
	}
	mustValid(t, msgs[0])

	test := render(&plugin.Notification{Kind: plugin.NotifyTest, Title: "Testnachricht", Body: "Hallo! Alles ok."}, time.UTC)
	if len(test) != 1 || test[0] != "🧪 *Testnachricht*\n\nHallo\\! Alles ok\\." {
		t.Fatalf("test: %q", test)
	}
}

func TestRenderMoreThanMaxEvents(t *testing.T) {
	n := &plugin.Notification{Kind: plugin.NotifyEvent, Title: "Viele Ereignisse", Link: "http://ui/events?n=1"}
	for i := range maxEvents + 5 {
		n.Events = append(n.Events, plugin.EventView{ID: int64(i), Severity: plugin.SevLow, Title: fmt.Sprintf("Ereignis %02d", i)})
	}
	msgs := render(n, time.UTC)
	var all strings.Builder
	for _, m := range msgs {
		plain, _ := mustValid(t, m)
		all.WriteString(plain)
	}
	text := all.String()
	if !strings.Contains(text, fmt.Sprintf("Ereignis %02d", maxEvents-1)) || strings.Contains(text, fmt.Sprintf("Ereignis %02d", maxEvents)) {
		t.Fatalf("event cap not applied:\n%s", text)
	}
	if !strings.HasSuffix(msgs[len(msgs)-1], `… und 5 weitere Ereignisse – [Alle anzeigen](http://ui/events?n=1)`) {
		t.Fatalf("footer:\n%s", msgs[len(msgs)-1])
	}
	n.Events = n.Events[:maxEvents+1]
	n.Link = ""
	msgs = render(n, time.UTC)
	if !strings.HasSuffix(msgs[len(msgs)-1], `… und 1 weiteres Ereignis`) {
		t.Fatalf("footer without link:\n%s", msgs[len(msgs)-1])
	}
}

func TestRenderSplitsAtEventBoundaries(t *testing.T) {
	n := &plugin.Notification{Kind: plugin.NotifyEvent, Title: "Große Sammelmeldung (Batch)", RuleName: "Gesammelt"}
	for i := range maxEvents {
		n.Events = append(n.Events, plugin.EventView{
			ID: int64(i), Severity: plugin.SevHigh, Label: "Port neu",
			Title:   fmt.Sprintf("EVENT-%02d Port %d offen", i, 1000+i),
			Message: strings.Repeat("Dienst-Info (v1.2) ", 40),
			Link:    fmt.Sprintf("http://ui/events/%d", i),
		})
	}
	msgs := render(n, time.UTC)
	if len(msgs) < 3 {
		t.Fatalf("expected a split into several messages, got %d", len(msgs))
	}
	next := 0
	for i, m := range msgs {
		plain, links := mustValid(t, m)
		if want := fmt.Sprintf("Teil %d/%d", i+1, len(msgs)); !strings.HasSuffix(plain, want) {
			t.Errorf("message %d lacks marker %q", i, want)
		}
		if i > 0 && !strings.Contains(strings.SplitN(plain, "\n", 2)[0], "(Fortsetzung)") {
			t.Errorf("message %d lacks continuation header: %q", i, strings.SplitN(plain, "\n", 2)[0])
		}
		// Events appear in order; every event of this message is complete (title, message
		// and its own link are in the same message).
		first := next
		for strings.Contains(plain, fmt.Sprintf("EVENT-%02d ", next)) {
			if strings.Count(plain, fmt.Sprintf("EVENT-%02d ", next)) != 1 {
				t.Errorf("event %d duplicated", next)
			}
			next++
		}
		if next == first {
			t.Fatalf("message %d contains no event", i)
		}
		for e := first; e < next; e++ {
			found := false
			for _, l := range links {
				found = found || l == fmt.Sprintf("http://ui/events/%d", e)
			}
			if !found {
				t.Errorf("message %d: link of event %d missing (event split?)", i, e)
			}
		}
		if strings.Count(plain, "Dienst-Info (v1.2)") != 40*(next-first) {
			t.Errorf("message %d: event messages incomplete", i)
		}
	}
	if next != maxEvents {
		t.Fatalf("found %d of %d events in order", next, maxEvents)
	}
	if testing.Verbose() {
		t.Logf("%d parts, first part:\n%s", len(msgs), msgs[0][:600])
	}
}

func TestRenderLongReportBody(t *testing.T) {
	var body strings.Builder
	for i := range 300 {
		fmt.Fprintf(&body, "- Gerät %03d: 192.168.8.%d (neu) [ok]\n", i, i%255)
	}
	body.WriteString(strings.Repeat("x_", 3000)) // one very long line
	n := &plugin.Notification{Kind: plugin.NotifyReport, Title: "Änderungsbericht", Body: body.String()}
	msgs := render(n, time.UTC)
	if len(msgs) < 4 {
		t.Fatalf("%d messages", len(msgs))
	}
	var all strings.Builder
	for _, m := range msgs {
		plain, _ := mustValid(t, m)
		all.WriteString(plain)
	}
	for _, want := range []string{"• Gerät 000: 192.168.8.0 (neu) [ok]", "• Gerät 299: 192.168.8.44 (neu) [ok]"} {
		if !strings.Contains(all.String(), want) {
			t.Errorf("missing %q", want)
		}
	}
	if got := strings.Count(all.String(), "x_"); got < 2990 {
		t.Errorf("long line truncated: %d of 3000 repetitions", got)
	}
}

// TestRenderWorstCase fills every field to its limit with characters that need
// escaping; every message must still be valid and within Telegram's limit.
func TestRenderWorstCase(t *testing.T) {
	longURL := "http://h/" + strings.Repeat(")", maxURLLen-len("http://h/"))
	ev := plugin.EventView{
		Severity: plugin.SevCritical, Label: strings.Repeat("*", 500), Title: strings.Repeat("_", 1000),
		Message: strings.Repeat(`\`, 5000), DeviceName: strings.Repeat("[", 500), DeviceIP: strings.Repeat(")", 200),
		Link: longURL, DeviceLink: longURL, Escalated: true, Acknowledged: true, At: time.Now(),
	}
	n := &plugin.Notification{
		Kind: plugin.NotifyEscalation, Title: strings.Repeat("!", 1000), RuleName: strings.Repeat("-", 1000),
		Link: longURL, Body: strings.Repeat("🔴.", 5000),
	}
	for range 30 {
		n.Events = append(n.Events, ev)
	}
	msgs := render(n, time.UTC)
	for _, m := range msgs {
		mustValid(t, m)
	}
	head := strings.SplitN(msgs[0], "\n\n", 2)[0]
	if utf16Len(head)+utf16Len(eventBlock(ev, time.UTC))+2 > maxMessageLen-partReserve {
		t.Logf("header and worst-case event do not fit together; header is sent separately")
	}
	cont := headerEmoji(n) + " *" + escape(clip(title(n), 80)) + "* _" + escape("(Fortsetzung)") + "_"
	if l := utf16Len(cont) + 2 + utf16Len(eventBlock(ev, time.UTC)); l > maxMessageLen-partReserve {
		t.Fatalf("worst-case event block does not fit into a continuation message (%d units)", l)
	}
}

func TestPriorityRank(t *testing.T) {
	cases := []struct {
		prio, silentBelow plugin.Priority
		silent            bool
	}{
		{plugin.PrioLow, plugin.PrioNormal, true},
		{plugin.PrioNormal, plugin.PrioNormal, false},
		{plugin.PrioUrgent, plugin.PrioUrgent, false},
		{plugin.PrioHigh, plugin.PrioUrgent, true},
		{plugin.PrioLow, plugin.PrioLow, false},
		{"", plugin.PrioHigh, true},
	}
	for _, c := range cases {
		if got := priorityRank(c.prio) < priorityRank(c.silentBelow); got != c.silent {
			t.Errorf("prio %q silent_below %q: silent=%v, want %v", c.prio, c.silentBelow, got, c.silent)
		}
	}
}
