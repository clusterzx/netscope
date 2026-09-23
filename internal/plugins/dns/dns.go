// Package dns implements the "dns" scanner: reverse DNS (PTR) lookups of all device
// addresses against a configurable resolver.
package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the reverse DNS scanner.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "dns",
		Kind:               plugin.KindScanner,
		Name:               "Reverse-DNS",
		Description:        "Ermittelt Hostnamen per Reverse-DNS-Abfrage (PTR) gegen einen konfigurierbaren DNS-Server.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "15 * * * *",
		DefaultTimeout:     10 * time.Minute,
		DefaultConcurrency: 16,
		DefaultRetries:     1,
		Targets:            plugin.TargetDevices,
		Presence:           false,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "resolver", Type: plugin.FieldString, Label: "DNS-Server", Default: "192.168.8.1", Required: true,
			Placeholder: "192.168.8.1 oder dns.lan:53",
			Description: "Hostname oder IP des DNS-Servers, optional mit Port (Standard 53). Meist der Router, der die DHCP-Namen kennt."},
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Timeout pro Abfrage", Default: "2s",
			Description: "Maximale Wartezeit auf eine Antwort je Adresse.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(60)}},
		{Key: "all_ips", Type: plugin.FieldBool, Label: "Alle IP-Adressen abfragen", Default: false,
			Description: "Aus: nur die primäre IP eines Geräts. An: alle bekannten Adressen, der erste gefundene Name gewinnt."},
	}}
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	if _, err := resolverAddr(s.String("resolver")); err != nil {
		return &plugin.ValidationError{Errors: []plugin.FieldError{{Field: "resolver", Message: err.Error()}}}
	}
	return nil
}

// resolverAddr normalizes "host", "host:port", "v6addr" or "[v6addr]:port" to host:port.
func resolverAddr(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", errors.New("DNS-Server fehlt")
	}
	if host, port, err := net.SplitHostPort(s); err == nil {
		p, perr := strconv.Atoi(port)
		if host == "" || perr != nil || p < 1 || p > 65535 {
			return "", fmt.Errorf("ungültiger DNS-Server %q (host oder host:port erwartet)", s)
		}
		return net.JoinHostPort(host, port), nil
	}
	if a, err := netip.ParseAddr(strings.Trim(s, "[]")); err == nil {
		return net.JoinHostPort(a.String(), "53"), nil
	}
	if strings.ContainsAny(s, " /:[]") {
		return "", fmt.Errorf("ungültiger DNS-Server %q (host oder host:port erwartet)", s)
	}
	return net.JoinHostPort(s, "53"), nil
}

// newResolver returns a pure Go resolver that sends every query to addr.
func newResolver(addr string, timeout time.Duration) *net.Resolver {
	return &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, network, _ string) (net.Conn, error) {
			d := net.Dialer{Timeout: timeout}
			return d.DialContext(ctx, network, addr)
		},
	}
}

// outcome of the lookups of one device.
type outcome int

const (
	outcomeName     outcome = iota // a PTR record was found
	outcomeNotFound                // every address answered NXDOMAIN / no PTR
	outcomeUnknown                 // timeouts or server errors: nothing is known
)

// lookupDevice queries the addresses in order and returns the first name found.
func lookupDevice(ctx context.Context, r *net.Resolver, timeout time.Duration, ips []string) (outcome, string, string, error) {
	notFound := 0
	var lastErr error
	for _, ip := range ips {
		qctx, cancel := context.WithTimeout(ctx, timeout)
		names, err := r.LookupAddr(qctx, ip)
		cancel()
		if err == nil {
			for _, n := range names {
				if n = strings.TrimSuffix(strings.TrimSpace(n), "."); n != "" {
					return outcomeName, ip, n, nil
				}
			}
			notFound++
			continue
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) && dnsErr.IsNotFound {
			notFound++
			continue
		}
		lastErr = err
	}
	if notFound == len(ips) {
		return outcomeNotFound, "", "", nil
	}
	return outcomeUnknown, "", "", lastErr
}

// addresses returns the addresses to query for a device.
func addresses(d plugin.DeviceInfo, all bool) []string {
	ips := []string{d.PrimaryIP}
	if d.PrimaryIP == "" && len(d.IPs) > 0 {
		ips = []string{d.IPs[0]}
	}
	if all {
		ips = append(ips, d.IPs...)
	}
	var out []string
	seen := map[string]bool{}
	for _, raw := range ips {
		a, err := netip.ParseAddr(raw)
		if err != nil {
			continue
		}
		a = a.Unmap()
		if a.IsLoopback() || a.IsUnspecified() || a.IsMulticast() || a.IsLinkLocalUnicast() || seen[a.String()] {
			continue
		}
		seen[a.String()] = true
		out = append(out, a.String())
	}
	return out
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	addr, err := resolverAddr(rc.Settings.String("resolver"))
	if err != nil {
		return err
	}
	timeout := rc.Settings.Duration("timeout")
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	all := rc.Settings.Bool("all_ips")
	r := newResolver(addr, timeout)
	devices := rc.Targets.Devices
	var done, answered, failed atomic.Int64
	rc.Progress(0, len(devices))
	err = plugin.ForEach(ctx, rc.Parallelism(), devices, func(ctx context.Context, d plugin.DeviceInfo) error {
		defer func() { rc.Progress(int(done.Add(1)), len(devices)) }()
		ips := addresses(d, all)
		if len(ips) == 0 {
			return nil
		}
		res, ip, name, err := lookupDevice(ctx, r, timeout, ips)
		if ctx.Err() != nil {
			return nil
		}
		var obs *plugin.Observation
		switch res {
		case outcomeName:
			answered.Add(1)
			rc.AddStat("names", 1)
			obs = &plugin.Observation{DeviceID: d.ID, IP: ip, Hostname: name, Raw: ip + " PTR " + name}
		case outcomeNotFound:
			answered.Add(1)
			rc.AddStat("not_found", 1)
			obs = &plugin.Observation{DeviceID: d.ID, IP: ips[0], Clear: []string{"hostname"}, Raw: ips[0] + " NXDOMAIN"}
		default:
			failed.Add(1)
			rc.Log.Debug("Reverse-DNS ohne Ergebnis", "ip", ips[0], "error", err)
			return nil
		}
		if _, err := rc.Sink.Observe(ctx, obs); err != nil && ctx.Err() == nil {
			rc.Log.Warn("Ergebnis konnte nicht gespeichert werden", "ip", obs.IP, "error", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if n := failed.Load(); n > 0 && answered.Load() == 0 {
		return fmt.Errorf("DNS-Server %s hat auf keine der %d Abfragen geantwortet", addr, n)
	}
	return nil
}
