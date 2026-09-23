// Package ssh is the Linux inventory scanner: it logs into known devices with vault
// credentials and runs a fixed, read-only command script (see script.go) to collect OS,
// hardware, network, packages, services, sockets and docker containers.
package ssh

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/sshx"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin implements the ssh scanner.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "ssh",
		Kind: plugin.KindScanner,
		Name: "SSH-Inventar",
		Description: "Liest per SSH Betriebssystem, Hardware, Netzwerk, Pakete, Dienste, lauschende Sockets und " +
			"Docker-Container von Linux-Hosts aus – ausschließlich mit fest definierten Lesekommandos.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultSchedule:    "0 5 * * *",
		DefaultTimeout:     30 * time.Minute,
		DefaultConcurrency: 8,
		DefaultRetries:     1,
		Targets:            plugin.TargetDevices,
		Presence:           false,
	}
}

// Host key policies.
const (
	hostKeyTOFU     = "tofu"
	hostKeyInsecure = "insecure"
)

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "credentials", Type: plugin.FieldCredentialRef, Label: "Zugangsdaten", Multi: true, Required: true,
			CredentialTypes: []string{plugin.CredSSH, plugin.CredPassword}, Group: "Verbindung",
			Description: "Werden der Reihe nach probiert, bis eine Anmeldung klappt. Das funktionierende Credential wird pro Gerät gemerkt und beim nächsten Lauf zuerst verwendet."},
		{Key: "port", Type: plugin.FieldInt, Label: "SSH-Port", Default: 22, Group: "Verbindung",
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(65535)}},
		{Key: "require_open_port", Type: plugin.FieldBool, Label: "Nur Geräte mit offenem SSH-Port", Default: true, Group: "Verbindung",
			Description: "Geräte überspringen, deren bekannte offene Ports den SSH-Port nicht enthalten. Geräte ohne Portdaten werden trotzdem versucht."},
		{Key: "command_timeout", Type: plugin.FieldDuration, Label: "Zeitlimit pro Kommando", Default: "20s", Group: "Verbindung",
			Description: "Gilt für den Verbindungsaufbau und jedes einzelne Kommando auf dem Zielsystem.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(600)}},
		{Key: "collect_packages", Type: plugin.FieldBool, Label: "Installierte Pakete erfassen", Default: true, Group: "Umfang",
			Description: "dpkg, rpm oder apk – je nachdem, was vorhanden ist."},
		{Key: "collect_docker", Type: plugin.FieldBool, Label: "Docker-Container erfassen", Default: true, Group: "Umfang",
			Description: "Nur wenn docker vorhanden ist und der Benutzer darauf zugreifen darf."},
		{Key: "host_key_policy", Type: plugin.FieldEnum, Label: "Hostschlüssel-Prüfung", Default: hostKeyTOFU, Group: "Sicherheit",
			Options: []plugin.Option{
				{Value: hostKeyTOFU, Label: "Beim ersten Kontakt merken, danach prüfen (TOFU)"},
				{Value: hostKeyInsecure, Label: "Nicht prüfen (unsicher)"},
			},
			Description: "Bei TOFU wird ein geänderter Hostschlüssel abgelehnt (möglicher Man-in-the-Middle)."},
		{Key: "max_output_mb", Type: plugin.FieldInt, Label: "Maximale Ausgabe pro Gerät (MB)", Default: 16, Group: "Sicherheit", Advanced: true,
			Description: "Längere Ausgaben werden abgeschnitten; unvollständige Paket- oder Containerlisten werden dann nicht übernommen.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(256)}},
	}}
}

type config struct {
	credIDs         []int64
	port            int
	requireOpenPort bool
	commandTimeout  time.Duration
	packages        bool
	docker          bool
	knownHosts      string // "" disables host key checking
	maxOutput       int
}

func loadConfig(s plugin.Settings, dataDir string) config {
	c := config{
		credIDs:         s.CredentialIDs("credentials"),
		port:            s.Int("port"),
		requireOpenPort: s.Bool("require_open_port"),
		commandTimeout:  s.Duration("command_timeout"),
		packages:        s.Bool("collect_packages"),
		docker:          s.Bool("collect_docker"),
		maxOutput:       s.Int("max_output_mb") << 20,
	}
	if c.port < 1 || c.port > 65535 {
		c.port = 22
	}
	if c.commandTimeout <= 0 {
		c.commandTimeout = 20 * time.Second
	}
	if c.maxOutput <= 0 {
		c.maxOutput = 16 << 20
	}
	if s.String("host_key_policy") != hostKeyInsecure {
		c.knownHosts = filepath.Join(dataDir, "known_hosts")
	}
	return c
}

// scriptTimeout bounds one script run: every command is limited by timeout(1) on the
// host; this is the safety net when timeout(1) does not exist.
func (c config) scriptTimeout() time.Duration {
	return c.commandTimeout * time.Duration(len(commands)+2)
}

type target struct {
	dev plugin.DeviceInfo
	ip  string
}

// planTargets selects the address to connect to and applies require_open_port.
func planTargets(devices []plugin.DeviceInfo, port int, requireOpen bool) (targets []target, skipped int) {
	for _, d := range devices {
		ip := d.PrimaryIP
		if ip == "" && len(d.IPs) > 0 {
			ip = d.IPs[0]
		}
		if len(d.Ports) > 0 {
			open := ""
			for _, pr := range d.Ports {
				if pr.Proto == "tcp" && pr.Port == port {
					if open == "" || pr.IP == d.PrimaryIP {
						open = pr.IP
					}
				}
			}
			if open != "" {
				ip = open
			} else if requireOpen {
				skipped++
				continue
			}
		}
		if ip == "" {
			skipped++
			continue
		}
		targets = append(targets, target{dev: d, ip: ip})
	}
	return targets, skipped
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	cfg := loadConfig(rc.Settings, rc.DataDir)
	if len(cfg.credIDs) == 0 {
		return plugin.ErrNoCredential
	}
	creds := loadCredentials(ctx, rc, cfg.credIDs)
	if len(creds) == 0 {
		return errors.New("keines der konfigurierten Credentials ist verwendbar")
	}
	var subnets []netip.Prefix
	if rc.Inventory != nil {
		if list, err := rc.Inventory.Subnets(ctx); err != nil {
			rc.Log.Warn("Subnetze nicht lesbar – es werden keine weiteren IP-Adressen übernommen", "error", err)
		} else {
			for _, sn := range list {
				subnets = append(subnets, sn.CIDR)
			}
		}
	}
	store, err := loadCredStore(filepath.Join(rc.DataDir, "credentials.json"))
	if err != nil {
		rc.Log.Warn("gemerkte Credentials nicht lesbar – beginne neu", "error", err)
	}
	targets, skipped := planTargets(rc.Targets.Devices, cfg.port, cfg.requireOpenPort)
	rc.SetStat("targets", len(targets))
	rc.SetStat("skipped", skipped)
	if skipped > 0 {
		rc.Log.Info("Geräte ohne offenen SSH-Port übersprungen", "count", skipped, "port", cfg.port)
	}
	script := "sh -c " + sshx.ShellQuote(buildScript(scriptOptions{Packages: cfg.packages, Docker: cfg.docker, CommandTimeout: cfg.commandTimeout}))
	var (
		ok, failed atomic.Int64
		mu         sync.Mutex
		done       int
	)
	rc.Progress(0, len(targets))
	runErr := plugin.ForEach(ctx, rc.Parallelism(), targets, func(ctx context.Context, t target) error {
		err := p.scan(ctx, rc, cfg, creds, store, subnets, script, t)
		switch {
		case err == nil:
			ok.Add(1)
			rc.AddStat("scanned", 1)
		case ctx.Err() != nil:
		default:
			failed.Add(1)
			rc.AddStat("failed", 1)
			rc.Log.Warn("SSH-Inventar fehlgeschlagen", "device", t.dev.Name, "device_id", t.dev.ID, "ip", t.ip, "error", err)
		}
		mu.Lock()
		done++
		rc.Progress(done, len(targets))
		mu.Unlock()
		return nil
	})
	if err := store.save(); err != nil {
		rc.Log.Warn("gemerkte Credentials nicht speicherbar", "error", err)
	}
	if runErr != nil {
		return runErr
	}
	rc.Log.Info("SSH-Inventar abgeschlossen", "ok", ok.Load(), "failed", failed.Load(), "skipped", skipped)
	if ok.Load() == 0 && failed.Load() > 0 {
		return fmt.Errorf("kein Gerät per SSH inventarisiert (%d fehlgeschlagen)", failed.Load())
	}
	return nil
}

// loadCredentials decrypts the configured credentials and drops unusable ones.
func loadCredentials(ctx context.Context, rc *plugin.RunContext, ids []int64) []*plugin.Credential {
	var out []*plugin.Credential
	for _, id := range ids {
		c, err := rc.Creds.Get(ctx, id)
		if err != nil {
			rc.Log.Warn("Credential nicht verfügbar", "credential_id", id, "error", err)
			continue
		}
		if _, _, err := sshx.AuthMethods(c); err != nil {
			rc.Log.Warn("Credential unbrauchbar", "credential", c.Name, "error", err)
			continue
		}
		out = append(out, c)
	}
	return out
}

// isAuthError reports whether a dial error means "credential rejected" (try the next
// one) rather than a network or host key problem.
func isAuthError(err error) bool {
	s := err.Error()
	return strings.Contains(s, "unable to authenticate") || strings.Contains(s, "no supported methods remain")
}

// scan inventories one device.
func (p *Plugin) scan(ctx context.Context, rc *plugin.RunContext, cfg config, creds []*plugin.Credential, store *credStore,
	subnets []netip.Prefix, script string, t target) error {
	ordered := orderedCredentials(store.get(t.dev.ID), creds, func(c *plugin.Credential) int64 { return c.ID })
	var (
		client   *sshx.Client
		used     *plugin.Credential
		rejected []string
	)
	for _, c := range ordered {
		cl, err := sshx.Dial(ctx, t.ip, c, sshx.Options{Port: cfg.port, Timeout: cfg.commandTimeout, KnownHosts: cfg.knownHosts})
		if err == nil {
			client, used = cl, c
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if !isAuthError(err) {
			return err
		}
		rejected = append(rejected, c.Name)
	}
	if client == nil {
		store.set(t.dev.ID, 0)
		return fmt.Errorf("Anmeldung abgelehnt für %s", strings.Join(rejected, ", "))
	}
	defer client.Close()
	store.set(t.dev.ID, used.ID)

	runCtx, cancel := context.WithTimeout(ctx, cfg.scriptTimeout())
	defer cancel()
	res, err := client.Run(runCtx, script, cfg.maxOutput)
	if err != nil {
		return fmt.Errorf("Inventar-Skript: %w", err)
	}
	result := parseResult(res.Stdout, res.Stderr, res.Truncated)
	if result.Sections == 0 {
		return fmt.Errorf("Inventar-Skript lieferte keine Daten (Exit-Code %d): %s", res.ExitCode, firstLine(string(res.Stderr)))
	}
	if res.Truncated {
		rc.Log.Warn("Ausgabe gekürzt – Limit erreicht", "device", t.dev.Name, "limit_mb", cfg.maxOutput>>20)
	}
	for sec, msg := range result.Inventory.Errors {
		rc.Log.Debug("Abschnitt fehlerhaft", "device", t.dev.Name, "section", sec, "error", msg)
	}
	obs := result.observation(t.dev.ID, t.ip, used.Get("username"), subnets, string(res.Stdout))
	if _, err := rc.Sink.Observe(ctx, obs); err != nil {
		return fmt.Errorf("Beobachtung speichern: %w", err)
	}
	return nil
}
