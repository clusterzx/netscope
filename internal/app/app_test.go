package app

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"netscope/internal/config"
	"netscope/internal/logging"
	"netscope/internal/pluginhost"
	"netscope/internal/setup"
)

func startInstance(t *testing.T, adminPassword string, ui bool) (*services, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := config.Default()
	cfg.Listen, cfg.DataDir, cfg.AdminPassword, cfg.UI = "127.0.0.1:0", dir, adminPassword, ui
	cfg.MasterKeyFile = filepath.Join(dir, "master.key")
	cfg.Location = time.UTC
	level := new(slog.LevelVar)
	ring := logging.NewRing(100)
	log := logging.New(io.Discard, "text", level, ring)
	ctx, cancel := context.WithCancel(context.Background())
	s, err := start(ctx, &cfg, "test", log, level, ring, time.Now(), make(chan string, 1))
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		stop(s, log)
		cancel()
	})
	return s, dir
}

// With NETSCOPE_ADMIN_PASSWORD there is no wizard: the setup counts as completed and every
// plugin enabled by default runs at once, as before.
func TestAdminPasswordSkipsWizard(t *testing.T) {
	s, dir := startInstance(t, "automatisch-123", true)
	if s.host.Held() {
		t.Fatal("plugins held although NETSCOPE_ADMIN_PASSWORD is set")
	}
	for _, id := range s.host.IDs() {
		p, _ := s.host.Plugin(id)
		if c, _ := s.host.Config(id); c.Enabled != p.Info().DefaultEnabled {
			t.Errorf("%s: enabled=%v, default %v", id, c.Enabled, p.Info().DefaultEnabled)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, setup.CodeFile)); !os.IsNotExist(err) {
		t.Fatal("setup code written although the wizard is skipped")
	}
	// a scheduled scanner can run right away
	if _, err := s.host.Trigger(context.Background(), "icmp", pluginhost.TriggerOptions{}); err != nil {
		t.Fatalf("run after automatic setup: %v", err)
	}
}

// A new installation with web interface waits for the wizard: code in the file, plugins
// held. Without web interface there is no wizard either.
func TestNewInstallationWaitsForWizard(t *testing.T) {
	s, dir := startInstance(t, "", true)
	if !s.host.Held() {
		t.Fatal("plugins must be held until the setup is finished")
	}
	b, err := os.ReadFile(filepath.Join(dir, setup.CodeFile))
	if err != nil || len(b) < 14 {
		t.Fatalf("setup code file: %q %v", b, err)
	}
	if _, err := os.Stat(filepath.Join(dir, InitialPasswordFile)); !os.IsNotExist(err) {
		t.Fatal("the initial password file is gone with the wizard")
	}
	headless, dir2 := startInstance(t, "", false)
	if headless.host.Held() {
		t.Fatal("an instance without web interface has no wizard")
	}
	if _, err := os.Stat(filepath.Join(dir2, setup.CodeFile)); !os.IsNotExist(err) {
		t.Fatal("setup code without web interface")
	}
}
