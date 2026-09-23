package pluginhost

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"netscope/internal/bus"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/settings"
	"netscope/internal/vault"
)

// dummyScanner reports one device per run and proves the full cycle.
type dummyScanner struct{ runs atomic.Int32 }

func (d *dummyScanner) Info() plugin.Info {
	return plugin.Info{ID: "zz_dummy", Kind: plugin.KindScanner, Name: "Dummy", Description: "Test", Version: "1",
		DefaultEnabled: true, DefaultTimeout: time.Minute, Targets: plugin.TargetSubnets, Presence: true}
}
func (d *dummyScanner) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{{Key: "port", Type: plugin.FieldInt, Label: "Port", Default: 22},
		{Key: "secret", Type: plugin.FieldSecret, Label: "Secret"}}}
}
func (d *dummyScanner) Run(ctx context.Context, rc *plugin.RunContext) error {
	n := d.runs.Add(1)
	rc.Log.Info("scanne", "port", rc.Settings.Int("port"))
	rc.Progress(1, 1)
	ports := []plugin.Port{{Port: rc.Settings.Int("port"), Proto: "tcp", State: "open"}}
	if n > 1 {
		ports = append(ports, plugin.Port{Port: 8080, Proto: "tcp", State: "open"})
	}
	_, err := rc.Sink.Observe(ctx, &plugin.Observation{MACs: []string{"aa:00:00:00:00:77"}, IP: "192.168.99.7", Present: true,
		Ports: &plugin.PortScan{Protocol: "tcp", Scanned: []plugin.PortRange{{From: 1, To: 65535}}, Ports: ports}})
	return err
}

// dummyProcessor turns port.opened changes into events.
type dummyProcessor struct {
	mu      sync.Mutex
	changes []plugin.Change
}

func (d *dummyProcessor) Info() plugin.Info {
	return plugin.Info{ID: "zz_proc", Kind: plugin.KindProcessor, Name: "Proc", Description: "Test", Version: "1", DefaultEnabled: true}
}
func (d *dummyProcessor) Schema() plugin.Schema { return plugin.Schema{} }
func (d *dummyProcessor) HandleChanges(ctx context.Context, rc *plugin.RunContext, cs []plugin.Change) error {
	d.mu.Lock()
	d.changes = append(d.changes, cs...)
	d.mu.Unlock()
	for _, c := range cs {
		if c.Type == plugin.ChangePortOpened && !c.Initial {
			if _, err := rc.Events.Emit(ctx, plugin.Event{Type: plugin.EvPortOpened, DeviceID: c.DeviceID, Title: "Port neu"}); err != nil {
				return err
			}
		}
	}
	return nil
}

// flaky fails, blocks or does nothing depending on its params.
type flaky struct{ calls atomic.Int32 }

func (f *flaky) Info() plugin.Info {
	return plugin.Info{ID: "zz_flaky", Kind: plugin.KindImporter, Name: "Flaky", Description: "Test", Version: "1",
		DefaultEnabled: true, DefaultTimeout: time.Minute, DefaultRetries: 1}
}
func (f *flaky) Schema() plugin.Schema { return plugin.Schema{} }
func (f *flaky) Run(ctx context.Context, rc *plugin.RunContext) error {
	f.calls.Add(1)
	switch rc.Params["mode"] {
	case "block":
		<-ctx.Done()
		return ctx.Err()
	case "quiet":
		return plugin.ErrNoChanges
	}
	return errors.New("kaputt")
}
func (f *flaky) Actions() []plugin.Action {
	return []plugin.Action{{Name: "hello", Label: "Hallo", Scope: plugin.ActionPlugin,
		Params: []plugin.Field{{Key: "who", Type: plugin.FieldString, Label: "Wer", Required: true}}}}
}
func (f *flaky) RunAction(ctx context.Context, rc *plugin.RunContext, name string, params map[string]any) (*plugin.ActionResult, error) {
	return &plugin.ActionResult{Message: "Hallo " + params["who"].(string)}, nil
}

var (
	scanner = &dummyScanner{}
	proc    = &dummyProcessor{}
	fl      = &flaky{}
)

func init() {
	plugin.Register(scanner)
	plugin.Register(proc)
	plugin.Register(fl)
}

func newHost(t *testing.T) (*Host, *events.Store, *inventory.Store) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "h.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	key, _ := vault.NewKey()
	v, err := vault.Open(ctx, d, key, vault.KeySource{})
	if err != nil {
		t.Fatal(err)
	}
	st, _ := settings.Load(ctx, d)
	b := bus.New()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	inv, err := inventory.New(ctx, d, b, st, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := inv.SaveSubnet(ctx, &inventory.Subnet{CIDR: "192.168.99.0/24", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	ev := events.New(d, b, inv)
	h := New(Deps{DB: d, Bus: b, Log: log, Inventory: inv, Vault: v, Events: ev, Settings: st, DataDir: t.TempDir(), Location: time.UTC, Version: "test"})
	if err := h.Init(ctx); err != nil {
		t.Fatal(err)
	}
	inv.OnChanges(h.DispatchChanges)
	runCtx, cancel := context.WithCancel(ctx)
	h.Start(runCtx)
	t.Cleanup(func() {
		sctx, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		h.Stop(sctx)
		cancel()
	})
	return h, ev, inv
}

func waitRun(t *testing.T, h *Host, id int64) *RunView {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		rv, err := h.Run(context.Background(), id)
		if err == nil && rv.Status != StatusQueued && rv.Status != StatusRunning {
			return rv
		}
		if errors.Is(err, db.ErrNotFound) {
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("run %d did not finish", id)
	return nil
}

func TestRunCycle(t *testing.T) {
	ctx := context.Background()
	h, ev, _ := newHost(t)
	// settings change takes effect without restart; secrets are masked
	before, after, err := h.UpdateConfig(ctx, "zz_dummy", ConfigInput{Settings: map[string]any{"port": 2222, "secret": "pst"}})
	if err != nil {
		t.Fatal(err)
	}
	if before.Settings["port"] != int64(22) || after.Settings["port"] != int64(2222) || after.Settings["secret"] != plugin.SecretMask {
		t.Fatalf("config views: %v / %v", before.Settings, after.Settings)
	}
	if _, _, err := h.UpdateConfig(ctx, "zz_dummy", ConfigInput{Schedule: strPtr("nonsense")}); err == nil {
		t.Fatal("invalid cron accepted")
	}
	id1, err := h.Trigger(ctx, "zz_dummy", TriggerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	rv := waitRun(t, h, id1)
	if rv.Status != StatusSuccess || rv.Stats["observations"] != float64(1) {
		t.Fatalf("run 1: %+v", rv)
	}
	logs, _ := h.RunLogs(ctx, id1, 0, 100)
	if len(logs) == 0 {
		t.Fatal("no run logs")
	}
	id2, _ := h.Trigger(ctx, "zz_dummy", TriggerOptions{})
	waitRun(t, h, id2)
	// the second run opened port 8080 -> change -> processor -> event
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		list, _, _ := ev.List(ctx, events.Filter{Types: []string{plugin.EvPortOpened}})
		if len(list) == 1 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	proc.mu.Lock()
	defer proc.mu.Unlock()
	t.Fatalf("no port.opened event; processor saw %d changes", len(proc.changes))
}

func TestRetryCancelQuietAndActions(t *testing.T) {
	ctx := context.Background()
	h, ev, _ := newHost(t)
	if _, _, err := h.UpdateConfig(ctx, "zz_flaky", ConfigInput{RetryBackoffSeconds: intPtr(5)}); err != nil {
		t.Fatal(err)
	}
	id, _ := h.Trigger(ctx, "zz_flaky", TriggerOptions{})
	rv := waitRun(t, h, id)
	if rv.Status != StatusFailed || rv.Error != "kaputt" {
		t.Fatalf("expected failed run: %+v", rv)
	}
	runs, _, _ := h.Runs(ctx, RunFilter{PluginID: "zz_flaky"})
	if len(runs) != 2 || runs[0].Trigger != TriggerRetry || runs[0].Attempt != 2 || runs[0].NotBefore == nil {
		t.Fatalf("retry not scheduled with backoff: %+v", runs[0])
	}
	// the retry is queued with backoff; cancel it
	if err := h.Cancel(ctx, runs[0].ID); err != nil {
		t.Fatal(err)
	}
	// a running run can be cancelled
	id, _ = h.Trigger(ctx, "zz_flaky", TriggerOptions{Params: map[string]any{"mode": "block"}})
	for i := 0; i < 200; i++ {
		if rv, _ := h.Run(ctx, id); rv.Status == StatusRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := h.Cancel(ctx, id); err != nil {
		t.Fatal(err)
	}
	if rv := waitRun(t, h, id); rv.Status != StatusCancelled {
		t.Fatalf("cancel: %+v", rv)
	}
	// scheduled uneventful runs are dropped from the history
	id, _ = h.Trigger(ctx, "zz_flaky", TriggerOptions{Trigger: TriggerSchedule, Params: map[string]any{"mode": "quiet"}})
	if rv := waitRun(t, h, id); rv != nil {
		t.Fatalf("quiet run kept: %+v", rv)
	}
	// actions run as runs and return their result
	out, err := h.RunAction(ctx, "zz_flaky", "hello", map[string]any{"who": "Welt"}, nil, "test", 5*time.Second)
	if err != nil || !out.Finished || out.Result == nil || out.Result.Message != "Hallo Welt" {
		t.Fatalf("action: %+v %v", out, err)
	}
	if _, err := h.RunAction(ctx, "zz_flaky", "hello", map[string]any{}, nil, "test", time.Second); err == nil {
		t.Fatal("missing required action param accepted")
	}
	// a finally failed run emits plugin.failed (dedup per plugin)
	list, _, _ := ev.List(ctx, events.Filter{Types: []string{plugin.EvPluginFailed}})
	if len(list) != 0 {
		t.Fatalf("plugin.failed before retries exhausted: %d", len(list))
	}
}

func TestNeverParallelToItself(t *testing.T) {
	ctx := context.Background()
	h, _, _ := newHost(t)
	a, _ := h.Trigger(ctx, "zz_flaky", TriggerOptions{Params: map[string]any{"mode": "block"}})
	b, _ := h.Trigger(ctx, "zz_flaky", TriggerOptions{Params: map[string]any{"mode": "block"}})
	time.Sleep(300 * time.Millisecond)
	ra, _ := h.Run(ctx, a)
	rb, _ := h.Run(ctx, b)
	if ra.Status != StatusRunning || rb.Status != StatusQueued {
		t.Fatalf("expected running+queued, got %s/%s", ra.Status, rb.Status)
	}
	if _, err := h.Trigger(ctx, "zz_flaky", TriggerOptions{Trigger: TriggerSchedule}); !errors.Is(err, ErrAlreadyQueued) {
		t.Fatalf("scheduled trigger while busy: %v", err)
	}
	_ = h.Cancel(ctx, a)
	waitRun(t, h, a)
	for i := 0; i < 200; i++ {
		if rv, _ := h.Run(ctx, b); rv.Status == StatusRunning {
			_ = h.Cancel(ctx, b)
			waitRun(t, h, b)
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("queued run did not start after the first one finished")
}

func strPtr(s string) *string { return &s }
func intPtr(i int) *int       { return &i }
