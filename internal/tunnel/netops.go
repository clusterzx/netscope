package tunnel

import (
	"net/netip"
	"time"

	"netscope/internal/wgconf"
)

// ifPrefix names NetScope's tunnel interfaces: nswg<credential id>.
const ifPrefix = "nswg"

// Interfaces used for the availability probe and the connection test; never managed
// as tunnels.
const (
	probeIface = ifPrefix + "probe"
	testIface  = ifPrefix + "test"
)

// spec describes the desired state of one interface.
type spec struct {
	Config *wgconf.Config
	// Routes are the prefixes sent through the tunnel (the peer's AllowedIPs are set to
	// exactly these); nil for the connection test.
	Routes []netip.Prefix
	// Addresses assigns the tunnel addresses (false for the connection test).
	Addresses bool
}

// peerStats is the live state of the tunnel's peer.
type peerStats struct {
	LastHandshake time.Time
	Rx, Tx        int64
}

// netOps is the host side of the tunnels (Linux: netlink + WireGuard kernel module).
type netOps interface {
	// Probe checks that WireGuard interfaces can be created.
	Probe() error
	// Apply creates or updates an interface (idempotent).
	Apply(name string, s spec) error
	// Remove deletes an interface (no error if it does not exist).
	Remove(name string) error
	// Links lists the existing tunnel interfaces (ifPrefix, without probe/test).
	Links() ([]string, error)
	// Peer returns the handshake and traffic counters of the interface's peer.
	Peer(name string) (peerStats, error)
	Close() error
}
