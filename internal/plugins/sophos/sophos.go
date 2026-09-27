// Package sophos imports the DHCP reservations of Sophos Firewall (SFOS) through its XML
// API. SFOS does not offer the current DHCP leases or the ARP table through that API; they
// only appear as events in its syslog.
package sophos

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugins/netsrc"
)

func init() { plugin.Register(&Plugin{}) }

const timeout = 30 * time.Second

// Plugin is the Sophos Firewall importer.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "sophos",
		Kind: plugin.KindImporter,
		Name: "Sophos Firewall",
		Description: "Liest die DHCP-Reservierungen (Name, MAC, IP, Schnittstelle) von Sophos Firewalls über die XML-API. " +
			"Aktuelle Leases und die ARP-Tabelle stellt SFOS über die API nicht bereit.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultSchedule:    "0 * * * *",
		DefaultTimeout:     time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     1,
		Targets:            plugin.TargetNone,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		netsrc.SourcesField("Firewalls", "", "Hostname, IP oder URL der Verwaltung (Standard-Port 4444), eine pro Zeile."),
		netsrc.CredentialsField([]string{plugin.CredPassword},
			"Benutzer/Passwort eines Administrators. Die API muss aktiv und die Adresse von NetScope zugelassen sein "+
				"(v20/21: Sicherung & Firmware → API; v22: Verwaltung → API-Zugriff)."),
		netsrc.CreateField(),
		netsrc.VerifyTLSField(),
	}}
}

func parseSource(s string) error {
	_, err := netsrc.BaseURL(s, "https", 4444)
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
	return netsrc.Run(ctx, rc, netsrc.Options{PluginID: "sophos", Sources: s.StringList(netsrc.KeySources), Label: "Firewall",
		Create: s.Bool(netsrc.KeyCreate)}, func(ctx context.Context, src string) (*netsrc.Result, error) {
		return fetch(ctx, rc, src)
	})
}

type response struct {
	Login struct {
		Status string `xml:"status"`
	} `xml:"Login"`
	Status struct {
		Code string `xml:"code,attr"`
		Text string `xml:",chardata"`
	} `xml:"Status"`
	Servers []struct {
		Name      string `xml:"Name"`
		Interface string `xml:"Interface"`
		Status    string `xml:"Status"`
		Leases    []struct {
			HostName string `xml:"HostName"`
			MAC      string `xml:"MACAddress"`
			IP       string `xml:"IPAddress"`
		} `xml:"StaticLease>Lease"`
	} `xml:"DHCPServer"`
}

// statusText explains the documented response codes.
var statusText = map[string]string{
	"532": "die API ist nicht aktiviert",
	"534": "die Adresse von NetScope ist für die API nicht zugelassen",
	"535": "Anmeldung abgelehnt",
}

func fetch(ctx context.Context, rc *plugin.RunContext, src string) (*netsrc.Result, error) {
	base, err := netsrc.BaseURL(src, "https", 4444)
	if err != nil {
		return nil, err
	}
	creds, err := netsrc.Credentials(ctx, rc, []string{plugin.CredPassword}, src)
	if err != nil {
		return nil, err
	}
	hc := netsrc.NewHTTPClient(rc.Settings.Bool(netsrc.KeyVerifyTLS), timeout)
	var res *netsrc.Result
	err = netsrc.TryCredentials(rc, src, creds, func(c *plugin.Credential) error {
		r, err := query(ctx, hc, base.String(), c.Get("username"), c.Get("password"))
		if err != nil {
			return err
		}
		res = r
		return nil
	})
	return res, err
}

func query(ctx context.Context, hc *http.Client, base, user, pass string) (*netsrc.Result, error) {
	var req strings.Builder
	req.WriteString("<Request><Login><Username>")
	_ = xml.EscapeText(&req, []byte(user))
	req.WriteString("</Username><Password>")
	_ = xml.EscapeText(&req, []byte(pass))
	req.WriteString("</Password></Login><Get><DHCPServer/></Get></Request>")
	form := url.Values{"reqxml": {req.String()}}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/webconsole/APIController", strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := hc.Do(r)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("XML-API: HTTP %d", resp.StatusCode)
	}
	var out response
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&out); err != nil {
		return nil, fmt.Errorf("XML-API: Antwort nicht lesbar: %w", err)
	}
	if code := out.Status.Code; code != "" && code != "200" && code != "216" {
		msg := statusText[code]
		if msg == "" {
			msg = strings.TrimSpace(out.Status.Text)
		}
		if code == "535" {
			return nil, fmt.Errorf("%w (Sophos %s)", netsrc.ErrAuth, code)
		}
		return nil, fmt.Errorf("Sophos-API: %s (Code %s)", msg, code)
	}
	if st := strings.ToLower(out.Login.Status); st != "" && !strings.Contains(st, "successful") {
		return nil, fmt.Errorf("%w: %s", netsrc.ErrAuth, out.Login.Status)
	}
	if out.Login.Status == "" && len(out.Servers) == 0 {
		return nil, errors.New("XML-API: unerwartete Antwort (keine Anmeldung, keine DHCP-Server)")
	}
	res := &netsrc.Result{}
	for _, s := range out.Servers {
		for _, l := range s.Leases {
			res.Clients = append(res.Clients, netsrc.Client{MAC: l.MAC, IP: l.IP, Name: l.HostName, Hostname: l.HostName,
				Kind: netsrc.KindStatic, Static: true, Interface: s.Interface, Extra: map[string]string{"dhcp_server": s.Name}})
		}
	}
	return res, nil
}
