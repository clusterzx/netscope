// Package upnp implements the "upnp" scanner: SSDP discovery (M-SEARCH) followed by
// the download of the device descriptions, which reveal friendly name, manufacturer,
// model and the device role (router, media player, printer …).
package upnp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/ipv4"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

var ssdpGroup = &net.UDPAddr{IP: net.IPv4(239, 255, 255, 250), Port: 1900}

const (
	maxResponses        = 4096 // SSDP responses processed per session
	maxLocationsPerHost = 16   // description documents fetched per host
	maxRawSize          = 64 << 10
)

// Plugin is the UPnP/SSDP scanner.
type Plugin struct {
	// client overrides the HTTP client for description downloads (tests).
	client *http.Client
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "upnp",
		Kind:               plugin.KindScanner,
		Name:               "UPnP / SSDP",
		Description:        "Findet UPnP-Geräte per SSDP und liest ihre Gerätebeschreibung: Name, Hersteller, Modell und Gerätetyp.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "*/30 * * * *",
		DefaultTimeout:     5 * time.Minute,
		DefaultConcurrency: 8,
		DefaultRetries:     1,
		Targets:            plugin.TargetSubnets,
		Presence:           false,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "listen", Type: plugin.FieldDuration, Label: "Lauschdauer", Default: "4s",
			Description: "Wie lange pro Netz auf SSDP-Antworten gewartet wird (Geräte antworten innerhalb von 2 Sekunden).",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(60)}},
	}}
}

// session is one discovery on one interface.
type session struct {
	iface string
	label string
	scope func(netip.Addr) bool
}

// buildSessions groups the targets by interface.
func buildSessions(t plugin.Targets, ifaceFor func(netip.Addr) string) ([]session, []error) {
	var errs []error
	prefixes := map[string][]netip.Prefix{}
	addrs := map[string]map[netip.Addr]bool{}
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
				errs = append(errs, fmt.Errorf("kein lokales Interface für %s – SSDP erreicht nur direkt angeschlossene Netze", a))
				continue
			}
			if addrs[iface] == nil {
				addrs[iface] = map[netip.Addr]bool{}
			}
			addrs[iface][a] = true
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
				errs = append(errs, fmt.Errorf("Subnetz %s: kein lokales Interface – SSDP erreicht nur direkt angeschlossene Netze", p))
				continue
			}
			prefixes[iface] = append(prefixes[iface], p)
		}
	}
	var out []session
	for iface, set := range addrs {
		out = append(out, session{iface: iface, label: fmt.Sprintf("%d Geräte über %s", len(set), iface),
			scope: func(a netip.Addr) bool { return set[a] }})
	}
	for iface, list := range prefixes {
		names := make([]string, len(list))
		for i, p := range list {
			names[i] = p.String()
		}
		out = append(out, session{iface: iface, label: strings.Join(names, ", ") + " über " + iface,
			scope: func(a netip.Addr) bool {
				for _, p := range list {
					if p.Contains(a) {
						return true
					}
				}
				return false
			}})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].iface < out[j].iface })
	return out, errs
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	listen := rc.Settings.Duration("listen")
	if listen <= 0 {
		listen = 4 * time.Second
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
	sessions, errs := buildSessions(targets, netutil.InterfaceFor)
	for _, err := range errs {
		rc.Log.Warn(err.Error())
	}
	if len(sessions) == 0 {
		if len(errs) > 0 {
			return errors.Join(errs...)
		}
		if len(skipped) > 0 {
			rc.Log.Info("Nur geroutete Ziele – SSDP erreicht Geräte hinter Routern und Tunneln nicht", "subnets", len(skipped))
			return nil
		}
		rc.Log.Info("Nichts zu tun: keine Netze oder Geräte im Scope")
		return nil
	}
	client := p.client
	if client == nil {
		client = newHTTPClient()
	}
	var failed []error
	for i, s := range sessions {
		responders, err := search(ctx, rc, s, listen)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", s.label, err))
			rc.Log.Warn("SSDP-Suche fehlgeschlagen", "ziel", s.label, "error", err)
			rc.Progress(i+1, len(sessions))
			continue
		}
		n := describe(ctx, rc, client, responders)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rc.Log.Info("SSDP-Suche abgeschlossen", "ziel", s.label, "hosts", n)
		rc.Progress(i+1, len(sessions))
	}
	if len(failed) == len(sessions) {
		return errors.Join(failed...)
	}
	return nil
}

// responder collects the SSDP responses of one address.
type responder struct {
	ip        netip.Addr
	responses []ssdpResponse
}

// locations returns the distinct description URLs, sorted.
func (r *responder) locations() []string {
	var out []string
	seen := map[string]bool{}
	for _, resp := range r.responses {
		if resp.Location != "" && !seen[resp.Location] {
			seen[resp.Location] = true
			out = append(out, resp.Location)
		}
	}
	sort.Strings(out)
	return out
}

// server returns the first SERVER header.
func (r *responder) server() string {
	for _, resp := range r.responses {
		if resp.Server != "" {
			return resp.Server
		}
	}
	return ""
}

func mSearch(st string) []byte {
	return []byte("M-SEARCH * HTTP/1.1\r\n" +
		"HOST: 239.255.255.250:1900\r\n" +
		"MAN: \"ssdp:discover\"\r\n" +
		"MX: 2\r\n" +
		"ST: " + st + "\r\n" +
		"USER-AGENT: NetScope/1.0 UPnP/1.1\r\n\r\n")
}

// search sends M-SEARCH requests on the session's interface and collects the responses
// of addresses in scope.
func search(ctx context.Context, rc *plugin.RunContext, s session, listen time.Duration) ([]*responder, error) {
	ifc, err := net.InterfaceByName(s.iface)
	if err != nil {
		return nil, fmt.Errorf("Interface %s: %w", s.iface, err)
	}
	c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		return nil, err
	}
	defer c.Close()
	pc := ipv4.NewPacketConn(c)
	if err := pc.SetMulticastInterface(ifc); err != nil {
		rc.Log.Debug("Multicast-Interface nicht gesetzt", "interface", s.iface, "error", err)
	}
	_ = pc.SetMulticastTTL(2)
	lctx, cancel := context.WithTimeout(ctx, listen)
	defer cancel()
	stop := context.AfterFunc(lctx, func() { _ = c.SetReadDeadline(time.Now()) })
	defer stop()

	send := func() {
		for _, st := range []string{"ssdp:all", "upnp:rootdevice"} {
			if _, err := c.WriteToUDP(mSearch(st), ssdpGroup); err != nil && lctx.Err() == nil {
				rc.Log.Debug("M-SEARCH nicht gesendet", "interface", s.iface, "error", err)
			}
		}
	}
	send()
	resend := time.AfterFunc(min(time.Second, listen/2), send)
	defer resend.Stop()

	byIP := map[netip.Addr]*responder{}
	count := 0
	buf := make([]byte, 8192)
	failures := 0
	for {
		n, src, err := c.ReadFromUDP(buf)
		if err != nil {
			if lctx.Err() != nil {
				break
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				break
			}
			if failures++; failures > 100 {
				return nil, err
			}
			continue
		}
		failures = 0
		a, ok := netip.AddrFromSlice(src.IP)
		if !ok || !s.scope(a.Unmap()) {
			continue
		}
		a = a.Unmap()
		resp, ok := parseSSDP(buf[:n])
		if !ok {
			continue
		}
		if count < maxResponses {
			count++
			r := byIP[a]
			if r == nil {
				r = &responder{ip: a}
				byIP[a] = r
			}
			if !slices.Contains(r.responses, resp) {
				r.responses = append(r.responses, resp)
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rc.AddStat("responses", count)
	out := make([]*responder, 0, len(byIP))
	for _, r := range byIP {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ip.Less(out[j].ip) })
	return out, nil
}

// describe fetches the descriptions of every responder and writes one observation per
// host as soon as its documents are read.
func describe(ctx context.Context, rc *plugin.RunContext, client *http.Client, responders []*responder) int {
	var (
		mu sync.Mutex
		n  int
	)
	_ = plugin.ForEach(ctx, rc.Parallelism(), responders, func(ctx context.Context, r *responder) error {
		obs := describeHost(ctx, rc, client, r)
		if ctx.Err() != nil {
			return nil
		}
		if _, err := rc.Sink.Observe(ctx, obs); err != nil {
			if ctx.Err() == nil {
				rc.Log.Warn("Ergebnis konnte nicht gespeichert werden", "ip", obs.IP, "error", err)
			}
			return nil
		}
		rc.AddStat("hosts", 1)
		mu.Lock()
		n++
		mu.Unlock()
		return nil
	})
	return n
}

// describeHost downloads the description documents of one responder.
func describeHost(ctx context.Context, rc *plugin.RunContext, client *http.Client, r *responder) *plugin.Observation {
	var (
		descs []description
		raws  []string
	)
	locs := r.locations()
	if len(locs) > maxLocationsPerHost {
		locs = locs[:maxLocationsPerHost]
	}
	for _, loc := range locs {
		if ctx.Err() != nil {
			break
		}
		dev, raw, err := fetchDescription(ctx, client, loc, r.ip)
		if err != nil {
			rc.Log.Debug("Gerätebeschreibung nicht lesbar", "ip", r.ip.String(), "location", loc, "error", err)
			continue
		}
		descs = append(descs, description{Location: loc, Device: dev})
		raws = append(raws, "<!-- "+loc+" -->\n"+string(raw))
	}
	return observation(r, descs, raws)
}

// observation builds the observation of one responder from its SSDP responses and the
// parsed description documents.
func observation(r *responder, descs []description, raws []string) *plugin.Observation {
	obs := &plugin.Observation{IP: r.ip.String(), Present: true}
	attrs := map[string]string{}
	if srv := r.server(); srv != "" {
		attrs["upnp.server"] = srv
	}
	if p := primary(descs); p != nil {
		d := p.Device
		obs.Hostname = d.FriendlyName
		obs.Vendor = d.Manufacturer
		obs.Model = modelOf(d)
		obs.DeviceType = category(d)
		if d.DeviceType != "" {
			attrs["upnp.deviceType"] = d.DeviceType
		}
		if d.FriendlyName != "" {
			attrs["upnp.friendlyName"] = d.FriendlyName
		}
		if d.UDN != "" {
			attrs["upnp.udn"] = d.UDN
		}
	}
	if len(attrs) > 0 {
		obs.Attrs = attrs
	}
	if descs == nil {
		descs = []description{}
	}
	obs.Inventory = map[string]any{"ssdp": r.responses, "descriptions": descs}
	raw := strings.Join(raws, "\n")
	if len(raw) > maxRawSize {
		raw = raw[:maxRawSize]
	}
	obs.Raw = raw
	return obs
}
