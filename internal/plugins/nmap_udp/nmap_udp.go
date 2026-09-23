// Package nmapudp implements a separate, infrequent UDP top-ports scan. It reuses the
// nmap XML parser and writes one observation per device.
package nmapudp

import (
	"context"
	"fmt"
	"io"
	"strconv"
	"sync/atomic"
	"time"

	"netscope/internal/execx"
	"netscope/internal/plugin"
	"netscope/internal/plugins/nmap/nmapxml"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the nmap UDP scanner.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "nmap_udp",
		Kind:               plugin.KindScanner,
		Name:               "Nmap (UDP)",
		Description:        "Seltener UDP-Scan der häufigsten Ports bekannter Geräte (langsam, daher standardmäßig wöchentlich).",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "0 4 * * 0",
		DefaultTimeout:     6 * time.Hour,
		DefaultConcurrency: 4,
		DefaultRetries:     0,
		Targets:            plugin.TargetDevices,
		Presence:           false,
		Binaries:           []string{"nmap"},
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "top_ports", Type: plugin.FieldInt, Label: "Anzahl Top-Ports", Default: 50,
			Description: "Wie viele der häufigsten UDP-Ports gescannt werden (1–1000).",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(1000)}},
		{Key: "timing", Type: plugin.FieldEnum, Label: "Timing", Default: "T4", Options: []plugin.Option{
			{Value: "T0", Label: "T0 – paranoid"},
			{Value: "T1", Label: "T1 – heimlich"},
			{Value: "T2", Label: "T2 – höflich"},
			{Value: "T3", Label: "T3 – normal"},
			{Value: "T4", Label: "T4 – aggressiv"},
			{Value: "T5", Label: "T5 – wahnsinnig"},
		}},
		{Key: "service_detection", Type: plugin.FieldBool, Label: "Diensterkennung (-sV)", Default: false,
			Description: "UDP-Diensterkennung ist langsam und wird selten benötigt."},
		{Key: "include_open_filtered", Type: plugin.FieldBool, Label: "open|filtered aufnehmen", Default: false,
			Description: "Auch Ports melden, deren Zustand nicht sicher ist (bei UDP häufig)."},
		{Key: "host_timeout", Type: plugin.FieldDuration, Label: "Host-Timeout", Default: "20m",
			Validation: &plugin.Validation{Min: plugin.Int64(10)}, Advanced: true},
	}}
}

type target struct {
	ip       string
	deviceID int64
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	topPorts := rc.Settings.Int("top_ports")
	if topPorts < 1 {
		topPorts = 50
	}
	timing := rc.Settings.String("timing")
	if timing == "" {
		timing = "T4"
	}
	serviceDet := rc.Settings.Bool("service_detection")
	includeOF := rc.Settings.Bool("include_open_filtered")
	hostTimeout := durationArg(rc.Settings.Duration("host_timeout"))

	var targets []target
	seen := map[string]bool{}
	for _, d := range rc.Targets.Devices {
		for _, ip := range append([]string{d.PrimaryIP}, d.IPs...) {
			if ip != "" && !seen[ip] {
				seen[ip] = true
				targets = append(targets, target{ip: ip, deviceID: d.ID})
			}
		}
	}
	if len(targets) == 0 {
		rc.Log.Info("keine Geräte für den UDP-Scan")
		return nil
	}
	rc.SetStat("hosts", len(targets))

	var done, portsFound int64
	rc.Progress(0, len(targets))
	return plugin.ForEach(ctx, rc.Parallelism(), targets, func(ctx context.Context, t target) error {
		obs, n, err := p.scan(ctx, rc, t, topPorts, timing, hostTimeout, serviceDet, includeOF)
		d := atomic.AddInt64(&done, 1)
		rc.Progress(int(d), len(targets))
		if err != nil {
			rc.Log.Warn("UDP-Scan fehlgeschlagen", "ip", t.ip, "err", err)
			return nil
		}
		if obs == nil {
			return nil
		}
		atomic.AddInt64(&portsFound, int64(n))
		rc.SetStat("ports", int(atomic.LoadInt64(&portsFound)))
		if _, err := rc.Sink.Observe(ctx, obs); err != nil {
			rc.Log.Warn("Beobachtung fehlgeschlagen", "ip", t.ip, "err", err)
		}
		return nil
	})
}

func (p *Plugin) scan(ctx context.Context, rc *plugin.RunContext, t target, topPorts int, timing, hostTimeout string,
	serviceDet, includeOF bool) (*plugin.Observation, int, error) {
	args := []string{"-sU", "--top-ports", strconv.Itoa(topPorts), "-n", "-oX", "-", "-Pn", "-" + timing}
	if serviceDet {
		args = append(args, "-sV")
	}
	if hostTimeout != "" {
		args = append(args, "--host-timeout", hostTimeout)
	}
	args = append(args, t.ip)

	var (
		obs    *plugin.Observation
		nports int
	)
	err := execx.Stream(ctx, func(stdout io.Reader) error {
		_, perr := nmapxml.Parse(stdout, func(run *nmapxml.Run, h *nmapxml.Host) error {
			obs, nports = buildUDPObservation(run, h, t.deviceID, t.ip, includeOF)
			if h.TimedOut {
				rc.Log.Warn("UDP-Scan im Timeout", "ip", t.ip)
			}
			return nil
		})
		return perr
	}, "nmap", args...)
	if err != nil {
		return nil, 0, err
	}
	return obs, nports, nil
}

// buildUDPObservation turns a scanned host into a UDP port observation.
func buildUDPObservation(run *nmapxml.Run, h *nmapxml.Host, deviceID int64, ip string, includeOF bool) (*plugin.Observation, int) {
	ports := make([]plugin.Port, 0, len(h.Ports))
	for _, p := range h.Ports {
		open := p.State.State == "open"
		openFiltered := p.State.State == "open|filtered"
		if !open && !(includeOF && openFiltered) {
			continue
		}
		port := plugin.Port{Port: p.PortID, Proto: "udp", State: p.State.State}
		if p.Service != nil {
			port.Service = p.Service.Name
			port.Product = p.Service.Product
			port.Version = p.Service.Version
			port.ExtraInfo = p.Service.ExtraInfo
		}
		ports = append(ports, port)
	}
	scan := &plugin.PortScan{Protocol: "udp", Ports: ports}
	if !h.TimedOut {
		scan.Scanned = run.Scanned("udp")
	}
	obs := &plugin.Observation{
		DeviceID: deviceID,
		IP:       ip,
		Target:   ip,
		Ports:    scan,
		Raw:      h.Raw,
	}
	return obs, len(ports)
}

func durationArg(d time.Duration) string {
	if d <= 0 {
		return ""
	}
	return fmt.Sprintf("%ds", int64(d/time.Second))
}
