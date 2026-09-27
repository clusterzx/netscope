// Package wininv parses the inventory of a Windows host: the JSON document the PowerShell
// script of internal/hostscript (windows.ps1) prints on a host with the NetScope agent. It
// yields the device observation (OS with a CPE for the CVE match, hardware, software,
// addresses, structured inventory for the device page) and, on DHCP servers, the leases
// and reservations as clients of other devices.
package wininv

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
	"netscope/internal/plugins/netsrc"
)

// DHCPSource is the source of the leases a Windows DHCP server reports (hostname priority,
// device inventory).
const DHCPSource = "windows_dhcp"

// ---------------------------------------------------------------- script output

// report is the JSON document of windows.ps1.
type report struct {
	Format      int               `json:"format"`
	Errors      map[string]string `json:"errors"`
	Unavailable []string          `json:"unavailable"`
	OS          *osSection        `json:"os"`
	Computer    *computer         `json:"computer"`
	CPU         []CPU             `json:"cpu"`
	Volumes     []Volume          `json:"volumes"`
	Disks       []Disk            `json:"disks"`
	Interfaces  []Interface       `json:"interfaces"`
	Software    []software        `json:"software"`
	Hotfixes    []Hotfix          `json:"hotfixes"`
	Updates     *updates          `json:"updates"`
	Services    []Service         `json:"services"`
	Listening   []Socket          `json:"listening"`
	Firewall    []FirewallProfile `json:"firewall"`
	Defender    *Defender         `json:"defender"`
	Antivirus   []Antivirus       `json:"antivirus"`
	DHCP        *dhcp             `json:"dhcp"`
	CollectedAt string            `json:"collectedAt"`
}

type osSection struct {
	Caption          string `json:"caption"`
	Version          string `json:"version"`
	Build            string `json:"build"`
	UBR              *int64 `json:"ubr"`
	DisplayVersion   string `json:"displayVersion"`
	ReleaseID        string `json:"releaseId"`
	Edition          string `json:"edition"`
	InstallationType string `json:"installationType"`
	ProductType      int    `json:"productType"` // 1 workstation, 2 domain controller, 3 server
	Architecture     string `json:"architecture"`
	InstallDate      string `json:"installDate"`
	LastBoot         string `json:"lastBoot"`
	Hostname         string `json:"hostname"`
	Timezone         string `json:"timezone"`
}

type computer struct {
	Manufacturer string `json:"manufacturer"`
	Model        string `json:"model"`
	Family       string `json:"family"`
	Domain       string `json:"domain"`
	PartOfDomain bool   `json:"partOfDomain"`
	DomainRole   int    `json:"domainRole"`
	MemoryBytes  int64  `json:"memoryBytes"`
	Serial       string `json:"serial"`
	BIOSVendor   string `json:"biosVendor"`
	BIOSVersion  string `json:"biosVersion"`
	BIOSDate     string `json:"biosDate"`
	Chassis      []int  `json:"chassis"`
}

type software struct {
	Name        string `json:"name"`
	Version     string `json:"version"`
	Publisher   string `json:"publisher"`
	InstallDate string `json:"installDate"`
	Arch        string `json:"arch"`
}

type updates struct {
	RebootRequired bool            `json:"rebootRequired"`
	LastSearch     string          `json:"lastSearch"`
	LastInstall    string          `json:"lastInstall"`
	Pending        []PendingUpdate `json:"pending"`
}

type dhcp struct {
	Scopes       []DHCPScope `json:"scopes"`
	Leases       []lease     `json:"leases"`
	Reservations []lease     `json:"reservations"`
}

type lease struct {
	IP          string `json:"ip"`
	MAC         string `json:"mac"`
	Hostname    string `json:"hostname"`
	Name        string `json:"name"`
	State       string `json:"state"`
	Expires     string `json:"expires"`
	Scope       string `json:"scope"`
	Description string `json:"description"`
}

// ---------------------------------------------------------------- stored inventory

// Inventory is the structured host inventory stored in device_inventory (source "agent").
// Platform tells the device page to use the Windows view.
type Inventory struct {
	Platform      string            `json:"platform"` // windows
	Hostname      string            `json:"hostname,omitempty"`
	FQDN          string            `json:"fqdn,omitempty"`
	Domain        string            `json:"domain,omitempty"`
	Workgroup     string            `json:"workgroup,omitempty"`
	DomainRole    string            `json:"domainRole,omitempty"`
	OS            *OSInfo           `json:"os,omitempty"`
	Hardware      *Hardware         `json:"hardware,omitempty"`
	CPU           []CPU             `json:"cpu,omitempty"`
	MemoryBytes   int64             `json:"memoryBytes,omitempty"`
	Volumes       []Volume          `json:"volumes,omitempty"`
	Disks         []Disk            `json:"disks,omitempty"`
	Interfaces    []Interface       `json:"interfaces,omitempty"`
	Updates       *Updates          `json:"updates,omitempty"`
	Services      []Service         `json:"services,omitempty"`
	Listening     []Socket          `json:"listening,omitempty"`
	Firewall      []FirewallProfile `json:"firewall,omitempty"`
	Defender      *Defender         `json:"defender,omitempty"`
	Antivirus     []Antivirus       `json:"antivirus,omitempty"`
	DHCP          *DHCPSummary      `json:"dhcp,omitempty"`
	SoftwareCount int               `json:"softwareCount,omitempty"`
	UptimeSeconds int64             `json:"uptimeSeconds,omitempty"`
	BootTime      *time.Time        `json:"bootTime,omitempty"`
	CollectedAt   *time.Time        `json:"collectedAt,omitempty"`
	// Errors holds collection errors per section; Unavailable lists sections the host
	// cannot provide (module or cmdlet missing).
	Errors      map[string]string `json:"errors,omitempty"`
	Unavailable []string          `json:"unavailable,omitempty"`
}

// OSInfo describes the Windows release.
type OSInfo struct {
	Name             string     `json:"name"`
	Version          string     `json:"version,omitempty"` // 10.0.22631.6199
	DisplayVersion   string     `json:"displayVersion,omitempty"`
	Edition          string     `json:"edition,omitempty"`
	InstallationType string     `json:"installationType,omitempty"` // Client | Server | Server Core
	Architecture     string     `json:"architecture,omitempty"`
	InstallDate      *time.Time `json:"installDate,omitempty"`
	Timezone         string     `json:"timezone,omitempty"`
}

// Hardware is the machine as the firmware describes it.
type Hardware struct {
	Manufacturer string     `json:"manufacturer,omitempty"`
	Model        string     `json:"model,omitempty"`
	Family       string     `json:"family,omitempty"`
	Serial       string     `json:"serial,omitempty"`
	BIOSVendor   string     `json:"biosVendor,omitempty"`
	BIOSVersion  string     `json:"biosVersion,omitempty"`
	BIOSDate     *time.Time `json:"biosDate,omitempty"`
	Chassis      string     `json:"chassis,omitempty"` // desktop | laptop | server | …
	Virtual      bool       `json:"virtual,omitempty"`
}

// CPU is one processor package.
type CPU struct {
	Name    string `json:"name"`
	Cores   int    `json:"cores"`
	Threads int    `json:"threads"`
	MHz     int    `json:"mhz,omitempty"`
}

// Volume is a local fixed drive.
type Volume struct {
	Drive string `json:"drive"`
	Label string `json:"label,omitempty"`
	FS    string `json:"fs,omitempty"`
	Size  int64  `json:"size"`
	Free  int64  `json:"free"`
}

// Disk is a physical (or virtual) disk drive.
type Disk struct {
	Model     string `json:"model,omitempty"`
	Size      int64  `json:"size,omitempty"`
	Interface string `json:"interface,omitempty"`
	Media     string `json:"media,omitempty"`
	Serial    string `json:"serial,omitempty"`
}

// Interface is a network adapter with its addresses.
type Interface struct {
	Index       int      `json:"index"`
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	MAC         string   `json:"mac,omitempty"`
	Status      string   `json:"status,omitempty"` // Up, Disconnected, Disabled …
	SpeedBps    int64    `json:"speedBps,omitempty"`
	Virtual     bool     `json:"virtual,omitempty"`
	Hardware    bool     `json:"hardware,omitempty"`
	Addresses   []IfAddr `json:"addresses,omitempty"`
}

// IfAddr is an address of an adapter.
type IfAddr struct {
	Address      string `json:"address"`
	PrefixLength int    `json:"prefixLength"`
	Family       string `json:"family"`                 // IPv4 | IPv6
	PrefixOrigin string `json:"prefixOrigin,omitempty"` // Manual | Dhcp | RouterAdvertisement | WellKnown
	SuffixOrigin string `json:"suffixOrigin,omitempty"` // Manual | Dhcp | Link | Random …
	State        string `json:"state,omitempty"`
}

// Updates is the patch state of the host.
type Updates struct {
	RebootRequired bool            `json:"rebootRequired"`
	LastSearch     *time.Time      `json:"lastSearch,omitempty"`
	LastInstall    *time.Time      `json:"lastInstall,omitempty"`
	Pending        []PendingUpdate `json:"pending"`
	// PendingSecurity counts pending updates with an MSRC severity.
	PendingSecurity int      `json:"pendingSecurity"`
	Hotfixes        []Hotfix `json:"hotfixes,omitempty"`
	LastHotfix      *Hotfix  `json:"lastHotfix,omitempty"`
	Known           bool     `json:"known"` // the pending list could be read
	// PendingDenied: Windows Update refused the pending list because the agent runs without
	// administrator rights (virtual service account); not a collection error.
	PendingDenied bool `json:"pendingDenied,omitempty"`
}

// PendingUpdate is an update Windows Update knows but has not installed.
type PendingUpdate struct {
	Title      string   `json:"title"`
	KB         []string `json:"kb,omitempty"`
	Severity   string   `json:"severity,omitempty"` // MSRC: Critical, Important, Moderate, Low
	Categories []string `json:"categories,omitempty"`
	Downloaded bool     `json:"downloaded,omitempty"`
}

// Hotfix is an installed update (Win32_QuickFixEngineering).
type Hotfix struct {
	ID          string `json:"id"`
	Description string `json:"description,omitempty"`
	InstalledOn string `json:"installedOn,omitempty"`
}

// Service is a Windows service.
type Service struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName,omitempty"`
	State       string `json:"state"`     // Running, Stopped …
	StartMode   string `json:"startMode"` // Auto, Manual, Disabled
	Account     string `json:"account,omitempty"`
}

// Socket is a listening TCP port or a bound UDP port.
type Socket struct {
	Proto   string `json:"proto"`
	Address string `json:"address"`
	Port    int    `json:"port"`
	PID     int    `json:"pid,omitempty"`
	Process string `json:"process,omitempty"`
}

// FirewallProfile is the state of one Windows Firewall profile.
type FirewallProfile struct {
	Profile string `json:"profile"`
	Enabled bool   `json:"enabled"`
}

// Defender is the state of Microsoft Defender Antivirus.
type Defender struct {
	Antivirus        bool   `json:"antivirus"`
	Realtime         bool   `json:"realtime"`
	SignatureUpdated string `json:"signatureUpdated,omitempty"`
	SignatureVersion string `json:"signatureVersion,omitempty"`
	ProductVersion   string `json:"productVersion,omitempty"`
}

// Antivirus is a product registered with the Security Center (client editions).
type Antivirus struct {
	Name    string `json:"name"`
	State   int64  `json:"state"`
	Enabled bool   `json:"enabled"`
	Current bool   `json:"current"` // signatures up to date
}

// DHCPScope is an IPv4 scope of a Windows DHCP server.
type DHCPScope struct {
	ID    string `json:"id"`
	Mask  string `json:"mask"`
	Name  string `json:"name,omitempty"`
	State string `json:"state,omitempty"`
	Start string `json:"start,omitempty"`
	End   string `json:"end,omitempty"`
	// Leases and Reservations count the entries of the scope.
	Leases       int `json:"leases"`
	Reservations int `json:"reservations"`
}

// DHCPSummary describes the DHCP server role (the leases become their own devices).
type DHCPSummary struct {
	Scopes       []DHCPScope `json:"scopes"`
	Leases       int         `json:"leases"`
	Reservations int         `json:"reservations"`
}

// ---------------------------------------------------------------- parsing

// Result is everything parsed from one inventory.
type Result struct {
	Observation *plugin.Observation
	// DHCP holds the leases and reservations of a DHCP server (nil elsewhere).
	DHCP []netsrc.Client
	// Sections is the number of sections with data (0 = the script produced nothing).
	Sections int
}

// Parse turns the output of windows.ps1 into an observation of the host. subnets are the
// configured networks: addresses inside them identify the host.
func Parse(stdout []byte, truncated bool, subnets []netip.Prefix, now time.Time) (*Result, error) {
	text := strings.TrimSpace(string(bytes.TrimPrefix(stdout, []byte{0xEF, 0xBB, 0xBF}))) // UTF-8 byte order mark
	if text == "" {
		return &Result{}, nil
	}
	var r report
	if err := json.Unmarshal([]byte(text), &r); err != nil {
		if truncated {
			return nil, errors.New("Ausgabe über der Größengrenze abgeschnitten")
		}
		return nil, fmt.Errorf("Ausgabe ist kein gültiges JSON: %w", err)
	}
	// Windows Update gives the pending list only to administrators and SYSTEM: a restricted
	// agent gets "access denied", which is expected rather than an error
	denied := accessDenied(r.Errors["updates.pending"])
	if denied {
		delete(r.Errors, "updates.pending")
	}
	inv := Inventory{Platform: "windows", Errors: translateErrors(r.Errors), Unavailable: r.Unavailable}
	obs := &plugin.Observation{Present: true, Inventory: &inv, Attrs: map[string]string{}, Raw: text}
	res := &Result{Observation: obs}
	count := func(ok bool) {
		if ok {
			res.Sections++
		}
	}
	inv.CollectedAt = parseTime(r.CollectedAt)

	if o := r.OS; o != nil {
		count(true)
		inv.Hostname = strings.TrimSpace(o.Hostname)
		obs.Hostname = inv.Hostname
		version := o.Version
		if o.UBR != nil && o.Build != "" && strings.HasSuffix(o.Version, "."+o.Build) {
			version += "." + strconv.FormatInt(*o.UBR, 10)
		}
		inv.OS = &OSInfo{Name: osName(o.Caption), Version: version, DisplayVersion: firstNonEmpty(o.DisplayVersion, o.ReleaseID),
			Edition: o.Edition, InstallationType: o.InstallationType, Architecture: o.Architecture, InstallDate: parseTime(o.InstallDate),
			Timezone: o.Timezone}
		info := &plugin.OSInfo{Name: inv.OS.Name, Family: "Windows", Vendor: "Microsoft", Generation: inv.OS.DisplayVersion, Accuracy: 100}
		if cpe := osCPE(o.ProductType, o.Build, version, o.Architecture); cpe != "" {
			info.CPEs = []string{cpe}
		}
		if info.Name != "" {
			obs.OS = info
		}
		obs.Attrs["os.build"] = version
		if boot := parseTime(o.LastBoot); boot != nil {
			inv.BootTime = boot
			if up := now.Sub(*boot); up > 0 {
				inv.UptimeSeconds = int64(up.Seconds())
			}
		}
		switch o.ProductType {
		case 2:
			inv.DomainRole = "Domänencontroller"
		case 3:
			inv.DomainRole = "Server"
		case 1:
			inv.DomainRole = "Arbeitsplatz"
		}
	}

	if c := r.Computer; c != nil {
		count(true)
		hw := &Hardware{Manufacturer: clean(c.Manufacturer), Model: clean(c.Model), Family: clean(c.Family), Serial: clean(c.Serial),
			BIOSVendor: clean(c.BIOSVendor), BIOSVersion: clean(c.BIOSVersion), BIOSDate: parseTime(c.BIOSDate)}
		hw.Virtual = isVirtual(hw.Manufacturer, hw.Model)
		hw.Chassis = chassis(c.Chassis)
		inv.Hardware = hw
		inv.MemoryBytes = c.MemoryBytes
		if c.PartOfDomain {
			inv.Domain = strings.ToLower(strings.TrimSpace(c.Domain))
			if inv.Hostname != "" && inv.Domain != "" {
				inv.FQDN = strings.ToLower(inv.Hostname) + "." + inv.Domain
			}
		} else {
			inv.Workgroup = strings.TrimSpace(c.Domain)
		}
		obs.Vendor, obs.Model = hw.Manufacturer, hw.Model
		obs.DeviceType = deviceType(r.OS, hw)
	}

	inv.CPU = r.CPU
	count(len(r.CPU) > 0)
	inv.Volumes = r.Volumes
	count(len(r.Volumes) > 0)
	inv.Disks = r.Disks
	count(len(r.Disks) > 0)

	for i := range r.Interfaces {
		ifc := &r.Interfaces[i]
		if mac, ok := netutil.NormalizeMAC(ifc.MAC); ok {
			ifc.MAC = mac
		} else {
			ifc.MAC = ""
		}
		for j := range ifc.Addresses {
			a := &ifc.Addresses[j]
			a.Address, _, _ = strings.Cut(a.Address, "%") // zone of link-local IPv6 addresses
		}
	}
	inv.Interfaces = r.Interfaces
	count(len(r.Interfaces) > 0)
	obs.IPs, obs.MACs = identity(r.Interfaces, subnets)

	if r.Software != nil {
		count(true)
		obs.Packages = packages(r.Software)
		inv.SoftwareCount = len(obs.Packages.Packages)
	}

	if r.Updates != nil || r.Hotfixes != nil {
		count(true)
		inv.Updates = updateState(r.Updates, r.Hotfixes, r.Errors)
		if denied {
			inv.Updates.Known, inv.Updates.PendingDenied = false, true
		}
	}

	inv.Services = r.Services
	count(len(r.Services) > 0)
	sort.SliceStable(r.Listening, func(i, j int) bool {
		a, b := r.Listening[i], r.Listening[j]
		if a.Proto != b.Proto {
			return a.Proto < b.Proto
		}
		if a.Port != b.Port {
			return a.Port < b.Port
		}
		return a.Address < b.Address
	})
	inv.Listening = r.Listening
	count(len(r.Listening) > 0)
	inv.Firewall = r.Firewall
	inv.Defender = r.Defender
	for i := range r.Antivirus {
		av := &r.Antivirus[i]
		av.Enabled, av.Current = avState(av.State)
	}
	inv.Antivirus = r.Antivirus

	if d := r.DHCP; d != nil {
		count(true)
		inv.DHCP, res.DHCP = dhcpClients(d)
	}
	return res, nil
}

// ---------------------------------------------------------------- helpers

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}

func parseTime(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil || t.Year() < 1980 {
		return nil
	}
	return &t
}

// osName drops the vendor prefix of Win32_OperatingSystem.Caption ("Microsoft Windows 11 Pro").
func osName(caption string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(caption), "Microsoft "))
}

// placeholders are what board vendors leave in the firmware fields.
var placeholders = []string{"to be filled by o.e.m.", "default string", "system product name", "system manufacturer",
	"system serial number", "o.e.m.", "oem", "none", "not applicable", "not specified", "invalid", "0", "123456789",
	"chassis serial number", "base board serial number", "system version", "type1productconfigid", "undefined"}

func clean(s string) string {
	s = strings.TrimSpace(s)
	l := strings.ToLower(s)
	for _, p := range placeholders {
		if l == p {
			return ""
		}
	}
	return s
}

// isVirtual recognises the virtual hardware of the common hypervisors.
func isVirtual(manufacturer, model string) bool {
	m := strings.ToLower(manufacturer + " " + model)
	for _, k := range []string{"virtual machine", "vmware", "virtualbox", "qemu", "kvm", "xen", "hvm domu", "bochs", "parallels",
		"amazon ec2", "google compute engine", "openstack", "proxmox", "bhyve"} {
		if strings.Contains(m, k) {
			return true
		}
	}
	return false
}

// chassis maps the SMBIOS chassis types to a device class.
func chassis(types []int) string {
	for _, t := range types {
		switch t {
		case 8, 9, 10, 11, 12, 14, 18, 21, 31, 32:
			return "laptop"
		case 30:
			return "tablet"
		case 17, 23, 28, 29:
			return "server"
		case 3, 4, 5, 6, 7, 13, 15, 16, 24, 34, 35, 36:
			return "desktop"
		}
	}
	return ""
}

func deviceType(o *osSection, hw *Hardware) string {
	switch {
	case hw.Virtual:
		return "vm"
	case o != nil && (o.ProductType == 2 || o.ProductType == 3):
		return "server"
	case hw.Chassis == "laptop" || hw.Chassis == "tablet" || hw.Chassis == "desktop" || hw.Chassis == "server":
		return hw.Chassis
	}
	return ""
}

// identity returns the addresses of the host inside the configured subnets and the MACs of
// the adapters carrying them. Without a match (no subnets, host in a foreign network) the
// MACs of the connected physical adapters with a global address identify the host.
func identity(ifs []Interface, subnets []netip.Prefix) (ips, macs []string) {
	seenIP, seenMAC := map[string]bool{}, map[string]bool{}
	addMAC := func(m string) {
		if m != "" && !seenMAC[m] {
			seenMAC[m] = true
			macs = append(macs, m)
		}
	}
	for _, ifc := range ifs {
		for _, a := range ifc.Addresses {
			addr, ok := usable(a)
			if !ok {
				continue
			}
			for _, p := range subnets {
				if p.Contains(addr) {
					if !seenIP[addr.String()] {
						seenIP[addr.String()] = true
						ips = append(ips, addr.String())
					}
					addMAC(ifc.MAC)
					break
				}
			}
		}
	}
	if len(macs) == 0 {
		for _, ifc := range ifs {
			if !ifc.Hardware || ifc.Virtual || !strings.EqualFold(ifc.Status, "Up") {
				continue
			}
			for _, a := range ifc.Addresses {
				if _, ok := usable(a); ok {
					addMAC(ifc.MAC)
					break
				}
			}
		}
	}
	netutil.SortIPs(ips)
	return ips, macs
}

// usable reports whether an address can identify the host: global unicast, not a
// temporary (privacy) IPv6 address, not tentative or duplicate.
func usable(a IfAddr) (netip.Addr, bool) {
	addr, err := netip.ParseAddr(a.Address)
	if err != nil {
		return netip.Addr{}, false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() || strings.EqualFold(a.SuffixOrigin, "Random") {
		return netip.Addr{}, false
	}
	if a.State != "" && !strings.EqualFold(a.State, "Preferred") && !strings.EqualFold(a.State, "Deprecated") {
		return netip.Addr{}, false
	}
	return addr, true
}

func packages(list []software) *plugin.PackageInventory {
	seen := map[string]bool{}
	out := &plugin.PackageInventory{Manager: "windows", Packages: []plugin.Package{}}
	for _, s := range list {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			continue
		}
		p := plugin.Package{Name: name, Version: strings.TrimSpace(s.Version), Arch: s.Arch}
		k := p.Name + "\x00" + p.Version + "\x00" + p.Arch
		if seen[k] {
			continue
		}
		seen[k] = true
		out.Packages = append(out.Packages, p)
	}
	sort.Slice(out.Packages, func(i, j int) bool {
		a, b := out.Packages[i], out.Packages[j]
		if !strings.EqualFold(a.Name, b.Name) {
			return strings.ToLower(a.Name) < strings.ToLower(b.Name)
		}
		return a.Version < b.Version
	})
	return out
}

func updateState(u *updates, hotfixes []Hotfix, errs map[string]string) *Updates {
	out := &Updates{Pending: []PendingUpdate{}}
	if u != nil {
		out.RebootRequired = u.RebootRequired
		out.LastSearch, out.LastInstall = parseTime(u.LastSearch), parseTime(u.LastInstall)
		_, failed := errs["updates.pending"]
		out.Known = !failed && errs["updates"] == ""
		if u.Pending != nil {
			out.Pending = u.Pending
		}
		for _, p := range out.Pending {
			if p.Severity != "" {
				out.PendingSecurity++
			}
		}
	}
	sort.SliceStable(hotfixes, func(i, j int) bool { return hotfixes[i].InstalledOn > hotfixes[j].InstalledOn })
	out.Hotfixes = hotfixes
	if len(hotfixes) > 0 && hotfixes[0].InstalledOn != "" {
		h := hotfixes[0]
		out.LastHotfix = &h
	}
	return out
}

// accessDenied recognises E_ACCESSDENIED in a (localised) error message by its code.
func accessDenied(msg string) bool {
	m := strings.ToUpper(msg)
	return strings.Contains(m, "0X80070005") || strings.Contains(m, "E_ACCESSDENIED")
}

// avState decodes the Security Center productState: byte 2 = scanner state (0x10 on),
// byte 3 = signatures (0x00 up to date).
func avState(state int64) (enabled, current bool) {
	return (state>>8)&0x10 != 0, state&0xff == 0
}

// errorTexts translates the fixed error words of the script.
var errorTexts = map[string]string{"timeout": "Zeitüberschreitung"}

func translateErrors(in map[string]string) map[string]string {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		if t, ok := errorTexts[v]; ok {
			v = t
		}
		out[k] = v
	}
	return out
}

// dhcpClients turns leases and reservations into clients (declined addresses are left out)
// and counts them per scope.
func dhcpClients(d *dhcp) (*DHCPSummary, []netsrc.Client) {
	sum := &DHCPSummary{Scopes: d.Scopes}
	byScope := map[string]*DHCPScope{}
	for i := range sum.Scopes {
		byScope[sum.Scopes[i].ID] = &sum.Scopes[i]
	}
	var clients []netsrc.Client
	for _, r := range d.Reservations {
		sum.Reservations++
		if s := byScope[r.Scope]; s != nil {
			s.Reservations++
		}
		clients = append(clients, netsrc.Client{MAC: r.MAC, IP: r.IP, Name: strings.TrimSpace(r.Name), Kind: netsrc.KindStatic, Static: true,
			Interface: r.Scope, Extra: extra("Beschreibung", r.Description)})
	}
	for _, l := range d.Leases {
		if strings.EqualFold(l.State, "Declined") {
			continue
		}
		sum.Leases++
		if s := byScope[l.Scope]; s != nil {
			s.Leases++
		}
		c := netsrc.Client{MAC: l.MAC, IP: l.IP, Hostname: strings.TrimSpace(l.Hostname), Kind: netsrc.KindDHCP,
			Static: strings.Contains(l.State, "Reservation"), Interface: l.Scope, Extra: extra("Status", l.State)}
		if t := parseTime(l.Expires); t != nil {
			c.Expires = *t
		}
		clients = append(clients, c)
	}
	return sum, clients
}

func extra(k, v string) map[string]string {
	if v = strings.TrimSpace(v); v == "" {
		return nil
	}
	return map[string]string{k: v}
}
