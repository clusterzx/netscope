// Package proxmox imports hypervisor nodes, virtual machines and containers from the
// Proxmox VE API. Guests are linked to scanned devices by the MACs of their network
// configuration and get a runs_on relation to their node. Optionally the Docker
// containers inside running LXC containers are read over SSH on the node (pct exec).
package proxmox

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/sshx"
)

func init() { plugin.Register(&Plugin{}) }

// External reference sources.
const (
	refNode  = "proxmox-node" // ID: node name
	refGuest = "proxmox"      // ID: <node>/<qemu|lxc>/<vmid>
)

// requestTimeout bounds a single API request.
const requestTimeout = 60 * time.Second

// Plugin is the Proxmox VE importer.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:          "proxmox",
		Kind:        plugin.KindImporter,
		Name:        "Proxmox VE",
		Description: "Importiert Nodes, VMs und LXC-Container aus der Proxmox-VE-API (Name, VMID, Status, MACs, Ressourcen) und verknüpft sie per MAC mit gescannten Geräten inklusive „läuft auf Node X“. Optional auch die Docker-Container in LXCs.",
		Version:     "1.1.0",

		DefaultEnabled:     false,
		DefaultSchedule:    "*/15 * * * *",
		DefaultTimeout:     10 * time.Minute,
		DefaultConcurrency: 4,
		DefaultRetries:     1,
		Targets:            plugin.TargetNone,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "urls", Type: plugin.FieldStringList, Label: "API-URLs", Required: true, Placeholder: "https://pve.lan:8006",
			Description: "Eine Adresse pro Zeile, mit Port. In einem Cluster genügt ein Node, alle anderen werden über ihn abgefragt; weitere Nodes desselben Clusters dienen als Ausweichadresse und werden nicht doppelt importiert. Mehrere eigenständige Hosts oder Cluster einfach untereinander eintragen.",
			Validation:  &plugin.Validation{Format: "url"}},
		{Key: "credentials", Type: plugin.FieldCredentialRef, Label: "API-Tokens", Multi: true,
			CredentialTypes: []string{plugin.CredAPIToken},
			Description: "Credentials vom Typ API-Token (Token-ID user@realm!tokenname und Secret). Leer = automatisch das passende Token je Host nach dem Geltungsbereich des Credentials (z. B. dem Gerät des Proxmox-Hosts zugewiesen). " +
				"Wird ein Token abgelehnt, wird das nächste probiert. Lesende Rechte genügen, z. B. die Rolle PVEAuditor auf /."},
		{Key: "verify_tls", Type: plugin.FieldBool, Label: "TLS-Zertifikat prüfen", Default: false,
			Description: "Ausgeschaltet lassen, solange Proxmox das selbstsignierte Standardzertifikat verwendet."},
		{Key: "include_stopped", Type: plugin.FieldBool, Label: "Gestoppte Gäste importieren", Default: true},
		{Key: "include_templates", Type: plugin.FieldBool, Label: "Vorlagen importieren", Default: false,
			Description: "VM- und Container-Vorlagen (Templates) als eigene Geräte führen."},
		{Key: "guest_agent_ips", Type: plugin.FieldBool, Label: "IP-Adressen aus dem Gast lesen", Default: true,
			Description: "Bei laufenden VMs den QEMU-Gast-Agent (falls aktiviert) und bei Containern die Interfaces abfragen."},
		{Key: "create_missing", Type: plugin.FieldBool, Label: "Fehlende Geräte anlegen", Default: true,
			Description: "Nodes, VMs und Container anlegen, die noch kein Scan gefunden hat (z. B. gestoppte VMs)."},
		{Key: "lxc_docker", Type: plugin.FieldBool, Label: "Docker-Container in LXCs erfassen", Default: false, Group: lxcGroup,
			Description: "Die Proxmox-API kann nicht in Container hineinschauen. NetScope meldet sich dafür per SSH am Node an und liest mit pct exec die Docker-Container laufender LXCs – nur lesend. " +
				"Empfohlen: das Skript netscope-docker-inventory als Forced Command für den Schlüssel einrichten, dann kann er nichts anderes ausführen (siehe Doku)."},
		{Key: "lxc_docker_credentials", Type: plugin.FieldCredentialRef, Label: "SSH-Zugangsdaten für die Nodes", Multi: true, Group: lxcGroup,
			CredentialTypes: []string{plugin.CredSSH, plugin.CredPassword}, VisibleIf: lxcVisible,
			Description: "Leer = automatisch die SSH-Zugangsdaten, deren Geltungsbereich die Node-IP abdeckt. Der Benutzer muss pct ausführen dürfen (root)."},
		{Key: "lxc_docker_port", Type: plugin.FieldInt, Label: "SSH-Port", Default: 22, Group: lxcGroup, VisibleIf: lxcVisible, Advanced: true,
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(65535)}},
		{Key: "lxc_docker_host_key_policy", Type: plugin.FieldEnum, Label: "SSH-Hostschlüssel", Default: "tofu", Group: lxcGroup, VisibleIf: lxcVisible,
			Options: []plugin.Option{
				{Value: "tofu", Label: "Beim ersten Kontakt merken, Änderungen ablehnen"},
				{Value: "insecure", Label: "Nicht prüfen (unsicher)"},
			}},
		{Key: "lxc_docker_timeout", Type: plugin.FieldDuration, Label: "Zeitlimit pro Node", Default: "3m", Group: lxcGroup, VisibleIf: lxcVisible, Advanced: true,
			Description: "Für alle Container eines Nodes zusammen; jedes einzelne docker-Kommando ist auf dem Node auf 20 s begrenzt.",
			Validation:  &plugin.Validation{Min: plugin.Int64(10), Max: plugin.Int64(1800)}},
	}}
}

const lxcGroup = "Docker in LXC-Containern"

var lxcVisible = &plugin.Condition{Field: "lxc_docker", Equals: []any{true}}

// MigrateSettings implements plugin.SettingsMigrator: version 1.0 had a single "url"
// and "credential".
func (p *Plugin) MigrateSettings(stored map[string]any) map[string]any {
	if u, ok := stored["url"].(string); ok {
		if _, has := stored["urls"]; !has && strings.TrimSpace(u) != "" {
			stored["urls"] = []any{u}
		}
		delete(stored, "url")
	}
	if c, ok := stored["credential"]; ok {
		if _, has := stored["credentials"]; !has {
			if id := plugin.NewSettings(map[string]any{"c": c}).CredentialID("c"); id > 0 {
				stored["credentials"] = []any{id}
			}
		}
		delete(stored, "credential")
	}
	return stored
}

// Endpoints implements plugin.EndpointProvider.
func (p *Plugin) Endpoints(s plugin.Settings) []string {
	var out []string
	for _, raw := range s.StringList("urls") {
		if u, err := url.Parse(strings.TrimSpace(raw)); err == nil && u.Hostname() != "" {
			out = append(out, u.Hostname())
		}
	}
	return out
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	s := rc.Settings
	urls := s.StringList("urls")
	if len(urls) == 0 {
		return errors.New("keine Proxmox-API-URL konfiguriert")
	}
	st := &runState{
		tokens:   &plugin.CredentialPicker{Creds: rc.Creds, Types: []string{plugin.CredAPIToken}, Allowed: s.CredentialIDs("credentials"), Log: rc.Log},
		imported: map[string]string{},
	}
	if s.Bool("lxc_docker") {
		st.lxc = &lxcDockerConfig{
			picker: &plugin.CredentialPicker{Creds: rc.Creds, Types: []string{plugin.CredSSH, plugin.CredPassword},
				Allowed: s.CredentialIDs("lxc_docker_credentials"), Log: rc.Log,
				Check: func(c *plugin.Credential) error { _, _, err := sshx.AuthMethods(c); return err }},
			port:    s.Int("lxc_docker_port"),
			timeout: s.Duration("lxc_docker_timeout"),
		}
		if st.lxc.timeout <= 0 {
			st.lxc.timeout = 3 * time.Minute
		}
		if s.String("lxc_docker_host_key_policy") != "insecure" {
			st.lxc.knownHosts = filepath.Join(rc.DataDir, "known_hosts")
		}
	}
	for _, k := range []string{"nodes", "vms", "containers"} {
		rc.SetStat(k, 0)
	}
	var errs []error
	for _, u := range urls {
		err := p.importURL(ctx, rc, st, u)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(urls) == 1 {
			return err
		}
		rc.AddStat("failed_urls", 1)
		rc.Log.Warn("Proxmox-Endpunkt fehlgeschlagen", "url", u, "error", err)
		errs = append(errs, fmt.Errorf("%s: %w", u, err))
	}
	if len(errs) == len(urls) {
		return fmt.Errorf("kein Proxmox-Endpunkt importiert: %w", errors.Join(errs...))
	}
	return nil
}

// runState is shared by the endpoints of one run.
type runState struct {
	tokens *plugin.CredentialPicker
	lxc    *lxcDockerConfig // nil = Docker in LXCs disabled

	mu       sync.Mutex
	imported map[string]string // cluster (or standalone node) -> URL that imported it

	done, total atomic.Int64
}

// claim records that a cluster is imported through url; false (with the other URL) if
// another endpoint of the run already imported it.
func (st *runState) claim(key, url string) (string, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	if prev, ok := st.imported[key]; ok {
		return prev, false
	}
	st.imported[key] = url
	return "", true
}

// importURL imports one API endpoint, trying the applicable tokens in order.
func (p *Plugin) importURL(ctx context.Context, rc *plugin.RunContext, st *runState, rawURL string) error {
	s := rc.Settings
	if _, err := baseURL(rawURL); err != nil {
		return err
	}
	tokens, err := st.tokens.For(ctx, plugin.CredentialTarget{IP: hostIP(ctx, rawURL)})
	if err != nil {
		return err
	}
	if len(tokens) == 0 {
		return fmt.Errorf("kein API-Token passt zu %s (Auswahl oder Geltungsbereich der Credentials prüfen): %w", rawURL, plugin.ErrNoCredential)
	}
	var lastErr error
	for i, cred := range tokens {
		c, err := newClient(rawURL, cred.Get("token_id"), cred.Get("token"), s.Bool("verify_tls"), requestTimeout)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", cred.Name, err)
			continue
		}
		im := &importer{
			rc:            rc,
			c:             c,
			url:           rawURL,
			st:            st,
			lxc:           st.lxc,
			createMissing: s.Bool("create_missing"),
			stopped:       s.Bool("include_stopped"),
			templates:     s.Bool("include_templates"),
			guestIPs:      s.Bool("guest_agent_ips"),
		}
		err = im.run(ctx)
		c.close()
		if errors.Is(err, errTokenRejected) && i < len(tokens)-1 {
			rc.Log.Info("API-Token abgelehnt, nächstes wird probiert", "url", rawURL, "credential", cred.Name, "error", err)
			lastErr = err
			continue
		}
		return err
	}
	return lastErr
}

type importer struct {
	rc            *plugin.RunContext
	c             *client
	url           string
	st            *runState
	lxc           *lxcDockerConfig
	createMissing bool
	stopped       bool
	templates     bool
	guestIPs      bool

	docker  map[string]*lxcDocker // guest ref -> Docker inside the LXC
	objects int64
	failed  atomic.Int64
}

// errTokenRejected matches answers another token might pass (401, missing permissions).
var errTokenRejected = errors.New("Token abgelehnt")

// rejectedError is a token problem with a user-facing message.
type rejectedError struct{ msg string }

func (e *rejectedError) Error() string        { return e.msg }
func (e *rejectedError) Is(target error) bool { return target == errTokenRejected }

// wrapFatal turns errors of the initial requests into user-facing messages.
func wrapFatal(what string, err error) error {
	var ae *apiError
	if errors.As(err, &ae) {
		switch ae.Status {
		case 401:
			return &rejectedError{fmt.Sprintf("Anmeldung an der Proxmox-API fehlgeschlagen (%s) – Token-ID und Secret prüfen", ae.Msg)}
		case 403:
			return &rejectedError{fmt.Sprintf("%s: keine Berechtigung (%s) – das Token braucht lesende Rechte, z. B. die Rolle PVEAuditor auf /", what, ae.Msg)}
		}
		return fmt.Errorf("%s: %w", what, err)
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	return fmt.Errorf("Proxmox-API nicht erreichbar (%s): %w", what, err)
}

func (im *importer) run(ctx context.Context) error {
	rc, c := im.rc, im.c

	var status []clusterEntry
	if err := c.get(ctx, "/cluster/status", &status); err != nil {
		var ae *apiError
		if !errors.As(err, &ae) || ae.Status == 401 {
			return wrapFatal("Cluster-Status", err)
		}
		rc.Log.Warn("Cluster-Status nicht lesbar, Node-IPs sind unbekannt", "error", err)
	}
	var nodes []nodeEntry
	if err := c.get(ctx, "/nodes", &nodes); err != nil {
		return wrapFatal("Node-Liste", err)
	}
	if len(nodes) == 0 {
		// the API filters by permission instead of failing
		return &rejectedError{"die Proxmox-API liefert keine Nodes – dem Token fehlen Leserechte (z. B. Rolle PVEAuditor auf /; bei Privilege Separation muss das Token selbst berechtigt sein)"}
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Node < nodes[j].Node })
	cluster, nodeIPs := clusterInfo(status)
	key := "cluster:" + cluster
	if cluster == "" {
		key = "node:" + nodes[0].Node
	}
	if prev, ok := im.st.claim(key, im.url); !ok {
		rc.Log.Info("Cluster bereits über einen anderen Endpunkt importiert", "url", im.url, "importiert_über", prev, "cluster", cluster)
		return nil
	}
	var resources []resource
	if err := c.get(ctx, "/cluster/resources?type=vm", &resources); err != nil {
		return wrapFatal("Gäste-Liste", err)
	}

	guests := im.selectGuests(resources)
	im.objects = int64(len(nodes) + len(guests))
	rc.Progress(int(im.st.done.Load()), int(im.st.total.Add(im.objects)))

	if len(nodes) == 1 && nodeIPs[nodes[0].Node] == "" {
		if ip := hostIP(ctx, im.url); ip != "" {
			nodeIPs[nodes[0].Node] = ip
		}
	}
	if im.lxc != nil {
		im.docker = im.collectLXCDocker(ctx, nodes, nodeIPs, guests)
	}
	for _, n := range nodes {
		if err := ctx.Err(); err != nil {
			return err
		}
		im.observeNode(ctx, n, cluster, nodeIPs[n.Node])
	}
	err := plugin.ForEach(ctx, rc.Parallelism(), guests, func(ctx context.Context, r resource) error {
		im.observeGuest(ctx, r)
		return nil
	})
	if err != nil {
		return err
	}
	if n := im.failed.Load(); n > 0 && n == im.objects {
		return fmt.Errorf("keines der %d Proxmox-Objekte konnte gespeichert werden", n)
	}
	rc.Log.Info("Proxmox-Import abgeschlossen", "url", im.url, "cluster", cluster, "nodes", len(nodes), "gaeste", len(guests), "fehler", im.failed.Load())
	return nil
}

// selectGuests filters and orders the guest list according to the settings.
func (im *importer) selectGuests(resources []resource) []resource {
	var out []resource
	for _, r := range resources {
		if r.Type != "qemu" && r.Type != "lxc" {
			continue
		}
		if r.Template.int() == 1 && !im.templates {
			im.rc.AddStat("skipped_templates", 1)
			continue
		}
		if r.Status != "running" && !im.stopped {
			im.rc.AddStat("skipped_stopped", 1)
			continue
		}
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Node != out[j].Node {
			return out[i].Node < out[j].Node
		}
		return out[i].VMID < out[j].VMID
	})
	return out
}

// clusterInfo extracts the cluster name and node addresses from /cluster/status.
func clusterInfo(status []clusterEntry) (string, map[string]string) {
	cluster := ""
	ips := map[string]string{}
	for _, e := range status {
		switch e.Type {
		case "cluster":
			cluster = e.Name
		case "node":
			if a, err := netip.ParseAddr(strings.TrimSpace(e.IP)); err == nil {
				ips[e.Name] = a.Unmap().String()
			}
		}
	}
	return cluster, ips
}

// hostIP returns the IPv4 address of an API URL's host (used to select the token and for
// a single node without cluster/status information).
func hostIP(ctx context.Context, rawURL string) string {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil {
		return ""
	}
	host := u.Hostname()
	if a, err := netip.ParseAddr(host); err == nil {
		return a.Unmap().String()
	}
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(lctx, "ip4", host)
	if err != nil || len(addrs) == 0 {
		return ""
	}
	return addrs[0].Unmap().String()
}

func (im *importer) progress() {
	im.rc.Progress(int(im.st.done.Add(1)), int(im.st.total.Load()))
}

// nodeInventory is the structured inventory of a hypervisor node.
type nodeInventory struct {
	Node       string    `json:"node"`
	Cluster    string    `json:"cluster,omitempty"`
	Status     string    `json:"status"`
	IP         string    `json:"ip,omitempty"`
	PVEVersion string    `json:"pveversion,omitempty"`
	Kernel     string    `json:"kernel,omitempty"`
	CPUModel   string    `json:"cpuModel,omitempty"`
	Sockets    int64     `json:"sockets,omitempty"`
	Cores      int64     `json:"cores,omitempty"`
	CPUs       int64     `json:"cpus"`
	CPU        float64   `json:"cpu"`
	Mem        int64     `json:"mem"`
	MaxMem     int64     `json:"maxmem"`
	Swap       int64     `json:"swap,omitempty"`
	MaxSwap    int64     `json:"maxswap,omitempty"`
	Disk       int64     `json:"disk"`
	MaxDisk    int64     `json:"maxdisk"`
	Uptime     int64     `json:"uptime"`
	LoadAvg    []float64 `json:"loadavg,omitempty"`
	BootMode   string    `json:"bootMode,omitempty"`
}

// nodeObservation builds the observation of a node; st may be nil (offline node or
// missing permission).
func nodeObservation(n nodeEntry, st *nodeStatus, cluster, ip string, create bool) *plugin.Observation {
	inv := nodeInventory{
		Node: n.Node, Cluster: cluster, Status: n.Status, IP: ip,
		CPUs: n.MaxCPU.int(), CPU: float64(n.CPU), Mem: n.Mem.int(), MaxMem: n.MaxMem.int(),
		Disk: n.Disk.int(), MaxDisk: n.MaxDisk.int(), Uptime: n.Uptime.int(),
	}
	obs := &plugin.Observation{
		IP:         ip,
		Target:     "Node " + n.Node,
		Ref:        &plugin.ExternalRef{Source: refNode, ID: n.Node, Data: map[string]any{"node": n.Node, "cluster": cluster}},
		Hostname:   n.Node,
		DeviceType: "hypervisor",
		Create:     create && ip != "",
		Attrs:      map[string]string{"proxmox.node": n.Node},
	}
	if cluster != "" {
		obs.Attrs["proxmox.cluster"] = cluster
	}
	if st != nil {
		inv.PVEVersion = pveVersion(st.PVEVersion)
		inv.Kernel = st.CurrentKernel.Release
		if inv.Kernel == "" {
			inv.Kernel = st.KVersion
		}
		inv.CPUModel = st.CPUInfo.Model
		inv.Sockets, inv.Cores = st.CPUInfo.Sockets.int(), st.CPUInfo.Cores.int()
		if cpus := st.CPUInfo.CPUs.int(); cpus > 0 {
			inv.CPUs = cpus
		}
		inv.CPU = float64(st.CPU)
		if st.Memory.Total > 0 {
			inv.Mem, inv.MaxMem = st.Memory.Used.int(), st.Memory.Total.int()
		}
		inv.Swap, inv.MaxSwap = st.Swap.Used.int(), st.Swap.Total.int()
		if st.RootFS.Total > 0 {
			inv.Disk, inv.MaxDisk = st.RootFS.Used.int(), st.RootFS.Total.int()
		}
		if st.Uptime > 0 {
			inv.Uptime = st.Uptime.int()
		}
		for _, l := range st.LoadAvg {
			inv.LoadAvg = append(inv.LoadAvg, float64(l))
		}
		inv.BootMode = st.BootInfo.Mode
		if inv.PVEVersion != "" {
			obs.OS = &plugin.OSInfo{Name: "Proxmox VE " + inv.PVEVersion, Family: "Linux", Vendor: "Proxmox",
				Generation: inv.PVEVersion, Type: "hypervisor", Accuracy: 100}
			obs.Attrs["proxmox.version"] = inv.PVEVersion
		}
	}
	obs.Inventory = inv
	return obs
}

func (im *importer) observeNode(ctx context.Context, n nodeEntry, cluster, ip string) {
	rc := im.rc
	defer im.progress()
	var st *nodeStatus
	if n.Status == "online" {
		var s nodeStatus
		if err := im.c.get(ctx, "/nodes/"+url.PathEscape(n.Node)+"/status", &s); err != nil {
			rc.Log.Warn("Node-Status nicht lesbar", "node", n.Node, "error", err)
		} else {
			st = &s
		}
	}
	if ip == "" {
		rc.Log.Warn("Keine IP-Adresse für Node bekannt, Zuordnung nur über bereits verknüpfte Geräte", "node", n.Node)
	}
	id, err := rc.Sink.Observe(ctx, nodeObservation(n, st, cluster, ip, im.createMissing))
	if err != nil {
		im.failed.Add(1)
		rc.Log.Warn("Node konnte nicht gespeichert werden", "node", n.Node, "error", err)
		return
	}
	if id == 0 {
		rc.AddStat("unmatched", 1)
		rc.Log.Info("Node ist nicht im Inventar", "node", n.Node, "ip", ip)
	}
	rc.AddStat("nodes", 1)
}

// guestInventory is the structured inventory of a VM or container.
type guestInventory struct {
	VMID         int64          `json:"vmid"`
	Node         string         `json:"node"`
	Type         string         `json:"type"`
	Name         string         `json:"name"`
	Status       string         `json:"status"`
	CPUs         int64          `json:"cpus"`
	CPU          float64        `json:"cpu"`
	Mem          int64          `json:"mem"`
	MaxMem       int64          `json:"maxmem"`
	Disk         int64          `json:"disk"`
	MaxDisk      int64          `json:"maxdisk"`
	Uptime       int64          `json:"uptime"`
	Tags         []string       `json:"tags,omitempty"`
	Template     bool           `json:"template"`
	HAState      string         `json:"hastate,omitempty"`
	Pool         string         `json:"pool,omitempty"`
	Lock         string         `json:"lock,omitempty"`
	OSType       string         `json:"ostype,omitempty"`
	OnBoot       bool           `json:"onboot"`
	Agent        bool           `json:"agent,omitempty"`
	Unprivileged bool           `json:"unprivileged,omitempty"`
	Description  string         `json:"description,omitempty"`
	Networks     []netIface     `json:"networks,omitempty"`
	GuestIPs     []string       `json:"guestIps,omitempty"`
	Docker       *lxcDockerInfo `json:"docker,omitempty"`
}

// guestRef returns the external reference id of a guest.
func guestRef(r resource) string {
	return fmt.Sprintf("%s/%s/%d", r.Node, r.Type, r.VMID.int())
}

// guestObservation builds the observation of a VM or container. cfg may be nil when the
// configuration could not be read; addrs are the addresses reported from inside the guest;
// docker is the Docker inventory inside an LXC (nil = not read, the stored containers stay).
func guestObservation(r resource, cfg guestConfig, addrs []string, docker *lxcDocker, create bool) *plugin.Observation {
	vmid := r.VMID.int()
	inv := guestInventory{
		VMID: vmid, Node: r.Node, Type: r.Type, Name: r.Name, Status: r.Status,
		CPUs: r.MaxCPU.int(), CPU: float64(r.CPU), Mem: r.Mem.int(), MaxMem: r.MaxMem.int(),
		Disk: r.Disk.int(), MaxDisk: r.MaxDisk.int(), Uptime: r.Uptime.int(),
		Tags: splitTags(r.Tags), Template: r.Template.int() == 1, HAState: r.HAState, Pool: r.Pool, Lock: r.Lock,
	}
	hostname := r.Name
	var macs []string
	if cfg != nil {
		inv.Networks = guestNets(cfg)
		inv.OSType = cfg.str("ostype")
		inv.OnBoot = cfg.str("onboot") == "1"
		inv.Description = strings.TrimSpace(cfg.str("description"))
		if r.Type == "qemu" {
			inv.Agent = agentEnabled(cfg.str("agent"))
		} else {
			inv.Unprivileged = cfg.str("unprivileged") == "1"
			if h := strings.TrimSpace(cfg.str("hostname")); h != "" {
				hostname = h
			}
		}
		seen := map[string]bool{}
		for _, n := range inv.Networks {
			if n.MAC != "" && !seen[n.MAC] {
				seen[n.MAC] = true
				macs = append(macs, n.MAC)
			}
		}
		if len(addrs) == 0 && r.Type == "lxc" {
			addrs = staticLXCAddresses(inv.Networks)
		}
	}
	inv.GuestIPs = addrs
	if docker != nil {
		info := docker.info
		inv.Docker = &info
	}
	devType := "vm"
	if r.Type == "lxc" {
		devType = "container"
	}
	obs := &plugin.Observation{
		MACs:   macs,
		Target: fmt.Sprintf("%s/%d %s", r.Type, vmid, r.Name),
		Ref: &plugin.ExternalRef{Source: refGuest, ID: guestRef(r),
			Data: map[string]any{"node": r.Node, "type": r.Type, "vmid": vmid, "name": r.Name}},
		Hostname:   hostname,
		DeviceType: devType,
		// Without configuration the MACs are unknown: creating a device now would leave a
		// duplicate once the MACs match a scanned device, so only known guests are updated.
		Create: create && cfg != nil,
		Attrs: map[string]string{
			"proxmox.vmid": strconv.FormatInt(vmid, 10),
			"proxmox.node": r.Node,
			"proxmox.type": r.Type,
		},
		Inventory: inv,
		Relations: []plugin.Relation{{
			Kind:  plugin.RelRunsOn,
			Other: plugin.DeviceRef{Ref: &plugin.ExternalRef{Source: refNode, ID: r.Node}},
		}},
	}
	if len(addrs) > 0 {
		obs.IP = addrs[0]
	}
	if len(addrs) > 1 {
		obs.IPs = addrs[1:]
	}
	if docker != nil {
		obs.Containers = docker.inv
	}
	return obs
}

func (im *importer) observeGuest(ctx context.Context, r resource) {
	rc := im.rc
	defer im.progress()
	vmid := r.VMID.int()
	base := fmt.Sprintf("/nodes/%s/%s/%d", url.PathEscape(r.Node), r.Type, vmid)
	var cfg guestConfig
	if err := im.c.get(ctx, base+"/config", &cfg); err != nil {
		if ctx.Err() != nil {
			return
		}
		rc.Log.Warn("Gast-Konfiguration nicht lesbar, Zuordnung ohne MAC", "vmid", vmid, "node", r.Node, "error", err)
		cfg = nil
	}
	var addrs []string
	if im.guestIPs && r.Status == "running" && cfg != nil {
		addrs = im.guestAddrs(ctx, r, base, cfg)
	}
	obs := guestObservation(r, cfg, addrs, im.docker[guestRef(r)], im.createMissing)
	id, err := rc.Sink.Observe(ctx, obs)
	if err != nil {
		im.failed.Add(1)
		rc.Log.Warn("Gast konnte nicht gespeichert werden", "vmid", vmid, "name", r.Name, "error", err)
		return
	}
	if id == 0 {
		rc.AddStat("unmatched", 1)
		rc.Log.Debug("Gast ist nicht im Inventar", "vmid", vmid, "name", r.Name)
	}
	if r.Type == "qemu" {
		rc.AddStat("vms", 1)
	} else {
		rc.AddStat("containers", 1)
	}
}

// guestAddrs asks the running guest for its addresses (QEMU guest agent or LXC
// interfaces). Failures are expected (agent not installed) and only logged at debug level.
func (im *importer) guestAddrs(ctx context.Context, r resource, base string, cfg guestConfig) []string {
	var macs []string
	for _, n := range guestNets(cfg) {
		if n.MAC != "" {
			macs = append(macs, n.MAC)
		}
	}
	switch r.Type {
	case "qemu":
		if !agentEnabled(cfg.str("agent")) {
			return nil
		}
		var res agentInterfaces
		if err := im.c.get(ctx, base+"/agent/network-get-interfaces", &res); err != nil {
			im.rc.Log.Debug("Gast-Agent liefert keine Adressen", "vmid", r.VMID.int(), "error", err)
			return nil
		}
		return guestAddresses(res.Result, macs)
	case "lxc":
		var res []guestInterface
		if err := im.c.get(ctx, base+"/interfaces", &res); err != nil {
			im.rc.Log.Debug("Container-Interfaces nicht lesbar", "vmid", r.VMID.int(), "error", err)
			return nil
		}
		return guestAddresses(res, macs)
	}
	return nil
}
