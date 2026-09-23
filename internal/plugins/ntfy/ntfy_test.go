package ntfy

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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

func publishContext(t *testing.T, settings map[string]any, creds plugintest.Creds) *plugin.PublishContext {
	t.Helper()
	vals, err := (&Plugin{}).Schema().Validate(settings, nil, nil)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	if creds == nil {
		creds = plugintest.Creds{}
	}
	return &plugin.PublishContext{
		PluginID: "ntfy",
		Settings: plugin.NewSettings(vals),
		Log:      slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Creds:    creds,
		Env:      plugin.Env{Location: time.UTC},
	}
}

// request is the last request seen by the fake ntfy server.
type request struct {
	path   string
	header http.Header
	msg    map[string]any
	count  int // number of requests so far
}

type server struct {
	mu     sync.Mutex
	last   request
	status int
	resp   string
}

func (s *server) get() request {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.last
}

func newServer(t *testing.T, status int, resp string) (*server, *httptest.Server) {
	s := &server{status: status, resp: resp}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		req := request{path: r.URL.Path, header: r.Header.Clone()}
		if err := json.Unmarshal(b, &req.msg); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		if r.Method != http.MethodPost {
			t.Errorf("method %s", r.Method)
		}
		s.mu.Lock()
		req.count = s.last.count + 1
		s.last = req
		s.mu.Unlock()
		w.WriteHeader(s.status)
		_, _ = io.WriteString(w, s.resp)
	}))
	t.Cleanup(srv.Close)
	return s, srv
}

func sample() *plugin.Notification {
	at := time.Date(2026, 9, 22, 12, 3, 0, 0, time.UTC)
	return &plugin.Notification{
		ID: 8, Kind: plugin.NotifyEvent, Priority: plugin.PrioHigh, Title: "2 Ereignisse auf nas_01",
		Link: "http://ui/events?n=8",
		Events: []plugin.EventView{
			{ID: 1, Severity: plugin.SevMedium, Label: "Port neu", Title: "Neuer Port 22/tcp auf nas_01",
				Message: "OpenSSH *9.6* [beta]", At: at, DeviceName: "nas_01", DeviceIP: "192.168.8.10",
				Link: "http://ui/events/1"},
			{ID: 2, Severity: plugin.SevCritical, Title: "CVE-2024-6387 (9.8)", Message: "1. Zeile\n- Punkt",
				DeviceIP: "192.168.8.10", Link: "http://ui/events/(2)", Escalated: true},
		},
	}
}

func TestRegistered(t *testing.T) {
	p, ok := plugin.Get("ntfy")
	if !ok {
		t.Fatal("ntfy not registered")
	}
	info := p.Info()
	if info.Kind != plugin.KindPublisher || info.DefaultEnabled || info.Targets != plugin.TargetNone {
		t.Fatalf("info: %+v", info)
	}
	d := p.Schema().Defaults()
	if d["server"] != "https://ntfy.sh" || d["markdown"] != true || d["tags_by_severity"] != true || d["timeout"] != "10s" {
		t.Fatalf("defaults: %v", d)
	}
}

func TestPublishMarkdown(t *testing.T) {
	s, srv := newServer(t, http.StatusOK, `{"id":"abc","event":"message"}`)
	pc := publishContext(t, map[string]any{"server": srv.URL + "/", "topic": "netscope-test", "token": "tk_secret123"}, nil)
	if err := (&Plugin{}).Publish(context.Background(), pc, sample()); err != nil {
		t.Fatal(err)
	}
	r := s.get()
	if r.count != 1 || r.path != "/" {
		t.Fatalf("%d requests, path %q", r.count, r.path)
	}
	for k, want := range map[string]string{
		"Authorization": "Bearer tk_secret123",
		"Content-Type":  "application/json",
		"User-Agent":    "NetScope/1.0",
	} {
		if got := r.header.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	wantMsg := "**Neuer Port 22/tcp auf nas\\_01** (Mittel)  \n" +
		"nas\\_01 · 192.168.8.10 · 22.09. 12:03  \n" +
		"OpenSSH \\*9.6\\* \\[beta\\]  \n" +
		"[Öffnen](http://ui/events/1)\n\n" +
		"**CVE-2024-6387 (9.8)** (Kritisch)  \n" +
		"192.168.8.10  \n" +
		"1\\. Zeile  \n" +
		"\\- Punkt  \n" +
		"[Öffnen](http://ui/events/%282%29) · ⏰ eskaliert\n\n" +
		"[In NetScope öffnen](http://ui/events?n=8)"
	want := map[string]any{
		"topic":    "netscope-test",
		"title":    "2 Ereignisse auf nas_01",
		"message":  wantMsg,
		"priority": float64(4),
		"tags":     []any{"rotating_light"},
		"click":    "http://ui/events?n=8",
		"markdown": true,
	}
	if len(r.msg) != len(want) {
		t.Errorf("keys: %v", r.msg)
	}
	for k, v := range want {
		if fmt.Sprint(r.msg[k]) != fmt.Sprint(v) {
			t.Errorf("%s = %q\nwant %q", k, r.msg[k], v)
		}
	}
}

func TestPublishPlainAndBasicAuth(t *testing.T) {
	s, srv := newServer(t, http.StatusOK, "{}")
	creds := plugintest.Creds{5: {ID: 5, Name: "ntfy", Type: plugin.CredPassword,
		Public: map[string]string{"username": "alice"}, Secret: map[string]string{"password": "pä$$wort"}}}
	pc := publishContext(t, map[string]any{
		"server": srv.URL + "/ntfy", "topic": "alerts", "credential": 5, "markdown": false, "tags_by_severity": false,
	}, creds)
	n := sample()
	n.Kind = plugin.NotifyEscalation
	if err := (&Plugin{}).Publish(context.Background(), pc, n); err != nil {
		t.Fatal(err)
	}
	r := s.get()
	if r.path != "/ntfy/" {
		t.Errorf("path %q", r.path)
	}
	if got, want := r.header.Get("Authorization"), "Basic "+base64.StdEncoding.EncodeToString([]byte("alice:pä$$wort")); got != want {
		t.Errorf("Authorization %q, want %q", got, want)
	}
	wantMsg := "[Mittel] Neuer Port 22/tcp auf nas_01\n" +
		"nas_01 · 192.168.8.10 · 22.09. 12:03\n" +
		"OpenSSH *9.6* [beta]\n" +
		"http://ui/events/1\n\n" +
		"[Kritisch] CVE-2024-6387 (9.8)\n" +
		"192.168.8.10\n" +
		"1. Zeile\n- Punkt\n" +
		"⏰ eskaliert\n" +
		"http://ui/events/(2)\n\n" +
		"In NetScope öffnen: http://ui/events?n=8"
	if r.msg["message"] != wantMsg {
		t.Errorf("message:\n%q\nwant\n%q", r.msg["message"], wantMsg)
	}
	if r.msg["title"] != "Eskalation – nicht quittiert: 2 Ereignisse auf nas_01" {
		t.Errorf("title %q", r.msg["title"])
	}
	if fmt.Sprint(r.msg["tags"]) != "[alarm_clock]" {
		t.Errorf("tags %v", r.msg["tags"])
	}
	if _, ok := r.msg["markdown"]; ok {
		t.Errorf("markdown key present in plain mode")
	}

	// The token wins over the credential.
	pc = publishContext(t, map[string]any{"server": srv.URL, "topic": "alerts", "credential": 5, "token": "tk_x"}, creds)
	if err := (&Plugin{}).Publish(context.Background(), pc, n); err != nil {
		t.Fatal(err)
	}
	r = s.get()
	if r.header.Get("Authorization") != "Bearer tk_x" {
		t.Errorf("Authorization %q", r.header.Get("Authorization"))
	}
}

func TestCredentialErrors(t *testing.T) {
	_, srv := newServer(t, http.StatusOK, "{}")
	creds := plugintest.Creds{
		1: {ID: 1, Name: "ssh", Type: plugin.CredSSH, Secret: map[string]string{"password": "x"}},
		2: {ID: 2, Name: "ohne-user", Type: plugin.CredPassword, Secret: map[string]string{"password": "geheim123"}},
	}
	for id, want := range map[int]string{1: "erwartet", 2: "Benutzername fehlt", 3: "kein Credential"} {
		pc := publishContext(t, map[string]any{"server": srv.URL, "topic": "t", "credential": id}, creds)
		err := (&Plugin{}).Publish(context.Background(), pc, sample())
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("credential %d: err = %v, want %q", id, err, want)
		}
		if err != nil && strings.Contains(err.Error(), "geheim123") {
			t.Errorf("secret leaked: %v", err)
		}
	}
}

func TestPriorityTagsClick(t *testing.T) {
	for p, want := range map[plugin.Priority]int{plugin.PrioLow: 2, plugin.PrioNormal: 3, plugin.PrioHigh: 4, plugin.PrioUrgent: 5, "": 3} {
		if got := priority(p); got != want {
			t.Errorf("priority(%q) = %d, want %d", p, got, want)
		}
	}
	for sev, want := range map[plugin.Severity]string{
		plugin.SevCritical: "rotating_light", plugin.SevHigh: "warning", plugin.SevMedium: "large_orange_diamond",
		plugin.SevLow: "large_blue_diamond", plugin.SevInfo: "information_source",
	} {
		n := &plugin.Notification{Kind: plugin.NotifyEvent, Events: []plugin.EventView{{Severity: sev}}}
		if got := tags(n, true); len(got) != 1 || got[0] != want {
			t.Errorf("tags(%s) = %v, want %s", sev, got, want)
		}
	}
	if got := tags(&plugin.Notification{Kind: plugin.NotifyReport}, true); fmt.Sprint(got) != "[bar_chart]" {
		t.Errorf("report tags %v", got)
	}
	if got := tags(&plugin.Notification{Kind: plugin.NotifyTest}, true); fmt.Sprint(got) != "[test_tube]" {
		t.Errorf("test tags %v", got)
	}

	n := sample()
	if got := click(n); got != n.Link {
		t.Errorf("click (several events) = %q", got)
	}
	n.Events = n.Events[:1]
	if got := click(n); got != "http://ui/events/1" {
		t.Errorf("click (single event) = %q", got)
	}
	n = sample()
	n.Link = ""
	if got := click(n); got != "http://ui/events/1" {
		t.Errorf("click (no notification link) = %q", got)
	}
	if got := click(&plugin.Notification{Link: "javascript:alert(1)"}); got != "" {
		t.Errorf("click accepted %q", got)
	}
}

func TestMessageSizeLimit(t *testing.T) {
	n := &plugin.Notification{Kind: plugin.NotifyEvent, Title: "Viele", Link: "http://ui/events?n=1"}
	for i := range 60 {
		n.Events = append(n.Events, plugin.EventView{
			Severity: plugin.SevLow, Title: fmt.Sprintf("Ereignis %02d", i),
			Message: strings.Repeat("äöü ", 200), Link: fmt.Sprintf("http://ui/events/%d", i),
		})
	}
	for _, md := range []bool{true, false} {
		msg := render(n, md, time.UTC)
		if len(msg) > maxMessageBytes {
			t.Errorf("md=%v: message has %d bytes", md, len(msg))
		}
		shown := strings.Count(msg, "Ereignis ")
		wantMore := fmt.Sprintf("… und %d weitere Ereignisse", 60-shown)
		if shown == 0 || shown == 60 || !strings.Contains(msg, wantMore) || !strings.Contains(msg, "http://ui/events?n=1") {
			t.Errorf("md=%v: %d shown, message ends with %q", md, shown, msg[max(len(msg)-120, 0):])
		}
	}
	// Report bodies are clipped at a rune boundary.
	body := render(&plugin.Notification{Kind: plugin.NotifyReport, Title: "Bericht", Body: strings.Repeat("ä", 5000)}, true, time.UTC)
	if len(body) > maxMessageBytes || !strings.HasSuffix(body, "…") || !strings.HasPrefix(body, "ää") {
		t.Errorf("body clipping: %d bytes", len(body))
	}
	// Without events and body the title is the message.
	if got := render(&plugin.Notification{Kind: plugin.NotifyTest, Title: "Test *1*"}, true, time.UTC); got != `Test \*1\*` {
		t.Errorf("empty message = %q", got)
	}
}

func TestEscapeMD(t *testing.T) {
	cases := map[string]string{
		"my_host *x* [a](b) <tag> `c` ~d~ | #1": `my\_host \*x\* \[a\](b) \<tag\> \` + "`" + `c\` + "`" + ` \~d\~ \| \#1`,
		"- Liste":                               `\- Liste`,
		"+ plus":                                `\+ plus`,
		"===":                                   `\===`,
		"12. Punkt":                             `12\. Punkt`,
		"3) Punkt":                              `3\) Punkt`,
		"192.168.8.1 antwortet nicht":           "192.168.8.1 antwortet nicht",
		"    eingerückt":                        "eingerückt",
		"a\nb":                                  "a  \nb",
		`C:\pfad`:                               `C:\\pfad`,
	}
	for in, want := range cases {
		if got := escapeMD(in); got != want {
			t.Errorf("escapeMD(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestErrors(t *testing.T) {
	cases := []struct {
		status int
		resp   string
		header map[string]string
		want   []string
	}{
		{http.StatusForbidden, `{"code":40301,"http":403,"error":"forbidden","link":"https://ntfy.sh/docs/publish/#authentication"}`, nil,
			[]string{"HTTP 403 Forbidden", "forbidden (Code 40301)", "Token bzw. Zugangsdaten"}},
		{http.StatusTooManyRequests, `{"code":42901,"http":429,"error":"limit reached: too many requests"}`, map[string]string{"Retry-After": "60"},
			[]string{"429", "Ratenlimit", "Retry-After: 60"}},
		{http.StatusInternalServerError, "kaputt\n\n  sehr kaputt", nil, []string{"HTTP 500", "kaputt sehr kaputt"}},
	}
	for _, tc := range cases {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for k, v := range tc.header {
				w.Header().Set(k, v)
			}
			w.WriteHeader(tc.status)
			_, _ = io.WriteString(w, tc.resp)
		}))
		pc := publishContext(t, map[string]any{"server": srv.URL, "topic": "t", "token": "tk_verysecret"}, nil)
		err := (&Plugin{}).Publish(context.Background(), pc, sample())
		srv.Close()
		if err == nil {
			t.Fatalf("%d: expected error", tc.status)
		}
		for _, w := range tc.want {
			if !strings.Contains(err.Error(), w) {
				t.Errorf("%d: error %q lacks %q", tc.status, err, w)
			}
		}
		if strings.Contains(err.Error(), "tk_verysecret") {
			t.Errorf("token leaked: %v", err)
		}
	}

	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	pc := publishContext(t, map[string]any{"server": u, "topic": "t"}, nil)
	if err := (&Plugin{}).Publish(context.Background(), pc, sample()); err == nil || !strings.Contains(err.Error(), "nicht erreichbar") {
		t.Fatalf("err = %v", err)
	}
}

func TestTopicValidation(t *testing.T) {
	p := &Plugin{}
	for topic, ok := range map[string]bool{"netscope": true, "a-b_C9": true, "": false, "mit leerzeichen": false,
		"a/b": false, strings.Repeat("x", 65): false} {
		_, err := p.Schema().Validate(map[string]any{"topic": topic}, nil, nil)
		if (err == nil) != ok {
			t.Errorf("topic %q: err = %v, want ok=%v", topic, err, ok)
		}
	}
}
