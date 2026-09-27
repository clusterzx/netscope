// Package pihole imports DHCP leases, DHCP reservations and the network table (MAC →
// addresses and names from DNS queries) of Pi-hole v6 through its REST API.
package pihole

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"regexp"
	"strings"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
	"netscope/internal/plugins/netsrc"
)

func init() { plugin.Register(&Plugin{}) }

const timeout = 30 * time.Second

// Plugin is the Pi-hole importer.
type Plugin struct {
	now func() time.Time
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "pihole",
		Kind: plugin.KindImporter,
		Name: "Pi-hole",
		Description: "Liest DHCP-Leases und Reservierungen sowie die Netzwerk-Tabelle (Geräte mit Namen aus DNS-Anfragen) " +
			"von Pi-hole v6 über die REST-API.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultSchedule:    "*/10 * * * *",
		DefaultTimeout:     2 * time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     1,
		Targets:            plugin.TargetNone,
	}
}

const keyNetwork = "include_network"

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		netsrc.SourcesField("Pi-hole", "", "Hostname, IP oder URL (z. B. http://pi.hole oder https://pihole.lan), einer pro Zeile."),
		netsrc.CredentialsField([]string{plugin.CredPassword},
			"Benutzer/Passwort mit dem Web-Passwort oder besser einem App-Passwort von Pi-hole (Benutzer bleibt leer)."),
		{Key: keyNetwork, Type: plugin.FieldBool, Label: "Netzwerk-Tabelle einbeziehen", Default: true,
			Description: "Auch Geräte aus der Netzwerk-Tabelle von Pi-hole (alle, die DNS-Anfragen stellen – mit oder ohne DHCP)."},
		netsrc.CreateField(),
		netsrc.VerifyTLSField(),
	}}
}

func parseSource(s string) error {
	_, err := netsrc.BaseURL(s, "http", 80)
	return err
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	return netsrc.ValidateSources(s, parseSource)
}

// Endpoints implements plugin.EndpointProvider.
func (p *Plugin) Endpoints(s plugin.Settings) []string { return netsrc.Endpoints(s) }

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	s := rc.Settings
	return netsrc.Run(ctx, rc, netsrc.Options{PluginID: "pihole", Sources: s.StringList(netsrc.KeySources), Label: "Pi-hole",
		Create: s.Bool(netsrc.KeyCreate)}, func(ctx context.Context, src string) (*netsrc.Result, error) {
		return p.fetch(ctx, rc, src)
	})
}

func (p *Plugin) clock() time.Time {
	if p.now != nil {
		return p.now()
	}
	return time.Now()
}

type session struct {
	hc   *http.Client
	base string
	sid  string
}

func (s *session) header() http.Header {
	h := http.Header{}
	if s.sid != "" {
		h.Set("X-FTL-SID", s.sid)
	}
	return h
}

func (s *session) get(ctx context.Context, path string, dst any) error {
	_, err := netsrc.Do(ctx, s.hc, http.MethodGet, s.base+path, nil, s.header(), dst)
	return err
}

// login opens a session (Pi-hole limits their number: always log out afterwards).
func login(ctx context.Context, hc *http.Client, base, password string) (*session, error) {
	body, _ := json.Marshal(map[string]string{"password": password})
	var res struct {
		Session struct {
			Valid   bool   `json:"valid"`
			SID     string `json:"sid"`
			Message string `json:"message"`
		} `json:"session"`
	}
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	resp, err := netsrc.Do(ctx, hc, http.MethodPost, base+"/api/auth", bytes.NewReader(body), h, &res)
	if err != nil {
		if resp != nil && resp.StatusCode == http.StatusTooManyRequests {
			return nil, errors.New("Pi-hole lehnt die Anmeldung ab: zu viele Sitzungen oder Versuche (HTTP 429)")
		}
		return nil, err
	}
	if !res.Session.Valid {
		return nil, fmt.Errorf("%w: %s", netsrc.ErrAuth, res.Session.Message)
	}
	return &session{hc: hc, base: base, sid: res.Session.SID}, nil
}

func (s *session) logout() {
	if s.sid == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = netsrc.Do(ctx, s.hc, http.MethodDelete, s.base+"/api/auth", nil, s.header(), nil)
}

func (p *Plugin) fetch(ctx context.Context, rc *plugin.RunContext, src string) (*netsrc.Result, error) {
	base, err := netsrc.BaseURL(src, "http", 80)
	if err != nil {
		return nil, err
	}
	// with no password configured Pi-hole needs no session; credentials are then optional
	creds, cerr := netsrc.Credentials(ctx, rc, []string{plugin.CredPassword}, src)
	if cerr != nil && !errors.Is(cerr, plugin.ErrNoCredential) {
		return nil, cerr
	}
	hc := netsrc.NewHTTPClient(rc.Settings.Bool(netsrc.KeyVerifyTLS), timeout)
	if len(creds) == 0 {
		creds = []*plugin.Credential{{Name: "ohne Passwort"}}
	}
	var res *netsrc.Result
	err = netsrc.TryCredentials(rc, src, creds, func(c *plugin.Credential) error {
		s, err := login(ctx, hc, base.String(), c.Get("password"))
		if err != nil {
			return err
		}
		defer s.logout()
		r, err := p.read(ctx, rc, s)
		res = r
		return err
	})
	return res, err
}

func (p *Plugin) read(ctx context.Context, rc *plugin.RunContext, s *session) (*netsrc.Result, error) {
	now := p.clock()
	var clients []netsrc.Client
	var active struct {
		Config struct {
			DHCP struct {
				Active bool `json:"active"`
			} `json:"dhcp"`
		} `json:"config"`
	}
	if err := s.get(ctx, "/api/config/dhcp/active", &active); err != nil {
		return nil, fmt.Errorf("DHCP-Status: %w", err)
	}
	if active.Config.DHCP.Active {
		var leases struct {
			Leases []struct {
				Expires int64  `json:"expires"`
				Name    string `json:"name"`
				HWAddr  string `json:"hwaddr"`
				IP      string `json:"ip"`
			} `json:"leases"`
		}
		if err := s.get(ctx, "/api/dhcp/leases", &leases); err != nil {
			return nil, fmt.Errorf("DHCP-Leases: %w", err)
		}
		for _, l := range leases.Leases {
			c := netsrc.Client{MAC: l.HWAddr, IP: l.IP, Hostname: placeholder(l.Name), Kind: netsrc.KindDHCP}
			if l.Expires > 0 {
				c.Expires = time.Unix(l.Expires, 0).UTC()
				if c.Expires.Before(now) {
					continue
				}
			}
			clients = append(clients, c)
		}
		rc.AddStat("leases", len(leases.Leases))
		var hosts struct {
			Config struct {
				DHCP struct {
					Hosts []string `json:"hosts"`
				} `json:"dhcp"`
			} `json:"config"`
		}
		if err := s.get(ctx, "/api/config/dhcp/hosts", &hosts); err != nil {
			rc.Log.Warn("DHCP-Reservierungen nicht lesbar", "error", err)
		}
		for _, h := range hosts.Config.DHCP.Hosts {
			if c, ok := parseHost(h); ok {
				clients = append(clients, c)
			}
		}
	}
	if rc.Settings.Bool(keyNetwork) {
		var table struct {
			Devices []struct {
				HWAddr    string  `json:"hwaddr"`
				Interface string  `json:"interface"`
				LastQuery int64   `json:"lastQuery"`
				MacVendor *string `json:"macVendor"`
				IPs       []struct {
					IP       string  `json:"ip"`
					Name     *string `json:"name"`
					LastSeen int64   `json:"lastSeen"`
				} `json:"ips"`
			} `json:"devices"`
		}
		if err := s.get(ctx, "/api/network/devices?max_devices=10000&max_addresses=50", &table); err != nil {
			return nil, fmt.Errorf("Netzwerk-Tabelle: %w", err)
		}
		for _, d := range table.Devices {
			mac, ok := netutil.NormalizeMAC(d.HWAddr) // skips "ip-192.168.5.20" pseudo entries
			if !ok || mac == "00:00:00:00:00:00" {
				continue
			}
			c := netsrc.Client{MAC: mac, Kind: netsrc.KindARP, Interface: d.Interface}
			if d.MacVendor != nil {
				c.Vendor = *d.MacVendor
			}
			var newest int64
			for _, a := range d.IPs {
				if a.LastSeen < newest || net4(a.IP) == "" {
					continue
				}
				newest, c.IP = a.LastSeen, a.IP
				if a.Name != nil {
					c.Hostname = strings.TrimSuffix(*a.Name, ".")
				}
			}
			if last := max(newest, d.LastQuery); last > 0 {
				c.LastSeen = time.Unix(last, 0).UTC()
			}
			clients = append(clients, c)
		}
		rc.AddStat("network_devices", len(table.Devices))
	}
	return &netsrc.Result{Clients: clients}, nil
}

// net4 returns ip if it is IPv4 (the network table also holds IPv6 addresses).
func net4(ip string) string {
	if a := net.ParseIP(ip); a != nil && a.To4() != nil {
		return ip
	}
	return ""
}

func placeholder(s string) string {
	s = strings.TrimSpace(s)
	if s == "*" {
		return ""
	}
	return s
}

var leaseTime = regexp.MustCompile(`^(\d+[smhdw]?|infinite)$`)

// parseHost reads a reservation in dnsmasq dhcp-host syntax, e.g.
// "00:20:e0:3b:13:af,192.168.0.123,laptop,24h" (fields in any order).
func parseHost(spec string) (netsrc.Client, bool) {
	c := netsrc.Client{Kind: netsrc.KindStatic, Static: true}
	for _, f := range strings.Split(spec, ",") {
		f = strings.TrimSpace(f)
		switch {
		case f == "" || strings.HasPrefix(f, "set:") || strings.HasPrefix(f, "tag:") || strings.HasPrefix(f, "id:") || f == "ignore":
			if f == "ignore" {
				return c, false
			}
		case c.MAC == "" && isMAC(f):
			c.MAC = f
		case c.IP == "" && net.ParseIP(strings.Trim(f, "[]")) != nil:
			c.IP = strings.Trim(f, "[]")
		case leaseTime.MatchString(f):
		default:
			if c.Name == "" {
				c.Name = f
			}
		}
	}
	return c, c.MAC != ""
}

func isMAC(s string) bool {
	_, ok := netutil.NormalizeMAC(s)
	return ok
}
