// Package netsrc is the shared part of the importers that read clients from routers,
// firewalls, network controllers and DHCP servers (OPNsense, pfSense, UniFi, MikroTik,
// FortiGate, Sophos, Meraki, Fritz!Box, Pi-hole): the client model, merging of DHCP, ARP
// and controller records, observations, the run loop over several sources and HTTP helpers.
//
// Importers fill names, addresses and details; they never report presence (their data
// lags, and offline detection needs a scanner's view of a whole subnet).
package netsrc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

// Record kinds of a client.
const (
	KindDHCP   = "dhcp"   // active lease
	KindStatic = "static" // reservation / static mapping
	KindARP    = "arp"    // ARP / neighbour table
	KindClient = "client" // client list of a controller or router
)

// Client is a device a source knows.
type Client struct {
	MAC string
	IP  string
	// Name was given by an administrator (reservation, alias) and wins over Hostname, the
	// name the client reported.
	Name     string
	Hostname string
	Vendor   string
	Kind     string
	Static   bool
	Expires  time.Time // end of the lease (zero: none or unknown)
	// Interface, network or VLAN as the source names it.
	Interface string
	VLAN      int
	Online    *bool
	LastSeen  time.Time
	Wired     *bool
	// Uplink: MAC of the switch or access point the client hangs off, with port or SSID.
	UplinkMAC  string
	UplinkIP   string // when the uplink MAC is unknown (e.g. the router itself)
	UplinkName string
	Port       string
	SSID       string
	OS         string
	// Extra holds further scalar details for the inventory view.
	Extra map[string]string
}

// Bool returns a pointer (for Online and Wired).
func Bool(v bool) *bool { return &v }

// name is the best name of a client.
func (c *Client) name() string {
	if n := strings.TrimSpace(c.Name); n != "" {
		return n
	}
	h := strings.TrimSpace(c.Hostname)
	if h == "*" || strings.EqualFold(h, "unknown") {
		return ""
	}
	return h
}

// Merge combines the records of each MAC address (e.g. a lease, a reservation and an ARP
// entry of the same device) and drops records without a valid MAC. Records given earlier
// win for single values; later ones only fill gaps.
func Merge(lists ...[]Client) []*Client {
	byMAC := map[string]*Client{}
	var order []string
	for _, list := range lists {
		for _, c := range list {
			mac, ok := netutil.NormalizeMAC(c.MAC)
			if !ok {
				continue
			}
			c.MAC = mac
			have := byMAC[mac]
			if have == nil {
				cp := c
				if cp.Extra != nil {
					cp.Extra = cloneMap(c.Extra)
				}
				byMAC[mac] = &cp
				order = append(order, mac)
				continue
			}
			have.fill(&c)
		}
	}
	out := make([]*Client, 0, len(order))
	for _, m := range order {
		out = append(out, byMAC[m])
	}
	return out
}

func cloneMap(m map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// fill copies the values of o that c lacks.
func (c *Client) fill(o *Client) {
	str := func(dst *string, v string) {
		if strings.TrimSpace(*dst) == "" {
			*dst = v
		}
	}
	str(&c.IP, o.IP)
	str(&c.Name, o.Name)
	str(&c.Hostname, o.Hostname)
	str(&c.Vendor, o.Vendor)
	str(&c.Interface, o.Interface)
	str(&c.UplinkMAC, o.UplinkMAC)
	str(&c.UplinkIP, o.UplinkIP)
	str(&c.UplinkName, o.UplinkName)
	str(&c.Port, o.Port)
	str(&c.SSID, o.SSID)
	str(&c.OS, o.OS)
	if c.Kind == KindARP && o.Kind != KindARP {
		c.Kind = o.Kind // a lease or client record says more than an ARP entry
	}
	c.Static = c.Static || o.Static
	if c.Expires.IsZero() {
		c.Expires = o.Expires
	}
	if c.VLAN == 0 {
		c.VLAN = o.VLAN
	}
	if c.Online == nil {
		c.Online = o.Online
	}
	if o.LastSeen.After(c.LastSeen) {
		c.LastSeen = o.LastSeen
	}
	if c.Wired == nil {
		c.Wired = o.Wired
	}
	for k, v := range o.Extra {
		if c.Extra == nil {
			c.Extra = map[string]string{}
		}
		if _, ok := c.Extra[k]; !ok {
			c.Extra[k] = v
		}
	}
}

// Inventory is the per-device view of what a source knows (device_inventory).
type Inventory struct {
	Source    string            `json:"source"` // router, firewall or controller
	Kind      string            `json:"kind"`   // dhcp | static | arp | client
	IP        string            `json:"ip,omitempty"`
	Name      string            `json:"name,omitempty"`
	Hostname  string            `json:"hostname,omitempty"`
	Static    bool              `json:"static"`
	Expires   *time.Time        `json:"expires,omitempty"`
	Interface string            `json:"interface,omitempty"`
	VLAN      int               `json:"vlan,omitempty"`
	Online    *bool             `json:"online,omitempty"`
	LastSeen  *time.Time        `json:"lastSeen,omitempty"`
	Wired     *bool             `json:"wired,omitempty"`
	Uplink    string            `json:"uplink,omitempty"`
	Port      string            `json:"port,omitempty"`
	SSID      string            `json:"ssid,omitempty"`
	OS        string            `json:"os,omitempty"`
	Vendor    string            `json:"vendor,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

func timePtr(t time.Time) *time.Time {
	if t.IsZero() {
		return nil
	}
	return &t
}

// Observation turns a client into an observation of pluginID. create adds devices no scan
// has found yet.
func (c *Client) Observation(pluginID, source string, create bool) *plugin.Observation {
	inv := Inventory{Source: source, Kind: c.Kind, IP: c.IP, Name: strings.TrimSpace(c.Name), Hostname: strings.TrimSpace(c.Hostname),
		Static: c.Static, Expires: timePtr(c.Expires), Interface: c.Interface, VLAN: c.VLAN, Online: c.Online, LastSeen: timePtr(c.LastSeen),
		Wired: c.Wired, Uplink: c.UplinkName, Port: c.Port, SSID: c.SSID, OS: c.OS, Vendor: c.Vendor, Extra: c.Extra}
	if inv.Uplink == "" {
		inv.Uplink = c.UplinkMAC
	}
	target := c.IP
	if target == "" {
		target = c.MAC
	}
	obs := &plugin.Observation{
		MACs:      []string{c.MAC},
		IP:        c.IP,
		Target:    target,
		Hostname:  c.name(),
		Vendor:    strings.TrimSpace(c.Vendor),
		Create:    create,
		Attrs:     map[string]string{pluginID + ".static": strconv.FormatBool(c.Static)},
		Inventory: inv,
	}
	if c.OS != "" {
		obs.OS = &plugin.OSInfo{Name: c.OS}
	}
	var other *plugin.DeviceRef
	if up, ok := netutil.NormalizeMAC(c.UplinkMAC); ok && up != c.MAC {
		other = &plugin.DeviceRef{MAC: up}
	} else if c.UplinkIP != "" && c.UplinkIP != c.IP {
		other = &plugin.DeviceRef{IP: c.UplinkIP}
	}
	if other != nil {
		kind := plugin.RelSwitchPort
		if c.Wired != nil && !*c.Wired {
			kind = plugin.RelWireless
		}
		obs.Relations = []plugin.Relation{{Kind: kind, Other: *other, RemotePort: c.Port, Label: c.SSID}}
	}
	return obs
}

// ---------------------------------------------------------------- run loop

// Fetch reads the clients of one source (a host name, address or URL from the settings).
// It may also return network devices of the source itself (switches, access points) as
// Devices; they are observed before the clients so that relations resolve.
type Fetch func(ctx context.Context, source string) (*Result, error)

// Result is what a source returned.
type Result struct {
	Devices []*plugin.Observation
	Clients []Client
}

// Options control Run.
type Options struct {
	PluginID string
	// Sources from the settings; Label names them in messages ("Router", "Controller").
	Sources []string
	Label   string
	Create  bool
	// Filter drops clients before they are observed (nil: keep all).
	Filter func(*Client) bool
}

// Run reads every source and observes its clients. One failing source out of several is
// logged and counted; the run fails when every source failed.
func Run(ctx context.Context, rc *plugin.RunContext, o Options, fetch Fetch) error {
	if len(o.Sources) == 0 {
		return fmt.Errorf("kein %s konfiguriert", o.Label)
	}
	for _, k := range []string{"clients", "observed", "devices"} {
		rc.SetStat(k, 0)
	}
	var errs []error
	for i, src := range o.Sources {
		err := runSource(ctx, rc, o, src, fetch)
		if len(o.Sources) > 1 {
			rc.Progress(i+1, len(o.Sources))
		}
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if len(o.Sources) == 1 {
			return err
		}
		rc.AddStat("failed_sources", 1)
		rc.Log.Warn(o.Label+" nicht gelesen", "quelle", src, "error", err)
		errs = append(errs, fmt.Errorf("%s: %w", src, err))
	}
	if len(errs) == len(o.Sources) {
		return fmt.Errorf("keine Quelle gelesen: %w", errors.Join(errs...))
	}
	return nil
}

func runSource(ctx context.Context, rc *plugin.RunContext, o Options, src string, fetch Fetch) error {
	res, err := fetch(ctx, src)
	if err != nil {
		return err
	}
	for _, d := range res.Devices {
		if _, err := rc.Sink.Observe(ctx, d); err != nil {
			rc.Log.Warn("Netzwerkgerät konnte nicht gespeichert werden", "ziel", d.Target, "error", err)
			continue
		}
		rc.AddStat("devices", 1)
	}
	clients := Merge(res.Clients)
	sort.SliceStable(clients, func(i, j int) bool { return clients[i].IP < clients[j].IP })
	rc.AddStat("clients", len(clients))
	rc.Log.Info("Clients gelesen", "quelle", src, "clients", len(clients), "netzwerkgeraete", len(res.Devices))
	failed, observed := 0, 0
	for i, c := range clients {
		if err := ctx.Err(); err != nil {
			return err
		}
		if o.Filter != nil && !o.Filter(c) {
			continue
		}
		_, err := rc.Sink.Observe(ctx, c.Observation(o.PluginID, src, o.Create))
		if len(o.Sources) == 1 {
			rc.Progress(i+1, len(clients))
		}
		if err != nil {
			failed++
			rc.Log.Warn("Client konnte nicht gespeichert werden", "mac", c.MAC, "ip", c.IP, "error", err)
			continue
		}
		observed++
		rc.AddStat("observed", 1)
	}
	if failed > 0 && observed == 0 {
		return fmt.Errorf("keiner der %d Clients konnte gespeichert werden", failed)
	}
	return nil
}
