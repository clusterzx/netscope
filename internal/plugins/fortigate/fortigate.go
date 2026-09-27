// Package fortigate imports DHCP leases, the ARP table and the detected devices (device
// identification: host name, OS, FortiSwitch port, FortiAP) of Fortinet FortiGate
// firewalls through the FortiOS REST API with a REST API administrator token.
package fortigate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugins/netsrc"
)

func init() { plugin.Register(&Plugin{}) }

const (
	timeout  = 60 * time.Second
	pageSize = 1000
)

// Plugin is the FortiGate importer.
type Plugin struct {
	now func() time.Time
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "fortigate",
		Kind: plugin.KindImporter,
		Name: "Fortinet FortiGate",
		Description: "Liest DHCP-Leases, die ARP-Tabelle und die von der FortiGate erkannten Geräte (Name, Betriebssystem, " +
			"FortiSwitch-Port, FortiAP) über die FortiOS-REST-API.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultSchedule:    "*/10 * * * *",
		DefaultTimeout:     3 * time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     1,
		Targets:            plugin.TargetNone,
	}
}

const (
	keyVDOMs   = "vdoms"
	keyDevices = "include_devices"
)

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		netsrc.SourcesField("FortiGates", "", "Hostname, IP oder URL der Verwaltungsoberfläche (z. B. https://fw.lan:8443), eine pro Zeile."),
		netsrc.CredentialsField([]string{plugin.CredAPIToken},
			"API-Token eines REST-API-Administrators (System → Administratoren → REST-API-Admin; Profil z. B. super_admin_readonly, "+
				"vertrauenswürdige Hosts: die Adresse von NetScope)."),
		{Key: keyVDOMs, Type: plugin.FieldStringList, Label: "VDOMs", Default: []string{},
			Description: "Nur bei mehreren VDOMs: deren Namen. Leer = die VDOM des Tokens."},
		netsrc.ARPField(),
		{Key: keyDevices, Type: plugin.FieldBool, Label: "Erkannte Geräte", Default: true,
			Description: "Geräte aus der Geräteerkennung der FortiGate (Name, Betriebssystem, FortiSwitch-Port, FortiAP); braucht „Device detection“ auf den Schnittstellen."},
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
	return netsrc.Run(ctx, rc, netsrc.Options{PluginID: "fortigate", Sources: s.StringList(netsrc.KeySources), Label: "FortiGate",
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

type api struct {
	hc    *http.Client
	base  string
	token string
}

type monitor struct {
	Results json.RawMessage `json:"results"`
	Status  string          `json:"status"`
	Total   int             `json:"total"`
}

// get calls a monitor endpoint and decodes its results.
func (a *api) get(ctx context.Context, path string, q url.Values, dst any) (*monitor, error) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+a.token)
	u := a.base + "/api/v2/monitor/" + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var m monitor
	if _, err := netsrc.Do(ctx, a.hc, http.MethodGet, u, nil, h, &m); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if m.Status != "" && m.Status != "success" {
		return nil, fmt.Errorf("%s: Status %s", path, m.Status)
	}
	return &m, json.Unmarshal(m.Results, dst)
}

func (p *Plugin) fetch(ctx context.Context, rc *plugin.RunContext, src string) (*netsrc.Result, error) {
	base, err := netsrc.BaseURL(src, "https", 443)
	if err != nil {
		return nil, err
	}
	creds, err := netsrc.Credentials(ctx, rc, []string{plugin.CredAPIToken}, src)
	if err != nil {
		return nil, err
	}
	hc := netsrc.NewHTTPClient(rc.Settings.Bool(netsrc.KeyVerifyTLS), timeout)
	vdoms := rc.Settings.StringList(keyVDOMs)
	if len(vdoms) == 0 {
		vdoms = []string{""}
	}
	var res *netsrc.Result
	err = netsrc.TryCredentials(rc, src, creds, func(c *plugin.Credential) error {
		a := &api{hc: hc, base: base.String(), token: c.Get("token")}
		res = &netsrc.Result{}
		for _, v := range vdoms {
			cl, err := p.read(ctx, rc, a, strings.TrimSpace(v))
			if err != nil {
				if v != "" {
					return fmt.Errorf("VDOM %s: %w", v, err)
				}
				return err
			}
			res.Clients = append(res.Clients, cl...)
		}
		return nil
	})
	return res, err
}

type lease struct {
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	Hostname  string `json:"hostname"`
	VCI       string `json:"vci"`
	Expire    any    `json:"expire_time"`
	Status    string `json:"status"`
	Interface string `json:"interface"`
	Reserved  bool   `json:"reserved"`
	SSID      string `json:"ssid"`
	AP        string `json:"access_point"`
	Type      string `json:"type"`
}

type arp struct {
	IP        string `json:"ip"`
	MAC       string `json:"mac"`
	Interface string `json:"interface"`
	Age       any    `json:"age"`
}

type detected struct {
	IP        string `json:"ipv4_address"`
	MAC       string `json:"mac"`
	Hostname  string `json:"hostname"`
	Vendor    string `json:"hardware_vendor"`
	Family    string `json:"hardware_family"`
	HWType    string `json:"hardware_type"`
	OSName    string `json:"os_name"`
	OSVersion string `json:"os_version"`
	LastSeen  any    `json:"last_seen"`
	Online    any    `json:"is_online"`
	Interface string `json:"detected_interface"`
	SwName    string `json:"fortiswitch_name"`
	SwPort    string `json:"fortiswitch_port_name"`
	SwVLAN    any    `json:"fortiswitch_vlan_id"`
	APName    string `json:"fortiap_name"`
	APSSID    string `json:"fortiap_ssid"`
}

func (p *Plugin) read(ctx context.Context, rc *plugin.RunContext, a *api, vdom string) ([]netsrc.Client, error) {
	now := p.clock()
	q := url.Values{}
	if vdom != "" {
		q.Set("vdom", vdom)
	}
	var leases []lease
	if _, err := a.get(ctx, "system/dhcp", q, &leases); err != nil {
		return nil, err
	}
	var out []netsrc.Client
	for _, l := range leases {
		if l.Type == "ipv6" || l.Status == "conflicted" || l.MAC == "" {
			continue
		}
		c := netsrc.Client{MAC: l.MAC, IP: l.IP, Hostname: l.Hostname, Kind: netsrc.KindDHCP, Static: l.Reserved, Interface: l.Interface, SSID: l.SSID,
			Extra: map[string]string{}}
		if n := num(l.Expire); n > 0 {
			c.Expires = time.Unix(n, 0).UTC()
		}
		if l.VCI != "" {
			c.Extra["dhcp_vci"] = l.VCI
		}
		if l.AP != "" {
			c.Extra["fortiap"] = l.AP
		}
		out = append(out, c)
	}
	rc.AddStat("leases", len(leases))
	if rc.Settings.Bool(netsrc.KeyARP) {
		var entries []arp
		if _, err := a.get(ctx, "network/arp", q, &entries); err != nil {
			rc.Log.Warn("ARP-Tabelle nicht lesbar (Recht sysgrp/netgrp lesen?)", "error", err)
		}
		for _, e := range entries {
			c := netsrc.Client{MAC: e.MAC, IP: e.IP, Interface: e.Interface, Kind: netsrc.KindARP}
			if age := num(e.Age); age >= 0 {
				c.LastSeen = now.Add(-time.Duration(age) * time.Second)
			}
			out = append(out, c)
		}
	}
	if rc.Settings.Bool(keyDevices) {
		devices, err := p.devices(ctx, a, q)
		if err != nil {
			rc.Log.Warn("Erkannte Geräte nicht lesbar (Recht „User & Device“ lesen?)", "error", err)
		}
		out = append(out, devices...)
	}
	return out, nil
}

// devices pages through the device identification results.
func (p *Plugin) devices(ctx context.Context, a *api, base url.Values) ([]netsrc.Client, error) {
	var out []netsrc.Client
	for start := 0; ; start += pageSize {
		q := url.Values{}
		for k, v := range base {
			q[k] = v
		}
		q.Set("start", strconv.Itoa(start))
		q.Set("number", strconv.Itoa(pageSize))
		var items []detected
		m, err := a.get(ctx, "user/device/query", q, &items)
		if err != nil {
			return out, err
		}
		for _, d := range items {
			if d.MAC == "" {
				continue
			}
			c := netsrc.Client{MAC: d.MAC, IP: d.IP, Hostname: d.Hostname, Vendor: d.Vendor, Kind: netsrc.KindClient, Interface: d.Interface,
				Extra: map[string]string{}}
			if os := strings.TrimSpace(d.OSName + " " + d.OSVersion); os != "" {
				c.OS = os
			}
			if n := num(d.LastSeen); n > 0 {
				c.LastSeen = time.Unix(n, 0).UTC()
			}
			switch strings.ToLower(fmt.Sprint(d.Online)) {
			case "true", "1":
				c.Online = netsrc.Bool(true)
			case "false", "0":
				c.Online = netsrc.Bool(false)
			}
			if d.SwName != "" {
				c.UplinkName, c.Port, c.Wired = d.SwName, d.SwPort, netsrc.Bool(true)
				c.VLAN = int(num(d.SwVLAN))
			}
			if d.APName != "" {
				c.UplinkName, c.SSID, c.Wired = d.APName, d.APSSID, netsrc.Bool(false)
			}
			if t := strings.TrimSpace(d.Family + " " + d.HWType); t != "" {
				c.Extra["geraet"] = t
			}
			out = append(out, c)
		}
		if len(items) < pageSize || (m.Total > 0 && start+len(items) >= m.Total) {
			return out, nil
		}
	}
}

// num reads a number that FortiOS may send as number or string.
func num(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		if err != nil {
			return -1
		}
		return n
	}
	return -1
}
