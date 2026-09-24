// Package mdns implements the "mdns" scanner: multicast DNS / DNS-SD discovery
// (RFC 6762/6763). It enumerates the advertised service types, queries their
// instances and reverse-resolves known addresses, then reports per responding IPv4
// address the host name, services, TXT hints, model and a device type guess.
package mdns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

var mdnsGroup = &net.UDPAddr{IP: net.IPv4(224, 0, 0, 251), Port: 5353}

const (
	maxServiceQueries = 64   // service types queried per session
	maxReverseQueries = 1024 // reverse lookups per session
	questionsPerQuery = 16   // questions per query packet
)

// Plugin is the mDNS discovery scanner.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "mdns",
		Kind:               plugin.KindScanner,
		Name:               "mDNS / Bonjour",
		Description:        "Findet Geräte und Dienste per Multicast-DNS (Bonjour/Avahi): Hostnamen, Dienste, Modell-Hinweise und Gerätetyp.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "*/30 * * * *",
		DefaultTimeout:     5 * time.Minute,
		DefaultConcurrency: 4,
		DefaultRetries:     1,
		Targets:            plugin.TargetSubnets,
		Presence:           false,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "listen", Type: plugin.FieldDuration, Label: "Lauschdauer", Default: "5s",
			Description: "Wie lange pro Netz auf Antworten gewartet wird. Nach 40 % der Zeit werden die gefundenen Dienste im Detail abgefragt.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(120)}},
	}}
}

// session is one discovery on one interface.
type session struct {
	iface   string
	label   string
	scope   func(netip.Addr) bool
	reverse []netip.Addr // known addresses to resolve
}

// buildSessions groups the targets by interface. Targets without a local interface are
// returned as errors.
func buildSessions(t plugin.Targets, known []netip.Addr, ifaceFor func(netip.Addr) string) ([]session, []error) {
	var errs []error
	type ifaceTargets struct {
		prefixes []netip.Prefix
		addrs    map[netip.Addr]bool
	}
	byIface := map[string]*ifaceTargets{}
	entry := func(iface string) *ifaceTargets {
		e := byIface[iface]
		if e == nil {
			e = &ifaceTargets{addrs: map[netip.Addr]bool{}}
			byIface[iface] = e
		}
		return e
	}
	if t.DeviceMode {
		for _, raw := range t.DeviceIPs() {
			a, err := netip.ParseAddr(raw)
			if err != nil || !a.Unmap().Is4() {
				continue
			}
			a = a.Unmap()
			iface := ""
			for _, s := range t.Subnets {
				if s.Interface != "" && s.CIDR.Contains(a) {
					iface = s.Interface
					break
				}
			}
			if iface == "" {
				iface = ifaceFor(a)
			}
			if iface == "" {
				errs = append(errs, fmt.Errorf("kein lokales Interface für %s – mDNS erreicht nur direkt angeschlossene Netze", a))
				continue
			}
			entry(iface).addrs[a] = true
		}
	} else {
		for _, s := range t.Subnets {
			p := s.CIDR.Masked()
			if !p.Addr().Is4() {
				continue
			}
			iface := s.Interface
			if iface == "" {
				iface = ifaceFor(p.Addr())
			}
			if iface == "" {
				errs = append(errs, fmt.Errorf("Subnetz %s: kein lokales Interface – mDNS erreicht nur direkt angeschlossene Netze", p))
				continue
			}
			e := entry(iface)
			e.prefixes = append(e.prefixes, p)
			for _, a := range known {
				if p.Contains(a) {
					e.addrs[a] = true
				}
			}
		}
	}
	ifaces := make([]string, 0, len(byIface))
	for iface := range byIface {
		ifaces = append(ifaces, iface)
	}
	sort.Strings(ifaces)
	out := make([]session, 0, len(ifaces))
	for _, iface := range ifaces {
		e := byIface[iface]
		s := session{iface: iface}
		for a := range e.addrs {
			s.reverse = append(s.reverse, a)
		}
		sort.Slice(s.reverse, func(i, j int) bool { return s.reverse[i].Less(s.reverse[j]) })
		if len(s.reverse) > maxReverseQueries {
			s.reverse = s.reverse[:maxReverseQueries]
		}
		if t.DeviceMode {
			addrs := e.addrs
			s.scope = func(a netip.Addr) bool { return addrs[a] }
			s.label = fmt.Sprintf("%d Geräte über %s", len(addrs), iface)
		} else {
			prefixes := e.prefixes
			s.scope = func(a netip.Addr) bool {
				for _, p := range prefixes {
					if p.Contains(a) {
						return true
					}
				}
				return false
			}
			names := make([]string, len(prefixes))
			for i, p := range prefixes {
				names[i] = p.String()
			}
			s.label = strings.Join(names, ", ") + " über " + iface
		}
		out = append(out, s)
	}
	return out, errs
}

// knownAddrs returns the IPv4 addresses of known devices for reverse lookups.
func knownAddrs(ctx context.Context, rc *plugin.RunContext) []netip.Addr {
	devices := rc.Targets.Devices
	if len(devices) == 0 && !rc.Targets.DeviceMode && rc.Inventory != nil {
		if list, err := rc.Inventory.Devices(ctx, ""); err == nil {
			devices = list
		}
	}
	var out []netip.Addr
	for _, raw := range (plugin.Targets{Devices: devices}).DeviceIPs() {
		if a, err := netip.ParseAddr(raw); err == nil && a.Unmap().Is4() {
			out = append(out, a.Unmap())
		}
	}
	return out
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	listen := rc.Settings.Duration("listen")
	if listen <= 0 {
		listen = 5 * time.Second
	}
	targets := rc.Targets
	if !targets.DeviceMode && len(targets.Subnets) == 0 && rc.Scope.AllSubnets {
		if subs, err := netutil.LocalSubnets(); err == nil {
			for _, s := range subs {
				targets.Subnets = append(targets.Subnets, plugin.SubnetTarget{CIDR: s.Prefix, Interface: s.Interface})
			}
		}
		rc.Log.Info("Keine Subnetze im Scope – nutze die lokal angeschlossenen Netze", "anzahl", len(targets.Subnets))
	}
	// multicast does not cross routers or tunnels: routed networks are skipped, not failed
	targets, skipped := targets.LocalOnly(plugin.RoutedPrefixes(ctx, rc))
	if len(skipped) > 0 {
		rc.NotCovered(skipped...)
	}
	sessions, errs := buildSessions(targets, knownAddrs(ctx, rc), netutil.InterfaceFor)
	for _, err := range errs {
		rc.Log.Warn(err.Error())
	}
	if len(sessions) == 0 {
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		if len(skipped) > 0 {
			rc.Log.Info("Nur geroutete Ziele – mDNS erreicht Geräte hinter Routern und Tunneln nicht", "subnets", len(skipped))
			return nil
		}
		rc.Log.Info("Nichts zu tun: keine Netze oder Geräte im Scope")
		return nil
	}
	var (
		mu     sync.Mutex
		done   int
		failed []error
	)
	rc.Progress(0, len(sessions))
	err := plugin.ForEach(ctx, rc.Parallelism(), sessions, func(ctx context.Context, s session) error {
		n, err := discover(ctx, rc, s, listen)
		mu.Lock()
		done++
		rc.Progress(done, len(sessions))
		if err != nil && ctx.Err() == nil {
			failed = append(failed, fmt.Errorf("%s: %w", s.label, err))
			rc.Log.Warn("mDNS-Abfrage fehlgeschlagen", "ziel", s.label, "error", err)
		}
		mu.Unlock()
		if err == nil {
			rc.Log.Info("mDNS-Abfrage abgeschlossen", "ziel", s.label, "hosts", n)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(failed) == len(sessions) {
		return errors.Join(failed...)
	}
	return nil
}

// discover runs one session: send the queries, listen, then write one observation per
// host.
func discover(ctx context.Context, rc *plugin.RunContext, s session, listen time.Duration) (int, error) {
	ifc, err := net.InterfaceByName(s.iface)
	if err != nil {
		return 0, fmt.Errorf("Interface %s: %w", s.iface, err)
	}
	uc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return 0, err
	}
	upc := ipv4.NewPacketConn(uc)
	if err := upc.SetMulticastInterface(ifc); err != nil {
		rc.Log.Debug("Multicast-Interface nicht gesetzt", "interface", s.iface, "error", err)
	}
	_ = upc.SetMulticastTTL(255)
	conns := []*net.UDPConn{uc}
	if mc, err := net.ListenMulticastUDP("udp4", ifc, mdnsGroup); err != nil {
		rc.Log.Debug("Empfang auf 224.0.0.251:5353 nicht möglich, nur Unicast-Antworten", "interface", s.iface, "error", err)
	} else {
		conns = append(conns, mc)
	}
	lctx, cancel := context.WithTimeout(ctx, listen)
	defer cancel()
	stop := context.AfterFunc(lctx, func() {
		for _, c := range conns {
			c.Close()
		}
	})
	defer stop()

	col := newCollector()
	var wg sync.WaitGroup
	for i, c := range conns {
		wg.Add(1)
		go func(c *net.UDPConn, multicast bool) {
			defer wg.Done()
			receive(c, multicast, ifc.Index, col)
		}(c, i > 0)
	}
	send := func(msgs [][]byte) {
		for _, m := range msgs {
			if _, err := uc.WriteToUDP(m, mdnsGroup); err != nil && lctx.Err() == nil {
				rc.Log.Debug("mDNS-Anfrage nicht gesendet", "interface", s.iface, "error", err)
			}
		}
	}
	queried := map[netip.Addr]bool{}
	reverse := func(addrs []netip.Addr) {
		var names []string
		for _, a := range addrs {
			if !queried[a] && len(queried) < maxReverseQueries {
				queried[a] = true
				names = append(names, reverseName(a))
			}
		}
		send(queries(names))
	}
	send(queries([]string{servicesDomain}))
	reverse(s.reverse)
	select {
	case <-lctx.Done():
	case <-time.After(listen * 2 / 5):
		types := col.serviceTypes()
		if len(types) > maxServiceQueries {
			types = types[:maxServiceQueries]
		}
		send(queries(append([]string{servicesDomain}, types...)))
		var fresh []netip.Addr
		for _, a := range col.responders() {
			if s.scope(a) {
				fresh = append(fresh, a)
			}
		}
		reverse(fresh)
	}
	<-lctx.Done()
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	rc.AddStat("packets", col.packetCount())
	n := 0
	for _, h := range col.hosts(s.scope) {
		obs := observation(h)
		if obs == nil {
			continue
		}
		if _, err := rc.Sink.Observe(ctx, obs); err != nil {
			if ctx.Err() != nil {
				return n, ctx.Err()
			}
			rc.Log.Warn("Ergebnis konnte nicht gespeichert werden", "ip", obs.IP, "error", err)
			continue
		}
		n++
		rc.AddStat("hosts", 1)
	}
	return n, nil
}

// receive reads packets until the connection is closed. On the multicast socket,
// packets that arrived on other interfaces are dropped when the platform reports it.
func receive(c *net.UDPConn, multicast bool, ifIndex int, col *collector) {
	var pc *ipv4.PacketConn
	if multicast {
		pc = ipv4.NewPacketConn(c)
		if pc.SetControlMessage(ipv4.FlagInterface, true) != nil {
			pc = nil
		}
	}
	buf := make([]byte, maxPacket+1)
	failures := 0
	for {
		var (
			n   int
			src net.Addr
			cm  *ipv4.ControlMessage
			err error
		)
		if pc != nil {
			n, cm, src, err = pc.ReadFrom(buf)
		} else {
			n, src, err = c.ReadFrom(buf)
		}
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				return
			}
			if failures++; failures > 100 {
				return
			}
			continue
		}
		failures = 0
		if n > maxPacket || (cm != nil && cm.IfIndex != 0 && cm.IfIndex != ifIndex) {
			continue
		}
		ua, ok := src.(*net.UDPAddr)
		if !ok {
			continue
		}
		a, ok := netip.AddrFromSlice(ua.IP)
		if !ok || !a.Unmap().Is4() {
			continue
		}
		col.add(a.Unmap(), buf[:n])
	}
}

// queries packs PTR questions into as few query messages as needed.
func queries(names []string) [][]byte {
	var out [][]byte
	for len(names) > 0 {
		n := min(len(names), questionsPerQuery)
		b := dnsmessage.NewBuilder(nil, dnsmessage.Header{})
		b.EnableCompression()
		_ = b.StartQuestions()
		for _, name := range names[:n] {
			dn, err := dnsmessage.NewName(name)
			if err != nil {
				continue
			}
			_ = b.Question(dnsmessage.Question{Name: dn, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET})
		}
		if msg, err := b.Finish(); err == nil {
			out = append(out, msg)
		}
		names = names[n:]
	}
	return out
}

// observation converts an aggregated host (nil if there is nothing to report).
func observation(h *host) *plugin.Observation {
	hostname := h.hostname()
	if !h.Present && hostname == "" && len(h.Services) == 0 {
		return nil
	}
	types := h.serviceTypes()
	model := txtValue(h.Services, modelKeys)
	obs := &plugin.Observation{
		IP:         h.IP.String(),
		Present:    h.Present,
		Hostname:   hostname,
		Model:      model,
		Vendor:     txtValue(h.Services, vendorKeys),
		DeviceType: guessType(types, model),
		Raw:        h.raw(),
	}
	if len(types) > 0 {
		obs.Attrs = map[string]string{"mdns.services": strings.Join(types, ",")}
	}
	services := h.Services
	if services == nil {
		services = []Service{}
	}
	obs.Inventory = map[string]any{"services": services, "names": h.names()}
	return obs
}

// raw renders the host's records for the "Rohdaten" view.
func (h *host) raw() string {
	var b strings.Builder
	for _, n := range h.names() {
		fmt.Fprintf(&b, "name %s\n", n)
	}
	for _, s := range h.Services {
		fmt.Fprintf(&b, "service %s %q port %d", s.Type, s.Instance, s.Port)
		if len(s.TXT) > 0 {
			keys := make([]string, 0, len(s.TXT))
			for k := range s.TXT {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				fmt.Fprintf(&b, " %s=%s", k, s.TXT[k])
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}
