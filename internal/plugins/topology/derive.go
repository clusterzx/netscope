package topology

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"netscope/internal/plugin"
	"netscope/internal/plugins/snmp"
)

// source is the relations.source of every edge this processor owns.
const source = "topology"

// maxInventoryAge: SNMP inventories older than this are ignored (the switch is no
// longer queried), so its edges disappear.
const maxInventoryAge = 7 * 24 * time.Hour

// options of one rebuild.
type options struct {
	uplinkThreshold int
	attachGateway   bool
	excludeTags     map[string]bool
	retention       time.Duration // keep vanished switch_port/lldp edges this long (0 = off)
	now             time.Time
}

// edge is a derived relation (source "topology").
type edge struct {
	parent, child int64
	kind          string
	parentPort    string
	childPort     string
	data          map[string]any
}

type edgeKey struct {
	parent, child int64
	kind          string
}

func (e edge) key() edgeKey { return edgeKey{e.parent, e.child, e.kind} }

// result of a derivation.
type result struct {
	edges []edge         // current edges (inserted or refreshed)
	keep  map[int64]bool // existing topology edges kept unchanged (retention)
}

// port is a bridge port of a managed switch.
type port struct {
	key   string
	name  string
	macs  map[string]bool
	vlans map[int]bool
	lldp  bool // an LLDP neighbour is seen on this port
}

// managed is a device with a fresh SNMP inventory.
type managed struct {
	device int64
	inv    *snmp.Inventory
	ports  map[string]*port
	macs   int // distinct MACs in the FDB
}

func portKey(ifIndex, bridgePort int) string {
	if ifIndex > 0 {
		return fmt.Sprintf("if:%d", ifIndex)
	}
	return fmt.Sprintf("bp:%d", bridgePort)
}

func newManaged(si *switchInv) *managed {
	m := &managed{device: si.device, inv: &si.inv, ports: map[string]*port{}}
	all := map[string]bool{}
	for _, e := range si.inv.FDB {
		switch e.Status {
		case "self", "mgmt", "invalid":
			continue
		}
		if e.MAC == "" || e.BridgePort <= 0 {
			continue
		}
		k := portKey(e.IfIndex, e.BridgePort)
		p := m.ports[k]
		if p == nil {
			name := e.IfName
			if name == "" {
				name = fmt.Sprintf("Port %d", e.BridgePort)
			}
			p = &port{key: k, name: name, macs: map[string]bool{}, vlans: map[int]bool{}}
			m.ports[k] = p
		}
		p.macs[e.MAC] = true
		if e.VLAN > 0 {
			p.vlans[e.VLAN] = true
		}
		all[e.MAC] = true
	}
	m.macs = len(all)
	for _, n := range si.inv.LLDP {
		m.lldpPort(n).lldp = true
	}
	return m
}

// lldpPort returns the port an LLDP neighbour is seen on (created if the FDB has no
// entry for it).
func (m *managed) lldpPort(n snmp.LLDPNeighbor) *port {
	if n.LocalIfIndex > 0 {
		if p, ok := m.ports[portKey(n.LocalIfIndex, 0)]; ok {
			return p
		}
	}
	for _, p := range m.ports {
		if n.LocalPort != "" && p.name == n.LocalPort {
			return p
		}
	}
	k := "lldp:" + n.LocalPort
	if n.LocalIfIndex > 0 {
		k = portKey(n.LocalIfIndex, 0)
	}
	name := n.LocalPort
	if name == "" {
		name = fmt.Sprintf("Port %d", n.LocalPortNum)
	}
	p := &port{key: k, name: name, macs: map[string]bool{}, vlans: map[int]bool{}}
	m.ports[k] = p
	return p
}

func (m *managed) sortedPorts() []*port {
	out := make([]*port, 0, len(m.ports))
	for _, p := range m.ports {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].key < out[j].key })
	return out
}

// infraCaps are LLDP capabilities of network infrastructure.
var infraCaps = map[string]bool{"bridge": true, "router": true, "wlanAccessPoint": true, "repeater": true, "docsisCableDevice": true}

func normalizeTag(t string) string {
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(t))), "-")
}

func pairKey(a, b int64) [2]int64 {
	if a > b {
		a, b = b, a
	}
	return [2]int64{a, b}
}

// derive computes the topology edges from the inventory state.
func derive(in *input, o options) result {
	res := result{keep: map[int64]bool{}}
	excluded := func(id int64) bool {
		d := in.devices[id]
		if d == nil {
			return true
		}
		for _, t := range d.tags {
			if o.excludeTags[normalizeTag(t)] {
				return true
			}
		}
		return false
	}

	// edges of other sources: their children already have an upstream (VM on a host,
	// wireless client, manual placement) and must not be re-attached
	fixed := map[int64]bool{}
	manualPair := map[[2]int64]bool{}
	for _, r := range in.relations {
		if r.source == source {
			continue
		}
		switch r.kind {
		case plugin.RelRunsOn, plugin.RelWireless, plugin.RelSwitchPort, plugin.RelLLDP, plugin.RelManual:
			fixed[r.child] = true
		}
		if r.source == "manual" {
			fixed[r.child] = true
			manualPair[pairKey(r.parent, r.child)] = true
		}
	}

	// managed devices (fresh SNMP inventories)
	var sws []*managed
	byDev := map[int64]*managed{}
	for i := range in.inventories {
		si := &in.inventories[i]
		if excluded(si.device) || (!si.collectedAt.IsZero() && o.now.Sub(si.collectedAt) > maxInventoryAge) {
			continue
		}
		m := newManaged(si)
		sws = append(sws, m)
		byDev[m.device] = m
	}
	sort.Slice(sws, func(i, j int) bool { return sws[i].device < sws[j].device })

	// MAC resolution: device_macs, then interface MACs / LLDP chassis MACs reported by
	// managed devices, then ARP (IP -> device known only by IP)
	ifaceOwner := map[string]int64{}
	claim := func(mac string, dev int64) {
		if mac == "" {
			return
		}
		if cur, ok := ifaceOwner[mac]; ok && cur != dev {
			ifaceOwner[mac] = -1 // ambiguous
			return
		}
		ifaceOwner[mac] = dev
	}
	arpIP := map[string]string{}
	for _, m := range sws {
		for _, i := range m.inv.Interfaces {
			claim(i.MAC, m.device)
		}
		if m.inv.LLDPLocal != nil {
			claim(m.inv.LLDPLocal.ChassisMAC, m.device)
		}
		for _, a := range m.inv.ARP {
			if _, ok := arpIP[a.MAC]; !ok {
				arpIP[a.MAC] = a.IP
			}
		}
	}
	resolveMAC := func(mac string) (int64, string) {
		if id, ok := in.macOwner[mac]; ok {
			return id, "mac"
		}
		if id := ifaceOwner[mac]; id > 0 {
			return id, "snmpInterface"
		}
		if ip, ok := arpIP[mac]; ok {
			if id, ok := in.ipOwner[ip]; ok && in.devices[id] != nil && in.devices[id].macs == 0 {
				return id, "arp"
			}
		}
		return 0, ""
	}

	// ---------------------------------------------------------------- FDB
	type candidate struct {
		m *managed
		p *port
	}
	better := func(a, b candidate) bool {
		if len(a.p.macs) != len(b.p.macs) {
			return len(a.p.macs) < len(b.p.macs)
		}
		if a.m.macs != b.m.macs {
			return a.m.macs < b.m.macs // edge switch before core switch
		}
		if a.m.device != b.m.device {
			return a.m.device < b.m.device
		}
		return a.p.key < b.p.key
	}
	best := map[int64]candidate{}
	seenInFDB := map[int64]bool{}
	for _, m := range sws {
		for _, p := range m.sortedPorts() {
			uplink := p.lldp || len(p.macs) > o.uplinkThreshold
			for mac := range p.macs {
				dev, _ := resolveMAC(mac)
				if dev <= 0 || dev == m.device {
					continue
				}
				seenInFDB[dev] = true
				if uplink || excluded(dev) || fixed[dev] {
					continue
				}
				c := candidate{m, p}
				if cur, ok := best[dev]; !ok || better(c, cur) {
					best[dev] = c
				}
			}
		}
	}
	// two switches seeing each other on access ports: keep one direction (the switch
	// with the larger FDB is upstream)
	childPort := map[int64]string{}
	for dev, c := range best {
		if _, isSwitch := byDev[dev]; !isSwitch {
			continue
		}
		back, ok := best[c.m.device]
		if !ok || back.m.device != dev {
			continue
		}
		parent, child := c.m.device, dev
		if rank(byDev[dev]) > rank(c.m) {
			parent, child = dev, c.m.device
		}
		childPort[child] = best[parent].p.name // port of the child switch that sees the parent
		delete(best, parent)
	}
	// port on a child switch that sees its parent
	portTowards := func(child, parent int64) string {
		if name, ok := childPort[child]; ok {
			return name
		}
		cm := byDev[child]
		if cm == nil {
			return ""
		}
		for _, p := range cm.sortedPorts() {
			for mac := range p.macs {
				if dev, _ := resolveMAC(mac); dev == parent {
					return p.name
				}
			}
		}
		return ""
	}

	// ---------------------------------------------------------------- LLDP
	names := nameIndex(in, sws)
	type report struct {
		from       int64
		localPort  string
		remotePort string
		match      string
	}
	type pair struct {
		reports map[int64]report // by reporting device
	}
	pairs := map[[2]int64]*pair{}
	infraHint := map[int64]bool{}
	for _, m := range sws {
		for _, n := range m.inv.LLDP {
			to, match := matchNeighbor(n, resolveMAC, in.ipOwner, names)
			if to <= 0 || to == m.device || excluded(to) {
				continue
			}
			for _, c := range n.Capabilities {
				if infraCaps[c] {
					infraHint[to] = true
				}
			}
			k := pairKey(m.device, to)
			if manualPair[k] {
				continue
			}
			pr := pairs[k]
			if pr == nil {
				pr = &pair{reports: map[int64]report{}}
				pairs[k] = pr
			}
			if _, dup := pr.reports[m.device]; !dup {
				pr.reports[m.device] = report{from: m.device, localPort: m.lldpPort(n).name, remotePort: n.RemotePort(), match: match}
			}
		}
	}
	isInfra := func(id int64) bool {
		m := byDev[id]
		return (m != nil && m.macs > 0) || infraHint[id]
	}
	fdbSize := func(id int64) int {
		if m := byDev[id]; m != nil {
			return m.macs
		}
		return 0
	}
	lldpPair := map[[2]int64]bool{}
	var edges []edge
	for k, pr := range pairs {
		a, b := k[0], k[1]
		_, ra := pr.reports[a]
		_, rb := pr.reports[b]
		parent := a
		switch {
		case isInfra(a) != isInfra(b):
			if isInfra(b) {
				parent = b
			}
		case fdbSize(a) != fdbSize(b):
			if fdbSize(b) > fdbSize(a) {
				parent = b
			}
		case ra != rb:
			if rb {
				parent = b
			}
		}
		child := b
		if parent == b {
			child = a
		}
		e := edge{parent: parent, child: child, kind: plugin.RelLLDP, data: map[string]any{"via": "lldp"}}
		var by []int64
		if r, ok := pr.reports[parent]; ok {
			e.parentPort, e.childPort = r.localPort, r.remotePort
			e.data["match"] = r.match
			by = append(by, parent)
		}
		if r, ok := pr.reports[child]; ok {
			if e.parentPort == "" {
				e.parentPort = r.remotePort
			}
			e.childPort = r.localPort
			if _, ok := e.data["match"]; !ok {
				e.data["match"] = r.match
			}
			by = append(by, child)
		}
		e.data["reportedBy"] = by
		edges = append(edges, e)
		lldpPair[k] = true
	}

	// switch_port edges (not between LLDP neighbours)
	for dev, c := range best {
		if lldpPair[pairKey(dev, c.m.device)] {
			continue
		}
		vlans := make([]int, 0, len(c.p.vlans))
		for v := range c.p.vlans {
			vlans = append(vlans, v)
		}
		sort.Ints(vlans)
		data := map[string]any{"via": "fdb", "portMacs": len(c.p.macs)}
		if len(vlans) > 0 {
			data["vlans"] = vlans
		}
		edges = append(edges, edge{parent: c.m.device, child: dev, kind: plugin.RelSwitchPort, parentPort: c.p.name,
			childPort: portTowards(dev, c.m.device), data: data})
	}

	// devices with an upstream in layer 2
	hasL2 := map[int64]bool{}
	for id := range fixed {
		hasL2[id] = true
	}
	derived := map[edgeKey]bool{}
	derivedChild := map[int64]bool{}
	for _, e := range edges {
		derived[e.key()] = true
		hasL2[e.child] = true
		derivedChild[e.child] = true
	}

	// retention: vanished switch_port/lldp edges survive for a while (devices that sleep
	// drop out of the FDB after a few minutes)
	if o.retention > 0 {
		for _, r := range in.relations {
			if r.source != source || (r.kind != plugin.RelSwitchPort && r.kind != plugin.RelLLDP) {
				continue
			}
			if derived[edgeKey{r.parent, r.child, r.kind}] || o.now.Sub(r.lastSeen) > o.retention ||
				excluded(r.parent) || excluded(r.child) || fixed[r.child] {
				continue
			}
			switch r.kind {
			case plugin.RelSwitchPort:
				if byDev[r.parent] == nil || derivedChild[r.child] || seenInFDB[r.child] {
					continue
				}
			case plugin.RelLLDP:
				if (byDev[r.parent] == nil && byDev[r.child] == nil) || lldpPair[pairKey(r.parent, r.child)] {
					continue
				}
			}
			res.keep[r.id] = true
			hasL2[r.child] = true
		}
	}

	// layer 3: devices without an upstream hang below the gateway of their subnet (never
	// below a gateway that is itself downstream of the device – no cycles)
	if o.attachGateway {
		parents := map[int64][]int64{}
		for _, e := range edges {
			parents[e.child] = append(parents[e.child], e.parent)
		}
		for _, r := range in.relations {
			if r.source != source || res.keep[r.id] {
				parents[r.child] = append(parents[r.child], r.parent)
			}
		}
		ancestors := func(id int64) map[int64]bool {
			seen := map[int64]bool{}
			queue := []int64{id}
			for len(queue) > 0 && len(seen) < 10000 {
				cur := queue[0]
				queue = queue[1:]
				for _, p := range parents[cur] {
					if !seen[p] {
						seen[p] = true
						queue = append(queue, p)
					}
				}
			}
			return seen
		}
		gwAncestors := map[int64]map[int64]bool{}
		gateway := map[int64]int64{}
		cidr := map[int64]subnet{}
		for _, s := range in.subnets {
			if s.gateway == "" {
				continue
			}
			if g, ok := in.ipOwner[s.gateway]; ok && !excluded(g) {
				gateway[s.id], cidr[s.id] = g, s
			}
		}
		ids := make([]int64, 0, len(in.inSubnet))
		for id := range in.inSubnet {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		for _, id := range ids {
			if hasL2[id] || excluded(id) {
				continue
			}
			for _, sn := range in.inSubnet[id] {
				g, ok := gateway[sn]
				if !ok || g == id {
					continue
				}
				anc, ok := gwAncestors[g]
				if !ok {
					anc = ancestors(g)
					gwAncestors[g] = anc
				}
				if anc[id] {
					continue
				}
				e := edge{parent: g, child: id, kind: plugin.RelL3,
					data: map[string]any{"via": "gateway", "subnet": cidr[sn].cidr, "gateway": cidr[sn].gateway}}
				if !derived[e.key()] {
					derived[e.key()] = true
					edges = append(edges, e)
				}
			}
		}
	}

	sort.Slice(edges, func(i, j int) bool {
		a, b := edges[i], edges[j]
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		if a.parent != b.parent {
			return a.parent < b.parent
		}
		return a.child < b.child
	})
	res.edges = edges
	return res
}

// rank orders switches by FDB size (ties: lower device id ranks higher).
func rank(m *managed) float64 {
	return float64(m.macs) - float64(m.device)*1e-12
}

// nameIndex maps lower-case host names (full and short) to devices; ambiguous names
// are dropped.
func nameIndex(in *input, sws []*managed) map[string]int64 {
	idx := map[string]int64{}
	add := func(name string, id int64) {
		name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
		if name == "" || name == "localhost" {
			return
		}
		keys := []string{name}
		if short, _, found := strings.Cut(name, "."); found && short != "" {
			keys = append(keys, short)
		}
		for _, k := range keys {
			if cur, ok := idx[k]; ok && cur != id {
				idx[k] = -1
				continue
			}
			idx[k] = id
		}
	}
	ids := make([]int64, 0, len(in.devices))
	for id := range in.devices {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	for _, id := range ids {
		d := in.devices[id]
		add(d.hostname, id)
		add(d.display, id)
	}
	for _, m := range sws {
		add(m.inv.System.Name, m.device)
		if m.inv.LLDPLocal != nil {
			add(m.inv.LLDPLocal.SysName, m.device)
		}
	}
	return idx
}

// matchNeighbor resolves an LLDP neighbour to a device: chassis MAC, chassis network
// address, management address, system name, port MAC.
func matchNeighbor(n snmp.LLDPNeighbor, resolveMAC func(string) (int64, string), ipOwner map[string]int64, names map[string]int64) (int64, string) {
	if n.ChassisMAC != "" {
		if id, _ := resolveMAC(n.ChassisMAC); id > 0 {
			return id, "chassisMac"
		}
	}
	if n.ChassisIDSubtype == "networkAddress" {
		if id, ok := ipOwner[n.ChassisID]; ok {
			return id, "chassisIp"
		}
	}
	for _, a := range n.ManagementAddresses {
		if id, ok := ipOwner[a]; ok {
			return id, "managementIp"
		}
	}
	if name := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(n.SysName), ".")); name != "" {
		if id := names[name]; id > 0 {
			return id, "sysName"
		}
		if short, _, found := strings.Cut(name, "."); found {
			if id := names[short]; id > 0 {
				return id, "sysName"
			}
		}
	}
	if n.PortMAC != "" {
		if id, _ := resolveMAC(n.PortMAC); id > 0 {
			return id, "portMac"
		}
	}
	return 0, ""
}
