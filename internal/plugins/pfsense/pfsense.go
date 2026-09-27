// Package pfsense imports DHCP leases (ISC dhcpd or Kea), static mappings and the ARP
// table of pfSense firewalls over SSH (pfSense CE has no official REST API). Only fixed
// read commands run: cat of config and lease files, arp -an, and the Kea query
// lease4-get-all on its control socket.
package pfsense

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugins/netsrc"
	"netscope/internal/sshx"
)

func init() { plugin.Register(&Plugin{}) }

const maxOutput = 32 << 20

// keaSockets are the Kea control sockets (the path moved in 2025); the lease files, which
// moved for Kea 2.6.3+, are read by the script.
var keaSockets = []string{"/var/run/kea/kea4-ctrl-socket", "/tmp/kea4-ctrl-socket"}

// script prints each source after a marker line; missing files stay empty.
const script = `echo '@@NS:config@@'; cat /conf/config.xml 2>/dev/null
echo '@@NS:isc@@'; cat /var/dhcpd/var/db/dhcpd.leases 2>/dev/null
echo '@@NS:kea@@'; for f in /var/db/kea/dhcp4.leases /var/lib/kea/dhcp4.leases; do if [ -f "$f" ]; then cat "$f"; break; fi; done
echo '@@NS:arp@@'; /usr/sbin/arp -an 2>/dev/null
echo '@@NS:end@@'`

// Plugin is the pfSense importer.
type Plugin struct {
	now func() time.Time
	// dial is replaced in tests.
	dial func(ctx context.Context, host string, creds []*plugin.Credential, opt sshx.Options) (remote, error)
}

// remote is the part of an SSH connection the plugin uses.
type remote interface {
	Run(ctx context.Context, cmd string, max int) (*sshx.Result, error)
	Query(ctx context.Context, socket string, cmd []byte) ([]byte, error)
	Close() error
}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "pfsense",
		Kind: plugin.KindImporter,
		Name: "pfSense",
		Description: "Liest DHCP-Leases (ISC oder Kea), statische Zuordnungen und die ARP-Tabelle von pfSense-Firewalls per SSH " +
			"(nur feste Lesebefehle).",
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
		netsrc.SourcesField("Firewalls", "", "Hostname oder IP, eine pro Zeile."),
		netsrc.CredentialsField([]string{plugin.CredSSH, plugin.CredPassword},
			"SSH-Zugang (admin oder ein Benutzer mit dem Recht „User - System: Shell account access“; SSH unter System → Advanced aktivieren)."),
		{Key: "port", Type: plugin.FieldInt, Label: "SSH-Port", Default: 22, Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(65535)}},
		{Key: "host_key_policy", Type: plugin.FieldEnum, Label: "SSH-Hostschlüssel", Default: "tofu", Options: []plugin.Option{
			{Value: "tofu", Label: "Beim ersten Kontakt merken, Änderungen ablehnen"},
			{Value: "insecure", Label: "Nicht prüfen (unsicher)"},
		}},
		netsrc.ARPField(),
		netsrc.CreateField(),
	}}
}

func parseSource(s string) error {
	s = strings.TrimSpace(s)
	if s == "" || strings.Contains(s, "/") {
		return fmt.Errorf("%q: Hostname oder IP erwartet", s)
	}
	return nil
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
	return netsrc.Run(ctx, rc, netsrc.Options{PluginID: "pfsense", Sources: s.StringList(netsrc.KeySources), Label: "Firewall",
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

// sshRemote adapts an sshx client.
type sshRemote struct{ c *sshx.Client }

func (r sshRemote) Run(ctx context.Context, cmd string, max int) (*sshx.Result, error) {
	return r.c.Run(ctx, cmd, max)
}
func (r sshRemote) Close() error { return r.c.Close() }

// Query sends one command to a Kea control socket and reads the answer.
func (r sshRemote) Query(ctx context.Context, socket string, cmd []byte) ([]byte, error) {
	conn, err := r.c.DialUnix(socket)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if dl, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(dl)
	} else {
		_ = conn.SetDeadline(time.Now().Add(30 * time.Second))
	}
	if _, err := conn.Write(cmd); err != nil {
		return nil, err
	}
	return io.ReadAll(io.LimitReader(conn, maxOutput))
}

func (p *Plugin) fetch(ctx context.Context, rc *plugin.RunContext, host string) (*netsrc.Result, error) {
	creds, err := netsrc.Credentials(ctx, rc, []string{plugin.CredSSH, plugin.CredPassword}, host)
	if err != nil {
		return nil, err
	}
	opt := sshx.Options{Port: rc.Settings.Int("port")}
	if rc.Settings.String("host_key_policy") != "insecure" {
		opt.KnownHosts = filepath.Join(rc.DataDir, "known_hosts")
	}
	dial := p.dial
	if dial == nil {
		dial = func(ctx context.Context, host string, creds []*plugin.Credential, opt sshx.Options) (remote, error) {
			c, _, err := sshx.DialFirst(ctx, host, creds, opt)
			if err != nil {
				return nil, err
			}
			return sshRemote{c}, nil
		}
	}
	r, err := dial(ctx, host, creds, opt)
	if err != nil {
		if errors.Is(err, sshx.ErrHostKeyChanged) {
			return nil, err
		}
		return nil, fmt.Errorf("SSH-Verbindung zu %s fehlgeschlagen: %w", host, err)
	}
	defer r.Close()
	res, err := r.Run(ctx, script, maxOutput)
	if err != nil {
		return nil, err
	}
	sections := splitSections(res.Stdout)
	if _, ok := sections["end"]; !ok {
		return nil, errors.New("keine Befehlsausgabe – die Anmeldung landet vermutlich im Konsolenmenü von pfSense. " +
			"Einen Benutzer mit Shell-Zugriff verwenden („User - System: Shell account access“)")
	}
	now := p.clock()
	statics, ifaces := parseConfig(sections["config"])
	if len(sections["config"]) == 0 {
		rc.Log.Warn("config.xml nicht lesbar – statische Zuordnungen und Schnittstellennamen fehlen (Benutzer ohne Leserecht?)", "firewall", host)
	}
	var clients []netsrc.Client
	clients = append(clients, statics...)
	clients = append(clients, netsrc.ParseISCLeases(bytes.NewReader(sections["isc"]), now)...)
	kea := keaViaSocket(ctx, r, now)
	if kea == nil {
		kea = netsrc.ParseKeaLeases(bytes.NewReader(sections["kea"]), now)
	}
	clients = append(clients, kea...)
	if rc.Settings.Bool(netsrc.KeyARP) {
		clients = append(clients, netsrc.ParseARPBSD(bytes.NewReader(sections["arp"]), now)...)
	}
	for i := range clients {
		if d, ok := ifaces[clients[i].Interface]; ok {
			clients[i].Interface = d
		}
	}
	return &netsrc.Result{Clients: clients}, nil
}

// splitSections cuts the script output at its markers.
func splitSections(out []byte) map[string][]byte {
	sections := map[string][]byte{}
	cur := ""
	var buf bytes.Buffer
	flush := func() {
		if cur != "" {
			sections[cur] = append([]byte(nil), buf.Bytes()...)
		}
		buf.Reset()
	}
	for _, line := range bytes.SplitAfter(out, []byte("\n")) {
		t := bytes.TrimSpace(line)
		if bytes.HasPrefix(t, []byte("@@NS:")) && bytes.HasSuffix(t, []byte("@@")) {
			flush()
			cur = string(t[5 : len(t)-2])
			continue
		}
		buf.Write(line)
	}
	flush()
	return sections
}

// keaViaSocket asks the running Kea for its leases (nil: no Kea or no access).
func keaViaSocket(ctx context.Context, r remote, now time.Time) []netsrc.Client {
	for _, sock := range keaSockets {
		out, err := r.Query(ctx, sock, []byte(`{"command":"lease4-get-all"}`))
		if err != nil || len(out) == 0 {
			continue
		}
		var res struct {
			Result    int `json:"result"`
			Arguments struct {
				Leases []struct {
					IP       string `json:"ip-address"`
					MAC      string `json:"hw-address"`
					Hostname string `json:"hostname"`
					Valid    int64  `json:"valid-lft"`
					CLTT     int64  `json:"cltt"`
					State    int    `json:"state"`
				} `json:"leases"`
			} `json:"arguments"`
		}
		if json.Unmarshal(out, &res) != nil || (res.Result != 0 && res.Result != 3) { // 3 = empty
			continue
		}
		out2 := []netsrc.Client{}
		for _, l := range res.Arguments.Leases {
			if l.State != 0 || l.MAC == "" {
				continue
			}
			c := netsrc.Client{IP: l.IP, MAC: l.MAC, Hostname: strings.TrimSuffix(l.Hostname, "."), Kind: netsrc.KindDHCP}
			if l.CLTT > 0 && l.Valid > 0 {
				c.Expires = time.Unix(l.CLTT+l.Valid, 0).UTC()
				c.LastSeen = time.Unix(l.CLTT, 0).UTC()
				if c.Expires.Before(now) {
					continue
				}
			}
			out2 = append(out2, c)
		}
		return out2
	}
	return nil
}

// node is a generic XML element.
type node struct {
	XMLName xml.Name
	Nodes   []node `xml:",any"`
	Text    string `xml:",chardata"`
}

func (n *node) child(name string) *node {
	for i := range n.Nodes {
		if n.Nodes[i].XMLName.Local == name {
			return &n.Nodes[i]
		}
	}
	return nil
}

func (n *node) text(name string) string {
	if c := n.child(name); c != nil {
		return strings.TrimSpace(c.Text)
	}
	return ""
}

// parseConfig reads the static mappings of every DHCP interface and the descriptions of
// the interfaces (physical name → description) from config.xml.
func parseConfig(data []byte) ([]netsrc.Client, map[string]string) {
	ifaces := map[string]string{}
	if len(bytes.TrimSpace(data)) == 0 {
		return nil, ifaces
	}
	var root node
	if err := xml.Unmarshal(data, &root); err != nil {
		return nil, ifaces
	}
	descr := map[string]string{} // lan → LAN
	if in := root.child("interfaces"); in != nil {
		for _, i := range in.Nodes {
			name := i.text("descr")
			if name == "" {
				name = strings.ToUpper(i.XMLName.Local)
			}
			descr[i.XMLName.Local] = name
			if phys := i.text("if"); phys != "" {
				ifaces[phys] = name
			}
		}
	}
	var out []netsrc.Client
	if d := root.child("dhcpd"); d != nil {
		for _, i := range d.Nodes {
			for _, m := range i.Nodes {
				if m.XMLName.Local != "staticmap" {
					continue
				}
				c := netsrc.Client{MAC: m.text("mac"), IP: m.text("ipaddr"), Hostname: m.text("hostname"), Name: m.text("descr"),
					Kind: netsrc.KindStatic, Static: true, Interface: descr[i.XMLName.Local]}
				if c.Name == "" {
					c.Name = c.Hostname
				}
				if c.MAC != "" {
					out = append(out, c)
				}
			}
		}
	}
	return out, ifaces
}
