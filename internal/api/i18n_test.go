package api

import (
	"encoding/json"
	"strings"
	"testing"

	"netscope/internal/events"
	"netscope/internal/i18n"
	"netscope/internal/plugin"
	_ "netscope/internal/plugins/diff" // catalog of the event titles below
)

func eventLabel(t *testing.T, body []byte, typ string) string {
	t.Helper()
	var list []plugin.EventSpec
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	for _, e := range list {
		if e.Type == typ {
			return e.Label
		}
	}
	t.Fatalf("event type %s missing", typ)
	return ""
}

func errorMessage(t *testing.T, body []byte) (string, []plugin.FieldError) {
	t.Helper()
	var e struct {
		Error apiError `json:"error"`
	}
	if err := json.Unmarshal(body, &e); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return e.Error.Message, e.Error.Fields
}

// TestLocaleSelection: the answer follows the user's preference, otherwise Accept-Language,
// otherwise German.
func TestLocaleSelection(t *testing.T) {
	h := newHarness(t)

	// before signing in: the browser language decides
	_, body := h.do(t, "GET", "/api/v1/devices", nil, "Accept-Language", "en-US,en;q=0.9")
	if msg, _ := errorMessage(t, body); msg != i18n.T(i18n.EN, "Anmeldung erforderlich") || msg == "Anmeldung erforderlich" {
		t.Errorf("unauthenticated (en): %q", msg)
	}
	_, body = h.do(t, "GET", "/api/v1/devices", nil)
	if msg, _ := errorMessage(t, body); msg != "Anmeldung erforderlich" {
		t.Errorf("unauthenticated (default): %q", msg)
	}

	h.login(t)
	_, body = h.do(t, "GET", "/api/v1/events/types", nil)
	if l := eventLabel(t, body, plugin.EvDeviceNew); l != "Neues Gerät" {
		t.Errorf("default must be German, got %q", l)
	}
	_, body = h.do(t, "GET", "/api/v1/events/types", nil, "Accept-Language", "fr-FR,fr;q=0.9")
	if l := eventLabel(t, body, plugin.EvDeviceNew); l != "Neues Gerät" {
		t.Errorf("unsupported browser language must fall back to German, got %q", l)
	}
	_, body = h.do(t, "GET", "/api/v1/events/types", nil, "Accept-Language", "en-GB")
	if l := eventLabel(t, body, plugin.EvDeviceNew); l != "New device" {
		t.Errorf("Accept-Language en: %q", l)
	}

	// the preference is stored and wins over the browser
	resp, body := h.do(t, "PUT", "/api/v1/auth/preferences", map[string]string{"locale": "en"}, csrf, "1")
	if resp.StatusCode != 200 {
		t.Fatalf("preferences: %d %s", resp.StatusCode, body)
	}
	var me meResponse
	if err := json.Unmarshal(body, &me); err != nil || me.User.Locale != "en" || me.Principal.Locale != "en" {
		t.Fatalf("preferences answer: %v %s", err, body)
	}
	_, body = h.do(t, "GET", "/api/v1/auth/me", nil)
	if err := json.Unmarshal(body, &me); err != nil || me.User.Locale != "en" {
		t.Fatalf("me: %v %s", err, body)
	}
	_, body = h.do(t, "GET", "/api/v1/events/types", nil, "Accept-Language", "de-DE")
	if l := eventLabel(t, body, plugin.EvDeviceNew); l != "New device" {
		t.Errorf("preference must win over Accept-Language: %q", l)
	}
	// plugin settings, permissions and errors follow as well
	_, body = h.do(t, "GET", "/api/v1/permissions", nil)
	if want := i18n.T(i18n.EN, "Geräte bearbeiten"); want == "Geräte bearbeiten" || !strings.Contains(string(body), `"label":"`+want+`"`) {
		t.Errorf("permissions not translated: %.300s", body)
	}
	resp, body = h.do(t, "POST", "/api/v1/devices", map[string]string{"ip": "not-an-ip"}, csrf, "1")
	msg, fields := errorMessage(t, body)
	if resp.StatusCode != 400 || msg == "" || strings.Contains(msg, "ungültig") || (len(fields) > 0 && strings.Contains(fields[0].Message, "ungültig")) {
		t.Errorf("validation error not translated: %d %q %v", resp.StatusCode, msg, fields)
	}
	resp, body = h.do(t, "PUT", "/api/v1/system/settings", map[string]any{"publicUrl": "ftp://x", "offlineAfterMissed": 2,
		"maxParallelRuns": 4}, csrf, "1")
	msg, fields = errorMessage(t, body)
	if resp.StatusCode != 400 || len(fields) != 1 || fields[0].Field != "publicUrl" || fields[0].Message != "valid http(s) URL expected" ||
		!strings.HasPrefix(msg, "invalid settings: publicUrl: valid") {
		t.Errorf("field error not translated: %d %q %v", resp.StatusCode, msg, fields)
	}
	_, body = h.do(t, "GET", "/api/v1/meta", nil)
	if want := i18n.T(i18n.EN, "Benutzername"); want == "Benutzername" || !strings.Contains(string(body), `"label":"`+want+`"`) {
		t.Errorf("credential type schema not translated: %.300s", body)
	}

	// invalid values are rejected, "" goes back to the browser language
	resp, _ = h.do(t, "PUT", "/api/v1/auth/preferences", map[string]string{"locale": "fr"}, csrf, "1")
	if resp.StatusCode != 400 {
		t.Errorf("invalid locale accepted: %d", resp.StatusCode)
	}
	resp, _ = h.do(t, "PUT", "/api/v1/auth/preferences", map[string]string{"locale": ""}, csrf, "1")
	if resp.StatusCode != 200 {
		t.Fatalf("reset preference: %d", resp.StatusCode)
	}
	_, body = h.do(t, "GET", "/api/v1/events/types", nil, "Accept-Language", "de")
	if l := eventLabel(t, body, plugin.EvDeviceNew); l != "Neues Gerät" {
		t.Errorf("after reset the browser decides: %q", l)
	}
}

// TestLocalizeStoredEvents: events stored in German are shown in English when read.
func TestLocalizeStoredEvents(t *testing.T) {
	ev := events.Event{Type: plugin.EvDeviceNew, Label: "Neues Gerät", Title: "Neues Gerät: drucker (10.0.0.5)"}
	en := localizeEvent(ev, i18n.EN)
	if en.Label != "New device" || en.Title != "New device: drucker (10.0.0.5)" {
		t.Errorf("event: %+v", en)
	}
	if de := localizeEvent(ev, i18n.DE); de.Title != ev.Title {
		t.Errorf("German must stay: %+v", de)
	}
	// a title without a pattern stays as it was stored
	if out := localizeEvent(events.Event{Title: "eigener Titel"}, i18n.EN); out.Title != "eigener Titel" {
		t.Errorf("unknown title changed: %q", out.Title)
	}
}
