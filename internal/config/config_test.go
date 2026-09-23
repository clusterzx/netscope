package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesDefaultAndAppliesEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NETSCOPE_DATA_DIR", dir)
	t.Setenv("NETSCOPE_CONFIG", "")
	t.Setenv("NETSCOPE_LOG_LEVEL", "debug")
	t.Setenv("NETSCOPE_TRUSTED_PROXIES", "10.0.0.1, 192.168.8.0/24")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "config.yaml")); err != nil {
		t.Fatalf("default config not written: %v", err)
	}
	if cfg.LogLevel != "debug" || cfg.Listen != ":8080" {
		t.Fatalf("unexpected cfg %+v", cfg)
	}
	if len(cfg.Proxies) != 2 || cfg.Proxies[0].Bits() != 32 {
		t.Fatalf("proxies %v", cfg.Proxies)
	}
	if cfg.MasterKeyFile != filepath.Join(dir, "master.key") {
		t.Fatalf("master key file %s", cfg.MasterKeyFile)
	}
	// second load reads the file
	t.Setenv("NETSCOPE_LOG_LEVEL", "")
	cfg2, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg2.LogLevel != "info" {
		t.Fatalf("expected file value info, got %s", cfg2.LogLevel)
	}
}

func TestInvalid(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("NETSCOPE_DATA_DIR", dir)
	t.Setenv("NETSCOPE_CONFIG", "")
	t.Setenv("NETSCOPE_LOG_LEVEL", "loud")
	if _, err := Load(); err == nil {
		t.Fatal("expected error")
	}
}
