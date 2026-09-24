// Package topology is the processor that builds the topology graph: devices are attached
// to switch ports (bridge FDB), network devices are linked by LLDP, and devices without a
// layer-2 upstream hang below the gateway of their subnet. It owns the relations with
// source "topology" and never touches edges of other sources (manual, proxmox …).
package topology

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// defaultDebounce is the delay between a finished input run and the rebuild; runs that
// finish within the delay are covered by one rebuild.
const defaultDebounce = 15 * time.Second

// triggers are the plugins whose successful runs change the topology inputs.
var triggers = map[string]bool{"snmp": true, "proxmox": true, "arpscan": true}

// Plugin implements the topology processor.
type Plugin struct {
	mu        sync.Mutex   // serialises rebuilds
	gen       atomic.Int64 // debounce generation
	lastBuild atomic.Int64 // unix ms of the start of the last successful rebuild
	debounce  time.Duration
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "topology",
		Kind: plugin.KindProcessor,
		Name: "Topologie",
		Description: "Leitet aus SNMP-FDB, LLDP-Nachbarn und Subnetz-Gateways die Verbindungen zwischen Geräten ab " +
			"(Gerät → Switch-Port, LLDP-Nachbarn, Layer 3 zum Gateway).",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "*/15 * * * *",
		DefaultTimeout:     5 * time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     1,
		Targets:            plugin.TargetNone,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "uplink_mac_threshold", Type: plugin.FieldInt, Label: "Uplink ab MAC-Anzahl", Default: 4,
			Description: "Ein Switch-Port mit mehr MAC-Adressen gilt als Uplink (dahinter hängt ein weiterer Switch oder AP); " +
				"Geräte auf Uplinks werden nicht diesem Port zugeordnet. Ports mit LLDP-Nachbar sind immer Uplinks.",
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(10000)}},
		{Key: "attach_to_gateway", Type: plugin.FieldBool, Label: "Geräte ohne Switch-Port ans Gateway hängen", Default: true,
			Description: "Geräte ohne ermittelte Layer-2-Verbindung werden als Layer-3-Kante unter das Gateway ihres Subnetzes gehängt."},
		{Key: "exclude_tags", Type: plugin.FieldStringList, Label: "Geräte mit diesen Tags ausschließen",
			Description: "Ein Tag pro Zeile. Solche Geräte erhalten keine automatisch ermittelten Verbindungen."},
		{Key: "edge_retention", Type: plugin.FieldDuration, Label: "Haltezeit verschwundener Verbindungen", Default: "24h", Advanced: true,
			Description: "Switch-Port- und LLDP-Verbindungen bleiben so lange bestehen, wenn ein Gerät (z. B. im Standby) nicht mehr " +
				"in der MAC-Tabelle steht und nirgends sonst gesehen wird. 0 = sofort entfernen.",
			Validation: &plugin.Validation{Min: plugin.Int64(0), Max: plugin.Int64(30 * 24 * 3600)}},
	}}
}

func loadOptions(s plugin.Settings, now time.Time) options {
	o := options{
		uplinkThreshold: s.Int("uplink_mac_threshold"),
		attachGateway:   s.Bool("attach_to_gateway"),
		excludeTags:     map[string]bool{},
		retention:       s.Duration("edge_retention"),
		now:             now,
	}
	if o.uplinkThreshold < 1 {
		o.uplinkThreshold = 4
	}
	for _, t := range s.StringList("exclude_tags") {
		if t = normalizeTag(t); t != "" {
			o.excludeTags[t] = true
		}
	}
	return o
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	return p.rebuild(ctx, rc)
}

// HandleRunFinished implements plugin.RunFinishedHandler: a successful snmp, proxmox or
// arpscan run triggers a rebuild after a short debounce delay. Several runs finishing
// within the delay lead to a single rebuild; runs already covered by a rebuild that
// started after they finished are ignored.
func (p *Plugin) HandleRunFinished(ctx context.Context, rc *plugin.RunContext, run plugin.RunSummary) error {
	if !triggers[run.PluginID] || run.Status != "success" {
		return nil
	}
	finished := run.Finished
	if finished.IsZero() {
		finished = time.Now()
	}
	if p.lastBuild.Load() >= finished.UnixMilli() {
		return nil
	}
	gen := p.gen.Add(1)
	delay := p.debounce
	if delay <= 0 {
		delay = defaultDebounce
	}
	t := time.NewTimer(delay)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return nil
	case <-t.C:
	}
	if p.gen.Load() != gen || p.lastBuild.Load() >= finished.UnixMilli() {
		return nil // a later trigger or a rebuild in the meantime covers this run
	}
	return p.rebuild(ctx, rc)
}

// rebuild derives and stores the topology.
func (p *Plugin) rebuild(ctx context.Context, rc *plugin.RunContext) error {
	if rc.DB == nil {
		return errors.New("keine Datenbank verfügbar")
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	start := time.Now()
	opt := loadOptions(rc.Settings, start)
	in, err := loadInput(ctx, rc.DB)
	if err != nil {
		return fmt.Errorf("Topologie-Daten lesen: %w", err)
	}
	res := result{keep: map[int64]bool{}}
	for _, part := range in.split() {
		r := derive(part, opt)
		res.edges = append(res.edges, r.edges...)
		for id := range r.keep {
			res.keep[id] = true
		}
	}
	st, err := writeEdges(ctx, rc.DB, in, res, start)
	if err != nil {
		return fmt.Errorf("Topologie speichern: %w", err)
	}
	p.lastBuild.Store(start.UnixMilli())
	counts := map[string]int{}
	for _, e := range res.edges {
		counts[e.kind]++
	}
	took := time.Since(start)
	rc.SetStat("switch_port", counts[plugin.RelSwitchPort])
	rc.SetStat("lldp", counts[plugin.RelLLDP])
	rc.SetStat("l3", counts[plugin.RelL3])
	rc.SetStat("created", st.created)
	rc.SetStat("removed", st.removed)
	rc.SetStat("kept", st.kept)
	rc.SetStat("duration_ms", took.Milliseconds())
	rc.Log.Info("Topologie aktualisiert", "switch_port", counts[plugin.RelSwitchPort], "lldp", counts[plugin.RelLLDP],
		"l3", counts[plugin.RelL3], "neu", st.created, "entfernt", st.removed, "gehalten", st.kept, "dauer_ms", took.Milliseconds())
	return nil
}
