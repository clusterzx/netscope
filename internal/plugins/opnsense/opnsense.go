// Package opnsense imports DHCP leases, reservations and the ARP table of OPNsense
// firewalls through their REST API (key and secret). It reads every DHCP backend that
// answers – Kea, Dnsmasq (default since 25.7) and ISC dhcpd – in the snake_case URLs of
// 25.7+ and the camelCase URLs of older releases.
package opnsense

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugins/netsrc"
)

func init() { plugin.Register(&Plugin{}) }

const timeout = 30 * time.Second

// Plugin is the OPNsense importer.
type Plugin struct {
	now func() time.Time
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "opnsense",
		Kind: plugin.KindImporter,
		Name: "OPNsense",
		Description: "Liest DHCP-Leases (Kea, Dnsmasq, ISC), Reservierungen und die ARP-Tabelle von OPNsense-Firewalls über die REST-API " +
			"und füllt damit Namen, Adressen und Hersteller – auch für Netze hinter der Firewall.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultSchedule:    "*/10 * * * *",
		DefaultTimeout:     2 * time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     1,
		Targets:            plugin.TargetNone,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		netsrc.SourcesField("Firewalls", "", "Hostname, IP oder URL der Weboberfläche (z. B. https://fw.example.lan:8443), eine pro Zeile."),
		netsrc.CredentialsField([]string{plugin.CredAPIToken, plugin.CredPassword},
			"API-Schlüssel aus System → Zugang → Benutzer → API-Schlüssel: Credential „API-Token“ mit Key als Token-ID und Secret als Token "+
				"(oder Benutzer/Passwort mit Key und Secret). Nötige Rechte: DHCP-Leases (Kea, Dnsmasq bzw. ISC) und Diagnose: ARP-Tabelle."),
		netsrc.ARPField(),
		netsrc.CreateField(),
		netsrc.VerifyTLSField(),
	}}
}

func parseSource(s string) error {
	_, err := netsrc.BaseURL(s, "https", 443)
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
	return netsrc.Run(ctx, rc, netsrc.Options{PluginID: "opnsense", Sources: s.StringList(netsrc.KeySources), Label: "Firewall",
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

// api is a signed-in view of one firewall.
type api struct {
	hc   *http.Client
	base string
	key  string
	sec  string
}

// get reads a JSON endpoint. The first of the given paths that exists wins (snake_case of
// 25.7+, then camelCase); ok is false when none exists (404) or the key lacks the right
// (403).
func (a *api) get(ctx context.Context, dst any, paths ...string) (ok bool, err error) {
	for _, p := range paths {
		resp, err := netsrc.Do(ctx, a.hc, http.MethodGet, a.base+p, nil, netsrc.BasicAuth(a.key, a.sec), dst)
		if err == nil {
			return true, nil
		}
		if resp == nil {
			return false, err
		}
		switch resp.StatusCode {
		case http.StatusUnauthorized:
			return false, err // wrong key: the next credential may work
		case http.StatusNotFound, http.StatusForbidden:
			continue // module not installed, or no privilege for this URL spelling
		}
		return false, err
	}
	return false, nil
}

type rows struct {
	Rows []map[string]any `json:"rows"`
}

func (p *Plugin) fetch(ctx context.Context, rc *plugin.RunContext, src string) (*netsrc.Result, error) {
	base, err := netsrc.BaseURL(src, "https", 443)
	if err != nil {
		return nil, err
	}
	creds, err := netsrc.Credentials(ctx, rc, []string{plugin.CredAPIToken, plugin.CredPassword}, src)
	if err != nil {
		return nil, err
	}
	hc := netsrc.NewHTTPClient(rc.Settings.Bool(netsrc.KeyVerifyTLS), timeout)
	var res *netsrc.Result
	err = netsrc.TryCredentials(rc, src, creds, func(c *plugin.Credential) error {
		key, sec := c.Get("token_id"), c.Get("token")
		if c.Type == plugin.CredPassword {
			key, sec = c.Get("username"), c.Get("password")
		}
		a := &api{hc: hc, base: base.String(), key: key, sec: sec}
		r, err := p.read(ctx, rc, a)
		res = r
		return err
	})
	return res, err
}

// read collects leases, reservations and ARP entries of one firewall.
func (p *Plugin) read(ctx context.Context, rc *plugin.RunContext, a *api) (*netsrc.Result, error) {
	now := p.clock()
	var (
		clients  []netsrc.Client
		answered []string
	)
	type source struct {
		name  string
		paths []string
		parse func(map[string]any) (netsrc.Client, bool)
	}
	for _, s := range []source{
		{"Kea", []string{"/api/kea/leases4/search"}, func(r map[string]any) (netsrc.Client, bool) { return keaLease(r, now) }},
		{"Dnsmasq", []string{"/api/dnsmasq/leases/search"}, func(r map[string]any) (netsrc.Client, bool) { return dnsmasqLease(r, now) }},
		{"ISC", []string{"/api/dhcpv4/leases/search_lease", "/api/dhcpv4/leases/searchLease"}, iscLease},
		{"Kea-Reservierungen", []string{"/api/kea/dhcpv4/search_reservation", "/api/kea/dhcpv4/searchReservation"}, keaReservation},
		{"Dnsmasq-Hosts", []string{"/api/dnsmasq/settings/search_host", "/api/dnsmasq/settings/searchHost"}, dnsmasqHost},
	} {
		var r rows
		ok, err := a.get(ctx, &r, s.paths...)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", s.name, err)
		}
		if !ok {
			continue
		}
		answered = append(answered, s.name)
		n := 0
		for _, row := range r.Rows {
			if c, ok := s.parse(row); ok {
				clients = append(clients, c)
				n++
			}
		}
		rc.AddStat("rows_"+strings.ToLower(strings.SplitN(s.name, "-", 2)[0]), n)
	}
	if rc.Settings.Bool(netsrc.KeyARP) {
		var arp []map[string]any
		ok, err := a.get(ctx, &arp, "/api/diagnostics/interface/get_arp", "/api/diagnostics/interface/getArp")
		if err != nil {
			return nil, fmt.Errorf("ARP: %w", err)
		}
		if ok {
			answered = append(answered, "ARP")
			for _, row := range arp {
				if c, ok := arpEntry(row, now); ok {
					clients = append(clients, c)
				}
			}
		}
	}
	if len(answered) == 0 {
		return nil, errors.New("keine Daten: Weder DHCP-Leases noch ARP-Tabelle erreichbar – Rechte des API-Schlüssels prüfen (DHCP-Leases, Diagnose: ARP-Tabelle)")
	}
	rc.Log.Debug("OPNsense-Quellen", "gelesen", strings.Join(answered, ", "))
	return &netsrc.Result{Clients: clients}, nil
}

// ---------------------------------------------------------------- rows

// str reads a value that may be a string, number or bool.
func str(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func num(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return n
	}
	return 0
}

// truthy: "1", 1, true or a non-empty list (is_reserved changed type across releases).
func truthy(v any) bool {
	switch x := v.(type) {
	case []any:
		return len(x) > 0
	case bool:
		return x
	case float64:
		return x != 0
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "1" || s == "true" || s == "yes"
	}
	return false
}

// name drops the placeholders dnsmasq and Kea use for unknown names.
func name(v any) string {
	s := str(v)
	if s == "*" || s == "-" {
		return ""
	}
	return strings.TrimSuffix(s, ".")
}

func epoch(v any) time.Time {
	if n := num(v); n > 0 {
		return time.Unix(n, 0).UTC()
	}
	return time.Time{}
}

func iface(r map[string]any) string {
	if d := str(r["if_descr"]); d != "" {
		return d
	}
	return str(r["if"])
}

func keaLease(r map[string]any, now time.Time) (netsrc.Client, bool) {
	c := netsrc.Client{MAC: str(r["hwaddr"]), IP: str(r["address"]), Hostname: name(r["hostname"]), Vendor: str(r["mac_info"]),
		Kind: netsrc.KindDHCP, Expires: epoch(r["expire"]), Interface: iface(r), Static: truthy(r["is_reserved"]) || str(r["lease_type"]) == "static"}
	if st := str(r["state"]); st != "" && st != "0" {
		return c, false // declined or expired-reclaimed
	}
	if !c.Expires.IsZero() && c.Expires.Before(now) {
		return c, false
	}
	return c, c.MAC != ""
}

func dnsmasqLease(r map[string]any, now time.Time) (netsrc.Client, bool) {
	c := netsrc.Client{MAC: str(r["hwaddr"]), IP: str(r["address"]), Hostname: name(r["hostname"]), Vendor: str(r["mac_info"]),
		Kind: netsrc.KindDHCP, Expires: epoch(r["expire"]), Interface: iface(r), Static: truthy(r["is_reserved"]) || str(r["lease_type"]) == "static"}
	if !c.Expires.IsZero() && c.Expires.Before(now) {
		return c, false
	}
	return c, c.MAC != ""
}

func iscLease(r map[string]any) (netsrc.Client, bool) {
	static := str(r["type"]) == "static"
	c := netsrc.Client{MAC: str(r["mac"]), IP: str(r["address"]), Hostname: name(r["hostname"]), Vendor: str(r["man"]),
		Kind: netsrc.KindDHCP, Interface: iface(r), Static: static}
	if c.Hostname == "" {
		c.Hostname = name(r["client-hostname"])
	}
	if static {
		c.Kind, c.Name = netsrc.KindStatic, str(r["descr"])
	}
	switch str(r["status"]) {
	case "online":
		c.Online = netsrc.Bool(true)
	case "offline":
		c.Online = netsrc.Bool(false)
	}
	// "ends" is local time of the firewall; stored as given for display
	if e := str(r["ends"]); e != "" {
		if t, err := time.ParseInLocation("2006/01/02 15:04:05", e, time.Local); err == nil {
			c.Expires = t
		}
	}
	if st := str(r["state"]); !static && st != "" && st != "active" {
		return c, false
	}
	return c, c.MAC != ""
}

func keaReservation(r map[string]any) (netsrc.Client, bool) {
	c := netsrc.Client{MAC: str(r["hw_address"]), IP: str(r["ip_address"]), Name: firstNonEmpty(str(r["description"]), str(r["hostname"])),
		Hostname: str(r["hostname"]), Kind: netsrc.KindStatic, Static: true}
	return c, c.MAC != ""
}

func dnsmasqHost(r map[string]any) (netsrc.Client, bool) {
	if truthy(r["ignore"]) {
		return netsrc.Client{}, false
	}
	// hwaddr may hold several addresses separated by commas
	mac := strings.TrimSpace(strings.SplitN(str(r["hwaddr"]), ",", 2)[0])
	c := netsrc.Client{MAC: mac, IP: str(r["ip"]), Name: firstNonEmpty(str(r["descr"]), str(r["host"])), Hostname: str(r["host"]),
		Kind: netsrc.KindStatic, Static: true}
	return c, c.MAC != ""
}

func arpEntry(r map[string]any, now time.Time) (netsrc.Client, bool) {
	if truthy(r["permanent"]) || truthy(r["expired"]) {
		return netsrc.Client{}, false // own addresses of the firewall, stale entries
	}
	c := netsrc.Client{MAC: str(r["mac"]), IP: str(r["ip"]), Hostname: name(r["hostname"]), Vendor: str(r["manufacturer"]),
		Kind: netsrc.KindARP, Interface: firstNonEmpty(str(r["intf_description"]), str(r["intf"])), LastSeen: now}
	return c, c.MAC != "" && c.IP != ""
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
