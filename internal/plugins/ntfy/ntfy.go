// Package ntfy publishes notifications to an ntfy server (ntfy.sh or self-hosted).
//
// It uses ntfy's JSON publishing API: one POST of a JSON object to the server root URL
// ({"topic","title","message","priority","tags","click","markdown"}). Unlike header
// based publishing, titles and messages can contain any UTF-8 text without RFC 2047
// encoding.
package ntfy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

const (
	defaultServer  = "https://ntfy.sh"
	defaultTimeout = 10 * time.Second
	userAgent      = "NetScope/1.0"
)

// Plugin is the ntfy publisher.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "ntfy",
		Kind:               plugin.KindPublisher,
		Name:               "ntfy",
		Description:        "Sendet Push-Benachrichtigungen über ntfy (ntfy.sh oder eigener Server) an ein Topic.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultTimeout:     time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     3,
		Targets:            plugin.TargetNone,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "server", Type: plugin.FieldString, Label: "Server", Default: defaultServer, Group: "Verbindung",
			Description: "Basis-URL des ntfy-Servers.",
			Validation:  &plugin.Validation{Format: "url"}},
		{Key: "topic", Type: plugin.FieldString, Label: "Topic", Required: true, Group: "Verbindung",
			Description: "Name des Topics (1–64 Zeichen: Buchstaben, Ziffern, - und _). Auf ntfy.sh ist jedes " +
				"Topic ohne Zugriffsschutz öffentlich – einen schwer zu erratenden Namen wählen.",
			Placeholder: "netscope-a8f3k2",
			Validation:  &plugin.Validation{Pattern: `^[-_A-Za-z0-9]{1,64}$`}},
		{Key: "token", Type: plugin.FieldSecret, Label: "Access-Token", Group: "Anmeldung",
			Description: "Optional: ntfy-Access-Token (tk_…), gesendet als „Authorization: Bearer“. Hat Vorrang vor dem Credential."},
		{Key: "credential", Type: plugin.FieldCredentialRef, Label: "Benutzer/Passwort", Group: "Anmeldung",
			CredentialTypes: []string{plugin.CredPassword},
			Description:     "Optional: Zugangsdaten für Basic-Auth, falls kein Token gesetzt ist."},
		{Key: "markdown", Type: plugin.FieldBool, Label: "Markdown", Default: true, Group: "Darstellung",
			Description: "Nachricht als Markdown senden (Fettschrift, Links). Die ntfy-Web-App stellt Markdown dar; " +
				"Apps ohne Markdown-Unterstützung zeigen den Text mit Formatierungszeichen."},
		{Key: "tags_by_severity", Type: plugin.FieldBool, Label: "Emoji nach Schweregrad", Default: true, Group: "Darstellung",
			Description: "Setzt Tags nach dem höchsten Schweregrad (🚨 kritisch, ⚠️ hoch, 🔶 mittel, 🔷 niedrig, ℹ️ info)."},
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Timeout", Default: "10s", Advanced: true,
			Description: "Maximale Dauer einer Zustellung.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(120)}},
	}}
}

// message is the JSON publish request of ntfy.
type message struct {
	Topic    string   `json:"topic"`
	Title    string   `json:"title,omitempty"`
	Message  string   `json:"message"`
	Priority int      `json:"priority"`
	Tags     []string `json:"tags,omitempty"`
	Click    string   `json:"click,omitempty"`
	Markdown bool     `json:"markdown,omitempty"`
}

// errorResponse is ntfy's JSON error body.
type errorResponse struct {
	Code  int    `json:"code"`
	HTTP  int    `json:"http"`
	Error string `json:"error"`
}

// Publish implements plugin.Publisher.
func (p *Plugin) Publish(ctx context.Context, pc *plugin.PublishContext, n *plugin.Notification) error {
	s := pc.Settings
	server := strings.TrimRight(strings.TrimSpace(s.String("server")), "/")
	if server == "" {
		server = defaultServer
	}
	u, err := url.Parse(server)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return errors.New("ntfy: ungültige Server-URL")
	}
	topic := strings.TrimSpace(s.String("topic"))
	if topic == "" {
		return errors.New("ntfy: kein Topic konfiguriert")
	}
	auth, err := authorization(ctx, pc)
	if err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}
	msg := build(n, topic, s.Bool("markdown"), s.Bool("tags_by_severity"), pc.Env.Location)
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("ntfy: %w", err)
	}

	timeout := s.Duration("timeout")
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, server+"/", bytes.NewReader(body))
	if err != nil {
		return errors.New("ntfy: Anfrage konnte nicht erstellt werden")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
			return fmt.Errorf("ntfy: %s: Zeitüberschreitung nach %s", u.Host, timeout)
		}
		return fmt.Errorf("ntfy: %s nicht erreichbar: %w", u.Host, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode/100 == 2 {
		logger(pc).Debug("ntfy-Nachricht gesendet", "notification", n.ID, "kind", n.Kind,
			"events", len(n.Events), "priority", msg.Priority)
		return nil
	}
	return fmt.Errorf("ntfy: %w", statusError(resp, raw))
}

func statusError(resp *http.Response, raw []byte) error {
	detail := excerpt(raw, 200)
	var er errorResponse
	if json.Unmarshal(raw, &er) == nil && er.Error != "" {
		detail = excerpt([]byte(er.Error), 200)
		if er.Code != 0 {
			detail = fmt.Sprintf("%s (Code %d)", detail, er.Code)
		}
	}
	msg := "Server antwortete mit HTTP " + resp.Status
	if detail != "" {
		msg += ": " + detail
	}
	switch resp.StatusCode {
	case http.StatusUnauthorized, http.StatusForbidden:
		msg += " – Token bzw. Zugangsdaten und Schreibrecht auf das Topic prüfen"
	case http.StatusTooManyRequests:
		msg += " – Ratenlimit erreicht, später erneut versuchen"
		if ra := strings.TrimSpace(resp.Header.Get("Retry-After")); ra != "" {
			msg += " (Retry-After: " + excerpt([]byte(ra), 40) + ")"
		}
	case http.StatusRequestEntityTooLarge:
		msg += " – Nachricht zu groß"
	}
	return errors.New(msg)
}

// authorization returns the Authorization header value: the token wins over the
// credential. Secrets are never part of error messages.
func authorization(ctx context.Context, pc *plugin.PublishContext) (string, error) {
	if tok := strings.TrimSpace(pc.Settings.String("token")); tok != "" {
		return "Bearer " + tok, nil
	}
	id := pc.Settings.CredentialID("credential")
	if id <= 0 {
		return "", nil
	}
	if pc.Creds == nil {
		return "", errors.New("Credential-Zugriff nicht verfügbar")
	}
	cred, err := pc.Creds.Get(ctx, id)
	if err != nil {
		return "", fmt.Errorf("Credential %d: %w", id, err)
	}
	if err := cred.RequireType(plugin.CredPassword); err != nil {
		return "", err
	}
	user, pass := cred.Get("username"), cred.Get("password")
	if user == "" {
		return "", fmt.Errorf("Credential %q: Benutzername fehlt", cred.Name)
	}
	req := &http.Request{Header: http.Header{}}
	req.SetBasicAuth(user, pass)
	return req.Header.Get("Authorization"), nil
}

var httpClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// excerpt returns the start of b as a single printable line of at most n runes.
func excerpt(b []byte, n int) string {
	s := strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) || !unicode.IsPrint(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(string(b), ""))
	s = strings.Join(strings.Fields(s), " ")
	if rs := []rune(s); len(rs) > n {
		s = string(rs[:n]) + "…"
	}
	return s
}

func logger(pc *plugin.PublishContext) *slog.Logger {
	if pc.Log != nil {
		return pc.Log
	}
	return slog.New(slog.DiscardHandler)
}
