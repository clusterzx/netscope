// Package cve is the processor that mirrors the NVD CVE database locally and matches the
// software of every device against it.
//
// Sync: the NVD CVE JSON 2.0 data feeds (one per year plus "modified") are downloaded
// when their .meta checksum changes, verified and streamed into nvd_cves and
// nvd_cpe_matches (never loaded into memory as a whole). After the initial load only
// the "modified" feed is fetched, unless the last sync is older than 7 days.
//
// Matching: device-side CPEs come from nmap service detection (ports.cpes), the
// effective OS fact, detected web applications and — via a curated mapping table —
// installed packages of SSH hosts. They are compared with the vulnerable CPE criteria
// of the NVD (exact versions and version ranges). Matching is heuristic by nature; see
// Disclaimer.
//
// Events: cve.new is raised for new matches at or above the configured CVSS score, but
// never for the first evaluation of a device or for the first data of a kind on a
// device (initial import), and only if the CVE was published/changed by the NVD since
// the last sync or the device's software changed since its last evaluation. cve.resolved
// is raised when the software behind a reported CVE was updated or removed.
package cve

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"netscope/internal/plugin"
)

func init() { plugin.Register(New()) }

// DefaultFeedURL is the base URL of the official NVD CVE JSON 2.0 feeds.
const DefaultFeedURL = "https://nvd.nist.gov/feeds/json/cve/2.0"

// Plugin is the cve processor.
type Plugin struct {
	now        func() time.Time
	client     *http.Client // nil: a client with the configured timeout
	attempts   int
	retryDelay time.Duration
	// flushAge: queued devices are matched without waiting for the end of the run that
	// changed them once their oldest change is this old.
	flushAge time.Duration

	syncMu  sync.Mutex
	matchMu sync.Mutex

	mu        sync.Mutex
	pending   map[int64]time.Time // device -> first queued change
	evaluated map[int64]int64     // device -> last evaluation (ms), survives until restart
}

// New creates the plugin.
func New() *Plugin {
	return &Plugin{now: time.Now, attempts: 4, retryDelay: 15 * time.Second, flushAge: 2 * time.Minute,
		pending: map[int64]time.Time{}, evaluated: map[int64]int64{}}
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "cve",
		Kind: plugin.KindProcessor,
		Name: "CVE-Abgleich",
		Description: "Spiegelt die NVD-Schwachstellendatenbank lokal und gleicht Dienst-, Web-, Betriebssystem- und " +
			"Paketversionen der Geräte heuristisch gegen bekannte CVEs ab.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "30 4 * * *",
		DefaultTimeout:     2 * time.Hour,
		DefaultConcurrency: 4,
		DefaultRetries:     2,
		Targets:            plugin.TargetNone,
	}
}

// minScoreOptions are the selectable event thresholds.
var minScoreOptions = []plugin.Option{
	{Value: "0", Label: "Alle Schwachstellen"},
	{Value: "4.0", Label: "ab 4.0 (mittel)"},
	{Value: "5.0", Label: "ab 5.0"},
	{Value: "6.0", Label: "ab 6.0"},
	{Value: "7.0", Label: "ab 7.0 (hoch)"},
	{Value: "8.0", Label: "ab 8.0"},
	{Value: "9.0", Label: "ab 9.0 (kritisch)"},
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	const (
		gMirror = "NVD-Spiegel"
		gMatch  = "Abgleich"
		gEvents = "Events"
	)
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "feed_base_url", Type: plugin.FieldString, Label: "Feed-URL", Group: gMirror, Required: true, Default: DefaultFeedURL,
			Description: "Basis-URL der NVD-Feeds im Format CVE JSON 2.0 (nvdcve-2.0-<Jahr>.json.gz mit .meta-Datei). Nur für eigene Spiegel ändern.",
			Validation:  &plugin.Validation{Format: "url"}},
		{Key: "start_year", Type: plugin.FieldInt, Label: "CVEs ab Jahrgang", Group: gMirror, Default: firstFeedYear,
			Description: "Ältester gespiegelter CVE-Jahrgang (2002 schließt 1999–2001 ein). Spätere Jahrgänge sparen Speicher und Downloadzeit, übersehen aber alte Lücken in alter Software.",
			Validation:  &plugin.Validation{Min: plugin.Int64(firstFeedYear), Max: plugin.Int64(2100)}},
		{Key: "http_timeout", Type: plugin.FieldDuration, Label: "Timeout pro Download", Group: gMirror, Default: "5m", Advanced: true,
			Description: "Maximale Dauer eines einzelnen Downloads (die großen Jahres-Feeds sind rund 30 MB groß).",
			Validation:  &plugin.Validation{Min: plugin.Int64(10), Max: plugin.Int64(3600)}},
		{Key: "keep_downloads", Type: plugin.FieldBool, Label: "Downloads behalten", Group: gMirror, Default: false, Advanced: true,
			Description: "Heruntergeladene Feed-Dateien im Plugin-Verzeichnis aufbewahren statt sie nach dem Import zu löschen."},
		{Key: "include_packages", Type: plugin.FieldBool, Label: "Installierte Pakete abgleichen", Group: gMatch, Default: true,
			Description: "Paketlisten von SSH-Hosts (dpkg, rpm, apk) über eine kuratierte Zuordnung abgleichen. Distributionen spielen Sicherheitskorrekturen zurück, ohne die Version zu erhöhen – diese Treffer sind immer heuristisch."},
		{Key: "include_os", Type: plugin.FieldBool, Label: "Betriebssystem abgleichen", Group: gMatch, Default: true,
			Description: "CPEs des wirksamen Betriebssystems abgleichen (z. B. Firmware aus der nmap-OS-Erkennung). Distributionen selbst werden nicht abgeglichen, dafür die Pakete."},
		{Key: "include_os_guesses", Type: plugin.FieldBool, Label: "Auch unsichere OS-Vermutungen abgleichen", Group: gMatch, Default: false, Advanced: true,
			VisibleIf:   &plugin.Condition{Field: "include_os", Equals: []any{true}},
			Description: "Die nmap-Fingerabdruck-Erkennung nennt oft mehrere Kandidaten („Android 9 - 10“) oder nur ein allgemeines Betriebssystem ohne Patchstand (iOS, Android, Windows, macOS, BSD). Solche Vermutungen werden standardmäßig nicht abgeglichen, weil sonst jede CVE der Hauptversion gemeldet würde. Der Linux-Kernel aus einem Fingerabdruck wird nie abgeglichen."},
		{Key: "include_http_apps", Type: plugin.FieldBool, Label: "Erkannte Web-Anwendungen abgleichen", Group: gMatch, Default: true,
			Description: "Vom HTTP-Scanner erkannte Anwendungen mit Version (z. B. Grafana) abgleichen."},
		{Key: "min_event_score", Type: plugin.FieldEnum, Label: "Events ab CVSS", Group: gEvents, Default: "7.0", Options: minScoreOptions,
			Description: "Für neue bzw. behobene CVEs unterhalb dieses CVSS-Basiswerts werden keine Events erzeugt; sie erscheinen trotzdem in der Liste."},
		{Key: "heuristic_events", Type: plugin.FieldBool, Label: "Events auch für heuristische Treffer", Group: gEvents, Default: true,
			Description: "Auch für Treffer aus Paketversionen, Distributions-Bannern und OS-Vermutungen Events erzeugen. Ausschalten, wenn Backports zu vielen Fehlalarmen führen."},
	}}
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	var errs []plugin.FieldError
	if y := s.Int("start_year"); y > time.Now().Year() {
		errs = append(errs, plugin.FieldError{Field: "start_year", Message: fmt.Sprintf("darf nicht nach %d liegen", time.Now().Year())})
	}
	if u := s.String("feed_base_url"); strings.HasSuffix(u, ".json.gz") || strings.HasSuffix(u, ".meta") {
		errs = append(errs, plugin.FieldError{Field: "feed_base_url", Message: "Basis-URL ohne Dateinamen angeben"})
	}
	if len(errs) > 0 {
		return &plugin.ValidationError{Errors: errs}
	}
	return nil
}

// config is the typed view of the settings.
type config struct {
	baseURL         string
	startYear       int
	httpTimeout     time.Duration
	keepDownloads   bool
	includePackages bool
	includeOS       bool
	includeHTTP     bool
	// includeOSGuesses also matches ambiguous or generic fingerprint OS guesses.
	includeOSGuesses bool
	minScore         float64
	heuristicEvents  bool
}

func loadConfig(s plugin.Settings) config {
	c := config{
		baseURL:          strings.TrimRight(s.String("feed_base_url"), "/"),
		startYear:        s.Int("start_year"),
		httpTimeout:      s.Duration("http_timeout"),
		keepDownloads:    s.Bool("keep_downloads"),
		includePackages:  s.Bool("include_packages"),
		includeOS:        s.Bool("include_os"),
		includeOSGuesses: s.Bool("include_os_guesses"),
		includeHTTP:      s.Bool("include_http_apps"),
		heuristicEvents:  s.Bool("heuristic_events"),
	}
	if c.baseURL == "" {
		c.baseURL = DefaultFeedURL
	}
	if c.startYear < firstFeedYear {
		c.startYear = firstFeedYear
	}
	if c.httpTimeout <= 0 {
		c.httpTimeout = 5 * time.Minute
	}
	c.minScore, _ = strconv.ParseFloat(s.String("min_event_score"), 64)
	return c
}

func (p *Plugin) setup() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.now == nil {
		p.now = time.Now
	}
	if p.pending == nil {
		p.pending = map[int64]time.Time{}
	}
	if p.evaluated == nil {
		p.evaluated = map[int64]int64{}
	}
	if p.attempts == 0 {
		p.attempts = 4
	}
	if p.retryDelay == 0 {
		p.retryDelay = 15 * time.Second
	}
	if p.flushAge == 0 {
		p.flushAge = 2 * time.Minute
	}
}

// Run implements plugin.Runner: sync the mirror, then match all devices.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	_, _, err := p.syncAndMatch(ctx, rc, false)
	return err
}

func (p *Plugin) syncAndMatch(ctx context.Context, rc *plugin.RunContext, force bool) (*syncStats, *matchStats, error) {
	p.setup()
	if rc.DB == nil {
		return nil, nil, errors.New("keine Datenbank verfügbar")
	}
	cfg := loadConfig(rc.Settings)
	st, syncErr := p.sync(ctx, rc, cfg, force)
	if ctx.Err() != nil {
		return st, nil, ctx.Err()
	}
	var changed map[string]struct{}
	if st != nil {
		changed = st.Changed
	}
	ms, matchErr := p.fullMatch(ctx, rc, cfg, changed)
	if syncErr != nil {
		syncErr = fmt.Errorf("NVD-Synchronisation: %w", syncErr)
	}
	return st, ms, errors.Join(syncErr, matchErr)
}

// fullMatch matches all devices (skipped while the mirror is empty).
func (p *Plugin) fullMatch(ctx context.Context, rc *plugin.RunContext, cfg config, changed map[string]struct{}) (*matchStats, error) {
	empty, err := mirrorEmpty(ctx, rc.DB)
	if err != nil {
		return nil, err
	}
	if empty {
		rc.Log.Warn("Lokale NVD-Kopie ist leer – Abgleich übersprungen")
		return nil, errors.New("die lokale NVD-Kopie ist leer; zuerst synchronisieren")
	}
	p.clearPending(p.now())
	start := time.Now()
	ms, err := p.match(ctx, rc, cfg, nil, changed)
	if ms != nil {
		rc.SetStat("devices", ms.Devices)
		rc.SetStat("device_cpes", ms.CPEs)
		rc.SetStat("device_cves_active", ms.Active)
		rc.SetStat("device_cves_new", ms.New)
		rc.SetStat("device_cves_resolved", ms.Resolved)
		rc.SetStat("events", ms.Events)
		rc.Log.Info("CVE-Abgleich abgeschlossen", "geraete", ms.Devices, "cpes", ms.CPEs, "aktiv", ms.Active,
			"neu", ms.New, "behoben", ms.Resolved, "events", ms.Events, "dauer", time.Since(start).Round(time.Millisecond).String())
	}
	if err != nil {
		return ms, fmt.Errorf("CVE-Abgleich: %w", err)
	}
	return ms, nil
}

// ---------------------------------------------------------------- actions

// Actions implements plugin.ActionProvider.
func (p *Plugin) Actions() []plugin.Action {
	return []plugin.Action{
		{Name: "sync", Label: "NVD jetzt synchronisieren", Scope: plugin.ActionPlugin,
			Description: "Lädt geänderte NVD-Feeds herunter und gleicht danach alle Geräte ab.",
			Params: []plugin.Field{{Key: "full", Type: plugin.FieldBool, Label: "Alle Feeds neu laden", Default: false,
				Description: "Alle Jahres-Feeds unabhängig von Prüfsummen neu herunterladen und importieren (dauert mehrere Minuten)."}}},
		{Name: "match", Label: "Abgleich jetzt ausführen", Scope: plugin.ActionPlugin,
			Description: "Gleicht alle Geräte gegen die lokale NVD-Kopie ab, ohne etwas herunterzuladen."},
	}
}

// RunAction implements plugin.ActionProvider.
func (p *Plugin) RunAction(ctx context.Context, rc *plugin.RunContext, name string, params map[string]any) (*plugin.ActionResult, error) {
	p.setup()
	if rc.DB == nil {
		return nil, errors.New("keine Datenbank verfügbar")
	}
	switch name {
	case "sync":
		st, ms, err := p.syncAndMatch(ctx, rc, paramBool(params, "full"))
		res := &plugin.ActionResult{Message: summary(st, ms), Data: actionData(st, ms)}
		if err != nil {
			return res, err
		}
		return res, nil
	case "match":
		ms, err := p.fullMatch(ctx, rc, loadConfig(rc.Settings), nil)
		if err != nil {
			return nil, err
		}
		return &plugin.ActionResult{Message: summary(nil, ms), Data: actionData(nil, ms)}, nil
	}
	return nil, fmt.Errorf("unbekannte Aktion %q", name)
}

func paramBool(params map[string]any, key string) bool {
	switch v := params[key].(type) {
	case bool:
		return v
	case string:
		b, _ := strconv.ParseBool(v)
		return b
	}
	return false
}

func summary(st *syncStats, ms *matchStats) string {
	var parts []string
	if st != nil {
		switch {
		case len(st.Failed) > 0:
			parts = append(parts, fmt.Sprintf("NVD-Sync mit Fehlern (%s)", strings.Join(st.Failed, ", ")))
		case st.Downloaded == 0:
			parts = append(parts, "NVD-Kopie ist aktuell")
		default:
			parts = append(parts, fmt.Sprintf("NVD: %d Feed(s) geladen, %d CVEs aktualisiert", st.Downloaded, st.Written))
		}
	}
	if ms != nil {
		parts = append(parts, fmt.Sprintf("Abgleich: %d Geräte, %d aktive Treffer, %d neu, %d behoben", ms.Devices, ms.Active, ms.New, ms.Resolved))
	}
	return strings.Join(parts, " · ")
}

func actionData(st *syncStats, ms *matchStats) map[string]any {
	out := map[string]any{}
	if st != nil {
		out["syncMode"] = st.Mode
		out["feedsChecked"] = st.Checked
		out["feedsDownloaded"] = st.Downloaded
		out["cvesWritten"] = st.Written
		out["feedsFailed"] = st.Failed
	}
	if ms != nil {
		out["devices"] = ms.Devices
		out["active"] = ms.Active
		out["new"] = ms.New
		out["resolved"] = ms.Resolved
		out["events"] = ms.Events
	}
	return out
}

// ---------------------------------------------------------------- change handling

// relevantChange reports whether a change can alter the CPEs of a device.
func relevantChange(c plugin.Change, cfg config) bool {
	switch c.Type {
	case plugin.ChangeDeviceCreated, plugin.ChangePortOpened, plugin.ChangePortChanged, plugin.ChangePortClosed:
		return true
	case plugin.ChangePackages:
		return cfg.includePackages
	case plugin.ChangeOS:
		return cfg.includeOS
	}
	return false
}

// HandleChanges implements plugin.ChangeHandler. Affected devices are queued and matched
// in one batch when the run that produced the changes has finished (HandleRunFinished),
// when a queued change is older than flushAge, or at once for changes outside a run.
func (p *Plugin) HandleChanges(ctx context.Context, rc *plugin.RunContext, changes []plugin.Change) error {
	p.setup()
	cfg := loadConfig(rc.Settings)
	now := p.now()
	immediate := false
	p.mu.Lock()
	for _, c := range changes {
		if c.DeviceID <= 0 || !relevantChange(c, cfg) {
			continue
		}
		if _, ok := p.pending[c.DeviceID]; !ok {
			p.pending[c.DeviceID] = now
		}
		if c.RunID == 0 {
			immediate = true
		}
	}
	for _, t := range p.pending {
		if now.Sub(t) >= p.flushAge {
			immediate = true
			break
		}
	}
	p.mu.Unlock()
	if !immediate {
		return nil
	}
	return p.flush(ctx, rc)
}

// HandleRunFinished implements plugin.RunFinishedHandler: devices whose web
// applications or OS details were refreshed by the run (which the core reports without
// a change) are queued, then all queued devices are matched.
func (p *Plugin) HandleRunFinished(ctx context.Context, rc *plugin.RunContext, run plugin.RunSummary) error {
	p.setup()
	if rc.DB == nil {
		return nil
	}
	if run.RunID > 0 && (run.Kind == plugin.KindScanner || run.Kind == plugin.KindImporter) {
		ids, err := devicesTouchedByRun(ctx, rc, run)
		if err != nil {
			return err
		}
		now := p.now()
		p.mu.Lock()
		for _, id := range ids {
			if _, ok := p.pending[id]; !ok {
				p.pending[id] = now
			}
		}
		p.mu.Unlock()
	}
	return p.flush(ctx, rc)
}

// devicesTouchedByRun returns devices whose HTTP services or OS facts a run wrote.
func devicesTouchedByRun(ctx context.Context, rc *plugin.RunContext, run plugin.RunSummary) ([]int64, error) {
	cfg := loadConfig(rc.Settings)
	var ids []int64
	seen := map[int64]bool{}
	add := func(q string, args ...any) error {
		return queryEach(ctx, rc.DB.R, q, args, func(r *sql.Rows) error {
			var id int64
			if err := r.Scan(&id); err != nil {
				return err
			}
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
			return nil
		})
	}
	started := run.Started.UnixMilli()
	if cfg.includeHTTP {
		if err := add(`SELECT DISTINCT device_id FROM http_services WHERE run_id = ? OR (gone_at IS NOT NULL AND gone_at >= ?)`, run.RunID, started); err != nil {
			return nil, err
		}
	}
	if cfg.includeOS {
		if err := add(`SELECT DISTINCT device_id FROM device_facts WHERE kind = 'os' AND run_id = ?`, run.RunID); err != nil {
			return nil, err
		}
	}
	return ids, nil
}

// flush matches all queued devices.
func (p *Plugin) flush(ctx context.Context, rc *plugin.RunContext) error {
	p.mu.Lock()
	if len(p.pending) == 0 {
		p.mu.Unlock()
		return nil
	}
	queued := p.pending
	p.pending = map[int64]time.Time{}
	p.mu.Unlock()
	requeue := func() {
		p.mu.Lock()
		for id, t := range queued {
			if cur, ok := p.pending[id]; !ok || t.Before(cur) {
				p.pending[id] = t
			}
		}
		p.mu.Unlock()
	}
	if rc.DB == nil {
		requeue()
		return errors.New("keine Datenbank verfügbar")
	}
	empty, err := mirrorEmpty(ctx, rc.DB)
	if err != nil {
		requeue()
		return err
	}
	if empty {
		return nil // nothing to match against; the first sync matches everything
	}
	ids := make([]int64, 0, len(queued))
	for id := range queued {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	ms, err := p.match(ctx, rc, loadConfig(rc.Settings), ids, nil)
	if err != nil {
		requeue()
		return fmt.Errorf("CVE-Abgleich für %d Geräte: %w", len(ids), err)
	}
	rc.Log.Debug("CVE-Abgleich nach Änderungen", "geraete", ms.Devices, "neu", ms.New, "behoben", ms.Resolved, "events", ms.Events)
	return nil
}

// clearPending drops queued devices that a full match started at t covers.
func (p *Plugin) clearPending(t time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for id, at := range p.pending {
		if !at.After(t) {
			delete(p.pending, id)
		}
	}
}
