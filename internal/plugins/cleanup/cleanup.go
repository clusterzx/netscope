// Package cleanup enforces raw data retention per data type and downsamples time
// series (raw 7 days, 5-minute aggregates 90 days, hourly aggregates 1 year).
// Events are never deleted.
package cleanup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/timeseries"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the cleanup processor.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "cleanup",
		Kind: plugin.KindProcessor,
		Name: "Aufräumen & Downsampling",
		Description: "Verdichtet Zeitreihen (roh → 5-Minuten → Stunden) und löscht Rohdaten nach ihrer Aufbewahrungsfrist " +
			"(Beobachtungen, Laufprotokolle, Historie, Zeitreihen, alte Uploads/Backups). Events bleiben immer erhalten.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "*/15 * * * *",
		DefaultTimeout:     30 * time.Minute,
		DefaultConcurrency: 1,
	}
}

func days(key, label string, def int64, desc string) plugin.Field {
	return plugin.Field{Key: key, Type: plugin.FieldInt, Label: label, Default: def, Description: desc,
		Validation: &plugin.Validation{Min: plugin.Int64(0), Max: plugin.Int64(3650)}, Group: "Aufbewahrung (Tage)"}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		days("observations_days", "Rohdaten (Beobachtungen)", 30, "Rohdaten pro Plugin und Lauf."),
		days("presence_observations_days", "Rohdaten häufiger Anwesenheits-Scans", 2,
			"Rohdaten der Anwesenheits-Scanner (ARP, ICMP, nmap), die sehr oft laufen. Begrenzt auch den Lauf-Vergleich dieser Plugins."),
		days("run_logs_days", "Laufprotokolle", 30, ""),
		days("runs_days", "Laufhistorie", 90, "Abgeschlossene Läufe inkl. Protokoll."),
		days("notifications_days", "Benachrichtigungsverlauf", 90, ""),
		days("history_days", "Zustandshistorie", 365, "Geschlossene historische Einträge (Ports, Zertifikate, Pakete, Container, IPs, Hostnamen, CVEs). Begrenzt auch den Diff-Zeitraum."),
		days("ts_raw_days", "Zeitreihen roh", 7, ""),
		days("ts_5m_days", "Zeitreihen 5-Minuten-Aggregate", 90, ""),
		days("ts_1h_days", "Zeitreihen Stunden-Aggregate", 365, ""),
		days("audit_days", "Audit-Log", 0, "0 = unbegrenzt aufbewahren."),
		days("uploads_days", "Hochgeladene Dateien", 30, "Importdateien unter /data/uploads."),
		{Key: "backups_keep", Type: plugin.FieldInt, Label: "Anzahl aufbewahrter Backups", Default: 10,
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(1000)}, Group: "Aufbewahrung (Tage)"},
		{Key: "prune_every", Type: plugin.FieldDuration, Label: "Löschlauf höchstens alle", Default: "6h",
			Description: "Downsampling läuft bei jedem Lauf, das Löschen alter Daten seltener.",
			Validation:  &plugin.Validation{Min: plugin.Int64(900)}},
	}}
}

// batchDelete deletes rows matching where in chunks to keep write locks short.
func batchDelete(ctx context.Context, rc *plugin.RunContext, table, where string, args ...any) (int64, error) {
	var total int64
	q := fmt.Sprintf("DELETE FROM %s WHERE rowid IN (SELECT rowid FROM %s WHERE %s LIMIT 5000)", table, table, where)
	for {
		if err := ctx.Err(); err != nil {
			return total, err
		}
		res, err := rc.DB.W.ExecContext(ctx, q, args...)
		if err != nil {
			return total, fmt.Errorf("%s: %w", table, err)
		}
		n, _ := res.RowsAffected()
		total += n
		if n < 5000 {
			return total, nil
		}
	}
}

func cutoff(now time.Time, d int) int64 {
	return now.Add(-time.Duration(d) * 24 * time.Hour).UnixMilli()
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	now := time.Now()
	s := rc.Settings
	n5, n1, err := timeseries.Downsample(ctx, rc.DB, now)
	if err != nil {
		return err
	}
	rc.SetStat("downsampled_5m", n5)
	rc.SetStat("downsampled_1h", n1)
	stamp := filepath.Join(rc.DataDir, "last_prune")
	if b, err := os.ReadFile(stamp); err == nil {
		if ms, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 64); err == nil && now.Sub(time.UnixMilli(ms)) < s.Duration("prune_every") && rc.Trigger != "manual" {
			rc.Log.Info("Zeitreihen verdichtet", "5m", n5, "1h", n1)
			return nil
		}
	}
	deleted := map[string]int64{}
	del := func(label, table, where string, args ...any) error {
		n, err := batchDelete(ctx, rc, table, where, args...)
		deleted[label] += n
		return err
	}
	steps := []func() error{
		func() error {
			return del("observations", "observations", "ts < ?", cutoff(now, s.Int("observations_days")))
		},
		func() error {
			var ids []string
			for _, p := range plugin.All() {
				if p.Info().Presence {
					ids = append(ids, p.Info().ID)
				}
			}
			if len(ids) == 0 {
				return nil
			}
			args := []any{cutoff(now, s.Int("presence_observations_days"))}
			for _, id := range ids {
				args = append(args, id)
			}
			return del("observations", "observations", "ts < ? AND plugin_id IN ("+strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")+")", args...)
		},
		func() error { return del("run_logs", "run_logs", "ts < ?", cutoff(now, s.Int("run_logs_days"))) },
		func() error {
			return del("runs", "runs", "status NOT IN ('queued','running') AND created_at < ?", cutoff(now, s.Int("runs_days")))
		},
		func() error {
			return del("notifications", "notifications", "status IN ('sent','failed','skipped') AND created_at < ?", cutoff(now, s.Int("notifications_days")))
		},
		func() error {
			c := cutoff(now, s.Int("history_days"))
			for _, t := range []string{"ports", "certificates", "http_services", "packages", "containers", "container_images",
				"device_ips", "device_facts", "device_cves"} {
				if err := del("history", t, "gone_at IS NOT NULL AND gone_at < ?", c); err != nil {
					return err
				}
			}
			return nil
		},
		func() error {
			if d := s.Int("audit_days"); d > 0 {
				return del("audit", "audit_log", "ts < ?", cutoff(now, d))
			}
			return nil
		},
		func() error {
			// WITHOUT ROWID table: delete directly (small)
			res, err := rc.DB.W.ExecContext(ctx, "DELETE FROM rule_throttle WHERE last_at < ?", cutoff(now, 30))
			if err == nil {
				n, _ := res.RowsAffected()
				deleted["throttle"] += n
			}
			return err
		},
		func() error {
			return del("escalations", "escalations", "done_at IS NOT NULL AND done_at < ?", cutoff(now, 30))
		},
		func() error { return del("sessions", "sessions", "expires_at < ?", now.UnixMilli()) },
		func() error {
			// expired API tokens stay visible for a while (why does my script fail?), then go
			return del("api_tokens", "api_tokens", "expires_at IS NOT NULL AND expires_at < ?", cutoff(now, 30))
		},
		func() error {
			n, err := timeseries.Prune(ctx, rc.DB, now, time.Duration(s.Int("ts_raw_days"))*24*time.Hour,
				time.Duration(s.Int("ts_5m_days"))*24*time.Hour, time.Duration(s.Int("ts_1h_days"))*24*time.Hour)
			deleted["timeseries"] += n
			return err
		},
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	if root := rc.Env.DataRoot; root != "" {
		n := pruneFiles(filepath.Join(root, "uploads"), time.Duration(s.Int("uploads_days"))*24*time.Hour, 0, now)
		deleted["uploads"] = int64(n)
		n = pruneFiles(filepath.Join(root, "backups"), 0, s.Int("backups_keep"), now)
		deleted["backups"] = int64(n)
	}
	if _, err := rc.DB.W.ExecContext(ctx, "PRAGMA optimize"); err != nil {
		rc.Log.Warn("PRAGMA optimize", "err", err)
	}
	if err := rc.DB.Checkpoint(ctx); err != nil {
		rc.Log.Warn("WAL-Checkpoint", "err", err)
	}
	_ = os.WriteFile(stamp, []byte(strconv.FormatInt(now.UnixMilli(), 10)), 0o640)
	keys := make([]string, 0, len(deleted))
	for k := range deleted {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	args := []any{"5m", n5, "1h", n1}
	for _, k := range keys {
		rc.SetStat("deleted_"+k, deleted[k])
		args = append(args, k, deleted[k])
	}
	rc.Log.Info("Aufräumen abgeschlossen", args...)
	return nil
}

// pruneFiles deletes regular files older than maxAge (if > 0) and keeps only the newest
// keep files (if > 0). It returns the number of deleted files.
func pruneFiles(dir string, maxAge time.Duration, keep int, now time.Time) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	type f struct {
		path string
		mod  time.Time
	}
	var files []f
	for _, e := range entries {
		if !e.Type().IsRegular() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, f{filepath.Join(dir, e.Name()), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.After(files[j].mod) })
	n := 0
	for i, fl := range files {
		if (maxAge > 0 && now.Sub(fl.mod) > maxAge) || (keep > 0 && i >= keep) {
			if os.Remove(fl.path) == nil {
				n++
			}
		}
	}
	return n
}
