// Package rules is the notification rule engine. Events never go to publishers
// directly: every new event is evaluated against the UI-managed rules; matching rule
// actions create (or extend) pending notifications which a dispatcher delivers through
// publisher plugins – bundled per run, collected over N minutes, delayed during quiet
// hours, throttled, and escalated when not acknowledged in time.
package rules

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// PayloadCond compares an event payload field.
type PayloadCond struct {
	Field string `json:"field"`
	Op    string `json:"op"` // = != > >= < <= contains
	Value string `json:"value"`
}

// TimeWindow limits a rule to days/times (local time zone). From > To wraps midnight.
type TimeWindow struct {
	Days []int  `json:"days,omitempty"` // 0 = Sunday … 6 = Saturday; empty = every day
	From string `json:"from"`           // HH:MM
	To   string `json:"to"`             // HH:MM
}

// Conditions of a rule; empty fields do not restrict.
type Conditions struct {
	EventTypes   []string      `json:"eventTypes"`
	MinSeverity  string        `json:"minSeverity,omitempty"`
	Tags         []string      `json:"tags,omitempty"`
	Groups       []int64       `json:"groups,omitempty"`
	Subnets      []string      `json:"subnets,omitempty"`
	DeviceQuery  string        `json:"deviceQuery,omitempty"`
	OnlyUnknown  bool          `json:"onlyUnknown,omitempty"` // device not marked as known
	DeviceStates []string      `json:"deviceStates,omitempty"`
	Payload      []PayloadCond `json:"payload,omitempty"`
	TimeWindow   *TimeWindow   `json:"timeWindow,omitempty"`
	// Sites limits the rule to events of these NetScope sites (central instance);
	// 0 is this instance.
	Sites []int64 `json:"sites,omitempty"`
}

// QuietHours delay or drop notifications in a time range.
type QuietHours struct {
	From        string `json:"from"`
	To          string `json:"to"`
	Behavior    string `json:"behavior"` // delay | drop
	AllowUrgent bool   `json:"allowUrgent"`
}

// Action sends matching events through a publisher.
type Action struct {
	Publisher            string      `json:"publisher"`
	Priority             string      `json:"priority"`
	Mode                 string      `json:"mode"` // immediate | batch
	BatchMinutes         int         `json:"batchMinutes,omitempty"`
	Throttle             string      `json:"throttle,omitempty"` // duration, e.g. "24h"
	QuietHours           *QuietHours `json:"quietHours,omitempty"`
	EscalateAfterMinutes int         `json:"escalateAfterMinutes,omitempty"`
	EscalatePublisher    string      `json:"escalatePublisher,omitempty"`
	EscalatePriority     string      `json:"escalatePriority,omitempty"`
}

// Rule is a notification rule.
type Rule struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Enabled     bool       `json:"enabled"`
	SortOrder   int        `json:"sortOrder"`
	Stop        bool       `json:"stop"`
	Conditions  Conditions `json:"conditions"`
	Actions     []Action   `json:"actions"`
	Builtin     string     `json:"builtin,omitempty"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

func parseHM(s string) (int, error) {
	h, m, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		return 0, fmt.Errorf("Uhrzeit %q: HH:MM erwartet", s)
	}
	hh, err1 := strconv.Atoi(h)
	mm, err2 := strconv.Atoi(m)
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return 0, fmt.Errorf("Uhrzeit %q: HH:MM erwartet", s)
	}
	return hh*60 + mm, nil
}

// inRange reports whether minute-of-day m is in [from, to); from > to wraps midnight;
// from == to means the whole day.
func inRange(m, from, to int) bool {
	switch {
	case from == to:
		return true
	case from < to:
		return m >= from && m < to
	default:
		return m >= from || m < to
	}
}

var validOps = map[string]bool{"=": true, "!=": true, ">": true, ">=": true, "<": true, "<=": true, "contains": true}

// Validate checks and normalizes a rule. publisherExists is used to verify actions.
// Errors are returned as *plugin.ValidationError with JSON paths as field names
// (e.g. "conditions.payload.0.field", "actions.1.throttle") so forms can mark them.
func (r *Rule) Validate(publisherExists func(string) bool) error {
	var errs []plugin.FieldError
	add := func(field, format string, args ...any) {
		errs = append(errs, plugin.FieldError{Field: field, Message: fmt.Sprintf(format, args...)})
	}
	hm := func(field, value string) {
		if _, err := parseHM(value); err != nil {
			add(field, "%s", err.Error())
		}
	}
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		add("name", "Name erforderlich")
	}
	c := &r.Conditions
	for _, t := range c.EventTypes {
		if t == "*" || strings.HasSuffix(t, ".*") {
			continue
		}
		if _, ok := plugin.LookupEvent(t); !ok {
			add("conditions.eventTypes", "unbekannter Event-Typ %q", t)
		}
	}
	if c.MinSeverity != "" && !plugin.Severity(c.MinSeverity).Valid() {
		add("conditions.minSeverity", "ungültiger Schweregrad %q", c.MinSeverity)
	}
	for i, s := range c.Subnets {
		p, err := plugin.ParsePrefix(s)
		if err != nil {
			add(fmt.Sprintf("conditions.subnets.%d", i), "%s", err.Error())
			continue
		}
		c.Subnets[i] = p.String()
	}
	for _, id := range c.Sites {
		if id < 0 {
			add("conditions.sites", "ungültiger Standort %d", id)
		}
	}
	for _, st := range c.DeviceStates {
		if st != "known" && st != "unknown" && st != "ignored" {
			add("conditions.deviceStates", "ungültiger Gerätezustand %q", st)
		}
	}
	for i, pc := range c.Payload {
		if strings.TrimSpace(pc.Field) == "" {
			add(fmt.Sprintf("conditions.payload.%d.field", i), "Payload-Bedingung %d: Feld fehlt", i+1)
		}
		if !validOps[pc.Op] {
			add(fmt.Sprintf("conditions.payload.%d.op", i), "Payload-Bedingung %d: Operator %q unbekannt", i+1, pc.Op)
		}
	}
	if tw := c.TimeWindow; tw != nil {
		hm("conditions.timeWindow.from", tw.From)
		hm("conditions.timeWindow.to", tw.To)
		for _, d := range tw.Days {
			if d < 0 || d > 6 {
				add("conditions.timeWindow.days", "Wochentag %d ungültig (0–6)", d)
			}
		}
	}
	if len(r.Actions) == 0 {
		add("actions", "mindestens eine Aktion erforderlich")
	}
	for i := range r.Actions {
		a := &r.Actions[i]
		f := func(name string) string { return fmt.Sprintf("actions.%d.%s", i, name) }
		if a.Publisher == "" || (publisherExists != nil && !publisherExists(a.Publisher)) {
			add(f("publisher"), "Aktion %d: unbekannter Publisher %q", i+1, a.Publisher)
		}
		if a.Priority == "" {
			a.Priority = string(plugin.PrioNormal)
		}
		if !plugin.Priority(a.Priority).Valid() {
			add(f("priority"), "Aktion %d: ungültige Priorität %q", i+1, a.Priority)
		}
		switch a.Mode {
		case "", "immediate":
			a.Mode = "immediate"
			a.BatchMinutes = 0
		case "batch":
			if a.BatchMinutes < 1 || a.BatchMinutes > 1440 {
				add(f("batchMinutes"), "Aktion %d: Sammelzeitraum 1–1440 Minuten", i+1)
			}
		default:
			add(f("mode"), "Aktion %d: Modus immediate oder batch", i+1)
		}
		if a.Throttle != "" {
			d, err := time.ParseDuration(a.Throttle)
			if err != nil || d < time.Minute {
				add(f("throttle"), "Aktion %d: Drosselung als Dauer ≥ 1m (z. B. 24h)", i+1)
			}
		}
		if q := a.QuietHours; q != nil {
			hm(f("quietHours.from"), q.From)
			hm(f("quietHours.to"), q.To)
			if q.Behavior == "" {
				q.Behavior = "delay"
			}
			if q.Behavior != "delay" && q.Behavior != "drop" {
				add(f("quietHours.behavior"), "Aktion %d: Ruhezeit-Verhalten delay oder drop", i+1)
			}
		}
		if a.EscalateAfterMinutes < 0 || a.EscalateAfterMinutes > 10080 {
			add(f("escalateAfterMinutes"), "Aktion %d: Eskalation 0–10080 Minuten", i+1)
		}
		if a.EscalateAfterMinutes > 0 {
			if a.EscalatePublisher != "" && publisherExists != nil && !publisherExists(a.EscalatePublisher) {
				add(f("escalatePublisher"), "Aktion %d: unbekannter Eskalations-Publisher %q", i+1, a.EscalatePublisher)
			}
			if a.EscalatePriority != "" && !plugin.Priority(a.EscalatePriority).Valid() {
				add(f("escalatePriority"), "Aktion %d: ungültige Eskalations-Priorität", i+1)
			}
		}
	}
	if len(errs) > 0 {
		return &plugin.ValidationError{Errors: errs}
	}
	return nil
}

// ---------------------------------------------------------------- storage

const ruleSelect = "SELECT id, name, description, enabled, sort_order, stop, conditions, actions, builtin, created_at, updated_at FROM rules"

func scanRule(rows *sql.Rows) (*Rule, error) {
	var (
		r          Rule
		cond, acts string
		cre, upd   int64
	)
	if err := rows.Scan(&r.ID, &r.Name, &r.Description, &r.Enabled, &r.SortOrder, &r.Stop, &cond, &acts, &r.Builtin, &cre, &upd); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(cond), &r.Conditions); err != nil {
		return nil, fmt.Errorf("Regel %d: %w", r.ID, err)
	}
	if err := json.Unmarshal([]byte(acts), &r.Actions); err != nil {
		return nil, fmt.Errorf("Regel %d: %w", r.ID, err)
	}
	if r.Conditions.EventTypes == nil {
		r.Conditions.EventTypes = []string{}
	}
	r.CreatedAt, r.UpdatedAt = db.Time(cre), db.Time(upd)
	return &r, nil
}

// List returns all rules in evaluation order.
func (e *Engine) List(ctx context.Context) ([]*Rule, error) {
	rows, err := e.db.R.QueryContext(ctx, ruleSelect+" ORDER BY sort_order, id")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []*Rule{}
	for rows.Next() {
		r, err := scanRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Get returns one rule.
func (e *Engine) Get(ctx context.Context, id int64) (*Rule, error) {
	rows, err := e.db.R.QueryContext(ctx, ruleSelect+" WHERE id = ?", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	if !rows.Next() {
		return nil, db.ErrNotFound
	}
	return scanRule(rows)
}

// Save creates (ID 0) or updates a rule.
func (e *Engine) Save(ctx context.Context, r *Rule) error {
	if err := r.Validate(e.publisherExists); err != nil {
		return err
	}
	if r.Conditions.EventTypes == nil {
		r.Conditions.EventTypes = []string{}
	}
	cond, _ := json.Marshal(r.Conditions)
	acts, _ := json.Marshal(r.Actions)
	now := time.Now()
	if r.ID == 0 {
		res, err := e.db.W.ExecContext(ctx, `INSERT INTO rules(name, description, enabled, sort_order, stop, conditions, actions, builtin, created_at, updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?)`, r.Name, r.Description, db.Bool(r.Enabled), r.SortOrder, db.Bool(r.Stop), string(cond), string(acts),
			r.Builtin, now.UnixMilli(), now.UnixMilli())
		if err != nil {
			return err
		}
		r.ID, _ = res.LastInsertId()
		r.CreatedAt, r.UpdatedAt = now, now
		return nil
	}
	res, err := e.db.W.ExecContext(ctx, `UPDATE rules SET name = ?, description = ?, enabled = ?, sort_order = ?, stop = ?, conditions = ?,
		actions = ?, updated_at = ? WHERE id = ?`, r.Name, r.Description, db.Bool(r.Enabled), r.SortOrder, db.Bool(r.Stop), string(cond),
		string(acts), now.UnixMilli(), r.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	r.UpdatedAt = now
	return nil
}

// Delete removes a rule.
func (e *Engine) Delete(ctx context.Context, id int64) error {
	res, err := e.db.W.ExecContext(ctx, "DELETE FROM rules WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return nil
}

// Reorder sets the evaluation order: ids in the given order get sort orders 10, 20, …;
// rules not listed keep their relative order after them. All in one transaction.
func (e *Engine) Reorder(ctx context.Context, ids []int64) ([]*Rule, error) {
	current, err := e.List(ctx)
	if err != nil {
		return nil, err
	}
	known := make(map[int64]bool, len(current))
	for _, r := range current {
		known[r.ID] = true
	}
	seen := map[int64]bool{}
	order := make([]int64, 0, len(current))
	for _, id := range ids {
		if !known[id] {
			return nil, fmt.Errorf("Regel %d: %w", id, db.ErrNotFound)
		}
		if seen[id] {
			return nil, &plugin.ValidationError{Errors: []plugin.FieldError{{Field: "ids", Message: fmt.Sprintf("Regel %d doppelt", id)}}}
		}
		seen[id] = true
		order = append(order, id)
	}
	for _, r := range current {
		if !seen[r.ID] {
			order = append(order, r.ID)
		}
	}
	err = e.db.Tx(ctx, func(tx *sql.Tx) error {
		now := time.Now().UnixMilli()
		for i, id := range order {
			if _, err := tx.ExecContext(ctx, "UPDATE rules SET sort_order = ?, updated_at = ? WHERE id = ? AND sort_order <> ?",
				(i+1)*10, now, id, (i+1)*10); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return e.List(ctx)
}

// SeedDefaults creates the default rules once (the user may edit or delete them).
func (e *Engine) SeedDefaults(ctx context.Context) error {
	var seeded int
	if err := e.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM settings WHERE key = 'rules.seeded'").Scan(&seeded); err != nil {
		return err
	}
	if seeded > 0 {
		return nil
	}
	defaults := []Rule{
		{Name: "Neues unbekanntes Gerät → Telegram sofort", Builtin: "new-unknown-device", Enabled: true, SortOrder: 10,
			Description: "Meldet jedes neu entdeckte Gerät, das nicht als bekannt markiert ist, sofort.",
			Conditions:  Conditions{EventTypes: []string{plugin.EvDeviceNew}, OnlyUnknown: true},
			Actions:     []Action{{Publisher: "telegram", Priority: string(plugin.PrioHigh), Mode: "immediate"}}},
		{Name: "Neuer Port auf bekanntem Gerät → Telegram gesammelt", Builtin: "new-port-known-device", Enabled: true, SortOrder: 20,
			Description: "Sammelt neu geöffnete Ports auf bekannten Geräten und schickt sie stündlich gebündelt.",
			Conditions:  Conditions{EventTypes: []string{plugin.EvPortOpened}, DeviceStates: []string{"known"}},
			Actions:     []Action{{Publisher: "telegram", Priority: string(plugin.PrioNormal), Mode: "batch", BatchMinutes: 60}}},
		{Name: "CVE ≥ 9 → Telegram sofort", Builtin: "critical-cve", Enabled: true, SortOrder: 30,
			Description: "Kritische Schwachstellen (CVSS ab 9.0) werden sofort gemeldet.",
			Conditions:  Conditions{EventTypes: []string{plugin.EvCVENew}, Payload: []PayloadCond{{Field: "cvss", Op: ">=", Value: "9"}}},
			Actions:     []Action{{Publisher: "telegram", Priority: string(plugin.PrioUrgent), Mode: "immediate"}}},
		{Name: "Zertifikat < 14 Tage → täglich einmal", Builtin: "cert-expiry", Enabled: true, SortOrder: 40,
			Description: "Zertifikate, die in weniger als 14 Tagen ablaufen (oder abgelaufen sind), höchstens einmal täglich melden.",
			Conditions: Conditions{EventTypes: []string{plugin.EvCertExpiring, plugin.EvCertExpired},
				Payload: []PayloadCond{{Field: "days_left", Op: "<", Value: "14"}}},
			Actions: []Action{{Publisher: "telegram", Priority: string(plugin.PrioNormal), Mode: "immediate", Throttle: "24h"}}},
	}
	for i := range defaults {
		r := defaults[i]
		if err := r.Validate(nil); err != nil {
			return fmt.Errorf("Standardregel %q: %w", r.Name, err)
		}
		cond, _ := json.Marshal(r.Conditions)
		acts, _ := json.Marshal(r.Actions)
		now := db.Now()
		if _, err := e.db.W.ExecContext(ctx, `INSERT INTO rules(name, description, enabled, sort_order, stop, conditions, actions, builtin, created_at, updated_at)
			VALUES (?,?,?,?,0,?,?,?,?,?)`, r.Name, r.Description, db.Bool(r.Enabled), r.SortOrder, string(cond), string(acts), r.Builtin, now, now); err != nil {
			return err
		}
	}
	_, err := e.db.W.ExecContext(ctx, "INSERT INTO settings(key, value, updated_at) VALUES ('rules.seeded', 'true', ?)", db.Now())
	return err
}

// ---------------------------------------------------------------- matching

// DeviceContext is what conditions may look at.
type DeviceContext struct {
	ID    int64
	State string
	Tags  []string
	IPs   []string
}

// CondResult explains one condition (used by the simulation).
type CondResult struct {
	Condition string `json:"condition"`
	OK        bool   `json:"ok"`
	Detail    string `json:"detail"`
}

func payloadValue(p map[string]any, field string) (any, bool) {
	v, ok := p[field]
	return v, ok
}

func compare(v any, op, want string) bool {
	var s string
	switch x := v.(type) {
	case string:
		s = x
	case float64:
		s = strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		s = strconv.Itoa(x)
	case int64:
		s = strconv.FormatInt(x, 10)
	case bool:
		s = strconv.FormatBool(x)
	case nil:
		s = ""
	default:
		b, _ := json.Marshal(x)
		s = string(b)
	}
	if op == "contains" {
		return strings.Contains(strings.ToLower(s), strings.ToLower(want))
	}
	a, errA := strconv.ParseFloat(s, 64)
	b, errB := strconv.ParseFloat(want, 64)
	if errA == nil && errB == nil {
		switch op {
		case "=":
			return a == b
		case "!=":
			return a != b
		case ">":
			return a > b
		case ">=":
			return a >= b
		case "<":
			return a < b
		case "<=":
			return a <= b
		}
		return false
	}
	switch op {
	case "=":
		return strings.EqualFold(s, want)
	case "!=":
		return !strings.EqualFold(s, want)
	case ">":
		return s > want
	case ">=":
		return s >= want
	case "<":
		return s < want
	case "<=":
		return s <= want
	}
	return false
}

// EventInput is the part of an event the matcher needs.
type EventInput struct {
	ID       int64
	Type     string
	Severity plugin.Severity
	DeviceID int64
	RunID    int64
	Title    string
	Message  string
	Payload  map[string]any
	At       time.Time
	DedupKey string
	SiteID   int64 // 0 = this instance
}

// matchSites reports whether the event comes from one of the rule's sites (0 = this
// instance); applies is false when the rule has no site condition.
func (e *Engine) matchSites(c Conditions, ev EventInput) (ok, applies bool) {
	if len(c.Sites) == 0 {
		return true, false
	}
	for _, id := range c.Sites {
		if id == ev.SiteID {
			return true, true
		}
	}
	return false, true
}

// match evaluates the conditions; it returns all condition results (for explanations).
func (e *Engine) match(ctx context.Context, c Conditions, ev EventInput, dev *DeviceContext) (bool, []CondResult) {
	var res []CondResult
	all := true
	add := func(name string, ok bool, detail string) {
		res = append(res, CondResult{Condition: name, OK: ok, Detail: detail})
		if !ok {
			all = false
		}
	}
	if len(c.EventTypes) > 0 {
		ok := false
		for _, p := range c.EventTypes {
			if plugin.MatchEventType(p, ev.Type) {
				ok = true
			}
		}
		add("Event-Typ", ok, fmt.Sprintf("%s in %s", ev.Type, strings.Join(c.EventTypes, ", ")))
	}
	if c.MinSeverity != "" {
		ok := ev.Severity.Rank() >= plugin.Severity(c.MinSeverity).Rank()
		add("Schweregrad", ok, fmt.Sprintf("%s ≥ %s", ev.Severity, c.MinSeverity))
	}
	if ok, applies := e.matchSites(c, ev); applies {
		names := make([]string, 0, len(c.Sites))
		for _, id := range c.Sites {
			names = append(names, e.siteName(ctx, id))
		}
		add("Standort", ok, fmt.Sprintf("%s in [%s]", e.siteName(ctx, ev.SiteID), strings.Join(names, ", ")))
	}
	needsDevice := len(c.Tags) > 0 || len(c.Groups) > 0 || len(c.Subnets) > 0 || c.DeviceQuery != "" || c.OnlyUnknown || len(c.DeviceStates) > 0
	if needsDevice && dev == nil {
		add("Gerät", false, "Event hat kein Gerät")
	}
	if dev != nil {
		if len(c.Tags) > 0 {
			ok := false
			for _, t := range c.Tags {
				for _, dt := range dev.Tags {
					if strings.EqualFold(t, dt) {
						ok = true
					}
				}
			}
			add("Tag", ok, fmt.Sprintf("Geräte-Tags [%s], gesucht [%s]", strings.Join(dev.Tags, ", "), strings.Join(c.Tags, ", ")))
		}
		if len(c.Groups) > 0 {
			ok := false
			for _, g := range c.Groups {
				if in, err := e.inv.DeviceInGroup(ctx, dev.ID, g); err == nil && in {
					ok = true
				}
			}
			add("Gruppe", ok, fmt.Sprintf("Mitglied einer der Gruppen %v", c.Groups))
		}
		if len(c.Subnets) > 0 {
			ok := false
			for _, s := range c.Subnets {
				p, err := netip.ParsePrefix(s)
				if err != nil {
					continue
				}
				for _, ip := range dev.IPs {
					if a, err := netip.ParseAddr(ip); err == nil && p.Contains(a) {
						ok = true
					}
				}
			}
			add("Subnetz", ok, fmt.Sprintf("IPs [%s] in [%s]", strings.Join(dev.IPs, ", "), strings.Join(c.Subnets, ", ")))
		}
		if c.DeviceQuery != "" {
			ok, err := e.inv.DeviceMatches(ctx, dev.ID, c.DeviceQuery)
			detail := c.DeviceQuery
			if err != nil {
				detail += " (Fehler: " + err.Error() + ")"
			}
			add("Geräte-Filter", ok && err == nil, detail)
		}
		if c.OnlyUnknown {
			add("Nur nicht bekannte Geräte", dev.State != "known", "Zustand "+dev.State)
		}
		if len(c.DeviceStates) > 0 {
			ok := false
			for _, s := range c.DeviceStates {
				if s == dev.State {
					ok = true
				}
			}
			add("Gerätezustand", ok, fmt.Sprintf("%s in [%s]", dev.State, strings.Join(c.DeviceStates, ", ")))
		}
	}
	for _, pc := range c.Payload {
		v, present := payloadValue(ev.Payload, pc.Field)
		ok := present && compare(v, pc.Op, pc.Value)
		add("Payload "+pc.Field, ok, fmt.Sprintf("%v %s %s", v, pc.Op, pc.Value))
	}
	if tw := c.TimeWindow; tw != nil {
		local := ev.At.In(e.loc)
		from, _ := parseHM(tw.From)
		to, _ := parseHM(tw.To)
		ok := inRange(local.Hour()*60+local.Minute(), from, to)
		if len(tw.Days) > 0 {
			dayOK := false
			for _, d := range tw.Days {
				if int(local.Weekday()) == d {
					dayOK = true
				}
			}
			ok = ok && dayOK
		}
		add("Zeitfenster", ok, fmt.Sprintf("%s, %s–%s", local.Format("Mon 15:04"), tw.From, tw.To))
	}
	return all, res
}
