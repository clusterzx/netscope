package ssh

import (
	"fmt"
	"net/netip"
	"sort"
	"strings"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

// Inventory is the structured host inventory stored in device_inventory (source "ssh").
// Packages and containers are reported in their own observation sections.
type Inventory struct {
	User          string            `json:"user,omitempty"` // login user of the working credential
	Hostname      string            `json:"hostname,omitempty"`
	OSRelease     map[string]string `json:"osRelease,omitempty"`
	Kernel        string            `json:"kernel,omitempty"` // uname -srmo
	CPU           *CPUInfo          `json:"cpu,omitempty"`
	Memory        *MemInfo          `json:"memory,omitempty"`
	Filesystems   []Filesystem      `json:"filesystems,omitempty"`
	Disks         []BlockDevice     `json:"disks,omitempty"`
	Interfaces    []Interface       `json:"interfaces,omitempty"`
	Services      []Service         `json:"services,omitempty"`
	Listening     []Socket          `json:"listening,omitempty"`
	UptimeSeconds int64             `json:"uptimeSeconds,omitempty"`
	BootTime      *time.Time        `json:"bootTime,omitempty"`
	// LastUpdate is the time of the last package installation/upgrade.
	LastUpdate        *time.Time  `json:"lastUpdate,omitempty"`
	LastUpdateSource  string      `json:"lastUpdateSource,omitempty"` // apt-history | dpkg-status | rpm | apk
	PackageDBModified *time.Time  `json:"packageDbModified,omitempty"`
	PackageManager    string      `json:"packageManager,omitempty"`
	PackageCount      int         `json:"packageCount,omitempty"`
	Docker            *DockerInfo `json:"docker,omitempty"`
	// Errors holds collection errors per section (command failed, output unparsable,
	// cut off by the output limit).
	Errors map[string]string `json:"errors,omitempty"`
	// Unavailable lists sections whose programs do not exist on the host.
	Unavailable []string `json:"unavailable,omitempty"`
}

// DockerInfo summarises the docker engine of the host.
type DockerInfo struct {
	Containers int `json:"containers"`
	Running    int `json:"running"`
	Images     int `json:"images"`
}

// Result is everything parsed from one script run.
type Result struct {
	Inventory  Inventory
	Hostname   string
	OS         *plugin.OSInfo
	Packages   *plugin.PackageInventory
	Containers *plugin.ContainerInventory
	// Sections is the number of sections found in the output (0 = the script did not run).
	Sections int
}

// parseResult turns the script output into a Result. truncated reports that the output
// hit the size limit (the last section is incomplete).
func parseResult(stdout, stderr []byte, truncated bool) *Result {
	secs := splitOutput(stdout, stderr)
	r := &Result{Sections: len(secs)}
	inv := &r.Inventory
	inv.Errors = map[string]string{}
	fail := func(name string, err error) { inv.Errors[name] = err.Error() }

	// usable returns the section if it ran successfully, otherwise records why not.
	usable := func(name string) *sectionOutput {
		s, ok := secs[name]
		switch {
		case !ok:
			return nil
		case s.Missing:
			inv.Unavailable = append(inv.Unavailable, name)
			return nil
		case !s.Done:
			if truncated {
				inv.Errors[name] = "Ausgabe gekürzt (Limit erreicht)"
			} else {
				inv.Errors[name] = "unvollständige Ausgabe"
			}
			return nil
		case s.RC == 124 || s.RC == 143:
			inv.Errors[name] = "Zeitüberschreitung"
			return nil
		case s.RC != 0:
			inv.Errors[name] = commandError(s)
			return nil
		}
		return s
	}

	var (
		now = time.Now()
		loc = time.UTC
	)
	if s := usable(secDate); s != nil {
		if t, l, err := parseDate(s.Out); err == nil {
			now, loc = t, l
		} else {
			fail(secDate, err)
		}
	}
	if s := usable(secOSRelease); s != nil {
		inv.OSRelease = parseOSRelease(s.Out)
		if len(inv.OSRelease) == 0 {
			fail(secOSRelease, fmt.Errorf("keine Einträge"))
		}
	}
	if s := usable(secUname); s != nil {
		inv.Kernel = firstLine(s.Out)
	}
	if s := usable(secHostname); s != nil {
		inv.Hostname = firstLine(s.Out)
		if inv.Hostname != "" && !strings.EqualFold(inv.Hostname, "localhost") && !strings.EqualFold(inv.Hostname, "(none)") {
			r.Hostname = inv.Hostname
		}
	}
	if s := usable(secCPUInfo); s != nil {
		c := parseCPUInfo(s.Out)
		inv.CPU = &c
	}
	if s := usable(secNproc); s != nil {
		var n int
		if _, err := fmt.Sscan(firstLine(s.Out), &n); err == nil && n > 0 {
			if inv.CPU == nil {
				inv.CPU = &CPUInfo{}
			}
			inv.CPU.Usable = n
		} else {
			fail(secNproc, fmt.Errorf("ungültige Ausgabe %q", firstLine(s.Out)))
		}
	}
	if s := usable(secMeminfo); s != nil {
		if m, err := parseMeminfo(s.Out); err == nil {
			inv.Memory = &m
		} else {
			fail(secMeminfo, err)
		}
	}
	if s := usable(secUptime); s != nil {
		if up, err := parseUptime(s.Out); err == nil {
			inv.UptimeSeconds = int64(up)
			boot := now.Add(-time.Duration(up * float64(time.Second))).UTC().Truncate(time.Minute)
			inv.BootTime = &boot
		} else {
			fail(secUptime, err)
		}
	}
	if s := usable(secDF); s != nil {
		inv.Filesystems = parseDF(s.Out)
	}
	if s := usable(secLsblk); s != nil {
		if d, err := parseLsblk(s.Out); err == nil {
			inv.Disks = d
		} else {
			fail(secLsblk, err)
		}
	}
	if s := usable(secIPAddr); s != nil {
		if ifs, err := parseIPAddr(s.Out); err == nil {
			inv.Interfaces = ifs
		} else {
			fail(secIPAddr, err)
		}
	}
	if s := usable(secServices); s != nil {
		inv.Services = parseServices(s.Out)
	}
	if s := usable(secSockets); s != nil {
		inv.Listening = parseSockets(s.Out, s.Variant)
	}

	// packages: several managers may exist (e.g. rpm installed on Debian) – the one with
	// the most packages wins
	for _, pm := range []struct {
		name, sec string
		parse     func(string) []plugin.Package
	}{{"dpkg", secPkgDpkg, parseDpkg}, {"rpm", secPkgRPM, parseRPM}, {"apk", secPkgApk, parseApk}} {
		s := usable(pm.sec)
		if s == nil {
			continue
		}
		pkgs := pm.parse(s.Out)
		if len(pkgs) == 0 {
			continue // an empty list would mark every package as removed
		}
		if r.Packages == nil || len(pkgs) > len(r.Packages.Packages) {
			r.Packages = &plugin.PackageInventory{Manager: pm.name, Packages: pkgs}
		}
	}
	if r.Packages != nil {
		inv.PackageManager, inv.PackageCount = r.Packages.Manager, len(r.Packages.Packages)
	}
	parseLastUpdate(inv, usable, loc, fail)

	// docker: only complete, successful listings are reported
	if ps := usable(secDockerPS); ps != nil {
		containers, bad := parseDockerPS(ps.Out)
		if bad > 0 {
			fail(secDockerPS, fmt.Errorf("%d Zeile(n) nicht lesbar", bad))
		} else {
			ci := &plugin.ContainerInventory{Engine: "docker", Containers: containers}
			if ci.Containers == nil {
				ci.Containers = []plugin.Container{}
			}
			if im := usable(secDockerImages); im != nil {
				images, refs, badImg := parseDockerImages(im.Out)
				if badImg > 0 {
					fail(secDockerImages, fmt.Errorf("%d Zeile(n) nicht lesbar", badImg))
				} else {
					ci.Images = images
					resolveImageIDs(ci.Containers, refs, images)
				}
			}
			r.Containers = ci
			d := &DockerInfo{Containers: len(ci.Containers), Images: len(ci.Images)}
			for _, c := range ci.Containers {
				if c.State == "running" {
					d.Running++
				}
			}
			inv.Docker = d
		}
	} else {
		// docker_images is meaningless without the container list: only note whether
		// docker exists at all
		usable(secDockerImages)
		delete(inv.Errors, secDockerImages)
	}

	r.OS = osInfo(inv.OSRelease)
	sort.Strings(inv.Unavailable)
	if len(inv.Errors) == 0 {
		inv.Errors = nil
	}
	return r
}

func parseLastUpdate(inv *Inventory, usable func(string) *sectionOutput, loc *time.Location, fail func(string, error)) {
	set := func(t time.Time, source string) {
		if t.IsZero() {
			return
		}
		if inv.LastUpdate == nil || t.After(*inv.LastUpdate) {
			tt := t
			inv.LastUpdate, inv.LastUpdateSource = &tt, source
		}
	}
	if s := usable(secLastDpkg); s != nil {
		if t, err := parseEpoch(s.Out); err == nil {
			inv.PackageDBModified = &t
		} else {
			fail(secLastDpkg, err)
		}
	}
	if s := usable(secLastApt); s != nil && strings.TrimSpace(s.Out) != "" {
		if t, err := parseAptHistory(s.Out, loc); err == nil {
			set(t, "apt-history")
		} else {
			fail(secLastApt, err)
		}
	}
	if inv.LastUpdate == nil && inv.PackageDBModified != nil {
		set(*inv.PackageDBModified, "dpkg-status")
	}
	if s := usable(secLastRPM); s != nil && strings.TrimSpace(s.Out) != "" {
		if t, err := parseRPMLast(s.Out, loc); err == nil {
			set(t, "rpm")
		} else {
			fail(secLastRPM, err)
		}
	}
	if s := usable(secLastApk); s != nil {
		if t, err := parseEpoch(s.Out); err == nil {
			set(t, "apk")
		} else {
			fail(secLastApk, err)
		}
	}
}

// commandError describes a failed command by its exit code and first stderr line.
func commandError(s *sectionOutput) string {
	msg := fmt.Sprintf("Exit-Code %d", s.RC)
	if e := firstLine(s.Err); e != "" {
		if len(e) > 200 {
			e = e[:200]
		}
		msg += ": " + e
	}
	return msg
}

// osInfo builds the OS fact from os-release.
func osInfo(rel map[string]string) *plugin.OSInfo {
	if len(rel) == 0 {
		return nil
	}
	name := rel["PRETTY_NAME"]
	if name == "" {
		name = strings.TrimSpace(rel["NAME"] + " " + firstNonEmpty(rel["VERSION"], rel["VERSION_ID"]))
	}
	if name == "" {
		return nil
	}
	o := &plugin.OSInfo{Name: name, Family: "Linux", Vendor: rel["ID"], Generation: rel["VERSION_ID"], Accuracy: 100}
	if cpe := strings.TrimSpace(rel["CPE_NAME"]); strings.HasPrefix(cpe, "cpe:") {
		o.CPEs = []string{cpe}
	}
	return o
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

// virtualIfPrefixes are container/VM bridge interfaces whose addresses never identify
// the host in the LAN.
var virtualIfPrefixes = []string{"docker", "br-", "veth", "virbr", "cni", "flannel", "cali", "podman", "lxcbr", "lxdbr",
	"vxlan", "weave", "kube-", "tunl", "cilium", "vnet"}

func isVirtualInterface(name string) bool {
	for _, p := range virtualIfPrefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// hostAddresses returns the global addresses of the host inside the configured subnets
// (never docker/bridge addresses) and the MAC of the interface carrying scannedIP.
func hostAddresses(ifs []Interface, scannedIP string, subnets []netip.Prefix) (ips []string, mac string) {
	scanned, _ := netip.ParseAddr(scannedIP)
	seen := map[string]bool{}
	for _, ifc := range ifs {
		for _, a := range ifc.Addresses {
			addr, err := netip.ParseAddr(a.Address)
			if err != nil {
				continue
			}
			addr = addr.Unmap()
			if scanned.IsValid() && addr == scanned.Unmap() && ifc.MAC != "" {
				mac = ifc.MAC
			}
			if isVirtualInterface(ifc.Name) || a.Temporary || (a.Scope != "" && a.Scope != "global") {
				continue
			}
			if !addr.IsGlobalUnicast() {
				continue
			}
			for _, p := range subnets {
				if p.Contains(addr) && !seen[addr.String()] {
					seen[addr.String()] = true
					ips = append(ips, addr.String())
					break
				}
			}
		}
	}
	netutil.SortIPs(ips)
	return ips, mac
}

// observation builds the device observation of a successful scan.
func (r *Result) observation(devID int64, ip, user string, subnets []netip.Prefix, raw string) *plugin.Observation {
	inv := r.Inventory
	inv.User = user
	ips, mac := hostAddresses(inv.Interfaces, ip, subnets)
	obs := &plugin.Observation{
		DeviceID:   devID,
		IP:         ip,
		IPs:        ips,
		Present:    true,
		Hostname:   r.Hostname,
		OS:         r.OS,
		Packages:   r.Packages,
		Containers: r.Containers,
		Inventory:  inv,
		Raw:        raw,
	}
	if mac != "" {
		obs.MACs = []string{mac}
	}
	if inv.Kernel != "" {
		obs.Attrs = map[string]string{"os.kernel": inv.Kernel}
	}
	return obs
}
