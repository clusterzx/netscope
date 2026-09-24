package plugin

import "time"

// Observation is everything a scanner or importer learned about one device in one go.
// The core resolves the device (DeviceID > MAC > external Ref > IP), writes the
// observation to the raw table, applies it to the state tables and derives Changes.
//
// Nil/empty sections mean "no information" and never delete existing data. To state
// that a section is complete (so missing items are closed), the section types carry
// the scanned range (PortScan.Scanned, HTTPScan.Scanned, TLSScan.Scanned) or are
// complete by definition (PackageInventory, ContainerInventory).
type Observation struct {
	// Identity – at least one of DeviceID, MACs, IP, Ref must be set.
	DeviceID int64        `json:"deviceId,omitempty"`
	MACs     []string     `json:"macs,omitempty"`
	IP       string       `json:"ip,omitempty"`  // address the device was observed at
	IPs      []string     `json:"ips,omitempty"` // further addresses reported by the device itself
	Ref      *ExternalRef `json:"ref,omitempty"`

	// Target is a human-readable label for logs and raw data (defaults to IP).
	Target string `json:"target,omitempty"`
	// Present: the device actively answered right now (updates last_seen / online and
	// presence tracking). Present observations create unknown devices.
	Present bool `json:"present,omitempty"`
	// Create: create a device if none matches, even if not Present (importers).
	Create bool `json:"create,omitempty"`
	// Power is the run state reported by a hypervisor or orchestrator (nil = unknown). A
	// stopped device goes offline at once; a running one counts as online only while no
	// presence scanner tracks it – network scanners decide reachability.
	Power *PowerState `json:"power,omitempty"`

	Hostname   string     `json:"hostname,omitempty"`
	Vendor     string     `json:"vendor,omitempty"`
	Model      string     `json:"model,omitempty"`
	DeviceType string     `json:"deviceType,omitempty"`
	OS         *OSInfo    `json:"os,omitempty"`
	FirstSeen  *time.Time `json:"firstSeen,omitempty"` // historical first sighting (imports)
	// Clear closes facts of this source that are no longer valid: "hostname", "vendor",
	// "model", "type", "os".
	Clear []string `json:"clear,omitempty"`

	Ports      *PortScan           `json:"ports,omitempty"`
	HTTP       *HTTPScan           `json:"http,omitempty"`
	TLS        *TLSScan            `json:"tls,omitempty"`
	Packages   *PackageInventory   `json:"packages,omitempty"`
	Containers *ContainerInventory `json:"containers,omitempty"`
	// Inventory is stored as the latest structured inventory of this source
	// (device_inventory) and shown on the device page.
	Inventory any `json:"inventory,omitempty"`
	// Attrs are scalar facts (e.g. "snmp.sysDescr", "upnp.friendlyName").
	Attrs     map[string]string `json:"attrs,omitempty"`
	Metrics   []Metric          `json:"metrics,omitempty"`
	Relations []Relation        `json:"relations,omitempty"`
	// Manual carries user-curated data from imports (NetAlertX, CSV).
	Manual *ManualData `json:"manual,omitempty"`

	// Raw is the plugin's raw output for this host (shown under "Rohdaten").
	Raw string `json:"-"`
}

// PowerState is the run state of a device as its hypervisor sees it.
type PowerState struct {
	Running bool `json:"running"`
	// Expected: the device is meant to run (e.g. autostart). Only then do state changes
	// raise device.offline / device.online events; guests stopped on purpose stay quiet.
	Expected bool `json:"expected,omitempty"`
}

// ExternalRef links a device to an object in a foreign system.
type ExternalRef struct {
	Source string `json:"source"` // e.g. "proxmox"
	ID     string `json:"id"`     // e.g. "pve1/qemu/101"
	Data   any    `json:"data,omitempty"`
}

// OSInfo is an operating system guess or fact.
type OSInfo struct {
	Name       string   `json:"name"`
	Family     string   `json:"family,omitempty"`
	Vendor     string   `json:"vendor,omitempty"`
	Generation string   `json:"generation,omitempty"`
	Type       string   `json:"type,omitempty"`     // nmap device class, e.g. "general purpose", "router"
	Accuracy   int      `json:"accuracy,omitempty"` // 0-100, 100 = fact (ssh)
	CPEs       []string `json:"cpes,omitempty"`
}

// PortRange is an inclusive port range.
type PortRange struct {
	From int `json:"from"`
	To   int `json:"to"`
}

// Contains reports whether port p lies in any of the ranges.
func ContainsPort(ranges []PortRange, p int) bool {
	for _, r := range ranges {
		if p >= r.From && p <= r.To {
			return true
		}
	}
	return false
}

// PortScan is the result of a port scan of one address.
type PortScan struct {
	Protocol string `json:"protocol"` // tcp | udp
	// Scanned is the exact set of ports probed. Previously open ports inside this set
	// that are not reported anymore are closed. Empty = never close anything.
	Scanned []PortRange `json:"scanned,omitempty"`
	Ports   []Port      `json:"ports"`
}

// Port is an open (or open|filtered) port.
type Port struct {
	Port      int               `json:"port"`
	Proto     string            `json:"proto"`
	State     string            `json:"state"` // open | open|filtered
	Service   string            `json:"service,omitempty"`
	Product   string            `json:"product,omitempty"`
	Version   string            `json:"version,omitempty"`
	ExtraInfo string            `json:"extraInfo,omitempty"`
	Tunnel    string            `json:"tunnel,omitempty"` // "ssl"
	CPEs      []string          `json:"cpes,omitempty"`
	Scripts   map[string]string `json:"scripts,omitempty"`
}

// HTTPScan is the result of probing HTTP(S) endpoints of one address.
type HTTPScan struct {
	Scanned  []int         `json:"scanned,omitempty"` // ports probed
	Services []HTTPService `json:"services"`
}

// HTTPService describes one HTTP(S) endpoint.
type HTTPService struct {
	Port        int               `json:"port"`
	Scheme      string            `json:"scheme"`
	URL         string            `json:"url"`
	StatusCode  int               `json:"statusCode"`
	Title       string            `json:"title,omitempty"`
	Server      string            `json:"server,omitempty"`
	ContentType string            `json:"contentType,omitempty"`
	Redirects   []string          `json:"redirects,omitempty"`
	FinalURL    string            `json:"finalUrl,omitempty"`
	FaviconHash *int32            `json:"faviconHash,omitempty"` // mmh3(base64(favicon)), Shodan style
	FaviconMD5  string            `json:"faviconMd5,omitempty"`
	Apps        []DetectedApp     `json:"apps,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
}

// DetectedApp is a recognised web application.
type DetectedApp struct {
	Name       string `json:"name"`
	Version    string `json:"version,omitempty"`
	Confidence string `json:"confidence"` // high | medium | low
	CPE        string `json:"cpe,omitempty"`
	Evidence   string `json:"evidence,omitempty"`
}

// TLSScan is the result of TLS probing of one address.
type TLSScan struct {
	Scanned []int     `json:"scanned,omitempty"`
	Certs   []TLSCert `json:"certs"`
}

// TLSCert is the leaf certificate and TLS configuration of one endpoint.
type TLSCert struct {
	Port          int       `json:"port"`
	ServerName    string    `json:"serverName,omitempty"`
	Fingerprint   string    `json:"fingerprint"` // sha256 hex of leaf DER
	SubjectCN     string    `json:"subjectCn"`
	SANs          []string  `json:"sans,omitempty"`
	Issuer        string    `json:"issuer"`
	IssuerCN      string    `json:"issuerCn"`
	Serial        string    `json:"serial"`
	NotBefore     time.Time `json:"notBefore"`
	NotAfter      time.Time `json:"notAfter"`
	SelfSigned    bool      `json:"selfSigned"`
	ChainValid    bool      `json:"chainValid"`
	ChainError    string    `json:"chainError,omitempty"`
	KeyType       string    `json:"keyType"`
	KeyBits       int       `json:"keyBits"`
	SignatureAlg  string    `json:"signatureAlg"`
	Versions      []string  `json:"versions,omitempty"` // supported protocol versions
	Cipher        string    `json:"cipher,omitempty"`   // negotiated with default settings
	WeakProtocols []string  `json:"weakProtocols,omitempty"`
	WeakCiphers   []string  `json:"weakCiphers,omitempty"`
}

// PackageInventory is the complete package list of one package manager.
type PackageInventory struct {
	Manager  string    `json:"manager"` // dpkg | rpm | apk
	Packages []Package `json:"packages"`
}

// Package is an installed package.
type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Arch    string `json:"arch,omitempty"`
}

// ContainerInventory is the complete container list of one engine on one host.
type ContainerInventory struct {
	Engine     string           `json:"engine"` // docker
	Containers []Container      `json:"containers"`
	Images     []ContainerImage `json:"images,omitempty"`
}

// Container is a container on a host.
type Container struct {
	ID             string            `json:"id"`
	Name           string            `json:"name"`
	Image          string            `json:"image"`
	ImageID        string            `json:"imageId,omitempty"`
	State          string            `json:"state,omitempty"`
	Status         string            `json:"status,omitempty"`
	Ports          []ContainerPort   `json:"ports,omitempty"`
	Networks       []string          `json:"networks,omitempty"`
	ComposeProject string            `json:"composeProject,omitempty"`
	ComposeService string            `json:"composeService,omitempty"`
	Labels         map[string]string `json:"labels,omitempty"`
	Created        time.Time         `json:"created"`
}

// ContainerPort is a published or exposed container port.
type ContainerPort struct {
	IP          string `json:"ip,omitempty"`
	PrivatePort int    `json:"privatePort"`
	PublicPort  int    `json:"publicPort,omitempty"`
	Type        string `json:"type"` // tcp | udp
}

// ContainerImage is an image present on a host.
type ContainerImage struct {
	ID      string    `json:"id"`
	Tags    []string  `json:"tags,omitempty"`
	Size    int64     `json:"size"`
	Created time.Time `json:"created"`
}

// Metric is one time series sample (per run). For single values Min = Avg = Max.
type Metric struct {
	Name string  `json:"name"` // e.g. icmp.rtt_ms
	Key  string  `json:"key,omitempty"`
	Unit string  `json:"unit,omitempty"`
	Min  float64 `json:"min"`
	Avg  float64 `json:"avg"`
	Max  float64 `json:"max"`
}

// Relation kinds.
const (
	RelRunsOn     = "runs_on"     // VM/CT runs on hypervisor node
	RelSwitchPort = "switch_port" // device is attached to a switch port
	RelLLDP       = "lldp"        // LLDP/CDP neighbour
	RelL3         = "l3"          // routed via gateway
	RelWireless   = "wireless"    // associated to an access point
	RelManual     = "manual"
)

// Relation links the observed device with another device. The set of relations a
// plugin reports for a device (per kind and direction) replaces the previous set.
type Relation struct {
	Kind string `json:"kind"`
	// Parent is true when the observed device is the upstream side (host, switch).
	Parent     bool      `json:"parent"`
	Other      DeviceRef `json:"other"`
	LocalPort  string    `json:"localPort,omitempty"`
	RemotePort string    `json:"remotePort,omitempty"`
	Label      string    `json:"label,omitempty"`
}

// DeviceRef identifies another device (must exist already).
type DeviceRef struct {
	DeviceID int64        `json:"deviceId,omitempty"`
	MAC      string       `json:"mac,omitempty"`
	IP       string       `json:"ip,omitempty"`
	Ref      *ExternalRef `json:"ref,omitempty"`
}

// ManualData is user-curated data from imports. Empty strings mean "no value".
type ManualData struct {
	// Overwrite replaces existing manual values; otherwise only empty fields are filled.
	Overwrite   bool           `json:"overwrite"`
	DisplayName string         `json:"displayName,omitempty"`
	Type        string         `json:"type,omitempty"`
	Location    string         `json:"location,omitempty"`
	Owner       string         `json:"owner,omitempty"`
	Notes       string         `json:"notes,omitempty"`
	Criticality string         `json:"criticality,omitempty"`
	State       string         `json:"state,omitempty"` // known | unknown | ignored
	Tags        []string       `json:"tags,omitempty"`
	Custom      map[string]any `json:"custom,omitempty"`
}
