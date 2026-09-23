package n8n

import (
	"context"
	"encoding/json"
	"errors"
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
	"netscope/internal/plugins/webhook"
)

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
		PluginID: "n8n",
		Settings: s,
		Log:      slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Creds:    plugintest.Creds{},
		Env:      plugin.Env{Location: time.UTC},
	}
}

func notification() *plugin.Notification {
	at := time.Date(2026, 9, 22, 8, 15, 0, 0, time.UTC)
	return &plugin.Notification{
		ID: 9, Kind: plugin.NotifyEvent, Priority: plugin.PrioUrgent, Title: "Neues unbekanntes Gerät",
		RuleID: 1, RuleName: "Neues unbekanntes Gerät → sofort", CreatedAt: at,
		Link: "http://netscope.lan/events?notification=9",
		Events: []plugin.EventView{{
			ID: 77, Type: plugin.EvDeviceNew, Label: "Neues Gerät", Category: "device", Severity: plugin.SevMedium,
			Title: "Neues Gerät 192.168.8.77 (Espressif)", At: at, DeviceID: 5, DeviceIP: "192.168.8.77",
			DeviceMAC: "24:0a:c4:00:00:01", Link: "http://netscope.lan/events/77", DeviceLink: "http://netscope.lan/devices/5",
			Payload: map[string]any{"vendor": "Espressif Inc.", "source": "arpscan"},
		}},
	}
}

// request is the last request seen by a capture server.
type request struct {
	method string
	path   string
	header http.Header
	body   []byte
	count  int // number of requests so far
}

type capture struct {
	mu   sync.Mutex
	last request
}

func (c *capture) get() request {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.last
}

func (c *capture) handler(status int, resp string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.last = request{method: r.Method, path: r.URL.Path, header: r.Header.Clone(), body: b, count: c.last.count + 1}
		c.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, resp)
	}
}

func TestRegistered(t *testing.T) {
	p, ok := plugin.Get("n8n")
	if !ok {
		t.Fatal("n8n not registered")
	}
	if info := p.Info(); info.Kind != plugin.KindPublisher || info.DefaultEnabled || info.Targets != plugin.TargetNone {
		t.Fatalf("info: %+v", info)
	}
}

func TestPublishPayloadAndAuthHeader(t *testing.T) {
	c := &capture{}
	srv := httptest.NewServer(c.handler(http.StatusOK, `{"message":"Workflow was started"}`))
	defer srv.Close()
	pc := publishContext(t, map[string]any{
		"url":               srv.URL + "/webhook/netscope",
		"auth_header_name":  "X-N8N-Key",
		"auth_header_value": "s3cret-value",
	})
	if err := (&Plugin{}).Publish(context.Background(), pc, notification()); err != nil {
		t.Fatal(err)
	}
	r := c.get()
	if r.count != 1 || r.method != http.MethodPost || r.path != "/webhook/netscope" {
		t.Fatalf("request: %d %s %s", r.count, r.method, r.path)
	}
	for k, want := range map[string]string{
		"X-N8N-Key":                  "s3cret-value",
		"Content-Type":               "application/json",
		"User-Agent":                 "NetScope/1.0",
		"X-NetScope-Event":           "event",
		"X-NetScope-Notification-Id": "9",
	} {
		if got := r.header.Get(k); got != want {
			t.Errorf("header %s = %q, want %q", k, got, want)
		}
	}
	if r.header.Get("X-NetScope-Signature") != "" {
		t.Error("n8n requests are not signed")
	}
	var pl webhook.Payload
	if err := json.Unmarshal(r.body, &pl); err != nil {
		t.Fatal(err)
	}
	if pl.Version != webhook.PayloadVersion || pl.Notification.Priority != "urgent" || pl.Summary.Events != 1 ||
		pl.Events[0].Type != plugin.EvDeviceNew || pl.Events[0].Payload["vendor"] != "Espressif Inc." ||
		pl.Events[0].Device == nil || pl.Events[0].Device.MAC != "24:0a:c4:00:00:01" {
		t.Fatalf("payload: %s", r.body)
	}
	// The body is exactly what the shared builder produces (single source of truth).
	want, _ := webhook.EncodePayload(webhook.BuildPayload(notification(), mustParse(t, pl.SentAt)))
	if string(want) != string(r.body) {
		t.Fatalf("payload differs from webhook.BuildPayload:\n%s\n%s", r.body, want)
	}
}

func mustParse(t *testing.T, s string) time.Time {
	t.Helper()
	ts, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return ts
}

func TestPublishWithoutAuth(t *testing.T) {
	c := &capture{}
	srv := httptest.NewServer(c.handler(http.StatusOK, ""))
	defer srv.Close()
	pc := publishContext(t, map[string]any{"url": srv.URL + "/webhook/x"})
	n := &plugin.Notification{ID: 3, Kind: plugin.NotifyReport, Priority: plugin.PrioLow, Title: "Wochenbericht", Body: "# Woche 38\n- 3 neue Geräte"}
	if err := (&Plugin{}).Publish(context.Background(), pc, n); err != nil {
		t.Fatal(err)
	}
	r := c.get()
	var m map[string]any
	if err := json.Unmarshal(r.body, &m); err != nil {
		t.Fatal(err)
	}
	if m["body"] != "# Woche 38\n- 3 neue Geräte" || r.header.Get("X-NetScope-Event") != "report" {
		t.Fatalf("body %v, event %q", m["body"], r.header.Get("X-NetScope-Event"))
	}
	if evs, ok := m["events"].([]any); !ok || len(evs) != 0 {
		t.Fatalf("events: %v", m["events"])
	}
}

func TestPublishErrorsWithHints(t *testing.T) {
	notFound := `{"code":404,"message":"The requested webhook \"POST netscope\" is not registered.","hint":"Click the 'Execute workflow' button"}`
	c := &capture{}
	srv := httptest.NewServer(c.handler(http.StatusNotFound, notFound))
	defer srv.Close()

	err := (&Plugin{}).Publish(context.Background(), publishContext(t, map[string]any{"url": srv.URL + "/webhook-test/netscope"}), notification())
	if err == nil || !strings.Contains(err.Error(), "404") || !strings.Contains(err.Error(), "is not registered") ||
		!strings.Contains(err.Error(), "Production-URL") {
		t.Fatalf("err = %v", err)
	}
	err = (&Plugin{}).Publish(context.Background(), publishContext(t, map[string]any{"url": srv.URL + "/webhook/netscope"}), notification())
	if err == nil || !strings.Contains(err.Error(), "Workflow aktiv?") {
		t.Fatalf("err = %v", err)
	}

	forbidden := httptest.NewServer(c.handler(http.StatusForbidden, "Authorization data is wrong!"))
	defer forbidden.Close()
	err = (&Plugin{}).Publish(context.Background(), publishContext(t, map[string]any{
		"url": forbidden.URL, "auth_header_name": "X-N8N-Key", "auth_header_value": "falsch-geheim",
	}), notification())
	if err == nil || !strings.Contains(err.Error(), "403") || !strings.Contains(err.Error(), "Auth-Header") {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), "falsch-geheim") {
		t.Fatalf("secret leaked: %v", err)
	}
}

func TestValidateSettings(t *testing.T) {
	p := &Plugin{}
	cases := []struct {
		settings map[string]any
		field    string
	}{
		{map[string]any{"url": "https://n8n.lan/webhook/a", "auth_header_name": "X-N8N-Key"}, "auth_header_value"},
		{map[string]any{"url": "https://n8n.lan/webhook/a", "auth_header_value": "x"}, "auth_header_name"},
		{map[string]any{"url": "https://n8n.lan/webhook/a", "auth_header_name": "Content-Type", "auth_header_value": "x"}, "auth_header_name"},
		{map[string]any{"url": "https://n8n.lan/webhook/a", "auth_header_name": "X-N8N-Key", "auth_header_value": "x"}, ""},
		{map[string]any{"url": "https://n8n.lan/webhook/a"}, ""},
	}
	for _, tc := range cases {
		vals, err := p.Schema().Validate(tc.settings, nil, nil)
		if err != nil {
			t.Fatalf("%v: %v", tc.settings, err)
		}
		err = p.ValidateSettings(plugin.NewSettings(vals))
		if tc.field == "" {
			if err != nil {
				t.Errorf("%v: %v", tc.settings, err)
			}
			continue
		}
		var ve *plugin.ValidationError
		ok := errors.As(err, &ve)
		if !ok || len(ve.Errors) != 1 || ve.Errors[0].Field != tc.field {
			t.Errorf("%v: err = %v, want field %s", tc.settings, err, tc.field)
		}
	}
	if _, err := p.Schema().Validate(map[string]any{"url": "https://n8n.lan/webhook/a", "auth_header_name": "X N8N"}, nil, nil); err == nil {
		t.Error("header name with space accepted")
	}
}
