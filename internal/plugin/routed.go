package plugin

import (
	"context"
	"net/netip"
	"slices"
)

// RoutedPrefixes returns the subnets that are reached through a router or tunnel: the
// routed subnets of the run's targets and – for device scans, whose targets carry no
// subnets – of the subnet configuration.
func RoutedPrefixes(ctx context.Context, rc *RunContext) []netip.Prefix {
	var out []netip.Prefix
	add := func(list []SubnetTarget) {
		for _, s := range list {
			if p := s.CIDR.Masked(); s.Routed && !slices.Contains(out, p) {
				out = append(out, p)
			}
		}
	}
	add(rc.Targets.Subnets)
	if rc.Inventory != nil {
		if list, err := rc.Inventory.Subnets(ctx); err == nil {
			add(list)
		}
	}
	return out
}

// LocalOnly removes everything behind a router or tunnel from the targets, for layer-2 and
// multicast scanners (ARP, mDNS, SSDP) that cannot reach it: routed subnets and device
// addresses inside the routed prefixes (devices without other addresses are dropped). It
// returns the reduced targets and the prefixes that were skipped, for RunContext.NotCovered.
func (t Targets) LocalOnly(routed []netip.Prefix) (Targets, []netip.Prefix) {
	if len(routed) == 0 {
		return t, nil
	}
	var skipped []netip.Prefix
	skip := func(p netip.Prefix) {
		if !slices.Contains(skipped, p) {
			skipped = append(skipped, p)
		}
	}
	behind := func(ip string) (netip.Prefix, bool) {
		a, err := netip.ParseAddr(ip)
		if err != nil {
			return netip.Prefix{}, false
		}
		for _, p := range routed {
			if p.Contains(a.Unmap()) {
				return p, true
			}
		}
		return netip.Prefix{}, false
	}
	out := Targets{DeviceMode: t.DeviceMode}
	for _, s := range t.Subnets {
		if p, ok := behind(s.CIDR.Addr().String()); ok && p.Bits() <= s.CIDR.Bits() {
			skip(p)
			continue
		}
		if s.Routed {
			skip(s.CIDR.Masked())
			continue
		}
		out.Subnets = append(out.Subnets, s)
	}
	out.Devices = make([]DeviceInfo, 0, len(t.Devices))
	for _, d := range t.Devices {
		var ips []string
		for _, ip := range d.IPs {
			if p, ok := behind(ip); ok {
				skip(p)
				continue
			}
			ips = append(ips, ip)
		}
		primary := d.PrimaryIP
		if p, ok := behind(primary); ok {
			skip(p)
			primary = ""
			if len(ips) > 0 {
				primary = ips[0]
			}
		}
		if primary == "" && len(ips) == 0 && (d.PrimaryIP != "" || len(d.IPs) > 0) {
			continue // only reachable through a router
		}
		d.PrimaryIP, d.IPs = primary, ips
		out.Devices = append(out.Devices, d)
	}
	return out, skipped
}
