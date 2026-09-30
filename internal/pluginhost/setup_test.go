package pluginhost

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/settings"
)

func countRuns(t *testing.T, h *Host, id string) int {
	t.Helper()
	var n int
	if err := h.DB.R.QueryRow("SELECT COUNT(*) FROM runs WHERE plugin_id = ?", id).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

// Before the setup is finished no plugin runs: scheduled and manual runs are refused,
// runs queued before are not started. Release lets them run.
func TestHoldUntilSetupFinished(t *testing.T) {
	ctx := context.Background()
	h, _, _ := newHost(t)
	h.Hold()
	if !h.Held() {
		t.Fatal("not held")
	}
	if _, _, err := h.UpdateConfig(ctx, "zz_dummy", ConfigInput{Schedule: strPtr("@every 10s")}); err != nil {
		t.Fatal(err)
	}
	for _, trigger := range []string{TriggerSchedule, TriggerManual} {
		if _, err := h.Trigger(ctx, "zz_dummy", TriggerOptions{Trigger: trigger}); !errors.Is(err, ErrSetupPending) {
			t.Fatalf("%s run while held: %v", trigger, err)
		}
	}
	if _, err := h.RunAction(ctx, "zz_flaky", "hello", map[string]any{"who": "x"}, nil, "test", time.Second); !errors.Is(err, ErrSetupPending) {
		t.Fatalf("action while held: %v", err)
	}
	// a run queued before (e.g. by an earlier start) is not started either
	if _, err := h.DB.W.ExecContext(ctx, `INSERT INTO runs(plugin_id, trigger, status, attempt, scope, params, created_at, requested_by)
		VALUES ('zz_flaky', 'manual', 'queued', 1, '{}', '{"mode":"quiet"}', 1, 'test')`); err != nil {
		t.Fatal(err)
	}
	h.kick()
	time.Sleep(2500 * time.Millisecond)
	status := func() string {
		var s string
		_ = h.DB.R.QueryRow("SELECT status FROM runs WHERE plugin_id = 'zz_flaky' ORDER BY id LIMIT 1").Scan(&s)
		return s
	}
	if s := status(); s != StatusQueued {
		t.Fatalf("queued run started while held: %s", s)
	}
	if n := countRuns(t, h, "zz_dummy"); n != 0 {
		t.Fatalf("%d runs while held", n)
	}
	h.Release()
	deadline := time.Now().Add(10 * time.Second)
	for status() == StatusQueued {
		if time.Now().After(deadline) {
			t.Fatal("queued run did not start after Release")
		}
		time.Sleep(50 * time.Millisecond)
	}
	if testing.Short() {
		return
	}
	// the schedule starts after Release (every 10 s)
	deadline = time.Now().Add(15 * time.Second)
	for countRuns(t, h, "zz_dummy") == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no scheduled run after Release")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

// The time zone is a setting: a change moves the next scheduled run and the time zone
// plugins get (reports), without a restart.
func TestTimezoneChangeWithoutRestart(t *testing.T) {
	ctx := context.Background()
	h, _, _ := newHost(t)
	if _, _, err := h.UpdateConfig(ctx, "zz_dummy", ConfigInput{Schedule: strPtr("0 3 * * *")}); err != nil {
		t.Fatal(err)
	}
	setTZ := func(name string) {
		t.Helper()
		sys := h.Settings.System()
		sys.Timezone = name
		if err := h.Settings.SetSystem(ctx, sys); err != nil {
			t.Fatal(err)
		}
	}
	setTZ("UTC")
	if next := h.NextRun("zz_dummy"); next == nil || next.UTC().Hour() != 3 {
		t.Fatalf("UTC: next run %v", next)
	}
	setTZ("Asia/Tokyo") // UTC+9, no daylight saving time
	if next := h.NextRun("zz_dummy"); next == nil || next.UTC().Hour() != 18 {
		t.Fatalf("Tokyo: next run %v", next)
	}
	if loc := h.Env().Location; loc == nil || loc.String() != "Asia/Tokyo" {
		t.Fatalf("plugin environment: %v", loc)
	}
	if err := (&settings.System{Timezone: "Mars/Olympus"}).Validate(); err == nil {
		t.Fatal("unknown time zone accepted")
	}
}

// Excluded addresses are removed from the targets of a run; a device without another
// address is dropped, subnet scanners get the exclusions.
func TestScanExclusions(t *testing.T) {
	ctx := context.Background()
	h, _, _ := newHost(t)
	sys := h.Settings.System()
	sys.ScanExclusions = []string{"192.168.99.7", "10.0.0.0/30"}
	if err := h.Settings.SetSystem(ctx, sys); err != nil {
		t.Fatal(err)
	}
	tg := plugin.Targets{Devices: []plugin.DeviceInfo{
		{ID: 1, PrimaryIP: "192.168.99.7"},
		{ID: 2, PrimaryIP: "192.168.99.7", IPs: []string{"192.168.99.7", "192.168.99.8"},
			Ports: []plugin.PortRef{{IP: "192.168.99.7", Port: 80}, {IP: "192.168.99.8", Port: 443}}},
		{ID: 3, PrimaryIP: "10.0.0.9", IPs: []string{"10.0.0.2"}},
		{ID: 4},
	}}
	h.applyExclusions(&tg)
	if len(tg.Exclude) != 2 || !tg.Excluded(netip.MustParseAddr("10.0.0.3")) {
		t.Fatalf("exclusions: %v", tg.Exclude)
	}
	if len(tg.Devices) != 3 || tg.Devices[0].ID != 2 || tg.Devices[1].ID != 3 || tg.Devices[2].ID != 4 {
		t.Fatalf("devices: %+v", tg.Devices)
	}
	d2 := tg.Devices[0]
	if d2.PrimaryIP != "192.168.99.8" || len(d2.IPs) != 1 || len(d2.Ports) != 1 || d2.Ports[0].Port != 443 {
		t.Fatalf("device 2: %+v", d2)
	}
	if d3 := tg.Devices[1]; d3.PrimaryIP != "10.0.0.9" || len(d3.IPs) != 0 {
		t.Fatalf("device 3: %+v", d3)
	}
}
