// Package openwrt imports DHCP leases and static leases from an OpenWrt router (e.g.
// GL.iNet) over SSH ("uci show dhcp", dnsmasq lease file) or the LuCI ubus JSON-RPC API.
// It only fills hostnames and addresses and never reports presence.
package openwrt

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
	"netscope/internal/sshx"
)

func init() { plugin.Register(&Plugin{}) }

// luciTimeout bounds a single LuCI request.
const luciTimeout = 30 * time.Second

// Plugin is the OpenWrt DHCP importer.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:          "openwrt",
		Kind:        plugin.KindImporter,
		Name:        "OpenWrt DHCP",
		Description: "Liest DHCP-Leases und statische Leases von OpenWrt-/GL.iNet-Routern (per SSH oder LuCI) und füllt damit Hostnamen und Adressen.",
		Version:     "1.1.0",

		DefaultEnabled:     false,
		DefaultSchedule:    "*/10 * * * *",
		DefaultTimeout:     2 * time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     1,
		Targets:            plugin.TargetNone,
	}
}

var (
	visibleSSH  = &plugin.Condition{Field: "method", Equals: []any{"ssh"}}
	visibleLuCI = &plugin.Condition{Field: "method", Equals: []any{"luci"}}
)

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "hosts", Type: plugin.FieldStringList, Label: "Router", Default: []string{"192.168.8.1"}, Required: true,
			Description: "Hostname oder IP-Adresse, ein Router pro Zeile. Bei LuCI auch als URL, z. B. http://192.168.8.1:8080 (GL.iNet-Firmware 4.x); ohne URL wird http://<Router> verwendet."},
		{Key: "method", Type: plugin.FieldEnum, Label: "Zugriff", Default: "ssh", Options: []plugin.Option{
			{Value: "ssh", Label: "SSH (dhcp.leases und uci show dhcp)"},
			{Value: "luci", Label: "LuCI (ubus JSON-RPC)"},
		}},
		{Key: "credentials", Type: plugin.FieldCredentialRef, Label: "Zugangsdaten", Multi: true,
			CredentialTypes: []string{plugin.CredSSH, plugin.CredPassword},
			Description: "SSH: Credentials vom Typ SSH oder Benutzer/Passwort. LuCI: Benutzer/Passwort (leerer Benutzer = root). " +
				"Leer = automatisch die passenden je Router nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen."},
		{Key: "port", Type: plugin.FieldInt, Label: "SSH-Port", Default: 22, VisibleIf: visibleSSH,
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(65535)}},
		{Key: "host_key_policy", Type: plugin.FieldEnum, Label: "SSH-Hostschlüssel", Default: "tofu", VisibleIf: visibleSSH,
			Options: []plugin.Option{
				{Value: "tofu", Label: "Beim ersten Kontakt merken, Änderungen ablehnen"},
				{Value: "insecure", Label: "Nicht prüfen (unsicher)"},
			}},
		{Key: "verify_tls", Type: plugin.FieldBool, Label: "TLS-Zertifikat prüfen", Default: false, VisibleIf: visibleLuCI, Advanced: true,
			Description: "Nur bei HTTPS mit gültigem Zertifikat einschalten; OpenWrt nutzt standardmäßig ein selbstsigniertes."},
		{Key: "import_static", Type: plugin.FieldBool, Label: "Statische Leases importieren", Default: true,
			Description: "Feste Zuordnungen (uci dhcp host) auch ohne aktive Lease übernehmen; ihr Name hat Vorrang vor dem vom Client gemeldeten."},
		{Key: "create_missing", Type: plugin.FieldBool, Label: "Fehlende Geräte anlegen", Default: false,
			Description: "Geräte aus Leases anlegen, die noch kein Scan gefunden hat. Aus: nur bekannte Geräte ergänzen."},
	}}
}

// MigrateSettings implements plugin.SettingsMigrator: version 1.0 had a single "host",
// "credential" and "luci_url".
func (p *Plugin) MigrateSettings(stored map[string]any) map[string]any {
	host, _ := stored["host"].(string)
	luci, _ := stored["luci_url"].(string)
	if _, has := stored["hosts"]; !has {
		switch {
		case stored["method"] == "luci" && strings.TrimSpace(luci) != "":
			stored["hosts"] = []any{luci}
		case strings.TrimSpace(host) != "":
			stored["hosts"] = []any{host}
		}
	}
	if c, ok := stored["credential"]; ok {
		if _, has := stored["credentials"]; !has {
			if id := plugin.NewSettings(map[string]any{"c": c}).CredentialID("c"); id > 0 {
				stored["credentials"] = []any{id}
			}
		}
	}
	delete(stored, "host")
	delete(stored, "luci_url")
	delete(stored, "credential")
	return stored
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	for _, h := range s.StringList("hosts") {
		if _, _, err := parseRouter(h, s.String("method")); err != nil {
			return plugin.FieldErr("hosts", err.Error())
		}
	}
	return nil
}

// Endpoints implements plugin.EndpointProvider.
func (p *Plugin) Endpoints(s plugin.Settings) []string {
	var out []string
	for _, h := range s.StringList("hosts") {
		if host, _, err := parseRouter(h, s.String("method")); err == nil {
			out = append(out, host)
		}
	}
	return out
}

// parseRouter splits a router entry into host name and LuCI URL (only for method luci:
// the entry itself when it is a URL, else http://<host>).
func parseRouter(entry, method string) (host, luciURL string, err error) {
	entry = strings.TrimSpace(entry)
	if strings.Contains(entry, "://") {
		u, err := url.Parse(entry)
		if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return "", "", fmt.Errorf("%q: http(s)-URL oder Hostname erwartet", entry)
		}
		if method != "luci" {
			return "", "", fmt.Errorf("%q: URLs gibt es nur für den Zugriff per LuCI – für SSH Hostname oder IP angeben", entry)
		}
		return u.Hostname(), entry, nil
	}
	if entry == "" || (strings.ContainsAny(entry, " /:") && net.ParseIP(entry) == nil) {
		return "", "", fmt.Errorf("%q: Hostname oder IP erwartet", entry)
	}
	return entry, "http://" + entry, nil
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	s := rc.Settings
	routers := s.StringList("hosts")
	if len(routers) == 0 {
		return fmt.Errorf("kein Router konfiguriert")
	}
	method := s.String("method")
	types := []string{plugin.CredSSH, plugin.CredPassword}
	if method == "luci" {
		types = []string{plugin.CredPassword}
	} else {
		method = "ssh"
	}
	picker := &plugin.CredentialPicker{Creds: rc.Creds, Types: types, Allowed: s.CredentialIDs("credentials"), Log: rc.Log}
	for _, k := range []string{"leases", "static", "observed"} {
		rc.SetStat(k, 0)
	}
	var errs []error
	for i, r := range routers {
		err := p.importRouter(ctx, rc, picker, method, r)
		if len(routers) > 1 {
			rc.Progress(i+1, len(routers))
		}
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(routers) == 1 {
			return err
		}
		rc.AddStat("failed_routers", 1)
		rc.Log.Warn("Router nicht gelesen", "router", r, "error", err)
		errs = append(errs, fmt.Errorf("%s: %w", r, err))
	}
	if len(errs) == len(routers) {
		return fmt.Errorf("kein Router gelesen: %w", errors.Join(errs...))
	}
	return nil
}

// importRouter reads one router, trying the applicable credentials in order.
func (p *Plugin) importRouter(ctx context.Context, rc *plugin.RunContext, picker *plugin.CredentialPicker, method, entry string) error {
	s := rc.Settings
	host, luciURL, err := parseRouter(entry, method)
	if err != nil {
		return err
	}
	ip, _ := netutil.ResolveHost(ctx, host)
	creds, err := picker.For(ctx, plugin.CredentialTarget{IP: ip})
	if err != nil {
		return err
	}
	if len(creds) == 0 {
		return fmt.Errorf("keine passenden Zugangsdaten für %s (Auswahl oder Geltungsbereich der Credentials prüfen): %w", host, plugin.ErrNoCredential)
	}
	var (
		leases   []lease
		sections []uciSection
	)
	switch method {
	case "luci":
		endpoint, err := ubusEndpoint(luciURL)
		if err != nil {
			return err
		}
		c := newUbusClient(endpoint, s.Bool("verify_tls"), luciTimeout)
		defer c.close()
		for i, cred := range creds {
			user := cred.Get("username")
			if user == "" {
				user = "root"
			}
			leases, sections, err = fetchLuCI(ctx, rc.Log, c, user, cred.Get("password"))
			if errors.Is(err, errLoginRejected) && i < len(creds)-1 {
				rc.Log.Info("LuCI-Anmeldung abgelehnt, nächste Zugangsdaten werden probiert", "router", host, "credential", cred.Name)
				continue
			}
			break
		}
		if err != nil {
			return err
		}
	default:
		opt := sshx.Options{Port: s.Int("port")}
		if s.String("host_key_policy") != "insecure" {
			opt.KnownHosts = filepath.Join(rc.DataDir, "known_hosts")
		}
		leases, sections, err = fetchSSH(ctx, rc.Log, host, creds, opt)
		if err != nil {
			return err
		}
	}

	hosts := staticHosts(sections)
	entries := mergeEntries(leases, hosts, s.Bool("import_static"))
	rc.AddStat("leases", len(leases))
	rc.AddStat("static", len(hosts))
	rc.Log.Info("DHCP-Daten gelesen", "router", host, "zugriff", method, "leases", len(leases), "statisch", len(hosts))

	create := s.Bool("create_missing")
	single := len(s.StringList("hosts")) == 1
	failed := 0
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		obs := entryObservation(e, host, method, create)
		id, err := rc.Sink.Observe(ctx, obs)
		if single {
			rc.Progress(i+1, len(entries))
		}
		if err != nil {
			failed++
			rc.Log.Warn("Lease konnte nicht gespeichert werden", "mac", e.MACs[0], "ip", obs.IP, "error", err)
			continue
		}
		rc.AddStat("observed", 1)
		if id == 0 {
			rc.AddStat("unmatched", 1)
		}
	}
	if failed > 0 && failed == len(entries) {
		return fmt.Errorf("keine der %d Leases konnte gespeichert werden", failed)
	}
	return nil
}

// leaseInfo describes the active lease in the inventory.
type leaseInfo struct {
	IP       string     `json:"ip"`
	Hostname string     `json:"hostname,omitempty"`
	Expires  *time.Time `json:"expires,omitempty"`
	Infinite bool       `json:"infinite,omitempty"`
	ClientID string     `json:"clientId,omitempty"`
	DUID     string     `json:"duid,omitempty"`
}

// dhcpInventory is the structured inventory stored per device.
type dhcpInventory struct {
	Router     string     `json:"router"`
	Method     string     `json:"method"`
	Static     bool       `json:"static"`
	StaticName string     `json:"staticName,omitempty"`
	StaticIP   string     `json:"staticIp,omitempty"`
	StaticMACs []string   `json:"staticMacs,omitempty"`
	LeaseTime  string     `json:"leaseTime,omitempty"`
	DNS        bool       `json:"dns,omitempty"`
	Section    string     `json:"section,omitempty"`
	Lease      *leaseInfo `json:"lease,omitempty"`
}

// entryObservation builds the observation of one DHCP client.
func entryObservation(e *entry, router, method string, create bool) *plugin.Observation {
	inv := dhcpInventory{Router: router, Method: method, Static: e.Static != nil}
	if st := e.Static; st != nil {
		inv.StaticName, inv.StaticIP, inv.StaticMACs = st.Name, st.IP, st.MACs
		inv.LeaseTime, inv.DNS, inv.Section = st.LeaseTime, st.DNS, st.Section
	}
	if l := e.Lease; l != nil {
		li := &leaseInfo{IP: l.IP, Hostname: l.Hostname, Infinite: l.Infinite, ClientID: l.ClientID, DUID: l.DUID}
		if !l.Infinite && !l.Expires.IsZero() {
			exp := l.Expires
			li.Expires = &exp
		}
		inv.Lease = li
	}
	target := e.ip()
	if target == "" {
		target = e.MACs[0]
	}
	return &plugin.Observation{
		MACs:      e.MACs,
		IP:        e.ip(),
		Target:    target,
		Hostname:  strings.TrimSpace(e.hostname()),
		Create:    create,
		Attrs:     map[string]string{"dhcp.static": strconv.FormatBool(e.Static != nil)},
		Inventory: inv,
	}
}
