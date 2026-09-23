package proxmox

import (
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"netscope/internal/netutil"
)

// qemuModels are the NIC models of QEMU net[n] entries ("virtio=AA:BB:…").
var qemuModels = map[string]bool{
	"e1000": true, "e1000-82540em": true, "e1000-82544gc": true, "e1000-82545em": true, "e1000e": true,
	"i82551": true, "i82557b": true, "i82559er": true, "ne2k_isa": true, "ne2k_pci": true, "pcnet": true,
	"rtl8139": true, "virtio": true, "vmxnet3": true,
}

// netIface is a parsed net[n] entry of a guest configuration.
type netIface struct {
	ID       string `json:"id"`
	MAC      string `json:"mac,omitempty"`
	Model    string `json:"model,omitempty"` // qemu NIC model
	Name     string `json:"name,omitempty"`  // lxc interface name
	Bridge   string `json:"bridge,omitempty"`
	VLAN     string `json:"vlan,omitempty"`
	Firewall bool   `json:"firewall,omitempty"`
	LinkDown bool   `json:"linkDown,omitempty"`
	IP       string `json:"ip,omitempty"`  // lxc: dhcp | manual | CIDR
	IP6      string `json:"ip6,omitempty"` // lxc: auto | dhcp | manual | CIDR
	Gateway  string `json:"gateway,omitempty"`
}

// parseNet parses a Proxmox network property string, e.g.
// qemu "virtio=BC:24:11:2E:C5:6A,bridge=vmbr0,firewall=1" or
// lxc  "name=eth0,bridge=vmbr0,hwaddr=BC:24:11:27:22:B5,ip=dhcp,type=veth".
func parseNet(id, spec string) netIface {
	n := netIface{ID: id}
	for i, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		k, v, hasValue := strings.Cut(part, "=")
		k = strings.ToLower(strings.TrimSpace(k))
		v = strings.TrimSpace(v)
		if !hasValue {
			// qemu: the model may be given without MAC as first element ("virtio,bridge=…")
			if i == 0 && qemuModels[k] {
				n.Model = k
			}
			continue
		}
		switch k {
		case "model":
			n.Model = v
		case "macaddr", "hwaddr":
			n.MAC = v
		case "name":
			n.Name = v
		case "bridge":
			n.Bridge = v
		case "tag":
			n.VLAN = v
		case "firewall":
			n.Firewall = v == "1"
		case "link_down":
			n.LinkDown = v == "1"
		case "ip":
			n.IP = v
		case "ip6":
			n.IP6 = v
		case "gw":
			n.Gateway = v
		default:
			if qemuModels[k] {
				n.Model, n.MAC = k, v
			}
		}
	}
	if mac, ok := netutil.NormalizeMAC(n.MAC); ok {
		n.MAC = mac
	} else {
		n.MAC = ""
	}
	return n
}

// guestNets returns the net0 … netN entries of a configuration ordered by index.
func guestNets(cfg guestConfig) []netIface {
	type item struct {
		idx int
		n   netIface
	}
	var items []item
	for key := range cfg {
		if !strings.HasPrefix(key, "net") {
			continue
		}
		idx, err := strconv.Atoi(key[3:])
		if err != nil {
			continue
		}
		items = append(items, item{idx, parseNet(key, cfg.str(key))})
	}
	sort.Slice(items, func(i, j int) bool { return items[i].idx < items[j].idx })
	out := make([]netIface, len(items))
	for i, it := range items {
		out[i] = it.n
	}
	return out
}

// agentEnabled reports whether the QEMU guest agent is enabled in the VM configuration
// ("1", "enabled=1,fstrim_cloned_disks=1", "1,type=virtio").
func agentEnabled(spec string) bool {
	first, _, _ := strings.Cut(strings.TrimSpace(spec), ",")
	k, v, hasValue := strings.Cut(first, "=")
	if !hasValue {
		return k == "1"
	}
	return strings.TrimSpace(k) == "enabled" && strings.TrimSpace(v) == "1"
}

// splitTags splits Proxmox tags ("prod;web", older releases also "prod,web" or spaces).
func splitTags(s string) []string {
	var out []string
	for _, t := range strings.FieldsFunc(s, func(r rune) bool { return r == ';' || r == ',' || r == ' ' }) {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// pveVersion extracts the release from "pve-manager/8.2.4/faa83925c9641325".
func pveVersion(s string) string {
	parts := strings.Split(s, "/")
	if len(parts) >= 2 && parts[0] == "pve-manager" {
		return parts[1]
	}
	return s
}

// ignoredGuestIface reports interfaces whose addresses do not identify the guest on
// the network (container bridges and tunnels inside the guest).
func ignoredGuestIface(name string) bool {
	name = strings.ToLower(name)
	if name == "lo" {
		return true
	}
	// docker user networks are named br-<12 hex digits>; other bridges (br-lan) are real
	if rest, ok := strings.CutPrefix(name, "br-"); ok && len(rest) == 12 && isHex(rest) {
		return true
	}
	for _, p := range []string{"docker", "veth", "cni", "flannel", "cali", "virbr", "lxcbr", "podman", "kube-", "vxlan", "tun", "wg", "tailscale", "zt"} {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

func isHex(s string) bool {
	for _, c := range s {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
			return false
		}
	}
	return s != ""
}

// guestAddresses picks the addresses reported from inside a guest. Addresses of
// interfaces whose MAC belongs to the guest configuration come first (IPv4 before
// IPv6); loopback, link-local and container bridge addresses are dropped.
func guestAddresses(ifaces []guestInterface, macs []string) []string {
	own := map[string]bool{}
	for _, m := range macs {
		own[m] = true
	}
	type cand struct {
		addr     netip.Addr
		ownMAC   bool
		position int
	}
	var cands []cand
	seen := map[netip.Addr]bool{}
	for _, ifc := range ifaces {
		if ignoredGuestIface(ifc.Name) {
			continue
		}
		mac, _ := netutil.NormalizeMAC(ifc.mac())
		for _, s := range ifc.addrs() {
			a, err := netip.ParseAddr(s)
			if err != nil {
				continue
			}
			a = a.Unmap().WithZone("")
			if a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsMulticast() || a.IsUnspecified() || seen[a] {
				continue
			}
			seen[a] = true
			cands = append(cands, cand{addr: a, ownMAC: mac != "" && own[mac], position: len(cands)})
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.ownMAC != b.ownMAC {
			return a.ownMAC
		}
		if a.addr.Is4() != b.addr.Is4() {
			return a.addr.Is4()
		}
		return a.position < b.position
	})
	out := make([]string, len(cands))
	for i, c := range cands {
		out[i] = c.addr.String()
	}
	return out
}

// staticLXCAddresses returns the addresses configured statically in lxc net[n] entries.
func staticLXCAddresses(nets []netIface) []string {
	var out []string
	for _, n := range nets {
		for _, v := range []string{n.IP, n.IP6} {
			if p, err := netip.ParsePrefix(v); err == nil {
				out = append(out, p.Addr().String())
			} else if a, err := netip.ParseAddr(v); err == nil {
				out = append(out, a.String())
			}
		}
	}
	return out
}
