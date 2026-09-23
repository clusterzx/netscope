// Package openwrt imports DHCP leases and static leases from an OpenWrt router (e.g.
// GL.iNet) over SSH ("uci show dhcp", dnsmasq lease file) or the LuCI ubus JSON-RPC API.
// It only fills hostnames and addresses and never reports presence.
package openwrt

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"

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
		Description: "Liest DHCP-Leases und statische Leases vom OpenWrt-/GL.iNet-Router (per SSH oder LuCI) und füllt damit Hostnamen und Adressen.",
		Version:     "1.0.0",

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
		{Key: "host", Type: plugin.FieldString, Label: "Router", Default: "192.168.8.1", Required: true,
			Description: "Hostname oder IP-Adresse des Routers.", Validation: &plugin.Validation{Format: "host"}},
		{Key: "method", Type: plugin.FieldEnum, Label: "Zugriff", Default: "ssh", Options: []plugin.Option{
			{Value: "ssh", Label: "SSH (dhcp.leases und uci show dhcp)"},
			{Value: "luci", Label: "LuCI (ubus JSON-RPC)"},
		}},
		{Key: "credential", Type: plugin.FieldCredentialRef, Label: "Zugangsdaten", Required: true,
			CredentialTypes: []string{plugin.CredSSH, plugin.CredPassword},
			Description:     "SSH: Credential vom Typ SSH oder Benutzer/Passwort. LuCI: Benutzer/Passwort (leerer Benutzer = root)."},
		{Key: "port", Type: plugin.FieldInt, Label: "SSH-Port", Default: 22, VisibleIf: visibleSSH,
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(65535)}},
		{Key: "host_key_policy", Type: plugin.FieldEnum, Label: "SSH-Hostschlüssel", Default: "tofu", VisibleIf: visibleSSH,
			Options: []plugin.Option{
				{Value: "tofu", Label: "Beim ersten Kontakt merken, Änderungen ablehnen"},
				{Value: "insecure", Label: "Nicht prüfen (unsicher)"},
			}},
		{Key: "luci_url", Type: plugin.FieldString, Label: "LuCI-URL", Placeholder: "http://192.168.8.1", VisibleIf: visibleLuCI,
			Description: "Leer = http://<Router>. Bei GL.iNet-Firmware 4.x läuft LuCI meist auf Port 8080 (http://192.168.8.1:8080).",
			Validation:  &plugin.Validation{Format: "url"}},
		{Key: "verify_tls", Type: plugin.FieldBool, Label: "TLS-Zertifikat prüfen", Default: false, VisibleIf: visibleLuCI, Advanced: true,
			Description: "Nur bei HTTPS mit gültigem Zertifikat einschalten; OpenWrt nutzt standardmäßig ein selbstsigniertes."},
		{Key: "import_static", Type: plugin.FieldBool, Label: "Statische Leases importieren", Default: true,
			Description: "Feste Zuordnungen (uci dhcp host) auch ohne aktive Lease übernehmen; ihr Name hat Vorrang vor dem vom Client gemeldeten."},
		{Key: "create_missing", Type: plugin.FieldBool, Label: "Fehlende Geräte anlegen", Default: false,
			Description: "Geräte aus Leases anlegen, die noch kein Scan gefunden hat. Aus: nur bekannte Geräte ergänzen."},
	}}
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	s := rc.Settings
	host := strings.TrimSpace(s.String("host"))
	if host == "" {
		return fmt.Errorf("kein Router konfiguriert")
	}
	credID := s.CredentialID("credential")
	if credID == 0 {
		return plugin.ErrNoCredential
	}
	cred, err := rc.Creds.Get(ctx, credID)
	if err != nil {
		return fmt.Errorf("Credential %d: %w", credID, err)
	}
	method := s.String("method")
	var (
		leases   []lease
		sections []uciSection
	)
	switch method {
	case "luci":
		if err := cred.RequireType(plugin.CredPassword); err != nil {
			return fmt.Errorf("LuCI-Zugriff: %w", err)
		}
		raw := s.String("luci_url")
		if raw == "" {
			raw = "http://" + host
		}
		endpoint, err := ubusEndpoint(raw)
		if err != nil {
			return err
		}
		c := newUbusClient(endpoint, s.Bool("verify_tls"), luciTimeout)
		defer c.close()
		user := cred.Get("username")
		if user == "" {
			user = "root"
		}
		leases, sections, err = fetchLuCI(ctx, rc.Log, c, user, cred.Get("password"))
		if err != nil {
			return err
		}
	default:
		method = "ssh"
		opt := sshx.Options{Port: s.Int("port")}
		if s.String("host_key_policy") != "insecure" {
			opt.KnownHosts = filepath.Join(rc.DataDir, "known_hosts")
		}
		leases, sections, err = fetchSSH(ctx, rc.Log, host, cred, opt)
		if err != nil {
			return err
		}
	}

	hosts := staticHosts(sections)
	entries := mergeEntries(leases, hosts, s.Bool("import_static"))
	rc.SetStat("leases", len(leases))
	rc.SetStat("static", len(hosts))
	rc.SetStat("observed", 0)
	rc.Log.Info("DHCP-Daten gelesen", "router", host, "zugriff", method, "leases", len(leases), "statisch", len(hosts))

	create := s.Bool("create_missing")
	failed := 0
	for i, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		obs := entryObservation(e, host, method, create)
		id, err := rc.Sink.Observe(ctx, obs)
		rc.Progress(i+1, len(entries))
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
