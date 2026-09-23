// Package docker imports containers and images from Docker Engine APIs (local socket,
// TCP or the remote socket through an SSH tunnel) and attaches them to the host device.
package docker

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/sshx"
)

func init() { plugin.Register(&Plugin{}) }

const defaultTimeout = 15 * time.Second

// Plugin is the Docker importer.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:          "docker",
		Kind:        plugin.KindImporter,
		Name:        "Docker",
		Description: "Liest Container, Images, veröffentlichte Ports und Compose-Projekte von Docker-Hosts (lokaler Socket, TCP oder SSH-Tunnel) und ordnet sie dem Host als Kind-Objekte zu.",
		Version:     "1.0.0",

		DefaultEnabled:     false,
		DefaultSchedule:    "*/10 * * * *",
		DefaultTimeout:     5 * time.Minute,
		DefaultConcurrency: 4,
		DefaultRetries:     1,
		Targets:            plugin.TargetNone,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "endpoints", Type: plugin.FieldStringList, Label: "Endpunkte", Required: true,
			Placeholder: "unix:///var/run/docker.sock",
			Description: "Ein Endpunkt pro Zeile: unix:///var/run/docker.sock (in den NetScope-Container gemounteter Socket des eigenen Hosts), tcp://host:2375 (unverschlüsselte Docker-API) oder ssh://[benutzer@]host[:port] (Socket /var/run/docker.sock des Hosts per SSH-Tunnel).",
			Validation:  &plugin.Validation{Pattern: `^(unix|tcp|ssh)://\S+$`}},
		{Key: "ssh_credential", Type: plugin.FieldCredentialRef, Label: "SSH-Credential",
			CredentialTypes: []string{plugin.CredSSH, plugin.CredPassword},
			Description:     "Für ssh://-Endpunkte. Ein Benutzer in der URL hat Vorrang; er braucht Zugriff auf den Docker-Socket (root oder Gruppe docker)."},
		{Key: "host_key_policy", Type: plugin.FieldEnum, Label: "SSH-Hostschlüssel", Default: "tofu", Options: []plugin.Option{
			{Value: "tofu", Label: "Beim ersten Kontakt merken, Änderungen ablehnen"},
			{Value: "insecure", Label: "Nicht prüfen (unsicher)"},
		}},
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Timeout pro Anfrage", Default: "15s",
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(300)}},
	}}
}

// ValidateSettings implements plugin.SettingsValidator.
func (p *Plugin) ValidateSettings(s plugin.Settings) error {
	var errs []plugin.FieldError
	needSSH := false
	for _, raw := range s.StringList("endpoints") {
		ep, err := parseEndpoint(raw)
		if err != nil {
			errs = append(errs, plugin.FieldError{Field: "endpoints", Message: err.Error()})
			continue
		}
		needSSH = needSSH || ep.Scheme == "ssh"
	}
	if needSSH && s.CredentialID("ssh_credential") == 0 {
		errs = append(errs, plugin.FieldError{Field: "ssh_credential", Message: "für ssh://-Endpunkte erforderlich"})
	}
	if len(errs) > 0 {
		return &plugin.ValidationError{Errors: errs}
	}
	return nil
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	s := rc.Settings
	var eps []endpoint
	for _, raw := range s.StringList("endpoints") {
		ep, err := parseEndpoint(raw)
		if err != nil {
			return err
		}
		eps = append(eps, ep)
	}
	if len(eps) == 0 {
		return errors.New("keine Docker-Endpunkte konfiguriert")
	}
	im := &importer{rc: rc, timeout: s.Duration("timeout"), credID: s.CredentialID("ssh_credential")}
	if im.timeout <= 0 {
		im.timeout = defaultTimeout
	}
	if s.String("host_key_policy") != "insecure" {
		im.knownHosts = filepath.Join(rc.DataDir, "known_hosts")
	}
	rc.SetStat("endpoints", len(eps))
	rc.SetStat("containers", 0)
	rc.Progress(0, len(eps))

	var (
		mu   sync.Mutex
		errs []error
		done atomic.Int64
	)
	err := plugin.ForEach(ctx, rc.Parallelism(), eps, func(ctx context.Context, ep endpoint) error {
		defer func() { rc.Progress(int(done.Add(1)), len(eps)) }()
		if err := im.importEndpoint(ctx, ep); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			rc.AddStat("failed", 1)
			rc.Log.Warn("Docker-Endpunkt fehlgeschlagen", "endpoint", ep.label(), "error", err)
			mu.Lock()
			errs = append(errs, fmt.Errorf("%s: %w", ep.label(), err))
			mu.Unlock()
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(errs) == len(eps) {
		return fmt.Errorf("kein Docker-Endpunkt erreichbar: %w", errors.Join(errs...))
	}
	return nil
}

type importer struct {
	rc         *plugin.RunContext
	timeout    time.Duration
	knownHosts string
	credID     int64

	credOnce sync.Once
	cred     *plugin.Credential
	credErr  error

	localOnce sync.Once
	localIP   string
	localErr  error
}

func (im *importer) sshCredential(ctx context.Context) (*plugin.Credential, error) {
	im.credOnce.Do(func() {
		if im.credID == 0 {
			im.credErr = errors.New("für ssh://-Endpunkte ist ein SSH-Credential nötig")
			return
		}
		im.cred, im.credErr = im.rc.Creds.Get(ctx, im.credID)
		if im.credErr != nil {
			im.credErr = fmt.Errorf("SSH-Credential %d: %w", im.credID, im.credErr)
		}
	})
	return im.cred, im.credErr
}

func (im *importer) localAddress() (string, error) {
	im.localOnce.Do(func() { im.localIP, im.localErr = localIPv4() })
	return im.localIP, im.localErr
}

// withUser returns a copy of cred with another user name (from the endpoint URL).
func withUser(cred *plugin.Credential, user string) *plugin.Credential {
	if cred == nil || user == "" {
		return cred
	}
	cp := *cred
	cp.Public = map[string]string{}
	for k, v := range cred.Public {
		cp.Public[k] = v
	}
	cp.Public["username"] = user
	cp.Secret = map[string]string{}
	for k, v := range cred.Secret {
		if k != "username" {
			cp.Secret[k] = v
		}
	}
	return &cp
}

// transport opens the connection path to the engine and determines the host address.
func (im *importer) transport(ctx context.Context, ep endpoint) (dial func(context.Context) (net.Conn, error), closeFn func(), hostIP string, err error) {
	d := net.Dialer{Timeout: im.timeout}
	switch ep.Scheme {
	case "unix":
		ip, err := im.localAddress()
		if err != nil {
			return nil, nil, "", fmt.Errorf("IP-Adresse des lokalen Docker-Hosts unbekannt: %w", err)
		}
		return func(ctx context.Context) (net.Conn, error) { return d.DialContext(ctx, "unix", ep.Path) }, func() {}, ip, nil
	case "tcp":
		ip, err := resolveHost(ctx, ep.Host)
		if err != nil {
			return nil, nil, "", err
		}
		addr := net.JoinHostPort(ep.Host, strconv.Itoa(ep.Port))
		return func(ctx context.Context) (net.Conn, error) { return d.DialContext(ctx, "tcp", addr) }, func() {}, ip, nil
	case "ssh":
		cred, err := im.sshCredential(ctx)
		if err != nil {
			return nil, nil, "", err
		}
		cl, err := sshx.Dial(ctx, ep.Host, withUser(cred, ep.User), sshx.Options{Port: ep.Port, Timeout: im.timeout, KnownHosts: im.knownHosts})
		if err != nil {
			return nil, nil, "", fmt.Errorf("SSH-Verbindung: %w", err)
		}
		ip := ""
		if ta, ok := cl.Raw().RemoteAddr().(*net.TCPAddr); ok {
			if a, ok := netip.AddrFromSlice(ta.IP); ok {
				ip = a.Unmap().String()
			}
		}
		if ip == "" {
			if ip, err = resolveHost(ctx, ep.Host); err != nil {
				cl.Close()
				return nil, nil, "", err
			}
		}
		dial := func(context.Context) (net.Conn, error) {
			conn, err := cl.DialUnix(ep.Path)
			if err != nil {
				return nil, fmt.Errorf("Docker-Socket %s über SSH nicht erreichbar: %w", ep.Path, err)
			}
			return conn, nil
		}
		return dial, func() { cl.Close() }, ip, nil
	}
	return nil, nil, "", fmt.Errorf("nicht unterstütztes Schema %q", ep.Scheme)
}

// engineInventory is the structured inventory of the Docker host.
type engineInventory struct {
	Endpoint        string   `json:"endpoint"`
	EngineVersion   string   `json:"engineVersion"`
	APIVersion      string   `json:"apiVersion"`
	Platform        string   `json:"platform,omitempty"`
	OS              string   `json:"os"`
	OSType          string   `json:"osType"`
	Kernel          string   `json:"kernel"`
	Architecture    string   `json:"architecture"`
	CPUs            int      `json:"cpus"`
	Memory          int64    `json:"memory"`
	StorageDriver   string   `json:"storageDriver,omitempty"`
	RootDir         string   `json:"rootDir,omitempty"`
	Swarm           string   `json:"swarm,omitempty"`
	Containers      int      `json:"containers"`
	Running         int      `json:"containersRunning"`
	Paused          int      `json:"containersPaused"`
	Stopped         int      `json:"containersStopped"`
	Images          int      `json:"images"`
	ComposeProjects []string `json:"composeProjects,omitempty"`
}

// hostObservation builds the observation of the Docker host.
func hostObservation(ep endpoint, hostIP, apiVersion string, ver *versionResp, info *infoResp,
	containers []plugin.Container, images []plugin.ContainerImage, raw []byte) *plugin.Observation {
	engineVersion := info.ServerVersion
	if engineVersion == "" {
		engineVersion = ver.Version
	}
	inv := engineInventory{
		Endpoint: ep.label(), EngineVersion: engineVersion, APIVersion: apiVersion, Platform: ver.Platform.Name,
		OS: info.OperatingSystem, OSType: info.OSType, Kernel: info.KernelVersion, Architecture: info.Architecture,
		CPUs: info.NCPU, Memory: info.MemTotal, StorageDriver: info.Driver, RootDir: info.DockerRootDir,
		Swarm: info.Swarm.LocalNodeState, Containers: len(containers), Images: len(images),
	}
	projects := map[string]bool{}
	for _, c := range containers {
		switch c.State {
		case "running", "restarting":
			inv.Running++
		case "paused":
			inv.Paused++
		default:
			inv.Stopped++
		}
		if c.ComposeProject != "" && !projects[c.ComposeProject] {
			projects[c.ComposeProject] = true
			inv.ComposeProjects = append(inv.ComposeProjects, c.ComposeProject)
		}
	}
	sort.Strings(inv.ComposeProjects)
	if images == nil {
		inv.Images = info.Images
	}
	return &plugin.Observation{
		IP:         hostIP,
		Target:     ep.label(),
		Hostname:   info.Name,
		Containers: &plugin.ContainerInventory{Engine: "docker", Containers: containers, Images: images},
		Inventory:  inv,
		Raw:        string(raw),
	}
}

// importEndpoint reads one engine and writes the host observation.
func (im *importer) importEndpoint(ctx context.Context, ep endpoint) error {
	rc := im.rc
	dial, closeFn, hostIP, err := im.transport(ctx, ep)
	if err != nil {
		return err
	}
	defer closeFn()
	eng := newEngine(dial, im.timeout)
	defer eng.close()

	ver, err := eng.negotiate(ctx)
	if err != nil {
		return fmt.Errorf("Docker-API nicht erreichbar: %w", err)
	}
	var info infoResp
	if _, err := eng.get(ctx, "/info", &info); err != nil {
		return err
	}
	var list []containerResp
	raw, err := eng.get(ctx, "/containers/json?all=1", &list)
	if err != nil {
		return err
	}
	containers := convertContainers(list)
	var images []plugin.ContainerImage
	var imgList []imageResp
	if _, err := eng.get(ctx, "/images/json", &imgList); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rc.Log.Warn("Image-Liste nicht lesbar", "endpoint", ep.label(), "error", err)
	} else {
		images = convertImages(imgList)
	}

	obs := hostObservation(ep, hostIP, eng.version, ver, &info, containers, images, raw)
	id, err := rc.Sink.Observe(ctx, obs)
	if err != nil {
		return fmt.Errorf("Speichern fehlgeschlagen: %w", err)
	}
	if id == 0 {
		rc.AddStat("unknown_hosts", 1)
		rc.Log.Warn("Docker-Host ist nicht im Inventar – die Container werden zugeordnet, sobald ein Scan den Host gefunden hat",
			"endpoint", ep.label(), "ip", hostIP, "hostname", info.Name)
		return nil
	}
	rc.AddStat("containers", len(containers))
	rc.AddStat("images", len(images))
	rc.Log.Info("Docker-Host importiert", "endpoint", ep.label(), "ip", hostIP, "hostname", info.Name,
		"container", len(containers), "images", len(images), "api", eng.version)
	return nil
}
