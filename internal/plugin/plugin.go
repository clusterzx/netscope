// Package plugin is the SDK every NetScope capability is written against.
//
// A plugin is a Go type implementing Plugin plus one or more capability interfaces:
//
//   - Runner          scheduled/manual runs (scanners, importers, processor jobs)
//   - Publisher       delivers notifications (publishers)
//   - ChangeHandler   reacts to state changes written by the core (processors)
//   - RunFinishedHandler reacts to finished runs of other plugins
//   - ActionProvider  exposes buttons on the plugin page or per device (e.g. WOL)
//
// Plugins register themselves in init() via Register; internal/plugins/all imports them.
// The settings form in the UI is rendered from Schema() – no plugin ships its own UI.
package plugin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"regexp"
	"sort"
	"sync"
	"time"

	"netscope/internal/db"
)

// Kind is one of the four plugin types.
type Kind string

const (
	KindScanner   Kind = "scanner"
	KindImporter  Kind = "importer"
	KindProcessor Kind = "processor"
	KindPublisher Kind = "publisher"
)

// Kinds lists all kinds in display order.
var Kinds = []Kind{KindScanner, KindImporter, KindProcessor, KindPublisher}

// TargetMode tells the core how to resolve a run's scope into Targets.
type TargetMode string

const (
	// TargetNone: the plugin does not work on network targets (importers, processors).
	TargetNone TargetMode = ""
	// TargetSubnets: the plugin scans address ranges. If the scope is restricted to
	// devices/groups/tags, Targets.DeviceMode is set and only Targets.Devices are scanned.
	TargetSubnets TargetMode = "subnets"
	// TargetDevices: the plugin works on known devices (Targets.Devices).
	TargetDevices TargetMode = "devices"
)

// Info describes a plugin. Defaults are applied when the plugin config is first created.
type Info struct {
	ID          string `json:"id"`
	Kind        Kind   `json:"kind"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`

	DefaultEnabled     bool          `json:"defaultEnabled"`
	DefaultSchedule    string        `json:"defaultSchedule"` // cron, "" = manual only
	DefaultTimeout     time.Duration `json:"defaultTimeoutNs"`
	DefaultConcurrency int           `json:"defaultConcurrency"`
	DefaultRetries     int           `json:"defaultRetries"`

	Targets  TargetMode `json:"targets"`
	Presence bool       `json:"presence"` // runs decide online/offline state
	Binaries []string   `json:"binaries,omitempty"`
}

// Plugin is implemented by every plugin.
type Plugin interface {
	Info() Info
	Schema() Schema
}

// Runner executes a run. Results are written through rc.Sink as soon as they are known
// (per host), never collected until the end. Run must return promptly when ctx is done.
type Runner interface {
	Plugin
	Run(ctx context.Context, rc *RunContext) error
}

// Publisher delivers a notification.
type Publisher interface {
	Plugin
	Publish(ctx context.Context, pc *PublishContext, n *Notification) error
}

// ChangeHandler receives the state changes the core derived from observations.
// It is called asynchronously (never blocks scanners), in order, per plugin.
type ChangeHandler interface {
	Plugin
	HandleChanges(ctx context.Context, rc *RunContext, changes []Change) error
}

// RunFinishedHandler is notified after any run of another plugin finished.
type RunFinishedHandler interface {
	Plugin
	HandleRunFinished(ctx context.Context, rc *RunContext, run RunSummary) error
}

// ActionProvider exposes actions (buttons) on the plugin page or on devices.
type ActionProvider interface {
	Plugin
	Actions() []Action
	RunAction(ctx context.Context, rc *RunContext, name string, params map[string]any) (*ActionResult, error)
}

// SettingsValidator performs cross-field validation after schema validation.
type SettingsValidator interface {
	ValidateSettings(s Settings) error
}

// ActionScope says where an action button is shown.
type ActionScope string

const (
	ActionPlugin ActionScope = "plugin" // plugin page
	ActionDevice ActionScope = "device" // device detail / bulk actions; rc.Targets.Devices holds the device(s)
)

// Action is a user-triggered operation.
type Action struct {
	Name        string      `json:"name"`
	Label       string      `json:"label"`
	Description string      `json:"description,omitempty"`
	Scope       ActionScope `json:"scope"`
	Params      []Field     `json:"params,omitempty"`
	Confirm     string      `json:"confirm,omitempty"` // confirmation question, if any
}

// ActionResult is returned to the UI.
type ActionResult struct {
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Scope restricts which subnets/devices a plugin works on.
type Scope struct {
	AllSubnets bool     `json:"allSubnets"`
	Subnets    []string `json:"subnets,omitempty"` // CIDRs of configured subnets
	Groups     []int64  `json:"groups,omitempty"`
	Tags       []string `json:"tags,omitempty"`
	Devices    []int64  `json:"devices,omitempty"`
	Query      string   `json:"query,omitempty"` // device filter query language
}

// DeviceRestricted reports whether the scope selects devices rather than subnets.
func (s Scope) DeviceRestricted() bool {
	return len(s.Groups) > 0 || len(s.Tags) > 0 || len(s.Devices) > 0 || s.Query != ""
}

// DefaultScope covers all enabled subnets.
func DefaultScope() Scope { return Scope{AllSubnets: true} }

// SubnetTarget is a configured subnet.
type SubnetTarget struct {
	ID        int64        `json:"id"`
	CIDR      netip.Prefix `json:"cidr"`
	Name      string       `json:"name"`
	Interface string       `json:"interface"`
	Gateway   string       `json:"gateway"`
}

// PortRef is an open port of a known device.
type PortRef struct {
	IP      string `json:"ip"`
	Proto   string `json:"proto"`
	Port    int    `json:"port"`
	Service string `json:"service"`
	Product string `json:"product"`
	Version string `json:"version"`
	Tunnel  string `json:"tunnel"`
}

// DeviceInfo is the read model of a device handed to plugins.
type DeviceInfo struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"` // display name, falls back to hostname / IP
	Hostname    string    `json:"hostname"`
	Vendor      string    `json:"vendor"`
	Model       string    `json:"model"`
	Type        string    `json:"type"`
	OS          string    `json:"os"`
	PrimaryIP   string    `json:"primaryIp"`
	PrimaryMAC  string    `json:"primaryMac"`
	IPs         []string  `json:"ips"`
	MACs        []string  `json:"macs"`
	Tags        []string  `json:"tags"`
	State       string    `json:"state"`
	Criticality string    `json:"criticality"`
	Online      bool      `json:"online"`
	LastSeen    time.Time `json:"lastSeen"`
	Ports       []PortRef `json:"ports,omitempty"`
}

// Targets is the resolved scope of a run.
type Targets struct {
	Subnets []SubnetTarget `json:"subnets"`
	Devices []DeviceInfo   `json:"devices"`
	// DeviceMode is true when the scope selected devices (groups, tags, explicit devices,
	// query). Subnet scanners must then only scan the device addresses.
	DeviceMode bool `json:"deviceMode"`
}

// DeviceIPs returns all addresses of the target devices (primary first, deduplicated).
func (t Targets) DeviceIPs() []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range t.Devices {
		for _, ip := range append([]string{d.PrimaryIP}, d.IPs...) {
			if ip != "" && !seen[ip] {
				seen[ip] = true
				out = append(out, ip)
			}
		}
	}
	return out
}

// Env holds global, non-plugin-specific settings.
type Env struct {
	PublicURL string         // base URL for deep links into the UI (no trailing slash)
	Location  *time.Location // configured time zone
	Version   string
	DataRoot  string // NetScope data directory (/data); plugins use DataDir for own files
}

// Sink receives observations. Observe writes synchronously (one transaction per call)
// and returns the id of the matched or created device (0 if none).
type Sink interface {
	Observe(ctx context.Context, obs *Observation) (int64, error)
}

// EventEmitter stores events (processors, publishers never emit).
type EventEmitter interface {
	Emit(ctx context.Context, ev Event) (int64, error)
}

// CredentialProvider decrypts vault credentials referenced by credential-ref fields.
type CredentialProvider interface {
	Get(ctx context.Context, id int64) (*Credential, error)
}

// InventoryReader gives read access to the inventory.
type InventoryReader interface {
	Device(ctx context.Context, id int64) (*DeviceInfo, error)
	// Devices returns devices matching a filter query ("" = all non-ignored devices).
	Devices(ctx context.Context, query string) ([]DeviceInfo, error)
	DeviceByMAC(ctx context.Context, mac string) (*DeviceInfo, error)
	DeviceByIP(ctx context.Context, ip string) (*DeviceInfo, error)
	Subnets(ctx context.Context) ([]SubnetTarget, error)
}

// RunContext is handed to Run, HandleChanges, HandleRunFinished and RunAction.
// Fields that do not apply are zero (e.g. RunID is 0 for hooks outside a run).
type RunContext struct {
	RunID       int64
	PluginID    string
	Trigger     string
	Settings    Settings
	Scope       Scope
	Targets     Targets
	Params      map[string]any
	Concurrency int

	Log       *slog.Logger
	Sink      Sink
	Events    EventEmitter
	Creds     CredentialProvider
	Inventory InventoryReader
	// DB gives processors direct access to their own tables. Scanners and importers
	// must write through Sink only.
	DB      *db.DB
	DataDir string // persistent directory owned by the plugin
	Env     Env

	OnProgress func(done, total int)
	// OnLive forwards live UI updates (set by the host; see Live).
	OnLive func(topic, typ string, data map[string]any)

	statsMu    sync.Mutex
	stats      map[string]any
	incomplete []netip.Prefix
	allUnsure  bool
}

// PresenceIncomplete tells the core that the given subnets were not scanned successfully
// in this run (without arguments: the whole run is unreliable), so devices there are not
// counted as missed by the presence evaluation.
func (rc *RunContext) PresenceIncomplete(prefixes ...netip.Prefix) {
	rc.statsMu.Lock()
	defer rc.statsMu.Unlock()
	if len(prefixes) == 0 {
		rc.allUnsure = true
		return
	}
	rc.incomplete = append(rc.incomplete, prefixes...)
}

// IncompletePresence returns what PresenceIncomplete recorded.
func (rc *RunContext) IncompletePresence() (all bool, prefixes []netip.Prefix) {
	rc.statsMu.Lock()
	defer rc.statsMu.Unlock()
	return rc.allUnsure, append([]netip.Prefix(nil), rc.incomplete...)
}

// Live sends a live update to connected browsers (SSE). Only topics the host allows are
// forwarded (currently "health"); without a host it is a no-op.
func (rc *RunContext) Live(topic, typ string, data map[string]any) {
	if rc.OnLive != nil {
		rc.OnLive(topic, typ, data)
	}
}

// Progress reports run progress (shown live in the UI).
func (rc *RunContext) Progress(done, total int) {
	if rc.OnProgress != nil {
		rc.OnProgress(done, total)
	}
}

// AddStat increments an integer statistic of the run.
func (rc *RunContext) AddStat(key string, n int) {
	rc.statsMu.Lock()
	defer rc.statsMu.Unlock()
	if rc.stats == nil {
		rc.stats = map[string]any{}
	}
	cur, _ := rc.stats[key].(int)
	rc.stats[key] = cur + n
}

// SetStat sets a statistic of the run.
func (rc *RunContext) SetStat(key string, v any) {
	rc.statsMu.Lock()
	defer rc.statsMu.Unlock()
	if rc.stats == nil {
		rc.stats = map[string]any{}
	}
	rc.stats[key] = v
}

// Stats returns a copy of the run statistics.
func (rc *RunContext) Stats() map[string]any {
	rc.statsMu.Lock()
	defer rc.statsMu.Unlock()
	out := make(map[string]any, len(rc.stats))
	for k, v := range rc.stats {
		out[k] = v
	}
	return out
}

// Parallelism returns the configured concurrency (at least 1).
func (rc *RunContext) Parallelism() int {
	if rc.Concurrency < 1 {
		return 1
	}
	return rc.Concurrency
}

// PublishContext is handed to Publisher.Publish.
type PublishContext struct {
	PluginID string
	Settings Settings
	Log      *slog.Logger
	Creds    CredentialProvider
	Env      Env
}

// RunSummary describes a finished run for RunFinishedHandler.
type RunSummary struct {
	RunID    int64
	PluginID string
	Kind     Kind
	Status   string
	Presence bool
	Targets  Targets
	Started  time.Time
	Finished time.Time
	Error    string
}

// ErrNoChanges may be returned by Run for a successful run without noteworthy results
// (e.g. a frequent health check round where nothing changed). The core then does not keep
// the run in the run history.
var ErrNoChanges = errors.New("keine Änderungen")

// ForEach calls fn for every item with at most n concurrent calls. It stops starting new
// work when ctx is cancelled and returns ctx.Err() in that case, otherwise the joined
// errors of all failed calls.
func ForEach[T any](ctx context.Context, n int, items []T, fn func(ctx context.Context, item T) error) error {
	if n < 1 {
		n = 1
	}
	sem := make(chan struct{}, n)
	var (
		wg   sync.WaitGroup
		mu   sync.Mutex
		errs []error
	)
	for _, it := range items {
		select {
		case <-ctx.Done():
			wg.Wait()
			return ctx.Err()
		case sem <- struct{}{}:
		}
		wg.Add(1)
		go func(it T) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := fn(ctx, it); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(it)
	}
	wg.Wait()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	return errors.Join(errs...)
}

// ---------------------------------------------------------------- registry

var (
	regMu    sync.RWMutex
	registry = map[string]Plugin{}
	idRe     = regexp.MustCompile(`^[a-z][a-z0-9_]{1,31}$`)
)

// Register adds a plugin to the global registry. It panics on invalid or duplicate ids,
// which is a programming error caught at startup.
func Register(p Plugin) {
	info := p.Info()
	if !idRe.MatchString(info.ID) {
		panic(fmt.Sprintf("plugin: invalid id %q", info.ID))
	}
	switch info.Kind {
	case KindScanner, KindImporter, KindProcessor, KindPublisher:
	default:
		panic(fmt.Sprintf("plugin %s: invalid kind %q", info.ID, info.Kind))
	}
	if err := p.Schema().Check(); err != nil {
		panic(fmt.Sprintf("plugin %s: invalid schema: %v", info.ID, err))
	}
	if info.DefaultSchedule != "" {
		if _, ok := p.(Runner); !ok {
			panic(fmt.Sprintf("plugin %s: schedule without Runner", info.ID))
		}
	}
	regMu.Lock()
	defer regMu.Unlock()
	if _, dup := registry[info.ID]; dup {
		panic(fmt.Sprintf("plugin: duplicate id %q", info.ID))
	}
	registry[info.ID] = p
}

// Get returns a registered plugin.
func Get(id string) (Plugin, bool) {
	regMu.RLock()
	defer regMu.RUnlock()
	p, ok := registry[id]
	return p, ok
}

// All returns all registered plugins sorted by kind and id.
func All() []Plugin {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]Plugin, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	order := map[Kind]int{}
	for i, k := range Kinds {
		order[k] = i
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i].Info(), out[j].Info()
		if order[a.Kind] != order[b.Kind] {
			return order[a.Kind] < order[b.Kind]
		}
		return a.ID < b.ID
	})
	return out
}
