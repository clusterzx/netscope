package tunnel

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"

	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/wgconf"
)

type fakeNet struct {
	mu        sync.Mutex
	probeErr  error
	applyErr  error
	links     map[string]spec
	handshake map[string]time.Time
	removed   []string
}

func newFakeNet() *fakeNet {
	return &fakeNet{links: map[string]spec{}, handshake: map[string]time.Time{}}
}

func (f *fakeNet) Probe() error { return f.probeErr }
func (f *fakeNet) Apply(name string, s spec) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.applyErr != nil {
		return f.applyErr
	}
	f.links[name] = s
	return nil
}
func (f *fakeNet) Remove(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.links[name]; ok {
		f.removed = append(f.removed, name)
	}
	delete(f.links, name)
	return nil
}
func (f *fakeNet) Links() ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for n := range f.links {
		if n != testIface {
			out = append(out, n)
		}
	}
	return out, nil
}
func (f *fakeNet) Peer(name string) (peerStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.links[name]; !ok {
		return peerStats{}, errors.New("no such device")
	}
	return peerStats{LastHandshake: f.handshake[name], Rx: 100, Tx: 200}, nil
}
func (f *fakeNet) Close() error { return nil }
func (f *fakeNet) setHandshake(name string, t time.Time) {
	f.mu.Lock()
	f.handshake[name] = t
	f.mu.Unlock()
}

type subnets []inventory.Subnet

func (s *subnets) ListSubnets(context.Context) ([]inventory.Subnet, error) { return *s, nil }

type harness struct {
	m      *Manager
	net    *fakeNet
	events *plugintest.Events
	sn     *subnets
	now    time.Time
	cfg    string
}

func wgConfig(t *testing.T, allowed string) string {
	t.Helper()
	priv, _ := wgtypes.GeneratePrivateKey()
	peer, _ := wgtypes.GeneratePrivateKey()
	return "[Interface]\nPrivateKey = " + priv.String() + "\nAddress = 10.10.10.3/32\nDNS = 192.168.1.1\n\n[Peer]\nPublicKey = " +
		peer.PublicKey().String() + "\nEndpoint = 203.0.113.9:51820\nAllowedIPs = " + allowed + "\n"
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	id := int64(7)
	h := &harness{net: newFakeNet(), events: &plugintest.Events{}, now: time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC), cfg: wgConfig(t, "0.0.0.0/0")}
	h.sn = &subnets{
		{ID: 1, CIDR: "192.168.1.0/24", Enabled: true, Access: inventory.AccessWireGuard, TunnelCredentialID: &id},
		{ID: 2, CIDR: "10.20.0.0/24", Enabled: true, Access: inventory.AccessWireGuard, TunnelCredentialID: &id},
		{ID: 3, CIDR: "192.168.8.0/24", Enabled: true, Access: inventory.AccessDirect},
	}
	creds := plugintest.Creds{7: {ID: 7, Name: "Rechenzentrum", Type: plugin.CredWireGuard, Secret: map[string]string{"config": h.cfg}}}
	h.m = &Manager{
		d:       Deps{Subnets: h.sn, Creds: creds, Events: emitter{h.events}, Log: slog.New(slog.NewTextHandler(io.Discard, nil))},
		net:     h.net,
		now:     func() time.Time { return h.now },
		local:   func() []netip.Prefix { return []netip.Prefix{netip.MustParsePrefix("192.168.8.0/24")} },
		tunnels: map[int64]*tunnel{},
		kick:    make(chan struct{}, 1),
		avail:   Availability{Available: true},
	}
	return h
}

// emitter adapts plugintest.Events to the manager's emitter.
type emitter struct{ e *plugintest.Events }

func (e emitter) Emit(ctx context.Context, pluginID string, ev plugin.Event) (int64, error) {
	return e.e.Emit(ctx, ev)
}

func (h *harness) types() []string {
	var out []string
	for _, e := range h.events.Events {
		out = append(out, e.Type)
	}
	return out
}

func TestReconcileRoutesOnlySubnets(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.m.reconcile(ctx, false)
	s, ok := h.net.links["nswg7"]
	if !ok {
		t.Fatalf("interface not created: %v", h.net.links)
	}
	// the config says 0.0.0.0/0 – only the two subnets must be routed
	if got := prefixes(s.Routes); got != "10.20.0.0/24, 192.168.1.0/24" || !s.Addresses {
		t.Fatalf("routes = %s", got)
	}
	st := h.m.Statuses()
	if len(st) != 1 || st[0].State != StateConnecting || st[0].Name != "Rechenzentrum" || st[0].Endpoint != "203.0.113.9:51820" {
		t.Fatalf("status: %+v", st)
	}
	if !strings.Contains(strings.Join(st[0].Warnings, " "), "0.0.0.0/0") {
		t.Errorf("warnings: %v", st[0].Warnings)
	}
	if len(h.m.Unreachable()) != 2 {
		t.Errorf("connecting tunnel must be unreachable: %v", h.m.Unreachable())
	}

	h.now = h.now.Add(5 * time.Second)
	h.net.setHandshake("nswg7", h.now)
	h.m.poll(ctx)
	if st := h.m.Statuses(); st[0].State != StateUp || st[0].LastHandshake == nil || st[0].TxBytes != 200 {
		t.Fatalf("status: %+v", st)
	}
	if len(h.m.Unreachable()) != 0 || len(h.events.Events) != 0 {
		t.Errorf("unreachable=%v events=%v", h.m.Unreachable(), h.types())
	}
	// unchanged configuration is not applied again
	h.net.links["nswg7"] = spec{}
	h.m.reconcile(ctx, false)
	if h.net.links["nswg7"].Config != nil {
		t.Error("unchanged tunnel was re-applied")
	}
}

func TestOutageEvents(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.m.reconcile(ctx, false)
	h.net.setHandshake("nswg7", h.now)
	h.m.poll(ctx)

	h.now = h.now.Add(4 * time.Minute) // no handshake since
	h.m.poll(ctx)
	h.m.poll(ctx)
	if st := h.m.Statuses()[0]; st.State != StateDown {
		t.Fatalf("state = %s", st.State)
	}
	if !slices.Equal(h.types(), []string{plugin.EvTunnelDown}) || !strings.Contains(h.events.Events[0].Title, "getrennt") {
		t.Fatalf("events: %+v", h.events.Events)
	}
	if len(h.m.Unreachable()) != 2 {
		t.Error("subnets behind a down tunnel must be unreachable")
	}
	h.now = h.now.Add(2 * time.Minute)
	h.net.setHandshake("nswg7", h.now)
	h.m.poll(ctx)
	if !slices.Equal(h.types(), []string{plugin.EvTunnelDown, plugin.EvTunnelUp}) {
		t.Fatalf("events: %v", h.types())
	}
	if d := h.events.Events[1].Payload["down_seconds"]; d != int64(120) {
		t.Errorf("down_seconds = %v", d)
	}
}

func TestNeverConnects(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.m.reconcile(ctx, false)
	h.now = h.now.Add(connectGrace + time.Second)
	h.m.poll(ctx)
	if len(h.events.Events) != 1 || !strings.Contains(h.events.Events[0].Title, "lässt sich nicht aufbauen") {
		t.Fatalf("events: %+v", h.events.Events)
	}
}

func TestReconcileRemovesAndErrors(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.net.links["nswg99"] = spec{} // left over from an earlier run
	h.m.reconcile(ctx, false)
	if _, ok := h.net.links["nswg99"]; ok {
		t.Error("stale interface not removed")
	}
	// disabling the subnets removes the tunnel
	for i := range *h.sn {
		(*h.sn)[i].Enabled = false
	}
	h.m.reconcile(ctx, false)
	if _, ok := h.net.links["nswg7"]; ok || len(h.m.Statuses()) != 0 {
		t.Errorf("tunnel not removed: %v %v", h.net.links, h.m.Statuses())
	}

	// a subnet that is attached locally is rejected
	h2 := newHarness(t)
	(*h2.sn)[0].CIDR = "192.168.8.0/25"
	h2.m.reconcile(ctx, false)
	st := h2.m.Statuses()[0]
	if st.State != StateError || !strings.Contains(st.Error, "lokal angeschlossenen") {
		t.Fatalf("status: %+v", st)
	}
	if !slices.Equal(h2.types(), []string{plugin.EvTunnelDown}) {
		t.Errorf("events: %v", h2.types())
	}

	// no WireGuard on the host
	h3 := newHarness(t)
	h3.m.avail = Availability{Reason: "kein NET_ADMIN"}
	h3.m.reconcile(ctx, false)
	if st := h3.m.Statuses()[0]; st.State != StateError || !strings.Contains(st.Error, "kein NET_ADMIN") {
		t.Fatalf("status: %+v", st)
	}

	// apply errors (e.g. route conflict) are reported and retried on resync
	h4 := newHarness(t)
	h4.net.applyErr = errors.New("für 192.168.1.0/24 gibt es schon eine Route")
	h4.m.reconcile(ctx, false)
	if st := h4.m.Statuses()[0]; st.State != StateError || !strings.Contains(st.Error, "Route") {
		t.Fatalf("status: %+v", st)
	}
	h4.net.applyErr = nil
	h4.m.reconcile(ctx, true)
	if st := h4.m.Statuses()[0]; st.State != StateConnecting {
		t.Fatalf("after retry: %+v", st)
	}
}

func TestMissingInterfaceIsRecreated(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	h.m.reconcile(ctx, false)
	delete(h.net.links, "nswg7") // deleted by someone else
	if !h.m.poll(ctx) {
		t.Fatal("poll must request a reconcile")
	}
	h.m.reconcile(ctx, false)
	if _, ok := h.net.links["nswg7"]; !ok {
		t.Fatal("interface not recreated")
	}
}

func TestConnectionTest(t *testing.T) {
	h := newHarness(t)
	testTimeout = 300 * time.Millisecond
	defer func() { testTimeout = 10 * time.Second }()
	cfg, err := wgconf.Parse(wgConfig(t, "192.168.1.0/24"))
	if err != nil {
		t.Fatal(err)
	}
	h.m.now = time.Now
	res := h.m.Test(context.Background(), cfg)
	if res.OK || !strings.Contains(res.Message, "Keine Antwort") {
		t.Fatalf("result: %+v", res)
	}
	if _, ok := h.net.links[testIface]; ok {
		t.Error("test interface not removed")
	}
	go func() {
		time.Sleep(50 * time.Millisecond)
		h.net.setHandshake(testIface, time.Now())
	}()
	if res := h.m.Test(context.Background(), cfg); !res.OK {
		t.Fatalf("result: %+v", res)
	}
	// a config that already runs as tunnel is not opened twice
	h.m.now = func() time.Time { return h.now }
	h.m.reconcile(context.Background(), false)
	running, _ := wgconf.Parse(h.cfg)
	if res := h.m.Test(context.Background(), running); res.OK || !strings.Contains(res.Message, "läuft bereits") {
		t.Fatalf("result: %+v", res)
	}
}
