// Package agents is the "agent" plugin: the settings of the NetScope agents (intervals,
// what they collect, disk threshold). Agents deliver on their own schedule to the agent
// service (internal/agent); a manual run asks every connected agent for a fresh inventory.
package agents

import (
	"context"
	"errors"
	"time"

	"netscope/internal/plugin"
)

// ID of the plugin; observations of agents carry it as their source.
const ID = "agent"

func init() { plugin.Register(&Plugin{}) }

// RefreshAll is set by the agent service: it asks every agent for an inventory and returns
// how many were asked.
var RefreshAll func(ctx context.Context) (int, error)

// Plugin holds the agent settings.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   ID,
		Kind: plugin.KindImporter,
		Name: "NetScope-Agent",
		Description: "Systeme mit installiertem NetScope-Agent liefern Inventar (wie per SSH) und Auslastung " +
			"(CPU, RAM, Platten, Netz) von sich aus – ohne SSH-Zugang, auch hinter NAT. Installation unter Agents.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultTimeout:     time.Minute,
		DefaultConcurrency: 1,
	}
}

const (
	gCollect = "Erfassung"
	gAlerts  = "Warnungen"
)

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "inventory_interval", Type: plugin.FieldDuration, Label: "Inventar alle", Default: "1h", Group: gCollect,
			Description: "Wie oft jeder Agent das volle Inventar liefert (Pakete, Dienste, Ports, Docker). „Jetzt ausführen“ fordert es sofort an."},
		{Key: "sample_interval", Type: plugin.FieldDuration, Label: "Messung alle", Default: "1m", Group: gCollect,
			Description: "Abstand der Auslastungsmessungen (CPU, RAM, Platten, Netz)."},
		{Key: "report_interval", Type: plugin.FieldDuration, Label: "Messwerte senden alle", Default: "5m", Group: gCollect,
			Description: "Die Messungen werden gesammelt übertragen; so oft meldet sich der Agent mindestens."},
		{Key: "collect_packages", Type: plugin.FieldBool, Label: "Installierte Pakete erfassen", Default: true, Group: gCollect,
			Description: "Grundlage für den CVE-Abgleich."},
		{Key: "collect_docker", Type: plugin.FieldBool, Label: "Docker-Container erfassen", Default: true, Group: gCollect,
			Description: "Nur wenn der Agent mit --docker installiert wurde (Zugriff auf den Docker-Socket)."},
		{Key: "disk_threshold", Type: plugin.FieldInt, Label: "Dateisystem voll ab (%)", Default: 90, Group: gAlerts,
			Description: "Belegung, ab der ein Event „Dateisystem fast voll“ entsteht (0 = aus).",
			Validation:  &plugin.Validation{Min: plugin.Int64(0), Max: plugin.Int64(100)}},
		{Key: "offline_after", Type: plugin.FieldDuration, Label: "Agent gilt als weg nach", Default: "5m", Group: gAlerts,
			Description: "Ohne Meldung so lange entsteht „Agent meldet sich nicht“; scannt kein anderer Scanner das Gerät, geht es offline."},
	}}
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	switch {
	case s.Duration("inventory_interval") < 5*time.Minute:
		return plugin.FieldErr("inventory_interval", "mindestens 5 Minuten")
	case s.Duration("sample_interval") < 10*time.Second:
		return plugin.FieldErr("sample_interval", "mindestens 10 Sekunden")
	case s.Duration("report_interval") < s.Duration("sample_interval"):
		return plugin.FieldErr("report_interval", "nicht kürzer als der Messabstand")
	case s.Duration("offline_after") < time.Minute:
		return plugin.FieldErr("offline_after", "mindestens 1 Minute")
	}
	return nil
}

// Run asks every connected agent for a fresh inventory.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	if RefreshAll == nil {
		return errors.New("Agent-Dienst läuft nicht")
	}
	n, err := RefreshAll(ctx)
	if err != nil {
		return err
	}
	rc.SetStat("agents", n)
	rc.Log.Info("Inventar bei den Agents angefordert", "agents", n)
	return nil
}
