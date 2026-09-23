// Package icmp implements the "icmp" scanner: ping latency and packet loss per device,
// written as time series (icmp.rtt_ms, icmp.loss_pct) with min/avg/max per run.
package icmp

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// Metric names written by this plugin.
const (
	MetricRTT  = "icmp.rtt_ms"
	MetricLoss = "icmp.loss_pct"
)

// Plugin is the ICMP latency scanner.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "icmp",
		Kind:               plugin.KindScanner,
		Name:               "ICMP-Ping",
		Description:        "Misst Latenz und Paketverlust aller Geräte per Ping und speichert sie als Zeitreihe.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "*/5 * * * *",
		DefaultTimeout:     5 * time.Minute,
		DefaultConcurrency: 32,
		DefaultRetries:     0,
		Targets:            plugin.TargetDevices,
		Presence:           true,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "count", Type: plugin.FieldInt, Label: "Pakete pro Gerät", Default: 3,
			Description: "Anzahl der Echo-Requests je Adresse und Lauf.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(100)}},
		{Key: "interval", Type: plugin.FieldDuration, Label: "Abstand zwischen Paketen", Default: "250ms",
			Description: "Pause zwischen zwei Echo-Requests an dieselbe Adresse.",
			Validation:  &plugin.Validation{Max: plugin.Int64(10)}},
		{Key: "host_timeout", Type: plugin.FieldDuration, Label: "Wartezeit auf Antworten", Default: "3s",
			Description: "Wie lange nach dem letzten Echo-Request noch auf Antworten gewartet wird.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(60)}},
		{Key: "size", Type: plugin.FieldInt, Label: "Nutzdatengröße (Byte)", Default: 56, Advanced: true,
			Description: "Größe der ICMP-Nutzdaten (mindestens 24 Byte).",
			Validation:  &plugin.Validation{Min: plugin.Int64(24), Max: plugin.Int64(1472)}},
		{Key: "all_ips", Type: plugin.FieldBool, Label: "Alle IP-Adressen pingen", Default: false,
			Description: "Aus: nur die primäre IP eines Geräts. An: jede bekannte IP-Adresse (eigene Zeitreihe je IP)."},
		{Key: "privileged", Type: plugin.FieldBool, Label: "Raw-Sockets verwenden", Default: true, Advanced: true,
			Description: "Privilegierter ICMP-Modus (root bzw. NET_RAW). Aus: unprivilegierte ICMP-Datagramm-Sockets, erfordert passendes net.ipv4.ping_group_range."},
	}}
}

type config struct {
	count      int
	interval   time.Duration
	wait       time.Duration
	size       int
	allIPs     bool
	privileged bool
}

func loadConfig(s plugin.Settings) config {
	c := config{count: s.Int("count"), interval: s.Duration("interval"), wait: s.Duration("host_timeout"),
		size: s.Int("size"), allIPs: s.Bool("all_ips"), privileged: s.Bool("privileged")}
	if c.count < 1 {
		c.count = 3
	}
	if c.interval <= 0 {
		c.interval = 250 * time.Millisecond
	}
	if c.wait <= 0 {
		c.wait = 3 * time.Second
	}
	if c.size < 24 {
		c.size = 56
	}
	return c
}

// timeout is the total time budget of one pinger: all packets plus the reply wait.
func (c config) timeout() time.Duration {
	return time.Duration(c.count-1)*c.interval + c.wait
}

// target is one address of one device.
type target struct {
	deviceID int64
	ip       netip.Addr
	key      string // series key: "" for the primary address, the IP for further ones
}

// targetsFor lists the addresses to ping.
func targetsFor(devices []plugin.DeviceInfo, allIPs bool) []target {
	var out []target
	for _, d := range devices {
		ips := []string{d.PrimaryIP}
		if d.PrimaryIP == "" && len(d.IPs) > 0 {
			ips = []string{d.IPs[0]}
		}
		if allIPs {
			ips = append(ips, d.IPs...)
		}
		seen := map[netip.Addr]bool{}
		for i, raw := range ips {
			a, err := netip.ParseAddr(raw)
			if err != nil {
				continue
			}
			a = a.Unmap()
			if seen[a] || a.IsLoopback() || a.IsMulticast() || a.IsUnspecified() || a.IsLinkLocalUnicast() {
				continue
			}
			seen[a] = true
			t := target{deviceID: d.ID, ip: a}
			if i > 0 {
				t.key = a.String()
			}
			out = append(out, t)
		}
	}
	return out
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	cfg := loadConfig(rc.Settings)
	targets := targetsFor(rc.Targets.Devices, cfg.allIPs)
	if len(targets) == 0 {
		rc.Log.Info("Keine Geräte mit IP-Adresse im Scope")
		return nil
	}
	var (
		done, failed atomic.Int64
		errMu        sync.Mutex
		firstErr     error
	)
	rc.Progress(0, len(targets))
	err := plugin.ForEach(ctx, rc.Parallelism(), targets, func(ctx context.Context, t target) error {
		defer func() { rc.Progress(int(done.Add(1)), len(targets)) }()
		st, err := ping(ctx, rc, t.ip, cfg)
		if ctx.Err() != nil {
			return nil
		}
		if err != nil {
			failed.Add(1)
			errMu.Lock()
			if firstErr == nil {
				firstErr = err
			}
			errMu.Unlock()
			rc.Log.Warn("Ping fehlgeschlagen", "ip", t.ip.String(), "error", err)
			return nil
		}
		obs := observation(t.deviceID, t.ip.String(), t.key, st)
		if obs == nil {
			rc.Log.Warn("Keine Echo-Requests gesendet", "ip", t.ip.String())
			return nil
		}
		if obs.Present {
			rc.AddStat("online", 1)
		} else {
			rc.AddStat("offline", 1)
		}
		if _, err := rc.Sink.Observe(ctx, obs); err != nil && ctx.Err() == nil {
			rc.Log.Warn("Ergebnis konnte nicht gespeichert werden", "ip", t.ip.String(), "error", err)
			return nil
		}
		rc.AddStat("hosts", 1)
		return nil
	})
	if err != nil {
		return err
	}
	if n := failed.Load(); n == int64(len(targets)) {
		return fmt.Errorf("kein Ping möglich – für ICMP braucht NetScope root bzw. die Capability NET_RAW (oder „Raw-Sockets verwenden“ aus und passendes net.ipv4.ping_group_range): %w", firstErr)
	}
	return nil
}

// ping sends the configured echo requests to one address.
func ping(ctx context.Context, rc *plugin.RunContext, ip netip.Addr, cfg config) (*probing.Statistics, error) {
	p := probing.New(ip.String())
	if ip.Is4() {
		p.SetNetwork("ip4")
	} else {
		p.SetNetwork("ip6")
	}
	p.SetPrivileged(cfg.privileged)
	p.SetLogger(logger{rc: rc, ip: ip.String()})
	p.Count = cfg.count
	p.Interval = cfg.interval
	p.Timeout = cfg.timeout()
	p.Size = cfg.size
	p.RecordRtts = false
	p.RecordTTLs = false
	if err := p.RunWithContext(ctx); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
		return nil, err
	}
	return p.Statistics(), nil
}

// observation maps ping statistics to an observation (nil if nothing was sent). A host
// with at least one reply is present and gets RTT and loss samples; a silent host only
// gets a 100 % loss sample and is not marked present.
func observation(deviceID int64, ip, key string, st *probing.Statistics) *plugin.Observation {
	if st == nil || st.PacketsSent == 0 {
		return nil
	}
	obs := &plugin.Observation{DeviceID: deviceID, IP: ip}
	if st.PacketsRecv == 0 {
		obs.Metrics = []plugin.Metric{{Name: MetricLoss, Key: key, Unit: "%", Min: 100, Avg: 100, Max: 100}}
		return obs
	}
	loss := min(max(st.PacketLoss, 0), 100)
	obs.Present = true
	obs.Metrics = []plugin.Metric{
		{Name: MetricRTT, Key: key, Unit: "ms", Min: ms(st.MinRtt), Avg: ms(st.AvgRtt), Max: ms(st.MaxRtt)},
		{Name: MetricLoss, Key: key, Unit: "%", Min: loss, Avg: loss, Max: loss},
	}
	return obs
}

// ms converts a duration to milliseconds with microsecond resolution.
func ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}

// logger routes pro-bing's internal messages to the run log at debug level.
type logger struct {
	rc *plugin.RunContext
	ip string
}

func (l logger) Fatalf(format string, v ...any) { l.log(format, v...) }
func (l logger) Errorf(format string, v ...any) { l.log(format, v...) }
func (l logger) Warnf(format string, v ...any)  { l.log(format, v...) }
func (l logger) Infof(format string, v ...any)  { l.log(format, v...) }
func (l logger) Debugf(format string, v ...any) { l.log(format, v...) }

func (l logger) log(format string, v ...any) {
	l.rc.Log.Debug("pro-bing: "+fmt.Sprintf(format, v...), "ip", l.ip)
}
