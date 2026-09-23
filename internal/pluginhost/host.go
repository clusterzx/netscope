// Package pluginhost runs plugins: it keeps their configuration (validated against the
// settings schema, secrets encrypted in the vault), schedules runs (cron), executes them
// in a worker pool (never a plugin in parallel with itself), records run history and
// logs, retries failed runs with backoff and feeds processors with state changes.
package pluginhost

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"netscope/internal/bus"
	"netscope/internal/cron"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/settings"
	"netscope/internal/vault"
)

// Limits for the generic plugin configuration.
const (
	MinTimeout     = 10 * time.Second
	MaxTimeout     = 24 * time.Hour
	MaxRetries     = 10
	MinBackoff     = 5 * time.Second
	MaxBackoff     = 24 * time.Hour
	MaxConcurrency = 256
	defaultTimeout = 30 * time.Minute
)

// Config is the effective configuration of one plugin (settings decrypted).
type Config struct {
	PluginID     string
	Enabled      bool
	Schedule     string
	Timeout      time.Duration
	Retries      int
	RetryBackoff time.Duration
	Concurrency  int
	Scope        plugin.Scope
	Settings     map[string]any
	UpdatedAt    time.Time
}

func (c *Config) clone() *Config {
	cp := *c
	cp.Settings = make(map[string]any, len(c.Settings))
	for k, v := range c.Settings {
		cp.Settings[k] = v
	}
	return &cp
}

type progress struct {
	done, total int
	at          time.Time
}

type activeRun struct {
	runID    int64
	started  time.Time
	cancel   context.CancelCauseFunc
	mu       sync.Mutex
	progress progress
}

// Deps are the services the host needs.
type Deps struct {
	DB        *db.DB
	Bus       *bus.Bus
	Log       *slog.Logger
	Inventory *inventory.Store
	Vault     *vault.Vault
	Events    *events.Store
	Settings  *settings.Store
	DataDir   string
	Location  *time.Location
	Version   string
}

// Host manages all plugins.
type Host struct {
	Deps

	mu       sync.RWMutex
	plugins  map[string]plugin.Plugin
	configs  map[string]*Config
	active   map[string]*activeRun
	missing  map[string][]string // missing binaries per plugin
	hooks    map[string]*hookQueue
	metrics  *metrics
	wakeCh   chan struct{}
	dispatch chan struct{}
	// unreachable returns subnets that cannot be reached right now (tunnel down)
	unreachable func() []netip.Prefix

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// New creates the host for all registered plugins.
func New(d Deps) *Host {
	h := &Host{Deps: d, plugins: map[string]plugin.Plugin{}, configs: map[string]*Config{}, active: map[string]*activeRun{},
		missing: map[string][]string{}, hooks: map[string]*hookQueue{}, metrics: newMetrics(),
		wakeCh: make(chan struct{}, 1), dispatch: make(chan struct{}, 1)}
	for _, p := range plugin.All() {
		h.plugins[p.Info().ID] = p
	}
	return h
}

// Init creates missing config rows, loads all configurations and resets runs that were
// interrupted by a restart.
func (h *Host) Init(ctx context.Context) error {
	for id, p := range h.plugins {
		info := p.Info()
		for _, bin := range info.Binaries {
			if _, err := exec.LookPath(bin); err != nil {
				h.missing[id] = append(h.missing[id], bin)
			}
		}
		if err := h.ensureConfig(ctx, p); err != nil {
			return fmt.Errorf("plugin %s: %w", id, err)
		}
		cfg, err := h.loadConfig(ctx, p)
		if err != nil {
			return fmt.Errorf("plugin %s: %w", id, err)
		}
		h.configs[id] = cfg
	}
	now := db.Now()
	if _, err := h.DB.W.ExecContext(ctx, `UPDATE runs SET status = 'cancelled', finished_at = ?, error = 'Abgebrochen: NetScope wurde neu gestartet'
		WHERE status = 'running'`, now); err != nil {
		return err
	}
	return nil
}

func defaultsFor(info plugin.Info) (timeout time.Duration, concurrency int) {
	timeout = info.DefaultTimeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	concurrency = info.DefaultConcurrency
	if concurrency < 1 {
		concurrency = 1
	}
	return timeout, concurrency
}

func (h *Host) ensureConfig(ctx context.Context, p plugin.Plugin) error {
	info := p.Info()
	var n int
	if err := h.DB.W.QueryRowContext(ctx, "SELECT COUNT(*) FROM plugin_configs WHERE plugin_id = ?", info.ID).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	timeout, conc := defaultsFor(info)
	pub, sec := p.Schema().SplitSecrets(p.Schema().Defaults())
	blob, err := h.sealSecrets(sec)
	if err != nil {
		return err
	}
	scope := plugin.DefaultScope()
	_, err = h.DB.W.ExecContext(ctx, `INSERT INTO plugin_configs(plugin_id, enabled, schedule, timeout_s, retries, retry_backoff_s, concurrency,
		scope, settings, secrets, secrets_key_id, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
		info.ID, db.Bool(info.DefaultEnabled), info.DefaultSchedule, int(timeout/time.Second), info.DefaultRetries, 60, conc,
		db.JSON(scope), db.JSON(pub), blob, h.Vault.KeyID(), db.Now())
	return err
}

func (h *Host) sealSecrets(sec map[string]any) ([]byte, error) {
	clean := map[string]any{}
	for k, v := range sec {
		if s, ok := v.(string); ok && s != "" {
			clean[k] = s
		}
	}
	if len(clean) == 0 {
		return nil, nil
	}
	b, err := json.Marshal(clean)
	if err != nil {
		return nil, err
	}
	return h.Vault.Encrypt(b)
}

func (h *Host) loadConfig(ctx context.Context, p plugin.Plugin) (*Config, error) {
	var (
		c                       Config
		timeoutS, backoffS      int
		scopeJSON, settingsJSON string
		secrets                 []byte
		updated                 int64
	)
	err := h.DB.R.QueryRowContext(ctx, `SELECT plugin_id, enabled, schedule, timeout_s, retries, retry_backoff_s, concurrency, scope, settings, secrets, updated_at
		FROM plugin_configs WHERE plugin_id = ?`, p.Info().ID).Scan(&c.PluginID, &c.Enabled, &c.Schedule, &timeoutS, &c.Retries, &backoffS,
		&c.Concurrency, &scopeJSON, &settingsJSON, &secrets, &updated)
	if err != nil {
		return nil, err
	}
	c.Timeout, c.RetryBackoff, c.UpdatedAt = time.Duration(timeoutS)*time.Second, time.Duration(backoffS)*time.Second, db.Time(updated)
	if err := db.Unmarshal(scopeJSON, &c.Scope); err != nil {
		c.Scope = plugin.DefaultScope()
	}
	stored := map[string]any{}
	_ = db.Unmarshal(settingsJSON, &stored)
	if len(secrets) > 0 {
		plain, err := h.Vault.Decrypt(secrets)
		if err != nil {
			return nil, fmt.Errorf("Secrets entschlüsseln: %w", err)
		}
		sec := map[string]any{}
		if err := json.Unmarshal(plain, &sec); err != nil {
			return nil, err
		}
		for k, v := range sec {
			stored[k] = v
		}
	}
	if m, ok := p.(plugin.SettingsMigrator); ok {
		stored = m.MigrateSettings(stored)
	}
	c.Settings = p.Schema().Normalize(stored)
	if _, ok := p.(plugin.Runner); !ok {
		c.Schedule = ""
	}
	return &c, nil
}

// Plugin returns a registered plugin.
func (h *Host) Plugin(id string) (plugin.Plugin, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	p, ok := h.plugins[id]
	return p, ok
}

// Config returns a copy of a plugin's configuration.
func (h *Host) Config(id string) (*Config, bool) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	c, ok := h.configs[id]
	if !ok {
		return nil, false
	}
	return c.clone(), true
}

// IDs returns all plugin ids sorted by kind and id.
func (h *Host) IDs() []string {
	var out []string
	for _, p := range plugin.All() {
		if _, ok := h.plugins[p.Info().ID]; ok {
			out = append(out, p.Info().ID)
		}
	}
	return out
}

// Env returns the plugin environment.
func (h *Host) Env() plugin.Env {
	return plugin.Env{PublicURL: h.Settings.System().PublicURL, Location: h.Location, Version: h.Version, DataRoot: h.DataDir}
}

func (h *Host) dataDir(id string) string {
	dir := filepath.Join(h.DataDir, "plugins", id)
	_ = os.MkdirAll(dir, 0o750)
	return dir
}

// ConfigInput changes the configuration; nil fields stay unchanged. Settings is the full
// settings object (secret fields may carry plugin.SecretMask to keep their value).
type ConfigInput struct {
	Enabled             *bool          `json:"enabled,omitempty"`
	Schedule            *string        `json:"schedule,omitempty"`
	TimeoutSeconds      *int           `json:"timeoutSeconds,omitempty"`
	Retries             *int           `json:"retries,omitempty"`
	RetryBackoffSeconds *int           `json:"retryBackoffSeconds,omitempty"`
	Concurrency         *int           `json:"concurrency,omitempty"`
	Scope               *plugin.Scope  `json:"scope,omitempty"`
	Settings            map[string]any `json:"settings,omitempty"`
}

// ConfigError is a validation error of the generic configuration.
type ConfigError struct{ Field, Message string }

func (e *ConfigError) Error() string { return e.Field + ": " + e.Message }

func (h *Host) validateScope(ctx context.Context, s *plugin.Scope) error {
	var subnets []string
	for _, c := range s.Subnets {
		p, err := plugin.ParsePrefix(c)
		if err != nil {
			return &ConfigError{"scope.subnets", err.Error()}
		}
		subnets = append(subnets, p.String())
	}
	s.Subnets = subnets
	for _, g := range s.Groups {
		var n int
		if err := h.DB.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM groups WHERE id = ?", g).Scan(&n); err != nil || n == 0 {
			return &ConfigError{"scope.groups", fmt.Sprintf("Gruppe %d existiert nicht", g)}
		}
	}
	var tags []string
	for _, t := range s.Tags {
		if t = strings.ToLower(strings.TrimSpace(t)); t != "" {
			tags = append(tags, t)
		}
	}
	s.Tags = tags
	if s.Query != "" {
		if _, _, err := h.Inventory.CompileQuery(ctx, s.Query); err != nil {
			return &ConfigError{"scope.query", err.Error()}
		}
	}
	return nil
}

// UpdateConfig validates and stores a configuration change. It returns the masked
// configuration before and after (for the audit log).
func (h *Host) UpdateConfig(ctx context.Context, id string, in ConfigInput) (before, after *ConfigView, err error) {
	p, ok := h.Plugin(id)
	if !ok {
		return nil, nil, db.ErrNotFound
	}
	cur, _ := h.Config(id)
	before = h.configView(p, cur)
	next := cur.clone()
	_, isRunner := p.(plugin.Runner)
	if in.Enabled != nil {
		next.Enabled = *in.Enabled
	}
	if in.Schedule != nil {
		s := strings.TrimSpace(*in.Schedule)
		if s != "" {
			if !isRunner {
				return nil, nil, &ConfigError{"schedule", "dieses Plugin hat keine Läufe"}
			}
			if err := cron.Validate(s); err != nil {
				return nil, nil, &ConfigError{"schedule", err.Error()}
			}
		}
		next.Schedule = s
	}
	if in.TimeoutSeconds != nil {
		d := time.Duration(*in.TimeoutSeconds) * time.Second
		if d < MinTimeout || d > MaxTimeout {
			return nil, nil, &ConfigError{"timeoutSeconds", fmt.Sprintf("muss zwischen %s und %s liegen", MinTimeout, MaxTimeout)}
		}
		next.Timeout = d
	}
	if in.Retries != nil {
		if *in.Retries < 0 || *in.Retries > MaxRetries {
			return nil, nil, &ConfigError{"retries", fmt.Sprintf("0–%d erlaubt", MaxRetries)}
		}
		next.Retries = *in.Retries
	}
	if in.RetryBackoffSeconds != nil {
		d := time.Duration(*in.RetryBackoffSeconds) * time.Second
		if d < MinBackoff || d > MaxBackoff {
			return nil, nil, &ConfigError{"retryBackoffSeconds", fmt.Sprintf("muss zwischen %s und %s liegen", MinBackoff, MaxBackoff)}
		}
		next.RetryBackoff = d
	}
	if in.Concurrency != nil {
		if *in.Concurrency < 1 || *in.Concurrency > MaxConcurrency {
			return nil, nil, &ConfigError{"concurrency", fmt.Sprintf("1–%d erlaubt", MaxConcurrency)}
		}
		next.Concurrency = *in.Concurrency
	}
	if in.Scope != nil {
		sc := *in.Scope
		if err := h.validateScope(ctx, &sc); err != nil {
			return nil, nil, err
		}
		next.Scope = sc
	}
	if in.Settings != nil {
		vals, err := p.Schema().Validate(in.Settings, cur.Settings, func(cid int64, types []string) error {
			return h.Vault.Check(ctx, cid, types)
		})
		if err != nil {
			return nil, nil, err
		}
		if v, ok := p.(plugin.SettingsValidator); ok {
			if err := v.ValidateSettings(plugin.NewSettings(vals)); err != nil {
				return nil, nil, err
			}
		}
		next.Settings = vals
	}
	pub, sec := p.Schema().SplitSecrets(next.Settings)
	blob, err := h.sealSecrets(sec)
	if err != nil {
		return nil, nil, err
	}
	next.UpdatedAt = time.Now()
	_, err = h.DB.W.ExecContext(ctx, `UPDATE plugin_configs SET enabled = ?, schedule = ?, timeout_s = ?, retries = ?, retry_backoff_s = ?,
		concurrency = ?, scope = ?, settings = ?, secrets = ?, secrets_key_id = ?, updated_at = ? WHERE plugin_id = ?`,
		db.Bool(next.Enabled), next.Schedule, int(next.Timeout/time.Second), next.Retries, int(next.RetryBackoff/time.Second),
		next.Concurrency, db.JSON(next.Scope), db.JSON(pub), blob, h.Vault.KeyID(), next.UpdatedAt.UnixMilli(), id)
	if err != nil {
		return nil, nil, err
	}
	h.mu.Lock()
	h.configs[id] = next
	h.mu.Unlock()
	h.wake()
	h.Bus.Publish(bus.TopicPlugin, "updated", map[string]any{"id": id})
	return before, h.configView(p, next), nil
}

// PublisherEnabled reports whether a publisher plugin exists and is enabled.
func (h *Host) PublisherEnabled(id string) bool {
	p, ok := h.Plugin(id)
	if !ok {
		return false
	}
	if _, ok := p.(plugin.Publisher); !ok {
		return false
	}
	c, _ := h.Config(id)
	return c != nil && c.Enabled
}

// Publishers lists all publisher plugins (id, name, enabled).
func (h *Host) Publishers() []PublisherInfo {
	var out []PublisherInfo
	for _, id := range h.IDs() {
		p, _ := h.Plugin(id)
		if _, ok := p.(plugin.Publisher); !ok {
			continue
		}
		c, _ := h.Config(id)
		out = append(out, PublisherInfo{ID: id, Name: p.Info().Name, Enabled: c.Enabled})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// PublisherInfo is a short publisher description.
type PublisherInfo struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Enabled bool   `json:"enabled"`
}

// ErrPublisherDisabled is returned when a notification targets a disabled publisher.
var ErrPublisherDisabled = errors.New("Publisher ist deaktiviert")

// Publish delivers a notification through a publisher plugin.
func (h *Host) Publish(ctx context.Context, id string, n *plugin.Notification) error {
	p, ok := h.Plugin(id)
	if !ok {
		return fmt.Errorf("Publisher %q existiert nicht", id)
	}
	pub, ok := p.(plugin.Publisher)
	if !ok {
		return fmt.Errorf("Plugin %q ist kein Publisher", id)
	}
	cfg, _ := h.Config(id)
	if !cfg.Enabled && n.Kind != plugin.NotifyTest {
		return ErrPublisherDisabled
	}
	ctx, cancel := context.WithTimeout(ctx, cfg.Timeout)
	defer cancel()
	pc := &plugin.PublishContext{PluginID: id, Settings: plugin.NewSettings(cfg.Settings), Log: h.Log.With("plugin", id),
		Creds: h.CredentialProvider(), Env: h.Env()}
	start := time.Now()
	err := safeCall(func() error { return pub.Publish(ctx, pc, n) })
	h.metrics.notification(id, err == nil, time.Since(start))
	return err
}

// safeCall converts panics into errors.
func safeCall(fn func() error) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("Plugin-Panic: %v", r)
		}
	}()
	return fn()
}

func (h *Host) wake() {
	select {
	case h.wakeCh <- struct{}{}:
	default:
	}
}

func (h *Host) kick() {
	select {
	case h.dispatch <- struct{}{}:
	default:
	}
}

// Start runs the scheduler, dispatcher and hook workers until Stop.
func (h *Host) Start(ctx context.Context) {
	h.ctx, h.cancel = context.WithCancel(ctx)
	for _, id := range h.IDs() {
		p := h.plugins[id]
		_, isCH := p.(plugin.ChangeHandler)
		_, isRF := p.(plugin.RunFinishedHandler)
		if isCH || isRF {
			q := newHookQueue()
			h.hooks[id] = q
			h.wg.Add(1)
			go h.hookWorker(id, q)
		}
	}
	h.wg.Add(2)
	go h.schedulerLoop()
	go h.dispatcherLoop()
	h.kick()
}

// Stop cancels running plugins and waits for them to finish (bounded by ctx).
func (h *Host) Stop(ctx context.Context) {
	if h.cancel == nil {
		return
	}
	h.mu.RLock()
	for _, a := range h.active {
		a.cancel(errShutdown)
	}
	h.mu.RUnlock()
	h.cancel()
	done := make(chan struct{})
	go func() { h.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		h.Log.Warn("plugins did not stop in time")
	}
}

var (
	errShutdown  = errors.New("Abgebrochen: NetScope wird beendet")
	errCancelled = errors.New("Abgebrochen durch Benutzer")
)

// scanNullableInt helps scanning optional integer columns.
func nullInt(v sql.NullInt64) int64 {
	if v.Valid {
		return v.Int64
	}
	return 0
}
