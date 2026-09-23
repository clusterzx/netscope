// Package report sends a scheduled change report (weekly by default) through publishers.
package report

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/reports"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the report processor.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "report",
		Kind:               plugin.KindProcessor,
		Name:               "Geplanter Bericht",
		Description:        "Verschickt regelmäßig (Standard: montags 08:00) einen Änderungsbericht über die gewählten Publisher.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultSchedule:    "0 8 * * 1",
		DefaultTimeout:     5 * time.Minute,
		DefaultConcurrency: 1,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "publishers", Type: plugin.FieldStringList, Label: "Publisher", Required: true,
			Description: "Plugin-IDs der Publisher, über die der Bericht verschickt wird (z. B. telegram, email).",
			Validation:  &plugin.Validation{Pattern: `^[a-z][a-z0-9_]{1,31}$`, Min: plugin.Int64(1)}},
		{Key: "period_days", Type: plugin.FieldInt, Label: "Zeitraum (Tage)", Default: 7,
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(90)}},
		{Key: "priority", Type: plugin.FieldEnum, Label: "Priorität", Default: "low", Options: []plugin.Option{
			{Value: "low", Label: "Niedrig"}, {Value: "normal", Label: "Normal"}, {Value: "high", Label: "Hoch"}}},
		{Key: "title", Type: plugin.FieldString, Label: "Titel", Default: "NetScope Wochenbericht",
			Description: "Kalenderwoche und Zeitraum werden angehängt."},
	}}
}

// ValidateSettings checks that the publishers exist.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	for _, id := range s.StringList("publishers") {
		pl, ok := plugin.Get(id)
		if !ok {
			return fmt.Errorf("Publisher %q existiert nicht", id)
		}
		if pl.Info().Kind != plugin.KindPublisher {
			return fmt.Errorf("%q ist kein Publisher", id)
		}
	}
	return nil
}

// Run implements plugin.Runner: builds the report and queues one notification per
// publisher (the notification dispatcher delivers and retries).
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	pubs := rc.Settings.StringList("publishers")
	if len(pubs) == 0 {
		return errors.New("keine Publisher konfiguriert")
	}
	loc := rc.Env.Location
	if loc == nil {
		loc = time.Local
	}
	to := time.Now()
	from := to.Add(-time.Duration(rc.Settings.Int("period_days")) * 24 * time.Hour)
	r, err := reports.BuildChangeReport(ctx, rc.DB, from, to)
	if err != nil {
		return err
	}
	base := ""
	if rc.Env.PublicURL != "" {
		base = rc.Env.PublicURL + "/reports"
	}
	body := r.Markdown(loc, base)
	extra, err := r.NotificationExtra(loc, rc.Env.PublicURL)
	if err != nil {
		return fmt.Errorf("Bericht als PDF: %w", err)
	}
	_, week := to.In(loc).ISOWeek()
	title := strings.TrimSpace(rc.Settings.String("title"))
	if title == "" {
		title = "NetScope Bericht"
	}
	title = fmt.Sprintf("%s KW %d (%s–%s)", title, week, from.In(loc).Format("02.01."), to.In(loc).Format("02.01.2006"))
	now := db.Now()
	for _, pub := range pubs {
		if _, err := rc.DB.W.ExecContext(ctx, `INSERT INTO notifications(publisher_id, kind, priority, status, event_ids, title, body,
			extra, deliver_after, created_at) VALUES (?, ?, ?, 'pending', '[]', ?, ?, ?, ?, ?)`, pub, plugin.NotifyReport, rc.Settings.String("priority"),
			title, body, extra, now, now); err != nil {
			return err
		}
		rc.Log.Info("Bericht eingereiht", "publisher", pub)
	}
	rc.SetStat("publishers", len(pubs))
	rc.SetStat("new_devices", len(r.NewDevices))
	return nil
}
