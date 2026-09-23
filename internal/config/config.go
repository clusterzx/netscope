// Package config loads the bootstrap configuration (/data/config.yaml) with NETSCOPE_*
// environment overrides. Everything else is stored in the database and edited in the UI.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// Config is the bootstrap configuration.
type Config struct {
	Listen    string `yaml:"listen"`
	DataDir   string `yaml:"data_dir"`
	LogLevel  string `yaml:"log_level"`
	LogFormat string `yaml:"log_format"`
	Timezone  string `yaml:"timezone"`
	// MasterKeyFile holds the base64 vault master key. NETSCOPE_MASTER_KEY (the key
	// itself) takes precedence. The file is generated on first start if missing.
	MasterKeyFile  string   `yaml:"master_key_file"`
	TrustedProxies []string `yaml:"trusted_proxies"`

	// Derived / env-only values (not written to the file).
	MasterKey     string         `yaml:"-"` // from NETSCOPE_MASTER_KEY
	AdminPassword string         `yaml:"-"` // NETSCOPE_ADMIN_PASSWORD, used for the first user only
	Path          string         `yaml:"-"`
	Proxies       []netip.Prefix `yaml:"-"`
	Location      *time.Location `yaml:"-"`
}

// Default returns the default configuration.
func Default() Config {
	return Config{
		Listen:        ":8080",
		DataDir:       "/data",
		LogLevel:      "info",
		LogFormat:     "json",
		Timezone:      "Europe/Berlin",
		MasterKeyFile: "",
		TrustedProxies: []string{"127.0.0.0/8", "::1/128", "10.0.0.0/8", "172.16.0.0/12",
			"192.168.0.0/16", "fc00::/7"},
	}
}

const fileHeader = `# NetScope bootstrap configuration.
# Only settings needed before the database is available live here; everything else is
# configured in the web UI. Every key can be overridden with NETSCOPE_<KEY> (upper case),
# e.g. NETSCOPE_LISTEN=:8080, NETSCOPE_LOG_LEVEL=debug.
# The vault master key is read from NETSCOPE_MASTER_KEY or from master_key_file
# (default: <data_dir>/master.key, generated on first start – back it up!).
`

// Load reads the config file (path from NETSCOPE_CONFIG, default /data/config.yaml),
// applies environment overrides and validates the result. If the file does not exist it
// is created with the effective defaults.
func Load() (*Config, error) {
	cfg := Default()
	if v := os.Getenv("NETSCOPE_DATA_DIR"); v != "" {
		cfg.DataDir = v
	}
	path := os.Getenv("NETSCOPE_CONFIG")
	if path == "" {
		path = filepath.Join(cfg.DataDir, "config.yaml")
	}
	cfg.Path = path
	b, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := yaml.Unmarshal(b, &cfg); err != nil {
			return nil, fmt.Errorf("config %s: %w", path, err)
		}
	case errors.Is(err, os.ErrNotExist):
		if err := writeDefault(path, cfg); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	applyEnv(&cfg)
	if err := cfg.finish(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func writeDefault(path string, cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	out, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append([]byte(fileHeader), out...), 0o640)
}

func applyEnv(cfg *Config) {
	str := func(key string, dst *string) {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			*dst = v
		}
	}
	str("NETSCOPE_LISTEN", &cfg.Listen)
	str("NETSCOPE_DATA_DIR", &cfg.DataDir)
	str("NETSCOPE_LOG_LEVEL", &cfg.LogLevel)
	str("NETSCOPE_LOG_FORMAT", &cfg.LogFormat)
	str("NETSCOPE_TIMEZONE", &cfg.Timezone)
	str("NETSCOPE_MASTER_KEY_FILE", &cfg.MasterKeyFile)
	str("NETSCOPE_MASTER_KEY", &cfg.MasterKey)
	str("NETSCOPE_ADMIN_PASSWORD", &cfg.AdminPassword)
	if v := os.Getenv("NETSCOPE_TRUSTED_PROXIES"); v != "" {
		cfg.TrustedProxies = splitList(v)
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == ';' }) {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c *Config) finish() error {
	if c.Listen == "" {
		return errors.New("listen darf nicht leer sein")
	}
	if c.DataDir == "" {
		return errors.New("data_dir darf nicht leer sein")
	}
	if c.MasterKeyFile == "" {
		c.MasterKeyFile = filepath.Join(c.DataDir, "master.key")
	}
	if _, err := ParseLevel(c.LogLevel); err != nil {
		return err
	}
	switch c.LogFormat {
	case "json", "text":
	default:
		return fmt.Errorf("log_format %q: erlaubt sind json, text", c.LogFormat)
	}
	loc, err := time.LoadLocation(c.Timezone)
	if err != nil {
		return fmt.Errorf("timezone %q: %w", c.Timezone, err)
	}
	c.Location = loc
	c.Proxies = nil
	for _, p := range c.TrustedProxies {
		if !strings.Contains(p, "/") {
			a, err := netip.ParseAddr(p)
			if err != nil {
				return fmt.Errorf("trusted_proxies: %q ist weder IP noch CIDR", p)
			}
			c.Proxies = append(c.Proxies, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		pfx, err := netip.ParsePrefix(p)
		if err != nil {
			return fmt.Errorf("trusted_proxies: %w", err)
		}
		c.Proxies = append(c.Proxies, pfx.Masked())
	}
	return nil
}

// ParseLevel parses a slog level name.
func ParseLevel(s string) (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(strings.ToUpper(s))); err != nil {
		return 0, fmt.Errorf("log_level %q: erlaubt sind debug, info, warn, error", s)
	}
	return l, nil
}

// DBPath is the location of the SQLite database.
func (c *Config) DBPath() string { return filepath.Join(c.DataDir, "netscope.db") }
