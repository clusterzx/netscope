package agent

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"netscope/internal/agent/proto"
	"netscope/internal/bus"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/inventory"
	"netscope/internal/logging"
	"netscope/internal/pluginhost"
	"netscope/internal/settings"
	"netscope/internal/vault"
)

type harness struct {
	s   *Service
	d   *db.DB
	inv *inventory.Store
}

func newHarness(t *testing.T, version string) *harness {
	t.Helper()
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "a.db"))
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
	log := logging.New(io.Discard, "text", new(slog.LevelVar), logging.NewRing(10))
	inv, err := inventory.New(ctx, d, b, st, log)
	if err != nil {
		t.Fatal(err)
	}
	if err := inv.SaveSubnet(ctx, &inventory.Subnet{CIDR: "192.168.8.0/24", Name: "lan", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	ev := events.New(d, b, inv)
	host := pluginhost.New(pluginhost.Deps{DB: d, Bus: b, Log: log, Inventory: inv, Vault: v, Events: ev, Settings: st,
		DataDir: dir, Location: time.UTC, Version: "test"})
	if err := host.Init(ctx); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "bin")
	_ = os.MkdirAll(bin, 0o755)
	s := New(Deps{DB: d, Bus: b, Log: log, Inventory: inv, Events: ev, Host: host, BinDir: bin, Version: version})
	return &harness{s: s, d: d, inv: inv}
}

func (h *harness) eventTypes(t *testing.T) []string {
	t.Helper()
	rows, err := h.d.R.Query("SELECT type FROM events ORDER BY id")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		_ = rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func fixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("..", "plugins", "ssh", "testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestAgentLifecycle(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "v2")
	one := 1
	enr, token, err := h.s.CreateEnrollment(ctx, EnrollmentInput{Name: "LAN", Tags: []string{"agent", " "}, MaxUses: &one}, "admin")
	if err != nil || !enr.Usable || len(enr.Tags) != 1 {
		t.Fatalf("enrollment: %+v %v", enr, err)
	}
	host := proto.Host{MachineID: "m-1", Hostname: "netscope", OS: "Ubuntu 24.04", Arch: "amd64", Version: "v1"}
	resp, err := h.s.Enroll(ctx, proto.EnrollRequest{Token: token, Host: host}, "192.168.8.123")
	if err != nil || resp.AgentID == 0 || resp.Config.InventoryInterval != time.Hour {
		t.Fatalf("enroll: %+v %v", resp, err)
	}
	if _, err := h.s.Enroll(ctx, proto.EnrollRequest{Token: token, Host: proto.Host{MachineID: "m-2"}}, ""); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("used-up token accepted: %v", err)
	}
	if _, err := h.s.Authenticate(ctx, resp.Secret+"x", ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatalf("wrong secret: %v", err)
	}
	sess, err := h.s.Authenticate(ctx, resp.Secret, "192.168.8.123")
	if err != nil {
		t.Fatal(err)
	}

	// measurements wait for the first inventory
	if err := h.s.ReportMetrics(ctx, sess, proto.MetricsReport{Samples: []proto.Sample{{At: time.Now(), MemTotal: 1}}}); !errors.Is(err, ErrNoDevice) {
		t.Fatalf("metrics before inventory: %v", err)
	}
	report := proto.InventoryReport{Host: host, CollectedAt: time.Now(), Stdout: fixture(t, "ubuntu2404.out"), Stderr: fixture(t, "ubuntu2404.err")}
	if err := h.s.ReportInventory(ctx, sess, report); err != nil {
		t.Fatal(err)
	}
	a, _ := h.s.Agent(ctx, resp.AgentID)
	if a.DeviceID == nil || a.LastInventoryAt == nil || a.LastError != "" {
		t.Fatalf("agent after inventory: %+v", a)
	}
	dev, err := h.inv.Device(ctx, *a.DeviceID)
	if err != nil || dev.Hostname != "netscope" || dev.PrimaryIP != "192.168.8.123" || !slices.Contains(dev.MACs, "bc:24:11:27:22:b5") ||
		!slices.Contains(dev.Tags, "agent") || !dev.Online {
		t.Fatalf("device: %+v %v", dev, err)
	}
	if slices.Contains(dev.MACs, "7e:3f:ab:f0:4d:80") {
		t.Fatal("docker0 MAC must not identify the host")
	}

	// samples with their own time; a nearly full disk raises disk.full once
	t0 := time.Now().Add(-2 * time.Minute).Truncate(time.Second)
	cpu := 12.5
	full := proto.Sample{At: t0.Add(time.Minute), CPU: &cpu, MemTotal: 1000, MemUsed: 250, Disks: []proto.Disk{{Mount: "/", Total: 100, Used: 95}},
		Net: []proto.NetRate{{Interface: "eth0", RX: 1000, TX: 10}}}
	for i := 0; i < 2; i++ {
		if err := h.s.ReportMetrics(ctx, sess, proto.MetricsReport{Samples: []proto.Sample{{At: t0, MemTotal: 1000, MemUsed: 500}, full}}); err != nil {
			t.Fatal(err)
		}
	}
	var n int
	var first int64
	if err := h.d.R.QueryRow(`SELECT COUNT(*), MIN(r.ts) FROM ts_raw r JOIN ts_series s ON s.id = r.series_id WHERE s.metric = ? AND s.device_id = ?`,
		MetricMem, *a.DeviceID).Scan(&n, &first); err != nil {
		t.Fatal(err)
	}
	if n < 2 || first != t0.UnixMilli() {
		t.Fatalf("memory series: %d points, first at %d (want %d)", n, first, t0.UnixMilli())
	}
	full.Disks[0].Used = 50
	if err := h.s.ReportMetrics(ctx, sess, proto.MetricsReport{Samples: []proto.Sample{full}}); err != nil {
		t.Fatal(err)
	}
	if got := h.eventTypes(t); slices.Index(got, "disk.full") < 0 || slices.Index(got, "disk.ok") < slices.Index(got, "disk.full") ||
		count(got, "disk.full") != 1 {
		t.Fatalf("disk events: %v", got)
	}

	// "collect now" and the update offer answer the long poll at once
	if err := h.s.RequestRefresh(ctx, resp.AgentID); err != nil {
		t.Fatal(err)
	}
	p, err := h.s.Poll(ctx, sess, time.Minute, "v1")
	if err != nil || !p.Refresh || p.Update != nil {
		t.Fatalf("poll with refresh: %+v %v", p, err)
	}
	start := time.Now()
	if p, err = h.s.Poll(ctx, sess, 50*time.Millisecond, "v1"); err != nil || p.Refresh || time.Since(start) < 40*time.Millisecond {
		t.Fatalf("idle poll: %+v %v", p, err)
	}
	if err := os.WriteFile(h.s.BinaryPath("linux-amd64"), []byte("agent-v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if p, err = h.s.Poll(ctx, sess, time.Minute, "v1"); err != nil || p.Update == nil || p.Update.Version != "v2" ||
		p.Update.Path != "/agent/bin/linux-amd64" || len(p.Update.SHA256) != 64 {
		t.Fatalf("update offer: %+v %v", p, err)
	}
	if p, _ = h.s.Poll(ctx, sess, 10*time.Millisecond, "v2"); p.Update != nil {
		t.Fatalf("current agent offered an update: %+v", p.Update)
	}

	// silence: agent.offline, the device goes offline (no scanner tracks it); contact again: agent.online
	if _, err := h.d.W.Exec("UPDATE agents SET last_seen_at = ?", time.Now().Add(-time.Hour).UnixMilli()); err != nil {
		t.Fatal(err)
	}
	if err := h.s.checkOffline(ctx); err != nil {
		t.Fatal(err)
	}
	if dev, _ = h.inv.Device(ctx, *a.DeviceID); dev.Online {
		t.Fatal("device of a silent agent still online")
	}
	if a, _ = h.s.Agent(ctx, resp.AgentID); a.Online {
		t.Fatal("silent agent online")
	}
	if _, err := h.s.Authenticate(ctx, resp.Secret, "192.168.8.123"); err != nil {
		t.Fatal(err)
	}
	if got := h.eventTypes(t); count(got, "agent.offline") != 1 || count(got, "agent.online") != 1 {
		t.Fatalf("agent events: %v", got)
	}

	// reinstall (same machine id): same agent, new secret; removal stops the old one
	_, token2, _ := h.s.CreateEnrollment(ctx, EnrollmentInput{Name: "neu"}, "admin")
	resp2, err := h.s.Enroll(ctx, proto.EnrollRequest{Token: token2, Host: host}, "")
	if err != nil || resp2.AgentID != resp.AgentID {
		t.Fatalf("reinstall: %+v %v", resp2, err)
	}
	if _, err := h.s.Authenticate(ctx, resp.Secret, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("old secret still valid after reinstall")
	}
	if err := h.s.Delete(ctx, resp.AgentID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Authenticate(ctx, resp2.Secret, ""); !errors.Is(err, ErrUnauthenticated) {
		t.Fatal("removed agent still accepted")
	}
	if _, err := h.inv.Device(ctx, *a.DeviceID); err != nil {
		t.Fatal("the device must stay when the agent is removed")
	}

	// another host without machine id: a new agent id (never reused), so it cannot take
	// over the device of the removed agent through the reference agent-<id>
	other := proto.Host{Hostname: "alpine", Arch: "amd64", Version: "v2"}
	resp3, err := h.s.Enroll(ctx, proto.EnrollRequest{Token: token2, Host: other}, "")
	if err != nil || resp3.AgentID == resp.AgentID {
		t.Fatalf("id reused: %+v %v", resp3, err)
	}
	sess3, _ := h.s.Authenticate(ctx, resp3.Secret, "")
	alpine := proto.InventoryReport{Host: other, Stdout: fixture(t, "alpine322.out"), Stderr: fixture(t, "alpine322.err")}
	if err := h.s.ReportInventory(ctx, sess3, alpine); err != nil {
		t.Fatal(err)
	}
	if a3, _ := h.s.Agent(ctx, resp3.AgentID); a3.DeviceID == nil || *a3.DeviceID == *a.DeviceID {
		t.Fatalf("second host bound to the first device: %+v", a3)
	}
}

func count(list []string, v string) int {
	n := 0
	for _, s := range list {
		if s == v {
			n++
		}
	}
	return n
}

func TestEnrollmentRules(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, "dev")
	zero := 0
	if _, _, err := h.s.CreateEnrollment(ctx, EnrollmentInput{Name: "x", MaxUses: &zero}, ""); err == nil {
		t.Fatal("max uses 0 accepted")
	}
	past := time.Now().Add(-time.Hour)
	if _, _, err := h.s.CreateEnrollment(ctx, EnrollmentInput{Name: "x", ExpiresAt: &past}, ""); err == nil {
		t.Fatal("expired token accepted")
	}
	e, token, _ := h.s.CreateEnrollment(ctx, EnrollmentInput{Name: "x"}, "")
	if err := h.s.RevokeEnrollment(ctx, e.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := h.s.Enroll(ctx, proto.EnrollRequest{Token: token}, ""); !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("revoked token: %v", err)
	}
	if e, _ = h.s.Enrollment(ctx, e.ID); e.Usable {
		t.Fatal("revoked token usable")
	}
	// development builds offer no updates
	if u := h.s.offer("amd64", "v1"); u != nil {
		t.Fatalf("dev offers %+v", u)
	}
	if InstallScript("http://x:8080") == "" || !strings.Contains(InstallScript("http://x:8080"), "URL='http://x:8080'") {
		t.Fatal("install script without URL")
	}
}
