package webhook

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

var update = flag.Bool("update", false, "rewrite golden files in testdata/")

type testWriter struct{ t testing.TB }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
}

func publishContext(t *testing.T, p plugin.Plugin, settings map[string]any) *plugin.PublishContext {
	t.Helper()
	vals, err := p.Schema().Validate(settings, nil, nil)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	s := plugin.NewSettings(vals)
	if v, ok := p.(plugin.SettingsValidator); ok {
		if err := v.ValidateSettings(s); err != nil {
			t.Fatalf("ValidateSettings: %v", err)
		}
	}
	return &plugin.PublishContext{
		PluginID: p.Info().ID,
		Settings: s,
		Log:      slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Creds:    plugintest.Creds{},
		Env:      plugin.Env{PublicURL: "http://192.168.8.123:8080", Location: time.UTC},
	}
}

// sampleNotification is also the source of the example in docs/PUBLISHERS.md.
func sampleNotification() *plugin.Notification {
	at := time.Date(2026, 9, 22, 12, 3, 4, 0, time.UTC)
	return &plugin.Notification{
		ID:        42,
		Kind:      plugin.NotifyEvent,
		Priority:  plugin.PrioHigh,
		Title:     "2 neue Ereignisse auf nas",
		RuleID:    3,
		RuleName:  "Neuer Port auf bekanntem Gerät",
		Link:      "http://192.168.8.123:8080/events?notification=42",
		CreatedAt: at.Add(30 * time.Second),
		Events: []plugin.EventView{
			{
				ID: 1001, Type: plugin.EvPortOpened, Label: "Port neu", Category: "port",
				Severity: plugin.SevMedium, Title: "Neuer Port 22/tcp (ssh) auf nas",
				Message: "OpenSSH 9.6p1 Ubuntu 3ubuntu13", At: at,
				DeviceID: 17, DeviceName: "nas", DeviceIP: "192.168.8.10", DeviceMAC: "aa:bb:cc:dd:ee:ff",
				Link:       "http://192.168.8.123:8080/events/1001",
				DeviceLink: "http://192.168.8.123:8080/devices/17",
				Payload: map[string]any{
					"device_name": "nas", "device_ip": "192.168.8.10", "device_mac": "aa:bb:cc:dd:ee:ff",
					"device_state": "known", "ip": "192.168.8.10", "port": 22, "proto": "tcp",
					"service": "ssh", "product": "OpenSSH", "version": "9.6p1 Ubuntu 3ubuntu13",
				},
			},
			{
				ID: 1002, Type: plugin.EvCVENew, Label: "Neue Schwachstelle", Category: "vulnerability",
				Severity: plugin.SevCritical, Title: "CVE-2024-6387 (CVSS 8.1) auf nas",
				Message: "OpenSSH: Race Condition im Signal-Handler (regreSSHion)", At: at.Add(2 * time.Second),
				DeviceID: 17, DeviceName: "nas", DeviceIP: "192.168.8.10", DeviceMAC: "aa:bb:cc:dd:ee:ff",
				Link:       "http://192.168.8.123:8080/events/1002",
				DeviceLink: "http://192.168.8.123:8080/devices/17",
				Payload: map[string]any{
					"device_name": "nas", "device_ip": "192.168.8.10", "device_mac": "aa:bb:cc:dd:ee:ff",
					"device_state": "known", "cve": "CVE-2024-6387", "cvss": 8.1,
					"vector":  "CVSS:3.1/AV:N/AC:H/PR:N/UI:N/S:U/C:H/I:H/A:H",
					"product": "openssh", "version": "9.6p1", "cpe": "cpe:2.3:a:openbsd:openssh:9.6:p1:*:*:*:*:*:*",
					"match_type": "range",
				},
			},
		},
	}
}

func TestRegistered(t *testing.T) {
	p, ok := plugin.Get("webhook")
	if !ok {
		t.Fatal("webhook not registered")
	}
	info := p.Info()
	if info.Kind != plugin.KindPublisher || info.DefaultEnabled || info.DefaultSchedule != "" || info.Targets != plugin.TargetNone {
		t.Fatalf("unexpected info: %+v", info)
	}
	if _, ok := p.(plugin.Publisher); !ok {
		t.Fatal("not a Publisher")
	}
	d := p.Schema().Defaults()
	if d["method"] != "POST" || d["timeout"] != "10s" || d["verify_tls"] != true {
		t.Fatalf("defaults: %v", d)
	}
}

func TestBuildPayloadGolden(t *testing.T) {
	sent := time.Date(2026, 9, 22, 12, 3, 40, 0, time.UTC)
	got, err := EncodePayload(BuildPayload(sampleNotification(), sent))
	if err != nil {
		t.Fatal(err)
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, got, "", "  "); err != nil {
		t.Fatal(err)
	}
	pretty.WriteByte('\n')
	golden := filepath.Join("testdata", "payload_v1.json")
	if *update {
		if err := os.WriteFile(golden, pretty.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := plugintest.Fixture(t, "payload_v1.json")
	want = bytes.ReplaceAll(want, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(pretty.Bytes(), want) {
		t.Fatalf("payload differs from %s (run with -update after checking the change):\n%s", golden, pretty.String())
	}
	// HTML characters stay readable (no & escaping) and the body is compact.
	if !bytes.Contains(got, []byte("?notification=42")) || bytes.Contains(got, []byte(`&`)) || bytes.Contains(got, []byte("\n")) {
		t.Fatalf("unexpected encoding: %s", got)
	}
}

func TestBuildPayloadStableShape(t *testing.T) {
	n := &plugin.Notification{
		ID: 7, Kind: plugin.NotifyTest, Priority: plugin.PrioNormal, Title: "Testnachricht",
		Body: "**Hallo** aus NetScope", CreatedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.FixedZone("CET", 3600)),
		Events: []plugin.EventView{{ID: 1, Type: plugin.EvPluginFailed, Severity: plugin.SevMedium, Title: "nmap fehlgeschlagen"}},
	}
	b, err := EncodePayload(BuildPayload(n, time.Time{}))
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"version", "source", "sentAt", "notification", "summary", "events", "text", "body"} {
		if _, ok := m[k]; !ok {
			t.Errorf("missing key %q", k)
		}
	}
	notif := m["notification"].(map[string]any)
	if notif["createdAt"] != "2026-01-02T02:04:05Z" || notif["ruleId"] != float64(0) || notif["link"] != "" {
		t.Errorf("notification: %v", notif)
	}
	ev := m["events"].([]any)[0].(map[string]any)
	if ev["device"] != nil {
		t.Errorf("device should be null: %v", ev["device"])
	}
	if p, ok := ev["payload"].(map[string]any); !ok || len(p) != 0 {
		t.Errorf("payload should be {}: %v", ev["payload"])
	}
	sum := m["summary"].(map[string]any)
	by := sum["bySeverity"].(map[string]any)
	if len(by) != 5 || by["medium"] != float64(1) || sum["maxSeverity"] != "medium" || sum["maxSeverityRank"] != float64(2) {
		t.Errorf("summary: %v", sum)
	}
	if m["body"] != "**Hallo** aus NetScope" || !strings.Contains(m["text"].(string), "nmap fehlgeschlagen") {
		t.Errorf("text/body: %v / %v", m["text"], m["body"])
	}

	// Without events the list is [] (not null).
	b, _ = EncodePayload(BuildPayload(&plugin.Notification{Kind: plugin.NotifyReport, Title: "Wochenbericht"}, time.Now()))
	if !bytes.Contains(b, []byte(`"events":[]`)) || !bytes.Contains(b, []byte(`"maxSeverity":"info"`)) {
		t.Errorf("empty notification: %s", b)
	}
}

func TestSignKnownVector(t *testing.T) {
	// Same vector as in docs/PUBLISHERS.md (computed with openssl dgst -sha256 -hmac).
	got := Sign("geheim", "1758542620", []byte(`{"version":1}`))
	const want = "sha256=03c1a28b2650ebb177d50893f4f563f6e139d4b60a0dcced9699d7d4ca4fa268"
	if got != want {
		t.Fatalf("Sign = %s, want %s", got, want)
	}
}

type recorded struct {
	method, path, query string
	header              http.Header
	body                []byte
}

type recorder struct {
	mu   sync.Mutex
	reqs []recorded
}

func (r *recorder) handler(status int, respBody string) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		b, _ := io.ReadAll(req.Body)
		r.mu.Lock()
		r.reqs = append(r.reqs, recorded{req.Method, req.URL.Path, req.URL.RawQuery, req.Header.Clone(), b})
		r.mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}
}

func (r *recorder) all() []recorded {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]recorded(nil), r.reqs...)
}

func TestPublishSignedPost(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler(http.StatusNoContent, ""))
	defer srv.Close()

	const secret = "k8s7Qm2vX1zR4tY9pL0wE3nB6cH5jD8f"
	p := &Plugin{}
	pc := publishContext(t, p, map[string]any{
		"url":         srv.URL + "/hook?token=abc",
		"headers":     []any{"Authorization: Bearer xyz", "X-Custom:  eins zwei "},
		"hmac_secret": secret,
	})
	before := time.Now().Add(-time.Second)
	if err := p.Publish(context.Background(), pc, sampleNotification()); err != nil {
		t.Fatal(err)
	}
	reqs := rec.all()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	r := reqs[0]
	if r.method != http.MethodPost || r.path != "/hook" || r.query != "token=abc" {
		t.Errorf("request line: %s %s?%s", r.method, r.path, r.query)
	}
	checks := map[string]string{
		"Content-Type":               "application/json",
		"User-Agent":                 "NetScope/1.0",
		"X-NetScope-Event":           "event",
		"X-NetScope-Notification-Id": "42",
		"Authorization":              "Bearer xyz",
		"X-Custom":                   "eins zwei",
	}
	for k, want := range checks {
		if got := r.header.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	ts := r.header.Get("X-NetScope-Timestamp")
	sec, err := strconv.ParseInt(ts, 10, 64)
	if err != nil || sec < before.Unix() || sec > time.Now().Unix()+1 {
		t.Fatalf("timestamp %q", ts)
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "." + string(r.body)))
	want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
	if got := r.header.Get("X-NetScope-Signature"); !hmac.Equal([]byte(got), []byte(want)) {
		t.Fatalf("signature %q, want %q", got, want)
	}

	var pl Payload
	if err := json.Unmarshal(r.body, &pl); err != nil {
		t.Fatal(err)
	}
	if pl.Version != 1 || pl.Source != "netscope" || pl.Notification.ID != 42 || len(pl.Events) != 2 {
		t.Fatalf("payload: %+v", pl)
	}
	sentAt, err := time.Parse(time.RFC3339, pl.SentAt)
	if err != nil || sentAt.Unix() != sec {
		t.Errorf("sentAt %q does not match timestamp %s", pl.SentAt, ts)
	}
	if pl.Summary.MaxSeverity != "critical" || pl.Summary.BySeverity["critical"] != 1 || pl.Summary.BySeverity["medium"] != 1 {
		t.Errorf("summary: %+v", pl.Summary)
	}
	if d := pl.Events[0].Device; d == nil || d.Name != "nas" || d.Link != "http://192.168.8.123:8080/devices/17" {
		t.Errorf("device: %+v", d)
	}
}

func TestPublishPutUnsigned(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewServer(rec.handler(http.StatusOK, `{"ok":true}`))
	defer srv.Close()
	p := &Plugin{}
	pc := publishContext(t, p, map[string]any{"url": srv.URL, "method": "PUT"})
	n := &plugin.Notification{ID: 1, Kind: plugin.NotifyEscalation, Priority: plugin.PrioUrgent, Title: "x"}
	if err := p.Publish(context.Background(), pc, n); err != nil {
		t.Fatal(err)
	}
	r := rec.all()[0]
	if r.method != http.MethodPut {
		t.Errorf("method %s", r.method)
	}
	if r.header.Get("X-NetScope-Signature") != "" || r.header.Get("X-NetScope-Timestamp") != "" {
		t.Error("unsigned request carries signature headers")
	}
	if r.header.Get("X-NetScope-Event") != "escalation" {
		t.Errorf("event header %q", r.header.Get("X-NetScope-Event"))
	}
}

func TestPublishErrorStatus(t *testing.T) {
	rec := &recorder{}
	long := `{"error":"interner Fehler",` + "\n" + `"detail":"` + strings.Repeat("x", 1000) + `"}`
	srv := httptest.NewServer(rec.handler(http.StatusInternalServerError, long))
	defer srv.Close()
	p := &Plugin{}
	pc := publishContext(t, p, map[string]any{"url": srv.URL + "/p/geheimer-pfad?key=supersecret", "hmac_secret": "abcdefgh"})
	err := p.Publish(context.Background(), pc, sampleNotification())
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{"Webhook:", "HTTP 500 Internal Server Error", `{"error":"interner Fehler", "detail":"xxx`} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q lacks %q", msg, want)
		}
	}
	for _, leak := range []string{"supersecret", "geheimer-pfad", "abcdefgh"} {
		if strings.Contains(msg, leak) {
			t.Errorf("error leaks %q: %s", leak, msg)
		}
	}
	if len([]rune(msg)) > 300 {
		t.Errorf("error too long (%d runes)", len([]rune(msg)))
	}
}

func TestPublishRedirectNotFollowed(t *testing.T) {
	var hits int
	var mu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		mu.Unlock()
		http.Redirect(w, r, "https://elsewhere.example/new?secret=1", http.StatusFound)
	}))
	defer srv.Close()
	p := &Plugin{}
	err := p.Publish(context.Background(), publishContext(t, p, map[string]any{"url": srv.URL}), sampleNotification())
	if err == nil || !strings.Contains(err.Error(), "leitet weiter") || !strings.Contains(err.Error(), "https://elsewhere.example") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "secret=1") {
		t.Errorf("redirect target query leaked: %v", err)
	}
	if hits != 1 {
		t.Errorf("%d requests, redirect must not be followed", hits)
	}
}

func TestPublishTLSVerification(t *testing.T) {
	rec := &recorder{}
	srv := httptest.NewTLSServer(rec.handler(http.StatusOK, ""))
	defer srv.Close()
	p := &Plugin{}
	err := p.Publish(context.Background(), publishContext(t, p, map[string]any{"url": srv.URL}), sampleNotification())
	if err == nil || !strings.Contains(err.Error(), "certificate") {
		t.Fatalf("expected certificate error, got %v", err)
	}
	if err := p.Publish(context.Background(), publishContext(t, p, map[string]any{"url": srv.URL, "verify_tls": false}), sampleNotification()); err != nil {
		t.Fatalf("verify_tls=false: %v", err)
	}
	if len(rec.all()) != 1 {
		t.Fatalf("%d requests", len(rec.all()))
	}
}

func TestPublishUnreachableAndTimeout(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	url := srv.URL
	srv.Close()
	p := &Plugin{}
	err := p.Publish(context.Background(), publishContext(t, p, map[string]any{"url": url + "/x?token=topsecret"}), sampleNotification())
	if err == nil || !strings.Contains(err.Error(), "nicht erreichbar") || strings.Contains(err.Error(), "topsecret") {
		t.Fatalf("err = %v", err)
	}

	block := make(chan struct{})
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer slow.Close()
	defer close(block)
	start := time.Now()
	err = p.Publish(context.Background(), publishContext(t, p, map[string]any{"url": slow.URL, "timeout": "1s"}), sampleNotification())
	if err == nil || !strings.Contains(err.Error(), "Zeitüberschreitung") {
		t.Fatalf("err = %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("timeout not respected: %s", d)
	}

	// A cancelled context aborts immediately.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := p.Publish(ctx, publishContext(t, p, map[string]any{"url": slow.URL}), sampleNotification()); err == nil {
		t.Fatal("expected error for cancelled context")
	}
}

func TestValidateSettings(t *testing.T) {
	p := &Plugin{}
	for _, tc := range []struct {
		headers []any
		wantErr string
	}{
		{[]any{"Authorization: Bearer abc", "X-Api-Key: 1"}, ""},
		{[]any{"Content-Type: text/plain"}, "Content-Type"},
		{[]any{"x-netscope-signature: fake"}, "X-Netscope-Signature"},
		{[]any{"Host: example.org"}, "Host"},
		{[]any{"Bad(Name): x"}, "ungültiger Header-Name"},
	} {
		vals, err := p.Schema().Validate(map[string]any{"url": "https://example.org/h", "headers": tc.headers}, nil, nil)
		if err != nil {
			t.Fatalf("%v: schema: %v", tc.headers, err)
		}
		err = p.ValidateSettings(plugin.NewSettings(vals))
		switch {
		case tc.wantErr == "" && err != nil:
			t.Errorf("%v: unexpected error %v", tc.headers, err)
		case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
			t.Errorf("%v: err = %v, want %q", tc.headers, err, tc.wantErr)
		}
	}
	// The schema itself rejects lines without "Name: Wert" and non-http URLs.
	if _, err := p.Schema().Validate(map[string]any{"url": "https://example.org", "headers": []any{"kein header"}}, nil, nil); err == nil {
		t.Error("header without colon accepted")
	}
	if _, err := p.Schema().Validate(map[string]any{"url": "ftp://example.org"}, nil, nil); err == nil {
		t.Error("ftp URL accepted")
	}
	if _, err := p.Schema().Validate(map[string]any{}, nil, nil); err == nil {
		t.Error("missing url accepted")
	}
}
