// Package settings stores system-wide settings (edited in the UI) in the settings table.
package settings

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"netscope/internal/db"
	"netscope/internal/i18n"
	"netscope/internal/plugin"
)

// System holds global settings.
type System struct {
	// PublicURL is the external base URL (e.g. https://netscope.lan) used for deep links.
	PublicURL string `json:"publicUrl"`
	// HostnamePriority orders hostname sources; unlisted sources follow afterwards.
	HostnamePriority []string `json:"hostnamePriority"`
	// OfflineAfterMissed is the number of consecutive presence runs that must miss a
	// device before it is considered offline.
	OfflineAfterMissed int `json:"offlineAfterMissed"`
	// MaxParallelRuns limits concurrently running plugin runs.
	MaxParallelRuns int `json:"maxParallelRuns"`
	// DeviceTypes are the selectable device types.
	DeviceTypes []string `json:"deviceTypes"`
	// ObservationRawMaxKB caps stored raw output per observation.
	ObservationRawMaxKB int `json:"observationRawMaxKb"`
	// MetricsPublic serves /metrics without authentication.
	MetricsPublic bool `json:"metricsPublic"`
	// Language of notifications and scheduled reports: de | en (the web UI follows each
	// user's own choice).
	Language string `json:"language"`
	// Timezone (IANA name) of schedules, reports and dates NetScope writes. The bootstrap
	// value (NETSCOPE_TIMEZONE) is only the start value on the first start.
	Timezone string `json:"timezone"`
	// ScanExclusions are addresses (or ranges) no scanner probes, e.g. sensitive devices.
	ScanExclusions []string `json:"scanExclusions"`
}

// DefaultTimezone is used until a time zone is stored.
const DefaultTimezone = "Europe/Berlin"

// Location returns the configured time zone.
func (s System) Location() *time.Location {
	if loc, err := time.LoadLocation(s.Timezone); err == nil && s.Timezone != "" {
		return loc
	}
	loc, err := time.LoadLocation(DefaultTimezone)
	if err != nil {
		return time.UTC
	}
	return loc
}

// Exclusions returns the scan exclusions as prefixes (single addresses as /32 or /128).
func (s System) Exclusions() []netip.Prefix {
	var out []netip.Prefix
	for _, e := range s.ScanExclusions {
		if p, err := ParseExclusion(e); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// ParseExclusion parses an address or CIDR range.
func ParseExclusion(v string) (netip.Prefix, error) {
	v = strings.TrimSpace(v)
	if !strings.Contains(v, "/") {
		a, err := netip.ParseAddr(v)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("%q ist weder IP-Adresse noch CIDR", v)
		}
		a = a.Unmap()
		return netip.PrefixFrom(a, a.BitLen()), nil
	}
	p, err := netip.ParsePrefix(v)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("%q ist weder IP-Adresse noch CIDR", v)
	}
	return p.Masked(), nil
}

// Lang returns the language for notifications and scheduled reports.
func (s System) Lang() i18n.Locale {
	if l, ok := i18n.Parse(s.Language); ok {
		return l
	}
	return i18n.Default
}

// DefaultSystem returns the defaults.
func DefaultSystem() System {
	return System{
		PublicURL: "",
		HostnamePriority: []string{"manual", "openwrt", "opnsense", "pfsense", "unifi", "mikrotik", "fortigate", "sophos", "meraki", "fritzbox",
			"pihole", "windows_dhcp", "ssh", "snmp", "proxmox", "dns", "mdns", "netbios", "upnp", "docker", "nmap"},
		OfflineAfterMissed: 2,
		MaxParallelRuns:    4,
		DeviceTypes: []string{"router", "switch", "access-point", "firewall", "server", "hypervisor", "vm", "container",
			"nas", "desktop", "laptop", "phone", "tablet", "tv", "media-player", "speaker", "printer", "camera",
			"smart-home", "iot", "game-console", "ups", "other"},
		ObservationRawMaxKB: 256,
		Language:            string(i18n.Default),
		Timezone:            DefaultTimezone,
		ScanExclusions:      []string{},
	}
}

// Validate checks the settings.
func (s *System) Validate() error {
	if s.PublicURL != "" {
		u, err := url.Parse(s.PublicURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return plugin.FieldErr("publicUrl", "gültige http(s)-URL erwartet")
		}
		s.PublicURL = strings.TrimRight(s.PublicURL, "/")
	}
	if s.OfflineAfterMissed < 1 || s.OfflineAfterMissed > 100 {
		return plugin.FieldErr("offlineAfterMissed", "1–100 erwartet")
	}
	if s.MaxParallelRuns < 1 || s.MaxParallelRuns > 64 {
		return plugin.FieldErr("maxParallelRuns", "1–64 erwartet")
	}
	if s.ObservationRawMaxKB < 0 || s.ObservationRawMaxKB > 16384 {
		return plugin.FieldErr("observationRawMaxKb", "0–16384 erwartet")
	}
	if s.Language == "" {
		s.Language = string(i18n.Default)
	}
	if l, ok := i18n.Parse(s.Language); ok {
		s.Language = string(l)
	} else {
		return plugin.FieldErr("language", "de oder en erwartet")
	}
	if s.Timezone == "" {
		s.Timezone = DefaultTimezone
	}
	if _, err := time.LoadLocation(s.Timezone); err != nil || strings.EqualFold(s.Timezone, "Local") {
		return plugin.FieldErr("timezone", fmt.Sprintf("unbekannte Zeitzone %q", s.Timezone))
	}
	var excl []string
	for i, e := range s.ScanExclusions {
		if strings.TrimSpace(e) == "" {
			continue
		}
		p, err := ParseExclusion(e)
		if err != nil {
			return plugin.FieldErr(fmt.Sprintf("scanExclusions.%d", i), err.Error())
		}
		if p.Bits() == p.Addr().BitLen() {
			excl = append(excl, p.Addr().String())
		} else {
			excl = append(excl, p.String())
		}
	}
	clean := func(in []string) []string {
		seen := map[string]bool{}
		var out []string
		for _, v := range in {
			v = strings.TrimSpace(v)
			if v != "" && !seen[v] {
				seen[v] = true
				out = append(out, v)
			}
		}
		return out
	}
	s.HostnamePriority = clean(s.HostnamePriority)
	s.DeviceTypes = clean(s.DeviceTypes)
	s.ScanExclusions = clean(excl)
	if s.ScanExclusions == nil {
		s.ScanExclusions = []string{}
	}
	return nil
}

const systemKey = "system"

// Store caches settings and notifies listeners on change.
type Store struct {
	db        *db.DB
	mu        sync.RWMutex
	system    System
	listeners []func(System)
	// hasTimezone: the stored settings name a time zone (else the bootstrap value applies)
	hasTimezone bool
}

// Load reads the settings from the database.
func Load(ctx context.Context, d *db.DB) (*Store, error) {
	s := &Store{db: d, system: DefaultSystem()}
	var raw string
	err := d.R.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", systemKey).Scan(&raw)
	if err == nil {
		sys := DefaultSystem()
		sys.Timezone = ""
		if err := json.Unmarshal([]byte(raw), &sys); err != nil {
			return nil, fmt.Errorf("settings: %w", err)
		}
		s.hasTimezone = sys.Timezone != ""
		if err := sys.Validate(); err != nil {
			sys = DefaultSystem()
		}
		s.system = sys
	} else if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	return s, nil
}

// System returns the current system settings.
func (s *Store) System() System {
	s.mu.RLock()
	defer s.mu.RUnlock()
	c := s.system
	c.HostnamePriority = append([]string(nil), s.system.HostnamePriority...)
	c.DeviceTypes = append([]string(nil), s.system.DeviceTypes...)
	c.ScanExclusions = append([]string{}, s.system.ScanExclusions...)
	return c
}

// Location returns the configured time zone (schedules, reports, dates).
func (s *Store) Location() *time.Location {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.system.Location()
}

// InitTimezone stores the bootstrap time zone (config.yaml, NETSCOPE_TIMEZONE) as the
// setting unless one is stored already: on the first start and once for installations
// from before the setting existed. Afterwards only the setting counts.
func (s *Store) InitTimezone(ctx context.Context, name string) error {
	s.mu.RLock()
	has := s.hasTimezone
	sys := s.system
	s.mu.RUnlock()
	if has || name == "" {
		return nil
	}
	sys.Timezone = name
	return s.SetSystem(ctx, sys)
}

// SetSystem validates and stores the system settings.
func (s *Store) SetSystem(ctx context.Context, sys System) error {
	if err := sys.Validate(); err != nil {
		return err
	}
	b, _ := json.Marshal(sys)
	if _, err := s.db.W.ExecContext(ctx, `INSERT INTO settings(key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, systemKey, string(b), db.Now()); err != nil {
		return err
	}
	s.mu.Lock()
	s.system = sys
	s.hasTimezone = true
	ls := append([]func(System){}, s.listeners...)
	s.mu.Unlock()
	for _, l := range ls {
		l(sys)
	}
	return nil
}

// OnChange registers a listener called after SetSystem.
func (s *Store) OnChange(fn func(System)) {
	s.mu.Lock()
	s.listeners = append(s.listeners, fn)
	s.mu.Unlock()
}

// GetJSON reads an arbitrary JSON setting (false if missing).
func GetJSON(ctx context.Context, q db.Querier, key string, dst any) (bool, error) {
	var raw string
	err := q.QueryRowContext(ctx, "SELECT value FROM settings WHERE key = ?", key).Scan(&raw)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, err
	}
	return true, json.Unmarshal([]byte(raw), dst)
}

// SetJSON writes an arbitrary JSON setting.
func SetJSON(ctx context.Context, q db.Querier, key string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = q.ExecContext(ctx, `INSERT INTO settings(key, value, updated_at) VALUES (?,?,?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`, key, string(b), db.Now())
	return err
}
