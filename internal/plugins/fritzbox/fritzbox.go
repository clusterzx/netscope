// Package fritzbox imports the host list of AVM FRITZ!Box routers through TR-064 (SOAP
// with HTTP digest authentication): name, address, MAC, LAN or Wi-Fi, online state.
package fritzbox

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugins/netsrc"
)

func init() { plugin.Register(&Plugin{}) }

const (
	timeout     = 30 * time.Second
	hostsURN    = "urn:dslforum-org:service:Hosts:1"
	hostsURL    = "/upnp/control/hosts"
	maxFallback = 2000
)

// Plugin is the FRITZ!Box importer.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "fritzbox",
		Kind: plugin.KindImporter,
		Name: "FRITZ!Box",
		Description: "Liest die Geräteliste von FRITZ!Box-Routern über TR-064: Namen, Adressen, LAN oder WLAN und ob ein Gerät " +
			"gerade verbunden ist.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultSchedule:    "*/10 * * * *",
		DefaultTimeout:     2 * time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     1,
		Targets:            plugin.TargetNone,
	}
}

const keyInactive = "include_inactive"

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		netsrc.SourcesField("FRITZ!Box", "fritz.box", "Hostname oder IP; https://… nutzt den verschlüsselten Port 49443. Eine pro Zeile."),
		netsrc.CredentialsField([]string{plugin.CredPassword},
			"Benutzer/Passwort eines FRITZ!Box-Benutzers mit dem Recht „FRITZ!Box Einstellungen“. Unter Heimnetz → Netzwerk → "+
				"Netzwerkeinstellungen muss „Zugriff für Anwendungen zulassen“ aktiv sein."),
		{Key: keyInactive, Type: plugin.FieldBool, Label: "Auch nicht verbundene Geräte", Default: true,
			Description: "Die FRITZ!Box merkt sich Geräte, die gerade nicht verbunden sind; auch ihre Namen übernehmen."},
		netsrc.CreateField(),
		netsrc.VerifyTLSField(),
	}}
}

// base turns an entry into the TR-064 base URL (http :49000, https :49443).
func base(entry string) (string, error) {
	u, err := netsrc.BaseURL(entry, "http", 49000)
	if err != nil {
		return "", err
	}
	if u.Port() == "" {
		port := 49000
		if u.Scheme == "https" {
			port = 49443
		}
		u.Host = fmt.Sprintf("%s:%d", u.Hostname(), port)
		if strings.Contains(u.Hostname(), ":") {
			u.Host = fmt.Sprintf("[%s]:%d", u.Hostname(), port)
		}
	}
	u.Path = ""
	return u.String(), nil
}

func parseSource(s string) error {
	_, err := base(s)
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
	return netsrc.Run(ctx, rc, netsrc.Options{PluginID: "fritzbox", Sources: s.StringList(netsrc.KeySources), Label: "FRITZ!Box",
		Create: s.Bool(netsrc.KeyCreate), Filter: func(c *netsrc.Client) bool {
			return s.Bool(keyInactive) || c.Online == nil || *c.Online
		}}, func(ctx context.Context, src string) (*netsrc.Result, error) {
		return fetch(ctx, rc, src)
	})
}

func fetch(ctx context.Context, rc *plugin.RunContext, src string) (*netsrc.Result, error) {
	b, err := base(src)
	if err != nil {
		return nil, err
	}
	creds, err := netsrc.Credentials(ctx, rc, []string{plugin.CredPassword}, src)
	if err != nil {
		return nil, err
	}
	var res *netsrc.Result
	err = netsrc.TryCredentials(rc, src, creds, func(c *plugin.Credential) error {
		plain := netsrc.NewHTTPClient(rc.Settings.Bool(netsrc.KeyVerifyTLS), timeout)
		authed := &http.Client{Timeout: timeout, Transport: &netsrc.DigestTransport{User: c.Get("username"), Password: c.Get("password"),
			Base: plain.Transport}}
		box := &box{base: b, auth: authed, plain: plain}
		clients, err := box.hosts(ctx, rc)
		res = &netsrc.Result{Clients: clients}
		return err
	})
	return res, err
}

type box struct {
	base  string
	auth  *http.Client // SOAP calls
	plain *http.Client // the host list URL carries its own session id
}

// soap calls an action of the Hosts service and returns the response's child elements.
func (b *box) soap(ctx context.Context, action string, args map[string]string) (map[string]string, error) {
	var body strings.Builder
	body.WriteString(`<?xml version="1.0" encoding="utf-8"?><s:Envelope xmlns:s="http://schemas.xmlsoap.org/soap/envelope/" s:encodingStyle="http://schemas.xmlsoap.org/soap/encoding/"><s:Body>`)
	fmt.Fprintf(&body, `<u:%s xmlns:u="%s">`, action, hostsURN)
	for k, v := range args {
		fmt.Fprintf(&body, "<%s>", k)
		_ = xml.EscapeText(&body, []byte(v))
		fmt.Fprintf(&body, "</%s>", k)
	}
	fmt.Fprintf(&body, "</u:%s></s:Body></s:Envelope>", action)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, b.base+hostsURL, strings.NewReader(body.String()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("SOAPACTION", fmt.Sprintf(`"%s#%s"`, hostsURN, action))
	resp, err := b.auth.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	switch {
	case resp.StatusCode == http.StatusUnauthorized:
		return nil, fmt.Errorf("%w (TR-064)", netsrc.ErrAuth)
	case resp.StatusCode == http.StatusServiceUnavailable:
		return nil, errors.New("FRITZ!Box sperrt die Anmeldung vorübergehend nach Fehlversuchen (HTTP 503)")
	case resp.StatusCode != http.StatusOK:
		code := faultCode(data)
		if code == "606" {
			return nil, fmt.Errorf("%s: dem Benutzer fehlt das Recht „FRITZ!Box Einstellungen“ (UPnP-Fehler 606)", action)
		}
		return nil, fmt.Errorf("%s: HTTP %d %s", action, resp.StatusCode, code)
	}
	return responseValues(data), nil
}

// responseValues collects the leaf elements of a SOAP response.
func responseValues(data []byte) map[string]string {
	out := map[string]string{}
	dec := xml.NewDecoder(bytes.NewReader(data))
	var name string
	for {
		tok, err := dec.Token()
		if err != nil {
			return out
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name = t.Name.Local
		case xml.CharData:
			if name != "" {
				out[name] += string(t)
			}
		case xml.EndElement:
			name = ""
		}
	}
}

var faultRe = regexp.MustCompile(`<errorCode>(\d+)</errorCode>`)

func faultCode(data []byte) string {
	if m := faultRe.FindSubmatch(data); m != nil {
		return string(m[1])
	}
	return ""
}

// hosts reads the host list; without the right for the list it falls back to reading every
// host entry (slower, but these calls need no special right and also tell the address
// source).
func (b *box) hosts(ctx context.Context, rc *plugin.RunContext) ([]netsrc.Client, error) {
	v, err := b.soap(ctx, "X_AVM-DE_GetHostListPath", nil)
	if err == nil && v["NewX_AVM-DE_HostListPath"] != "" {
		list, lerr := b.list(ctx, v["NewX_AVM-DE_HostListPath"])
		if lerr == nil {
			return list, nil
		}
		rc.Log.Info("Hostliste der FRITZ!Box nicht lesbar, Einzelabfrage", "error", lerr)
	} else if err != nil {
		if errors.Is(err, netsrc.ErrAuth) {
			return nil, err
		}
		rc.Log.Info("Hostliste der FRITZ!Box nicht verfügbar, Einzelabfrage", "error", err)
	}
	n, err := b.soap(ctx, "GetHostNumberOfEntries", nil)
	if err != nil {
		return nil, err
	}
	count, _ := strconv.Atoi(n["NewHostNumberOfEntries"])
	var out []netsrc.Client
	for i := 0; i < min(count, maxFallback); i++ {
		e, err := b.soap(ctx, "GetGenericHostEntry", map[string]string{"NewIndex": strconv.Itoa(i)})
		if err != nil {
			return nil, err
		}
		c := netsrc.Client{MAC: e["NewMACAddress"], IP: e["NewIPAddress"], Hostname: hostName(e["NewHostName"]), Kind: netsrc.KindClient,
			Online: netsrc.Bool(e["NewActive"] == "1"), Wired: wired(e["NewInterfaceType"]), Static: e["NewAddressSource"] == "Static"}
		if secs, err := strconv.Atoi(e["NewLeaseTimeRemaining"]); err == nil && secs > 0 && e["NewAddressSource"] == "DHCP" {
			c.Expires = time.Now().Add(time.Duration(secs) * time.Second)
		}
		out = append(out, c)
	}
	return out, nil
}

type hostList struct {
	Items []struct {
		IP        string `xml:"IPAddress"`
		MAC       string `xml:"MACAddress"`
		Active    string `xml:"Active"`
		HostName  string `xml:"HostName"`
		Interface string `xml:"InterfaceType"`
		Port      string `xml:"X_AVM-DE_Port"`
		Speed     string `xml:"X_AVM-DE_Speed"`
		Guest     string `xml:"X_AVM-DE_Guest"`
		Model     string `xml:"X_AVM-DE_Model"`
		Class     string `xml:"X_AVM-DE_DeviceClass"`
		ClassUser string `xml:"X_AVM-DE_DeviceClassUser"`
		Friendly  string `xml:"X_AVM-DE_FriendlyName"`
	} `xml:"Item"`
}

// list fetches the XML host list from the path the box returned.
func (b *box) list(ctx context.Context, path string) ([]netsrc.Client, error) {
	u := path
	if !strings.HasPrefix(path, "http") {
		u = b.base + "/" + strings.TrimPrefix(path, "/")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := b.plain.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("Hostliste: HTTP %d", resp.StatusCode)
	}
	var l hostList
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&l); err != nil {
		return nil, fmt.Errorf("Hostliste: %w", err)
	}
	out := make([]netsrc.Client, 0, len(l.Items))
	for _, it := range l.Items {
		c := netsrc.Client{MAC: it.MAC, IP: it.IP, Hostname: hostName(firstNonEmpty(it.Friendly, it.HostName)), Kind: netsrc.KindClient,
			Online: netsrc.Bool(it.Active == "1"), Wired: wired(it.Interface), Extra: map[string]string{}}
		if p, _ := strconv.Atoi(it.Port); p > 0 {
			c.Port = "LAN " + it.Port
		}
		if s, _ := strconv.Atoi(it.Speed); s > 0 {
			c.Extra["speed_mbit"] = it.Speed
		}
		if it.Guest == "1" {
			c.Extra["gast"] = "ja"
		}
		if it.Model != "" {
			c.Extra["modell"] = it.Model
		}
		if cls := firstNonEmpty(it.ClassUser, it.Class); cls != "" && cls != "Generic" {
			c.Extra["geraeteklasse"] = cls
		}
		out = append(out, c)
	}
	return out, nil
}

func wired(t string) *bool {
	switch t {
	case "Ethernet", "HomePlug":
		return netsrc.Bool(true)
	case "802.11":
		return netsrc.Bool(false)
	}
	return nil
}

// autoName matches the names the box invents for unnamed devices (PC-192-168-178-24).
var autoName = regexp.MustCompile(`^PC-\d+-\d+-\d+-\d+$`)

func hostName(n string) string {
	n = strings.TrimSpace(n)
	if autoName.MatchString(n) {
		return ""
	}
	return n
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}
