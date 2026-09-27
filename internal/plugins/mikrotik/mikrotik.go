// Package mikrotik imports DHCP leases, the ARP table and the bridge host table of
// MikroTik routers and switches through the REST API of RouterOS 7 (service www-ssl, a
// user with the policies read and rest-api).
package mikrotik

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
	"netscope/internal/plugins/netsrc"
)

func init() { plugin.Register(&Plugin{}) }

const timeout = 60 * time.Second // RouterOS aborts REST commands after 60 s

// Plugin is the MikroTik importer.
type Plugin struct {
	now func() time.Time
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "mikrotik",
		Kind: plugin.KindImporter,
		Name: "MikroTik RouterOS",
		Description: "Liest DHCP-Leases, die ARP-Tabelle und die Bridge-Hosttabelle (welches Gerät an welchem Port) von MikroTik-Routern " +
			"und -Switches über die REST-API von RouterOS 7.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultSchedule:    "*/10 * * * *",
		DefaultTimeout:     3 * time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     1,
		Targets:            plugin.TargetNone,
	}
}

const keyBridge = "include_bridge"

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		netsrc.SourcesField("Router und Switches", "", "Hostname, IP oder URL (https://router oder http://router:8080), eins pro Zeile. Der Dienst www-ssl muss aktiv sein."),
		netsrc.CredentialsField([]string{plugin.CredPassword}, "Benutzer/Passwort eines RouterOS-Benutzers einer Gruppe mit den Rechten read und rest-api."),
		netsrc.ARPField(),
		{Key: keyBridge, Type: plugin.FieldBool, Label: "Bridge-Ports übernehmen", Default: true,
			Description: "Aus der Bridge-Hosttabelle: an welchem Port ein Gerät hängt (für die Topologie)."},
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
	return netsrc.Run(ctx, rc, netsrc.Options{PluginID: "mikrotik", Sources: s.StringList(netsrc.KeySources), Label: "Router",
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

type client struct {
	hc         *http.Client
	base       string
	user, pass string
}

// list reads a menu, e.g. "ip/arp", with only the given properties (the lease property
// class-id may contain a raw NUL byte that breaks the JSON).
func (c *client) list(ctx context.Context, menu string, props ...string) ([]map[string]string, error) {
	u := c.base + "/rest/" + menu
	if len(props) > 0 {
		u += "?.proplist=" + url.QueryEscape(strings.Join(props, ","))
	}
	h := netsrc.BasicAuth(c.user, c.pass)
	var out []map[string]string
	if _, err := netsrc.Do(ctx, c.hc, http.MethodGet, u, nil, h, &out); err != nil {
		return nil, fmt.Errorf("/%s: %w", menu, err)
	}
	return out, nil
}

func (p *Plugin) fetch(ctx context.Context, rc *plugin.RunContext, src string) (*netsrc.Result, error) {
	base, err := netsrc.BaseURL(src, "https", 443)
	if err != nil {
		return nil, err
	}
	creds, err := netsrc.Credentials(ctx, rc, []string{plugin.CredPassword}, src)
	if err != nil {
		return nil, err
	}
	hc := netsrc.NewHTTPClient(rc.Settings.Bool(netsrc.KeyVerifyTLS), timeout)
	routerIP, _ := netutil.ResolveHost(ctx, base.Hostname())
	var res *netsrc.Result
	err = netsrc.TryCredentials(rc, src, creds, func(cr *plugin.Credential) error {
		c := &client{hc: hc, base: base.String(), user: cr.Get("username"), pass: cr.Get("password")}
		r, err := p.read(ctx, rc, c, routerIP)
		res = r
		return err
	})
	return res, err
}

func (p *Plugin) read(ctx context.Context, rc *plugin.RunContext, c *client, routerIP string) (*netsrc.Result, error) {
	now := p.clock()
	servers, err := c.list(ctx, "ip/dhcp-server", "name", "interface")
	if err != nil {
		return nil, err
	}
	serverIf := map[string]string{}
	for _, s := range servers {
		serverIf[s["name"]] = s["interface"]
	}
	leases, err := c.list(ctx, "ip/dhcp-server/lease", "address", "mac-address", "active-address", "active-mac-address", "host-name",
		"comment", "dynamic", "disabled", "blocked", "status", "expires-after", "last-seen", "server")
	if err != nil {
		return nil, err
	}
	var clients []netsrc.Client
	for _, l := range leases {
		if cl, ok := lease(l, serverIf, now); ok {
			clients = append(clients, cl)
		}
	}
	rc.AddStat("leases", len(leases))
	if rc.Settings.Bool(netsrc.KeyARP) {
		arp, err := c.list(ctx, "ip/arp", "address", "mac-address", "interface", "dynamic", "complete", "status", "disabled", "invalid", "published")
		if err != nil {
			return nil, err
		}
		for _, a := range arp {
			if cl, ok := arpEntry(a, now); ok {
				clients = append(clients, cl)
			}
		}
	}
	if rc.Settings.Bool(keyBridge) {
		hosts, err := c.list(ctx, "interface/bridge/host", "mac-address", "on-interface", "vid", "local", "external", "invalid", "disabled")
		if err != nil {
			rc.Log.Warn("Bridge-Hosttabelle nicht lesbar", "error", err)
		}
		ports := bridgePorts(hosts)
		for i := range clients {
			mac, _ := netutil.NormalizeMAC(clients[i].MAC)
			if bp, ok := ports[mac]; ok {
				clients[i].Port = bp.port
				clients[i].UplinkIP = routerIP
				clients[i].Wired = netsrc.Bool(true)
				if clients[i].VLAN == 0 {
					clients[i].VLAN = bp.vid
				}
			}
		}
	}
	return &netsrc.Result{Clients: clients}, nil
}

// ---------------------------------------------------------------- rows

func truth(v string) bool { return v == "true" || v == "yes" }

// durRe matches RouterOS durations like "1w2d3h4m5s", "8m51s", "00:05:00".
var durRe = regexp.MustCompile(`^(?:(\d+)w)?(?:(\d+)d)?(?:(\d+)h)?(?:(\d+)m)?(?:(\d+)s)?(?:(\d+)ms)?$`)

// parseDuration reads a RouterOS duration ("" or "never": false).
func parseDuration(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "never" {
		return 0, false
	}
	if strings.Count(s, ":") == 2 {
		var h, m, sec int
		if _, err := fmt.Sscanf(s, "%d:%d:%d", &h, &m, &sec); err == nil {
			return time.Duration(h)*time.Hour + time.Duration(m)*time.Minute + time.Duration(sec)*time.Second, true
		}
	}
	m := durRe.FindStringSubmatch(s)
	if m == nil || s == "" {
		return 0, false
	}
	units := []time.Duration{7 * 24 * time.Hour, 24 * time.Hour, time.Hour, time.Minute, time.Second, time.Millisecond}
	var d time.Duration
	for i, u := range units {
		if m[i+1] != "" {
			n, _ := strconv.Atoi(m[i+1])
			d += time.Duration(n) * u
		}
	}
	return d, true
}

func lease(l map[string]string, serverIf map[string]string, now time.Time) (netsrc.Client, bool) {
	if truth(l["disabled"]) || truth(l["blocked"]) {
		return netsrc.Client{}, false
	}
	static := l["dynamic"] == "false"
	c := netsrc.Client{MAC: first(l["active-mac-address"], l["mac-address"]), IP: first(l["active-address"], l["address"]),
		Hostname: l["host-name"], Kind: netsrc.KindDHCP, Static: static, Interface: first(serverIf[l["server"]], l["server"])}
	if static {
		c.Name = strings.TrimSpace(l["comment"])
		if l["status"] != "bound" {
			c.Kind = netsrc.KindStatic
		}
	} else if l["status"] != "bound" {
		return c, false // offered, declined, … – not a lease in use
	}
	if d, ok := parseDuration(l["expires-after"]); ok {
		c.Expires = now.Add(d)
	}
	if d, ok := parseDuration(l["last-seen"]); ok {
		c.LastSeen = now.Add(-d)
	}
	return c, c.MAC != ""
}

func arpEntry(a map[string]string, now time.Time) (netsrc.Client, bool) {
	if truth(a["disabled"]) || truth(a["invalid"]) || truth(a["published"]) || a["complete"] == "false" {
		return netsrc.Client{}, false
	}
	switch a["status"] {
	case "failed", "incomplete", "permanent":
		return netsrc.Client{}, false
	}
	c := netsrc.Client{MAC: a["mac-address"], IP: a["address"], Interface: a["interface"], Kind: netsrc.KindARP}
	if a["status"] == "reachable" {
		c.LastSeen = now
	}
	return c, c.MAC != "" && c.IP != ""
}

type bridgePort struct {
	port string
	vid  int
}

// bridgePorts maps learned MAC addresses to the bridge port they were seen on.
func bridgePorts(hosts []map[string]string) map[string]bridgePort {
	out := map[string]bridgePort{}
	for _, h := range hosts {
		if truth(h["local"]) || truth(h["external"]) || truth(h["invalid"]) || truth(h["disabled"]) || h["on-interface"] == "" {
			continue
		}
		mac, ok := netutil.NormalizeMAC(h["mac-address"])
		if !ok {
			continue
		}
		vid, _ := strconv.Atoi(h["vid"])
		out[mac] = bridgePort{port: h["on-interface"], vid: vid}
	}
	return out
}

func first(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}
