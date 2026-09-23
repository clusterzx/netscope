package plugin

import "time"

// ChangeType identifies a state change derived by the core from observations.
type ChangeType string

const (
	ChangeDeviceCreated    ChangeType = "device.created"    // New: DeviceSnapshot
	ChangeDeviceOnline     ChangeType = "device.online"     // Old: *time.Time offline since
	ChangeDeviceOffline    ChangeType = "device.offline"    // Old: time.Time last seen
	ChangeIPChanged        ChangeType = "ip.changed"        // Old/New: string IP
	ChangeIPAdded          ChangeType = "ip.added"          // New: string IP
	ChangeMACChanged       ChangeType = "ip.mac_changed"    // Key: IP, Old/New: MACChange
	ChangeMACAdded         ChangeType = "mac.added"         // New: string MAC
	ChangeHostname         ChangeType = "hostname.changed"  // Old/New: string (effective hostname)
	ChangeOS               ChangeType = "os.changed"        // Old/New: string (effective OS)
	ChangePortOpened       ChangeType = "port.opened"       // New: *Port, Key: ip proto/port
	ChangePortClosed       ChangeType = "port.closed"       // Old: *Port
	ChangePortChanged      ChangeType = "port.changed"      // Old/New: *Port (service/product/version)
	ChangeCertAdded        ChangeType = "cert.added"        // New: *TLSCert, Key: ip:port
	ChangeCertChanged      ChangeType = "cert.changed"      // Old/New: *TLSCert
	ChangeCertRemoved      ChangeType = "cert.removed"      // Old: *TLSCert
	ChangeContainerAdded   ChangeType = "container.added"   // New: *Container
	ChangeContainerRemoved ChangeType = "container.removed" // Old: *Container
	ChangeContainerImage   ChangeType = "container.image"   // Old/New: *Container
	ChangePackages         ChangeType = "packages.changed"  // New: *PackageDelta
)

// Change is one state change of one device.
type Change struct {
	Type     ChangeType `json:"type"`
	DeviceID int64      `json:"deviceId"`
	PluginID string     `json:"pluginId"`
	RunID    int64      `json:"runId,omitempty"`
	At       time.Time  `json:"at"`
	// Initial is true when the device had no data of this kind from this source before
	// (first scan) – processors usually do not raise events for initial data.
	Initial bool   `json:"initial,omitempty"`
	Key     string `json:"key,omitempty"`
	Old     any    `json:"old,omitempty"`
	New     any    `json:"new,omitempty"`
}

// DeviceSnapshot describes a device at creation time.
type DeviceSnapshot struct {
	ID       int64  `json:"id"`
	IP       string `json:"ip,omitempty"`
	MAC      string `json:"mac,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	Vendor   string `json:"vendor,omitempty"`
	State    string `json:"state"`
	Source   string `json:"source"`
}

// MACChange describes an IP that is now answered by a different MAC.
type MACChange struct {
	IP          string `json:"ip"`
	OldMAC      string `json:"oldMac"`
	NewMAC      string `json:"newMac"`
	OldDeviceID int64  `json:"oldDeviceId"`
}

// PackageUpdate is a changed package version.
type PackageUpdate struct {
	Name string `json:"name"`
	Arch string `json:"arch,omitempty"`
	From string `json:"from"`
	To   string `json:"to"`
}

// PackageDelta aggregates package changes of one device in one observation.
type PackageDelta struct {
	Manager string          `json:"manager"`
	Added   []Package       `json:"added,omitempty"`
	Removed []Package       `json:"removed,omitempty"`
	Updated []PackageUpdate `json:"updated,omitempty"`
}

// Count is the total number of changed packages.
func (d *PackageDelta) Count() int { return len(d.Added) + len(d.Removed) + len(d.Updated) }
