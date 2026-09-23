// Package arpscan implements the "arpscan" scanner: ARP presence detection in directly
// attached IPv4 networks with the arp-scan binary. Every responding host is written to
// the inventory as soon as its reply line is read.
package arpscan

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"netscope/internal/execx"
	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the arp-scan presence scanner.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "arpscan",
		Kind:               plugin.KindScanner,
		Name:               "ARP-Scan",
		Description:        "Findet aktive Geräte per ARP (arp-scan) in direkt angeschlossenen Netzen und bestimmt ihre Anwesenheit.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "*/5 * * * *",
		DefaultTimeout:     5 * time.Minute,
		DefaultConcurrency: 4,
		DefaultRetries:     1,
		Targets:            plugin.TargetSubnets,
		Presence:           true,
		Binaries:           []string{"arp-scan"},
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "retries", Type: plugin.FieldInt, Label: "Versuche pro Adresse", Default: 2,
			Description: "Wie oft eine Adresse ohne Antwort erneut angefragt wird (arp-scan --retry, Gesamtzahl der Versuche).",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(10)}},
		{Key: "timeout_ms", Type: plugin.FieldInt, Label: "Timeout pro Adresse (ms)", Default: 500,
			Description: "Wartezeit auf die erste Antwort je Adresse in Millisekunden (arp-scan --timeout).",
			Validation:  &plugin.Validation{Min: plugin.Int64(50), Max: plugin.Int64(10000)}},
		{Key: "bandwidth", Type: plugin.FieldString, Label: "Sendebandbreite", Placeholder: "256K", Advanced: true,
			Description: "Maximale Sendebandbreite in Bit/s, z. B. 256K oder 1M (arp-scan --bandwidth). Leer = Standard von arp-scan.",
			Validation:  &plugin.Validation{Pattern: `^[0-9]+[KkMm]?$`}},
		{Key: "ignore_macs", Type: plugin.FieldStringList, Label: "Ignorierte MAC-Adressen", Advanced: true,
			Description: "Antworten dieser MAC-Adressen werden verworfen (eine pro Zeile).",
			Validation:  &plugin.Validation{Format: "mac"}},
	}}
}

type config struct {
	retries   int
	timeoutMS int
	bandwidth string
	ignore    map[string]bool
}

func loadConfig(s plugin.Settings) config {
	c := config{retries: s.Int("retries"), timeoutMS: s.Int("timeout_ms"), bandwidth: s.String("bandwidth"), ignore: map[string]bool{}}
	if c.retries < 1 {
		c.retries = 2
	}
	if c.timeoutMS < 1 {
		c.timeoutMS = 500
	}
	for _, m := range s.StringList("ignore_macs") {
		if n, ok := netutil.NormalizeMAC(m); ok {
			c.ignore[n] = true
		}
	}
	return c
}

// job is one arp-scan invocation on one interface.
type job struct {
	label   string
	iface   string
	targets []string            // arguments for arp-scan: one CIDR or single addresses
	prefix  netip.Prefix        // subnet mode: scanned network
	addrs   map[netip.Addr]bool // device mode: scanned addresses
}

// inScope reports whether a local address belongs to the scanned targets.
func (j job) inScope(a netip.Addr) bool {
	if j.addrs != nil {
		return j.addrs[a]
	}
	return j.prefix.Contains(a)
}

// buildJobs turns the run targets into arp-scan invocations. Targets that cannot be
// scanned are returned as errors (one per subnet or address).
func buildJobs(t plugin.Targets, ifaceFor func(netip.Addr) string) ([]job, []error) {
	var (
		jobs []job
		errs []error
	)
	if t.DeviceMode {
		byIface := map[string]map[netip.Addr]bool{}
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
				errs = append(errs, fmt.Errorf("kein lokales Interface für %s gefunden – arp-scan erreicht nur direkt angeschlossene Netze", a))
				continue
			}
			if byIface[iface] == nil {
				byIface[iface] = map[netip.Addr]bool{}
			}
			byIface[iface][a] = true
		}
		ifaces := make([]string, 0, len(byIface))
		for iface := range byIface {
			ifaces = append(ifaces, iface)
		}
		sort.Strings(ifaces)
		for _, iface := range ifaces {
			addrs := make([]netip.Addr, 0, len(byIface[iface]))
			for a := range byIface[iface] {
				addrs = append(addrs, a)
			}
			sort.Slice(addrs, func(i, j int) bool { return addrs[i].Less(addrs[j]) })
			targets := make([]string, len(addrs))
			for i, a := range addrs {
				targets[i] = a.String()
			}
			jobs = append(jobs, job{label: fmt.Sprintf("%d Geräte über %s", len(addrs), iface), iface: iface, targets: targets, addrs: byIface[iface]})
		}
		return jobs, errs
	}
	seen := map[netip.Prefix]bool{}
	for _, s := range t.Subnets {
		p := s.CIDR.Masked()
		if seen[p] {
			continue
		}
		seen[p] = true
		if !p.Addr().Is4() {
			errs = append(errs, fmt.Errorf("Subnetz %s: arp-scan unterstützt nur IPv4", p))
			continue
		}
		if p.Bits() < 16 {
			errs = append(errs, fmt.Errorf("Subnetz %s ist für arp-scan zu groß (maximal /16)", p))
			continue
		}
		iface := s.Interface
		if iface == "" {
			iface = ifaceFor(p.Addr())
		}
		if iface == "" {
			errs = append(errs, fmt.Errorf("Subnetz %s: kein lokales Interface gefunden – arp-scan erreicht nur direkt angeschlossene Netze (Interface in der Subnetz-Konfiguration eintragen)", p))
			continue
		}
		jobs = append(jobs, job{label: p.String(), iface: iface, targets: []string{p.String()}, prefix: p})
	}
	return jobs, errs
}

// localTargets returns the directly attached networks as targets (used when no subnet
// is configured at all).
func localTargets() plugin.Targets {
	subs, err := netutil.LocalSubnets()
	if err != nil {
		return plugin.Targets{}
	}
	var t plugin.Targets
	for _, s := range subs {
		t.Subnets = append(t.Subnets, plugin.SubnetTarget{CIDR: s.Prefix, Interface: s.Interface})
	}
	return t
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	cfg := loadConfig(rc.Settings)
	targets := rc.Targets
	if !targets.DeviceMode && len(targets.Subnets) == 0 && rc.Scope.AllSubnets {
		targets = localTargets()
		rc.Log.Info("Keine Subnetze im Scope – scanne die lokal angeschlossenen Netze", "anzahl", len(targets.Subnets))
	}
	if targets.DeviceMode && len(targets.DeviceIPs()) == 0 {
		rc.Log.Info("Keine Geräte mit IP-Adresse im Scope")
		return nil
	}
	jobs, jobErrs := buildJobs(targets, netutil.InterfaceFor)
	for _, err := range jobErrs {
		rc.Log.Warn(err.Error())
	}
	if len(jobs) == 0 {
		if len(jobErrs) > 0 {
			return errors.Join(jobErrs...)
		}
		return errors.New("keine scanbaren Netze im Scope")
	}
	var (
		mu     sync.Mutex
		done   int
		failed []error
	)
	rc.Progress(0, len(jobs))
	err := plugin.ForEach(ctx, rc.Parallelism(), jobs, func(ctx context.Context, j job) error {
		n, err := scan(ctx, rc, j, cfg)
		mu.Lock()
		done++
		rc.Progress(done, len(jobs))
		if err != nil && ctx.Err() == nil {
			failed = append(failed, fmt.Errorf("%s: %w", j.label, err))
		}
		mu.Unlock()
		if err != nil {
			if ctx.Err() == nil {
				rc.Log.Warn("arp-scan fehlgeschlagen", "ziel", j.label, "interface", j.iface, "error", err)
				// devices behind a failed scan must not be counted as missed
				if j.prefix.IsValid() {
					rc.PresenceIncomplete(j.prefix)
				} else {
					rc.PresenceIncomplete()
				}
			}
			return nil
		}
		rc.Log.Info("arp-scan abgeschlossen", "ziel", j.label, "interface", j.iface, "hosts", n)
		return nil
	})
	if err != nil {
		return err
	}
	if len(failed) == len(jobs) {
		return errors.Join(append(failed, jobErrs...)...)
	}
	return nil
}

// scan runs arp-scan for one job and writes every responding host immediately.
func scan(ctx context.Context, rc *plugin.RunContext, j job, cfg config) (int, error) {
	args := []string{"--plain", "--numeric", "--interface=" + j.iface,
		"--retry=" + strconv.Itoa(cfg.retries), "--timeout=" + strconv.Itoa(cfg.timeoutMS)}
	if cfg.bandwidth != "" {
		args = append(args, "--bandwidth="+cfg.bandwidth)
	}
	args = append(args, j.targets...)
	found := 0
	first := map[netip.Addr]string{}
	consume := func(r io.Reader) error {
		sc := bufio.NewScanner(r)
		for sc.Scan() {
			line := sc.Text()
			rep, ok := parseLine(line)
			if !ok {
				if strings.TrimSpace(line) != "" {
					rc.Log.Debug("arp-scan-Zeile nicht erkannt", "zeile", line)
				}
				continue
			}
			if mac, dup := first[rep.IP]; dup || rep.Dup > 1 {
				rc.AddStat("duplicates", 1)
				if dup && mac != rep.MAC {
					rc.Log.Warn("Möglicher IP-Adresskonflikt: mehrere MAC-Adressen antworten auf dieselbe IP",
						"ip", rep.IP.String(), "mac", mac, "weitere_mac", rep.MAC)
				} else {
					rc.Log.Debug("Doppelte ARP-Antwort ignoriert", "ip", rep.IP.String(), "mac", rep.MAC)
				}
				continue
			}
			first[rep.IP] = rep.MAC
			if cfg.ignore[rep.MAC] {
				rc.Log.Debug("MAC-Adresse wird ignoriert", "ip", rep.IP.String(), "mac", rep.MAC)
				continue
			}
			if err := observe(ctx, rc, rep.observation()); err != nil {
				return err
			}
			found++
		}
		return sc.Err()
	}
	if err := execx.Stream(ctx, consume, "arp-scan", args...); err != nil {
		if ctx.Err() != nil {
			return found, ctx.Err()
		}
		return found, classifyError(err, j.iface)
	}
	// arp-scan never sees the scanning host itself.
	self, err := localHost(j)
	if err != nil {
		rc.Log.Debug("Lokaler Host nicht ermittelt", "interface", j.iface, "error", err)
	} else if self != nil && (len(self.MACs) == 0 || !cfg.ignore[self.MACs[0]]) {
		if err := observe(ctx, rc, self); err != nil {
			return found, err
		}
		found++
	}
	return found, nil
}

// observe writes one observation. Store errors of single hosts are logged, only a
// cancelled context aborts the scan.
func observe(ctx context.Context, rc *plugin.RunContext, obs *plugin.Observation) error {
	if _, err := rc.Sink.Observe(ctx, obs); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rc.Log.Warn("Ergebnis konnte nicht gespeichert werden", "ip", obs.IP, "error", err)
		return nil
	}
	rc.AddStat("hosts", 1)
	return nil
}

// observation converts a reply into a presence observation.
func (r reply) observation() *plugin.Observation {
	obs := &plugin.Observation{
		IP:      r.IP.String(),
		MACs:    []string{r.MAC},
		Present: true,
		Vendor:  r.Vendor,
		Raw:     r.Line,
	}
	if r.HdrMAC != "" || r.VLAN > 0 {
		obs.Attrs = map[string]string{}
		if r.HdrMAC != "" {
			obs.Attrs["arpscan.hdr_mac"] = r.HdrMAC
		}
		if r.VLAN > 0 {
			obs.Attrs["arpscan.vlan"] = strconv.Itoa(r.VLAN)
		}
	}
	return obs
}

// localHost describes the scanning host on the job's interface if it has an address
// inside the scanned targets (nil otherwise).
func localHost(j job) (*plugin.Observation, error) {
	ifc, err := net.InterfaceByName(j.iface)
	if err != nil {
		return nil, err
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return nil, err
	}
	var ips []string
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip, ok := netip.AddrFromSlice(ipn.IP)
		if !ok {
			continue
		}
		ip = ip.Unmap()
		if ip.Is4() && j.inScope(ip) {
			ips = append(ips, ip.String())
		}
	}
	if len(ips) == 0 {
		return nil, nil
	}
	obs := &plugin.Observation{
		IP:      ips[0],
		IPs:     ips[1:],
		Present: true,
		Attrs:   map[string]string{"netscope.self": "true"},
		Raw:     fmt.Sprintf("lokaler Host auf %s: %s", j.iface, strings.Join(ips, ", ")),
	}
	if mac, ok := netutil.NormalizeMAC(ifc.HardwareAddr.String()); ok {
		obs.MACs = []string{mac}
	}
	if h, err := os.Hostname(); err == nil {
		obs.Hostname = h
	}
	return obs, nil
}

// classifyError turns arp-scan failures into actionable messages.
func classifyError(err error, iface string) error {
	low := strings.ToLower(err.Error())
	switch {
	case strings.Contains(low, "permission") || strings.Contains(low, "cap_net_raw") ||
		strings.Contains(low, "operation not permitted"):
		return fmt.Errorf("arp-scan darf auf %s keine Raw-Sockets öffnen – NetScope muss als root bzw. mit den Capabilities NET_RAW und NET_ADMIN laufen (Docker: cap_add: [NET_RAW, NET_ADMIN]): %w", iface, err)
	case strings.Contains(low, "no such device"):
		return fmt.Errorf("Interface %s existiert nicht: %w", iface, err)
	}
	return err
}
