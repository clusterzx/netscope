package proxmox

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"netscope/internal/dockercli"
	"netscope/internal/plugin"
	"netscope/internal/sshx"
)

// inventoryScript is sent to the node when no forced command is installed. With the
// recommended forced command the node ignores it and runs its installed copy.
//
//go:embed netscope-docker-inventory.sh
var inventoryScript string

// lxcHeader is the first line of the script output (format version 1).
const lxcHeader = "#netscope-docker-inventory 1"

// maxLXCOutput caps the script output of one node.
const maxLXCOutput = 32 << 20

// lxcDockerConfig are the settings of the Docker collection in LXC containers.
type lxcDockerConfig struct {
	picker     *plugin.CredentialPicker
	port       int
	knownHosts string // "" disables host key checking
	timeout    time.Duration
}

// lxcDocker is the Docker inventory read from one container.
type lxcDocker struct {
	inv  *plugin.ContainerInventory
	info lxcDockerInfo
}

// lxcDockerInfo summarizes Docker in the guest inventory.
type lxcDockerInfo struct {
	Version    string `json:"version,omitempty"`
	Containers int    `json:"containers"`
	Running    int    `json:"running"`
	Images     int    `json:"images"`
}

// lxcSection is the script output of one container.
type lxcSection struct {
	VMID     int64
	NoDocker bool
	errors   map[string]string // section -> exit code
	data     map[string]*strings.Builder
}

func (s *lxcSection) has(sec string) bool { return s.data[sec] != nil && s.errors[sec] == "" }

func (s *lxcSection) text(sec string) string {
	if b := s.data[sec]; b != nil {
		return b.String()
	}
	return ""
}

// lxcOutput is the parsed output of the inventory script.
type lxcOutput struct {
	Node     string
	Complete bool   // the closing "#end" was seen
	Error    string // script-level error ("pct nicht gefunden")
	LXC      []*lxcSection
}

// parseLXCOutput parses the script output. Lines before the header (login banners) are
// ignored; docker JSON lines never start with "#".
func parseLXCOutput(out string) (*lxcOutput, error) {
	lines := strings.Split(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	start := -1
	for i, l := range lines {
		if strings.TrimSpace(l) == lxcHeader {
			start = i
			break
		}
	}
	if start < 0 {
		return nil, errors.New("unerwartete Ausgabe – ist netscope-docker-inventory auf dem Node eingerichtet und der Forced Command korrekt?")
	}
	res := &lxcOutput{}
	var (
		cur     *lxcSection
		section string
	)
	for _, line := range lines[start+1:] {
		if strings.HasPrefix(line, "#") {
			key, arg, _ := strings.Cut(line[1:], " ")
			arg = strings.TrimSpace(arg)
			section = ""
			switch key {
			case "node":
				res.Node = arg
			case "lxc":
				cur = nil
				if id, err := strconv.ParseInt(arg, 10, 64); err == nil {
					cur = &lxcSection{VMID: id, errors: map[string]string{}, data: map[string]*strings.Builder{}}
					res.LXC = append(res.LXC, cur)
				}
			case "nodocker":
				if cur != nil {
					cur.NoDocker = true
				}
			case "version", "ps", "images":
				if cur != nil {
					section = key
					cur.data[key] = &strings.Builder{}
				}
			case "error":
				if cur == nil {
					res.Error = arg
					break
				}
				sec, code, _ := strings.Cut(arg, " ")
				if code == "" {
					code = "?"
				}
				cur.errors[sec] = code
			case "end":
				res.Complete = true
				cur = nil
			}
			continue
		}
		if cur != nil && section != "" && strings.TrimSpace(line) != "" {
			b := cur.data[section]
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return res, nil
}

// docker converts the output of one container. ok is false (with the reason) when the
// container list is unusable; the stored containers are then left untouched.
func (s *lxcSection) docker() (d *lxcDocker, ok bool, why string) {
	if s.NoDocker {
		return nil, false, "kein Docker"
	}
	if !s.has("ps") {
		return nil, false, "docker ps fehlgeschlagen (Exit-Code " + s.errors["ps"] + ")"
	}
	containers, bad := dockercli.ParsePS(s.text("ps"))
	if bad > 0 {
		return nil, false, fmt.Sprintf("%d unlesbare Zeilen in docker ps", bad)
	}
	if containers == nil {
		containers = []plugin.Container{}
	}
	var images []plugin.ContainerImage
	if s.has("images") {
		imgs, refs, badImg := dockercli.ParseImages(s.text("images"))
		if badImg == 0 {
			images = imgs
			if images == nil {
				images = []plugin.ContainerImage{}
			}
			dockercli.ResolveImageIDs(containers, refs, images)
		}
	}
	info := lxcDockerInfo{Containers: len(containers), Images: len(images)}
	if s.has("version") {
		info.Version = strings.TrimSpace(s.text("version"))
	}
	for _, c := range containers {
		if c.State == "running" || c.State == "restarting" {
			info.Running++
		}
	}
	return &lxcDocker{inv: &plugin.ContainerInventory{Engine: "docker", Containers: containers, Images: images}, info: info}, true, ""
}

// collectLXCDocker reads Docker in the running LXC containers of every node over SSH.
// The result is keyed by guest reference (<node>/lxc/<vmid>). Failures are logged per
// node and never fail the import.
func (im *importer) collectLXCDocker(ctx context.Context, nodes []nodeEntry, nodeIPs map[string]string, guests []resource) map[string]*lxcDocker {
	rc := im.rc
	want := map[string]bool{}
	for _, g := range guests {
		if g.Type == "lxc" && g.Status == "running" {
			want[g.Node] = true
		}
	}
	var list []nodeEntry
	for _, n := range nodes {
		switch {
		case !want[n.Node] || n.Status != "online":
		case nodeIPs[n.Node] == "":
			rc.Log.Warn("Docker in LXCs: IP-Adresse des Nodes unbekannt", "node", n.Node)
		default:
			list = append(list, n)
		}
	}
	out := map[string]*lxcDocker{}
	var mu sync.Mutex
	_ = plugin.ForEach(ctx, rc.Parallelism(), list, func(ctx context.Context, n nodeEntry) error {
		ip := nodeIPs[n.Node]
		res, err := im.lxcDockerNode(ctx, n.Node, ip)
		if err != nil {
			if ctx.Err() == nil {
				rc.AddStat("lxc_docker_failed", 1)
				rc.Log.Warn("Docker in LXCs nicht lesbar", "node", n.Node, "ip", ip, "error", err)
			}
			return nil
		}
		mu.Lock()
		for k, v := range res {
			out[k] = v
		}
		mu.Unlock()
		return nil
	})
	return out
}

// lxcDockerNode runs the inventory script on one node.
func (im *importer) lxcDockerNode(ctx context.Context, node, ip string) (map[string]*lxcDocker, error) {
	rc, cfg := im.rc, im.lxc
	creds, err := cfg.picker.For(ctx, plugin.CredentialTarget{IP: ip})
	if err != nil {
		return nil, err
	}
	if len(creds) == 0 {
		return nil, fmt.Errorf("keine passenden SSH-Zugangsdaten: %w", plugin.ErrNoCredential)
	}
	cl, cred, err := sshx.DialFirst(ctx, ip, creds, sshx.Options{Port: cfg.port, Timeout: 15 * time.Second, KnownHosts: cfg.knownHosts})
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	rctx, cancel := context.WithTimeout(ctx, cfg.timeout)
	defer cancel()
	res, err := cl.Run(rctx, "sh -c "+sshx.ShellQuote(inventoryScript), maxLXCOutput)
	if err != nil {
		return nil, fmt.Errorf("Inventar-Skript: %w", err)
	}
	out, err := parseLXCOutput(string(res.Stdout))
	if err != nil {
		if msg := strings.TrimSpace(string(res.Stderr)); msg != "" {
			err = fmt.Errorf("%w (%s)", err, firstLine(msg))
		}
		return nil, err
	}
	if out.Error != "" {
		return nil, errors.New(out.Error)
	}
	if !out.Complete {
		// truncated or timed out: the last container may be incomplete
		rc.Log.Warn("Docker in LXCs: Ausgabe unvollständig", "node", node, "truncated", res.Truncated, "exit", res.ExitCode)
		if len(out.LXC) > 0 {
			out.LXC = out.LXC[:len(out.LXC)-1]
		}
	}
	result := map[string]*lxcDocker{}
	for _, s := range out.LXC {
		d, ok, why := s.docker()
		if !ok {
			if !s.NoDocker {
				rc.Log.Warn("Docker im Container nicht lesbar", "node", node, "vmid", s.VMID, "reason", why)
			}
			continue
		}
		result[fmt.Sprintf("%s/lxc/%d", node, s.VMID)] = d
		rc.AddStat("lxc_docker_containers", len(d.inv.Containers))
	}
	rc.Log.Info("Docker in LXCs gelesen", "node", node, "ip", ip, "credential", cred.Name, "lxc_mit_docker", len(result), "lxc", len(out.LXC))
	return result, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}
