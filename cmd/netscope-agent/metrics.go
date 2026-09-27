package main

import (
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"netscope/internal/agent/proto"
)

// sampler reads the utilisation of a Linux host from /proc; rates need the previous reading.
type sampler struct {
	root   string // file system root (tests use a fixture directory)
	statfs func(path string) (total, used uint64, ok bool)
	prev   *reading
}

type reading struct {
	at          time.Time
	busy, total uint64
	net         map[string]counters
}

type counters struct{ rx, tx uint64 }

func (s *sampler) read(name string) string {
	b, _ := os.ReadFile(filepath.Join(s.root, name))
	return string(b)
}

// sample takes one reading.
func (s *sampler) sample(now time.Time) (proto.Sample, error) {
	busy, total, ok := parseCPU(s.read("proc/stat"))
	if !ok {
		return proto.Sample{}, errors.New("/proc/stat nicht lesbar")
	}
	mem := parseMeminfo(s.read("proc/meminfo"))
	load := parseLoad(s.read("proc/loadavg"))
	smp := proto.Sample{At: now, Load1: &load, MemTotal: mem["MemTotal"], SwapTot: mem["SwapTotal"]}
	avail, ok := mem["MemAvailable"]
	if !ok {
		avail = mem["MemFree"] + mem["Buffers"] + mem["Cached"]
	}
	if smp.MemTotal >= avail {
		smp.MemUsed = smp.MemTotal - avail
	}
	if smp.SwapTot >= mem["SwapFree"] {
		smp.SwapUsed = smp.SwapTot - mem["SwapFree"]
	}
	net := parseNetDev(s.read("proc/net/dev"))
	if p := s.prev; p != nil {
		if total > p.total && busy >= p.busy {
			cpu := float64(busy-p.busy) / float64(total-p.total) * 100
			smp.CPU = &cpu
		}
		if dt := now.Sub(p.at).Seconds(); dt > 0 {
			for _, name := range slices.Sorted(maps.Keys(net)) {
				c, old := net[name], p.net[name]
				if _, known := p.net[name]; known && c.rx >= old.rx && c.tx >= old.tx {
					smp.Net = append(smp.Net, proto.NetRate{Interface: name, RX: float64(c.rx-old.rx) / dt, TX: float64(c.tx-old.tx) / dt})
				}
			}
		}
	}
	seen := map[string]bool{}
	for _, m := range parseMounts(s.read("proc/self/mounts")) {
		// containers bind-mount single files (/etc/hosts …) from the host's disk
		p := filepath.Join(s.root, m.path)
		if fi, err := os.Stat(p); err != nil || !fi.IsDir() || seen[m.dev] {
			continue
		}
		if total, used, ok := s.statfs(p); ok {
			seen[m.dev] = true
			smp.Disks = append(smp.Disks, proto.Disk{Mount: m.path, Total: total, Used: used})
		}
	}
	s.prev = &reading{at: now, busy: busy, total: total, net: net}
	return smp, nil
}

// parseCPU returns busy and total jiffies of all cores ("cpu" line of /proc/stat).
func parseCPU(stat string) (busy, total uint64, ok bool) {
	for _, line := range strings.Split(stat, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "cpu" {
			continue
		}
		var v [8]uint64 // user nice system idle iowait irq softirq steal (guest is part of user)
		for i := 0; i < len(v) && i+1 < len(f); i++ {
			v[i], _ = strconv.ParseUint(f[i+1], 10, 64)
			total += v[i]
		}
		idle := v[3] + v[4]
		return total - idle, total, total > 0
	}
	return 0, 0, false
}

// parseMeminfo returns the fields of /proc/meminfo in bytes.
func parseMeminfo(s string) map[string]uint64 {
	out := map[string]uint64{}
	for _, line := range strings.Split(s, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		f := strings.Fields(rest)
		if !ok || len(f) == 0 {
			continue
		}
		v, err := strconv.ParseUint(f[0], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 1 && f[1] == "kB" {
			v *= 1024
		}
		out[name] = v
	}
	return out
}

func parseLoad(s string) float64 {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

// parseNetDev returns the byte counters of the relevant interfaces of /proc/net/dev.
func parseNetDev(s string) map[string]counters {
	out := map[string]counters{}
	for _, line := range strings.Split(s, "\n") {
		name, rest, ok := strings.Cut(line, ":")
		name = strings.TrimSpace(name)
		f := strings.Fields(rest)
		if !ok || len(f) < 9 || virtualInterface(name) {
			continue
		}
		rx, err1 := strconv.ParseUint(f[0], 10, 64)
		tx, err2 := strconv.ParseUint(f[8], 10, 64)
		if err1 == nil && err2 == nil {
			out[name] = counters{rx: rx, tx: tx}
		}
	}
	return out
}

// virtualInterface skips loopback and the plumbing of containers, VMs and firewalls.
func virtualInterface(name string) bool {
	if name == "lo" {
		return true
	}
	for _, p := range []string{"veth", "docker", "br-", "virbr", "vnet", "tap", "fwbr", "fwpr", "fwln", "cali", "flannel", "cni", "kube"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// diskTypes are local file systems worth watching (no pseudo, overlay or network mounts).
var diskTypes = map[string]bool{"ext2": true, "ext3": true, "ext4": true, "xfs": true, "btrfs": true, "zfs": true,
	"f2fs": true, "vfat": true, "exfat": true, "ntfs": true, "ntfs3": true, "jfs": true, "reiserfs": true, "bcachefs": true}

type mount struct{ dev, path string }

// parseMounts returns the mounts of local file systems (the caller keeps one per device).
func parseMounts(s string) []mount {
	var out []mount
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || !diskTypes[f[2]] {
			continue
		}
		mp := unescapeMount(f[1])
		skip := false
		for _, p := range []string{"/proc", "/sys", "/dev", "/run", "/snap", "/var/lib/docker", "/var/lib/containers"} {
			if mp == p || strings.HasPrefix(mp, p+"/") {
				skip = true
			}
		}
		if skip {
			continue
		}
		out = append(out, mount{dev: f[0], path: mp})
	}
	return out
}

// unescapeMount decodes the octal escapes of /proc/mounts (\040 = space).
func unescapeMount(s string) string {
	if !strings.Contains(s, `\`) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			if v, err := strconv.ParseUint(s[i+1:i+4], 8, 8); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
