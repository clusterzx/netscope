// Package email delivers notifications by SMTP as multipart/alternative messages
// (text/plain + inline-styled text/html).
//
// Transport security: "starttls" requires the server to offer STARTTLS and aborts
// otherwise (no silent downgrade), "tls" uses implicit TLS (SMTPS, port 465), "none"
// sends in clear text. Credentials are only sent over TLS, except to localhost.
package email

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/mail"
	"net/smtp"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

const (
	defaultTimeout = 20 * time.Second
	mailer         = "NetScope/1.0"
)

// Plugin is the e-mail publisher.
type Plugin struct {
	// rootCAs overrides the system trust store (tests); nil = system roots.
	rootCAs *x509.CertPool
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "email",
		Kind:               plugin.KindPublisher,
		Name:               "E-Mail (SMTP)",
		Description:        "Versendet Benachrichtigungen und Berichte als E-Mail (Text und HTML) über einen SMTP-Server.",
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
		{Key: "host", Type: plugin.FieldString, Label: "SMTP-Server", Required: true, Group: "Server",
			Placeholder: "smtp.example.org",
			Validation:  &plugin.Validation{Format: "host"}},
		{Key: "port", Type: plugin.FieldInt, Label: "Port", Default: 587, Group: "Server",
			Description: "Üblich: 587 für STARTTLS, 465 für TLS, 25 für interne Relays.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(65535)}},
		{Key: "security", Type: plugin.FieldEnum, Label: "Verschlüsselung", Default: "starttls", Group: "Server",
			Description: "Bei STARTTLS wird abgebrochen, wenn der Server kein STARTTLS anbietet.",
			Options: []plugin.Option{
				{Value: "starttls", Label: "STARTTLS (Port 587)"},
				{Value: "tls", Label: "TLS/SMTPS (Port 465)"},
				{Value: "none", Label: "Keine (nur vertrauenswürdige Relays)"},
			}},
		{Key: "credential", Type: plugin.FieldCredentialRef, Label: "Anmeldung", Group: "Server",
			CredentialTypes: []string{plugin.CredPassword},
			Description: "Optional: Benutzer/Passwort für SMTP-AUTH (PLAIN oder LOGIN). Ohne Benutzername wird die " +
				"Absenderadresse verwendet. Wird nur über TLS gesendet (Ausnahme: localhost)."},
		{Key: "from", Type: plugin.FieldString, Label: "Absender", Required: true, Group: "Nachricht",
			Description: "Adresse, optional mit Namen: NetScope <netscope@example.org>",
			Placeholder: "NetScope <netscope@example.org>",
			Validation:  &plugin.Validation{Format: "email"}},
		{Key: "to", Type: plugin.FieldStringList, Label: "Empfänger", Required: true, Group: "Nachricht",
			Description: "Eine Adresse pro Zeile.",
			Validation:  &plugin.Validation{Format: "email", Min: plugin.Int64(1), Max: plugin.Int64(50)}},
		{Key: "subject_prefix", Type: plugin.FieldString, Label: "Betreff-Präfix", Default: "[NetScope]", Group: "Nachricht",
			Validation: &plugin.Validation{Max: plugin.Int64(50)}},
		{Key: "helo", Type: plugin.FieldString, Label: "HELO-Name", Advanced: true,
			Description: "Hostname für EHLO/HELO. Leer = Hostname dieses Systems.",
			Validation:  &plugin.Validation{Format: "host"}},
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Timeout", Default: "20s", Advanced: true,
			Description: "Maximale Dauer der gesamten SMTP-Sitzung.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(300)}},
		{Key: "verify_tls", Type: plugin.FieldBool, Label: "TLS-Zertifikat prüfen", Default: true, Advanced: true,
			Description: "Nur für Server mit selbstsigniertem Zertifikat im eigenen Netz abschalten."},
	}}
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	var errs []plugin.FieldError
	if s.String("security") == "none" && s.CredentialID("credential") > 0 && !isLocalhost(s.String("host")) {
		errs = append(errs, plugin.FieldError{Field: "credential",
			Message: "Anmeldung ohne Verschlüsselung ist nur für localhost erlaubt – STARTTLS oder TLS wählen"})
	}
	if _, err := parseRecipients(s.StringList("to")); err != nil {
		errs = append(errs, plugin.FieldError{Field: "to", Message: err.Error()})
	}
	if len(errs) > 0 {
		return &plugin.ValidationError{Errors: errs}
	}
	return nil
}

type config struct {
	host      string
	port      int
	security  string
	credID    int64
	from      *mail.Address
	to        []*mail.Address
	prefix    string
	helo      string
	timeout   time.Duration
	verifyTLS bool
}

func loadConfig(s plugin.Settings) (config, error) {
	c := config{
		host:      strings.TrimSpace(s.String("host")),
		port:      s.Int("port"),
		security:  s.String("security"),
		credID:    s.CredentialID("credential"),
		prefix:    s.String("subject_prefix"),
		helo:      strings.TrimSpace(s.String("helo")),
		timeout:   s.Duration("timeout"),
		verifyTLS: s.Bool("verify_tls"),
	}
	if c.host == "" {
		return c, errors.New("kein SMTP-Server konfiguriert")
	}
	if c.port <= 0 || c.port > 65535 {
		c.port = 587
	}
	switch c.security {
	case "starttls", "tls", "none":
	default:
		c.security = "starttls"
	}
	from, err := mail.ParseAddress(s.String("from"))
	if err != nil {
		return c, errors.New("ungültige Absenderadresse")
	}
	c.from = from
	if c.to, err = parseRecipients(s.StringList("to")); err != nil {
		return c, err
	}
	if len(c.to) == 0 {
		return c, errors.New("keine Empfänger konfiguriert")
	}
	if c.helo == "" {
		c.helo = localName()
	}
	if c.timeout <= 0 {
		c.timeout = defaultTimeout
	}
	return c, nil
}

func parseRecipients(list []string) ([]*mail.Address, error) {
	var out []*mail.Address
	seen := map[string]bool{}
	for _, s := range list {
		a, err := mail.ParseAddress(s)
		if err != nil {
			return nil, fmt.Errorf("ungültige Empfängeradresse %q", s)
		}
		if key := strings.ToLower(a.Address); !seen[key] {
			seen[key] = true
			out = append(out, a)
		}
	}
	return out, nil
}

// localName returns a HELO name: the host name of this system if it is a valid DNS
// name, otherwise "localhost".
func localName() string {
	h, err := os.Hostname()
	if err != nil || h == "" || len(h) > 253 {
		return "localhost"
	}
	for _, r := range h {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '-' || r == '.') {
			return "localhost"
		}
	}
	return h
}

// isLocalhost mirrors net/smtp: PLAIN auth without TLS is accepted for these names only.
func isLocalhost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// authAllowed reports whether credentials may be sent on this connection.
func authAllowed(tlsActive bool, host string) bool {
	return tlsActive || isLocalhost(host)
}

type credentials struct {
	user, pass string
}

// Publish implements plugin.Publisher.
func (p *Plugin) Publish(ctx context.Context, pc *plugin.PublishContext, n *plugin.Notification) error {
	cfg, err := loadConfig(pc.Settings)
	if err != nil {
		return fmt.Errorf("E-Mail: %w", err)
	}
	var cr *credentials
	if cfg.credID > 0 {
		if cr, err = loadCredentials(ctx, pc, cfg); err != nil {
			return fmt.Errorf("E-Mail: %w", err)
		}
	}
	msg, err := buildMessage(n, envelope{
		From:   cfg.from,
		To:     cfg.to,
		Prefix: cfg.prefix,
		Now:    time.Now(),
		Loc:    pc.Env.Location,
		Mailer: mailer,
	})
	if err != nil {
		return fmt.Errorf("E-Mail: Nachricht konnte nicht erzeugt werden: %w", err)
	}
	if err := p.deliver(ctx, cfg, cr, msg); err != nil {
		return fmt.Errorf("E-Mail: %w", err)
	}
	logger(pc).Debug("E-Mail versendet", "notification", n.ID, "kind", n.Kind, "events", len(n.Events),
		"recipients", len(cfg.to), "server", net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port)), "security", cfg.security)
	return nil
}

func loadCredentials(ctx context.Context, pc *plugin.PublishContext, cfg config) (*credentials, error) {
	if pc.Creds == nil {
		return nil, errors.New("Credential-Zugriff nicht verfügbar")
	}
	cred, err := pc.Creds.Get(ctx, cfg.credID)
	if err != nil {
		return nil, fmt.Errorf("Credential %d: %w", cfg.credID, err)
	}
	if err := cred.RequireType(plugin.CredPassword); err != nil {
		return nil, err
	}
	cr := &credentials{user: cred.Get("username"), pass: cred.Get("password")}
	if cr.user == "" {
		cr.user = cfg.from.Address
	}
	if cr.pass == "" {
		return nil, fmt.Errorf("Credential %q: Passwort fehlt", cred.Name)
	}
	return cr, nil
}

// deliver runs one SMTP session. net/smtp has no context support: the connection gets
// the context deadline and is closed when the context ends.
func (p *Plugin) deliver(ctx context.Context, cfg config, cr *credentials, msg []byte) (err error) {
	ctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	addr := net.JoinHostPort(cfg.host, strconv.Itoa(cfg.port))

	var d net.Dialer
	raw, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("%s: Zeitüberschreitung beim Verbindungsaufbau (%s)", addr, cfg.timeout)
		}
		return fmt.Errorf("%s nicht erreichbar: %w", addr, err)
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { raw.Close() })
	defer stop()
	if dl, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(dl)
	}
	defer func() {
		// the connection deadline equals the context deadline and may fire first
		var ne net.Error
		if err != nil && (ctx.Err() != nil || (errors.As(err, &ne) && ne.Timeout())) {
			err = fmt.Errorf("%s: Zeitüberschreitung oder Abbruch nach %s (%w)%s", addr, cfg.timeout, err, portHint(cfg))
		}
	}()

	tlsCfg := &tls.Config{
		ServerName:         cfg.host,
		MinVersion:         tls.VersionTLS12,
		RootCAs:            p.rootCAs,
		InsecureSkipVerify: !cfg.verifyTLS, //nolint:gosec // explicit user setting verify_tls=false
	}
	conn := raw
	if cfg.security == "tls" {
		tc := tls.Client(raw, tlsCfg)
		if err := tc.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("TLS-Handshake mit %s fehlgeschlagen: %w%s", addr, err, portHint(cfg))
		}
		conn = tc
	}
	c, err := smtp.NewClient(conn, cfg.host)
	if err != nil {
		return fmt.Errorf("keine gültige SMTP-Begrüßung von %s: %w%s", addr, err, portHint(cfg))
	}
	defer c.Close()
	if err := c.Hello(cfg.helo); err != nil {
		return fmt.Errorf("EHLO abgelehnt: %w", err)
	}
	if cfg.security == "starttls" {
		if ok, _ := c.Extension("STARTTLS"); !ok {
			return fmt.Errorf("%s bietet kein STARTTLS an – Versand abgebrochen (Verschlüsselung „Keine“ nur für vertrauenswürdige Relays)", addr)
		}
		if err := c.StartTLS(tlsCfg); err != nil {
			return fmt.Errorf("STARTTLS fehlgeschlagen: %w", err)
		}
	}
	if cr != nil {
		if err := authenticate(c, cfg.host, cr); err != nil {
			return err
		}
	}
	if err := c.Mail(cfg.from.Address); err != nil {
		return fmt.Errorf("Absender %s abgelehnt: %w", cfg.from.Address, err)
	}
	for _, rcpt := range cfg.to {
		if err := c.Rcpt(rcpt.Address); err != nil {
			return fmt.Errorf("Empfänger %s abgelehnt: %w", rcpt.Address, err)
		}
	}
	w, err := c.Data()
	if err != nil {
		return fmt.Errorf("DATA abgelehnt: %w", err)
	}
	if _, err := w.Write(msg); err != nil {
		return fmt.Errorf("Senden der Nachricht fehlgeschlagen: %w", err)
	}
	if err := w.Close(); err != nil {
		return fmt.Errorf("Nachricht abgelehnt: %w", err)
	}
	// The message is accepted at this point; a failing QUIT does not matter.
	_ = c.Quit()
	return nil
}

func portHint(cfg config) string {
	switch {
	case cfg.security == "starttls" && cfg.port == 465:
		return " – Port 465 erwartet üblicherweise Verschlüsselung „TLS“"
	case cfg.security == "tls" && (cfg.port == 587 || cfg.port == 25):
		return fmt.Sprintf(" – Port %d erwartet üblicherweise STARTTLS", cfg.port)
	}
	return ""
}

func authenticate(c *smtp.Client, host string, cr *credentials) error {
	ok, mechs := c.Extension("AUTH")
	if !ok {
		return errors.New("Server bietet keine Anmeldung (AUTH) an – Credential entfernen oder Server prüfen")
	}
	_, tlsActive := c.TLSConnectionState()
	if !authAllowed(tlsActive, host) {
		return errors.New("Anmeldung ohne TLS verweigert – Verschlüsselung STARTTLS oder TLS wählen")
	}
	list := strings.Fields(strings.ToUpper(mechs))
	var a smtp.Auth
	switch {
	case slices.Contains(list, "PLAIN"):
		a = smtp.PlainAuth("", cr.user, cr.pass, host)
	case slices.Contains(list, "LOGIN"):
		a = &loginAuth{user: cr.user, pass: cr.pass, host: host}
	default:
		return fmt.Errorf("keine unterstützte Anmeldemethode (PLAIN, LOGIN) – Server bietet: %s", mechs)
	}
	if err := c.Auth(a); err != nil {
		return fmt.Errorf("Anmeldung fehlgeschlagen: %w", err)
	}
	return nil
}

// loginAuth implements the (non-standard but widespread) AUTH LOGIN mechanism.
type loginAuth struct {
	user, pass, host string
}

func (a *loginAuth) Start(server *smtp.ServerInfo) (string, []byte, error) {
	if !authAllowed(server.TLS, server.Name) {
		return "", nil, errors.New("unverschlüsselte Verbindung")
	}
	if server.Name != a.host {
		return "", nil, errors.New("falscher Servername")
	}
	return "LOGIN", nil, nil
}

func (a *loginAuth) Next(fromServer []byte, more bool) ([]byte, error) {
	if !more {
		return nil, nil
	}
	prompt := strings.ToLower(string(fromServer))
	switch {
	case strings.Contains(prompt, "user"):
		return []byte(a.user), nil
	case strings.Contains(prompt, "pass"):
		return []byte(a.pass), nil
	}
	return nil, errors.New("unerwartete Aufforderung bei AUTH LOGIN")
}

func logger(pc *plugin.PublishContext) *slog.Logger {
	if pc.Log != nil {
		return pc.Log
	}
	return slog.New(slog.DiscardHandler)
}
