package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/plugin"
)

// API versions: the client never asks for more than maxAPIVersion (the newest API whose
// responses it was written against) and falls back to fallbackAPIVersion when the
// daemon does not report its version.
const (
	maxAPIVersion      = "1.51"
	fallbackAPIVersion = "1.41"
	maxBody            = 64 << 20
)

// engine is an HTTP client for the Docker Engine API over an arbitrary transport.
type engine struct {
	http    *http.Client
	version string // negotiated API version, e.g. "1.51"
}

// newEngine builds a client whose connections are opened by dial.
func newEngine(dial func(ctx context.Context) (net.Conn, error), timeout time.Duration) *engine {
	tr := &http.Transport{
		DialContext:           func(ctx context.Context, _, _ string) (net.Conn, error) { return dial(ctx) },
		MaxIdleConns:          2,
		IdleConnTimeout:       30 * time.Second,
		ResponseHeaderTimeout: timeout,
		DisableCompression:    true,
	}
	return &engine{http: &http.Client{Transport: tr, Timeout: timeout}}
}

func (e *engine) close() { e.http.CloseIdleConnections() }

// getRaw fetches an API path ("/info"); versioned=false omits the /v1.xx prefix.
func (e *engine) getRaw(ctx context.Context, path string, versioned bool) ([]byte, error) {
	p := path
	if versioned {
		p = "/v" + e.version + path
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker"+p, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	resp, err := e.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		var msg struct {
			Message string `json:"message"`
		}
		_ = json.Unmarshal(body, &msg)
		if msg.Message == "" {
			msg.Message = strings.TrimSpace(string(body))
		}
		return nil, fmt.Errorf("%s: HTTP %d: %s", path, resp.StatusCode, msg.Message)
	}
	return body, nil
}

func (e *engine) get(ctx context.Context, path string, out any) ([]byte, error) {
	body, err := e.getRaw(ctx, path, true)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(body, out); err != nil {
		return nil, fmt.Errorf("%s: ungültige JSON-Antwort: %w", path, err)
	}
	return body, nil
}

// versionResp is GET /version.
type versionResp struct {
	Version       string `json:"Version"`
	APIVersion    string `json:"ApiVersion"`
	MinAPIVersion string `json:"MinAPIVersion"`
	Os            string `json:"Os"`
	Arch          string `json:"Arch"`
	KernelVersion string `json:"KernelVersion"`
	Platform      struct {
		Name string `json:"Name"`
	} `json:"Platform"`
}

// negotiate reads /version and selects the API version to use.
func (e *engine) negotiate(ctx context.Context) (*versionResp, error) {
	body, err := e.getRaw(ctx, "/version", false)
	if err != nil {
		return nil, err
	}
	var v versionResp
	if err := json.Unmarshal(body, &v); err != nil {
		return nil, fmt.Errorf("/version: ungültige JSON-Antwort: %w", err)
	}
	e.version = negotiateVersion(v.APIVersion, v.MinAPIVersion)
	return &v, nil
}

// negotiateVersion picks min(server, maxAPIVersion), but never less than the server's
// minimum.
func negotiateVersion(server, serverMin string) string {
	if !validVersion(server) {
		return fallbackAPIVersion
	}
	v := server
	if compareVersions(v, maxAPIVersion) > 0 {
		v = maxAPIVersion
	}
	if validVersion(serverMin) && compareVersions(v, serverMin) < 0 {
		v = serverMin
	}
	return v
}

func validVersion(v string) bool {
	major, minor, ok := strings.Cut(v, ".")
	if !ok {
		return false
	}
	_, err1 := strconv.Atoi(major)
	_, err2 := strconv.Atoi(minor)
	return err1 == nil && err2 == nil
}

// compareVersions compares "major.minor" API versions.
func compareVersions(a, b string) int {
	am, an, _ := strings.Cut(a, ".")
	bm, bn, _ := strings.Cut(b, ".")
	for _, pair := range [][2]string{{am, bm}, {an, bn}} {
		x, _ := strconv.Atoi(pair[0])
		y, _ := strconv.Atoi(pair[1])
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// infoResp is GET /info.
type infoResp struct {
	ID                string `json:"ID"`
	Name              string `json:"Name"`
	ServerVersion     string `json:"ServerVersion"`
	OperatingSystem   string `json:"OperatingSystem"`
	OSType            string `json:"OSType"`
	OSVersion         string `json:"OSVersion"`
	KernelVersion     string `json:"KernelVersion"`
	Architecture      string `json:"Architecture"`
	Driver            string `json:"Driver"`
	DockerRootDir     string `json:"DockerRootDir"`
	NCPU              int    `json:"NCPU"`
	MemTotal          int64  `json:"MemTotal"`
	Containers        int    `json:"Containers"`
	ContainersRunning int    `json:"ContainersRunning"`
	ContainersPaused  int    `json:"ContainersPaused"`
	ContainersStopped int    `json:"ContainersStopped"`
	Images            int    `json:"Images"`
	Swarm             struct {
		LocalNodeState string `json:"LocalNodeState"`
	} `json:"Swarm"`
}

// containerResp is an item of GET /containers/json.
type containerResp struct {
	ID      string   `json:"Id"`
	Names   []string `json:"Names"`
	Image   string   `json:"Image"`
	ImageID string   `json:"ImageID"`
	Command string   `json:"Command"`
	Created int64    `json:"Created"`
	Ports   []struct {
		IP          string `json:"IP"`
		PrivatePort int    `json:"PrivatePort"`
		PublicPort  int    `json:"PublicPort"`
		Type        string `json:"Type"`
	} `json:"Ports"`
	Labels          map[string]string `json:"Labels"`
	State           string            `json:"State"`
	Status          string            `json:"Status"`
	NetworkSettings struct {
		Networks map[string]json.RawMessage `json:"Networks"`
	} `json:"NetworkSettings"`
}

// imageResp is an item of GET /images/json.
type imageResp struct {
	ID       string   `json:"Id"`
	RepoTags []string `json:"RepoTags"`
	Created  int64    `json:"Created"`
	Size     int64    `json:"Size"`
}

// Compose labels.
const (
	labelComposeProject = "com.docker.compose.project"
	labelComposeService = "com.docker.compose.service"
)

// containerName picks the primary name; legacy links add names like "/web/db".
func containerName(names []string) string {
	best := ""
	for _, n := range names {
		n = strings.TrimPrefix(n, "/")
		if n == "" {
			continue
		}
		if best == "" || (strings.Contains(best, "/") && !strings.Contains(n, "/")) {
			best = n
		}
	}
	return best
}

// convertContainers maps the API list to the SDK model.
func convertContainers(list []containerResp) []plugin.Container {
	out := make([]plugin.Container, 0, len(list))
	for _, c := range list {
		name := containerName(c.Names)
		if name == "" {
			name = c.ID
			if len(name) > 12 {
				name = name[:12]
			}
		}
		pc := plugin.Container{
			ID:             c.ID,
			Name:           name,
			Image:          c.Image,
			ImageID:        c.ImageID,
			State:          c.State,
			Status:         c.Status,
			ComposeProject: c.Labels[labelComposeProject],
			ComposeService: c.Labels[labelComposeService],
			Labels:         c.Labels,
		}
		if c.Created > 0 {
			pc.Created = time.Unix(c.Created, 0).UTC()
		}
		seen := map[plugin.ContainerPort]bool{}
		for _, p := range c.Ports {
			cp := plugin.ContainerPort{IP: p.IP, PrivatePort: p.PrivatePort, PublicPort: p.PublicPort, Type: p.Type}
			if cp.Type == "" {
				cp.Type = "tcp"
			}
			if !seen[cp] {
				seen[cp] = true
				pc.Ports = append(pc.Ports, cp)
			}
		}
		sort.SliceStable(pc.Ports, func(i, j int) bool {
			a, b := pc.Ports[i], pc.Ports[j]
			if a.PrivatePort != b.PrivatePort {
				return a.PrivatePort < b.PrivatePort
			}
			if a.Type != b.Type {
				return a.Type < b.Type
			}
			return a.IP < b.IP
		})
		for n := range c.NetworkSettings.Networks {
			pc.Networks = append(pc.Networks, n)
		}
		sort.Strings(pc.Networks)
		out = append(out, pc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// convertImages maps the API list to the SDK model; untagged images keep an empty tag list.
func convertImages(list []imageResp) []plugin.ContainerImage {
	out := make([]plugin.ContainerImage, 0, len(list))
	for _, im := range list {
		ci := plugin.ContainerImage{ID: im.ID, Size: im.Size}
		for _, t := range im.RepoTags {
			if t != "" && t != "<none>:<none>" {
				ci.Tags = append(ci.Tags, t)
			}
		}
		if im.Created > 0 {
			ci.Created = time.Unix(im.Created, 0).UTC()
		}
		out = append(out, ci)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// endpoint is a configured Docker endpoint.
type endpoint struct {
	Raw    string
	Scheme string // unix | tcp | ssh
	Host   string // tcp, ssh
	Port   int    // tcp, ssh
	User   string // ssh (optional, overrides the credential's user name)
	Path   string // unix socket (local for unix, remote for ssh)
}

const defaultSocket = "/var/run/docker.sock"

// parseEndpoint parses unix:///path, tcp://host[:port] and ssh://[user@]host[:port][/socket].
func parseEndpoint(raw string) (endpoint, error) {
	s := strings.TrimSpace(raw)
	e := endpoint{Raw: s}
	scheme, rest, ok := strings.Cut(s, "://")
	if !ok {
		return e, fmt.Errorf("Endpunkt %q: Schema fehlt (unix://, tcp:// oder ssh://)", raw)
	}
	e.Scheme = strings.ToLower(scheme)
	switch e.Scheme {
	case "unix":
		if rest == "" {
			return e, fmt.Errorf("Endpunkt %q: Socket-Pfad fehlt", raw)
		}
		e.Path = rest
		return e, nil
	case "tcp", "ssh":
		u, err := url.Parse(s)
		if err != nil || u.Hostname() == "" {
			return e, fmt.Errorf("Endpunkt %q: Host fehlt oder ungültig", raw)
		}
		e.Host = u.Hostname()
		e.Port = 2375
		if e.Scheme == "ssh" {
			e.Port = 22
			e.Path = defaultSocket
			if p := strings.TrimRight(u.Path, "/"); p != "" {
				e.Path = p
			}
			if u.User != nil {
				e.User = u.User.Username()
				if _, hasPw := u.User.Password(); hasPw {
					return e, fmt.Errorf("Endpunkt %q: Passwörter gehören ins SSH-Credential, nicht in die URL", u.Redacted())
				}
			}
		} else if u.Path != "" && u.Path != "/" {
			return e, fmt.Errorf("Endpunkt %q: tcp:// erwartet keinen Pfad", raw)
		}
		if ps := u.Port(); ps != "" {
			p, err := strconv.Atoi(ps)
			if err != nil || p < 1 || p > 65535 {
				return e, fmt.Errorf("Endpunkt %q: ungültiger Port", raw)
			}
			e.Port = p
		}
		return e, nil
	}
	return e, fmt.Errorf("Endpunkt %q: nicht unterstütztes Schema %q (erlaubt: unix, tcp, ssh)", raw, scheme)
}

// label is the endpoint as shown in logs.
func (e endpoint) label() string { return e.Raw }
