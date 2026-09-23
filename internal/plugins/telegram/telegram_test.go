package telegram

import (
	"context"
	"encoding/json"
	"errors"
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

const testToken = "123456789:AAHfakeTokenForTests_abcdefghijk"

type testWriter struct{ t testing.TB }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimRight(string(p), "\n"))
	return len(p), nil
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
		PluginID: "telegram",
		Settings: s,
		Log:      slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Creds:    plugintest.Creds{},
		Env:      plugin.Env{PublicURL: "http://192.168.8.123:8080", Location: time.UTC},
	}
}

// fakeAPI is a minimal Bot API server. Responses are consumed per request; when they
// run out, requests succeed.
type fakeAPI struct {
	t         *testing.T
	mu        sync.Mutex
	paths     []string
	reqs      []map[string]any
	times     []time.Time
	responses []func(w http.ResponseWriter)
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	var m map[string]any
	if err := json.Unmarshal(body, &m); err != nil {
		f.t.Errorf("request body is not JSON: %v", err)
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		f.t.Errorf("Content-Type %q", ct)
	}
	if r.Method != http.MethodPost {
		f.t.Errorf("method %s", r.Method)
	}
	f.mu.Lock()
	i := len(f.reqs)
	f.paths = append(f.paths, r.URL.Path)
	f.reqs = append(f.reqs, m)
	f.times = append(f.times, time.Now())
	var resp func(http.ResponseWriter)
	if i < len(f.responses) {
		resp = f.responses[i]
	}
	f.mu.Unlock()
	if resp != nil {
		resp(w)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprintf(w, `{"ok":true,"result":{"message_id":%d}}`, i+1)
}

func (f *fakeAPI) requests() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.reqs...)
}

func (f *fakeAPI) path(i int) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.paths[i]
}

// gap returns the time between request i-1 and request i.
func (f *fakeAPI) gap(i int) time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.times[i].Sub(f.times[i-1])
}

func jsonResp(status int, body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func newAPI(t *testing.T, responses ...func(http.ResponseWriter)) (*fakeAPI, *httptest.Server) {
	f := &fakeAPI{t: t, responses: responses}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	return f, srv
}

func TestRegistered(t *testing.T) {
	p, ok := plugin.Get("telegram")
	if !ok {
		t.Fatal("telegram not registered")
	}
	info := p.Info()
	if info.Kind != plugin.KindPublisher || info.DefaultEnabled || info.DefaultSchedule != "" || info.Targets != plugin.TargetNone {
		t.Fatalf("info: %+v", info)
	}
	d := p.Schema().Defaults()
	if d["api_url"] != defaultAPIURL || d["silent_below"] != "normal" || d["disable_preview"] != true || d["timeout"] != "15s" {
		t.Fatalf("defaults: %v", d)
	}
}

func TestPublishRequest(t *testing.T) {
	f, srv := newAPI(t)
	pc := publishContext(t, map[string]any{
		"bot_token": testToken, "chat_id": "-1001234567890", "message_thread_id": 7,
		"api_url": srv.URL + "/", "silent_below": "high",
	})
	p := &Plugin{partInterval: time.Millisecond}
	n := sample()
	n.Priority = plugin.PrioNormal
	if err := p.Publish(context.Background(), pc, n); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests", len(reqs))
	}
	if f.path(0) != "/bot"+testToken+"/sendMessage" {
		t.Errorf("path %q", f.path(0))
	}
	r := reqs[0]
	if r["chat_id"] != "-1001234567890" || r["message_thread_id"] != float64(7) || r["parse_mode"] != "MarkdownV2" {
		t.Errorf("request: %v", r)
	}
	if r["disable_notification"] != true {
		t.Errorf("normal priority below silent_below=high must be silent: %v", r["disable_notification"])
	}
	if lp, ok := r["link_preview_options"].(map[string]any); !ok || lp["is_disabled"] != true {
		t.Errorf("link_preview_options: %v", r["link_preview_options"])
	}
	if want := render(n, time.UTC)[0]; r["text"] != want {
		t.Errorf("text:\n%v\nwant\n%s", r["text"], want)
	}

	// High priority is not silent; without thread id and preview option the keys are absent.
	pc = publishContext(t, map[string]any{
		"bot_token": testToken, "chat_id": "@netscope_alerts", "api_url": srv.URL,
		"silent_below": "high", "disable_preview": false,
	})
	n.Priority = plugin.PrioHigh
	if err := p.Publish(context.Background(), pc, n); err != nil {
		t.Fatal(err)
	}
	r = f.requests()[1]
	for _, k := range []string{"disable_notification", "message_thread_id", "link_preview_options"} {
		if _, ok := r[k]; ok {
			t.Errorf("unexpected key %s: %v", k, r)
		}
	}
	if r["chat_id"] != "@netscope_alerts" {
		t.Errorf("chat_id %v", r["chat_id"])
	}
}

func splitNotification() *plugin.Notification {
	n := &plugin.Notification{Kind: plugin.NotifyEvent, Priority: plugin.PrioHigh, Title: "Sammelmeldung"}
	for i := range 12 {
		n.Events = append(n.Events, plugin.EventView{
			ID: int64(i), Severity: plugin.SevHigh, Title: fmt.Sprintf("Ereignis %d", i),
			Message: strings.Repeat("lange Beschreibung. ", 38),
		})
	}
	return n
}

func TestPublishSplitMessages(t *testing.T) {
	f, srv := newAPI(t)
	pc := publishContext(t, map[string]any{"bot_token": testToken, "chat_id": "42", "api_url": srv.URL})
	p := &Plugin{partInterval: 20 * time.Millisecond}
	n := splitNotification()
	want := render(n, time.UTC)
	if len(want) < 2 {
		t.Fatalf("test notification does not split (%d parts)", len(want))
	}
	if err := p.Publish(context.Background(), pc, n); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != len(want) {
		t.Fatalf("%d requests, want %d", len(reqs), len(want))
	}
	for i := range reqs {
		if reqs[i]["text"] != want[i] {
			t.Errorf("part %d differs", i)
		}
		if i > 0 && f.gap(i) < 15*time.Millisecond {
			t.Errorf("parts %d and %d not paced", i-1, i)
		}
	}
}

func TestPublishRateLimitFirstPart(t *testing.T) {
	f, srv := newAPI(t, jsonResp(http.StatusTooManyRequests,
		`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 30","parameters":{"retry_after":30}}`))
	pc := publishContext(t, map[string]any{"bot_token": testToken, "chat_id": "42", "api_url": srv.URL})
	start := time.Now()
	err := (&Plugin{partInterval: time.Millisecond}).Publish(context.Background(), pc, sample())
	if err == nil {
		t.Fatal("expected error")
	}
	var rl *RateLimitError
	if !errors.As(err, &rl) || rl.RetryAfter() != 30*time.Second {
		t.Fatalf("err = %#v", err)
	}
	if !strings.Contains(err.Error(), "30s") || !strings.Contains(err.Error(), "429") {
		t.Errorf("error does not mention the delay: %v", err)
	}
	if len(f.requests()) != 1 || time.Since(start) > 5*time.Second {
		t.Errorf("must return immediately without retrying (%d requests)", len(f.requests()))
	}
}

func TestPublishRateLimitLaterPart(t *testing.T) {
	ok := func(w http.ResponseWriter) { _, _ = io.WriteString(w, `{"ok":true,"result":{}}`) }
	// Part 2 is rate limited for 1s: it is retried in-process (part 1 is already out).
	f, srv := newAPI(t, ok, jsonResp(http.StatusTooManyRequests,
		`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 1","parameters":{"retry_after":1}}`))
	pc := publishContext(t, map[string]any{"bot_token": testToken, "chat_id": "42", "api_url": srv.URL})
	n := splitNotification()
	parts := render(n, time.UTC)
	if err := (&Plugin{partInterval: time.Millisecond}).Publish(context.Background(), pc, n); err != nil {
		t.Fatal(err)
	}
	reqs := f.requests()
	if len(reqs) != len(parts)+1 {
		t.Fatalf("%d requests, want %d", len(reqs), len(parts)+1)
	}
	if reqs[1]["text"] != reqs[2]["text"] || reqs[2]["text"] != parts[1] {
		t.Error("rate-limited part was not resent")
	}
	if d := f.gap(2); d < 900*time.Millisecond {
		t.Errorf("retry after %s, want >= 1s", d)
	}

	// A long delay on a later part is reported with the number of delivered parts.
	f2, srv2 := newAPI(t, ok, jsonResp(http.StatusTooManyRequests,
		`{"ok":false,"error_code":429,"description":"Too Many Requests: retry after 90","parameters":{"retry_after":90}}`))
	pc = publishContext(t, map[string]any{"bot_token": testToken, "chat_id": "42", "api_url": srv2.URL})
	err := (&Plugin{partInterval: time.Millisecond}).Publish(context.Background(), pc, n)
	if err == nil || !strings.Contains(err.Error(), fmt.Sprintf("Teil 2 von %d", len(parts))) ||
		!strings.Contains(err.Error(), "1 bereits gesendet") || !strings.Contains(err.Error(), "1m30s") {
		t.Fatalf("err = %v", err)
	}
	if len(f2.requests()) != 2 {
		t.Fatalf("%d requests", len(f2.requests()))
	}
}

func TestPublishAPIErrors(t *testing.T) {
	cases := []struct {
		name string
		resp func(http.ResponseWriter)
		want []string
	}{
		{"chat not found", jsonResp(http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"Bad Request: chat not found"}`),
			[]string{"Chat nicht gefunden", "Bad Request: chat not found", "400"}},
		{"unauthorized", jsonResp(http.StatusUnauthorized, `{"ok":false,"error_code":401,"description":"Unauthorized"}`),
			[]string{"Bot-Token ungültig", "Unauthorized"}},
		{"migrated", jsonResp(http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"Bad Request: group chat was upgraded to a supergroup chat","parameters":{"migrate_to_chat_id":-1009876543210}}`),
			[]string{"Supergruppe", "-1009876543210"}},
		{"parse error", jsonResp(http.StatusBadRequest, `{"ok":false,"error_code":400,"description":"Bad Request: can't parse entities: Character '.' is reserved"}`),
			[]string{"Bot-API-Fehler 400", "can't parse entities"}},
		{"proxy error", func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = io.WriteString(w, "<html><body>502 Bad Gateway "+testToken+"</body></html>")
		}, []string{"HTTP 502 Bad Gateway", "<html><body>502 Bad Gateway ***"}},
		{"429 without body", func(w http.ResponseWriter) {
			w.Header().Set("Retry-After", "12")
			w.WriteHeader(http.StatusTooManyRequests)
		}, []string{"429", "12s"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, srv := newAPI(t, tc.resp)
			pc := publishContext(t, map[string]any{"bot_token": testToken, "chat_id": "42", "api_url": srv.URL})
			err := (&Plugin{}).Publish(context.Background(), pc, sample())
			if err == nil {
				t.Fatal("expected error")
			}
			for _, w := range tc.want {
				if !strings.Contains(err.Error(), w) {
					t.Errorf("error %q lacks %q", err, w)
				}
			}
			if strings.Contains(err.Error(), testToken) || strings.Contains(err.Error(), "AAHfake") {
				t.Errorf("token leaked: %v", err)
			}
		})
	}
}

func TestPublishUnreachableDoesNotLeakToken(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	u := srv.URL
	srv.Close()
	pc := publishContext(t, map[string]any{"bot_token": testToken, "chat_id": "42", "api_url": u})
	err := (&Plugin{}).Publish(context.Background(), pc, sample())
	if err == nil || !strings.Contains(err.Error(), "nicht erreichbar") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "AAHfake") || strings.Contains(err.Error(), "/bot") {
		t.Fatalf("token leaked: %v", err)
	}
}

func TestPublishTimeoutAndCancel(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(release)
	pc := publishContext(t, map[string]any{"bot_token": testToken, "chat_id": "42", "api_url": srv.URL, "timeout": "1s"})
	start := time.Now()
	err := (&Plugin{}).Publish(context.Background(), pc, sample())
	if err == nil || !strings.Contains(err.Error(), "Zeitüberschreitung") || time.Since(start) > 5*time.Second {
		t.Fatalf("err = %v after %s", err, time.Since(start))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	pc = publishContext(t, map[string]any{"bot_token": testToken, "chat_id": "42", "api_url": srv.URL, "timeout": "30s"})
	start = time.Now()
	if err := (&Plugin{}).Publish(ctx, pc, sample()); err == nil || time.Since(start) > 5*time.Second {
		t.Fatalf("cancelled context not respected: %v after %s", err, time.Since(start))
	}
}

func TestValidateSettings(t *testing.T) {
	p := &Plugin{}
	for _, tok := range []string{"123:short", "no-colon-token-aaaaaaaaaaaaaaaaaaaaa", "123456:bad char!aaaaaaaaaaaaaaaaaaaa"} {
		vals, err := p.Schema().Validate(map[string]any{"bot_token": tok, "chat_id": "1"}, nil, nil)
		if err != nil {
			t.Fatalf("schema: %v", err)
		}
		err = p.ValidateSettings(plugin.NewSettings(vals))
		if err == nil {
			t.Errorf("token %q accepted", tok)
			continue
		}
		if strings.Contains(err.Error(), tok) {
			t.Errorf("error echoes the token: %v", err)
		}
	}
	for chat, ok := range map[string]bool{"42": true, "-1001234567890": true, "@my_channel": true, "@ab": false, "abc": false, "12 34": false} {
		_, err := p.Schema().Validate(map[string]any{"bot_token": testToken, "chat_id": chat}, nil, nil)
		if (err == nil) != ok {
			t.Errorf("chat_id %q: err = %v, want ok=%v", chat, err, ok)
		}
	}
	if _, err := p.Schema().Validate(map[string]any{"chat_id": "42"}, nil, nil); err == nil {
		t.Error("missing bot_token accepted")
	}
}
