package pluginhost

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"netscope/internal/bus"
	"netscope/internal/cron"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/logging"
	"netscope/internal/plugin"
)

// Run triggers.
const (
	TriggerSchedule = "schedule"
	TriggerManual   = "manual"
	TriggerRetry    = "retry"
	TriggerDevice   = "device"
	TriggerAction   = "action"
	TriggerHook     = "hook"
)

// Run statuses.
const (
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSuccess   = "success"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
	StatusTimeout   = "timeout"
)

const actionParam = "_action"

// TriggerOptions describe a run request.
type TriggerOptions struct {
	Trigger     string
	Scope       *plugin.Scope // overrides the configured scope
	Params      map[string]any
	RequestedBy string
	attempt     int
	parent      int64
	notBefore   time.Time
}

// ErrAlreadyQueued is returned for a scheduled trigger while the plugin is queued/running.
var ErrAlreadyQueued = errors.New("Plugin ist bereits eingeplant oder läuft")

// Trigger queues a run of a plugin and returns the run id.
func (h *Host) Trigger(ctx context.Context, id string, opt TriggerOptions) (int64, error) {
	p, ok := h.Plugin(id)
	if !ok {
		return 0, db.ErrNotFound
	}
	_, isRunner := p.(plugin.Runner)
	_, isAction := opt.Params[actionParam]
	if !isRunner && !isAction {
		return 0, fmt.Errorf("Plugin %s kann nicht ausgeführt werden", id)
	}
	if opt.Trigger == "" {
		opt.Trigger = TriggerManual
	}
	if opt.attempt == 0 {
		opt.attempt = 1
	}
	cfg, _ := h.Config(id)
	scope := cfg.Scope
	if opt.Scope != nil {
		scope = *opt.Scope
		if err := h.validateScope(ctx, &scope); err != nil {
			return 0, err
		}
	}
	if opt.Trigger == TriggerSchedule {
		var n int
		if err := h.DB.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM runs WHERE plugin_id = ? AND status IN ('queued','running')", id).Scan(&n); err != nil {
			return 0, err
		}
		if n > 0 {
			return 0, ErrAlreadyQueued
		}
	}
	params := opt.Params
	if params == nil {
		params = map[string]any{}
	}
	var parent, notBefore any
	if opt.parent > 0 {
		parent = opt.parent
	}
	if !opt.notBefore.IsZero() {
		notBefore = opt.notBefore.UnixMilli()
	}
	res, err := h.DB.W.ExecContext(ctx, `INSERT INTO runs(plugin_id, trigger, status, attempt, parent_run_id, scope, params, not_before, created_at, requested_by)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, id, opt.Trigger, StatusQueued, opt.attempt, parent, db.JSON(scope), db.JSON(params), notBefore, db.Now(), opt.RequestedBy)
	if err != nil {
		return 0, err
	}
	runID, _ := res.LastInsertId()
	h.publishRun(ctx, "queued", runID, map[string]any{"pluginId": id, "trigger": opt.Trigger}, nil)
	h.kick()
	return runID, nil
}

// publishRun streams a run lifecycle message. data gets the run id and, as "run", the
// current RunView (view, or read from the database), so clients need not fetch it.
func (h *Host) publishRun(ctx context.Context, typ string, id int64, data map[string]any, view *RunView) {
	data["id"] = id
	if view == nil {
		if v, err := h.Run(ctx, id); err == nil {
			view = v
		}
	}
	if view != nil {
		data["run"] = view
	}
	h.Bus.Publish(bus.TopicRun, typ, data)
}

// Cancel cancels a queued or running run.
func (h *Host) Cancel(ctx context.Context, runID int64) error {
	res, err := h.DB.W.ExecContext(ctx, "UPDATE runs SET status = ?, finished_at = ?, error = ? WHERE id = ? AND status = ?",
		StatusCancelled, db.Now(), errCancelled.Error(), runID, StatusQueued)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n > 0 {
		h.publishRun(ctx, "finished", runID, map[string]any{"status": StatusCancelled, "error": errCancelled.Error()}, nil)
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, a := range h.active {
		if a.runID == runID {
			a.cancel(errCancelled)
			return nil
		}
	}
	return fmt.Errorf("Lauf %d läuft nicht", runID)
}

type queuedRun struct {
	id          int64
	pluginID    string
	trigger     string
	attempt     int
	parent      sql.NullInt64
	scope       plugin.Scope
	params      map[string]any
	requestedBy string
}

func (h *Host) execute(r queuedRun, a *activeRun, runCtx context.Context) {
	defer h.wg.Done()
	defer func() {
		h.mu.Lock()
		delete(h.active, r.pluginID)
		h.mu.Unlock()
		h.kick()
	}()
	p, _ := h.Plugin(r.pluginID)
	info := p.Info()
	cfg, _ := h.Config(r.pluginID)
	started := time.Now()
	res, err := h.DB.W.ExecContext(h.ctx, "UPDATE runs SET status = ?, started_at = ? WHERE id = ? AND status = ?",
		StatusRunning, started.UnixMilli(), r.id, StatusQueued)
	if err != nil {
		h.Log.Error("start run", "run", r.id, "err", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return // cancelled meanwhile
	}
	h.publishRun(h.ctx, "started", r.id, map[string]any{"pluginId": r.pluginID, "trigger": r.trigger}, nil)

	logs := newRunLogger(h, r.id, r.pluginID)
	logger := logging.Tee(h.Log.With("plugin", r.pluginID, "run", r.id), logs.record)
	ctx, cancelTimeout := context.WithTimeout(runCtx, cfg.Timeout)
	defer cancelTimeout()

	rc := &plugin.RunContext{
		RunID: r.id, PluginID: r.pluginID, Trigger: r.trigger, Settings: plugin.NewSettings(cfg.Settings),
		Scope: r.scope, Params: r.params, Concurrency: cfg.Concurrency, Log: logger,
		Sink: &sink{h: h, pluginID: r.pluginID, runID: r.id}, Events: events.Emitter{S: h.Events, PluginID: r.pluginID},
		Creds: h.CredentialProvider(), Inventory: h.Inventory, DB: h.DB, DataDir: h.dataDir(r.pluginID), Env: h.Env(),
	}
	rc.OnLive = h.liveFunc(r.pluginID)
	rc.OnProgress = func(done, total int) {
		a.mu.Lock()
		last := a.progress.at
		a.progress = progress{done: done, total: total, at: time.Now()}
		a.mu.Unlock()
		if time.Since(last) > 500*time.Millisecond || done == total {
			h.Bus.Publish(bus.TopicRun, "progress", map[string]any{"id": r.id, "pluginId": r.pluginID, "done": done, "total": total})
		}
	}
	var runErr error
	var actionResult *plugin.ActionResult
	mode := info.Targets
	if _, isAction := r.params[actionParam]; isAction && len(r.scope.Devices) > 0 {
		mode = plugin.TargetDevices // device actions always get their devices
	}
	targets, terr := h.Inventory.ResolveTargets(ctx, r.scope, mode)
	if terr != nil {
		runErr = fmt.Errorf("Scope auflösen: %w", terr)
	} else {
		rc.Targets = targets
		if len(targets.Subnets) > 0 || len(targets.Devices) > 0 {
			logger.Log(ctx, lifecycleLevel(r.trigger), "Lauf gestartet", "subnets", len(targets.Subnets), "devices", len(targets.Devices), "trigger", r.trigger)
		} else {
			logger.Log(ctx, lifecycleLevel(r.trigger), "Lauf gestartet", "trigger", r.trigger)
		}
		if name, ok := r.params[actionParam].(string); ok {
			actionResult, runErr = h.callAction(ctx, p, rc, name)
		} else if runner, ok := p.(plugin.Runner); ok {
			runErr = h.callRun(ctx, runner, rc, logger)
		} else {
			runErr = errors.New("Plugin hat keine Läufe")
		}
	}
	status := StatusSuccess
	errText := ""
	quiet := errors.Is(runErr, plugin.ErrNoChanges) && r.trigger == TriggerSchedule
	if errors.Is(runErr, plugin.ErrNoChanges) {
		runErr = nil
	}
	switch {
	case errors.Is(context.Cause(runCtx), errCancelled):
		status, errText = StatusCancelled, errCancelled.Error()
	case errors.Is(context.Cause(runCtx), errShutdown) || (h.ctx.Err() != nil && runErr != nil):
		status, errText = StatusCancelled, errShutdown.Error()
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		status, errText = StatusTimeout, fmt.Sprintf("Zeitüberschreitung nach %s", cfg.Timeout)
	case runErr != nil:
		status, errText = StatusFailed, runErr.Error()
	}
	finished := time.Now()
	stats := rc.Stats()
	stats["observations"] = rc.Sink.(*sink).count()
	if actionResult != nil {
		stats["result"] = actionResult
	}
	if status == StatusSuccess {
		logger.Log(context.Background(), lifecycleLevel(r.trigger), "Lauf beendet", "duration", finished.Sub(started).Round(time.Millisecond).String())
	} else {
		logger.Warn("Lauf nicht erfolgreich", "status", status, "error", errText)
	}
	logs.close()
	// the shutdown context may be gone: use a short background context for bookkeeping
	bg, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	finishedMsg := map[string]any{"pluginId": r.pluginID, "status": status, "error": errText, "durationMs": finished.Sub(started).Milliseconds()}
	var finalView *RunView
	if quiet && status == StatusSuccess {
		// uneventful scheduled run: do not keep it in the history; the live message still
		// carries the final view (marked discarded) so clients need not fetch it
		if v, err := h.Run(bg, r.id); err == nil {
			v.Status, v.DurationMs, v.Stats, v.Progress = status, finished.Sub(started).Milliseconds(), stats, nil
			fin := finished
			v.FinishedAt = &fin
			finalView = v
		}
		finishedMsg["discarded"] = true
		if _, err := h.DB.W.ExecContext(bg, "DELETE FROM runs WHERE id = ?", r.id); err != nil {
			h.Log.Error("drop quiet run", "run", r.id, "err", err)
		}
	} else if _, err := h.DB.W.ExecContext(bg, `UPDATE runs SET status = ?, finished_at = ?, duration_ms = ?, error = ?, stats = ? WHERE id = ?`,
		status, finished.UnixMilli(), finished.Sub(started).Milliseconds(), errText, db.JSON(stats), r.id); err != nil {
		h.Log.Error("finish run", "run", r.id, "err", err)
	}
	h.metrics.run(r.pluginID, status, finished.Sub(started))
	h.publishRun(bg, "finished", r.id, finishedMsg, finalView)

	if status == StatusFailed || status == StatusTimeout {
		if r.attempt <= cfg.Retries && h.ctx.Err() == nil {
			backoff := cfg.RetryBackoff * time.Duration(1<<uint(min(r.attempt-1, 10)))
			if backoff > 6*time.Hour {
				backoff = 6 * time.Hour
			}
			if _, err := h.Trigger(bg, r.pluginID, TriggerOptions{Trigger: TriggerRetry, Scope: &r.scope, Params: r.params,
				RequestedBy: r.requestedBy, attempt: r.attempt + 1, parent: r.id, notBefore: time.Now().Add(backoff)}); err != nil {
				h.Log.Error("schedule retry", "plugin", r.pluginID, "err", err)
			} else {
				h.Log.Info("Wiederholung eingeplant", "plugin", r.pluginID, "attempt", r.attempt+1, "in", backoff.String())
			}
		} else if _, isAction := r.params[actionParam]; !isAction {
			if _, err := h.Events.Emit(bg, "core", plugin.Event{Type: plugin.EvPluginFailed, RunID: r.id,
				Title:   fmt.Sprintf("%s: Lauf fehlgeschlagen", info.Name),
				Message: errText, Payload: map[string]any{"plugin": r.pluginID, "run_id": r.id, "error": errText, "attempt": r.attempt},
				DedupKey: "plugin.failed:" + r.pluginID, DedupWindow: 6 * time.Hour}); err != nil {
				h.Log.Error("emit plugin.failed", "err", err)
			}
		}
	}
	if _, isAction := r.params[actionParam]; isAction {
		return
	}
	summary := plugin.RunSummary{RunID: r.id, PluginID: r.pluginID, Kind: info.Kind, Status: status, Presence: info.Presence,
		Targets: rc.Targets, Started: started, Finished: finished, Error: errText}
	if all, prefixes := rc.IncompletePresence(); all {
		summary.Presence = false
	} else if len(prefixes) > 0 {
		// subnets whose scan failed must not count their devices as missed
		var kept []plugin.SubnetTarget
		for _, sn := range summary.Targets.Subnets {
			skip := false
			for _, p := range prefixes {
				if p.Masked() == sn.CIDR.Masked() {
					skip = true
				}
			}
			if !skip {
				kept = append(kept, sn)
			}
		}
		summary.Targets.Subnets = kept
		if len(kept) == 0 {
			summary.Presence = false
		}
		logger.Warn("Anwesenheitsauswertung ohne fehlgeschlagene Subnetze", "subnets", len(prefixes))
	}
	if err := h.Inventory.RunFinished(bg, summary); err != nil {
		h.Log.Error("presence evaluation", "plugin", r.pluginID, "err", err)
	}
	h.dispatchRunFinished(summary)
}

// lifecycleLevel keeps frequent scheduled runs out of the info log; manual runs are
// always logged.
func lifecycleLevel(trigger string) slog.Level {
	if trigger == TriggerSchedule || trigger == TriggerRetry {
		return slog.LevelDebug
	}
	return slog.LevelInfo
}

func (h *Host) callRun(ctx context.Context, runner plugin.Runner, rc *plugin.RunContext, logger *slog.Logger) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			logger.Error("Plugin-Panic", "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
			err = fmt.Errorf("Plugin-Panic: %v", rec)
		}
	}()
	return runner.Run(ctx, rc)
}

// sink wraps the inventory for one run and counts observations.
type sink struct {
	h        *Host
	pluginID string
	runID    int64
	mu       sync.Mutex
	n        int
}

func (s *sink) Observe(ctx context.Context, o *plugin.Observation) (int64, error) {
	id, err := s.h.Inventory.Observe(ctx, s.pluginID, s.runID, o)
	if err == nil {
		s.mu.Lock()
		s.n++
		s.mu.Unlock()
	}
	return id, err
}

func (s *sink) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.n
}

// ---------------------------------------------------------------- run logs

const maxRunLogLines = 5000

type logLine struct {
	ts    time.Time
	level string
	msg   string
	attrs string
	m     map[string]any // attrs for the live stream
}

// runLogger batches log lines of a run into run_logs and streams them to the bus.
type runLogger struct {
	h        *Host
	runID    int64
	pluginID string
	ch       chan logLine
	done     chan struct{}
	mu       sync.Mutex
	n        int
	dropped  int
	closed   bool
}

func newRunLogger(h *Host, runID int64, pluginID string) *runLogger {
	l := &runLogger{h: h, runID: runID, pluginID: pluginID, ch: make(chan logLine, 1024), done: make(chan struct{})}
	go l.loop()
	return l
}

func (l *runLogger) record(rec slog.Record, attrs []slog.Attr) {
	m := map[string]any{}
	add := func(a slog.Attr) {
		if a.Key == "plugin" || a.Key == "run" {
			return
		}
		v := a.Value.Resolve().Any()
		if err, ok := v.(error); ok {
			v = err.Error()
		}
		m[a.Key] = v
	}
	for _, a := range attrs {
		add(a)
	}
	rec.Attrs(func(a slog.Attr) bool { add(a); return true })
	b, _ := json.Marshal(m)
	line := logLine{ts: rec.Time, level: rec.Level.String(), msg: rec.Message, attrs: string(b), m: m}
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	if l.n >= maxRunLogLines {
		l.dropped++
		l.mu.Unlock()
		return
	}
	l.n++
	// send under the lock so close() can never close the channel in between
	l.ch <- line
	l.mu.Unlock()
}

func (l *runLogger) loop() {
	defer close(l.done)
	var batch []logLine
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	flush := func() {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		ids := make([]int64, len(batch))
		err := l.h.DB.Tx(ctx, func(tx *sql.Tx) error {
			for i, ln := range batch {
				res, err := tx.ExecContext(ctx, "INSERT INTO run_logs(run_id, ts, level, msg, attrs) VALUES (?,?,?,?,?)",
					l.runID, ln.ts.UnixMilli(), ln.level, ln.msg, ln.attrs)
				if err != nil {
					return err
				}
				ids[i], _ = res.LastInsertId()
			}
			return nil
		})
		if err != nil {
			l.h.Log.Error("write run log", "run", l.runID, "err", err)
		}
		// stream after storing: live lines carry the log id, so viewers can continue
		// seamlessly with GET /runs/{id}/logs?after=<id> (id 0 = not stored)
		for i, ln := range batch {
			var id int64
			if err == nil {
				id = ids[i]
			}
			attrs := ln.m
			if attrs == nil {
				attrs = map[string]any{}
			}
			l.h.Bus.Publish(bus.TopicRunLog, "line", map[string]any{"id": id, "runId": l.runID, "pluginId": l.pluginID, "ts": ln.ts,
				"level": ln.level, "msg": ln.msg, "attrs": attrs})
		}
		batch = batch[:0]
	}
	for {
		select {
		case ln, ok := <-l.ch:
			if !ok {
				flush()
				return
			}
			batch = append(batch, ln)
			if len(batch) >= 200 {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (l *runLogger) close() {
	l.mu.Lock()
	if l.closed {
		l.mu.Unlock()
		return
	}
	l.closed = true
	if l.dropped > 0 {
		l.ch <- logLine{ts: time.Now(), level: "WARN", msg: fmt.Sprintf("%d weitere Protokollzeilen verworfen (Limit %d)", l.dropped, maxRunLogLines), attrs: "{}"}
	}
	close(l.ch)
	l.mu.Unlock()
	<-l.done
}

// ---------------------------------------------------------------- scheduler

func (h *Host) schedulerLoop() {
	defer h.wg.Done()
	next := map[string]time.Time{}
	keys := map[string]string{}
	for {
		now := time.Now().In(h.Location)
		var earliest time.Time
		for _, id := range h.IDs() {
			cfg, _ := h.Config(id)
			if cfg == nil || !cfg.Enabled || cfg.Schedule == "" {
				delete(next, id)
				delete(keys, id)
				continue
			}
			sched, err := cron.Parse(cfg.Schedule)
			if err != nil {
				continue
			}
			if keys[id] != cfg.Schedule || next[id].IsZero() {
				keys[id] = cfg.Schedule
				next[id] = sched.Next(now)
			}
			if !next[id].After(now) {
				if _, err := h.Trigger(h.ctx, id, TriggerOptions{Trigger: TriggerSchedule, RequestedBy: "Zeitplan"}); err != nil {
					if errors.Is(err, ErrAlreadyQueued) {
						h.Log.Info("Geplanter Lauf übersprungen – vorheriger Lauf aktiv", "plugin", id)
					} else if h.ctx.Err() == nil {
						h.Log.Error("scheduled trigger", "plugin", id, "err", err)
					}
				}
				next[id] = sched.Next(now)
			}
			if earliest.IsZero() || next[id].Before(earliest) {
				earliest = next[id]
			}
		}
		wait := time.Minute
		if !earliest.IsZero() {
			if d := time.Until(earliest); d < wait {
				wait = d
			}
		}
		if wait < 50*time.Millisecond {
			wait = 50 * time.Millisecond
		}
		timer := time.NewTimer(wait)
		select {
		case <-h.ctx.Done():
			timer.Stop()
			return
		case <-h.wakeCh:
			timer.Stop()
		case <-timer.C:
		}
	}
}

// NextRun returns the next scheduled time of a plugin (nil if none).
func (h *Host) NextRun(id string) *time.Time {
	cfg, ok := h.Config(id)
	if !ok || !cfg.Enabled || cfg.Schedule == "" {
		return nil
	}
	s, err := cron.Parse(cfg.Schedule)
	if err != nil {
		return nil
	}
	t := s.Next(time.Now().In(h.Location))
	if t.IsZero() {
		return nil
	}
	return &t
}

func (h *Host) dispatcherLoop() {
	defer h.wg.Done()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.ctx.Done():
			return
		case <-h.dispatch:
		case <-ticker.C:
		}
		if err := h.dispatchQueued(); err != nil && h.ctx.Err() == nil {
			h.Log.Error("dispatch runs", "err", err)
		}
	}
}

func (h *Host) dispatchQueued() error {
	rows, err := h.DB.R.QueryContext(h.ctx, `SELECT id, plugin_id, trigger, attempt, parent_run_id, scope, params, requested_by FROM runs
		WHERE status = 'queued' AND (not_before IS NULL OR not_before <= ?) ORDER BY id`, db.Now())
	if err != nil {
		return err
	}
	var list []queuedRun
	for rows.Next() {
		var (
			r             queuedRun
			scope, params string
		)
		if err := rows.Scan(&r.id, &r.pluginID, &r.trigger, &r.attempt, &r.parent, &scope, &params, &r.requestedBy); err != nil {
			rows.Close()
			return err
		}
		_ = db.Unmarshal(scope, &r.scope)
		r.params = map[string]any{}
		_ = db.Unmarshal(params, &r.params)
		list = append(list, r)
	}
	rows.Close()
	limit := h.Settings.System().MaxParallelRuns
	for _, r := range list {
		if _, ok := h.Plugin(r.pluginID); !ok {
			_, _ = h.DB.W.ExecContext(h.ctx, "UPDATE runs SET status = ?, error = ?, finished_at = ? WHERE id = ?",
				StatusFailed, "Plugin nicht vorhanden", db.Now(), r.id)
			continue
		}
		h.mu.Lock()
		if _, busy := h.active[r.pluginID]; busy {
			h.mu.Unlock()
			continue
		}
		if len(h.active) >= limit {
			h.mu.Unlock()
			return nil
		}
		runCtx, cancel := context.WithCancelCause(h.ctx)
		a := &activeRun{runID: r.id, started: time.Now(), cancel: cancel}
		h.active[r.pluginID] = a
		h.mu.Unlock()
		h.wg.Add(1)
		go func(r queuedRun, a *activeRun, runCtx context.Context, cancel context.CancelCauseFunc) {
			defer cancel(nil)
			h.execute(r, a, runCtx)
		}(r, a, runCtx, cancel)
	}
	return nil
}
