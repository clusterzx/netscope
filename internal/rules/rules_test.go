package rules

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"netscope/internal/bus"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/settings"
)

type fakePub struct{ id string }

func (f fakePub) Info() plugin.Info {
	return plugin.Info{ID: f.id, Kind: plugin.KindPublisher, Name: f.id, Description: "x", Version: "1"}
}
func (f fakePub) Schema() plugin.Schema { return plugin.Schema{} }
func (f fakePub) Publish(ctx context.Context, pc *plugin.PublishContext, n *plugin.Notification) error {
	return nil
}

type recorder struct {
	mu       sync.Mutex
	sent     []*plugin.Notification
	fail     bool
	disabled map[string]bool
}

func (r *recorder) Publish(ctx context.Context, id string, n *plugin.Notification) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return io.ErrUnexpectedEOF
	}
	r.sent = append(r.sent, n)
	return nil
}
func (r *recorder) PublisherEnabled(id string) bool { return !r.disabled[id] }
func (r *recorder) Plugin(id string) (plugin.Plugin, bool) {
	if id == "telegram" || id == "email" {
		return fakePub{id}, true
	}
	return nil, false
}

type env struct {
	e   *Engine
	ev  *events.Store
	inv *inventory.Store
	pub *recorder
	now time.Time
	dev int64
}

func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "r.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	st, _ := settings.Load(ctx, d)
	_ = st.SetSystem(ctx, func() settings.System { s := settings.DefaultSystem(); s.PublicURL = "https://ns.lan"; return s }())
	b := bus.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	inv, err := inventory.New(ctx, d, b, st, log)
	if err != nil {
		t.Fatal(err)
	}
	_ = inv.SaveSubnet(ctx, &inventory.Subnet{CIDR: "192.168.8.0/24", Enabled: true})
	dev, err := inv.Observe(ctx, "arpscan", 1, &plugin.Observation{MACs: []string{"aa:bb:cc:00:00:10"}, IP: "192.168.8.10", Present: true})
	if err != nil {
		t.Fatal(err)
	}
	ev := events.New(d, b, inv)
	pub := &recorder{disabled: map[string]bool{}}
	e := New(d, b, log, ev, inv, pub, st, time.UTC)
	en := &env{e: e, ev: ev, inv: inv, pub: pub, dev: dev, now: time.Date(2026, 9, 22, 12, 0, 0, 0, time.UTC)}
	e.nowFn = func() time.Time { return en.now }
	e.ctx = ctx
	return en
}

func (en *env) emit(t *testing.T, ev plugin.Event) events.Event {
	t.Helper()
	ev.At = en.now
	id, err := en.ev.Emit(context.Background(), "test", ev)
	if err != nil {
		t.Fatal(err)
	}
	stored, err := en.ev.Get(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if err := en.e.Evaluate(context.Background(), *stored); err != nil {
		t.Fatal(err)
	}
	return *stored
}

func (en *env) save(t *testing.T, r Rule) *Rule {
	t.Helper()
	r.Enabled = true
	if err := en.e.Save(context.Background(), &r); err != nil {
		t.Fatal(err)
	}
	return &r
}

func (en *env) pending(t *testing.T) []NotificationView {
	t.Helper()
	list, _, err := en.e.Notifications(context.Background(), NotificationFilter{Status: NStatusPending, Limit: 100})
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestMatchConditions(t *testing.T) {
	en := newEnv(t)
	ctx := context.Background()
	_, _, _ = en.inv.Update(ctx, en.dev, inventory.DeviceUpdate{Tags: &[]string{"iot"}})
	dev, _ := en.e.device(ctx, en.dev)
	base := EventInput{Type: plugin.EvPortOpened, Severity: plugin.SevMedium, DeviceID: en.dev, At: en.now,
		Payload: map[string]any{"port": float64(22), "cvss": 9.1, "service": "ssh"}}
	cases := []struct {
		name string
		c    Conditions
		dev  *DeviceContext
		want bool
	}{
		{"empty matches", Conditions{}, dev, true},
		{"type exact", Conditions{EventTypes: []string{"port.opened"}}, dev, true},
		{"type pattern", Conditions{EventTypes: []string{"port.*"}}, dev, true},
		{"type other", Conditions{EventTypes: []string{"device.new"}}, dev, false},
		{"severity ok", Conditions{MinSeverity: "medium"}, dev, true},
		{"severity too low", Conditions{MinSeverity: "high"}, dev, false},
		{"tag", Conditions{Tags: []string{"IoT"}}, dev, true},
		{"tag missing", Conditions{Tags: []string{"server"}}, dev, false},
		{"subnet", Conditions{Subnets: []string{"192.168.8.0/24"}}, dev, true},
		{"subnet other", Conditions{Subnets: []string{"10.0.0.0/8"}}, dev, false},
		{"only unknown", Conditions{OnlyUnknown: true}, dev, true},
		{"state known", Conditions{DeviceStates: []string{"known"}}, dev, false},
		{"device query", Conditions{DeviceQuery: "tag:iot ip:192.168.8.10"}, dev, true},
		{"device condition without device", Conditions{Tags: []string{"iot"}}, nil, false},
		{"payload >=", Conditions{Payload: []PayloadCond{{"cvss", ">=", "9"}}}, dev, true},
		{"payload <", Conditions{Payload: []PayloadCond{{"cvss", "<", "9"}}}, dev, false},
		{"payload =", Conditions{Payload: []PayloadCond{{"service", "=", "SSH"}}}, dev, true},
		{"payload contains", Conditions{Payload: []PayloadCond{{"service", "contains", "s"}}}, dev, true},
		{"payload missing", Conditions{Payload: []PayloadCond{{"nope", "=", "x"}}}, dev, false},
		{"time window in", Conditions{TimeWindow: &TimeWindow{From: "08:00", To: "18:00"}}, dev, true},
		{"time window out", Conditions{TimeWindow: &TimeWindow{From: "20:00", To: "06:00"}}, dev, false},
		{"weekday", Conditions{TimeWindow: &TimeWindow{Days: []int{2}, From: "00:00", To: "00:00"}}, dev, true}, // 2026-09-22 is a Tuesday
		{"weekday other", Conditions{TimeWindow: &TimeWindow{Days: []int{0, 6}, From: "00:00", To: "00:00"}}, dev, false},
	}
	for _, c := range cases {
		in := base
		if c.dev == nil {
			in.DeviceID = 0
		}
		got, reasons := en.e.match(ctx, c.c, in, c.dev)
		if got != c.want {
			t.Errorf("%s: got %v want %v (%+v)", c.name, got, c.want, reasons)
		}
	}
}

func TestValidateRule(t *testing.T) {
	exists := func(id string) bool { return id == "telegram" }
	bad := []Rule{
		{Name: "", Actions: []Action{{Publisher: "telegram"}}},
		{Name: "x"},
		{Name: "x", Actions: []Action{{Publisher: "nope"}}},
		{Name: "x", Conditions: Conditions{EventTypes: []string{"no.such"}}, Actions: []Action{{Publisher: "telegram"}}},
		{Name: "x", Actions: []Action{{Publisher: "telegram", Mode: "batch"}}},
		{Name: "x", Actions: []Action{{Publisher: "telegram", Throttle: "10s"}}},
		{Name: "x", Actions: []Action{{Publisher: "telegram", QuietHours: &QuietHours{From: "25:00", To: "06:00"}}}},
		{Name: "x", Conditions: Conditions{Payload: []PayloadCond{{"a", "~", "b"}}}, Actions: []Action{{Publisher: "telegram"}}},
	}
	for i, r := range bad {
		if err := r.Validate(exists); err == nil {
			t.Errorf("rule %d accepted", i)
		}
	}
	good := Rule{Name: "ok", Conditions: Conditions{EventTypes: []string{"port.*"}}, Actions: []Action{{Publisher: "telegram"}}}
	if err := good.Validate(exists); err != nil || good.Actions[0].Mode != "immediate" || good.Actions[0].Priority != "normal" {
		t.Fatalf("defaults: %v %+v", err, good.Actions[0])
	}

	// all problems are reported at once, with JSON paths for the form fields
	multi := Rule{Name: " ", Conditions: Conditions{Subnets: []string{"10.0.0.0/8", "nope"}, Payload: []PayloadCond{{"", "=", "1"}}},
		Actions: []Action{{Publisher: "telegram"}, {Publisher: "telegram", Throttle: "5s", QuietHours: &QuietHours{From: "22:00", To: "7"}}}}
	err := multi.Validate(exists)
	var ve *plugin.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("want ValidationError, got %v", err)
	}
	var fields []string
	for _, fe := range ve.Errors {
		fields = append(fields, fe.Field)
	}
	want := []string{"name", "conditions.subnets.1", "conditions.payload.0.field", "actions.1.throttle", "actions.1.quietHours.to"}
	if strings.Join(fields, ",") != strings.Join(want, ",") {
		t.Errorf("fields %v, want %v", fields, want)
	}
}

func TestBundlingBatchThrottleQuiet(t *testing.T) {
	en := newEnv(t)
	ctx := context.Background()
	en.save(t, Rule{Name: "sofort", Conditions: Conditions{EventTypes: []string{plugin.EvPortOpened}},
		Actions: []Action{{Publisher: "telegram", Mode: "immediate"}}})
	// two events of the same run are bundled into one notification
	en.emit(t, plugin.Event{Type: plugin.EvPortOpened, DeviceID: en.dev, RunID: 7, Title: "a"})
	en.emit(t, plugin.Event{Type: plugin.EvPortOpened, DeviceID: en.dev, RunID: 7, Title: "b"})
	en.emit(t, plugin.Event{Type: plugin.EvPortOpened, DeviceID: en.dev, RunID: 8, Title: "c"})
	p := en.pending(t)
	if len(p) != 2 {
		t.Fatalf("expected 2 bundles, got %d", len(p))
	}
	sizes := map[int]bool{len(p[0].EventIDs): true, len(p[1].EventIDs): true}
	if !sizes[2] || !sizes[1] {
		t.Fatalf("bundles %v %v", p[0].EventIDs, p[1].EventIDs)
	}
	// dispatch after the bundle window
	en.now = en.now.Add(10 * time.Second)
	if err := en.e.deliverDue(ctx); err != nil {
		t.Fatal(err)
	}
	if len(en.pub.sent) != 2 {
		t.Fatalf("sent %d", len(en.pub.sent))
	}
	var bundle *plugin.Notification
	for _, n := range en.pub.sent {
		if len(n.Events) == 2 {
			bundle = n
		}
	}
	if bundle == nil || bundle.Events[0].Link != "https://ns.lan/events/"+itoa(bundle.Events[0].ID) {
		t.Fatalf("bundle/deep link wrong: %+v", bundle)
	}

	// batch mode collects over N minutes
	en.save(t, Rule{Name: "gesammelt", Conditions: Conditions{EventTypes: []string{plugin.EvPortClosed}},
		Actions: []Action{{Publisher: "email", Mode: "batch", BatchMinutes: 30}}})
	en.emit(t, plugin.Event{Type: plugin.EvPortClosed, DeviceID: en.dev, RunID: 9, Title: "x"})
	en.now = en.now.Add(10 * time.Minute)
	en.emit(t, plugin.Event{Type: plugin.EvPortClosed, DeviceID: en.dev, RunID: 10, Title: "y"})
	_ = en.e.deliverDue(ctx)
	if len(en.pub.sent) != 2 {
		t.Fatal("batch sent too early")
	}
	en.now = en.now.Add(21 * time.Minute)
	_ = en.e.deliverDue(ctx)
	if len(en.pub.sent) != 3 || len(en.pub.sent[2].Events) != 2 {
		t.Fatalf("batch not delivered as one: %d", len(en.pub.sent))
	}

	// throttle: once per 24h per event type + device + dedup key
	en.save(t, Rule{Name: "täglich", Conditions: Conditions{EventTypes: []string{plugin.EvCertExpiring}},
		Actions: []Action{{Publisher: "telegram", Throttle: "24h"}}})
	en.emit(t, plugin.Event{Type: plugin.EvCertExpiring, DeviceID: en.dev, Title: "c1", DedupKey: "c1", DedupWindow: time.Minute})
	en.now = en.now.Add(10 * time.Second)
	_ = en.e.deliverDue(ctx)
	sent := len(en.pub.sent)
	en.now = en.now.Add(2 * time.Hour)
	en.emit(t, plugin.Event{Type: plugin.EvCertExpiring, DeviceID: en.dev, Title: "c1 again", DedupKey: "c1", DedupWindow: time.Minute})
	if n := len(en.pending(t)); n != 0 {
		t.Fatalf("throttle: %d pending", n)
	}
	en.now = en.now.Add(23 * time.Hour)
	en.emit(t, plugin.Event{Type: plugin.EvCertExpiring, DeviceID: en.dev, Title: "c1 next day", DedupKey: "c1", DedupWindow: time.Minute})
	en.now = en.now.Add(10 * time.Second)
	_ = en.e.deliverDue(ctx)
	if len(en.pub.sent) != sent+1 {
		t.Fatalf("throttle expired: sent %d, want %d", len(en.pub.sent), sent+1)
	}
}

func TestQuietHoursDisabledPublisherEscalation(t *testing.T) {
	en := newEnv(t)
	ctx := context.Background()
	en.now = time.Date(2026, 9, 22, 23, 30, 0, 0, time.UTC)
	en.save(t, Rule{Name: "nachts", Conditions: Conditions{EventTypes: []string{plugin.EvDeviceOffline}},
		Actions: []Action{{Publisher: "telegram", QuietHours: &QuietHours{From: "22:00", To: "07:00", Behavior: "delay"}}}})
	en.emit(t, plugin.Event{Type: plugin.EvDeviceOffline, DeviceID: en.dev, Title: "weg"})
	p := en.pending(t)
	if len(p) != 1 || !p[0].DeliverAfter.Equal(time.Date(2026, 9, 23, 7, 0, 0, 0, time.UTC)) {
		t.Fatalf("quiet hours delay: %+v", p)
	}
	// disabled publisher: recorded as skipped
	en.pub.disabled["email"] = true
	en.save(t, Rule{Name: "aus", Conditions: Conditions{EventTypes: []string{plugin.EvDeviceOnline}}, Actions: []Action{{Publisher: "email"}}})
	en.emit(t, plugin.Event{Type: plugin.EvDeviceOnline, DeviceID: en.dev, Title: "da"})
	skipped, _, _ := en.e.Notifications(ctx, NotificationFilter{Status: NStatusSkipped, Publisher: "email", Limit: 10})
	if len(skipped) != 1 {
		t.Fatalf("disabled publisher not recorded: %d", len(skipped))
	}
	// escalation after 15 minutes without acknowledgement
	en.now = time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	en.save(t, Rule{Name: "eskalieren", Conditions: Conditions{EventTypes: []string{plugin.EvHealthDown}},
		Actions: []Action{{Publisher: "telegram", EscalateAfterMinutes: 15, EscalatePublisher: "email"}}})
	e1 := en.emit(t, plugin.Event{Type: plugin.EvHealthDown, DeviceID: en.dev, Title: "down 1"})
	e2 := en.emit(t, plugin.Event{Type: plugin.EvHealthDown, DeviceID: en.dev, Title: "down 2", RunID: 99})
	if _, err := en.ev.Ack(ctx, []int64{e2.ID}, "admin", "ok"); err != nil {
		t.Fatal(err)
	}
	en.now = en.now.Add(16 * time.Minute)
	if err := en.e.checkEscalations(ctx); err != nil {
		t.Fatal(err)
	}
	esc, _, _ := en.e.Notifications(ctx, NotificationFilter{Status: NStatusPending, Publisher: "email", Limit: 10})
	if len(esc) != 1 || esc[0].Kind != plugin.NotifyEscalation || esc[0].EventIDs[0] != e1.ID || esc[0].Priority != "urgent" {
		t.Fatalf("escalation: %+v", esc)
	}
	// a failing publisher is retried with backoff, not dropped
	en.pub.fail = true
	_ = en.e.deliverDue(ctx)
	list, _, _ := en.e.Notifications(ctx, NotificationFilter{Publisher: "email", Limit: 10})
	for _, n := range list {
		if n.Kind == plugin.NotifyEscalation && (n.Status != NStatusPending || n.Attempts != 1 || !n.DeliverAfter.After(en.now)) {
			t.Fatalf("retry: %+v", n)
		}
	}
}

func TestSimulateAndDefaults(t *testing.T) {
	en := newEnv(t)
	ctx := context.Background()
	if err := en.e.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	if err := en.e.SeedDefaults(ctx); err != nil {
		t.Fatal(err)
	}
	rules, _ := en.e.List(ctx)
	if len(rules) != 4 {
		t.Fatalf("defaults seeded %d", len(rules))
	}
	var cve *Rule
	for _, r := range rules {
		if r.Builtin == "critical-cve" {
			cve = r
		}
	}
	res, err := en.e.Simulate(ctx, cve, SimInput{Type: plugin.EvCVENew, DeviceID: en.dev, Payload: map[string]any{"cvss": 9.8}})
	if err != nil || !res.Matched || len(res.Actions) != 1 || res.PreviewText == "" {
		t.Fatalf("simulate: %+v %v", res, err)
	}
	res, _ = en.e.Simulate(ctx, cve, SimInput{Type: plugin.EvCVENew, Payload: map[string]any{"cvss": 5}})
	if res.Matched {
		t.Fatal("cvss 5 must not match")
	}
	if len(en.pending(t)) != 0 {
		t.Fatal("simulation must not create notifications")
	}
}

func itoa(n int64) string {
	b, _ := json.Marshal(n)
	return string(b)
}
