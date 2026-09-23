package pluginhost

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"netscope/internal/bus"
	"netscope/internal/events"
	"netscope/internal/plugin"
)

const (
	maxHookBatch   = 1000
	maxHookBacklog = 200000
)

type hookItem struct {
	changes []plugin.Change
	run     *plugin.RunSummary
}

// hookQueue is an unbounded (capped) FIFO feeding one handler plugin.
type hookQueue struct {
	mu      sync.Mutex
	items   []hookItem
	size    int
	signal  chan struct{}
	dropped int
}

func newHookQueue() *hookQueue { return &hookQueue{signal: make(chan struct{}, 1)} }

func (q *hookQueue) push(it hookItem) {
	q.mu.Lock()
	n := len(it.changes)
	if n == 0 {
		n = 1
	}
	if q.size+n > maxHookBacklog {
		q.dropped += n
		q.mu.Unlock()
		return
	}
	q.items = append(q.items, it)
	q.size += n
	q.mu.Unlock()
	select {
	case q.signal <- struct{}{}:
	default:
	}
}

// pop returns the next unit of work: either one run summary, or up to maxHookBatch
// consecutive changes merged into one batch.
func (q *hookQueue) pop() (hookItem, int, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	dropped := q.dropped
	q.dropped = 0
	if len(q.items) == 0 {
		return hookItem{}, dropped, false
	}
	first := q.items[0]
	if first.run != nil {
		q.items = q.items[1:]
		q.size--
		return first, dropped, true
	}
	var batch []plugin.Change
	i := 0
	for i < len(q.items) && q.items[i].run == nil && len(batch)+len(q.items[i].changes) <= maxHookBatch {
		batch = append(batch, q.items[i].changes...)
		i++
	}
	if i == 0 { // a single oversized item
		batch = first.changes
		i = 1
	}
	q.items = q.items[i:]
	q.size -= len(batch)
	return hookItem{changes: batch}, dropped, true
}

// DispatchChanges hands changes to all enabled change handlers (called by the inventory).
func (h *Host) DispatchChanges(changes []plugin.Change) {
	if len(changes) == 0 {
		return
	}
	for id, q := range h.hooks {
		p := h.plugins[id]
		if _, ok := p.(plugin.ChangeHandler); !ok {
			continue
		}
		if cfg, _ := h.Config(id); cfg == nil || !cfg.Enabled {
			continue
		}
		q.push(hookItem{changes: changes})
	}
}

func (h *Host) dispatchRunFinished(run plugin.RunSummary) {
	for id, q := range h.hooks {
		if id == run.PluginID {
			continue
		}
		p := h.plugins[id]
		if _, ok := p.(plugin.RunFinishedHandler); !ok {
			continue
		}
		if cfg, _ := h.Config(id); cfg == nil || !cfg.Enabled {
			continue
		}
		r := run
		q.push(hookItem{run: &r})
	}
}

func (h *Host) hookContext(id string, runID int64) *plugin.RunContext {
	cfg, _ := h.Config(id)
	return &plugin.RunContext{
		RunID: runID, PluginID: id, Trigger: TriggerHook, Settings: plugin.NewSettings(cfg.Settings), Scope: cfg.Scope,
		Params: map[string]any{}, Concurrency: cfg.Concurrency, Log: h.Log.With("plugin", id),
		Sink: &sink{h: h, pluginID: id, runID: 0}, Events: events.Emitter{S: h.Events, PluginID: id},
		Creds: h.CredentialProvider(), Inventory: h.Inventory, DB: h.DB, DataDir: h.dataDir(id), Env: h.Env(),
		OnLive: h.liveFunc(id),
	}
}

// liveTopics are the SSE topics plugins may publish on via RunContext.Live.
var liveTopics = map[string]bool{bus.TopicHealth: true}

func (h *Host) liveFunc(pluginID string) func(topic, typ string, data map[string]any) {
	return func(topic, typ string, data map[string]any) {
		if !liveTopics[topic] {
			h.Log.Warn("Live-Update auf nicht erlaubtem Topic verworfen", "plugin", pluginID, "topic", topic)
			return
		}
		if data == nil {
			data = map[string]any{}
		}
		data["pluginId"] = pluginID
		h.Bus.Publish(topic, typ, data)
	}
}

// Context returns a RunContext for direct calls into a plugin outside of a run (e.g. the
// API executing one health check immediately).
func (h *Host) Context(id string) (*plugin.RunContext, bool) {
	if _, ok := h.Plugin(id); !ok {
		return nil, false
	}
	return h.hookContext(id, 0), true
}

func (h *Host) hookWorker(id string, q *hookQueue) {
	defer h.wg.Done()
	p := h.plugins[id]
	for {
		it, dropped, ok := q.pop()
		if dropped > 0 {
			h.Log.Error("Änderungen verworfen – Verarbeitung zu langsam", "plugin", id, "dropped", dropped)
		}
		if !ok {
			select {
			case <-h.ctx.Done():
				return
			case <-q.signal:
				continue
			}
		}
		if h.ctx.Err() != nil {
			return
		}
		cfg, _ := h.Config(id)
		if cfg == nil || !cfg.Enabled {
			continue
		}
		timeout := cfg.Timeout
		if timeout <= 0 || timeout > 10*time.Minute {
			timeout = 10 * time.Minute
		}
		ctx, cancel := context.WithTimeout(h.ctx, timeout)
		var err error
		if it.run != nil {
			if rf, ok := p.(plugin.RunFinishedHandler); ok {
				rc := h.hookContext(id, it.run.RunID)
				err = safeCall(func() error { return rf.HandleRunFinished(ctx, rc, *it.run) })
			}
		} else if ch, ok := p.(plugin.ChangeHandler); ok {
			var runID int64
			if len(it.changes) > 0 {
				runID = it.changes[0].RunID
			}
			rc := h.hookContext(id, runID)
			start := time.Now()
			err = safeCall(func() error { return ch.HandleChanges(ctx, rc, it.changes) })
			h.metrics.hook(id, len(it.changes), time.Since(start))
		}
		cancel()
		if err != nil && !errors.Is(err, context.Canceled) {
			h.Log.Error("Verarbeitung fehlgeschlagen", "plugin", id, "err", err)
		}
	}
}

// Backlog returns the number of pending changes per handler.
func (h *Host) Backlog() map[string]int {
	out := map[string]int{}
	for id, q := range h.hooks {
		q.mu.Lock()
		out[id] = q.size
		q.mu.Unlock()
	}
	return out
}

// ---------------------------------------------------------------- actions

// ActionOutcome is the result of an action request.
type ActionOutcome struct {
	RunID    int64                `json:"runId"`
	Status   string               `json:"status"`
	Finished bool                 `json:"finished"`
	Result   *plugin.ActionResult `json:"result,omitempty"`
	Error    string               `json:"error,omitempty"`
}

// RunAction queues an action as a run and waits up to wait for its completion.
func (h *Host) RunAction(ctx context.Context, pluginID, name string, params map[string]any, deviceIDs []int64, requestedBy string, wait time.Duration) (*ActionOutcome, error) {
	p, ok := h.Plugin(pluginID)
	if !ok {
		return nil, fmt.Errorf("Plugin %q existiert nicht", pluginID)
	}
	ap, ok := p.(plugin.ActionProvider)
	if !ok {
		return nil, fmt.Errorf("Plugin %q hat keine Aktionen", pluginID)
	}
	var action *plugin.Action
	for _, a := range ap.Actions() {
		if a.Name == name {
			a := a
			action = &a
		}
	}
	if action == nil {
		return nil, fmt.Errorf("unbekannte Aktion %q", name)
	}
	cfg, _ := h.Config(pluginID)
	if !cfg.Enabled {
		return nil, fmt.Errorf("Plugin %s ist deaktiviert", p.Info().Name)
	}
	vals, err := plugin.Schema{Fields: action.Params}.Validate(params, nil, func(cid int64, types []string) error {
		return h.Vault.Check(ctx, cid, types)
	})
	if err != nil {
		return nil, err
	}
	vals[actionParam] = name
	var scope *plugin.Scope
	if action.Scope == plugin.ActionDevice {
		if len(deviceIDs) == 0 {
			return nil, errors.New("keine Geräte ausgewählt")
		}
		scope = &plugin.Scope{Devices: deviceIDs}
	}
	runID, err := h.Trigger(ctx, pluginID, TriggerOptions{Trigger: TriggerAction, Scope: scope, Params: vals, RequestedBy: requestedBy})
	if err != nil {
		return nil, err
	}
	out := &ActionOutcome{RunID: runID, Status: StatusQueued}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		rv, err := h.Run(ctx, runID)
		if err != nil {
			return nil, err
		}
		out.Status = rv.Status
		if rv.Status != StatusQueued && rv.Status != StatusRunning {
			out.Finished = true
			out.Error = rv.Error
			if res, ok := rv.Stats["result"].(map[string]any); ok {
				out.Result = &plugin.ActionResult{Data: res["data"]}
				out.Result.Message, _ = res["message"].(string)
			}
			return out, nil
		}
		select {
		case <-ctx.Done():
			return out, nil
		case <-time.After(200 * time.Millisecond):
		}
	}
	return out, nil
}

func (h *Host) callAction(ctx context.Context, p plugin.Plugin, rc *plugin.RunContext, name string) (res *plugin.ActionResult, err error) {
	ap, ok := p.(plugin.ActionProvider)
	if !ok {
		return nil, errors.New("Plugin hat keine Aktionen")
	}
	params := map[string]any{}
	for k, v := range rc.Params {
		if k != actionParam {
			params[k] = v
		}
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("Plugin-Panic: %v", r)
		}
	}()
	rc.Log.Info("Aktion gestartet", "action", name)
	return ap.RunAction(ctx, rc, name, params)
}
