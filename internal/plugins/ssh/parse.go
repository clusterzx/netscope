package ssh

import (
	"encoding/json"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/netutil"
)

// Every parser takes the raw output of one section and is tolerant against missing
// fields, busybox variants and garbage lines: unknown lines are skipped, never fatal.

// ---------------------------------------------------------------- date

// parseDate parses `date +'%s %z'` into the remote clock and its UTC offset.
func parseDate(s string) (time.Time, *time.Location, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return time.Time{}, nil, fmt.Errorf("leere Ausgabe")
	}
	sec, err := strconv.ParseInt(f[0], 10, 64)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("ungültige Zeit %q", f[0])
	}
	loc := time.UTC
	if len(f) > 1 {
		if l, ok := parseZoneOffset(f[1]); ok {
			loc = l
		}
	}
	return time.Unix(sec, 0).In(loc), loc, nil
}

// parseZoneOffset parses "+0200" / "-0530".
func parseZoneOffset(z string) (*time.Location, bool) {
	if len(z) != 5 || (z[0] != '+' && z[0] != '-') {
		return nil, false
	}
	h, err1 := strconv.Atoi(z[1:3])
	m, err2 := strconv.Atoi(z[3:5])
	if err1 != nil || err2 != nil {
		return nil, false
	}
	off := h*3600 + m*60
	if z[0] == '-' {
		off = -off
	}
	return time.FixedZone(z, off), true
}

// ---------------------------------------------------------------- os-release

// parseOSRelease parses /etc/os-release (shell-style KEY=VALUE, optionally quoted).
func parseOSRelease(s string) map[string]string {
	out := map[string]string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || k == "" || strings.ContainsAny(k, " \t") {
			continue
		}
		out[k] = unquoteShell(strings.TrimSpace(v))
	}
	return out
}

func unquoteShell(v string) string {
	if len(v) >= 2 && v[0] == '\'' && v[len(v)-1] == '\'' {
		return v[1 : len(v)-1]
	}
	if len(v) >= 2 && v[0] == '"' && v[len(v)-1] == '"' {
		v = v[1 : len(v)-1]
		var b strings.Builder
		for i := 0; i < len(v); i++ {
			if v[i] == '\\' && i+1 < len(v) && strings.ContainsRune("\"\\$`", rune(v[i+1])) {
				i++
			}
			b.WriteByte(v[i])
		}
		return b.String()
	}
	return v
}

// ---------------------------------------------------------------- simple values

// firstLine returns the first non-empty trimmed line.
func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// parseUptime parses /proc/uptime ("4303.37 4303.37") into seconds.
func parseUptime(s string) (float64, error) {
	f := strings.Fields(s)
	if len(f) == 0 {
		return 0, fmt.Errorf("leere Ausgabe")
	}
	v, err := strconv.ParseFloat(f[0], 64)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("ungültige Uptime %q", f[0])
	}
	return v, nil
}

// parseEpoch parses a unix timestamp (stat -c %Y).
func parseEpoch(s string) (time.Time, error) {
	v := firstLine(s)
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil || n <= 0 {
		return time.Time{}, fmt.Errorf("ungültiger Zeitstempel %q", v)
	}
	return time.Unix(n, 0).UTC(), nil
}

// ---------------------------------------------------------------- cpu

// CPUInfo summarises /proc/cpuinfo and nproc.
type CPUInfo struct {
	Model    string `json:"model,omitempty"`
	Vendor   string `json:"vendor,omitempty"`
	Hardware string `json:"hardware,omitempty"` // board name on ARM systems
	Sockets  int    `json:"sockets,omitempty"`
	Cores    int    `json:"cores,omitempty"`   // physical cores of the host (if reported)
	Threads  int    `json:"threads,omitempty"` // logical CPUs listed in /proc/cpuinfo
	Usable   int    `json:"usable,omitempty"`  // CPUs available to the system (nproc)
}

// parseCPUInfo parses /proc/cpuinfo (x86, ARM, MIPS, POWER variants).
func parseCPUInfo(s string) CPUInfo {
	var c CPUInfo
	sockets := map[string]int{} // physical id -> cpu cores
	curPhys := ""
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch strings.ToLower(k) {
		case "processor":
			if _, err := strconv.Atoi(v); err == nil {
				c.Threads++
			} else if c.Model == "" && v != "" {
				c.Model = v // older ARM kernels: "Processor : ARMv7 Processor rev 4 (v7l)"
			}
			curPhys = ""
		case "model name", "cpu model":
			if c.Model == "" && v != "" {
				c.Model = v
			}
		case "cpu":
			if c.Model == "" && v != "" { // POWER
				c.Model = v
			}
		case "vendor_id":
			if c.Vendor == "" {
				c.Vendor = v
			}
		case "hardware", "model":
			// x86 "model" is a number, ARM boards report their name
			if _, err := strconv.Atoi(v); err != nil && v != "" && c.Hardware == "" {
				c.Hardware = v
			}
		case "physical id":
			curPhys = v
			if _, ok := sockets[v]; !ok {
				sockets[v] = 0
			}
		case "cpu cores":
			if n, err := strconv.Atoi(v); err == nil && curPhys != "" {
				sockets[curPhys] = n
			}
		}
	}
	c.Sockets = len(sockets)
	for _, n := range sockets {
		c.Cores += n
	}
	return c
}

// ---------------------------------------------------------------- memory

// MemInfo summarises /proc/meminfo.
type MemInfo struct {
	TotalBytes     int64 `json:"totalBytes"`
	AvailableBytes int64 `json:"availableBytes"`
	SwapTotalBytes int64 `json:"swapTotalBytes"`
	SwapFreeBytes  int64 `json:"swapFreeBytes"`
}

// parseMeminfo parses /proc/meminfo.
func parseMeminfo(s string) (MemInfo, error) {
	kv := map[string]int64{}
	for _, line := range strings.Split(s, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		f := strings.Fields(v)
		if len(f) == 0 {
			continue
		}
		n, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			continue
		}
		if len(f) > 1 && strings.EqualFold(f[1], "kB") {
			n *= 1024
		}
		kv[strings.TrimSpace(k)] = n
	}
	total, ok := kv["MemTotal"]
	if !ok {
		return MemInfo{}, fmt.Errorf("MemTotal fehlt")
	}
	m := MemInfo{TotalBytes: total, SwapTotalBytes: kv["SwapTotal"], SwapFreeBytes: kv["SwapFree"]}
	if v, ok := kv["MemAvailable"]; ok {
		m.AvailableBytes = v
	} else {
		m.AvailableBytes = kv["MemFree"] + kv["Buffers"] + kv["Cached"]
	}
	return m, nil
}

// ---------------------------------------------------------------- filesystems

// Filesystem is one mounted filesystem (df -P -T).
type Filesystem struct {
	Device     string `json:"device"`
	Type       string `json:"type"`
	MountPoint string `json:"mountPoint"`
	SizeBytes  int64  `json:"sizeBytes"`
	UsedBytes  int64  `json:"usedBytes"`
	AvailBytes int64  `json:"availBytes"`
	UsePercent int    `json:"usePercent"`
}

// pseudoFS are virtual filesystems that are not reported.
var pseudoFS = map[string]bool{
	"tmpfs": true, "devtmpfs": true, "overlay": true, "squashfs": true, "efivarfs": true, "devpts": true,
	"proc": true, "sysfs": true, "cgroup": true, "cgroup2": true, "debugfs": true, "tracefs": true,
	"securityfs": true, "pstore": true, "bpf": true, "fusectl": true, "configfs": true, "mqueue": true,
	"hugetlbfs": true, "autofs": true, "binfmt_misc": true, "nsfs": true, "ramfs": true, "rpc_pipefs": true,
	"fuse.lxcfs": true, "fuse.snapfuse": true, "fuse.gvfsd-fuse": true, "fuse.portal": true, "shm": true,
}

// parseDF parses `df -P -T` (GNU coreutils and busybox). Pseudo filesystems are skipped
// and repeated bind mounts of the same filesystem are reported once.
func parseDF(s string) []Filesystem {
	var out []Filesystem
	blockSize := int64(1024)
	seen := map[string]bool{}
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		if f[0] == "Filesystem" {
			for _, h := range f {
				if b, ok := strings.CutSuffix(h, "-blocks"); ok {
					if n, err := strconv.ParseInt(b, 10, 64); err == nil && n > 0 {
						blockSize = n
					}
				}
			}
			continue
		}
		if len(f) < 7 {
			continue
		}
		size, err1 := strconv.ParseInt(f[2], 10, 64)
		used, err2 := strconv.ParseInt(f[3], 10, 64)
		avail, err3 := strconv.ParseInt(f[4], 10, 64)
		pct, err4 := strconv.Atoi(strings.TrimSuffix(f[5], "%"))
		if err1 != nil || err2 != nil || err3 != nil || err4 != nil {
			continue
		}
		fs := Filesystem{Device: f[0], Type: f[1], MountPoint: mountPoint(line, f), SizeBytes: size * blockSize,
			UsedBytes: used * blockSize, AvailBytes: avail * blockSize, UsePercent: pct}
		if pseudoFS[fs.Type] || fs.SizeBytes == 0 {
			continue
		}
		key := fmt.Sprintf("%s|%s|%d|%d", fs.Device, fs.Type, size, used)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, fs)
	}
	return out
}

// mountPoint returns the text after the sixth field (mount points may contain spaces).
func mountPoint(line string, f []string) string {
	rest := line
	for i := 0; i < 6; i++ {
		idx := strings.Index(rest, f[i])
		if idx < 0 {
			return strings.Join(f[6:], " ")
		}
		rest = rest[idx+len(f[i]):]
	}
	return strings.TrimSpace(rest)
}

// ---------------------------------------------------------------- block devices

// BlockDevice is a disk, partition or logical volume (lsblk).
type BlockDevice struct {
	Name       string        `json:"name"`
	Type       string        `json:"type"`
	SizeBytes  int64         `json:"sizeBytes"`
	Model      string        `json:"model,omitempty"`
	Serial     string        `json:"serial,omitempty"`
	Rotational *bool         `json:"rotational,omitempty"`
	Transport  string        `json:"transport,omitempty"`
	FSType     string        `json:"fsType,omitempty"`
	MountPoint string        `json:"mountPoint,omitempty"`
	Children   []BlockDevice `json:"children,omitempty"`
}

// parseLsblk parses `lsblk -J -b` (old versions print all values as strings, new ones
// use numbers/booleans; "mountpoints" is a list in newer versions). Loop and RAM devices
// are skipped.
func parseLsblk(s string) ([]BlockDevice, error) {
	var doc struct {
		BlockDevices []map[string]any `json:"blockdevices"`
	}
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	if err := d.Decode(&doc); err != nil {
		return nil, fmt.Errorf("lsblk-JSON: %w", err)
	}
	return lsblkDevices(doc.BlockDevices), nil
}

func lsblkDevices(list []map[string]any) []BlockDevice {
	var out []BlockDevice
	for _, m := range list {
		bd := BlockDevice{
			Name:      jsonString(m["name"]),
			Type:      jsonString(m["type"]),
			SizeBytes: jsonInt(m["size"]),
			Model:     jsonString(m["model"]),
			Serial:    jsonString(m["serial"]),
			Transport: jsonString(m["tran"]),
			FSType:    jsonString(m["fstype"]),
		}
		if bd.Name == "" || bd.Type == "loop" || bd.Type == "ram" {
			continue
		}
		if r, ok := jsonBool(m["rota"]); ok {
			bd.Rotational = &r
		}
		bd.MountPoint = jsonString(m["mountpoint"])
		if bd.MountPoint == "" {
			if mps, ok := m["mountpoints"].([]any); ok {
				for _, mp := range mps {
					if v := jsonString(mp); v != "" {
						bd.MountPoint = v
						break
					}
				}
			}
		}
		if children, ok := m["children"].([]any); ok {
			var cm []map[string]any
			for _, c := range children {
				if mm, ok := c.(map[string]any); ok {
					cm = append(cm, mm)
				}
			}
			bd.Children = lsblkDevices(cm)
		}
		out = append(out, bd)
	}
	return out
}

func jsonString(v any) string {
	switch x := v.(type) {
	case string:
		return strings.TrimSpace(x)
	case json.Number:
		return x.String()
	case bool:
		return strconv.FormatBool(x)
	}
	return ""
}

func jsonInt(v any) int64 {
	switch x := v.(type) {
	case json.Number:
		if n, err := x.Int64(); err == nil {
			return n
		}
		if f, err := x.Float64(); err == nil {
			return int64(f)
		}
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64); err == nil {
			return n
		}
	case float64:
		return int64(x)
	}
	return 0
}

func jsonBool(v any) (bool, bool) {
	switch x := v.(type) {
	case bool:
		return x, true
	case string:
		switch strings.TrimSpace(x) {
		case "1", "true":
			return true, true
		case "0", "false":
			return false, true
		}
	case json.Number:
		return x.String() != "0", true
	}
	return false, false
}

// ---------------------------------------------------------------- network interfaces

// Interface is a network interface with its addresses.
type Interface struct {
	Index     int      `json:"index"`
	Name      string   `json:"name"`
	MAC       string   `json:"mac,omitempty"`
	MTU       int      `json:"mtu,omitempty"`
	State     string   `json:"state,omitempty"` // operstate: UP, DOWN, UNKNOWN
	Type      string   `json:"type,omitempty"`  // link type: ether, loopback, none …
	Master    string   `json:"master,omitempty"`
	Flags     []string `json:"flags,omitempty"`
	Addresses []IfAddr `json:"addresses,omitempty"`
}

// IfAddr is an address of an interface (IPv4 or IPv6).
type IfAddr struct {
	Address   string `json:"address"`
	PrefixLen int    `json:"prefixLen"`
	Family    string `json:"family"` // inet | inet6
	Scope     string `json:"scope,omitempty"`
	Dynamic   bool   `json:"dynamic,omitempty"`
	Temporary bool   `json:"temporary,omitempty"`
}

// parseIPAddr parses `ip -j addr` or, if the output is not JSON, `ip addr` (iproute2
// and busybox).
func parseIPAddr(s string) ([]Interface, error) {
	t := strings.TrimSpace(s)
	if strings.HasPrefix(t, "[") {
		return parseIPAddrJSON(t)
	}
	return parseIPAddrText(s), nil
}

func parseIPAddrJSON(s string) ([]Interface, error) {
	var list []struct {
		Index     int      `json:"ifindex"`
		Name      string   `json:"ifname"`
		Flags     []string `json:"flags"`
		MTU       int      `json:"mtu"`
		Operstate string   `json:"operstate"`
		LinkType  string   `json:"link_type"`
		Address   string   `json:"address"`
		Master    string   `json:"master"`
		AddrInfo  []struct {
			Family    string `json:"family"`
			Local     string `json:"local"`
			PrefixLen int    `json:"prefixlen"`
			Scope     string `json:"scope"`
			Dynamic   bool   `json:"dynamic"`
			Temporary bool   `json:"temporary"`
		} `json:"addr_info"`
	}
	if err := json.Unmarshal([]byte(s), &list); err != nil {
		return nil, fmt.Errorf("ip-JSON: %w", err)
	}
	var out []Interface
	for _, l := range list {
		if l.Name == "" {
			continue // iproute2 4.x emits empty objects
		}
		ifc := Interface{Index: l.Index, Name: l.Name, MTU: l.MTU, State: l.Operstate, Type: l.LinkType, Master: l.Master, Flags: l.Flags}
		if mac, ok := netutil.NormalizeMAC(l.Address); ok && len(l.Address) == 17 {
			ifc.MAC = mac
		}
		for _, a := range l.AddrInfo {
			if a.Local == "" {
				continue
			}
			ifc.Addresses = append(ifc.Addresses, IfAddr{Address: a.Local, PrefixLen: a.PrefixLen, Family: a.Family,
				Scope: a.Scope, Dynamic: a.Dynamic, Temporary: a.Temporary})
		}
		out = append(out, ifc)
	}
	return out, nil
}

var ipHeaderRe = regexp.MustCompile(`^(\d+):\s+([^:@\s]+)(?:@[^:\s]+)?:\s+<([^>]*)>(.*)$`)

func parseIPAddrText(s string) []Interface {
	var out []Interface
	var cur *Interface
	for _, line := range strings.Split(s, "\n") {
		if m := ipHeaderRe.FindStringSubmatch(line); m != nil {
			idx, _ := strconv.Atoi(m[1])
			out = append(out, Interface{Index: idx, Name: m[2]})
			cur = &out[len(out)-1]
			if m[3] != "" {
				cur.Flags = strings.Split(m[3], ",")
			}
			rest := strings.Fields(m[4])
			for i := 0; i+1 < len(rest); i++ {
				switch rest[i] {
				case "mtu":
					cur.MTU, _ = strconv.Atoi(rest[i+1])
				case "state":
					cur.State = rest[i+1]
				case "master":
					cur.Master = rest[i+1]
				}
			}
			continue
		}
		if cur == nil {
			continue
		}
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch {
		case strings.HasPrefix(f[0], "link/"):
			cur.Type = strings.TrimPrefix(f[0], "link/")
			if len(f) > 1 {
				if mac, ok := netutil.NormalizeMAC(f[1]); ok && len(f[1]) == 17 {
					cur.MAC = mac
				}
			}
		case f[0] == "inet" || f[0] == "inet6":
			if len(f) < 2 {
				continue
			}
			p, err := netip.ParsePrefix(f[1])
			if err != nil {
				continue
			}
			a := IfAddr{Address: p.Addr().String(), PrefixLen: p.Bits(), Family: f[0]}
			for i := 2; i < len(f); i++ {
				switch f[i] {
				case "scope":
					if i+1 < len(f) {
						a.Scope = f[i+1]
					}
				case "dynamic":
					a.Dynamic = true
				case "temporary":
					a.Temporary = true
				}
			}
			cur.Addresses = append(cur.Addresses, a)
		}
	}
	return out
}

// ---------------------------------------------------------------- services

// Service is a running systemd service.
type Service struct {
	Unit        string `json:"unit"`
	Description string `json:"description,omitempty"`
}

// parseServices parses `systemctl list-units --type=service --state=running --no-legend --plain`.
func parseServices(s string) []Service {
	var out []Service
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(strings.TrimLeft(line, " ●*"))
		if len(f) < 4 || !strings.Contains(f[0], ".") {
			continue
		}
		svc := Service{Unit: f[0]}
		if len(f) > 4 {
			svc.Description = strings.Join(f[4:], " ")
		}
		out = append(out, svc)
	}
	return out
}

// ---------------------------------------------------------------- sockets

// Socket is a listening TCP socket or a bound UDP socket.
type Socket struct {
	Proto     string          `json:"proto"` // tcp | udp
	Address   string          `json:"address"`
	Port      int             `json:"port"`
	Interface string          `json:"interface,omitempty"`
	Processes []SocketProcess `json:"processes,omitempty"`
}

// SocketProcess is a process owning a socket.
type SocketProcess struct {
	Name string `json:"name"`
	PID  int    `json:"pid,omitempty"`
}

var ssUserRe = regexp.MustCompile(`\("([^"]*)",pid=(\d+)`)

// parseSockets parses `ss -tulpn[H]` or busybox/net-tools `netstat -tulpn`.
func parseSockets(s, variant string) []Socket {
	if variant == "netstat" {
		return parseNetstat(s)
	}
	var out []Socket
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] == "Netid" {
			continue
		}
		proto := f[0]
		if proto != "tcp" && proto != "udp" {
			continue
		}
		addr, port, zone, ok := splitHostPort(f[4])
		if !ok {
			continue
		}
		sock := Socket{Proto: proto, Address: addr, Port: port, Interface: zone}
		if i := strings.Index(line, "users:("); i >= 0 {
			for _, m := range ssUserRe.FindAllStringSubmatch(line[i:], -1) {
				pid, _ := strconv.Atoi(m[2])
				sock.Processes = append(sock.Processes, SocketProcess{Name: m[1], PID: pid})
			}
		}
		out = append(out, sock)
	}
	return sortSockets(out)
}

func parseNetstat(s string) []Socket {
	var out []Socket
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) < 6 {
			continue
		}
		proto := strings.TrimRight(f[0], "46")
		if proto != "tcp" && proto != "udp" {
			continue
		}
		if proto == "tcp" && (len(f) < 7 || f[5] != "LISTEN") {
			continue
		}
		addr, port, zone, ok := splitHostPort(f[3])
		if !ok {
			continue
		}
		sock := Socket{Proto: proto, Address: addr, Port: port, Interface: zone}
		if pp := f[len(f)-1]; strings.Contains(pp, "/") {
			pidS, name, _ := strings.Cut(pp, "/")
			if pid, err := strconv.Atoi(pidS); err == nil {
				sock.Processes = []SocketProcess{{Name: name, PID: pid}}
			}
		}
		out = append(out, sock)
	}
	return sortSockets(out)
}

// splitHostPort splits "127.0.0.53%lo:53", "[::1]:25", "*:22", ":::8080" and "::1:9000".
func splitHostPort(s string) (addr string, port int, zone string, ok bool) {
	i := strings.LastIndex(s, ":")
	if i < 0 {
		return "", 0, "", false
	}
	p, err := strconv.Atoi(s[i+1:])
	if err != nil || p < 0 || p > 65535 {
		return "", 0, "", false
	}
	addr = strings.TrimSuffix(strings.TrimPrefix(s[:i], "["), "]")
	if a, z, found := strings.Cut(addr, "%"); found {
		addr, zone = a, z
	}
	if addr == "" {
		addr = "*"
	}
	return addr, p, zone, true
}

func sortSockets(list []Socket) []Socket {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Proto != list[j].Proto {
			return list[i].Proto < list[j].Proto
		}
		if list[i].Port != list[j].Port {
			return list[i].Port < list[j].Port
		}
		return list[i].Address < list[j].Address
	})
	return list
}

// ---------------------------------------------------------------- last update

// parseAptHistory parses the last "Start-Date: 2026-09-22  19:15:14" line (local time of
// the host).
func parseAptHistory(s string, loc *time.Location) (time.Time, error) {
	var last string
	for _, line := range strings.Split(s, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), "Start-Date:"); ok {
			last = strings.Join(strings.Fields(v), " ")
		}
	}
	if last == "" {
		return time.Time{}, fmt.Errorf("kein Start-Date")
	}
	if loc == nil {
		loc = time.UTC
	}
	t, err := time.ParseInLocation("2006-01-02 15:04:05", last, loc)
	if err != nil {
		return time.Time{}, fmt.Errorf("ungültiges Datum %q", last)
	}
	return t.UTC(), nil
}

var rpmLastLayouts = []string{
	"Mon Jan _2 15:04:05 2006",
	"Mon 02 Jan 2006 03:04:05 PM MST",
	"Mon 02 Jan 2006 15:04:05 MST",
	"Mon _2 Jan 2006 03:04:05 PM MST",
	"Mon Jan _2 15:04:05 MST 2006",
}

// parseRPMLast parses the first line of `rpm -qa --last` ("name-ver.arch   <date>").
func parseRPMLast(s string, loc *time.Location) (time.Time, error) {
	line := firstLine(s)
	f := strings.Fields(line)
	if len(f) < 2 {
		return time.Time{}, fmt.Errorf("leere Ausgabe")
	}
	date := strings.Join(f[1:], " ")
	if loc == nil {
		loc = time.UTC
	}
	for _, layout := range rpmLastLayouts {
		t, err := time.ParseInLocation(layout, date, loc)
		if err != nil {
			continue
		}
		// unknown zone abbreviations are parsed with offset 0: use the host offset
		if name, off := t.Zone(); off == 0 && name != "UTC" && name != "GMT" && strings.Contains(layout, "MST") {
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc)
		}
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("unbekanntes Datumsformat %q", date)
}
