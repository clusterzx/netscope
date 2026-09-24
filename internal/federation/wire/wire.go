// Package wire defines the protocol between a site instance and its central instance.
//
// A site delivers batches of numbered items to POST /api/v1/federation/ingest of the
// central instance. Items are applied in order; the central instance acknowledges the
// highest applied number, and the site deletes everything up to it from its outbox. The
// numbering belongs to a stream (epoch); a new stream starts with a full synchronisation.
package wire

import (
	"encoding/json"
	"time"

	"netscope/internal/plugin"
)

// Protocol versions: a central instance accepts MinProtocol … Protocol, so sites and
// central need not be updated at the same time.
const (
	Protocol    = 1
	MinProtocol = 1
)

// TokenPrefix marks site tokens (ingest only, never valid for the regular API).
const TokenPrefix = "nss_"

// IngestPath is the endpoint of the central instance.
const IngestPath = "/api/v1/federation/ingest"

// Item kinds.
const (
	KindObservation = "observation" // Observation
	KindPresence    = "presence"    // Presence
	KindIPGone      = "ip_gone"     // IPGone
	KindDevice      = "device"      // DeviceOp
	KindEvent       = "event"       // Event
	KindSync        = "sync"        // Sync
)

// Batch is one delivery.
type Batch struct {
	Protocol int      `json:"protocol"`
	Epoch    string   `json:"epoch"`
	Instance Instance `json:"instance"`
	Status   *Status  `json:"status,omitempty"`
	Items    []Item   `json:"items"`
}

// Instance describes the delivering site.
type Instance struct {
	Version   string    `json:"version"`
	URL       string    `json:"url,omitempty"` // public URL of the site's web UI
	Hostname  string    `json:"hostname,omitempty"`
	StartedAt time.Time `json:"startedAt"`
}

// Status is the state of the site shown in the central site overview.
type Status struct {
	Buffered   int            `json:"buffered"`
	OldestItem *time.Time     `json:"oldestItem,omitempty"`
	Devices    int            `json:"devices"`
	Online     int            `json:"online"`
	Subnets    []Subnet       `json:"subnets"`
	Plugins    []PluginStatus `json:"plugins"`
	Syncing    bool           `json:"syncing,omitempty"`
}

// Subnet is a subnet configured at the site.
type Subnet struct {
	CIDR    string `json:"cidr"`
	Name    string `json:"name,omitempty"`
	Gateway string `json:"gateway,omitempty"`
	Access  string `json:"access,omitempty"`
	Enabled bool   `json:"enabled"`
	Devices int    `json:"devices"`
}

// PluginStatus is the state of an enabled plugin at the site.
type PluginStatus struct {
	ID        string     `json:"id"`
	Name      string     `json:"name"`
	LastRun   *time.Time `json:"lastRun,omitempty"`
	Status    string     `json:"status,omitempty"` // status of the last run
	Error     string     `json:"error,omitempty"`
	Scheduled bool       `json:"scheduled"`
}

// Item is one numbered change.
type Item struct {
	Seq  int64           `json:"seq"`
	Kind string          `json:"kind"`
	At   time.Time       `json:"at"`
	Data json.RawMessage `json:"data"`
}

// Response acknowledges a batch.
type Response struct {
	Protocol int   `json:"protocol"`
	Acked    int64 `json:"acked"`
	// Resync asks the site to start a new stream with a full synchronisation (the
	// central instance misses items, e.g. after a restore or a dropped buffer).
	Resync bool `json:"resync,omitempty"`
	// Site is the name of the site at the central instance.
	Site string `json:"site"`
}

// Observation is an observation applied at the site. Device is the site's device id;
// device ids inside the observation (relations) are site ids as well.
type Observation struct {
	Plugin string              `json:"plugin"`
	Device int64               `json:"device"`
	Obs    *plugin.Observation `json:"obs"`
}

// Presence is the online state of a device at the site. Online changes caused by an
// answering device are part of the observations; this item carries the transitions to
// offline and, during a synchronisation, the complete state.
type Presence struct {
	Device    int64      `json:"device"`
	Online    bool       `json:"online"`
	Since     *time.Time `json:"since,omitempty"` // online state unchanged since
	LastSeen  *time.Time `json:"lastSeen,omitempty"`
	FirstSeen *time.Time `json:"firstSeen,omitempty"`
	// Scanners are the presence scanners tracking the device (synchronisation only).
	Scanners []Scanner `json:"scanners,omitempty"`
}

// Scanner is a presence scanner that has seen a device.
type Scanner struct {
	Plugin   string    `json:"plugin"`
	LastSeen time.Time `json:"lastSeen"`
	Missed   int       `json:"missed"`
}

// IPGone closes an address the device no longer uses (IP change at the end of a run).
type IPGone struct {
	Device int64  `json:"device"`
	IP     string `json:"ip"`
}

// Device operations.
const (
	OpDeleted = "deleted"
	OpMerged  = "merged"
	OpSplit   = "split"
)

// DeviceOp mirrors a manual or automatic identity change at the site.
type DeviceOp struct {
	Op     string `json:"op"`
	Device int64  `json:"device"` // deleted: the device; merged: the target; split: the source
	// Sources are the devices merged into Device (merged).
	Sources []int64 `json:"sources,omitempty"`
	// New is the device created by a split, MACs the addresses it took over.
	New  int64    `json:"new,omitempty"`
	MACs []string `json:"macs,omitempty"`
}

// Event is an event raised at the site.
type Event struct {
	Type     string          `json:"type"`
	Severity plugin.Severity `json:"severity"`
	Device   int64           `json:"device,omitempty"`
	Plugin   string          `json:"plugin"`
	Title    string          `json:"title"`
	Message  string          `json:"message,omitempty"`
	Payload  map[string]any  `json:"payload,omitempty"`
	ID       int64           `json:"id"` // event id at the site (for links)
}

// Synchronisation phases.
const (
	SyncBegin = "begin"
	SyncEnd   = "end"
)

// Sync frames a full synchronisation: between begin and end the site sends the complete
// state of every device; end lists all devices so the central instance can remove the
// ones the site no longer has.
type Sync struct {
	Phase string `json:"phase"`
	// Reset (begin): device ids of the site are not the ones delivered before (the site
	// database was restored), so the central instance must re-match all devices.
	Reset   bool    `json:"reset,omitempty"`
	Devices []int64 `json:"devices,omitempty"` // end
}

// Marshal encodes an item payload.
func Marshal(v any) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	return json.RawMessage(b), err
}
