// Package events stores events (the history of what happened) and hands new events to
// the rule engine. Events are never deleted by retention.
package events

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"netscope/internal/bus"
	"netscope/internal/db"
	"netscope/internal/plugin"
)

// Event is the stored form of an event.
type Event struct {
	ID         int64           `json:"id"`
	TS         time.Time       `json:"ts"`
	Type       string          `json:"type"`
	Label      string          `json:"label"`
	Category   string          `json:"category"`
	Severity   plugin.Severity `json:"severity"`
	DeviceID   int64           `json:"deviceId,omitempty"`
	DeviceName string          `json:"deviceName,omitempty"`
	PluginID   string          `json:"pluginId"`
	RunID      int64           `json:"runId,omitempty"`
	Title      string          `json:"title"`
	Message    string          `json:"message"`
	Payload    map[string]any  `json:"payload"`
	DedupKey   string          `json:"dedupKey,omitempty"`
	AckedAt    *time.Time      `json:"ackedAt,omitempty"`
	AckedBy    string          `json:"ackedBy,omitempty"`
	AckNote    string          `json:"ackNote,omitempty"`
	// SiteID and Site name the site that raised the event (central instance; 0 and "" =
	// this instance).
	SiteID int64  `json:"siteId,omitempty"`
	Site   string `json:"site,omitempty"`
}

// DeviceLookup resolves device details for event payloads.
type DeviceLookup interface {
	EventDevice(ctx context.Context, id int64) (name, ip, mac, state string, err error)
}

// Store persists events.
type Store struct {
	db     *db.DB
	bus    *bus.Bus
	lookup DeviceLookup

	mu       sync.RWMutex
	handlers []func(Event)
	forward  func(ctx context.Context, ev Event)
}

// SetForwarder registers the delivery of new events to a central instance (site role);
// fn reports its own errors.
func (s *Store) SetForwarder(fn func(ctx context.Context, ev Event)) {
	s.mu.Lock()
	s.forward = fn
	s.mu.Unlock()
}

// New creates the store.
func New(d *db.DB, b *bus.Bus, lookup DeviceLookup) *Store {
	return &Store{db: d, bus: b, lookup: lookup}
}

// OnEvent registers a consumer of new events (the rule engine). It is called
// synchronously after the insert; consumers must queue work themselves.
func (s *Store) OnEvent(fn func(Event)) {
	s.mu.Lock()
	s.handlers = append(s.handlers, fn)
	s.mu.Unlock()
}

// Emit stores an event from a plugin. It returns 0 if the event was suppressed by its
// dedup key.
func (s *Store) Emit(ctx context.Context, pluginID string, in plugin.Event) (int64, error) {
	spec, ok := plugin.LookupEvent(in.Type)
	if !ok {
		return 0, fmt.Errorf("unbekannter Event-Typ %q", in.Type)
	}
	sev := in.Severity
	if sev == "" {
		sev = spec.DefaultSeverity
	}
	if !sev.Valid() {
		return 0, fmt.Errorf("ungültiger Schweregrad %q", sev)
	}
	at := in.At
	if at.IsZero() {
		at = time.Now()
	}
	if in.DeviceID > 0 {
		// events about devices of a site come from the site itself (see Import)
		var site sql.NullInt64
		if err := s.db.R.QueryRowContext(ctx, "SELECT site_id FROM devices WHERE id = ?", in.DeviceID).Scan(&site); err == nil && site.Valid {
			return 0, nil
		}
	}
	if in.DedupKey != "" {
		window := in.DedupWindow
		if window <= 0 {
			window = 24 * time.Hour
		}
		var n int
		if err := s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM events WHERE dedup_key = ? AND ts >= ?",
			in.DedupKey, at.Add(-window).UnixMilli()).Scan(&n); err != nil {
			return 0, err
		}
		if n > 0 {
			return 0, nil
		}
	}
	payload := map[string]any{}
	for k, v := range in.Payload {
		payload[k] = v
	}
	if in.DeviceID > 0 && s.lookup != nil {
		name, ip, mac, state, err := s.lookup.EventDevice(ctx, in.DeviceID)
		if err == nil {
			payload["device_name"], payload["device_ip"], payload["device_mac"], payload["device_state"] = name, ip, mac, state
		} else if errors.Is(err, db.ErrNotFound) {
			in.DeviceID = 0
		}
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = spec.Label
	}
	ev := Event{TS: at, Type: in.Type, Label: spec.Label, Category: spec.Category, Severity: sev, DeviceID: in.DeviceID,
		PluginID: pluginID, RunID: in.RunID, Title: title, Message: in.Message, Payload: payload, DedupKey: in.DedupKey}
	return s.insert(ctx, &ev)
}

// insert stores an event and hands it to the bus, the rule engine and the delivery to a
// central instance.
func (s *Store) insert(ctx context.Context, ev *Event) (int64, error) {
	pb, err := json.Marshal(ev.Payload)
	if err != nil {
		return 0, err
	}
	var dev, run, site any
	if ev.DeviceID > 0 {
		dev = ev.DeviceID
	}
	if ev.RunID > 0 {
		run = ev.RunID
	}
	if ev.SiteID > 0 {
		site = ev.SiteID
	}
	res, err := s.db.W.ExecContext(ctx, `INSERT INTO events(ts, type, category, severity, device_id, plugin_id, run_id, title, message, payload, dedup_key, site_id)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, ev.TS.UnixMilli(), ev.Type, ev.Category, string(ev.Severity), dev, ev.PluginID, run, ev.Title, ev.Message,
		string(pb), ev.DedupKey, site)
	if err != nil {
		return 0, err
	}
	ev.ID, _ = res.LastInsertId()
	if n, ok := ev.Payload["device_name"].(string); ok {
		ev.DeviceName = n
	}
	if s.bus != nil {
		s.bus.Publish(bus.TopicEvent, "created", *ev)
	}
	s.mu.RLock()
	hs := append([]func(Event){}, s.handlers...)
	fwd := s.forward
	s.mu.RUnlock()
	for _, h := range hs {
		h(*ev)
	}
	if fwd != nil && ev.SiteID == 0 {
		fwd(ctx, *ev)
	}
	return ev.ID, nil
}

// Imported is an event raised at a site.
type Imported struct {
	SiteID   int64
	Site     string
	DeviceID int64 // device here (0 = none or not known here)
	At       time.Time
	Type     string
	Severity plugin.Severity
	PluginID string
	Title    string
	Message  string
	Payload  map[string]any
	RemoteID int64 // event id at the site
}

// Import stores an event delivered by a site. The site has already deduplicated it; the
// device fields of the payload are refreshed from the device here when it is known.
func (s *Store) Import(ctx context.Context, in Imported) (int64, error) {
	spec, ok := plugin.LookupEvent(in.Type)
	if !ok {
		return 0, fmt.Errorf("unbekannter Event-Typ %q", in.Type)
	}
	sev := in.Severity
	if !sev.Valid() {
		sev = spec.DefaultSeverity
	}
	payload := map[string]any{}
	for k, v := range in.Payload {
		payload[k] = v
	}
	if in.DeviceID > 0 && s.lookup != nil {
		name, ip, mac, state, err := s.lookup.EventDevice(ctx, in.DeviceID)
		switch {
		case err == nil:
			payload["device_name"], payload["device_ip"], payload["device_mac"], payload["device_state"] = name, ip, mac, state
		case errors.Is(err, db.ErrNotFound):
			in.DeviceID = 0
		}
	}
	payload["site"] = in.Site
	if in.RemoteID > 0 {
		payload["site_event_id"] = in.RemoteID
	}
	at := in.At
	if at.IsZero() {
		at = time.Now()
	}
	title := strings.TrimSpace(in.Title)
	if title == "" {
		title = spec.Label
	}
	ev := Event{TS: at, Type: in.Type, Label: spec.Label, Category: spec.Category, Severity: sev, DeviceID: in.DeviceID,
		PluginID: in.PluginID, Title: title, Message: in.Message, Payload: payload, SiteID: in.SiteID, Site: in.Site}
	return s.insert(ctx, &ev)
}

// Emitter adapts the store to plugin.EventEmitter for one plugin.
type Emitter struct {
	S        *Store
	PluginID string
}

// Emit implements plugin.EventEmitter.
func (e Emitter) Emit(ctx context.Context, ev plugin.Event) (int64, error) {
	return e.S.Emit(ctx, e.PluginID, ev)
}

// Filter selects events.
type Filter struct {
	Types       []string // exact types or patterns like "port.*"
	Categories  []string
	MinSeverity plugin.Severity
	DeviceID    int64
	RunID       int64
	Acked       *bool
	From, To    time.Time
	Text        string
	Limit       int
	Offset      int
	IDs         []int64
	// Site restricts to the events of one site (0 = this instance, nil = all).
	Site *int64
}

func (f Filter) where() (string, []any) {
	var conds []string
	var args []any
	if len(f.Types) > 0 {
		var alts []string
		for _, t := range f.Types {
			if strings.HasSuffix(t, ".*") {
				alts = append(alts, "e.type LIKE ?")
				args = append(args, strings.TrimSuffix(t, "*")+"%")
			} else {
				alts = append(alts, "e.type = ?")
				args = append(args, t)
			}
		}
		conds = append(conds, "("+strings.Join(alts, " OR ")+")")
	}
	if len(f.Categories) > 0 {
		conds = append(conds, "e.category IN ("+db.Placeholders(len(f.Categories))+")")
		args = append(args, db.StringArgs(f.Categories)...)
	}
	if r := f.MinSeverity.Rank(); r > 0 {
		var sevs []string
		for _, s := range plugin.Severities[r:] {
			sevs = append(sevs, string(s))
		}
		conds = append(conds, "e.severity IN ("+db.Placeholders(len(sevs))+")")
		args = append(args, db.StringArgs(sevs)...)
	}
	if f.DeviceID > 0 {
		conds = append(conds, "e.device_id = ?")
		args = append(args, f.DeviceID)
	}
	if f.RunID > 0 {
		conds = append(conds, "e.run_id = ?")
		args = append(args, f.RunID)
	}
	if f.Acked != nil {
		if *f.Acked {
			conds = append(conds, "e.acked_at IS NOT NULL")
		} else {
			conds = append(conds, "e.acked_at IS NULL")
		}
	}
	if !f.From.IsZero() {
		conds = append(conds, "e.ts >= ?")
		args = append(args, f.From.UnixMilli())
	}
	if !f.To.IsZero() {
		conds = append(conds, "e.ts <= ?")
		args = append(args, f.To.UnixMilli())
	}
	if f.Text != "" {
		p := "%" + strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(f.Text) + "%"
		conds = append(conds, `(e.title LIKE ? ESCAPE '\' OR e.message LIKE ? ESCAPE '\' OR e.payload LIKE ? ESCAPE '\')`)
		args = append(args, p, p, p)
	}
	if len(f.IDs) > 0 {
		conds = append(conds, "e.id IN ("+db.Placeholders(len(f.IDs))+")")
		args = append(args, db.Int64Args(f.IDs)...)
	}
	if f.Site != nil {
		if *f.Site == 0 {
			conds = append(conds, "e.site_id IS NULL")
		} else {
			conds = append(conds, "e.site_id = ?")
			args = append(args, *f.Site)
		}
	}
	if len(conds) == 0 {
		return "1=1", nil
	}
	return strings.Join(conds, " AND "), args
}

const selectEvent = `SELECT e.id, e.ts, e.type, e.category, e.severity, IFNULL(e.device_id, 0), e.plugin_id, IFNULL(e.run_id, 0), e.title,
	e.message, e.payload, e.dedup_key, e.acked_at, e.acked_by, e.ack_note,
	COALESCE(NULLIF(d.display_name, ''), NULLIF(d.hostname, ''), NULLIF(d.primary_ip, ''), json_extract(e.payload, '$.device_name'), ''),
	IFNULL(e.site_id, 0), IFNULL(st.name, '')
	FROM events e LEFT JOIN devices d ON d.id = e.device_id LEFT JOIN sites st ON st.id = e.site_id`

func scanEvent(rows *sql.Rows) (Event, error) {
	var (
		e       Event
		ts      int64
		payload string
		acked   sql.NullInt64
		sev     string
	)
	if err := rows.Scan(&e.ID, &ts, &e.Type, &e.Category, &sev, &e.DeviceID, &e.PluginID, &e.RunID, &e.Title, &e.Message,
		&payload, &e.DedupKey, &acked, &e.AckedBy, &e.AckNote, &e.DeviceName, &e.SiteID, &e.Site); err != nil {
		return e, err
	}
	e.TS, e.Severity, e.AckedAt = db.Time(ts), plugin.Severity(sev), db.NullTime(acked)
	e.Payload = map[string]any{}
	_ = json.Unmarshal([]byte(payload), &e.Payload)
	if spec, ok := plugin.LookupEvent(e.Type); ok {
		e.Label = spec.Label
	}
	return e, nil
}

// List returns events matching the filter (newest first) and the total count.
func (s *Store) List(ctx context.Context, f Filter) ([]Event, int, error) {
	where, args := f.where()
	var total int
	if err := s.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM events e WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	rows, err := s.db.R.QueryContext(ctx, selectEvent+" WHERE "+where+" ORDER BY e.ts DESC, e.id DESC LIMIT ? OFFSET ?",
		append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Event{}
	for rows.Next() {
		e, err := scanEvent(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

// Get returns one event.
func (s *Store) Get(ctx context.Context, id int64) (*Event, error) {
	rows, err := s.db.R.QueryContext(ctx, selectEvent+" WHERE e.id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, db.ErrNotFound
	}
	e, err := scanEvent(rows)
	return &e, err
}

// ByIDs returns events by id (in id order).
func (s *Store) ByIDs(ctx context.Context, ids []int64) ([]Event, error) {
	if len(ids) == 0 {
		return []Event{}, nil
	}
	list, _, err := s.List(ctx, Filter{IDs: ids, Limit: 1000})
	if err != nil {
		return nil, err
	}
	// oldest first for notifications
	for i, j := 0, len(list)-1; i < j; i, j = i+1, j-1 {
		list[i], list[j] = list[j], list[i]
	}
	return list, nil
}

// Ack acknowledges events (already acknowledged ones are left untouched).
func (s *Store) Ack(ctx context.Context, ids []int64, by, note string) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res, err := s.db.W.ExecContext(ctx, "UPDATE events SET acked_at = ?, acked_by = ?, ack_note = ? WHERE acked_at IS NULL AND id IN ("+db.Placeholders(len(ids))+")",
		append([]any{db.Now(), by, note}, db.Int64Args(ids)...)...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 && s.bus != nil {
		s.bus.Publish(bus.TopicEvent, "acked", map[string]any{"ids": ids})
	}
	return int(n), nil
}

// AckFilter acknowledges all open events matching a filter.
func (s *Store) AckFilter(ctx context.Context, f Filter, by, note string) (int, error) {
	no := false
	f.Acked = &no
	where, args := f.where()
	res, err := s.db.W.ExecContext(ctx, "UPDATE events SET acked_at = ?, acked_by = ?, ack_note = ? WHERE id IN (SELECT e.id FROM events e WHERE "+where+")",
		append([]any{db.Now(), by, note}, args...)...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if n > 0 && s.bus != nil {
		s.bus.Publish(bus.TopicEvent, "acked", map[string]any{"count": n})
	}
	return int(n), nil
}

// OpenCounts returns the number of unacknowledged events per severity.
func (s *Store) OpenCounts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.R.QueryContext(ctx, "SELECT severity, COUNT(*) FROM events WHERE acked_at IS NULL GROUP BY severity")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for _, sv := range plugin.Severities {
		out[string(sv)] = 0
	}
	for rows.Next() {
		var (
			sev string
			n   int
		)
		if err := rows.Scan(&sev, &n); err != nil {
			return nil, err
		}
		out[sev] = n
	}
	return out, rows.Err()
}

// IsAcked reports whether an event has been acknowledged.
func (s *Store) IsAcked(ctx context.Context, id int64) (bool, error) {
	var acked sql.NullInt64
	err := s.db.R.QueryRowContext(ctx, "SELECT acked_at FROM events WHERE id = ?", id).Scan(&acked)
	if err != nil {
		return false, db.NotFound(err)
	}
	return acked.Valid, nil
}
