package pluginhost

import (
	"net/netip"
	"slices"
	"strings"

	"netscope/internal/plugin"
)

// SetUnreachable installs the source of subnets that cannot be reached right now
// (tunnels that are down). Runs skip them, so their devices are neither scanned nor
// counted as missed.
func (h *Host) SetUnreachable(fn func() []netip.Prefix) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.unreachable = fn
}

// dropUnreachable removes unreachable subnets and devices that only have addresses
// there from the targets and returns the affected subnets.
func (h *Host) dropUnreachable(t *plugin.Targets) []netip.Prefix {
	h.mu.RLock()
	fn := h.unreachable
	h.mu.RUnlock()
	if fn == nil {
		return nil
	}
	down := fn()
	if len(down) == 0 {
		return nil
	}
	var skipped []netip.Prefix
	mark := func(p netip.Prefix) {
		if !slices.Contains(skipped, p) {
			skipped = append(skipped, p)
		}
	}
	behind := func(a netip.Addr) (netip.Prefix, bool) {
		for _, p := range down {
			if p.Contains(a) {
				return p, true
			}
		}
		return netip.Prefix{}, false
	}
	subnets := t.Subnets[:0:0]
	for _, sn := range t.Subnets {
		if p, ok := behind(sn.CIDR.Addr()); ok && p.Bits() <= sn.CIDR.Bits() {
			mark(p)
			continue
		}
		subnets = append(subnets, sn)
	}
	t.Subnets = subnets
	devices := make([]plugin.DeviceInfo, 0, len(t.Devices))
	for _, d := range t.Devices {
		reachable, known := false, false
		var last netip.Prefix
		for _, ip := range append([]string{d.PrimaryIP}, d.IPs...) {
			a, err := netip.ParseAddr(ip)
			if err != nil {
				continue
			}
			known = true
			if p, ok := behind(a.Unmap()); ok {
				last = p
			} else {
				reachable = true
			}
		}
		if known && !reachable {
			mark(last)
			continue
		}
		devices = append(devices, d)
	}
	t.Devices = devices
	return skipped
}

func prefixList(ps []netip.Prefix) string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.String()
	}
	return strings.Join(out, ", ")
}

// onlyIn reports whether all known addresses of a device lie in the given subnets.
func onlyIn(d plugin.DeviceInfo, prefixes []netip.Prefix) bool {
	known := false
	for _, ip := range append([]string{d.PrimaryIP}, d.IPs...) {
		a, err := netip.ParseAddr(ip)
		if err != nil {
			continue
		}
		known = true
		if !slices.ContainsFunc(prefixes, func(p netip.Prefix) bool { return p.Contains(a.Unmap()) }) {
			return false
		}
	}
	return known
}
