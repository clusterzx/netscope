// Package federation joins NetScope instances: a site delivers what it learns (its
// observations, presence transitions, device operations and events) to a central
// instance, which applies them like its own data with the site as label. Sites work on
// their own; credentials never leave them and the central instance never triggers
// anything at a site. The protocol is described in package wire.
package federation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"sync"
	"time"

	"netscope/internal/bus"
	"netscope/internal/config"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/federation/wire"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/pluginhost"
	"netscope/internal/settings"
	"netscope/internal/vault"
)

// Roles of an instance.
const (
	RoleStandalone = "standalone"
	RoleSite       = "site"
	RoleCentral    = "central"
)

// Setting keys.
const (
	keySettings = "federation"
	keyToken    = "federation.token.secret" // re-encrypted by the vault key rotation
	keyStream   = "federation.stream"
	// KeyRestored marks a database restored from a backup: the next stream of a site
	// starts with a reset (device ids are no longer the ones delivered before).
	KeyRestored = "federation.restored"
)

// Settings is the federation configuration of this instance.
type Settings struct {
	Role string `json:"role"`
	// LocalName is the name of this instance in the site selector (central instance).
	LocalName string `json:"localName"`
	// CentralURL is the base URL of the central instance (site).
	CentralURL string `json:"centralUrl"`
	// Fingerprint pins the SHA-256 fingerprint of the central instance's TLS certificate
	// (hex, optional; for self-signed certificates).
	Fingerprint string `json:"fingerprint"`
}

// SettingsView is the API view (the token is never returned).
type SettingsView struct {
	Settings
	HasToken bool `json:"hasToken"`
	// Managed: the site settings come from environment variables and are read-only.
	Managed bool `json:"managed"`
}

// SettingsInput changes the settings; a nil Token keeps the stored one.
type SettingsInput struct {
	Settings
	Token *string `json:"token,omitempty"`
}

// Deps are the services the federation uses.
type Deps struct {
	DB        *db.DB
	Bus       *bus.Bus
	Log       *slog.Logger
	Vault     *vault.Vault
	Inventory *inventory.Store
	Events    *events.Store
	Settings  *settings.Store
	Host      *pluginhost.Host
	Config    *config.Config
	Version   string
	StartedAt time.Time
}

// Service is the federation of this instance (site or central role).
type Service struct {
	Deps

	mu    sync.RWMutex
	cfg   Settings
	token string

	site    *siteState
	central *central

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New loads the settings and registers the forwarding hooks.
func New(ctx context.Context, d Deps) (*Service, error) {
	s := &Service{Deps: d, site: newSiteState(), central: newCentral()}
	var cfg Settings
	if _, err := settings.GetJSON(ctx, d.DB.R, keySettings, &cfg); err != nil {
		return nil, fmt.Errorf("Verbund-Einstellungen: %w", err)
	}
	var sealed string
	if _, err := settings.GetJSON(ctx, d.DB.R, keyToken, &sealed); err != nil {
		return nil, err
	}
	if sealed != "" {
		tok, err := d.Vault.OpenString(sealed)
		if err != nil {
			return nil, fmt.Errorf("Token der Zentrale entschlüsseln: %w", err)
		}
		s.token = tok
	}
	if cfg.Role == "" {
		cfg.Role = RoleStandalone
	}
	if d.Config != nil && d.Config.CentralURL != "" {
		cfg.Role, cfg.CentralURL, cfg.Fingerprint = RoleSite, strings.TrimRight(d.Config.CentralURL, "/"), normFingerprint(d.Config.CentralFingerprint)
		s.token = d.Config.CentralToken
	}
	s.cfg = cfg
	d.Inventory.SetForwarder(s)
	d.Events.SetForwarder(s.forwardEvent)
	return s, nil
}

// Start runs the delivery (site) or the site monitor (central).
func (s *Service) Start(ctx context.Context) {
	s.ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(2)
	go s.deliveryLoop()
	go s.monitorLoop()
}

// Stop ends the loops.
func (s *Service) Stop() {
	if s.cancel != nil {
		s.cancel()
		s.wg.Wait()
	}
}

// Role returns the role of this instance.
func (s *Service) Role() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cfg.Role
}

// Current returns the settings.
func (s *Service) Current() SettingsView {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return SettingsView{Settings: s.cfg, HasToken: s.token != "", Managed: s.managed()}
}

// LocalName is the name of this instance among the sites (default "Zentrale").
func (s *Service) LocalName() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.cfg.LocalName != "" {
		return s.cfg.LocalName
	}
	return "Zentrale"
}

func (s *Service) managed() bool { return s.Config != nil && s.Config.CentralURL != "" }

func normFingerprint(v string) string {
	v = strings.ToLower(strings.TrimSpace(v))
	v = strings.NewReplacer(":", "", " ", "", "sha256/", "").Replace(v)
	return v
}

// Update validates and stores new settings.
func (s *Service) Update(ctx context.Context, in SettingsInput) (SettingsView, error) {
	cfg := in.Settings
	cfg.LocalName = strings.TrimSpace(cfg.LocalName)
	if len([]rune(cfg.LocalName)) > 64 {
		return SettingsView{}, plugin.FieldErr("localName", "höchstens 64 Zeichen")
	}
	cfg.CentralURL = strings.TrimRight(strings.TrimSpace(cfg.CentralURL), "/")
	cfg.Fingerprint = normFingerprint(cfg.Fingerprint)
	s.mu.RLock()
	oldCfg, oldToken := s.cfg, s.token
	s.mu.RUnlock()
	if s.managed() && (cfg.Role != RoleSite || cfg.CentralURL != oldCfg.CentralURL || cfg.Fingerprint != oldCfg.Fingerprint || in.Token != nil) {
		return SettingsView{}, plugin.FieldErr("role", "Die Anbindung an die Zentrale ist über Umgebungsvariablen festgelegt (NETSCOPE_CENTRAL_URL)")
	}
	token := oldToken
	if in.Token != nil {
		token = strings.TrimSpace(*in.Token)
	}
	switch cfg.Role {
	case RoleStandalone, RoleCentral:
	case RoleSite:
		u, err := url.Parse(cfg.CentralURL)
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
			return SettingsView{}, plugin.FieldErr("centralUrl", "http(s)-URL der Zentrale erwartet, z. B. https://netscope.example.org")
		}
		if u.Path != "" && u.Path != "/" {
			cfg.CentralURL = strings.TrimRight(u.Scheme+"://"+u.Host+u.Path, "/")
		}
		if token == "" {
			return SettingsView{}, plugin.FieldErr("token", "Token des Standorts erforderlich (wird in der Zentrale beim Anlegen des Standorts angezeigt)")
		}
		if !strings.HasPrefix(token, wire.TokenPrefix) {
			return SettingsView{}, plugin.FieldErr("token", "Standort-Tokens beginnen mit "+wire.TokenPrefix)
		}
		if cfg.Fingerprint != "" && !isHex64(cfg.Fingerprint) {
			return SettingsView{}, plugin.FieldErr("fingerprint", "SHA-256-Fingerprint als 64 Hex-Zeichen erwartet")
		}
	default:
		return SettingsView{}, plugin.FieldErr("role", fmt.Sprintf("unbekannte Rolle %q", cfg.Role))
	}
	if !s.managed() {
		if err := settings.SetJSON(ctx, s.DB.W, keySettings, cfg); err != nil {
			return SettingsView{}, err
		}
		sealed := ""
		if token != "" && cfg.Role == RoleSite {
			var err error
			if sealed, err = s.Vault.SealString(token); err != nil {
				return SettingsView{}, err
			}
		} else {
			token = ""
		}
		if err := settings.SetJSON(ctx, s.DB.W, keyToken, sealed); err != nil {
			return SettingsView{}, err
		}
	}
	// a different central instance (or leaving the site role) starts a new stream
	restart := cfg.Role != RoleSite || oldCfg.Role != RoleSite || cfg.CentralURL != oldCfg.CentralURL || token != oldToken
	s.mu.Lock()
	s.cfg, s.token = cfg, token
	s.mu.Unlock()
	if restart {
		if err := s.dropStream(ctx); err != nil {
			return SettingsView{}, err
		}
		s.site.reset()
	}
	s.site.kick()
	s.Bus.Publish(bus.TopicSystem, "federation", map[string]any{"role": cfg.Role})
	return s.Current(), nil
}

func isHex64(s string) bool {
	if len(s) != 64 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

// siteConfig returns the connection settings of a site (ok false if not a site).
func (s *Service) siteConfig() (Settings, string, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ok := s.cfg.Role == RoleSite && s.cfg.CentralURL != "" && s.token != ""
	return s.cfg, s.token, ok
}

// Active implements inventory.Forwarder: this instance delivers to a central instance.
func (s *Service) Active() bool {
	_, _, ok := s.siteConfig()
	return ok
}

// Enqueue implements inventory.Forwarder.
func (s *Service) Enqueue(ctx context.Context, q db.Querier, kind string, at time.Time, data any) error {
	raw, err := wire.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "INSERT INTO federation_outbox(kind, at, data) VALUES (?,?,?)", kind, at.UnixMilli(), string(raw)); err != nil {
		return err
	}
	s.site.kick()
	return nil
}

// forwardEvent queues a new event of this instance for the central instance.
func (s *Service) forwardEvent(ctx context.Context, ev events.Event) {
	if !s.Active() {
		return
	}
	item := wire.Event{Type: ev.Type, Severity: ev.Severity, Device: ev.DeviceID, Plugin: ev.PluginID, Title: ev.Title,
		Message: ev.Message, Payload: ev.Payload, ID: ev.ID}
	if err := s.Enqueue(context.WithoutCancel(ctx), s.DB.W, wire.KindEvent, ev.TS, item); err != nil {
		s.Log.Error("Event für die Zentrale vormerken", "event", ev.ID, "err", err)
	}
}

// errInactive is returned by site operations on an instance that is not a site.
var errInactive = errors.New("diese Instanz ist nicht als Standort an eine Zentrale angebunden")
