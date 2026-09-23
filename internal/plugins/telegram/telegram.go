// Package telegram delivers notifications through the Telegram Bot API (sendMessage,
// MarkdownV2). Events of one notification are bundled into one message; messages
// longer than Telegram's limit are split at event boundaries.
package telegram

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
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

const (
	defaultAPIURL  = "https://api.telegram.org"
	defaultTimeout = 15 * time.Second
	userAgent      = "NetScope/1.0"
	// maxInlineWait is the longest rate-limit delay waited for in-process. It only
	// applies after parts of a split message were delivered, because a retry by the
	// dispatcher would send those parts again.
	maxInlineWait = 30 * time.Second
)

var tokenRe = regexp.MustCompile(`^[0-9]{3,20}:[A-Za-z0-9_-]{20,}$`)

// Plugin is the Telegram publisher.
type Plugin struct {
	// partInterval paces the parts of a split message (Telegram allows about one message
	// per second and chat). Zero means one second; tests lower it.
	partInterval time.Duration
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "telegram",
		Kind:               plugin.KindPublisher,
		Name:               "Telegram",
		Description:        "Sendet gebündelte Benachrichtigungen über einen Telegram-Bot in einen Chat, eine Gruppe oder einen Kanal.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultTimeout:     2 * time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     3,
		Targets:            plugin.TargetNone,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "bot_token", Type: plugin.FieldSecret, Label: "Bot-Token", Required: true, Group: "Verbindung",
			Description: "Token von @BotFather im Format 123456789:AA…"},
		{Key: "chat_id", Type: plugin.FieldString, Label: "Chat-ID", Required: true, Group: "Verbindung",
			Description: "Numerische ID des Chats bzw. der Gruppe (Gruppen/Kanäle beginnen mit -100…) oder @kanalname. " +
				"Der Bot muss Mitglied sein bzw. vom Benutzer mit /start angeschrieben worden sein.",
			Placeholder: "-1001234567890",
			Validation:  &plugin.Validation{Pattern: `^(-?[0-9]{1,20}|@[A-Za-z][A-Za-z0-9_]{3,31})$`}},
		{Key: "message_thread_id", Type: plugin.FieldInt, Label: "Themen-ID (Forum)", Group: "Verbindung",
			Description: "Optional: ID des Themas in Gruppen mit aktivierten Themen. 0 = allgemeiner Bereich.",
			Validation:  &plugin.Validation{Min: plugin.Int64(0)}},
		{Key: "silent_below", Type: plugin.FieldEnum, Label: "Lautlos unterhalb von", Default: "normal", Group: "Darstellung",
			Description: "Benachrichtigungen mit niedrigerer Priorität werden ohne Ton zugestellt.",
			Options: []plugin.Option{
				{Value: "low", Label: "Niedrig (nie lautlos)"},
				{Value: "normal", Label: "Normal (nur Niedrig lautlos)"},
				{Value: "high", Label: "Hoch (Niedrig und Normal lautlos)"},
				{Value: "urgent", Label: "Dringend (nur Dringend mit Ton)"},
			}},
		{Key: "disable_preview", Type: plugin.FieldBool, Label: "Link-Vorschau unterdrücken", Default: true, Group: "Darstellung"},
		{Key: "api_url", Type: plugin.FieldString, Label: "Bot-API-URL", Default: defaultAPIURL, Advanced: true,
			Description: "Nur ändern für einen selbst betriebenen Bot-API-Server.",
			Validation:  &plugin.Validation{Format: "url"}},
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Timeout", Default: "15s", Advanced: true,
			Description: "Maximale Dauer einer Anfrage an die Bot-API.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(120)}},
	}}
}

// ValidateSettings implements plugin.SettingsValidator. The token is never echoed.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	if t := s.String("bot_token"); t != "" && !tokenRe.MatchString(t) {
		return &plugin.ValidationError{Errors: []plugin.FieldError{{
			Field: "bot_token", Message: "ungültiges Format (erwartet 123456789:ABC…, wie von @BotFather ausgegeben)",
		}}}
	}
	return nil
}

type config struct {
	token          string
	chatID         string
	threadID       int64
	apiURL         string
	silentBelow    plugin.Priority
	disablePreview bool
	timeout        time.Duration
}

func loadConfig(s plugin.Settings) (config, error) {
	c := config{
		token:          strings.TrimSpace(s.String("bot_token")),
		chatID:         strings.TrimSpace(s.String("chat_id")),
		threadID:       int64(s.Int("message_thread_id")),
		apiURL:         strings.TrimRight(strings.TrimSpace(s.String("api_url")), "/"),
		silentBelow:    plugin.Priority(s.String("silent_below")),
		disablePreview: s.Bool("disable_preview"),
		timeout:        s.Duration("timeout"),
	}
	if c.token == "" {
		return c, errors.New("kein Bot-Token konfiguriert")
	}
	if !tokenRe.MatchString(c.token) {
		return c, errors.New("Bot-Token hat ein ungültiges Format")
	}
	if c.chatID == "" {
		return c, errors.New("keine Chat-ID konfiguriert")
	}
	if c.apiURL == "" {
		c.apiURL = defaultAPIURL
	}
	if u, err := url.Parse(c.apiURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return c, errors.New("ungültige Bot-API-URL")
	}
	if !c.silentBelow.Valid() {
		c.silentBelow = plugin.PrioNormal
	}
	if c.timeout <= 0 {
		c.timeout = defaultTimeout
	}
	return c, nil
}

func priorityRank(p plugin.Priority) int {
	for i, v := range plugin.Priorities {
		if v == p {
			return i
		}
	}
	return 1 // unknown = normal
}

// Publish implements plugin.Publisher.
func (p *Plugin) Publish(ctx context.Context, pc *plugin.PublishContext, n *plugin.Notification) error {
	cfg, err := loadConfig(pc.Settings)
	if err != nil {
		return fmt.Errorf("Telegram: %w", err)
	}
	msgs := render(n, pc.Env.Location)
	silent := priorityRank(n.Priority) < priorityRank(cfg.silentBelow)
	for i, text := range msgs {
		if i > 0 {
			if err := sleep(ctx, p.interval()); err != nil {
				return fmt.Errorf("Telegram: abgebrochen nach %d von %d Teilen: %w", i, len(msgs), err)
			}
		}
		err := p.send(ctx, cfg, text, silent)
		var rl *RateLimitError
		if i > 0 && errors.As(err, &rl) && rl.Delay <= maxInlineWait {
			if serr := sleep(ctx, max(rl.Delay, time.Second)); serr == nil {
				err = p.send(ctx, cfg, text, silent)
			}
		}
		if err != nil {
			if i > 0 {
				return fmt.Errorf("Telegram: Teil %d von %d nicht zugestellt (%d bereits gesendet): %w", i+1, len(msgs), i, err)
			}
			return fmt.Errorf("Telegram: %w", err)
		}
	}
	logger(pc).Debug("Telegram-Nachricht gesendet", "notification", n.ID, "kind", n.Kind,
		"events", len(n.Events), "parts", len(msgs), "silent", silent)
	return nil
}

func (p *Plugin) interval() time.Duration {
	if p.partInterval > 0 {
		return p.partInterval
	}
	return time.Second
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

type sendMessageRequest struct {
	ChatID              string              `json:"chat_id"`
	MessageThreadID     int64               `json:"message_thread_id,omitempty"`
	Text                string              `json:"text"`
	ParseMode           string              `json:"parse_mode"`
	DisableNotification bool                `json:"disable_notification,omitempty"`
	LinkPreviewOptions  *linkPreviewOptions `json:"link_preview_options,omitempty"`
}

type linkPreviewOptions struct {
	IsDisabled bool `json:"is_disabled"`
}

type apiResponse struct {
	OK          bool   `json:"ok"`
	ErrorCode   int    `json:"error_code"`
	Description string `json:"description"`
	Parameters  struct {
		RetryAfter      int   `json:"retry_after"`
		MigrateToChatID int64 `json:"migrate_to_chat_id"`
	} `json:"parameters"`
}

// RateLimitError is returned when Telegram answers 429 Too Many Requests.
type RateLimitError struct {
	Delay time.Duration // retry_after of the Bot API (0 if not given)
}

func (e *RateLimitError) Error() string {
	if e.Delay > 0 {
		return fmt.Sprintf("Ratenlimit der Bot-API erreicht (HTTP 429) – erneuter Versuch frühestens in %s möglich", e.Delay)
	}
	return "Ratenlimit der Bot-API erreicht (HTTP 429)"
}

// RetryAfter returns the delay requested by Telegram.
func (e *RateLimitError) RetryAfter() time.Duration { return e.Delay }

var httpClient = &http.Client{
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// send posts one message. Errors never contain the token (it is part of the URL).
func (p *Plugin) send(ctx context.Context, cfg config, text string, silent bool) error {
	msg := sendMessageRequest{
		ChatID:              cfg.chatID,
		MessageThreadID:     cfg.threadID,
		Text:                text,
		ParseMode:           "MarkdownV2",
		DisableNotification: silent,
	}
	if cfg.disablePreview {
		msg.LinkPreviewOptions = &linkPreviewOptions{IsDisabled: true}
	}
	body, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.apiURL+"/bot"+cfg.token+"/sendMessage", bytes.NewReader(body))
	if err != nil {
		return errors.New("Anfrage an die Bot-API konnte nicht erstellt werden")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			err = ue.Err
		}
		host := apiHost(cfg.apiURL)
		if errors.Is(err, context.DeadlineExceeded) && ctx.Err() != nil {
			return fmt.Errorf("Bot-API %s: Zeitüberschreitung nach %s", host, cfg.timeout)
		}
		return fmt.Errorf("Bot-API %s nicht erreichbar: %s", host, redact(err.Error(), cfg.token))
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("Antwort der Bot-API unvollständig: %s", redact(err.Error(), cfg.token))
	}
	var ar apiResponse
	if err := json.Unmarshal(raw, &ar); err != nil {
		if resp.StatusCode == http.StatusTooManyRequests {
			return &RateLimitError{Delay: retryAfterHeader(resp)}
		}
		return fmt.Errorf("unerwartete Antwort der Bot-API (HTTP %s): %s", resp.Status, redact(excerpt(raw, 200), cfg.token))
	}
	if ar.OK && resp.StatusCode/100 == 2 {
		return nil
	}
	return apiError(resp, ar, cfg)
}

func apiError(resp *http.Response, ar apiResponse, cfg config) error {
	code := ar.ErrorCode
	if code == 0 {
		code = resp.StatusCode
	}
	desc := redact(excerpt([]byte(ar.Description), 300), cfg.token)
	if desc == "" {
		desc = resp.Status
	}
	switch {
	case code == http.StatusTooManyRequests:
		d := time.Duration(ar.Parameters.RetryAfter) * time.Second
		if d == 0 {
			d = retryAfterHeader(resp)
		}
		return &RateLimitError{Delay: d}
	case ar.Parameters.MigrateToChatID != 0:
		return fmt.Errorf("die Gruppe wurde in eine Supergruppe umgewandelt – neue Chat-ID %d eintragen", ar.Parameters.MigrateToChatID)
	case code == http.StatusUnauthorized:
		return fmt.Errorf("Bot-Token ungültig oder widerrufen (%d: %s)", code, desc)
	case strings.Contains(strings.ToLower(desc), "chat not found"):
		return fmt.Errorf("Chat nicht gefunden (%d: %s) – Chat-ID prüfen; der Bot muss Mitglied sein bzw. mit /start angeschrieben worden sein", code, desc)
	case code == http.StatusForbidden:
		return fmt.Errorf("Bot darf in diesen Chat nicht schreiben (%d: %s)", code, desc)
	}
	return fmt.Errorf("Bot-API-Fehler %d: %s", code, desc)
}

func retryAfterHeader(resp *http.Response) time.Duration {
	if s, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After"))); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 0
}

func apiHost(apiURL string) string {
	if u, err := url.Parse(apiURL); err == nil && u.Host != "" {
		return u.Host
	}
	return "(ungültig)"
}

// redact removes the bot token from a message (defence in depth; the token is only
// part of the request URL, which is never included in errors).
func redact(s, token string) string {
	if token == "" {
		return s
	}
	return strings.ReplaceAll(s, token, "***")
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
