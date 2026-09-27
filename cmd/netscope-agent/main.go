// Command netscope-agent collects the inventory and the utilisation of a Linux or Windows
// host and delivers it to a NetScope instance. It only connects out, runs the fixed
// read-only collection script of internal/hostscript (POSIX sh or PowerShell) and accepts
// nothing from the instance except "collect now", its settings and a newer version of itself
// (checked against SHA-256).
//
//	netscope-agent enroll --url https://netscope.lan --token nse_…   (once, by install.sh / install.ps1)
//	netscope-agent run [--config FILE]                                (the service)
//	netscope-agent version
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"strings"
	"sync"
	"time"

	"netscope/internal/agent/proto"
)

// version is set at build time (-ldflags "-X main.version=…").
var version = "dev"

// defaultConfigPath is the state file written by enroll (platform specific).
var defaultConfigPath = defaultConfigFile()

// exitRevoked tells systemd not to restart the agent (RestartPreventExitStatus=3): it was
// removed in NetScope.
const exitRevoked = 3

// errRestart: a new binary is in place; the service manager starts it after the agent ends.
var errRestart = errors.New("neue Version installiert")

func main() {
	args := os.Args[1:]
	cmd := "run"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "run":
		err = run(args)
	case "enroll":
		err = enroll(args)
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprint(os.Stderr, `NetScope-Agent

Befehle:
  enroll --url URL --token nse_… [--fingerprint SHA256] [--docker] [--config DATEI]
                  beim NetScope anmelden (macht install.sh bzw. install.ps1)
  run [--config DATEI]
                  Inventar und Auslastung liefern (Dienst)
  version         Version ausgeben
`)
		err = fmt.Errorf("unbekannter Befehl %q", cmd)
	}
	if errors.Is(err, errRevoked) {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(exitRevoked)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "Fehler:", err)
		os.Exit(1)
	}
}

func enroll(args []string) error {
	fs := flag.NewFlagSet("enroll", flag.ContinueOnError)
	url := fs.String("url", "", "Adresse der NetScope-Instanz, z. B. http://192.168.8.123:8080")
	token := fs.String("token", "", "Installations-Token (nse_…)")
	fingerprint := fs.String("fingerprint", "", "SHA-256 des TLS-Zertifikats (selbst signiert)")
	docker := fs.Bool("docker", false, "der Agent darf den Docker-Socket lesen")
	path := fs.String("config", defaultConfigPath, "Konfigurationsdatei")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *url == "" || !strings.HasPrefix(*token, proto.EnrollPrefix) {
		return errors.New("--url und --token nse_… sind nötig")
	}
	base := strings.TrimRight(*url, "/")
	fp := normalizeFingerprint(*fingerprint)
	host := hostInfo()
	host.Docker = *docker
	var resp proto.EnrollResponse
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	if err := newClient(base, fp, "").do(ctx, "POST", proto.PathEnroll, proto.EnrollRequest{Token: *token, Host: host}, &resp); err != nil {
		return err
	}
	cfg := &config{URL: base, AgentID: resp.AgentID, Secret: resp.Secret, Fingerprint: fp}
	if err := cfg.save(*path); err != nil {
		return err
	}
	fmt.Printf("Agent #%d bei %s angemeldet (%s).\n", resp.AgentID, base, host.Hostname)
	return nil
}

// agent is the running service.
type agent struct {
	cfg    *config
	client *client
	log    *slog.Logger

	mu       sync.Mutex
	settings proto.Config
	refresh  chan struct{}
	restart  chan struct{} // a new binary is in place
}

func run(args []string) error {
	fs := flag.NewFlagSet("run", flag.ContinueOnError)
	path := fs.String("config", defaultConfigPath, "Konfigurationsdatei")
	if err := fs.Parse(args); err != nil {
		return err
	}
	cfg, err := loadConfig(*path)
	if err != nil {
		return err
	}
	a := &agent{cfg: cfg, client: newClient(cfg.URL, cfg.Fingerprint, cfg.Secret), settings: proto.DefaultConfig(),
		log:     slog.New(slog.NewTextHandler(logWriter(*path), &slog.HandlerOptions{Level: slog.LevelInfo})),
		refresh: make(chan struct{}, 1), restart: make(chan struct{})}
	removeOldBinary()
	return serve(a.serve, a.log)
}

// serve runs the agent until ctx ends, it was removed (errRevoked) or it updated itself
// (errRestart).
func (a *agent) serve(ctx context.Context) error {
	a.log.Info("NetScope-Agent gestartet", "version", version, "instanz", a.cfg.URL, "agent", a.cfg.AgentID)

	errc := make(chan error, 3)
	go func() { errc <- a.pollLoop(ctx) }()
	go func() { errc <- a.inventoryLoop(ctx) }()
	go func() { errc <- a.metricsLoop(ctx) }()
	select {
	case <-ctx.Done():
		return nil
	case <-a.restart:
		a.log.Info("Neue Version installiert – Neustart")
		return errRestart
	case err := <-errc:
		return err
	}
}

func (a *agent) config() proto.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.settings
}

// pollLoop waits for settings, "collect now" and update offers.
func (a *agent) pollLoop(ctx context.Context) error {
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		var resp proto.PollResponse
		err := a.client.do(ctx, "GET", proto.PathPoll+"?wait=50&version="+version, nil, &resp)
		if errors.Is(err, errRevoked) {
			return err
		}
		if err != nil {
			if ctx.Err() == nil {
				a.log.Warn("Instanz nicht erreichbar", "err", err)
			}
			sleep(ctx, backoff)
			backoff = min(backoff*2, time.Minute)
			continue
		}
		backoff = 5 * time.Second
		a.mu.Lock()
		a.settings = sane(resp.Config)
		a.mu.Unlock()
		if resp.Refresh {
			select {
			case a.refresh <- struct{}{}:
			default:
			}
		}
		if resp.Update != nil && version != "dev" && resp.Update.Version != version {
			if err := a.selfUpdate(ctx, resp.Update); err != nil {
				a.log.Warn("Update fehlgeschlagen", "version", resp.Update.Version, "err", err)
				sleep(ctx, 10*time.Minute)
				continue
			}
			close(a.restart)
			return nil
		}
	}
	return nil
}

// sane keeps the agent reasonable whatever the instance sends.
func sane(c proto.Config) proto.Config {
	d := proto.DefaultConfig()
	if c.InventoryInterval < 5*time.Minute {
		c.InventoryInterval = d.InventoryInterval
	}
	if c.SampleInterval < 10*time.Second {
		c.SampleInterval = d.SampleInterval
	}
	if c.ReportInterval < c.SampleInterval {
		c.ReportInterval = c.SampleInterval
	}
	if c.CommandTimeout < time.Second {
		c.CommandTimeout = d.CommandTimeout
	}
	return c
}

// inventoryLoop collects the inventory after the start, periodically and on request.
func (a *agent) inventoryLoop(ctx context.Context) error {
	next := time.Duration(rand.Int64N(int64(20 * time.Second)))
	for {
		t := time.NewTimer(next)
		select {
		case <-ctx.Done():
			t.Stop()
			return nil
		case <-a.refresh:
			t.Stop()
		case <-t.C:
		}
		cfg := a.config()
		report := collect(ctx, cfg)
		report.Host = hostInfo()
		report.Host.Docker = dockerAccess()
		err := a.client.do(ctx, "POST", proto.PathInventory, report, nil)
		switch {
		case errors.Is(err, errRevoked):
			return err
		case err != nil:
			a.log.Warn("Inventar nicht zugestellt", "err", err)
			next = time.Minute
		default:
			a.log.Info("Inventar zugestellt", "bytes", len(report.Stdout))
			// ±10 % so many agents do not report in lockstep
			next = cfg.InventoryInterval + time.Duration(rand.Int64N(int64(cfg.InventoryInterval/5))) - cfg.InventoryInterval/10
		}
	}
}

// maxBuffered caps the samples kept while the instance is unreachable (one day at 1/min).
const maxBuffered = 1440

// metricsLoop samples the utilisation and delivers the samples in batches.
func (a *agent) metricsLoop(ctx context.Context) error {
	s := newSampler()
	var buf []proto.Sample
	lastSent, first := time.Now(), true
	for {
		cfg := a.config()
		if smp, err := s.sample(time.Now()); err == nil {
			buf = append(buf, smp)
			if len(buf) > maxBuffered {
				buf = buf[len(buf)-maxBuffered:]
			}
		} else {
			a.log.Warn("Messung fehlgeschlagen", "err", err)
		}
		// the first sample goes out at once so a new agent shows up promptly
		if first || time.Since(lastSent) >= cfg.ReportInterval-time.Second {
			first = false
			err := a.client.do(ctx, "POST", proto.PathMetrics, proto.MetricsReport{Samples: buf}, nil)
			switch {
			case errors.Is(err, errRevoked):
				return err
			case err == nil:
				buf, lastSent = nil, time.Now()
			case ctx.Err() == nil:
				a.log.Warn("Messwerte nicht zugestellt", "gepuffert", len(buf), "err", err)
			}
		}
		if !sleep(ctx, cfg.SampleInterval) {
			return nil
		}
	}
}

// sleep waits d or until ctx ends; it reports whether the wait completed.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}
