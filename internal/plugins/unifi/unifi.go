// Package unifi imports clients and network devices from UniFi Network controllers
// (UniFi OS consoles such as UDM/UCG/Cloud Key, or the classic self-hosted controller).
//
// With username and password it uses the controller API the web interface uses: online
// clients (stat/sta) with switch port or access point, SSID and VLAN, known offline clients
// and fixed IPs (rest/user), networks (rest/networkconf) and the UniFi devices themselves
// (stat/device). With an API key it uses the official Integration API, which only lists
// connected clients with few details.
package unifi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
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

const timeout = 60 * time.Second

// Plugin is the UniFi importer.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "unifi",
		Kind: plugin.KindImporter,
		Name: "UniFi Network",
		Description: "Liest Clients und Netzwerkgeräte aus UniFi-Controllern (UniFi-OS-Konsolen oder selbst gehosteter Controller): " +
			"Namen, feste IPs, VLAN, Switch-Port bzw. Access Point – für die Topologie.",
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
	keySites   = "sites"
	keyOffline = "include_offline"
	keyDevices = "include_devices"
)

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		netsrc.SourcesField("Controller", "", "URL der UniFi-Konsole (https://udm.lan) oder des Controllers (https://controller:8443), einer pro Zeile."),
		netsrc.CredentialsField([]string{plugin.CredPassword, plugin.CredAPIToken},
			"Benutzer/Passwort eines lokalen UniFi-Kontos ohne MFA (volle Daten: Ports, VLAN, bekannte Offline-Geräte) "+
				"oder ein API-Token mit dem API-Schlüssel aus Einstellungen → Control Plane → Integrations (nur verbundene Clients, wenige Details)."),
		{Key: keySites, Type: plugin.FieldStringList, Label: "Sites", Default: []string{"default"},
			Description: "Kurzname der Sites (in der URL …/site/<name>/); * liest alle."},
		{Key: keyOffline, Type: plugin.FieldBool, Label: "Bekannte Clients ohne Verbindung", Default: true,
			Description: "Auch Clients übernehmen, die der Controller kennt, die aber gerade nicht verbunden sind (Namen, feste IPs). Nur mit Benutzer/Passwort."},
		{Key: keyDevices, Type: plugin.FieldBool, Label: "UniFi-Geräte übernehmen", Default: true,
			Description: "Access Points, Switches und Gateways selbst (Modell, Name, Typ); nötig, damit Clients ihrem Access Point bzw. Switch zugeordnet werden."},
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
	return netsrc.Run(ctx, rc, netsrc.Options{PluginID: "unifi", Sources: s.StringList(netsrc.KeySources), Label: "Controller",
		Create: s.Bool(netsrc.KeyCreate)}, func(ctx context.Context, src string) (*netsrc.Result, error) {
		return fetch(ctx, rc, src)
	})
}

func fetch(ctx context.Context, rc *plugin.RunContext, src string) (*netsrc.Result, error) {
	base, err := netsrc.BaseURL(src, "https", 443)
	if err != nil {
		return nil, err
	}
	creds, err := netsrc.Credentials(ctx, rc, []string{plugin.CredPassword, plugin.CredAPIToken}, src)
	if err != nil {
		return nil, err
	}
	var res *netsrc.Result
	err = netsrc.TryCredentials(rc, src, creds, func(c *plugin.Credential) error {
		hc := netsrc.NewHTTPClient(rc.Settings.Bool(netsrc.KeyVerifyTLS), timeout)
		var r *netsrc.Result
		var err error
		if c.Type == plugin.CredAPIToken {
			r, err = integration(ctx, rc, hc, base.String(), c.Get("token"))
		} else {
			r, err = legacy(ctx, rc, hc, base.String(), c.Get("username"), c.Get("password"))
		}
		res = r
		return err
	})
	return res, err
}

// ---------------------------------------------------------------- legacy API

type session struct {
	hc      *http.Client
	base    string // controller URL
	prefix  string // "/proxy/network" on UniFi OS
	csrf    string
	unifiOS bool
}

type envelope struct {
	Meta struct {
		RC  string `json:"rc"`
		Msg string `json:"msg"`
	} `json:"meta"`
	Data json.RawMessage `json:"data"`
}

// login signs in on UniFi OS (/api/auth/login) or, if that does not exist, on a classic
// controller (/api/login).
func login(ctx context.Context, hc *http.Client, base, user, pass string) (*session, error) {
	body, _ := json.Marshal(map[string]any{"username": user, "password": pass, "rememberMe": true})
	h := http.Header{}
	h.Set("Content-Type", "application/json")
	os, err := isUniFiOS(ctx, hc, base)
	if err != nil {
		return nil, err
	}
	if os {
		resp, err := netsrc.Do(ctx, hc, http.MethodPost, base+"/api/auth/login", bytes.NewReader(body), h, nil)
		if err != nil {
			if resp != nil && resp.StatusCode == 499 {
				return nil, errors.New("das UniFi-Konto verlangt MFA – ein lokales Konto ohne MFA verwenden")
			}
			return nil, err
		}
		return &session{hc: hc, base: base, prefix: "/proxy/network", csrf: resp.Header.Get("X-Csrf-Token"), unifiOS: true}, nil
	}
	var env envelope
	if _, err := netsrc.Do(ctx, hc, http.MethodPost, base+"/api/login", bytes.NewReader(body), h, &env); err != nil {
		return nil, err
	}
	if env.Meta.RC != "ok" {
		return nil, fmt.Errorf("%w: %s", netsrc.ErrAuth, env.Meta.Msg)
	}
	return &session{hc: hc, base: base}, nil
}

// isUniFiOS tells a UniFi OS console (the start page answers 200) from a classic
// controller (redirects to /manage).
func isUniFiOS(ctx context.Context, hc *http.Client, base string) (bool, error) {
	noFollow := *hc
	noFollow.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/", nil)
	if err != nil {
		return false, err
	}
	resp, err := noFollow.Do(req)
	if err != nil {
		return false, fmt.Errorf("Controller nicht erreichbar: %w", err)
	}
	resp.Body.Close()
	return resp.StatusCode == http.StatusOK, nil
}

func (s *session) logout() {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := http.Header{}
	if s.csrf != "" {
		h.Set("X-Csrf-Token", s.csrf)
	}
	path := "/api/logout"
	if s.unifiOS {
		path = "/api/auth/logout"
	}
	_, _ = netsrc.Do(ctx, s.hc, http.MethodPost, s.base+path, nil, h, nil)
}

// get reads a controller endpoint (path below /api) into dst (the data array).
func (s *session) get(ctx context.Context, path string, dst any) error {
	var env envelope
	if _, err := netsrc.Do(ctx, s.hc, http.MethodGet, s.base+s.prefix+"/api"+path, nil, nil, &env); err != nil {
		return err
	}
	if env.Meta.RC != "" && env.Meta.RC != "ok" {
		return fmt.Errorf("%s: %s", path, env.Meta.Msg)
	}
	return json.Unmarshal(env.Data, dst)
}

type site struct {
	Name string `json:"name"`
	Desc string `json:"desc"`
}

type device struct {
	MAC     string  `json:"mac"`
	IP      string  `json:"ip"`
	Name    string  `json:"name"`
	Model   string  `json:"model"`
	Type    string  `json:"type"`
	Serial  string  `json:"serial"`
	Version string  `json:"version"`
	State   int     `json:"state"`
	Seen    float64 `json:"last_seen"`
}

type station struct {
	MAC       string  `json:"mac"`
	IP        string  `json:"ip"`
	Hostname  string  `json:"hostname"`
	Name      string  `json:"name"`
	Noted     bool    `json:"noted"`
	OUI       string  `json:"oui"`
	IsWired   *bool   `json:"is_wired"`
	IsGuest   bool    `json:"is_guest"`
	ESSID     string  `json:"essid"`
	APMAC     string  `json:"ap_mac"`
	SWMAC     string  `json:"sw_mac"`
	SWPort    any     `json:"sw_port"`
	Network   string  `json:"network"`
	NetworkID string  `json:"network_id"`
	VLAN      int     `json:"vlan"`
	FixedIP   string  `json:"fixed_ip"`
	UseFixed  bool    `json:"use_fixedip"`
	LastSeen  float64 `json:"last_seen"`
	Signal    int     `json:"signal"`
}

type network struct {
	ID      string `json:"_id"`
	Name    string `json:"name"`
	VLAN    any    `json:"vlan"`
	Enabled *bool  `json:"vlan_enabled"`
}

// deviceType maps UniFi device types to NetScope's.
func deviceType(t string) string {
	switch t {
	case "uap":
		return "access-point"
	case "usw":
		return "switch"
	case "ugw", "udm", "uxg":
		return "router"
	}
	return ""
}

func legacy(ctx context.Context, rc *plugin.RunContext, hc *http.Client, base, user, pass string) (*netsrc.Result, error) {
	s, err := login(ctx, hc, base, user, pass)
	if err != nil {
		return nil, err
	}
	defer s.logout()
	var sites []site
	if err := s.get(ctx, "/self/sites", &sites); err != nil {
		return nil, fmt.Errorf("Sites: %w", err)
	}
	want := rc.Settings.StringList(keySites)
	res := &netsrc.Result{}
	found := 0
	for _, st := range sites {
		if !siteWanted(want, st.Name, st.Desc) {
			continue
		}
		found++
		if err := readSite(ctx, rc, s, st, res); err != nil {
			return nil, fmt.Errorf("Site %s: %w", st.Name, err)
		}
	}
	if found == 0 {
		return nil, fmt.Errorf("keine der Sites %s gefunden (vorhanden: %s)", strings.Join(want, ", "), siteNames(sites))
	}
	return res, nil
}

func siteWanted(want []string, name, desc string) bool {
	if len(want) == 0 {
		return name == "default"
	}
	for _, w := range want {
		w = strings.TrimSpace(w)
		if w == "*" || strings.EqualFold(w, name) || strings.EqualFold(w, desc) {
			return true
		}
	}
	return false
}

func siteNames(sites []site) string {
	var out []string
	for _, s := range sites {
		out = append(out, s.Name)
	}
	return strings.Join(out, ", ")
}

func readSite(ctx context.Context, rc *plugin.RunContext, s *session, st site, res *netsrc.Result) error {
	sp := "/s/" + url.PathEscape(st.Name)
	var devices []device
	if err := s.get(ctx, sp+"/stat/device", &devices); err != nil {
		return fmt.Errorf("Geräte: %w", err)
	}
	names := map[string]string{} // uplink MAC → device name
	for _, d := range devices {
		names[strings.ToLower(d.MAC)] = d.Name
		if rc.Settings.Bool(keyDevices) && d.MAC != "" {
			res.Devices = append(res.Devices, deviceObservation(d, st.Name))
		}
	}
	var nets []network
	if err := s.get(ctx, sp+"/rest/networkconf", &nets); err != nil {
		rc.Log.Warn("Netzwerke nicht lesbar – VLAN-Namen fehlen", "site", st.Name, "error", err)
	}
	netByID := map[string]network{}
	for _, n := range nets {
		netByID[n.ID] = n
	}
	var online []station
	if err := s.get(ctx, sp+"/stat/sta", &online); err != nil {
		return fmt.Errorf("Clients: %w", err)
	}
	for _, c := range online {
		res.Clients = append(res.Clients, stationClient(c, names, netByID, true))
	}
	rc.AddStat("online", len(online))
	if rc.Settings.Bool(keyOffline) {
		var known []station
		if err := s.get(ctx, sp+"/rest/user", &known); err != nil {
			rc.Log.Warn("Bekannte Clients nicht lesbar", "site", st.Name, "error", err)
		}
		for _, c := range known {
			res.Clients = append(res.Clients, stationClient(c, names, netByID, false))
		}
		rc.AddStat("known", len(known))
	}
	return nil
}

func deviceObservation(d device, site string) *plugin.Observation {
	inv := map[string]any{"model": d.Model, "type": d.Type, "serial": d.Serial, "firmware": d.Version, "site": site, "online": d.State == 1}
	return &plugin.Observation{MACs: []string{d.MAC}, IP: d.IP, Target: firstNonEmpty(d.IP, d.MAC), Hostname: d.Name, Vendor: "Ubiquiti",
		Model: d.Model, DeviceType: deviceType(d.Type), Inventory: inv, Create: true}
}

// stationClient maps a client record; online marks records of connected clients.
func stationClient(c station, names map[string]string, nets map[string]network, online bool) netsrc.Client {
	cl := netsrc.Client{MAC: c.MAC, IP: c.IP, Hostname: c.Hostname, Vendor: c.OUI, Kind: netsrc.KindClient, Wired: c.IsWired,
		Interface: c.Network, VLAN: c.VLAN, SSID: c.ESSID, Extra: map[string]string{}}
	if c.Name != "" {
		cl.Name = c.Name // set in the controller by an administrator
	}
	if c.UseFixed && c.FixedIP != "" {
		cl.Static = true
		if cl.IP == "" {
			cl.IP = c.FixedIP
		}
	}
	if n, ok := nets[c.NetworkID]; ok {
		if cl.Interface == "" {
			cl.Interface = n.Name
		}
		if cl.VLAN == 0 {
			cl.VLAN = anyInt(n.VLAN)
		}
	}
	if c.LastSeen > 0 {
		cl.LastSeen = time.Unix(int64(c.LastSeen), 0).UTC()
	}
	if online {
		cl.Online = netsrc.Bool(true)
		switch {
		case c.IsWired != nil && !*c.IsWired && c.APMAC != "":
			cl.UplinkMAC, cl.UplinkName = c.APMAC, names[strings.ToLower(c.APMAC)]
		case c.SWMAC != "":
			cl.UplinkMAC, cl.UplinkName = c.SWMAC, names[strings.ToLower(c.SWMAC)]
			if p := anyInt(c.SWPort); p > 0 {
				cl.Port = strconv.Itoa(p)
			}
		}
		if c.Signal != 0 {
			cl.Extra["signal_dbm"] = strconv.Itoa(c.Signal)
		}
	}
	if !online {
		cl.Online = netsrc.Bool(false) // a connected record of the same client comes first and wins
	}
	if c.IsGuest {
		cl.Extra["gast"] = "ja"
	}
	return cl
}

func anyInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return 0
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

// ---------------------------------------------------------------- Integration API

type page struct {
	Offset     int             `json:"offset"`
	Limit      int             `json:"limit"`
	Count      int             `json:"count"`
	TotalCount int             `json:"totalCount"`
	Data       json.RawMessage `json:"data"`
}

// all reads every page of an Integration API list.
func all[T any](ctx context.Context, hc *http.Client, base, key, path string) ([]T, error) {
	h := http.Header{}
	h.Set("X-API-KEY", key)
	var out []T
	for offset := 0; ; {
		var p page
		u := fmt.Sprintf("%s/proxy/network/integration/v1%s?offset=%d&limit=200", base, path, offset)
		if _, err := netsrc.Do(ctx, hc, http.MethodGet, u, nil, h, &p); err != nil {
			return nil, err
		}
		var items []T
		if err := json.Unmarshal(p.Data, &items); err != nil {
			return nil, err
		}
		out = append(out, items...)
		offset += len(items)
		if len(items) == 0 || offset >= p.TotalCount {
			return out, nil
		}
	}
}

type iSite struct {
	ID       string `json:"id"`
	Internal string `json:"internalReference"`
	Name     string `json:"name"`
}

type iDevice struct {
	ID       string   `json:"id"`
	MAC      string   `json:"macAddress"`
	IP       string   `json:"ipAddress"`
	Name     string   `json:"name"`
	Model    string   `json:"model"`
	State    string   `json:"state"`
	Firmware string   `json:"firmwareVersion"`
	Features []string `json:"features"`
}

type iClient struct {
	Type        string `json:"type"`
	Name        string `json:"name"`
	IP          string `json:"ipAddress"`
	MAC         string `json:"macAddress"`
	Uplink      string `json:"uplinkDeviceId"`
	ConnectedAt string `json:"connectedAt"`
	Access      struct {
		Type string `json:"type"`
	} `json:"access"`
}

func integration(ctx context.Context, rc *plugin.RunContext, hc *http.Client, base, key string) (*netsrc.Result, error) {
	sites, err := all[iSite](ctx, hc, base, key, "/sites")
	if err != nil {
		var se *netsrc.HTTPStatusError
		if errors.As(err, &se) && se.Code == http.StatusNotFound {
			return nil, errors.New("die Integration-API gibt es nur auf UniFi-OS-Konsolen (Network 9 oder neuer) – für selbst gehostete Controller Benutzer/Passwort verwenden")
		}
		return nil, fmt.Errorf("Sites: %w", err)
	}
	want := rc.Settings.StringList(keySites)
	res := &netsrc.Result{}
	found := 0
	for _, st := range sites {
		if !siteWanted(want, st.Internal, st.Name) {
			continue
		}
		found++
		devices, err := all[iDevice](ctx, hc, base, key, "/sites/"+st.ID+"/devices")
		if err != nil {
			return nil, fmt.Errorf("Geräte: %w", err)
		}
		byID := map[string]iDevice{}
		for _, d := range devices {
			byID[d.ID] = d
			if rc.Settings.Bool(keyDevices) && d.MAC != "" {
				res.Devices = append(res.Devices, &plugin.Observation{MACs: []string{d.MAC}, IP: d.IP, Target: firstNonEmpty(d.IP, d.MAC),
					Hostname: d.Name, Vendor: "Ubiquiti", Model: d.Model, DeviceType: featureType(d.Features), Create: true,
					Inventory: map[string]any{"model": d.Model, "state": d.State, "firmware": d.Firmware, "site": st.Name}})
			}
		}
		clients, err := all[iClient](ctx, hc, base, key, "/sites/"+st.ID+"/clients")
		if err != nil {
			return nil, fmt.Errorf("Clients: %w", err)
		}
		for _, c := range clients {
			if c.MAC == "" {
				continue // VPN clients
			}
			cl := netsrc.Client{MAC: c.MAC, IP: c.IP, Name: c.Name, Kind: netsrc.KindClient, Online: netsrc.Bool(true), Extra: map[string]string{}}
			switch c.Type {
			case "WIRED":
				cl.Wired = netsrc.Bool(true)
			case "WIRELESS":
				cl.Wired = netsrc.Bool(false)
			}
			if up, ok := byID[c.Uplink]; ok {
				cl.UplinkMAC, cl.UplinkName = up.MAC, up.Name
			}
			if t, err := time.Parse(time.RFC3339, c.ConnectedAt); err == nil {
				cl.LastSeen = time.Now().UTC()
				cl.Extra["verbunden_seit"] = t.UTC().Format(time.RFC3339)
			}
			if c.Access.Type == "GUEST" {
				cl.Extra["gast"] = "ja"
			}
			res.Clients = append(res.Clients, cl)
		}
	}
	if found == 0 {
		return nil, fmt.Errorf("keine der Sites %s gefunden", strings.Join(want, ", "))
	}
	return res, nil
}

func featureType(features []string) string {
	has := map[string]bool{}
	for _, f := range features {
		has[f] = true
	}
	switch {
	case has["gateway"] || has["routing"]:
		return "router"
	case has["switching"] && !has["accessPoint"]:
		return "switch"
	case has["accessPoint"]:
		return "access-point"
	}
	return ""
}
