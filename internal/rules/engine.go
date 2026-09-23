package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"netscope/internal/bus"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/settings"
)

// Publisher delivers notifications (implemented by the plugin host).
type Publisher interface {
	Publish(ctx context.Context, id string, n *plugin.Notification) error
	PublisherEnabled(id string) bool
	Plugin(id string) (plugin.Plugin, bool)
}

// Notification statuses.
const (
	NStatusPending = "pending"
	NStatusSending = "sending"
	NStatusSent    = "sent"
	NStatusFailed  = "failed"
	NStatusSkipped = "skipped"
)

const (
	bundleWindow  = 5 * time.Second
	maxAttempts   = 5
	dispatchEvery = 2 * time.Second
)

// Engine evaluates rules and dispatches notifications.
type Engine struct {
	db       *db.DB
	bus      *bus.Bus
	log      *slog.Logger
	events   *events.Store
	inv      *inventory.Store
	pub      Publisher
	settings *settings.Store
	loc      *time.Location

	mu    sync.Mutex
	queue []events.Event
	sig   chan struct{}
	kick  chan struct{}
	wg    sync.WaitGroup
	ctx   context.Context
	stop  context.CancelFunc
	nowFn func() time.Time
}

// New creates the engine.
func New(d *db.DB, b *bus.Bus, log *slog.Logger, ev *events.Store, inv *inventory.Store, pub Publisher, st *settings.Store, loc *time.Location) *Engine {
	return &Engine{db: d, bus: b, log: log, events: ev, inv: inv, pub: pub, settings: st, loc: loc,
		sig: make(chan struct{}, 1), kick: make(chan struct{}, 1), nowFn: time.Now}
}

func (e *Engine) publisherExists(id string) bool {
	p, ok := e.pub.Plugin(id)
	if !ok {
		return false
	}
	_, isPub := p.(plugin.Publisher)
	return isPub
}

// OnEvent queues a new event for evaluation (registered with events.Store.OnEvent).
func (e *Engine) OnEvent(ev events.Event) {
	e.mu.Lock()
	e.queue = append(e.queue, ev)
	e.mu.Unlock()
	select {
	case e.sig <- struct{}{}:
	default:
	}
}

// Start runs the evaluator, dispatcher and escalation loops.
func (e *Engine) Start(ctx context.Context) {
	e.ctx, e.stop = context.WithCancel(ctx)
	e.wg.Add(2)
	go e.evaluator()
	go e.dispatcher()
}

// Stop ends the loops.
func (e *Engine) Stop() {
	if e.stop != nil {
		e.stop()
		e.wg.Wait()
	}
}

func (e *Engine) evaluator() {
	defer e.wg.Done()
	for {
		e.mu.Lock()
		batch := e.queue
		e.queue = nil
		e.mu.Unlock()
		for _, ev := range batch {
			if err := e.Evaluate(e.ctx, ev); err != nil && e.ctx.Err() == nil {
				e.log.Error("Regelauswertung fehlgeschlagen", "event", ev.ID, "err", err)
			}
		}
		if len(batch) > 0 {
			e.kickDispatcher()
			continue
		}
		select {
		case <-e.ctx.Done():
			return
		case <-e.sig:
		}
	}
}

func (e *Engine) kickDispatcher() {
	select {
	case e.kick <- struct{}{}:
	default:
	}
}

func (e *Engine) device(ctx context.Context, id int64) (*DeviceContext, error) {
	if id <= 0 {
		return nil, nil
	}
	info, err := e.inv.Device(ctx, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &DeviceContext{ID: info.ID, State: info.State, Tags: info.Tags, IPs: info.IPs}, nil
}

func toInput(ev events.Event) EventInput {
	return EventInput{ID: ev.ID, Type: ev.Type, Severity: ev.Severity, DeviceID: ev.DeviceID, RunID: ev.RunID, Title: ev.Title,
		Message: ev.Message, Payload: ev.Payload, At: ev.TS, DedupKey: ev.DedupKey}
}

// Evaluate matches one stored event against all enabled rules and plans notifications.
func (e *Engine) Evaluate(ctx context.Context, ev events.Event) error {
	in := toInput(ev)
	dev, err := e.device(ctx, in.DeviceID)
	if err != nil {
		return err
	}
	if dev != nil && dev.State == "ignored" {
		return nil // ignored devices never notify
	}
	rules, err := e.List(ctx)
	if err != nil {
		return err
	}
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		ok, _ := e.match(ctx, r.Conditions, in, dev)
		if !ok {
			continue
		}
		for i, a := range r.Actions {
			if _, err := e.plan(ctx, r, i, a, in, false); err != nil {
				e.log.Error("Benachrichtigung planen", "rule", r.ID, "err", err)
			}
		}
		if r.Stop {
			break
		}
	}
	return nil
}

// ActionPlan describes what an action would do (simulation) or did.
type ActionPlan struct {
	Publisher        string     `json:"publisher"`
	PublisherName    string     `json:"publisherName"`
	PublisherEnabled bool       `json:"publisherEnabled"`
	Priority         string     `json:"priority"`
	Mode             string     `json:"mode"`
	DeliverAt        time.Time  `json:"deliverAt"`
	Throttled        bool       `json:"throttled"`
	Quiet            string     `json:"quiet,omitempty"` // "" | delayed | dropped
	Skipped          string     `json:"skipped,omitempty"`
	EscalateAt       *time.Time `json:"escalateAt,omitempty"`
}

func quietEnd(now time.Time, from, to int) time.Time {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	end := day.Add(time.Duration(to) * time.Minute)
	if !end.After(now) {
		end = end.Add(24 * time.Hour)
	}
	return end
}

// plan computes (and unless dryRun stores) the notification for one rule action.
func (e *Engine) plan(ctx context.Context, r *Rule, idx int, a Action, ev EventInput, dryRun bool) (*ActionPlan, error) {
	now := e.nowFn().In(e.loc)
	p := &ActionPlan{Publisher: a.Publisher, Priority: a.Priority, Mode: a.Mode}
	if pl, ok := e.pub.Plugin(a.Publisher); ok {
		p.PublisherName = pl.Info().Name
	}
	p.PublisherEnabled = e.pub.PublisherEnabled(a.Publisher)
	skip := func(reason string) (*ActionPlan, error) {
		p.Skipped = reason
		if dryRun {
			return p, nil
		}
		_, err := e.db.W.ExecContext(ctx, `INSERT INTO notifications(rule_id, action_index, publisher_id, kind, priority, status, event_ids,
			deliver_after, created_at, error) VALUES (?,?,?,?,?,?,?,?,?,?)`, r.ID, idx, a.Publisher, plugin.NotifyEvent, a.Priority,
			NStatusSkipped, db.JSON([]int64{ev.ID}), now.UnixMilli(), now.UnixMilli(), reason)
		return p, err
	}
	if !e.publisherExists(a.Publisher) {
		return skip("Publisher existiert nicht")
	}
	if !p.PublisherEnabled {
		return skip("Publisher ist deaktiviert")
	}
	if a.Throttle != "" {
		d, _ := time.ParseDuration(a.Throttle)
		key := fmt.Sprintf("%s|%d|%s", ev.Type, ev.DeviceID, ev.DedupKey)
		var last sql.NullInt64
		if err := e.db.R.QueryRowContext(ctx, "SELECT last_at FROM rule_throttle WHERE rule_id = ? AND key = ?", r.ID, key).Scan(&last); err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if last.Valid && now.Sub(time.UnixMilli(last.Int64)) < d {
			p.Throttled = true
			return p, nil
		}
		if !dryRun {
			if _, err := e.db.W.ExecContext(ctx, `INSERT INTO rule_throttle(rule_id, key, last_at) VALUES (?,?,?)
				ON CONFLICT(rule_id, key) DO UPDATE SET last_at = excluded.last_at`, r.ID, key, now.UnixMilli()); err != nil {
				return nil, err
			}
		}
	}
	deliver := now.Add(bundleWindow)
	group := fmt.Sprintf("imm:%d:%d:%d", r.ID, idx, ev.RunID)
	if a.Mode == "batch" {
		deliver = now.Add(time.Duration(a.BatchMinutes) * time.Minute)
		group = fmt.Sprintf("batch:%d:%d", r.ID, idx)
	}
	if q := a.QuietHours; q != nil {
		from, _ := parseHM(q.From)
		to, _ := parseHM(q.To)
		if inRange(now.Hour()*60+now.Minute(), from, to) && !(q.AllowUrgent && a.Priority == string(plugin.PrioUrgent)) {
			if q.Behavior == "drop" {
				p.Quiet = "dropped"
				return skip("Ruhezeit")
			}
			p.Quiet = "delayed"
			end := quietEnd(now, from, to)
			if end.After(deliver) {
				deliver = end
			}
			group += ":quiet"
		}
	}
	p.DeliverAt = deliver
	if a.EscalateAfterMinutes > 0 {
		at := now.Add(time.Duration(a.EscalateAfterMinutes) * time.Minute)
		p.EscalateAt = &at
	}
	if dryRun {
		return p, nil
	}
	var created, extended int64 // notification ids for the live stream
	err := e.db.Tx(ctx, func(tx *sql.Tx) error {
		created, extended = 0, 0
		var (
			id  int64
			ids string
		)
		err := tx.QueryRowContext(ctx, "SELECT id, event_ids FROM notifications WHERE group_key = ? AND status = ? ORDER BY id LIMIT 1",
			group, NStatusPending).Scan(&id, &ids)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			res, err := tx.ExecContext(ctx, `INSERT INTO notifications(rule_id, action_index, publisher_id, kind, priority, status, group_key,
				event_ids, deliver_after, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, r.ID, idx, a.Publisher, plugin.NotifyEvent, a.Priority,
				NStatusPending, group, db.JSON([]int64{ev.ID}), deliver.UnixMilli(), now.UnixMilli())
			if err != nil {
				return err
			}
			created, _ = res.LastInsertId()
		case err != nil:
			return err
		default:
			var list []int64
			_ = json.Unmarshal([]byte(ids), &list)
			list = append(list, ev.ID)
			if _, err := tx.ExecContext(ctx, "UPDATE notifications SET event_ids = ? WHERE id = ?", db.JSON(list), id); err != nil {
				return err
			}
			extended = id
		}
		if a.EscalateAfterMinutes > 0 && ev.ID > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO escalations(event_id, rule_id, action_index, due_at) VALUES (?,?,?,?)`,
				ev.ID, r.ID, idx, p.EscalateAt.UnixMilli()); err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		live := map[string]any{"publisher": a.Publisher, "rule": r.ID, "event": ev.ID, "deliverAt": deliver.Format(time.RFC3339)}
		switch {
		case created > 0:
			live["id"] = created
			e.bus.Publish(bus.TopicNotification, "created", live)
		case extended > 0:
			live["id"] = extended
			e.bus.Publish(bus.TopicNotification, "updated", live)
		}
	}
	return p, err
}

// ---------------------------------------------------------------- dispatcher

func (e *Engine) dispatcher() {
	defer e.wg.Done()
	ticker := time.NewTicker(dispatchEvery)
	defer ticker.Stop()
	escalate := time.NewTicker(30 * time.Second)
	defer escalate.Stop()
	// a crash while sending leaves "sending" rows: retry them
	if _, err := e.db.W.ExecContext(e.ctx, "UPDATE notifications SET status = ? WHERE status = ?", NStatusPending, NStatusSending); err != nil {
		e.log.Error("reset notifications", "err", err)
	}
	for {
		select {
		case <-e.ctx.Done():
			return
		case <-ticker.C:
		case <-e.kick:
		case <-escalate.C:
			if err := e.checkEscalations(e.ctx); err != nil && e.ctx.Err() == nil {
				e.log.Error("Eskalationen prüfen", "err", err)
			}
		}
		if err := e.deliverDue(e.ctx); err != nil && e.ctx.Err() == nil {
			e.log.Error("Benachrichtigungen zustellen", "err", err)
		}
	}
}

type pending struct {
	id        int64
	ruleID    sql.NullInt64
	publisher string
	kind      string
	priority  string
	eventIDs  []int64
	title     string
	body      string
	extra     string // JSON plugin.NotificationExtra
	attempts  int
	created   int64
}

func (e *Engine) deliverDue(ctx context.Context) error {
	rows, err := e.db.R.QueryContext(ctx, `SELECT id, rule_id, publisher_id, kind, priority, event_ids, title, body, extra, attempts, created_at
		FROM notifications WHERE status = ? AND deliver_after <= ? ORDER BY deliver_after LIMIT 50`, NStatusPending, e.nowFn().UnixMilli())
	if err != nil {
		return err
	}
	var list []pending
	for rows.Next() {
		var (
			p   pending
			ids string
		)
		if err := rows.Scan(&p.id, &p.ruleID, &p.publisher, &p.kind, &p.priority, &ids, &p.title, &p.body, &p.extra, &p.attempts, &p.created); err != nil {
			rows.Close()
			return err
		}
		_ = json.Unmarshal([]byte(ids), &p.eventIDs)
		list = append(list, p)
	}
	rows.Close()
	for _, p := range list {
		if ctx.Err() != nil {
			return nil
		}
		e.deliver(ctx, p)
	}
	return nil
}

// eventLinks builds deep links (empty without a public URL).
func eventLinks(base string, ev events.Event) (string, string) {
	if base == "" {
		return "", ""
	}
	link := fmt.Sprintf("%s/events/%d", base, ev.ID)
	dev := ""
	if ev.DeviceID > 0 {
		dev = fmt.Sprintf("%s/devices/%d", base, ev.DeviceID)
	}
	return link, dev
}

// BuildNotification assembles the publisher payload for events.
func (e *Engine) BuildNotification(ctx context.Context, id int64, kind, priority, ruleName string, ruleID int64, evs []events.Event, title, body string, created time.Time) *plugin.Notification {
	base := e.settings.System().PublicURL
	n := &plugin.Notification{ID: id, Kind: kind, Priority: plugin.Priority(priority), RuleID: ruleID, RuleName: ruleName,
		Title: title, Body: body, CreatedAt: created}
	if base != "" {
		n.Link = base + "/events"
		if kind == plugin.NotifyReport {
			n.Link = base + "/reports"
		}
	}
	for _, ev := range evs {
		link, devLink := eventLinks(base, ev)
		v := plugin.EventView{ID: ev.ID, Type: ev.Type, Label: ev.Label, Category: ev.Category, Severity: ev.Severity, Title: ev.Title,
			Message: ev.Message, At: ev.TS, DeviceID: ev.DeviceID, DeviceName: ev.DeviceName, Link: link, DeviceLink: devLink,
			Payload: ev.Payload, Escalated: kind == plugin.NotifyEscalation, Acknowledged: ev.AckedAt != nil}
		if ip, ok := ev.Payload["device_ip"].(string); ok {
			v.DeviceIP = ip
		}
		if mac, ok := ev.Payload["device_mac"].(string); ok {
			v.DeviceMAC = mac
		}
		n.Events = append(n.Events, v)
	}
	if n.Title == "" {
		switch {
		case len(evs) == 1 && kind == plugin.NotifyEscalation:
			n.Title = "Eskalation: " + evs[0].Title
		case len(evs) == 1:
			n.Title = evs[0].Title
		case kind == plugin.NotifyEscalation:
			n.Title = fmt.Sprintf("Eskalation: %d nicht quittierte Ereignisse", len(evs))
		default:
			n.Title = fmt.Sprintf("%d Ereignisse", len(evs))
			if ruleName != "" {
				n.Title += " – " + ruleName
			}
		}
	}
	return n
}

func (e *Engine) deliver(ctx context.Context, p pending) {
	res, err := e.db.W.ExecContext(ctx, "UPDATE notifications SET status = ?, attempts = attempts + 1 WHERE id = ? AND status = ?",
		NStatusSending, p.id, NStatusPending)
	if err != nil {
		e.log.Error("notification claim", "err", err)
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return
	}
	evs, err := e.events.ByIDs(ctx, p.eventIDs)
	if err != nil {
		e.finish(ctx, p, err)
		return
	}
	if p.kind == plugin.NotifyEscalation {
		var open []events.Event
		for _, ev := range evs {
			if ev.AckedAt == nil {
				open = append(open, ev)
			}
		}
		if len(open) == 0 {
			_, _ = e.db.W.ExecContext(ctx, "UPDATE notifications SET status = ?, error = ? WHERE id = ?", NStatusSkipped, "inzwischen quittiert", p.id)
			return
		}
		evs = open
	}
	if (p.kind == plugin.NotifyEvent || p.kind == plugin.NotifyEscalation) && len(evs) == 0 {
		_, _ = e.db.W.ExecContext(ctx, "UPDATE notifications SET status = ?, error = ? WHERE id = ?", NStatusSkipped, "Ereignisse nicht mehr vorhanden", p.id)
		return
	}
	var ruleName string
	if p.ruleID.Valid {
		if r, err := e.Get(ctx, p.ruleID.Int64); err == nil {
			ruleName = r.Name
		}
	}
	n := e.BuildNotification(ctx, p.id, p.kind, p.priority, ruleName, p.ruleID.Int64, evs, p.title, p.body, time.UnixMilli(p.created))
	if p.extra != "" && p.extra != "{}" {
		var x plugin.NotificationExtra
		if err := json.Unmarshal([]byte(p.extra), &x); err != nil {
			e.log.Warn("Zusatzinhalt der Benachrichtigung unlesbar", "notification", p.id, "err", err)
		} else {
			n.HTML, n.Attachments = x.HTML, x.Attachments
		}
	}
	err = e.pub.Publish(ctx, p.publisher, n)
	e.finish(ctx, p, err)
}

func (e *Engine) finish(ctx context.Context, p pending, sendErr error) {
	now := e.nowFn()
	var err error
	switch {
	case sendErr == nil:
		_, err = e.db.W.ExecContext(ctx, "UPDATE notifications SET status = ?, sent_at = ?, error = '' WHERE id = ?", NStatusSent, now.UnixMilli(), p.id)
		e.bus.Publish(bus.TopicNotification, "sent", map[string]any{"id": p.id, "publisher": p.publisher})
	case p.attempts+1 >= maxAttempts:
		_, err = e.db.W.ExecContext(ctx, "UPDATE notifications SET status = ?, error = ? WHERE id = ?", NStatusFailed, sendErr.Error(), p.id)
		e.log.Warn("Benachrichtigung endgültig fehlgeschlagen", "id", p.id, "publisher", p.publisher, "err", sendErr)
		e.bus.Publish(bus.TopicNotification, "failed", map[string]any{"id": p.id, "publisher": p.publisher, "error": sendErr.Error()})
	default:
		backoff := time.Duration(30<<uint(p.attempts)) * time.Second
		// publishers may ask for a minimum delay (e.g. Telegram's retry_after on HTTP 429)
		var ra interface{ RetryAfter() time.Duration }
		if errors.As(sendErr, &ra) && ra.RetryAfter() > backoff {
			backoff = ra.RetryAfter()
		}
		_, err = e.db.W.ExecContext(ctx, "UPDATE notifications SET status = ?, error = ?, deliver_after = ? WHERE id = ?",
			NStatusPending, sendErr.Error(), now.Add(backoff).UnixMilli(), p.id)
		e.log.Warn("Benachrichtigung fehlgeschlagen, neuer Versuch geplant", "id", p.id, "publisher", p.publisher, "in", backoff.String(), "err", sendErr)
	}
	if err != nil {
		e.log.Error("notification status", "id", p.id, "err", err)
	}
}

func (e *Engine) checkEscalations(ctx context.Context) error {
	rows, err := e.db.R.QueryContext(ctx, `SELECT x.id, x.event_id, x.rule_id, x.action_index FROM escalations x
		WHERE x.done_at IS NULL AND x.due_at <= ?`, e.nowFn().UnixMilli())
	if err != nil {
		return err
	}
	type esc struct {
		id, event, rule int64
		idx             int
	}
	var list []esc
	for rows.Next() {
		var x esc
		if err := rows.Scan(&x.id, &x.event, &x.rule, &x.idx); err != nil {
			rows.Close()
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	now := e.nowFn()
	for _, x := range list {
		acked, err := e.events.IsAcked(ctx, x.event)
		if err == nil && !acked {
			r, rerr := e.Get(ctx, x.rule)
			if rerr == nil && x.idx < len(r.Actions) {
				a := r.Actions[x.idx]
				pub := a.EscalatePublisher
				if pub == "" {
					pub = a.Publisher
				}
				prio := a.EscalatePriority
				if prio == "" {
					prio = string(plugin.PrioUrgent)
				}
				res, err := e.db.W.ExecContext(ctx, `INSERT INTO notifications(rule_id, action_index, publisher_id, kind, priority, status,
					group_key, event_ids, deliver_after, created_at) VALUES (?,?,?,?,?,?,?,?,?,?)`, x.rule, x.idx, pub, plugin.NotifyEscalation,
					prio, NStatusPending, fmt.Sprintf("esc:%d", x.id), db.JSON([]int64{x.event}), now.UnixMilli(), now.UnixMilli())
				if err != nil {
					return err
				}
				nid, _ := res.LastInsertId()
				e.bus.Publish(bus.TopicNotification, "created", map[string]any{"id": nid, "publisher": pub, "rule": x.rule,
					"event": x.event, "deliverAt": now.Format(time.RFC3339), "escalation": true})
			}
		}
		if _, err := e.db.W.ExecContext(ctx, "UPDATE escalations SET done_at = ? WHERE id = ?", now.UnixMilli(), x.id); err != nil {
			return err
		}
	}
	if len(list) > 0 {
		e.kickDispatcher()
	}
	return nil
}

// ---------------------------------------------------------------- simulation

// SimInput is a synthetic event for the rule test.
type SimInput struct {
	Type     string         `json:"type"`
	Severity string         `json:"severity,omitempty"`
	DeviceID int64          `json:"deviceId,omitempty"`
	Title    string         `json:"title,omitempty"`
	Message  string         `json:"message,omitempty"`
	Payload  map[string]any `json:"payload,omitempty"`
	Deliver  bool           `json:"deliver,omitempty"` // actually send a test message
}

// SimResult explains the outcome of a simulation.
type SimResult struct {
	Matched     bool                 `json:"matched"`
	Conditions  []CondResult         `json:"conditions"`
	Actions     []ActionPlan         `json:"actions"`
	Preview     *plugin.Notification `json:"preview"`
	PreviewText string               `json:"previewText"`
	Delivery    map[string]string    `json:"delivery,omitempty"` // publisher -> "ok" | error
	Note        string               `json:"note,omitempty"`
}

// Simulate evaluates a (possibly unsaved) rule against a synthetic event.
func (e *Engine) Simulate(ctx context.Context, r *Rule, in SimInput) (*SimResult, error) {
	if err := r.Validate(e.publisherExists); err != nil {
		return nil, err
	}
	spec, ok := plugin.LookupEvent(in.Type)
	if !ok {
		return nil, fmt.Errorf("unbekannter Event-Typ %q", in.Type)
	}
	sev := plugin.Severity(in.Severity)
	if sev == "" {
		sev = spec.DefaultSeverity
	}
	if !sev.Valid() {
		return nil, fmt.Errorf("ungültiger Schweregrad %q", in.Severity)
	}
	payload := map[string]any{}
	for k, v := range in.Payload {
		payload[k] = v
	}
	ev := events.Event{Type: in.Type, Label: spec.Label, Category: spec.Category, Severity: sev, DeviceID: in.DeviceID, PluginID: "simulation",
		Title: in.Title, Message: in.Message, Payload: payload, TS: e.nowFn()}
	if ev.Title == "" {
		ev.Title = spec.Label + " (Simulation)"
	}
	if in.DeviceID > 0 {
		name, ip, mac, state, err := e.inv.EventDevice(ctx, in.DeviceID)
		if err != nil {
			return nil, fmt.Errorf("Gerät %d: %w", in.DeviceID, err)
		}
		ev.DeviceName = name
		payload["device_name"], payload["device_ip"], payload["device_mac"], payload["device_state"] = name, ip, mac, state
	}
	dev, err := e.device(ctx, in.DeviceID)
	if err != nil {
		return nil, err
	}
	res := &SimResult{Actions: []ActionPlan{}}
	res.Matched, res.Conditions = e.match(ctx, r.Conditions, toInput(ev), dev)
	if res.Conditions == nil {
		res.Conditions = []CondResult{}
	}
	if dev != nil && dev.State == "ignored" {
		res.Matched = false
		res.Note = "Das Gerät ist ignoriert – für ignorierte Geräte werden nie Benachrichtigungen verschickt."
	}
	if !r.Enabled {
		res.Note = strings.TrimSpace(res.Note + " Die Regel ist deaktiviert und würde im Betrieb nicht ausgewertet.")
	}
	res.Preview = e.BuildNotification(ctx, 0, plugin.NotifyEvent, string(plugin.PrioNormal), r.Name, r.ID, []events.Event{ev}, "", "", e.nowFn())
	res.PreviewText = res.Preview.PlainText()
	if !res.Matched {
		return res, nil
	}
	for i, a := range r.Actions {
		plan, err := e.plan(ctx, r, i, a, toInput(ev), true)
		if err != nil {
			return nil, err
		}
		res.Actions = append(res.Actions, *plan)
	}
	if in.Deliver {
		res.Delivery = map[string]string{}
		for _, a := range r.Actions {
			n := e.BuildNotification(ctx, 0, plugin.NotifyTest, a.Priority, r.Name, r.ID, []events.Event{ev}, "[Test] "+res.Preview.Title, "", e.nowFn())
			if err := e.pub.Publish(ctx, a.Publisher, n); err != nil {
				res.Delivery[a.Publisher] = err.Error()
			} else {
				res.Delivery[a.Publisher] = "ok"
			}
		}
	}
	return res, nil
}

// ---------------------------------------------------------------- history

// NotificationView is a notification for the API.
type NotificationView struct {
	ID           int64      `json:"id"`
	RuleID       int64      `json:"ruleId,omitempty"`
	RuleName     string     `json:"ruleName,omitempty"`
	Publisher    string     `json:"publisher"`
	Kind         string     `json:"kind"`
	Priority     string     `json:"priority"`
	Status       string     `json:"status"`
	EventIDs     []int64    `json:"eventIds"`
	Title        string     `json:"title,omitempty"`
	DeliverAfter time.Time  `json:"deliverAfter"`
	CreatedAt    time.Time  `json:"createdAt"`
	SentAt       *time.Time `json:"sentAt,omitempty"`
	Attempts     int        `json:"attempts"`
	Error        string     `json:"error,omitempty"`
}

// Notifications lists notifications (newest first).
// NotificationFilter selects notifications; zero values do not restrict.
type NotificationFilter struct {
	Status    string
	Publisher string
	EventID   int64
	RuleID    int64
	Limit     int
	Offset    int
}

func (e *Engine) Notifications(ctx context.Context, f NotificationFilter) ([]NotificationView, int, error) {
	var conds []string
	var args []any
	if f.Status != "" {
		conds = append(conds, "n.status = ?")
		args = append(args, f.Status)
	}
	if f.Publisher != "" {
		conds = append(conds, "n.publisher_id = ?")
		args = append(args, f.Publisher)
	}
	if f.EventID > 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM json_each(n.event_ids) j WHERE j.value = ?)")
		args = append(args, f.EventID)
	}
	if f.RuleID > 0 {
		conds = append(conds, "n.rule_id = ?")
		args = append(args, f.RuleID)
	}
	limit, offset := f.Limit, f.Offset
	where := "1=1"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}
	var total int
	if err := e.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications n WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := e.db.R.QueryContext(ctx, `SELECT n.id, IFNULL(n.rule_id, 0), IFNULL(r.name, ''), n.publisher_id, n.kind, n.priority, n.status,
		n.event_ids, n.title, n.deliver_after, n.created_at, n.sent_at, n.attempts, n.error
		FROM notifications n LEFT JOIN rules r ON r.id = n.rule_id WHERE `+where+` ORDER BY n.id DESC LIMIT ? OFFSET ?`, append(args, limit, offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []NotificationView{}
	for rows.Next() {
		var (
			v            NotificationView
			ids          string
			deliver, cre int64
			sent         sql.NullInt64
		)
		if err := rows.Scan(&v.ID, &v.RuleID, &v.RuleName, &v.Publisher, &v.Kind, &v.Priority, &v.Status, &ids, &v.Title, &deliver, &cre,
			&sent, &v.Attempts, &v.Error); err != nil {
			return nil, 0, err
		}
		_ = json.Unmarshal([]byte(ids), &v.EventIDs)
		if v.EventIDs == nil {
			v.EventIDs = []int64{}
		}
		sort.Slice(v.EventIDs, func(i, j int) bool { return v.EventIDs[i] < v.EventIDs[j] })
		v.DeliverAfter, v.CreatedAt, v.SentAt = db.Time(deliver), db.Time(cre), db.NullTime(sent)
		out = append(out, v)
	}
	return out, total, rows.Err()
}
