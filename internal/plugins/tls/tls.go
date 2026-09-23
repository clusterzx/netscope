// Package tls implements the TLS scanner: it collects the leaf certificate and the TLS
// configuration (versions, ciphers, chain validity) of every TLS port of a device.
//
// SSLv3 cannot be tested because the Go TLS stack does not support it; only TLS 1.0–1.3
// are probed.
package tls

import (
	"context"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the TLS scanner.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:                 "tls",
		Kind:               plugin.KindScanner,
		Name:               "TLS-Zertifikate",
		Description:        "Erfasst Zertifikate (CN/SAN, Aussteller, Gültigkeit, selbstsigniert) und die TLS-Konfiguration je Port. SSLv3 kann nicht geprüft werden.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "45 3 * * *",
		DefaultTimeout:     30 * time.Minute,
		DefaultConcurrency: 16,
		DefaultRetries:     0,
		Targets:            plugin.TargetDevices,
		Presence:           false,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "use_scanned_ports", Type: plugin.FieldBool, Label: "Bekannte Ports verwenden", Default: true,
			Description: "TLS-Ports aus den gescannten Diensten übernehmen (tunnel ssl, https, imaps, …)."},
		{Key: "extra_ports", Type: plugin.FieldStringList, Label: "Zusätzliche Ports", Default: []string{"443", "8443"},
			Validation: &plugin.Validation{Pattern: `^[0-9]{1,5}$`}},
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Timeout je Verbindung", Default: "5s",
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(120)}},
		{Key: "sni", Type: plugin.FieldEnum, Label: "SNI (Servername)", Default: "hostname", Options: []plugin.Option{
			{Value: "hostname", Label: "Hostname des Geräts senden"},
			{Value: "none", Label: "Kein SNI"},
		}},
		{Key: "check_weak", Type: plugin.FieldBool, Label: "Schwache Protokolle/Cipher prüfen", Default: true,
			Description: "Testet unterstützte TLS-Versionen und unsichere Cipher (zusätzliche Handshakes)."},
		{Key: "starttls", Type: plugin.FieldBool, Label: "STARTTLS verwenden", Default: false,
			Description: "STARTTLS für SMTP (25/587), IMAP (143), POP3 (110) und FTP (21)."},
	}}
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	var errs []plugin.FieldError
	for _, ep := range s.StringList("extra_ports") {
		if n, err := strconv.Atoi(ep); err != nil || n < 1 || n > 65535 {
			errs = append(errs, plugin.FieldError{Field: "extra_ports", Message: "ungültiger Port " + ep})
		}
	}
	if len(errs) > 0 {
		return &plugin.ValidationError{Errors: errs}
	}
	return nil
}

type endpoint struct {
	ip       string
	port     int
	starttls string // "" | smtp | imap | pop3 | ftp
}

type config struct {
	useScanned bool
	extraPorts []int
	timeout    time.Duration
	sni        string
	checkWeak  bool
	starttls   bool
}

func loadConfig(rc *plugin.RunContext) config {
	c := config{
		useScanned: rc.Settings.Bool("use_scanned_ports"),
		timeout:    rc.Settings.Duration("timeout"),
		sni:        rc.Settings.String("sni"),
		checkWeak:  rc.Settings.Bool("check_weak"),
		starttls:   rc.Settings.Bool("starttls"),
	}
	if c.timeout <= 0 {
		c.timeout = 5 * time.Second
	}
	if c.sni == "" {
		c.sni = "hostname"
	}
	for _, ep := range rc.Settings.StringList("extra_ports") {
		if n, err := strconv.Atoi(ep); err == nil && n >= 1 && n <= 65535 {
			c.extraPorts = append(c.extraPorts, n)
		}
	}
	return c
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	cfg := loadConfig(rc)
	devices := rc.Targets.Devices
	if len(devices) == 0 {
		rc.Log.Info("keine Geräte für TLS-Scan")
		return nil
	}
	var done, certsFound int64
	rc.Progress(0, len(devices))
	return plugin.ForEach(ctx, rc.Parallelism(), devices, func(ctx context.Context, d plugin.DeviceInfo) error {
		n := p.scanDevice(ctx, rc, d, cfg)
		atomic.AddInt64(&certsFound, int64(n))
		cur := atomic.AddInt64(&done, 1)
		rc.Progress(int(cur), len(devices))
		rc.SetStat("certs", int(atomic.LoadInt64(&certsFound)))
		return nil
	})
}

func (p *Plugin) scanDevice(ctx context.Context, rc *plugin.RunContext, d plugin.DeviceInfo, cfg config) int {
	byIP := endpointsByIP(d, cfg)
	serverName := ""
	if cfg.sni == "hostname" {
		serverName = strings.TrimSuffix(d.Hostname, ".")
	}
	total := 0
	for _, ip := range sortedKeys(byIP) {
		scan := &plugin.TLSScan{}
		for _, ep := range byIP[ip] {
			scan.Scanned = append(scan.Scanned, ep.port)
			cert, err := p.probe(ctx, ep, cfg, serverName)
			if err != nil {
				rc.Log.Debug("kein TLS", "ip", ep.ip, "port", ep.port, "err", err)
				continue
			}
			scan.Certs = append(scan.Certs, *cert)
		}
		sortInts(scan.Scanned)
		obs := &plugin.Observation{DeviceID: d.ID, IP: ip, Target: ip, TLS: scan, Present: len(scan.Certs) > 0}
		total += len(scan.Certs)
		if _, err := rc.Sink.Observe(ctx, obs); err != nil {
			rc.Log.Warn("Beobachtung fehlgeschlagen", "ip", ip, "err", err)
		}
	}
	return total
}

// endpointsByIP collects the TLS endpoints of a device grouped by IP.
func endpointsByIP(d plugin.DeviceInfo, cfg config) map[string][]endpoint {
	out := map[string]map[int]endpoint{}
	add := func(ip string, port int, starttls string) {
		if ip == "" || port < 1 || port > 65535 {
			return
		}
		if out[ip] == nil {
			out[ip] = map[int]endpoint{}
		}
		if _, ok := out[ip][port]; !ok {
			out[ip][port] = endpoint{ip: ip, port: port, starttls: starttls}
		}
	}
	if cfg.useScanned {
		for _, port := range d.Ports {
			if port.Proto != "" && !strings.EqualFold(port.Proto, "tcp") {
				continue
			}
			if isTLSService(port) {
				add(port.IP, port.Port, "")
			} else if cfg.starttls {
				if st := starttlsProto(port.Port, port.Service); st != "" {
					add(port.IP, port.Port, st)
				}
			}
		}
	}
	primary := d.PrimaryIP
	if primary == "" && len(d.IPs) > 0 {
		primary = d.IPs[0]
	}
	for _, port := range cfg.extraPorts {
		add(primary, port, "")
	}
	grouped := map[string][]endpoint{}
	for ip, ports := range out {
		for _, e := range ports {
			grouped[ip] = append(grouped[ip], e)
		}
	}
	return grouped
}

var tlsServices = map[string]bool{
	"https": true, "https-alt": true, "imaps": true, "pop3s": true, "smtps": true,
	"ldaps": true, "ftps": true, "ftps-data": true, "submissions": true, "ssl": true,
}

var tlsPortHints = map[int]bool{443: true, 8443: true, 9443: true, 8006: true, 8843: true, 993: true, 995: true, 465: true, 636: true, 990: true}

func isTLSService(p plugin.PortRef) bool {
	svc := strings.ToLower(p.Service)
	if p.Tunnel == "ssl" {
		return true
	}
	if tlsServices[svc] || strings.HasPrefix(svc, "ssl/") {
		return true
	}
	return tlsPortHints[p.Port]
}

// starttlsProto returns the STARTTLS protocol to use for a plaintext service port.
func starttlsProto(port int, service string) string {
	svc := strings.ToLower(service)
	switch {
	case port == 25 || port == 587 || svc == "smtp" || svc == "submission":
		return "smtp"
	case port == 143 || svc == "imap":
		return "imap"
	case port == 110 || svc == "pop3":
		return "pop3"
	case port == 21 || svc == "ftp":
		return "ftp"
	}
	return ""
}

func sortedKeys(m map[string][]endpoint) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func sortInts(s []int) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}
