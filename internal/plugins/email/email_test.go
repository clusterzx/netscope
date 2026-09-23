package email

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net"
	"net/mail"
	"regexp"
	"strings"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

type testWriter struct{ t testing.TB }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

var testCreds = plugintest.Creds{
	1: {ID: 1, Name: "smtp", Type: plugin.CredPassword,
		Public: map[string]string{"username": "netscope"}, Secret: map[string]string{"password": "s3cr3t-Pässwort"}},
	2: {ID: 2, Name: "ohne-user", Type: plugin.CredPassword, Secret: map[string]string{"password": "s3cr3t-Pässwort"}},
	3: {ID: 3, Name: "falsch", Type: plugin.CredPassword,
		Public: map[string]string{"username": "netscope"}, Secret: map[string]string{"password": "wrong-pw-123"}},
	4: {ID: 4, Name: "ssh", Type: plugin.CredSSH, Public: map[string]string{"username": "root"}},
}

func publishContext(t *testing.T, settings map[string]any) *plugin.PublishContext {
	t.Helper()
	p := &Plugin{}
	vals, err := p.Schema().Validate(settings, nil, nil)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	s := plugin.NewSettings(vals)
	if err := p.ValidateSettings(s); err != nil {
		t.Fatalf("ValidateSettings: %v", err)
	}
	return &plugin.PublishContext{
		PluginID: "email",
		Settings: s,
		Log:      slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Creds:    testCreds,
		Env:      plugin.Env{PublicURL: "http://192.168.8.123:8080", Location: time.UTC},
	}
}

func baseSettings(port int, extra map[string]any) map[string]any {
	s := map[string]any{
		"host": "127.0.0.1", "port": port,
		"from": "NetScope Überwachung <netscope@example.org>",
		"to":   []any{"admin@example.org", "Zweiter Empfänger <ops@example.org>"},
		"helo": "netscope.test",
	}
	for k, v := range extra {
		s[k] = v
	}
	return s
}

func sample() *plugin.Notification {
	at := time.Date(2026, 9, 22, 12, 3, 0, 0, time.UTC)
	return &plugin.Notification{
		ID: 11, Kind: plugin.NotifyEvent, Priority: plugin.PrioHigh,
		Title: "Neues Gerät: Küchen-Thermometer <script>", RuleName: "Unbekannte Geräte",
		Link: "http://192.168.8.123:8080/events?notification=11", CreatedAt: at,
		Events: []plugin.EventView{
			{ID: 1, Type: plugin.EvDeviceNew, Label: "Neues Gerät", Severity: plugin.SevMedium,
				Title: "Neues Gerät 192.168.8.77", Message: "Hersteller: Espressif & Co.\nQuelle: arpscan", At: at,
				DeviceID: 5, DeviceName: "esp-küche", DeviceIP: "192.168.8.77",
				Link: "http://192.168.8.123:8080/events/1", DeviceLink: "http://192.168.8.123:8080/devices/5"},
			{ID: 2, Type: plugin.EvCVENew, Label: "Neue Schwachstelle", Severity: plugin.SevCritical,
				Title: "CVE-2024-6387 auf nas", At: at.Add(time.Minute), DeviceName: "nas", DeviceIP: "192.168.8.10",
				Link: "javascript:alert(1)", Escalated: true, Acknowledged: true},
		},
	}
}

func newServer(t *testing.T, s *smtpServer) (*smtpServer, *Plugin) {
	cert, pool := testCert(t)
	if s.user == "" {
		s.user = "netscope"
	}
	s.pass = "s3cr3t-Pässwort"
	s.start(t, cert)
	return s, &Plugin{rootCAs: pool}
}

func TestRegistered(t *testing.T) {
	p, ok := plugin.Get("email")
	if !ok {
		t.Fatal("email not registered")
	}
	info := p.Info()
	if info.Kind != plugin.KindPublisher || info.DefaultEnabled || info.DefaultSchedule != "" || info.Targets != plugin.TargetNone {
		t.Fatalf("info: %+v", info)
	}
	d := p.Schema().Defaults()
	if d["port"] != int64(587) || d["security"] != "starttls" || d["subject_prefix"] != "[NetScope]" ||
		d["timeout"] != "20s" || d["verify_tls"] != true {
		t.Fatalf("defaults: %v", d)
	}
}

func TestSTARTTLSAuthAndMIME(t *testing.T) {
	srv, p := newServer(t, &smtpServer{starttls: true, authMechs: "PLAIN LOGIN", authBeforeTLS: true})
	pc := publishContext(t, baseSettings(srv.port(), map[string]any{"credential": 1}))
	n := sample()
	if err := p.Publish(context.Background(), pc, n); err != nil {
		t.Fatal(err)
	}
	sessions := srv.finish()
	if len(sessions) != 1 {
		t.Fatalf("%d sessions", len(sessions))
	}
	s := sessions[0]
	if got := strings.Join(s.Commands, " "); got != "EHLO STARTTLS EHLO AUTH MAIL RCPT RCPT DATA QUIT" {
		t.Errorf("commands: %s", got)
	}
	if !s.TLS || !s.AuthTLS || s.AuthMech != "PLAIN" || s.AuthUser != "netscope" {
		t.Errorf("session: tls=%v authTLS=%v mech=%s user=%q", s.TLS, s.AuthTLS, s.AuthMech, s.AuthUser)
	}
	if s.Helo != "netscope.test" {
		t.Errorf("HELO name %q", s.Helo)
	}
	if !strings.HasPrefix(s.From, "FROM:<netscope@example.org>") ||
		strings.Join(s.Rcpts, ",") != "TO:<admin@example.org>,TO:<ops@example.org>" {
		t.Errorf("envelope: %s → %v", s.From, s.Rcpts)
	}
	checkMIME(t, s.Data, n)
}

// checkMIME parses the message like a mail client would.
func checkMIME(t *testing.T, data []byte, n *plugin.Notification) {
	t.Helper()
	for i, b := range data {
		if b == '\n' && (i == 0 || data[i-1] != '\r') {
			t.Fatalf("bare LF at byte %d", i)
		}
	}
	for _, line := range strings.Split(string(data), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("line longer than 998 characters: %.80s…", line)
		}
		for _, r := range line {
			if r > 127 {
				t.Fatalf("non-ASCII in message: %q", line)
			}
		}
	}
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	h := msg.Header
	dec := new(mime.WordDecoder)
	rawSubject := h.Get("Subject")
	subject, err := dec.DecodeHeader(rawSubject)
	if err != nil {
		t.Fatal(err)
	}
	if want := "[NetScope] Neues Gerät: Küchen-Thermometer <script>"; subject != want {
		t.Errorf("subject %q, want %q", subject, want)
	}
	if !strings.HasPrefix(rawSubject, "=?utf-8?q?") {
		t.Errorf("subject not RFC 2047 encoded: %q", rawSubject)
	}
	from, err := mail.ParseAddress(h.Get("From"))
	if err != nil || from.Name != "NetScope Überwachung" || from.Address != "netscope@example.org" {
		t.Errorf("From %q → %v %v", h.Get("From"), from, err)
	}
	to, err := mail.ParseAddressList(h.Get("To"))
	if err != nil || len(to) != 2 || to[1].Name != "Zweiter Empfänger" || to[1].Address != "ops@example.org" {
		t.Errorf("To %q → %v %v", h.Get("To"), to, err)
	}
	if _, err := mail.ParseDate(h.Get("Date")); err != nil {
		t.Errorf("Date %q: %v", h.Get("Date"), err)
	}
	if !regexp.MustCompile(`^<[0-9a-z]+\.[0-9a-f]{24}@example\.org>$`).MatchString(h.Get("Message-ID")) {
		t.Errorf("Message-ID %q", h.Get("Message-ID"))
	}
	for k, want := range map[string]string{
		"MIME-Version": "1.0", "Auto-Submitted": "auto-generated", "X-Priority": "2 (High)",
		"Importance": "high", "X-NetScope-Kind": "event", "X-NetScope-Notification-Id": "11",
	} {
		if got := h.Get(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
	mt, params, err := mime.ParseMediaType(h.Get("Content-Type"))
	if err != nil || mt != "multipart/alternative" || params["boundary"] == "" {
		t.Fatalf("Content-Type %q", h.Get("Content-Type"))
	}
	mr := multipart.NewReader(msg.Body, params["boundary"])
	var parts []string
	var bodies []string
	for {
		part, err := mr.NextRawPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if cte := part.Header.Get("Content-Transfer-Encoding"); cte != "quoted-printable" {
			t.Errorf("part %d: Content-Transfer-Encoding %q", len(parts), cte)
		}
		b, err := io.ReadAll(quotedprintable.NewReader(part))
		if err != nil {
			t.Fatal(err)
		}
		parts = append(parts, part.Header.Get("Content-Type"))
		bodies = append(bodies, strings.ReplaceAll(string(b), "\r\n", "\n"))
	}
	if strings.Join(parts, " | ") != "text/plain; charset=utf-8 | text/html; charset=utf-8" {
		t.Fatalf("parts: %v", parts)
	}
	if !strings.Contains(bodies[0], n.PlainText()) {
		t.Errorf("text part does not contain PlainText():\n%s", bodies[0])
	}
	html := bodies[1]
	for _, want := range []string{
		"<title>Neues Gerät: Küchen-Thermometer &lt;script&gt;</title>",
		`<a href="http://192.168.8.123:8080/events/1"`,
		`<a href="http://192.168.8.123:8080/devices/5"`,
		`<a href="http://192.168.8.123:8080/events?notification=11"`,
		"background-color:#b91c1c", "background-color:#a16207", ">Kritisch</span>", ">Mittel</span>",
		"Hersteller: Espressif &amp; Co.<br>Quelle: arpscan",
		"Priorität: Hoch · Regel: Unbekannte Geräte · 2 Ereignisse", "&#9989; quittiert", "22.09.2026 12:04",
	} {
		if !strings.Contains(html, want) {
			t.Errorf("HTML lacks %q", want)
		}
	}
	if strings.Contains(html, "javascript:") || strings.Contains(html, "<script>") {
		t.Errorf("HTML contains unsafe content")
	}
}

func TestLOGINAuthAndImplicitTLS(t *testing.T) {
	srv, p := newServer(t, &smtpServer{implicitTLS: true, authMechs: "LOGIN"})
	pc := publishContext(t, baseSettings(srv.port(), map[string]any{"security": "tls", "port": srv.port(), "credential": 1}))
	if err := p.Publish(context.Background(), pc, sample()); err != nil {
		t.Fatal(err)
	}
	s := srv.finish()[0]
	if got := strings.Join(s.Commands, " "); got != "EHLO AUTH MAIL RCPT RCPT DATA QUIT" {
		t.Errorf("commands: %s", got)
	}
	if !s.TLS || s.AuthMech != "LOGIN" || s.AuthUser != "netscope" {
		t.Errorf("session: %+v", s)
	}
}

func TestSTARTTLSIsEnforced(t *testing.T) {
	srv, p := newServer(t, &smtpServer{starttls: false, authMechs: "PLAIN LOGIN", authBeforeTLS: true})
	pc := publishContext(t, baseSettings(srv.port(), map[string]any{"credential": 1}))
	err := p.Publish(context.Background(), pc, sample())
	if err == nil || !strings.Contains(err.Error(), "kein STARTTLS") {
		t.Fatalf("err = %v", err)
	}
	s := srv.finish()[0]
	for _, c := range s.Commands {
		if c == "AUTH" || c == "MAIL" || c == "DATA" {
			t.Fatalf("client continued without TLS: %v", s.Commands)
		}
	}
}

func TestPlaintextAuthOnlyToLocalhost(t *testing.T) {
	// security=none to 127.0.0.1: AUTH is allowed (localhost exception).
	// Credential 2 has no user name: the from address is used.
	srv, p := newServer(t, &smtpServer{authMechs: "PLAIN", authBeforeTLS: true, user: "netscope@example.org"})
	pc := publishContext(t, baseSettings(srv.port(), map[string]any{"security": "none", "credential": 2}))
	if err := p.Publish(context.Background(), pc, sample()); err != nil {
		t.Fatal(err)
	}
	s := srv.finish()[0]
	if s.TLS || s.AuthTLS || s.AuthUser != "netscope@example.org" {
		t.Errorf("session: %+v", s)
	}

	// Remote hosts: refused before connecting (settings) and during the session.
	vals, err := p.Schema().Validate(map[string]any{
		"host": "smtp.example.org", "security": "none", "credential": 1, "from": "a@example.org", "to": []any{"b@example.org"},
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := p.ValidateSettings(plugin.NewSettings(vals)); err == nil || !strings.Contains(err.Error(), "nur für localhost") {
		t.Errorf("ValidateSettings: %v", err)
	}
	for _, tc := range []struct {
		tls  bool
		host string
		ok   bool
	}{{false, "smtp.example.org", false}, {true, "smtp.example.org", true}, {false, "localhost", true},
		{false, "127.0.0.1", true}, {false, "::1", true}, {false, "192.168.8.1", false}} {
		if got := authAllowed(tc.tls, tc.host); got != tc.ok {
			t.Errorf("authAllowed(%v, %q) = %v", tc.tls, tc.host, got)
		}
	}
}

func TestTLSVerification(t *testing.T) {
	srv, _ := newServer(t, &smtpServer{starttls: true})
	// System roots do not trust the test CA.
	err := (&Plugin{}).Publish(context.Background(), publishContext(t, baseSettings(srv.port(), nil)), sample())
	if err == nil || !strings.Contains(err.Error(), "STARTTLS fehlgeschlagen") || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("err = %v", err)
	}
	if err := (&Plugin{}).Publish(context.Background(), publishContext(t, baseSettings(srv.port(), map[string]any{"verify_tls": false})), sample()); err != nil {
		t.Fatalf("verify_tls=false: %v", err)
	}
	sessions := srv.finish()
	if len(sessions) != 2 || sessions[1].Data == nil {
		t.Fatalf("sessions: %d", len(sessions))
	}
}

func TestAuthErrors(t *testing.T) {
	srv, p := newServer(t, &smtpServer{starttls: true, authMechs: "PLAIN"})
	err := p.Publish(context.Background(), publishContext(t, baseSettings(srv.port(), map[string]any{"credential": 3})), sample())
	if err == nil || !strings.Contains(err.Error(), "Anmeldung fehlgeschlagen") || !strings.Contains(err.Error(), "535") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "wrong-pw-123") {
		t.Fatalf("password leaked: %v", err)
	}
	err = p.Publish(context.Background(), publishContext(t, baseSettings(srv.port(), map[string]any{"credential": 4})), sample())
	if err == nil || !strings.Contains(err.Error(), "erwartet") {
		t.Fatalf("wrong credential type: %v", err)
	}

	noAuth, p2 := newServer(t, &smtpServer{starttls: true})
	err = p2.Publish(context.Background(), publishContext(t, baseSettings(noAuth.port(), map[string]any{"credential": 1})), sample())
	if err == nil || !strings.Contains(err.Error(), "keine Anmeldung") {
		t.Fatalf("server without AUTH: %v", err)
	}
	cram, p3 := newServer(t, &smtpServer{starttls: true, authMechs: "CRAM-MD5"})
	err = p3.Publish(context.Background(), publishContext(t, baseSettings(cram.port(), map[string]any{"credential": 1})), sample())
	if err == nil || !strings.Contains(err.Error(), "CRAM-MD5") {
		t.Fatalf("unsupported mechanism: %v", err)
	}
}

func TestTimeoutAndCancel(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { // never greet
				_, _ = io.Copy(io.Discard, c)
				c.Close()
			}()
		}
	}()
	port := ln.Addr().(*net.TCPAddr).Port
	start := time.Now()
	err = (&Plugin{}).Publish(context.Background(), publishContext(t, baseSettings(port, map[string]any{"timeout": "1s"})), sample())
	if err == nil || !strings.Contains(err.Error(), "Zeitüberschreitung") || time.Since(start) > 5*time.Second {
		t.Fatalf("err = %v after %s", err, time.Since(start))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start = time.Now()
	err = (&Plugin{}).Publish(ctx, publishContext(t, baseSettings(port, map[string]any{"timeout": "60s"})), sample())
	if err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("cancel not respected: %v after %s", err, time.Since(start))
	}
}

func TestBuildMessageEdgeCases(t *testing.T) {
	from, _ := mail.ParseAddress("netscope@example.org")
	to, _ := mail.ParseAddress("admin@example.org")
	n := &plugin.Notification{
		Kind: plugin.NotifyEscalation, Priority: plugin.PrioUrgent,
		Title: strings.Repeat("Größere Störung ", 20), Body: "Zeile 1\n.Zeile mit Punkt\n\nÄnderungen: <b>fett</b>",
	}
	data, err := buildMessage(n, envelope{From: from, To: []*mail.Address{to}, Prefix: "[NS]", Now: time.Now(), Loc: time.UTC})
	if err != nil {
		t.Fatal(err)
	}
	msg, err := mail.ReadMessage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	subject, err := new(mime.WordDecoder).DecodeHeader(msg.Header.Get("Subject"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(subject, "[NS] Eskalation – nicht quittiert: Größere Störung") || len([]rune(subject)) > maxSubjectRunes {
		t.Errorf("subject %q", subject)
	}
	for _, line := range strings.Split(string(data), "\r\n") {
		if len(line) > 998 {
			t.Fatalf("long header line: %d", len(line))
		}
	}
	if msg.Header.Get("X-Priority") != "1 (Highest)" {
		t.Errorf("X-Priority %q", msg.Header.Get("X-Priority"))
	}
	text := plainText(n)
	if !strings.HasPrefix(text, "⏰ ESKALATION – nicht quittiert\n\n") || !strings.Contains(text, ".Zeile mit Punkt") {
		t.Errorf("text part: %q", text)
	}
	html := htmlBody(n, time.UTC)
	if !strings.Contains(html, "Eskalation – nicht quittiert</td>") || !strings.Contains(html, "&lt;b&gt;fett&lt;/b&gt;") {
		t.Errorf("html: %s", html)
	}
}

func TestValidateSettings(t *testing.T) {
	p := &Plugin{}
	cases := []struct {
		settings map[string]any
		ok       bool
	}{
		{map[string]any{"host": "smtp.example.org", "from": "a@example.org", "to": []any{"b@example.org"}}, true},
		{map[string]any{"host": "smtp.example.org", "from": "kein-mail", "to": []any{"b@example.org"}}, false},
		{map[string]any{"host": "smtp.example.org", "from": "a@example.org", "to": []any{}}, false},
		{map[string]any{"host": "smtp.example.org", "from": "a@example.org", "to": []any{"b@example.org", "x"}}, false},
		{map[string]any{"host": "smtp example", "from": "a@example.org", "to": []any{"b@example.org"}}, false},
		{map[string]any{"host": "smtp.example.org", "from": "a@example.org", "to": []any{"b@example.org"}, "security": "ssl"}, false},
		{map[string]any{"host": "localhost", "from": "a@example.org", "to": []any{"b@example.org"}, "security": "none", "credential": 1}, true},
	}
	for _, tc := range cases {
		vals, err := p.Schema().Validate(tc.settings, nil, nil)
		if err == nil {
			err = p.ValidateSettings(plugin.NewSettings(vals))
		}
		if (err == nil) != tc.ok {
			t.Errorf("%v: err = %v, want ok=%v", tc.settings, err, tc.ok)
		}
	}
}
