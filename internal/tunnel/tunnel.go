// Package tunnel runs NetScope's own WireGuard tunnels into remote subnets. A tunnel is a
// vault credential of type wireguard; every enabled subnet with access "wireguard" that
// references it is routed through the tunnel's interface (nswg<credential id>).
//
// Only these subnets are routed – never the configuration's AllowedIPs, so a "0.0.0.0/0"
// config cannot take over the host's traffic – and options that would run commands or
// change the host (PostUp, DNS …) are ignored (see wgconf). The manager watches the
// handshakes: subnets behind a tunnel that is not up are skipped by scans, so their
// devices are not reported offline, and tunnel.down / tunnel.up events are emitted.
package tunnel

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"netscope/internal/bus"
	"netscope/internal/inventory"
	"netscope/internal/netutil"
	"netscope/internal/plugin"
	"netscope/internal/wgconf"
)

// Tunnel states.
const (
	StateConnecting = "connecting" // configured, waiting for the first handshake
	StateUp         = "up"         // recent handshake
	StateDown       = "down"       // no handshake for a while
	StateError      = "error"      // cannot be set up (config, permissions, route conflict …)
)

const (
	// handshakeFresh: WireGuard renews the handshake every 2 minutes while packets flow
	// (the keepalive makes sure they do).
	handshakeFresh = 3 * time.Minute
	connectGrace   = 45 * time.Second
	pollInterval   = 10 * time.Second
	resyncInterval = 5 * time.Minute
)

// testTimeout bounds the connection test (variable for tests).
var testTimeout = 10 * time.Second

// Status is the public state of one tunnel.
type Status struct {
	CredentialID  int64      `json:"credentialId"`
	Name          string     `json:"name"`
	Interface     string     `json:"interface"`
	State         string     `json:"state"`
	Error         string     `json:"error,omitempty"`
	Since         time.Time  `json:"since"`
	Subnets       []string   `json:"subnets"`
	Endpoint      string     `json:"endpoint"`
	Addresses     []string   `json:"addresses"`
	PublicKey     string     `json:"publicKey"`
	LastHandshake *time.Time `json:"lastHandshake,omitempty"`
	RxBytes       int64      `json:"rxBytes"`
	TxBytes       int64      `json:"txBytes"`
	Warnings      []string   `json:"warnings"`
}

// Availability says whether this host can run tunnels.
type Availability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// TestResult is the outcome of a connection test.
type TestResult struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
}

// SubnetSource lists the configured subnets.
type SubnetSource interface {
	ListSubnets(ctx context.Context) ([]inventory.Subnet, error)
}

// CredentialSource decrypts credentials.
type CredentialSource interface {
	Get(ctx context.Context, id int64) (*plugin.Credential, error)
}

// EventEmitter stores events.
type EventEmitter interface {
	Emit(ctx context.Context, pluginID string, ev plugin.Event) (int64, error)
}

// Publisher forwards live updates.
type Publisher interface {
	Publish(topic, typ string, data any)
}

// Deps are the services the manager uses.
type Deps struct {
	Subnets SubnetSource
	Creds   CredentialSource
	Events  EventEmitter
	Bus     Publisher
	Log     *slog.Logger
}

type tunnel struct {
	id        int64
	iface     string
	name      string
	subnets   []netip.Prefix
	summary   wgconf.Summary
	applied   string // fingerprint of the applied configuration ("" = apply again)
	appliedAt time.Time
	state     string
	since     time.Time
	err       string
	stats     peerStats
	// downSince is set while a tunnel.down event is outstanding
	downSince time.Time
	everUp    bool
}

// Manager runs the tunnels.
type Manager struct {
	d     Deps
	net   netOps
	now   func() time.Time
	local func() []netip.Prefix

	mu          sync.RWMutex
	avail       Availability
	statuses    []Status
	unreachable []netip.Prefix

	tunnels map[int64]*tunnel // owned by the loop goroutine
	testMu  sync.Mutex
	kick    chan struct{}
	cancel  context.CancelFunc
	done    chan struct{}
}

// New creates the manager. On hosts without WireGuard support it stays unavailable and
// reports every tunnel as error.
func New(d Deps) *Manager {
	m := &Manager{d: d, now: time.Now, local: localPrefixes, tunnels: map[int64]*tunnel{}, kick: make(chan struct{}, 1)}
	n, err := newNetOps()
	if err != nil {
		m.avail = Availability{Reason: err.Error()}
	} else {
		m.net = n
	}
	return m
}

func localPrefixes() []netip.Prefix {
	list, err := netutil.LocalSubnets()
	if err != nil {
		return nil
	}
	out := make([]netip.Prefix, 0, len(list))
	for _, l := range list {
		out = append(out, l.Prefix)
	}
	return out
}

// Start probes WireGuard support and starts the reconcile loop.
func (m *Manager) Start(ctx context.Context) {
	if m.net != nil {
		if err := m.net.Probe(); err != nil {
			m.setAvail(Availability{Reason: err.Error()})
		} else {
			m.setAvail(Availability{Available: true})
		}
	}
	if a := m.Availability(); !a.Available {
		m.d.Log.Info("WireGuard-Tunnel nicht verfügbar", "reason", a.Reason)
	}
	ctx, m.cancel = context.WithCancel(ctx)
	m.done = make(chan struct{})
	go m.loop(ctx)
}

// Stop ends the loop and removes the tunnel interfaces.
func (m *Manager) Stop() {
	if m.cancel == nil {
		return
	}
	m.cancel()
	<-m.done
	if m.net != nil {
		for _, t := range m.tunnels {
			if err := m.net.Remove(t.iface); err != nil {
				m.d.Log.Warn("Tunnel-Interface entfernen", "interface", t.iface, "err", err)
			}
		}
		_ = m.net.Close()
	}
}

// Reconcile applies configuration changes (subnets, credentials) soon.
func (m *Manager) Reconcile() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

func (m *Manager) setAvail(a Availability) {
	m.mu.Lock()
	m.avail = a
	m.mu.Unlock()
}

// Availability reports whether tunnels can run on this host.
func (m *Manager) Availability() Availability {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.avail
}

// Statuses returns the state of all tunnels.
func (m *Manager) Statuses() []Status {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.statuses)
}

// Status returns the state of the tunnel of a credential.
func (m *Manager) Status(credentialID int64) (Status, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.statuses {
		if s.CredentialID == credentialID {
			return s, true
		}
	}
	return Status{}, false
}

// Unreachable returns the subnets behind tunnels that are not up.
func (m *Manager) Unreachable() []netip.Prefix {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return slices.Clone(m.unreachable)
}

func (m *Manager) loop(ctx context.Context) {
	defer close(m.done)
	m.reconcile(ctx, false)
	poll := time.NewTicker(pollInterval)
	resync := time.NewTicker(resyncInterval)
	defer poll.Stop()
	defer resync.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-m.kick:
			m.reconcile(ctx, false)
		case <-resync.C:
			m.reconcile(ctx, true)
		case <-poll.C:
			if m.poll(ctx) {
				m.reconcile(ctx, false)
			}
		}
	}
}

func fingerprint(config string, routes []netip.Prefix) string {
	h := sha256.New()
	h.Write([]byte(config))
	for _, r := range routes {
		h.Write([]byte("\n" + r.String()))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// desired returns the tunnel subnets per credential.
func (m *Manager) desired(ctx context.Context) (map[int64][]netip.Prefix, error) {
	subnets, err := m.d.Subnets.ListSubnets(ctx)
	if err != nil {
		return nil, err
	}
	out := map[int64][]netip.Prefix{}
	for _, sn := range subnets {
		if !sn.Enabled || sn.Access != inventory.AccessWireGuard || sn.TunnelCredentialID == nil {
			continue
		}
		p, err := netip.ParsePrefix(sn.CIDR)
		if err != nil {
			continue
		}
		id := *sn.TunnelCredentialID
		out[id] = append(out[id], p.Masked())
	}
	for id := range out {
		slices.SortFunc(out[id], func(a, b netip.Prefix) int { return strings.Compare(a.String(), b.String()) })
	}
	return out, nil
}

// reconcile brings the interfaces in line with the configuration. resync re-applies
// tunnels that are not up (endpoint DNS may have changed, interface may be gone).
func (m *Manager) reconcile(ctx context.Context, resync bool) {
	want, err := m.desired(ctx)
	if err != nil {
		m.d.Log.Warn("Tunnel: Subnetze nicht lesbar", "err", err)
		return
	}
	for id, t := range m.tunnels {
		if _, ok := want[id]; !ok {
			if m.net != nil {
				if err := m.net.Remove(t.iface); err != nil {
					m.d.Log.Warn("Tunnel-Interface entfernen", "interface", t.iface, "err", err)
				}
			}
			delete(m.tunnels, id)
			m.d.Log.Info("Tunnel abgebaut", "tunnel", t.name, "interface", t.iface)
		}
	}
	if m.net != nil && m.Availability().Available {
		if links, err := m.net.Links(); err == nil {
			for _, l := range links {
				id, err := strconv.ParseInt(strings.TrimPrefix(l, ifPrefix), 10, 64)
				if _, ok := want[id]; err != nil || !ok {
					_ = m.net.Remove(l) // left over from a previous run
				}
			}
		}
	}
	now := m.now()
	for id, routes := range want {
		t := m.tunnels[id]
		if t == nil {
			t = &tunnel{id: id, iface: fmt.Sprintf("%s%d", ifPrefix, id), name: fmt.Sprintf("#%d", id), state: StateConnecting, since: now}
			m.tunnels[id] = t
		}
		t.subnets = routes
		if a := m.Availability(); !a.Available {
			m.setState(ctx, t, StateError, "WireGuard ist auf diesem Host nicht verfügbar: "+a.Reason, now)
			continue
		}
		cred, err := m.d.Creds.Get(ctx, id)
		if err != nil {
			m.setState(ctx, t, StateError, "Zugangsdaten nicht lesbar: "+err.Error(), now)
			continue
		}
		t.name = cred.Name
		if cred.Type != plugin.CredWireGuard {
			m.setState(ctx, t, StateError, fmt.Sprintf("Credential „%s“ ist keine WireGuard-Konfiguration", cred.Name), now)
			continue
		}
		text := cred.Get("config")
		cfg, err := wgconf.Parse(text)
		if err != nil {
			m.setState(ctx, t, StateError, "Konfiguration ungültig: "+err.Error(), now)
			continue
		}
		t.summary = cfg.Summary(routes)
		if msg := m.overlap(routes); msg != "" {
			m.setState(ctx, t, StateError, msg, now)
			t.applied = ""
			continue
		}
		fp := fingerprint(text, routes)
		if fp == t.applied && !(resync && t.state != StateUp) {
			continue
		}
		if err := m.net.Apply(t.iface, spec{Config: cfg, Routes: routes, Addresses: true}); err != nil {
			m.setState(ctx, t, StateError, err.Error(), now)
			t.applied = ""
			continue
		}
		changed := fp != t.applied
		t.applied, t.appliedAt = fp, now
		if changed || t.state == StateError {
			m.d.Log.Info("Tunnel eingerichtet", "tunnel", t.name, "interface", t.iface, "endpoint", cfg.Peer.Endpoint, "subnets", prefixes(routes))
			if t.state != StateUp {
				m.setState(ctx, t, StateConnecting, "", now)
			}
		}
	}
	m.poll(ctx)
}

// overlap rejects subnets that are directly attached to the host.
func (m *Manager) overlap(routes []netip.Prefix) string {
	for _, r := range routes {
		for _, l := range m.local() {
			if l.Overlaps(r) {
				return fmt.Sprintf("das Subnetz %s überschneidet sich mit dem lokal angeschlossenen Netz %s", r, l)
			}
		}
	}
	return ""
}

// poll reads the handshakes and updates states. It returns true if an interface is
// missing and the configuration must be applied again.
func (m *Manager) poll(ctx context.Context) (reapply bool) {
	now := m.now()
	for _, t := range m.tunnels {
		if t.applied == "" || m.net == nil {
			continue // error state, set by reconcile
		}
		st, err := m.net.Peer(t.iface)
		if err != nil {
			t.applied = ""
			reapply = true
			m.setState(ctx, t, StateDown, "Interface "+t.iface+" fehlt: "+err.Error(), now)
			continue
		}
		t.stats = st
		switch {
		case !st.LastHandshake.IsZero() && now.Sub(st.LastHandshake) < handshakeFresh:
			m.setState(ctx, t, StateUp, "", now)
		case now.Sub(t.appliedAt) < connectGrace:
			m.setState(ctx, t, StateConnecting, "", now)
		default:
			m.setState(ctx, t, StateDown, "", now)
		}
	}
	m.snapshot()
	return reapply
}

// setState records a state and emits tunnel.down / tunnel.up on transitions.
func (m *Manager) setState(ctx context.Context, t *tunnel, state, errMsg string, now time.Time) {
	changed := t.state != state
	if changed {
		t.state, t.since = state, now
	}
	t.err = errMsg
	switch state {
	case StateUp:
		if changed {
			m.d.Log.Info("Tunnel verbunden", "tunnel", t.name, "interface", t.iface)
		}
		t.everUp = true
		if !t.downSince.IsZero() {
			m.emit(ctx, t, plugin.EvTunnelUp, fmt.Sprintf("Tunnel „%s“ wieder verbunden", t.name), "",
				map[string]any{"down_seconds": int64(now.Sub(t.downSince).Seconds())})
			t.downSince = time.Time{}
		}
	case StateDown, StateError:
		if !t.downSince.IsZero() {
			break
		}
		t.downSince = now
		title := fmt.Sprintf("Tunnel „%s“ getrennt", t.name)
		if !t.everUp {
			title = fmt.Sprintf("Tunnel „%s“ lässt sich nicht aufbauen", t.name)
		}
		msg := errMsg
		if msg == "" {
			msg = fmt.Sprintf("Keine Antwort der Gegenstelle %s – die Geräte in %s werden solange nicht gescannt und nicht als offline gewertet.",
				t.summary.Endpoint, prefixes(t.subnets))
		}
		m.d.Log.Warn(title, "interface", t.iface, "error", errMsg)
		m.emit(ctx, t, plugin.EvTunnelDown, title, msg, map[string]any{"error": errMsg})
	}
	if changed {
		m.snapshot()
		if m.d.Bus != nil {
			m.d.Bus.Publish(bus.TopicSystem, "tunnel", m.Statuses())
		}
	}
}

func (m *Manager) emit(ctx context.Context, t *tunnel, typ, title, msg string, extra map[string]any) {
	if m.d.Events == nil {
		return
	}
	payload := map[string]any{"tunnel": t.name, "credential_id": t.id, "subnets": prefixList(t.subnets), "endpoint": t.summary.Endpoint}
	for k, v := range extra {
		payload[k] = v
	}
	if _, err := m.d.Events.Emit(ctx, "tunnel", plugin.Event{Type: typ, Title: title, Message: msg, Payload: payload}); err != nil {
		m.d.Log.Warn("Tunnel-Event speichern", "type", typ, "err", err)
	}
}

// snapshot publishes the state for readers (API, scans).
func (m *Manager) snapshot() {
	statuses := make([]Status, 0, len(m.tunnels))
	var down []netip.Prefix
	for _, t := range m.tunnels {
		s := Status{
			CredentialID: t.id, Name: t.name, Interface: t.iface, State: t.state, Error: t.err, Since: t.since,
			Subnets: prefixList(t.subnets), Endpoint: t.summary.Endpoint, Addresses: t.summary.Addresses,
			PublicKey: t.summary.PublicKey, RxBytes: t.stats.Rx, TxBytes: t.stats.Tx, Warnings: t.summary.Warnings,
		}
		if s.Warnings == nil {
			s.Warnings = []string{}
		}
		if s.Addresses == nil {
			s.Addresses = []string{}
		}
		if !t.stats.LastHandshake.IsZero() {
			hs := t.stats.LastHandshake
			s.LastHandshake = &hs
		}
		statuses = append(statuses, s)
		if t.state != StateUp {
			down = append(down, t.subnets...)
		}
	}
	slices.SortFunc(statuses, func(a, b Status) int { return int(a.CredentialID - b.CredentialID) })
	m.mu.Lock()
	m.statuses, m.unreachable = statuses, down
	m.mu.Unlock()
}

// Test checks a configuration: it opens a temporary interface and waits for the
// handshake with the server. A configuration that is already running as a tunnel is not
// opened twice; its state is reported instead.
func (m *Manager) Test(ctx context.Context, cfg *wgconf.Config) TestResult {
	if a := m.Availability(); !a.Available {
		return TestResult{Message: "WireGuard ist auf diesem Host nicht verfügbar: " + a.Reason}
	}
	pub := cfg.PrivateKey.PublicKey().String()
	for _, s := range m.Statuses() {
		if s.PublicKey != pub {
			continue
		}
		if s.State == StateUp {
			return TestResult{OK: true, Message: fmt.Sprintf("Der Tunnel „%s“ mit dieser Konfiguration steht bereits.", s.Name)}
		}
		return TestResult{Message: fmt.Sprintf("Diese Konfiguration läuft bereits als Tunnel „%s“ (Zustand: %s) %s", s.Name, s.State, s.Error)}
	}
	m.testMu.Lock()
	defer m.testMu.Unlock()
	_ = m.net.Remove(testIface)
	defer func() { _ = m.net.Remove(testIface) }()
	start := m.now()
	if err := m.net.Apply(testIface, spec{Config: cfg}); err != nil {
		return TestResult{Message: err.Error()}
	}
	ctx, cancel := context.WithTimeout(ctx, testTimeout)
	defer cancel()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		if st, err := m.net.Peer(testIface); err == nil && !st.LastHandshake.IsZero() {
			return TestResult{OK: true, Message: "Handshake mit " + cfg.Peer.Endpoint + " erfolgreich – Schlüssel und Endpoint stimmen.",
				LatencyMs: m.now().Sub(start).Milliseconds()}
		}
		select {
		case <-ctx.Done():
			return TestResult{Message: fmt.Sprintf("Keine Antwort von %s innerhalb von %d s. Prüfen: Endpoint und UDP-Port, Firewall der Gegenstelle, "+
				"ob der öffentliche Schlüssel %s dort als Peer eingetragen ist.", cfg.Peer.Endpoint, int(testTimeout.Seconds()), pub)}
		case <-tick.C:
		}
	}
}

func prefixList(ps []netip.Prefix) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return out
}

func prefixes(ps []netip.Prefix) string { return strings.Join(prefixList(ps), ", ") }
