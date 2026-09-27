// Package proto is the protocol between the NetScope agent and its instance. The agent
// only connects out (HTTP(S), JSON, gzip bodies) and never accepts commands beyond "collect
// now", new settings and the offer of a new agent version.
//
//	POST /api/v1/agent/enroll     enrollment token → agent id + secret (once, at install)
//	GET  /api/v1/agent/poll       long poll: settings, refresh request, update offer
//	POST /api/v1/agent/inventory  output of the collection script (internal/hostscript)
//	POST /api/v1/agent/metrics    batch of utilisation samples
//
// The same agent runs on Linux (systemd/OpenRC service, POSIX sh collection script) and on
// Windows (Windows service, PowerShell collection script).
//
// Every call after the enrollment carries "Authorization: Bearer nsag_…".
package proto

import "time"

// Paths of the agent API.
const (
	PathEnroll    = "/api/v1/agent/enroll"
	PathPoll      = "/api/v1/agent/poll"
	PathInventory = "/api/v1/agent/inventory"
	PathMetrics   = "/api/v1/agent/metrics"
	// PathBinary is followed by the platform, e.g. /agent/bin/linux-amd64 or windows-amd64
	// (+ ".sha256").
	PathBinary = "/agent/bin/"
)

// Token prefixes.
const (
	EnrollPrefix = "nse_"  // enrollment token (install command)
	AgentPrefix  = "nsag_" // secret of one agent
)

// Host describes the machine an agent runs on.
type Host struct {
	MachineID string `json:"machineId"` // /etc/machine-id, MachineGuid on Windows
	Hostname  string `json:"hostname"`
	OS        string `json:"os"` // PRETTY_NAME of os-release, product name and version on Windows
	// Platform is the operating system family: linux (also when absent, older agents) or windows.
	Platform string `json:"platform,omitempty"`
	Arch     string `json:"arch"`             // GOARCH (+ v7 for arm)
	Kernel   string `json:"kernel,omitempty"` // kernel release, build number on Windows
	Version  string `json:"version"`          // agent version
	Docker   bool   `json:"docker"`           // the agent may read the docker socket
}

// EnrollRequest registers an agent with an enrollment token.
type EnrollRequest struct {
	Token string `json:"token"`
	Host  Host   `json:"host"`
}

// EnrollResponse carries the agent's own secret (stored by the agent, only its hash on
// the server).
type EnrollResponse struct {
	AgentID int64  `json:"agentId"`
	Secret  string `json:"secret"`
	Config  Config `json:"config"`
}

// Config are the settings the instance hands out (plugin "agent").
type Config struct {
	InventoryInterval time.Duration `json:"inventoryInterval"` // full inventory
	SampleInterval    time.Duration `json:"sampleInterval"`    // one utilisation sample
	ReportInterval    time.Duration `json:"reportInterval"`    // sending the samples
	Packages          bool          `json:"packages"`          // collect installed packages
	Docker            bool          `json:"docker"`            // collect docker containers (if readable)
	CommandTimeout    time.Duration `json:"commandTimeout"`
}

// DefaultConfig is used until the instance sent its settings.
func DefaultConfig() Config {
	return Config{InventoryInterval: time.Hour, SampleInterval: time.Minute, ReportInterval: 5 * time.Minute,
		Packages: true, Docker: true, CommandTimeout: 30 * time.Second}
}

// PollResponse answers a long poll.
type PollResponse struct {
	Config Config `json:"config"`
	// Refresh asks for an inventory now (button in the UI).
	Refresh bool `json:"refresh,omitempty"`
	// Update offers a newer agent binary for this platform.
	Update *Update `json:"update,omitempty"`
}

// Update describes an agent binary the instance offers.
type Update struct {
	Version string `json:"version"`
	Path    string `json:"path"`   // relative to the instance URL
	SHA256  string `json:"sha256"` // hex
}

// InventoryReport carries the raw output of the collection script; the instance parses it
// with the same code as the SSH inventory (Linux) or as the JSON document of the Windows
// script (internal/agent/wininv).
type InventoryReport struct {
	Host        Host      `json:"host"`
	CollectedAt time.Time `json:"collectedAt"`
	Stdout      string    `json:"stdout"`
	Stderr      string    `json:"stderr"`
	Truncated   bool      `json:"truncated,omitempty"`
	Error       string    `json:"error,omitempty"` // the script could not run
}

// MetricsReport is a batch of samples (buffered while the instance was unreachable).
type MetricsReport struct {
	Samples []Sample `json:"samples"`
}

// Sample is the utilisation at one point in time. Rates are per second since the previous
// sample; they are absent in the first sample after the start.
type Sample struct {
	At       time.Time `json:"at"`
	CPU      *float64  `json:"cpu,omitempty"`   // busy %, all cores
	Load1    *float64  `json:"load1,omitempty"` // load average; Windows has none
	MemTotal uint64    `json:"memTotal"`        // bytes
	MemUsed  uint64    `json:"memUsed"`         // total − available
	SwapUsed uint64    `json:"swapUsed"`
	SwapTot  uint64    `json:"swapTotal"`
	Disks    []Disk    `json:"disks,omitempty"`
	Net      []NetRate `json:"net,omitempty"`
}

// Disk is the usage of one mounted file system.
type Disk struct {
	Mount string `json:"mount"`
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

// NetRate is the throughput of one interface in bytes per second.
type NetRate struct {
	Interface string  `json:"if"`
	RX        float64 `json:"rx"`
	TX        float64 `json:"tx"`
}
