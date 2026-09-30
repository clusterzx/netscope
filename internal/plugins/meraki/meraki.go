// Package meraki imports clients and devices of Cisco Meraki networks through the Meraki
// Dashboard API v1 (cloud): client names, addresses, VLAN, SSID, switch port or access
// point, OS and manufacturer, fixed IP assignments of MX appliances, and the Meraki
// devices themselves.
package meraki

import (
	"context"

	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugins/netsrc"
)

func init() { plugin.Register(&Plugin{}) }

const (
	timeout    = 60 * time.Second
	defaultAPI = "https://api.meraki.com/api/v1"
	maxRetries = 5
)

// Plugin is the Meraki importer.
type Plugin struct {
	// sleep waits for rate limits (replaced in tests).
	sleep func(ctx context.Context, d time.Duration) error
	// insecureTLS skips certificate checks (tests with a local server only).
	insecureTLS bool
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "meraki",
		Kind: plugin.KindImporter,
		Name: "Cisco Meraki",
		Description: "Liest Clients und Geräte aus Meraki-Netzen über die Dashboard-API: Namen, Adressen, VLAN, SSID, Switch-Port " +
			"bzw. Access Point, Betriebssystem, feste IP-Zuweisungen der MX-Appliances.",
		Version:            "1.0.0",
		Category:           plugin.CategoryControllers,
		DefaultEnabled:     false,
		DefaultSchedule:    "*/15 * * * *",
		DefaultTimeout:     5 * time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     1,
		Targets:            plugin.TargetNone,
	}
}

const (
	keyAPI      = "api_url"
	keyOrgs     = "organizations"
	keyNetworks = "networks"
	keyTimespan = "timespan"
	keyDevices  = "include_devices"
)

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		netsrc.CredentialsField([]string{plugin.CredAPIToken},
			"API-Token mit dem API-Schlüssel eines Dashboard-Benutzers (Mein Profil → API-Zugang); Lesezugriff auf die Organisation genügt. "+
				"Der Geltungsbereich des Credentials bezieht sich auf api.meraki.com."),
		{Key: keyOrgs, Type: plugin.FieldStringList, Label: "Organisationen", Default: []string{},
			Description: "Name oder ID; leer = alle, auf die der Schlüssel Zugriff hat."},
		{Key: keyNetworks, Type: plugin.FieldStringList, Label: "Netzwerke", Default: []string{},
			Description: "Name oder ID; leer = alle Netzwerke der Organisationen."},
		{Key: keyTimespan, Type: plugin.FieldDuration, Label: "Clients der letzten", Default: "24h",
			Description: "Zeitraum, in dem Clients gesehen worden sein müssen (höchstens 31 Tage)."},
		{Key: keyDevices, Type: plugin.FieldBool, Label: "Meraki-Geräte übernehmen", Default: true,
			Description: "Access Points, Switches und Appliances selbst; nötig, damit Clients ihrem Access Point bzw. Switch zugeordnet werden."},
		netsrc.CreateField(),
		{Key: keyAPI, Type: plugin.FieldString, Label: "API-URL", Default: defaultAPI, Advanced: true,
			Description: "Nur für andere Meraki-Regionen ändern (z. B. https://api.meraki.cn/api/v1).", Validation: &plugin.Validation{Format: "url"}},
	}}
}

// Endpoints implements plugin.EndpointProvider.
func (p *Plugin) Endpoints(s plugin.Settings) []string { return []string{netsrc.Host(apiURL(s))} }

func apiURL(s plugin.Settings) string {
	if u := strings.TrimRight(strings.TrimSpace(s.String(keyAPI)), "/"); u != "" {
		return u
	}
	return defaultAPI
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	o, fetch := p.sources(rc)
	return netsrc.Run(ctx, rc, o, fetch)
}

// TestConnection implements plugin.ConnectionTester.
func (p *Plugin) TestConnection(ctx context.Context, rc *plugin.RunContext) ([]plugin.ConnectionResult, error) {
	o, fetch := p.sources(rc)
	return netsrc.Test(ctx, rc, o, fetch)
}

// sources describes the configured systems and how one is read.
func (p *Plugin) sources(rc *plugin.RunContext) (netsrc.Options, netsrc.Fetch) {
	s := rc.Settings
	return netsrc.Options{PluginID: "meraki", Sources: []string{apiURL(s)}, Label: "Meraki-Dashboard",
			Create: s.Bool(netsrc.KeyCreate)}, func(ctx context.Context, src string) (*netsrc.Result, error) {
			return p.fetch(ctx, rc, src)
		}
}

type client struct {
	p     *Plugin
	hc    *http.Client
	base  string
	token string
}

// sameSite keeps the Authorization header on redirects between Meraki hosts (the API
// redirects to shards like n123.meraki.com; Go drops the header on such hops).
func sameSite(base string) func(*http.Request, []*http.Request) error {
	host := netsrc.Host(base)
	parent := host
	if i := strings.Index(host, "."); i > 0 {
		parent = host[i:] // ".meraki.com"
	}
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 10 {
			return errors.New("zu viele Weiterleitungen")
		}
		h := req.URL.Hostname()
		if req.URL.Scheme == "https" && (h == host || strings.HasSuffix(h, parent)) {
			req.Header.Set("Authorization", via[0].Header.Get("Authorization"))
		}
		return nil
	}
}

var nextRe = regexp.MustCompile(`<([^>]+)>;\s*rel="?next"?`)

// get reads one URL, waits out rate limits, and returns the next page's URL.
func (c *client) get(ctx context.Context, u string, dst any) (string, error) {
	h := http.Header{}
	h.Set("Authorization", "Bearer "+c.token)
	for attempt := 0; ; attempt++ {
		resp, err := netsrc.Do(ctx, c.hc, http.MethodGet, u, nil, h, dst)
		if err == nil {
			if m := nextRe.FindStringSubmatch(resp.Header.Get("Link")); m != nil {
				return m[1], nil
			}
			return "", nil
		}
		if resp == nil || resp.StatusCode != http.StatusTooManyRequests || attempt >= maxRetries {
			return "", err
		}
		wait := time.Second << attempt
		if s, e := strconv.Atoi(resp.Header.Get("Retry-After")); e == nil && s > 0 {
			wait = time.Duration(s) * time.Second
		}
		if err := c.p.wait(ctx, wait); err != nil {
			return "", err
		}
	}
}

func (p *Plugin) wait(ctx context.Context, d time.Duration) error {
	if p.sleep != nil {
		return p.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// list reads every page of a list endpoint.
func list[T any](ctx context.Context, c *client, path string, q url.Values) ([]T, error) {
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	var out []T
	for u != "" {
		var page []T
		next, err := c.get(ctx, u, &page)
		if err != nil {
			return nil, err
		}
		out = append(out, page...)
		u = next
	}
	return out, nil
}

type org struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type network struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	ProductTypes []string `json:"productTypes"`
}

type device struct {
	Name        string `json:"name"`
	Serial      string `json:"serial"`
	MAC         string `json:"mac"`
	LanIP       string `json:"lanIp"`
	Model       string `json:"model"`
	NetworkID   string `json:"networkId"`
	ProductType string `json:"productType"`
	Firmware    string `json:"firmware"`
}

type mclient struct {
	MAC         string `json:"mac"`
	IP          string `json:"ip"`
	Description string `json:"description"`
	FirstSeen   any    `json:"firstSeen"`
	LastSeen    any    `json:"lastSeen"`
	Vendor      string `json:"manufacturer"`
	OS          string `json:"os"`
	User        string `json:"user"`
	VLAN        any    `json:"vlan"`
	NamedVLAN   string `json:"namedVlan"`
	SSID        string `json:"ssid"`
	Switchport  any    `json:"switchport"`
	Serial      string `json:"recentDeviceSerial"`
	DeviceName  string `json:"recentDeviceName"`
	Connection  string `json:"recentDeviceConnection"`
	Status      string `json:"status"`
	Prediction  string `json:"deviceTypePrediction"`
}

func (p *Plugin) fetch(ctx context.Context, rc *plugin.RunContext, base string) (*netsrc.Result, error) {
	creds, err := netsrc.Credentials(ctx, rc, []string{plugin.CredAPIToken}, base)
	if err != nil {
		return nil, err
	}
	var res *netsrc.Result
	err = netsrc.TryCredentials(rc, base, creds, func(cr *plugin.Credential) error {
		hc := netsrc.NewHTTPClient(!p.insecureTLS, timeout)
		hc.CheckRedirect = sameSite(base)
		c := &client{p: p, hc: hc, base: base, token: cr.Get("token")}
		r, err := p.read(ctx, rc, c)
		res = r
		return err
	})
	return res, err
}

func wanted(filter []string, id, name string) bool {
	if len(filter) == 0 {
		return true
	}
	for _, f := range filter {
		f = strings.TrimSpace(f)
		if f == id || strings.EqualFold(f, name) {
			return true
		}
	}
	return false
}

func (p *Plugin) read(ctx context.Context, rc *plugin.RunContext, c *client) (*netsrc.Result, error) {
	s := rc.Settings
	orgs, err := list[org](ctx, c, "/organizations", url.Values{"perPage": {"9000"}})
	if err != nil {
		return nil, fmt.Errorf("Organisationen: %w", err)
	}
	span := s.Duration(keyTimespan)
	if span <= 0 || span > 31*24*time.Hour {
		span = 24 * time.Hour
	}
	res := &netsrc.Result{}
	found := 0
	for _, o := range orgs {
		if !wanted(s.StringList(keyOrgs), o.ID, o.Name) {
			continue
		}
		found++
		nets, err := list[network](ctx, c, "/organizations/"+url.PathEscape(o.ID)+"/networks", url.Values{"perPage": {"1000"}})
		if err != nil {
			return nil, fmt.Errorf("Netzwerke von %s: %w", o.Name, err)
		}
		devices, err := list[device](ctx, c, "/organizations/"+url.PathEscape(o.ID)+"/devices", url.Values{"perPage": {"1000"}})
		if err != nil {
			return nil, fmt.Errorf("Geräte von %s: %w", o.Name, err)
		}
		bySerial := map[string]device{}
		for _, d := range devices {
			bySerial[d.Serial] = d
			if s.Bool(keyDevices) && d.MAC != "" {
				res.Devices = append(res.Devices, deviceObservation(d, o.Name))
			}
		}
		for _, n := range nets {
			if !wanted(s.StringList(keyNetworks), n.ID, n.Name) {
				continue
			}
			clients, err := list[mclient](ctx, c, "/networks/"+url.PathEscape(n.ID)+"/clients",
				url.Values{"perPage": {"1000"}, "timespan": {strconv.Itoa(int(span.Seconds()))}})
			if err != nil {
				return nil, fmt.Errorf("Clients von %s: %w", n.Name, err)
			}
			for _, cl := range clients {
				res.Clients = append(res.Clients, merakiClient(cl, bySerial, n.Name))
			}
			rc.AddStat("networks", 1)
			if contains(n.ProductTypes, "appliance") {
				fixed, err := fixedIPs(ctx, c, n)
				if err != nil {
					rc.Log.Debug("Feste IP-Zuweisungen nicht lesbar", "netzwerk", n.Name, "error", err)
				}
				res.Clients = append(res.Clients, fixed...)
			}
		}
	}
	if found == 0 {
		return nil, fmt.Errorf("keine passende Organisation (vorhanden: %s)", orgNames(orgs))
	}
	return res, nil
}

func orgNames(orgs []org) string {
	var out []string
	for _, o := range orgs {
		out = append(out, o.Name)
	}
	return strings.Join(out, ", ")
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func deviceType(productType string) string {
	switch productType {
	case "wireless":
		return "access-point"
	case "switch":
		return "switch"
	case "appliance":
		return "firewall"
	case "cellularGateway":
		return "router"
	case "camera":
		return "camera"
	case "sensor":
		return "iot"
	}
	return ""
}

func deviceObservation(d device, org string) *plugin.Observation {
	return &plugin.Observation{MACs: []string{d.MAC}, IP: d.LanIP, Target: firstNonEmpty(d.LanIP, d.MAC), Hostname: d.Name, Vendor: "Cisco Meraki",
		Model: d.Model, DeviceType: deviceType(d.ProductType), Create: true,
		Inventory: map[string]any{"serial": d.Serial, "model": d.Model, "firmware": d.Firmware, "productType": d.ProductType, "organization": org}}
}

func merakiClient(c mclient, bySerial map[string]device, networkName string) netsrc.Client {
	cl := netsrc.Client{MAC: c.MAC, IP: c.IP, Name: c.Description, Vendor: c.Vendor, OS: c.OS, Kind: netsrc.KindClient, SSID: c.SSID,
		Interface: firstNonEmpty(c.NamedVLAN, networkName), Extra: map[string]string{"netzwerk": networkName}}
	cl.VLAN, _ = strconv.Atoi(fmt.Sprint(c.VLAN))
	if t := timeOf(c.LastSeen); !t.IsZero() {
		cl.LastSeen = t
	}
	switch c.Status {
	case "Online":
		cl.Online = netsrc.Bool(true)
	case "Offline":
		cl.Online = netsrc.Bool(false)
	}
	switch c.Connection {
	case "Wired":
		cl.Wired = netsrc.Bool(true)
	case "Wireless":
		cl.Wired = netsrc.Bool(false)
	}
	if d, ok := bySerial[c.Serial]; ok {
		cl.UplinkMAC, cl.UplinkName = d.MAC, d.Name
	} else if c.DeviceName != "" {
		cl.UplinkName = c.DeviceName
	}
	if sp := c.Switchport; sp != nil && fmt.Sprint(sp) != "" {
		cl.Port = fmt.Sprint(sp)
	}
	if c.User != "" {
		cl.Extra["benutzer"] = c.User
	}
	if c.Prediction != "" {
		cl.Extra["geraet"] = c.Prediction
	}
	return cl
}

// timeOf reads Meraki timestamps, which arrive as epoch seconds or ISO strings.
func timeOf(v any) time.Time {
	switch x := v.(type) {
	case float64:
		if x > 0 {
			return time.Unix(int64(x), 0).UTC()
		}
	case string:
		if t, err := time.Parse(time.RFC3339, x); err == nil {
			return t.UTC()
		}
		if n, err := strconv.ParseInt(x, 10, 64); err == nil && n > 0 {
			return time.Unix(n, 0).UTC()
		}
	}
	return time.Time{}
}

// fixedIPs reads the DHCP reservations of an MX appliance network.
func fixedIPs(ctx context.Context, c *client, n network) ([]netsrc.Client, error) {
	var vlans []struct {
		ID    any    `json:"id"`
		Name  string `json:"name"`
		Fixed map[string]struct {
			IP   string `json:"ip"`
			Name string `json:"name"`
		} `json:"fixedIpAssignments"`
	}
	if _, err := c.get(ctx, c.base+"/networks/"+url.PathEscape(n.ID)+"/appliance/vlans", &vlans); err != nil {
		return nil, err
	}
	var out []netsrc.Client
	for _, v := range vlans {
		vid, _ := strconv.Atoi(fmt.Sprint(v.ID))
		for mac, f := range v.Fixed {
			out = append(out, netsrc.Client{MAC: mac, IP: f.IP, Name: f.Name, Kind: netsrc.KindStatic, Static: true, VLAN: vid,
				Interface: firstNonEmpty(v.Name, n.Name)})
		}
	}
	return out, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}
