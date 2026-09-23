// Package nmap implements the TCP port/service/version/OS scanner. It runs a host
// discovery pass (or uses the known devices of the scope) and then scans every host in
// its own nmap process, writing one observation per host as soon as it finishes.
package nmap

import (
	"context"
	"fmt"
	"io"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"netscope/internal/execx"
	"netscope/internal/plugin"
	"netscope/internal/plugins/nmap/nmapxml"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the nmap TCP scanner.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "nmap",
		Kind:               plugin.KindScanner,
		Name:               "Nmap (TCP)",
		Description:        "Scannt TCP-Ports, erkennt Dienste, Versionen und Betriebssystem und liest CPE-Kennungen aus.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "0 3 * * *",
		DefaultTimeout:     3 * time.Hour,
		DefaultConcurrency: 4,
		DefaultRetries:     0,
		Targets:            plugin.TargetSubnets,
		Presence:           true,
		Binaries:           []string{"nmap"},
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "ports", Type: plugin.FieldString, Label: "Ports", Default: "top-1000",
			Description: "\"top-N\" (häufigste Ports), \"all\" (alle 65535) oder eine Portliste wie 22,80,443,8000-8100.",
			Placeholder: "top-1000"},
		{Key: "timing", Type: plugin.FieldEnum, Label: "Timing", Default: "T4", Options: []plugin.Option{
			{Value: "T0", Label: "T0 – paranoid (sehr langsam)"},
			{Value: "T1", Label: "T1 – heimlich"},
			{Value: "T2", Label: "T2 – höflich"},
			{Value: "T3", Label: "T3 – normal"},
			{Value: "T4", Label: "T4 – aggressiv (empfohlen im LAN)"},
			{Value: "T5", Label: "T5 – wahnsinnig (unzuverlässig)"},
		}},
		{Key: "service_detection", Type: plugin.FieldBool, Label: "Diensterkennung (-sV)", Default: true,
			Description: "Ermittelt Produkt und Version je offenem Port."},
		{Key: "version_intensity", Type: plugin.FieldInt, Label: "Erkennungsintensität", Default: 7,
			Description: "0 (schnell) bis 9 (gründlich). Nur bei aktiver Diensterkennung.",
			Validation:  &plugin.Validation{Min: plugin.Int64(0), Max: plugin.Int64(9)},
			VisibleIf:   &plugin.Condition{Field: "service_detection", Equals: []any{true}}},
		{Key: "os_detection", Type: plugin.FieldBool, Label: "OS-Erkennung (-O)", Default: true,
			Description: "Rät das Betriebssystem (--osscan-guess). Benötigt Root-Rechte im Container."},
		{Key: "min_os_accuracy", Type: plugin.FieldInt, Label: "Mindestgenauigkeit OS", Default: 85,
			Description: "OS-Treffer unter dieser Genauigkeit (0–100) werden verworfen.",
			Validation:  &plugin.Validation{Min: plugin.Int64(0), Max: plugin.Int64(100)}, Advanced: true},
		{Key: "host_timeout", Type: plugin.FieldDuration, Label: "Host-Timeout", Default: "15m",
			Description: "Bricht den Scan eines einzelnen Hosts nach dieser Dauer ab.",
			Validation:  &plugin.Validation{Min: plugin.Int64(10)}, Advanced: true},
		{Key: "discovery", Type: plugin.FieldEnum, Label: "Host-Erkennung", Default: "ping", Options: []plugin.Option{
			{Value: "ping", Label: "Ping-Scan (nmap -sn) vorab"},
			{Value: "known", Label: "Nur bekannte Geräte des Scopes (-Pn)"},
		}, Description: "Wie die zu scannenden Hosts bestimmt werden. Bei Geräte-Scope immer nur die Geräte."},
		{Key: "exclude", Type: plugin.FieldSubnetList, Label: "Ausschlüsse", Advanced: true,
			Description: "Adressen oder Subnetze (CIDR), die nicht gescannt werden."},
	}}
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	if _, err := portsToArgs(s.String("ports")); err != nil {
		return &plugin.ValidationError{Errors: []plugin.FieldError{{Field: "ports", Message: err.Error()}}}
	}
	return nil
}

type scanTarget struct {
	ip       string
	deviceID int64
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	portArgs, err := portsToArgs(rc.Settings.String("ports"))
	if err != nil {
		return err
	}
	timing := rc.Settings.String("timing")
	if timing == "" {
		timing = "T4"
	}
	minOSAcc := rc.Settings.Int("min_os_accuracy")
	serviceDet := rc.Settings.Bool("service_detection")
	osDet := rc.Settings.Bool("os_detection")
	hostTimeout := durationArg(rc.Settings.Duration("host_timeout"))
	excludes := rc.Settings.Prefixes("exclude")

	privileged := hasRawSocketPrivilege()
	if !privileged {
		rc.Log.Warn("nmap läuft ohne Root-Rechte – Fallback auf TCP-Connect-Scan (-sT), keine OS-Erkennung")
	}

	targets, err := p.targets(ctx, rc, timing, excludes)
	if err != nil {
		return err
	}
	targets = filterExcluded(targets, excludes)
	if len(targets) == 0 {
		rc.Log.Info("keine Ziele für den Scan")
		return nil
	}
	rc.SetStat("hosts", len(targets))

	var done int64
	var portsFound, hostsUp int64
	rc.Progress(0, len(targets))

	err = plugin.ForEach(ctx, rc.Parallelism(), targets, func(ctx context.Context, t scanTarget) error {
		obs, up, nports, err := p.scanHost(ctx, rc, t, portArgs, timing, hostTimeout, serviceDet, osDet, minOSAcc, privileged)
		n := atomic.AddInt64(&done, 1)
		rc.Progress(int(n), len(targets))
		if err != nil {
			rc.Log.Warn("Host-Scan fehlgeschlagen", "ip", t.ip, "err", err)
			return nil
		}
		if obs == nil {
			return nil
		}
		if up {
			atomic.AddInt64(&hostsUp, 1)
		}
		atomic.AddInt64(&portsFound, int64(nports))
		if _, err := rc.Sink.Observe(ctx, obs); err != nil {
			rc.Log.Warn("Beobachtung fehlgeschlagen", "ip", t.ip, "err", err)
		}
		return nil
	})
	rc.SetStat("hosts_up", int(atomic.LoadInt64(&hostsUp)))
	rc.SetStat("ports", int(atomic.LoadInt64(&portsFound)))
	return err
}

// targets resolves the hosts to scan for this run.
func (p *Plugin) targets(ctx context.Context, rc *plugin.RunContext, timing string, excludes []netip.Prefix) ([]scanTarget, error) {
	// Device scope (or discovery=known): scan the known devices only.
	if rc.Targets.DeviceMode || rc.Settings.String("discovery") == "known" {
		return deviceTargets(rc.Targets.Devices), nil
	}
	// Subnet scope with ping discovery.
	var cidrs []string
	for _, sn := range rc.Targets.Subnets {
		cidrs = append(cidrs, sn.CIDR.String())
	}
	if len(cidrs) == 0 {
		return nil, nil
	}
	return p.discover(ctx, rc, timing, cidrs, excludes)
}

func deviceTargets(devs []plugin.DeviceInfo) []scanTarget {
	var out []scanTarget
	seen := map[string]bool{}
	for _, d := range devs {
		for _, ip := range append([]string{d.PrimaryIP}, d.IPs...) {
			if ip != "" && !seen[ip] {
				seen[ip] = true
				out = append(out, scanTarget{ip: ip, deviceID: d.ID})
			}
		}
	}
	return out
}

// discover runs `nmap -sn` and observes every host that is up. It returns the up hosts as
// scan targets.
func (p *Plugin) discover(ctx context.Context, rc *plugin.RunContext, timing string, cidrs []string, excludes []netip.Prefix) ([]scanTarget, error) {
	args := []string{"-sn", "-n", "-oX", "-", "-" + timing}
	if ex := excludeArg(excludes); ex != "" {
		args = append(args, "--exclude", ex)
	}
	args = append(args, cidrs...)

	var (
		targets []scanTarget
		mu      sync.Mutex
		count   int
	)
	err := execx.Stream(ctx, func(stdout io.Reader) error {
		_, perr := nmapxml.Parse(stdout, func(_ *nmapxml.Run, h *nmapxml.Host) error {
			if !h.Up() {
				return nil
			}
			ip := h.IPv4()
			if ip == "" {
				return nil
			}
			obs := &plugin.Observation{IP: ip, Target: ip, Present: true, Raw: h.Raw}
			if mac, vendor := h.MAC(); mac != "" {
				obs.MACs = []string{mac}
				obs.Vendor = vendor
			}
			if _, err := rc.Sink.Observe(ctx, obs); err != nil {
				rc.Log.Warn("Discovery-Beobachtung fehlgeschlagen", "ip", ip, "err", err)
			}
			mu.Lock()
			targets = append(targets, scanTarget{ip: ip})
			count++
			mu.Unlock()
			return nil
		})
		return perr
	}, "nmap", args...)
	if err != nil {
		return nil, fmt.Errorf("Host-Erkennung: %w", err)
	}
	rc.Log.Info("Host-Erkennung abgeschlossen", "hosts", count)
	return targets, nil
}

// scanHost scans a single host and returns its observation, whether it is up and the
// number of open ports found.
func (p *Plugin) scanHost(ctx context.Context, rc *plugin.RunContext, t scanTarget, portArgs []string, timing, hostTimeout string,
	serviceDet, osDet bool, minOSAcc int, privileged bool) (*plugin.Observation, bool, int, error) {
	args := []string{"-oX", "-", "-n", "-Pn"}
	args = append(args, portArgs...)
	args = append(args, "-"+timing)
	if !privileged {
		args = append(args, "-sT")
	}
	if serviceDet {
		args = append(args, "-sV")
		if vi := rc.Settings.Int("version_intensity"); vi >= 0 {
			args = append(args, "--version-intensity", strconv.Itoa(vi))
		}
	}
	if osDet && privileged {
		args = append(args, "-O", "--osscan-guess")
	}
	if hostTimeout != "" {
		args = append(args, "--host-timeout", hostTimeout)
	}
	args = append(args, t.ip)

	var (
		obs    *plugin.Observation
		up     bool
		nports int
	)
	err := execx.Stream(ctx, func(stdout io.Reader) error {
		_, perr := nmapxml.Parse(stdout, func(run *nmapxml.Run, h *nmapxml.Host) error {
			present := hostPresent(h)
			o := buildObservation(run, h, minOSAcc, present)
			o.DeviceID = t.deviceID
			up = h.Up()
			if o.Ports != nil {
				nports = len(o.Ports.Ports)
			}
			if h.TimedOut {
				rc.Log.Warn("Host-Scan im Timeout", "ip", t.ip)
			}
			obs = o
			return nil
		})
		return perr
	}, "nmap", args...)
	if err != nil {
		return nil, false, 0, err
	}
	return obs, up, nports, nil
}

// hostPresent reports whether a host actively answered (rather than being assumed up by
// -Pn without any response), so presence tracking stays honest.
func hostPresent(h *nmapxml.Host) bool {
	if !h.Up() {
		return false
	}
	if h.Status.Reason != "" && h.Status.Reason != "user-set" && h.Status.Reason != "localhost-response" {
		return true
	}
	for _, port := range h.Ports {
		if port.State.State == "open" || port.State.State == "open|filtered" {
			return true
		}
	}
	return h.Status.Reason == "localhost-response"
}

func durationArg(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return strconv.FormatInt(int64(d/time.Second), 10) + "s"
}

func excludeArg(prefixes []netip.Prefix) string {
	var parts []string
	for _, p := range prefixes {
		parts = append(parts, p.String())
	}
	return strings.Join(parts, ",")
}

func filterExcluded(targets []scanTarget, excludes []netip.Prefix) []scanTarget {
	if len(excludes) == 0 {
		return targets
	}
	var out []scanTarget
	for _, t := range targets {
		a, err := netip.ParseAddr(t.ip)
		if err != nil {
			out = append(out, t)
			continue
		}
		skip := false
		for _, p := range excludes {
			if p.Contains(a) {
				skip = true
				break
			}
		}
		if !skip {
			out = append(out, t)
		}
	}
	return out
}
