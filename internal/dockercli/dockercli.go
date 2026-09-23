// Package dockercli parses the output of the docker CLI (`docker ps`, `docker images` with
// `--format "{{json .}}"`). It is shared by the collectors that run docker remotely (SSH
// inventory, containers in Proxmox LXCs).
package dockercli

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/plugin"
)

// dockerTimeLayout is the CreatedAt format of `docker ps` / `docker images`.
const dockerTimeLayout = "2006-01-02 15:04:05 -0700 MST"

// maxPortRange caps the expansion of published port ranges ("8000-9000->8000-9000/tcp").
const maxPortRange = 256

type dockerPSLine struct {
	ID         string `json:"ID"`
	Names      string `json:"Names"`
	Image      string `json:"Image"`
	CreatedAt  string `json:"CreatedAt"`
	State      string `json:"State"`
	Status     string `json:"Status"`
	Ports      string `json:"Ports"`
	Networks   string `json:"Networks"`
	Labels     string `json:"Labels"`
	RunningFor string `json:"RunningFor"`
}

// ParsePS parses `docker ps -a --no-trunc --format '{{json .}}'` (one JSON object
// per line). Invalid lines are skipped and counted.
func ParsePS(s string) ([]plugin.Container, int) {
	var (
		out []plugin.Container
		bad int
	)
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var l dockerPSLine
		if err := json.Unmarshal([]byte(line), &l); err != nil || l.ID == "" {
			bad++
			continue
		}
		name, _, _ := strings.Cut(l.Names, ",")
		c := plugin.Container{
			ID:     l.ID,
			Name:   strings.TrimPrefix(strings.TrimSpace(name), "/"),
			Image:  l.Image,
			State:  strings.ToLower(l.State),
			Status: l.Status,
			Ports:  ParsePorts(l.Ports),
			Labels: ParseLabels(l.Labels),
		}
		if c.State == "" {
			c.State = StateFromStatus(l.Status)
		}
		for _, n := range strings.Split(l.Networks, ",") {
			if n = strings.TrimSpace(n); n != "" {
				c.Networks = append(c.Networks, n)
			}
		}
		if c.Labels != nil {
			c.ComposeProject = c.Labels["com.docker.compose.project"]
			c.ComposeService = c.Labels["com.docker.compose.service"]
		}
		if t, err := time.Parse(dockerTimeLayout, l.CreatedAt); err == nil {
			c.Created = t.UTC()
		}
		out = append(out, c)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, bad
}

// StateFromStatus derives the state for old docker versions without the State field.
func StateFromStatus(status string) string {
	s := strings.ToLower(status)
	switch {
	case strings.HasPrefix(s, "up") && strings.Contains(s, "(paused)"):
		return "paused"
	case strings.HasPrefix(s, "up"):
		return "running"
	case strings.HasPrefix(s, "exited"):
		return "exited"
	case strings.HasPrefix(s, "created"):
		return "created"
	case strings.HasPrefix(s, "restarting"):
		return "restarting"
	case strings.HasPrefix(s, "dead"):
		return "dead"
	case strings.HasPrefix(s, "removal"):
		return "removing"
	}
	return ""
}

// ParseLabels parses "k=v,k2=v2". Values may contain commas; a part without "="
// belongs to the previous value.
func ParseLabels(s string) map[string]string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	out := map[string]string{}
	last := ""
	for _, part := range strings.Split(s, ",") {
		k, v, ok := strings.Cut(part, "=")
		if !ok || strings.ContainsAny(k, " ") || k == "" {
			if last != "" {
				out[last] += "," + part
			}
			continue
		}
		out[k] = v
		last = k
	}
	return out
}

// ParsePorts parses "127.0.0.1:18081->80/tcp, [::]:8080->80/tcp, 443/tcp,
// 0.0.0.0:8000-8001->8000-8001/udp, :::9000->9000/tcp".
func ParsePorts(s string) []plugin.ContainerPort {
	var out []plugin.ContainerPort
	for _, item := range strings.Split(s, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		pub, priv, mapped := strings.Cut(item, "->")
		if !mapped {
			priv, pub = pub, ""
		}
		privPorts, proto, ok := splitPortProto(priv)
		if !ok {
			continue
		}
		if pub == "" {
			for _, p := range privPorts {
				out = append(out, plugin.ContainerPort{PrivatePort: p, Type: proto})
			}
			continue
		}
		i := strings.LastIndex(pub, ":")
		if i < 0 {
			continue
		}
		ip := strings.TrimSuffix(strings.TrimPrefix(pub[:i], "["), "]")
		pubPorts, ok := portRange(pub[i+1:])
		if !ok {
			continue
		}
		for k, p := range privPorts {
			cp := plugin.ContainerPort{IP: ip, PrivatePort: p, Type: proto}
			if len(pubPorts) == len(privPorts) {
				cp.PublicPort = pubPorts[k]
			} else if len(pubPorts) > 0 {
				cp.PublicPort = pubPorts[0]
			}
			out = append(out, cp)
		}
	}
	return out
}

func splitPortProto(s string) ([]int, string, bool) {
	ports, proto, ok := strings.Cut(strings.TrimSpace(s), "/")
	if !ok {
		proto = "tcp"
	}
	list, okRange := portRange(ports)
	return list, proto, okRange
}

func portRange(s string) ([]int, bool) {
	a, b, isRange := strings.Cut(s, "-")
	from, err := strconv.Atoi(a)
	if err != nil || from < 0 || from > 65535 {
		return nil, false
	}
	if !isRange {
		return []int{from}, true
	}
	to, err := strconv.Atoi(b)
	if err != nil || to < from || to > 65535 {
		return nil, false
	}
	if to-from >= maxPortRange {
		to = from + maxPortRange - 1
	}
	out := make([]int, 0, to-from+1)
	for p := from; p <= to; p++ {
		out = append(out, p)
	}
	return out, true
}

type dockerImageLine struct {
	ID         string `json:"ID"`
	Repository string `json:"Repository"`
	Tag        string `json:"Tag"`
	CreatedAt  string `json:"CreatedAt"`
	Size       string `json:"Size"`
}

// ParseImages parses `docker images --no-trunc --format '{{json .}}'`. Lines of the
// same image (several tags) are merged. The second value maps "repo:tag" to image ids.
func ParseImages(s string) ([]plugin.ContainerImage, map[string]string, int) {
	byID := map[string]*plugin.ContainerImage{}
	var order []string
	refs := map[string]string{}
	bad := 0
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var l dockerImageLine
		if err := json.Unmarshal([]byte(line), &l); err != nil || l.ID == "" {
			bad++
			continue
		}
		im, ok := byID[l.ID]
		if !ok {
			im = &plugin.ContainerImage{ID: l.ID}
			if size, err := ParseHumanSize(l.Size); err == nil {
				im.Size = size
			}
			if t, err := time.Parse(dockerTimeLayout, l.CreatedAt); err == nil {
				im.Created = t.UTC()
			}
			byID[l.ID] = im
			order = append(order, l.ID)
		}
		if l.Repository != "" && l.Repository != "<none>" {
			ref := l.Repository
			if l.Tag != "" && l.Tag != "<none>" {
				ref += ":" + l.Tag
				im.Tags = append(im.Tags, ref)
				refs[ref] = l.ID
				if l.Tag == "latest" {
					refs[l.Repository] = l.ID
				}
			}
		}
	}
	out := make([]plugin.ContainerImage, 0, len(order))
	for _, id := range order {
		im := byID[id]
		sort.Strings(im.Tags)
		out = append(out, *im)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, refs, bad
}

// ResolveImageIDs fills Container.ImageID from the image list (docker ps shows the
// reference the container was created from; when the tag moved on, it shows the id).
func ResolveImageIDs(containers []plugin.Container, refs map[string]string, images []plugin.ContainerImage) {
	ids := map[string]bool{}
	for _, im := range images {
		ids[im.ID] = true
	}
	for i := range containers {
		img := containers[i].Image
		switch {
		case ids[img]:
			containers[i].ImageID = img
		case ids["sha256:"+img]:
			containers[i].ImageID = "sha256:" + img
		case refs[img] != "":
			containers[i].ImageID = refs[img]
		}
	}
}

// ParseHumanSize parses docker's decimal sizes ("12.8MB", "4.1kB", "1.2GB", "512B").
func ParseHumanSize(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, " "); i > 0 {
		s = s[:i] // "4.1kB (virtual 8.99MB)"
	}
	units := []struct {
		suffix string
		mult   float64
	}{{"TB", 1e12}, {"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"KB", 1e3}, {"B", 1}}
	for _, u := range units {
		if num, ok := strings.CutSuffix(s, u.suffix); ok {
			f, err := strconv.ParseFloat(num, 64)
			if err != nil || f < 0 {
				return 0, fmt.Errorf("ungültige Größe %q", s)
			}
			return int64(f*u.mult + 0.5), nil
		}
	}
	return 0, fmt.Errorf("ungültige Größe %q", s)
}
