package pluginhost

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"netscope/internal/cron"
	"netscope/internal/db"
	"netscope/internal/plugin"
)

// ConfigView is the API representation of a configuration (secrets masked).
type ConfigView struct {
	Enabled             bool           `json:"enabled"`
	Schedule            string         `json:"schedule"`
	ScheduleText        string         `json:"scheduleText"`
	TimeoutSeconds      int            `json:"timeoutSeconds"`
	Retries             int            `json:"retries"`
	RetryBackoffSeconds int            `json:"retryBackoffSeconds"`
	Concurrency         int            `json:"concurrency"`
	Scope               plugin.Scope   `json:"scope"`
	Settings            map[string]any `json:"settings"`
	UpdatedAt           time.Time      `json:"updatedAt"`
}

func (h *Host) configView(p plugin.Plugin, c *Config) *ConfigView {
	v := &ConfigView{Enabled: c.Enabled, Schedule: c.Schedule, TimeoutSeconds: int(c.Timeout / time.Second), Retries: c.Retries,
		RetryBackoffSeconds: int(c.RetryBackoff / time.Second), Concurrency: c.Concurrency, Scope: c.Scope,
		Settings: p.Schema().MaskSecrets(c.Settings), UpdatedAt: c.UpdatedAt}
	if c.Schedule != "" {
		v.ScheduleText, _ = cron.Describe(c.Schedule)
	}
	return v
}

// Progress of a running run.
type Progress struct {
	Done  int `json:"done"`
	Total int `json:"total"`
}

// RunView is a run for the API.
type RunView struct {
	ID          int64          `json:"id"`
	PluginID    string         `json:"pluginId"`
	PluginName  string         `json:"pluginName"`
	Kind        plugin.Kind    `json:"kind"`
	Trigger     string         `json:"trigger"`
	Status      string         `json:"status"`
	Attempt     int            `json:"attempt"`
	ParentRunID int64          `json:"parentRunId,omitempty"`
	Scope       plugin.Scope   `json:"scope"`
	Params      map[string]any `json:"params"`
	CreatedAt   time.Time      `json:"createdAt"`
	NotBefore   *time.Time     `json:"notBefore,omitempty"`
	StartedAt   *time.Time     `json:"startedAt,omitempty"`
	FinishedAt  *time.Time     `json:"finishedAt,omitempty"`
	DurationMs  int64          `json:"durationMs"`
	Error       string         `json:"error,omitempty"`
	Stats       map[string]any `json:"stats"`
	RequestedBy string         `json:"requestedBy"`
	Progress    *Progress      `json:"progress,omitempty"`
}

// PluginView is the API representation of a plugin.
type PluginView struct {
	Info            plugin.Info     `json:"info"`
	Capabilities    []string        `json:"capabilities"`
	Schema          plugin.Schema   `json:"schema"`
	Actions         []plugin.Action `json:"actions"`
	Config          *ConfigView     `json:"config"`
	NextRun         *time.Time      `json:"nextRun,omitempty"`
	Running         *RunView        `json:"running,omitempty"`
	LastRun         *RunView        `json:"lastRun,omitempty"`
	MissingBinaries []string        `json:"missingBinaries,omitempty"`
	Backlog         int             `json:"backlog"`
}

func capabilities(p plugin.Plugin) []string {
	var out []string
	if _, ok := p.(plugin.Runner); ok {
		out = append(out, "run")
	}
	if _, ok := p.(plugin.Publisher); ok {
		out = append(out, "publish")
	}
	if _, ok := p.(plugin.ChangeHandler); ok {
		out = append(out, "changes")
	}
	if _, ok := p.(plugin.RunFinishedHandler); ok {
		out = append(out, "runFinished")
	}
	if _, ok := p.(plugin.ActionProvider); ok {
		out = append(out, "actions")
	}
	return out
}

// View returns the API view of one plugin.
func (h *Host) View(ctx context.Context, id string) (*PluginView, error) {
	p, ok := h.Plugin(id)
	if !ok {
		return nil, db.ErrNotFound
	}
	cfg, _ := h.Config(id)
	v := &PluginView{Info: p.Info(), Capabilities: capabilities(p), Schema: p.Schema(), Actions: []plugin.Action{},
		Config: h.configView(p, cfg), NextRun: h.NextRun(id), MissingBinaries: h.missing[id]}
	if ap, ok := p.(plugin.ActionProvider); ok {
		v.Actions = ap.Actions()
	}
	if v.Schema.Fields == nil {
		v.Schema.Fields = []plugin.Field{}
	}
	if q := h.hooks[id]; q != nil {
		q.mu.Lock()
		v.Backlog = q.size
		q.mu.Unlock()
	}
	h.mu.RLock()
	a := h.active[id]
	h.mu.RUnlock()
	if a != nil {
		if rv, err := h.Run(ctx, a.runID); err == nil {
			v.Running = rv
		}
	}
	var last sql.NullInt64
	if err := h.DB.R.QueryRowContext(ctx, "SELECT MAX(id) FROM runs WHERE plugin_id = ? AND status NOT IN ('queued','running')", id).Scan(&last); err != nil {
		return nil, err
	}
	if last.Valid {
		if rv, err := h.Run(ctx, last.Int64); err == nil {
			v.LastRun = rv
		}
	}
	return v, nil
}

// Views returns all plugins.
func (h *Host) Views(ctx context.Context) ([]*PluginView, error) {
	var out []*PluginView
	for _, id := range h.IDs() {
		v, err := h.View(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

const runSelect = `SELECT id, plugin_id, trigger, status, attempt, parent_run_id, scope, params, created_at, not_before, started_at,
	finished_at, duration_ms, error, stats, requested_by FROM runs`

func (h *Host) scanRun(rows *sql.Rows) (*RunView, error) {
	var (
		r                                   RunView
		parent, notBefore, started, fin, dm sql.NullInt64
		scope, params, stats                string
		created                             int64
	)
	if err := rows.Scan(&r.ID, &r.PluginID, &r.Trigger, &r.Status, &r.Attempt, &parent, &scope, &params, &created, &notBefore,
		&started, &fin, &dm, &r.Error, &stats, &r.RequestedBy); err != nil {
		return nil, err
	}
	r.ParentRunID = nullInt(parent)
	r.CreatedAt, r.NotBefore, r.StartedAt, r.FinishedAt, r.DurationMs = db.Time(created), db.NullTime(notBefore),
		db.NullTime(started), db.NullTime(fin), nullInt(dm)
	_ = db.Unmarshal(scope, &r.Scope)
	r.Params, r.Stats = map[string]any{}, map[string]any{}
	_ = json.Unmarshal([]byte(params), &r.Params)
	_ = json.Unmarshal([]byte(stats), &r.Stats)
	if p, ok := h.Plugin(r.PluginID); ok {
		r.PluginName, r.Kind = p.Info().Name, p.Info().Kind
	} else {
		r.PluginName = r.PluginID
	}
	if r.Status == StatusRunning {
		h.mu.RLock()
		a := h.active[r.PluginID]
		h.mu.RUnlock()
		if a != nil && a.runID == r.ID {
			a.mu.Lock()
			if a.progress.total > 0 {
				r.Progress = &Progress{Done: a.progress.done, Total: a.progress.total}
			}
			a.mu.Unlock()
			if r.StartedAt != nil {
				r.DurationMs = time.Since(*r.StartedAt).Milliseconds()
			}
		}
	}
	return &r, nil
}

// Run returns one run.
func (h *Host) Run(ctx context.Context, id int64) (*RunView, error) {
	rows, err := h.DB.R.QueryContext(ctx, runSelect+" WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, db.ErrNotFound
	}
	return h.scanRun(rows)
}

// RunFilter selects runs.
type RunFilter struct {
	PluginID string
	Status   []string
	Kind     plugin.Kind
	Before   int64 // only runs with a smaller id (e.g. the previous run of a plugin)
	// FullScope keeps runs over whole subnets (no device, group, tag or query restriction).
	FullScope bool
	Limit     int
	Offset    int
}

// Runs lists runs (newest first) and the total count.
func (h *Host) Runs(ctx context.Context, f RunFilter) ([]*RunView, int, error) {
	var conds []string
	var args []any
	if f.PluginID != "" {
		conds = append(conds, "plugin_id = ?")
		args = append(args, f.PluginID)
	}
	if len(f.Status) > 0 {
		conds = append(conds, "status IN ("+db.Placeholders(len(f.Status))+")")
		args = append(args, db.StringArgs(f.Status)...)
	}
	if f.Before > 0 {
		conds = append(conds, "id < ?")
		args = append(args, f.Before)
	}
	if f.FullScope {
		conds = append(conds, `json_extract(scope, '$.devices') IS NULL AND json_extract(scope, '$.groups') IS NULL
			AND json_extract(scope, '$.tags') IS NULL AND IFNULL(json_extract(scope, '$.query'), '') = ''`)
	}
	if f.Kind != "" {
		var ids []string
		for _, id := range h.IDs() {
			if p, _ := h.Plugin(id); p.Info().Kind == f.Kind {
				ids = append(ids, id)
			}
		}
		if len(ids) == 0 {
			return []*RunView{}, 0, nil
		}
		conds = append(conds, "plugin_id IN ("+db.Placeholders(len(ids))+")")
		args = append(args, db.StringArgs(ids)...)
	}
	where := "1=1"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}
	var total int
	if err := h.DB.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	rows, err := h.DB.R.QueryContext(ctx, runSelect+" WHERE "+where+" ORDER BY id DESC LIMIT ? OFFSET ?", append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*RunView{}
	for rows.Next() {
		r, err := h.scanRun(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// RunLog is a log line of a run.
type RunLog struct {
	ID    int64          `json:"id"`
	TS    time.Time      `json:"ts"`
	Level string         `json:"level"`
	Msg   string         `json:"msg"`
	Attrs map[string]any `json:"attrs,omitempty"`
}

// RunLogs returns log lines of a run after a given id.
func (h *Host) RunLogs(ctx context.Context, runID, afterID int64, limit int) ([]RunLog, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := h.DB.R.QueryContext(ctx, "SELECT id, ts, level, msg, attrs FROM run_logs WHERE run_id = ? AND id > ? ORDER BY id LIMIT ?", runID, afterID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RunLog{}
	for rows.Next() {
		var (
			l     RunLog
			ts    int64
			attrs string
		)
		if err := rows.Scan(&l.ID, &ts, &l.Level, &l.Msg, &attrs); err != nil {
			return nil, err
		}
		l.TS = db.Time(ts)
		if attrs != "" && attrs != "{}" {
			_ = json.Unmarshal([]byte(attrs), &l.Attrs)
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// ActiveRuns returns the currently running runs.
func (h *Host) ActiveRuns(ctx context.Context) ([]*RunView, error) {
	h.mu.RLock()
	var ids []int64
	for _, a := range h.active {
		ids = append(ids, a.runID)
	}
	h.mu.RUnlock()
	out := []*RunView{}
	for _, id := range ids {
		if rv, err := h.Run(ctx, id); err == nil {
			out = append(out, rv)
		}
	}
	return out, nil
}

// TestPublisher sends a test notification through a publisher (even if disabled).
func (h *Host) TestPublisher(ctx context.Context, id, requestedBy string) error {
	env := h.Env()
	n := &plugin.Notification{Kind: plugin.NotifyTest, Priority: plugin.PrioNormal, Title: "NetScope: Testnachricht",
		Body: fmt.Sprintf("Diese Testnachricht wurde von %s ausgelöst. Ist sie angekommen, ist der Publisher korrekt eingerichtet.", requestedBy),
		Link: env.PublicURL, CreatedAt: time.Now()}
	return h.Publish(ctx, id, n)
}
