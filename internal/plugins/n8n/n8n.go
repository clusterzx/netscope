// Package n8n is a preconfigured webhook publisher for n8n workflows. It always POSTs
// the NetScope payload v1 (defined in package webhook) to the production URL of an
// n8n "Webhook" node, optionally with a header for n8n's header authentication.
package n8n

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugins/webhook"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the n8n publisher.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "n8n",
		Kind:               plugin.KindPublisher,
		Name:               "n8n",
		Description:        "Übergibt Benachrichtigungen als dokumentierten JSON-Payload an einen n8n-Workflow (Webhook-Node).",
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
		{Key: "url", Type: plugin.FieldString, Label: "Webhook-URL", Required: true,
			Description: "Production-URL des n8n-Webhook-Nodes (…/webhook/…, nicht …/webhook-test/…). " +
				"Der Workflow muss aktiv sein; HTTP-Methode im Node: POST.",
			Placeholder: "https://n8n.example.org/webhook/netscope",
			Validation:  &plugin.Validation{Format: "url"}},
		{Key: "auth_header_name", Type: plugin.FieldString, Label: "Auth-Header-Name", Group: "Authentifizierung",
			Description: "Optional, für „Header Auth“ im Webhook-Node, z. B. X-N8N-Key.",
			Placeholder: "X-N8N-Key",
			Validation:  &plugin.Validation{Pattern: "^[A-Za-z0-9!#$%&'*+.^_`|~-]+$"}},
		{Key: "auth_header_value", Type: plugin.FieldSecret, Label: "Auth-Header-Wert", Group: "Authentifizierung",
			Description: "Wert des Auth-Headers (verschlüsselt gespeichert)."},
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Timeout", Default: "10s", Advanced: true,
			Description: "Maximale Dauer einer Zustellung. Im Webhook-Node „Respond: Immediately“ wählen, " +
				"damit n8n nicht erst am Ende des Workflows antwortet.",
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(120)}},
		{Key: "verify_tls", Type: plugin.FieldBool, Label: "TLS-Zertifikat prüfen", Default: true, Advanced: true,
			Description: "Nur für n8n-Instanzen mit selbstsigniertem Zertifikat im eigenen Netz abschalten."},
	}}
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	name, value := s.String("auth_header_name"), s.String("auth_header_value")
	var errs []plugin.FieldError
	switch {
	case name != "" && value == "":
		errs = append(errs, plugin.FieldError{Field: "auth_header_value", Message: "Wert für den Auth-Header fehlt"})
	case name == "" && value != "":
		errs = append(errs, plugin.FieldError{Field: "auth_header_name", Message: "Name des Auth-Headers fehlt"})
	case name != "":
		if _, _, err := webhook.ParseHeader(name + ": x"); err != nil {
			errs = append(errs, plugin.FieldError{Field: "auth_header_name", Message: err.Error()})
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
	if name, value := s.String("auth_header_name"), s.String("auth_header_value"); name != "" && value != "" {
		name, _, err := webhook.ParseHeader(name + ": x")
		if err != nil {
			return fmt.Errorf("n8n: %w", err)
		}
		header.Set(name, value)
	}
	now := time.Now()
	body, err := webhook.EncodePayload(webhook.BuildPayload(n, now))
	if err != nil {
		return fmt.Errorf("n8n: Payload konnte nicht erzeugt werden: %w", err)
	}
	err = webhook.Send(ctx, webhook.Request{
		URL:            s.String("url"),
		Method:         http.MethodPost,
		Header:         header,
		Body:           body,
		Kind:           n.Kind,
		NotificationID: n.ID,
		Timestamp:      now,
		Timeout:        s.Duration("timeout"),
		VerifyTLS:      s.Bool("verify_tls"),
	})
	if err != nil {
		return fmt.Errorf("n8n: %w%s", err, hint(s.String("url"), err))
	}
	logger(pc).Debug("n8n-Webhook zugestellt", "notification", n.ID, "kind", n.Kind,
		"events", len(n.Events), "bytes", len(body))
	return nil
}

// hint explains the typical n8n misconfigurations behind an error.
func hint(url string, err error) string {
	var se *webhook.StatusError
	if !errors.As(err, &se) {
		return ""
	}
	switch se.StatusCode {
	case http.StatusNotFound:
		if strings.Contains(url, "/webhook-test/") {
			return " (Test-URL: funktioniert nur, solange im Editor „Listen for test event“ läuft – Production-URL verwenden)"
		}
		return " (Workflow aktiv? HTTP-Methode im Webhook-Node auf POST gestellt?)"
	case http.StatusUnauthorized, http.StatusForbidden:
		return " (Auth-Header-Name und -Wert mit der Header-Auth-Credential im Webhook-Node vergleichen)"
	}
	return ""
}

func logger(pc *plugin.PublishContext) *slog.Logger {
	if pc.Log != nil {
		return pc.Log
	}
	return slog.New(slog.DiscardHandler)
}
