// Package app wires all services together, runs the HTTP server and handles graceful
// shutdown (SIGTERM: cancel running plugins, checkpoint the database) and the in-process
// restart used by the database restore.
package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"netscope/internal/api"
	"netscope/internal/audit"
	"netscope/internal/auth"
	"netscope/internal/bus"
	"netscope/internal/config"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/inventory"
	"netscope/internal/logging"
	"netscope/internal/pluginhost"
	"netscope/internal/rules"
	"netscope/internal/settings"
	"netscope/internal/tunnel"
	"netscope/internal/vault"
	"netscope/internal/webui"

	_ "netscope/internal/plugins/all" // register all plugins
)

// InitialPasswordFile holds the generated admin password until it is changed.
const InitialPasswordFile = "admin-initial-password.txt"

type services struct {
	db      *db.DB
	host    *pluginhost.Host
	rules   *rules.Engine
	tunnels *tunnel.Manager
	srv     *http.Server
}

// Run starts NetScope and blocks until ctx is cancelled.
func Run(ctx context.Context, cfg *config.Config, version string) error {
	level := new(slog.LevelVar)
	lvl, _ := config.ParseLevel(cfg.LogLevel)
	level.Set(lvl)
	ring := logging.NewRing(5000)
	log := logging.New(os.Stdout, cfg.LogFormat, level, ring)
	slog.SetDefault(log)
	time.Local = cfg.Location
	log.Info("NetScope startet", "version", version, "listen", cfg.Listen, "data", cfg.DataDir)
	started := time.Now()
	for {
		restore := make(chan string, 1)
		s, err := start(ctx, cfg, version, log, level, ring, started, restore)
		if err != nil {
			return err
		}
		var staged string
		select {
		case <-ctx.Done():
		case staged = <-restore:
			log.Warn("Wiederherstellung angefordert – Dienste werden neu gestartet", "file", staged)
		}
		stop(s, log)
		if staged == "" {
			log.Info("NetScope beendet")
			return nil
		}
		if err := swapDatabase(cfg, staged, log); err != nil {
			log.Error("Wiederherstellung fehlgeschlagen", "err", err)
		}
	}
}

func start(ctx context.Context, cfg *config.Config, version string, log *slog.Logger, level *slog.LevelVar, ring *logging.Ring,
	started time.Time, restore chan string) (*services, error) {
	d, err := db.Open(ctx, cfg.DBPath())
	if err != nil {
		return nil, err
	}
	fail := func(err error) (*services, error) {
		d.Close()
		return nil, err
	}
	key, src, err := vault.LoadKey(cfg.MasterKey, cfg.MasterKeyFile)
	if err != nil {
		return fail(fmt.Errorf("vault: %w", err))
	}
	if src.Generated {
		log.Warn("Neuer Vault-Master-Key erzeugt – bitte sichern, ohne ihn sind gespeicherte Zugangsdaten verloren", "file", src.File)
	}
	v, err := vault.Open(ctx, d, key, src)
	if err != nil {
		return fail(err)
	}
	st, err := settings.Load(ctx, d)
	if err != nil {
		return fail(err)
	}
	authSvc := auth.New(d)
	created, generated, err := authSvc.EnsureAdmin(ctx, cfg.AdminPassword)
	if err != nil {
		return fail(err)
	}
	if created && generated != "" {
		p := filepath.Join(cfg.DataDir, InitialPasswordFile)
		if err := os.WriteFile(p, []byte(generated+"\n"), 0o600); err != nil {
			return fail(err)
		}
		log.Warn("Admin-Benutzer angelegt – das Startpasswort steht in der Datei (nach dem ersten Login ändern)", "user", "admin", "file", p)
	}
	b := bus.New()
	ring.AttachBus(b)
	inv, err := inventory.New(ctx, d, b, st, log)
	if err != nil {
		return fail(err)
	}
	if added, err := inv.SeedSubnets(ctx); err != nil {
		log.Warn("Subnetze erkennen", "err", err)
	} else if len(added) > 0 {
		log.Info("Lokale Subnetze übernommen", "subnets", strings.Join(added, ", "))
	}
	if filled, err := inv.FillGateways(ctx); err != nil {
		log.Warn("Gateways ermitteln", "err", err)
	} else if len(filled) > 0 {
		log.Info("Gateway der Subnetze aus der Routing-Tabelle übernommen", "subnets", strings.Join(filled, ", "))
	}
	ev := events.New(d, b, inv)
	host := pluginhost.New(pluginhost.Deps{DB: d, Bus: b, Log: log, Inventory: inv, Vault: v, Events: ev, Settings: st,
		DataDir: cfg.DataDir, Location: cfg.Location, Version: version})
	if err := host.Init(ctx); err != nil {
		return fail(err)
	}
	tunnels := tunnel.New(tunnel.Deps{Subnets: inv, Creds: v, Events: ev, Bus: b, Log: log.With("component", "tunnel")})
	host.SetUnreachable(tunnels.Unreachable)
	engine := rules.New(d, b, log, ev, inv, host, st, cfg.Location)
	if err := engine.SeedDefaults(ctx); err != nil {
		return fail(err)
	}
	inv.OnChanges(host.DispatchChanges)
	ev.OnEvent(engine.OnEvent)
	server := api.New(api.Deps{Config: cfg, DB: d, Bus: b, Log: log, Logs: ring, LevelVar: level, Auth: authSvc, Vault: v, Settings: st,
		Inventory: inv, Events: ev, Rules: engine, Host: host, Tunnels: tunnels, Audit: audit.New(d), Version: version, StartedAt: started,
		Restore: func(path string) {
			select {
			case restore <- path:
			default:
			}
		}, UI: webui.Handler()})
	srv := &http.Server{Addr: cfg.Listen, Handler: server.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 120 * time.Second,
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn)}
	tunnels.Start(ctx)
	host.Start(ctx)
	engine.Start(ctx)
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := authSvc.CleanupSessions(context.Background()); err != nil {
					log.Warn("sessions cleanup", "err", err)
				}
			}
		}
	}()
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()
	select {
	case err := <-errCh:
		stop(&services{db: d, host: host, rules: engine, tunnels: tunnels}, log)
		return nil, fmt.Errorf("HTTP-Server: %w", err)
	case <-time.After(200 * time.Millisecond):
	}
	log.Info("NetScope bereit", "listen", cfg.Listen, "plugins", len(host.IDs()))
	return &services{db: d, host: host, rules: engine, tunnels: tunnels, srv: srv}, nil
}

// stop shuts down in order: HTTP, rule engine, plugins (running runs are cancelled),
// database (WAL checkpoint).
func stop(s *services, log *slog.Logger) {
	if s.srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		if err := s.srv.Shutdown(ctx); err != nil {
			log.Warn("HTTP-Shutdown", "err", err)
		}
		cancel()
	}
	if s.rules != nil {
		s.rules.Stop()
	}
	if s.host != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		s.host.Stop(ctx)
		cancel()
	}
	if s.tunnels != nil {
		s.tunnels.Stop()
	}
	if err := s.db.Close(); err != nil {
		log.Error("Datenbank schließen", "err", err)
	}
}

// swapDatabase replaces the database with a staged, validated file. The previous file is
// kept as netscope.db.pre-restore-<time>; if the new database cannot be opened with the
// current vault key it is rolled back.
func swapDatabase(cfg *config.Config, staged string, log *slog.Logger) error {
	path := cfg.DBPath()
	keep := path + ".pre-restore-" + time.Now().Format("20060102-150405")
	if err := os.Rename(path, keep); err != nil {
		return err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(path + suffix)
	}
	if err := copyFile(staged, path); err != nil {
		_ = os.Rename(keep, path)
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	d, err := db.Open(ctx, path)
	if err == nil {
		var key vault.Key
		var src vault.KeySource
		key, src, err = vault.LoadKey(cfg.MasterKey, cfg.MasterKeyFile)
		if err == nil {
			_, err = vault.Open(ctx, d, key, src)
		}
		d.Close()
	}
	if err != nil {
		log.Error("Wiederhergestellte Datenbank nicht nutzbar – vorherige Datenbank wird verwendet", "err", err)
		_ = os.Remove(path)
		for _, suffix := range []string{"-wal", "-shm"} {
			_ = os.Remove(path + suffix)
		}
		return os.Rename(keep, path)
	}
	_ = os.Remove(staged)
	log.Info("Datenbank wiederhergestellt", "previous", keep)
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o640)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
