// Package oui implements the "oui" scanner: it derives the manufacturer of devices from
// their MAC addresses using local vendor files (IEEE registries downloaded by the plugin
// action, with arp-scan's and nmap's vendor files as fallback). New MACs are looked up
// immediately through the change hook.
package oui

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// ErrNoData is returned when no vendor file is available.
var ErrNoData = errors.New("keine OUI-Datei vorhanden – bitte auf der Plugin-Seite „OUI-Datei aktualisieren“ ausführen")

// Plugin is the OUI vendor lookup.
type Plugin struct {
	// fallbacks overrides the arp-scan/nmap vendor files (tests).
	fallbacks []layer

	mu    sync.Mutex
	cache map[string]*cached // by data directory
}

type cached struct {
	sig string
	db  *database
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "oui",
		Kind:               plugin.KindScanner,
		Name:               "OUI-Hersteller",
		Description:        "Ermittelt den Hersteller eines Geräts aus seiner MAC-Adresse anhand einer lokalen OUI-Datei (IEEE, arp-scan, nmap).",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "0 */6 * * *",
		DefaultTimeout:     10 * time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     0,
		Targets:            plugin.TargetDevices,
		Presence:           false,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	urls := make([]any, len(defaultURLs))
	for i, u := range defaultURLs {
		urls[i] = u
	}
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "urls", Type: plugin.FieldStringList, Label: "Download-URLs", Default: urls, Group: "Aktualisierung",
			Description: "IEEE-Registerdateien im CSV-Format (MA-L, MA-M, MA-S), die „OUI-Datei aktualisieren“ lädt. Der Dateiname der URL bestimmt die lokale Datei.",
			Validation:  &plugin.Validation{Format: "url", Min: plugin.Int64(1)}},
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Download-Timeout", Default: "2m", Group: "Aktualisierung",
			Description: "Maximale Dauer eines einzelnen Downloads.",
			Validation:  &plugin.Validation{Min: plugin.Int64(5), Max: plugin.Int64(1800)}},
	}}
}

func (p *Plugin) layers(dataDir string) []layer {
	fb := p.fallbacks
	if fb == nil {
		fb = defaultFallbacks
	}
	return dataLayers(dataDir, fb)
}

// database returns the parsed vendor table, reloading it when a source file changed.
func (p *Plugin) database(dataDir string, log func(msg string, args ...any)) (*database, error) {
	layers := p.layers(dataDir)
	sig := signature(layers)
	p.mu.Lock()
	defer p.mu.Unlock()
	if c := p.cache[dataDir]; c != nil && c.sig == sig {
		return c.db, nil
	}
	db, err := loadDatabase(layers)
	if err != nil {
		if db.empty() {
			return nil, err
		}
		log("Einige OUI-Dateien konnten nicht gelesen werden", "error", err)
	}
	if db.empty() {
		return nil, ErrNoData
	}
	if p.cache == nil {
		p.cache = map[string]*cached{}
	}
	p.cache[dataDir] = &cached{sig: sig, db: db}
	return db, nil
}

// candidateMACs returns the device MACs worth looking up: primary first, no random or
// multicast addresses.
func candidateMACs(d plugin.DeviceInfo) []string {
	var out []string
	seen := map[string]bool{}
	for _, raw := range append([]string{d.PrimaryMAC}, d.MACs...) {
		mac, ok := netutil.NormalizeMAC(raw)
		if !ok || seen[mac] || netutil.IsRandomizedMAC(mac) || netutil.IsMulticastMAC(mac) {
			continue
		}
		seen[mac] = true
		out = append(out, mac)
	}
	return out
}

// macVendor is one entry of the inventory written per device.
type macVendor struct {
	MAC    string `json:"mac"`
	Vendor string `json:"vendor"`
	Source string `json:"source"`
}

// deviceObservation looks up all MACs of a device. The vendor of the first MAC with a
// known vendor (primary MAC preferred) becomes the device vendor; the core keeps one
// vendor fact per source, so one observation per device avoids flapping facts on
// multi-NIC hosts. All MAC vendors are listed in the inventory.
func deviceObservation(db *database, d plugin.DeviceInfo) *plugin.Observation {
	var (
		inv    []macVendor
		chosen string
		vendor string
	)
	for _, mac := range candidateMACs(d) {
		v, src := db.lookup(mac)
		if v == "" {
			continue
		}
		inv = append(inv, macVendor{MAC: mac, Vendor: v, Source: src})
		if vendor == "" {
			chosen, vendor = mac, v
		}
	}
	if vendor == "" {
		return nil
	}
	return &plugin.Observation{
		DeviceID:  d.ID,
		MACs:      []string{chosen},
		Vendor:    vendor,
		Target:    chosen,
		Inventory: map[string]any{"macs": inv},
	}
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	db, err := p.database(rc.DataDir, rc.Log.Warn)
	if err != nil {
		return err
	}
	rc.SetStat("entries", db.entries())
	devices := rc.Targets.Devices
	rc.Progress(0, len(devices))
	for i, d := range devices {
		if err := ctx.Err(); err != nil {
			return err
		}
		obs := deviceObservation(db, d)
		if obs == nil {
			if len(candidateMACs(d)) > 0 {
				rc.AddStat("unknown", 1)
			}
		} else if _, err := rc.Sink.Observe(ctx, obs); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			rc.Log.Warn("Ergebnis konnte nicht gespeichert werden", "geraet", d.ID, "error", err)
		} else {
			rc.AddStat("hosts", 1)
		}
		rc.Progress(i+1, len(devices))
	}
	return nil
}

// HandleChanges implements plugin.ChangeHandler: new MACs and new devices get their
// vendor right away instead of at the next scheduled run.
func (p *Plugin) HandleChanges(ctx context.Context, rc *plugin.RunContext, changes []plugin.Change) error {
	var todo []plugin.Change
	for _, c := range changes {
		if c.Type == plugin.ChangeMACAdded || c.Type == plugin.ChangeDeviceCreated {
			todo = append(todo, c)
		}
	}
	if len(todo) == 0 {
		return nil
	}
	db, err := p.database(rc.DataDir, rc.Log.Warn)
	if err != nil {
		rc.Log.Debug("OUI-Nachschlagen übersprungen", "error", err)
		return nil
	}
	done := map[int64]bool{}
	for _, c := range todo {
		if c.DeviceID == 0 || done[c.DeviceID] {
			continue
		}
		done[c.DeviceID] = true
		obs := p.changeObservation(ctx, rc, db, c)
		if obs == nil {
			continue
		}
		if _, err := rc.Sink.Observe(ctx, obs); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			rc.Log.Warn("Hersteller konnte nicht gespeichert werden", "geraet", c.DeviceID, "error", err)
		}
	}
	return nil
}

// changeObservation looks up the vendor of a changed device like a scheduled run does
// (all MACs, primary preferred). Without inventory access only the changed MAC is used.
func (p *Plugin) changeObservation(ctx context.Context, rc *plugin.RunContext, db *database, c plugin.Change) *plugin.Observation {
	if rc.Inventory != nil {
		if d, err := rc.Inventory.Device(ctx, c.DeviceID); err == nil && d != nil {
			return deviceObservation(db, *d)
		}
	}
	mac, ok := netutil.NormalizeMAC(changedMAC(c))
	if !ok || netutil.IsRandomizedMAC(mac) || netutil.IsMulticastMAC(mac) {
		return nil
	}
	vendor, _ := db.lookup(mac)
	if vendor == "" {
		return nil
	}
	return &plugin.Observation{DeviceID: c.DeviceID, MACs: []string{mac}, Vendor: vendor, Target: mac}
}

// changedMAC extracts the MAC address of a mac.added or device.created change.
func changedMAC(c plugin.Change) string {
	switch v := c.New.(type) {
	case string:
		return v
	case *plugin.DeviceSnapshot:
		if v != nil {
			return v.MAC
		}
	case plugin.DeviceSnapshot:
		return v.MAC
	case map[string]any:
		s, _ := v["mac"].(string)
		return s
	}
	if c.Type == plugin.ChangeMACAdded {
		return c.Key
	}
	return ""
}

// Actions implements plugin.ActionProvider.
func (p *Plugin) Actions() []plugin.Action {
	return []plugin.Action{{
		Name:        "update",
		Label:       "OUI-Datei aktualisieren",
		Description: "Lädt die aktuellen Herstellerlisten der IEEE (MA-L, MA-M, MA-S) herunter und ersetzt die lokale OUI-Datei.",
		Scope:       plugin.ActionPlugin,
	}}
}

// RunAction implements plugin.ActionProvider.
func (p *Plugin) RunAction(ctx context.Context, rc *plugin.RunContext, name string, params map[string]any) (*plugin.ActionResult, error) {
	if name != "update" {
		return nil, fmt.Errorf("unbekannte Aktion %q", name)
	}
	timeout := rc.Settings.Duration("timeout")
	if timeout <= 0 {
		timeout = 2 * time.Minute
	}
	urls := rc.Settings.StringList("urls")
	if len(urls) == 0 {
		urls = defaultURLs
	}
	client := &http.Client{Timeout: timeout, Transport: http.DefaultTransport.(*http.Transport).Clone()}
	counts, err := download(ctx, client, urls, rc.DataDir, minEntries)
	if err != nil {
		return nil, fmt.Errorf("OUI-Aktualisierung fehlgeschlagen: %w", err)
	}
	db, err := p.database(rc.DataDir, rc.Log.Warn)
	if err != nil {
		return nil, err
	}
	names := make([]string, 0, len(counts))
	total := 0
	for n, c := range counts {
		names = append(names, n)
		total += c
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, n := range names {
		parts[i] = fmt.Sprintf("%s: %s", n, thousands(counts[n]))
	}
	rc.Log.Info("OUI-Dateien aktualisiert", "eintraege", total)
	return &plugin.ActionResult{
		Message: fmt.Sprintf("OUI-Datei aktualisiert: %s Einträge (%s)", thousands(total), strings.Join(parts, ", ")),
		Data:    map[string]any{"files": counts, "entries": total, "loaded": db.entries()},
	}, nil
}

// thousands formats n with German thousands separators.
func thousands(n int) string {
	s := fmt.Sprint(n)
	for i := len(s) - 3; i > 0; i -= 3 {
		s = s[:i] + "." + s[i:]
	}
	return s
}
