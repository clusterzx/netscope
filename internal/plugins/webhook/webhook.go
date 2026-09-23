// Package webhook is the generic webhook publisher: it POSTs (or PUTs) every
// notification as JSON (NetScope payload v1) to a configurable URL, optionally signed
// with HMAC-SHA256.
//
// The payload builder (BuildPayload, EncodePayload) and the HTTP delivery (Send) are
// exported and reused by the n8n publisher, so the payload is defined in one place.
package webhook

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/textproto"
	"strings"
	"time"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the webhook publisher.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "webhook",
		Kind:               plugin.KindPublisher,
		Name:               "Webhook",
		Description:        "Sendet Benachrichtigungen als JSON (NetScope-Payload v1) per HTTP an eine beliebige URL, optional mit HMAC-Signatur.",
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
		{Key: "url", Type: plugin.FieldString, Label: "URL", Required: true,
			Description: "Empfänger der Benachrichtigungen. Weiterleitungen (3xx) werden nicht verfolgt.",
			Placeholder: "https://example.org/hooks/netscope",
			Validation:  &plugin.Validation{Format: "url"}},
		{Key: "method", Type: plugin.FieldEnum, Label: "HTTP-Methode", Default: "POST",
			Options: []plugin.Option{{Value: "POST", Label: "POST"}, {Value: "PUT", Label: "PUT"}}},
		{Key: "headers", Type: plugin.FieldStringList, Label: "Zusätzliche Header",
			Description: "Ein Header pro Zeile im Format „Name: Wert“, z. B. „Authorization: Bearer …“. " +
				"Die Werte werden nicht verschlüsselt gespeichert und in der UI angezeigt – für ein " +
				"gemeinsames Geheimnis besser das HMAC-Secret verwenden. Content-Type, User-Agent, Host " +
				"und X-NetScope-* setzt NetScope selbst.",
			Placeholder: "X-Api-Key: …",
			Validation:  &plugin.Validation{Format: "header"}},
		{Key: "hmac_secret", Type: plugin.FieldSecret, Label: "HMAC-Secret", Group: "Signatur",
			Description: "Optional. Wenn gesetzt, trägt jede Anfrage die Header X-NetScope-Timestamp (Unix-Sekunden) " +
				"und X-NetScope-Signature = „sha256=“ + HEX(HMAC-SHA256(Secret, Timestamp + „.“ + Body)). " +
				"Empfohlen: mindestens 32 zufällige Zeichen."},
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Timeout", Default: "10s", Advanced: true,
			Description: "Maximale Dauer einer Zustellung.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(120)}},
		{Key: "verify_tls", Type: plugin.FieldBool, Label: "TLS-Zertifikat prüfen", Default: true, Advanced: true,
			Description: "Nur für Empfänger mit selbstsigniertem Zertifikat im eigenen Netz abschalten."},
	}}
}

// reservedHeaders are set by NetScope (or net/http) and cannot be configured; the same
// applies to every X-NetScope-* header.
var reservedHeaders = map[string]bool{
	"Host":              true,
	"Content-Type":      true,
	"Content-Length":    true,
	"Transfer-Encoding": true,
	"Connection":        true,
	"User-Agent":        true,
}

// ParseHeader splits a "Name: Wert" line and checks the name. The value is never part
// of an error message (it may be a token).
func ParseHeader(line string) (name, value string, err error) {
	rawName, rawValue, ok := strings.Cut(line, ":")
	name = strings.TrimSpace(rawName)
	if !ok || name == "" {
		return "", "", fmt.Errorf("Header-Zeile ohne „Name: Wert“")
	}
	for _, r := range name {
		if !isTokenRune(r) {
			return "", "", fmt.Errorf("ungültiger Header-Name %q", name)
		}
	}
	value = strings.TrimSpace(rawValue)
	for _, r := range value {
		if (r < 0x20 && r != '\t') || r == 0x7f {
			return "", "", fmt.Errorf("Header %s: Steuerzeichen im Wert", name)
		}
	}
	name = textproto.CanonicalMIMEHeaderKey(name)
	if reservedHeaders[name] || strings.HasPrefix(name, "X-Netscope-") {
		return "", "", fmt.Errorf("Header %s wird von NetScope gesetzt und kann nicht konfiguriert werden", name)
	}
	return name, value, nil
}

// isTokenRune reports whether r may appear in an HTTP header name (RFC 9110 token).
func isTokenRune(r rune) bool {
	if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
		return true
	}
	return strings.ContainsRune("!#$%&'*+-.^_`|~", r)
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	var errs []plugin.FieldError
	for _, line := range s.StringList("headers") {
		if _, _, err := ParseHeader(line); err != nil {
			errs = append(errs, plugin.FieldError{Field: "headers", Message: err.Error()})
		}
	}
	if len(errs) > 0 {
		return &plugin.ValidationError{Errors: errs}
	}
	return nil
}

// Publish implements plugin.Publisher.
func (p *Plugin) Publish(ctx context.Context, pc *plugin.PublishContext, n *plugin.Notification) error {
	s := pc.Settings
	header := http.Header{}
	for _, line := range s.StringList("headers") {
		name, value, err := ParseHeader(line)
		if err != nil {
			return fmt.Errorf("Webhook: %w", err)
		}
		header.Add(name, value)
	}
	now := time.Now()
	body, err := EncodePayload(BuildPayload(n, now))
	if err != nil {
		return fmt.Errorf("Webhook: Payload konnte nicht erzeugt werden: %w", err)
	}
	method := s.String("method")
	if method != http.MethodPut {
		method = http.MethodPost
	}
	err = Send(ctx, Request{
		URL:            s.String("url"),
		Method:         method,
		Header:         header,
		Body:           body,
		Kind:           n.Kind,
		NotificationID: n.ID,
		Secret:         s.String("hmac_secret"),
		Timestamp:      now,
		Timeout:        s.Duration("timeout"),
		VerifyTLS:      s.Bool("verify_tls"),
	})
	if err != nil {
		return fmt.Errorf("Webhook: %w", err)
	}
	logger(pc).Debug("Webhook zugestellt", "notification", n.ID, "kind", n.Kind,
		"events", len(n.Events), "bytes", len(body), "signed", s.String("hmac_secret") != "")
	return nil
}

func logger(pc *plugin.PublishContext) *slog.Logger {
	if pc.Log != nil {
		return pc.Log
	}
	return slog.New(slog.DiscardHandler)
}
