// Package netutil contains small helpers for addresses and interfaces.
package netutil

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"sort"
	"strings"
	"time"
)

// NormalizeMAC returns the MAC in lower-case colon notation (aa:bb:cc:dd:ee:ff).
// It accepts colon, dash, dot (cisco) and bare hex notations. Only 48-bit MACs are valid.
func NormalizeMAC(s string) (string, bool) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return "", false
	}
	hex := strings.NewReplacer(":", "", "-", "", ".", "", " ", "").Replace(s)
	if len(hex) != 12 {
		// "a:b:c:d:e:f" (leading zeros stripped, e.g. from SNMP/ARP tables)
		parts := strings.FieldsFunc(s, func(r rune) bool { return r == ':' || r == '-' })
		if len(parts) != 6 {
			return "", false
		}
		for i, p := range parts {
			if len(p) == 1 {
				parts[i] = "0" + p
			}
		}
		hex = strings.Join(parts, "")
		if len(hex) != 12 {
			return "", false
		}
	}
	for _, c := range hex {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", false
		}
	}
	var b strings.Builder
	for i := 0; i < 12; i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(hex[i : i+2])
	}
	mac := b.String()
	if mac == "00:00:00:00:00:00" || mac == "ff:ff:ff:ff:ff:ff" {
		return "", false
	}
	return mac, true
}

// IsRandomizedMAC reports whether the locally administered bit is set (private/random MAC).
func IsRandomizedMAC(mac string) bool {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) == 0 {
		return false
	}
	return hw[0]&0x02 != 0
}

// IsMulticastMAC reports whether the group bit is set.
func IsMulticastMAC(mac string) bool {
	hw, err := net.ParseMAC(mac)
	if err != nil || len(hw) == 0 {
		return false
	}
	return hw[0]&0x01 != 0
}

// NormalizeIP parses an IPv4/IPv6 address and returns its canonical string form.
func NormalizeIP(s string) (string, bool) {
	a, err := netip.ParseAddr(strings.TrimSpace(s))
	if err != nil {
		return "", false
	}
	a = a.Unmap().WithZone("")
	if a.IsUnspecified() {
		return "", false
	}
	return a.String(), true
}

// IPKey returns a 16-byte key that sorts addresses numerically (IPv4 as v4-mapped).
func IPKey(ip string) []byte {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return []byte{}
	}
	b := a.As16()
	return b[:]
}

// SortIPs sorts addresses numerically in place.
func SortIPs(ips []string) {
	sort.Slice(ips, func(i, j int) bool {
		a, errA := netip.ParseAddr(ips[i])
		b, errB := netip.ParseAddr(ips[j])
		if errA != nil || errB != nil {
			return ips[i] < ips[j]
		}
		return a.Less(b)
	})
}

// Hosts enumerates the usable host addresses of an IPv4 prefix (without network and
// broadcast address for prefixes shorter than /31). It refuses prefixes larger than /16.
func Hosts(p netip.Prefix) ([]netip.Addr, error) {
	p = p.Masked()
	if !p.Addr().Is4() {
		return nil, fmt.Errorf("nur IPv4-Subnetze werden aufgezählt: %s", p)
	}
	if p.Bits() < 16 {
		return nil, fmt.Errorf("Subnetz %s ist zu groß (maximal /16)", p)
	}
	var out []netip.Addr
	a := p.Addr()
	for p.Contains(a) {
		out = append(out, a)
		a = a.Next()
		if !a.IsValid() {
			break
		}
	}
	if p.Bits() < 31 && len(out) >= 2 {
		out = out[1 : len(out)-1]
	}
	return out, nil
}

// Broadcast returns the broadcast address of an IPv4 prefix.
func Broadcast(p netip.Prefix) (netip.Addr, bool) {
	if !p.Addr().Is4() {
		return netip.Addr{}, false
	}
	b := p.Masked().Addr().As4()
	hostBits := 32 - p.Bits()
	v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
	v |= (1 << uint(hostBits)) - 1
	return netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}), true
}

// LocalSubnet is an IPv4 network directly attached to a local interface.
type LocalSubnet struct {
	Interface string
	Prefix    netip.Prefix
	Addr      netip.Addr
}

// LocalSubnets lists IPv4 networks of up, non-loopback interfaces, skipping container
// bridges (docker0, br-*, veth*, cni*, flannel*) and link-local ranges.
func LocalSubnets() ([]LocalSubnet, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, err
	}
	var out []LocalSubnet
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := ifc.Name
		skip := false
		for _, pfx := range []string{"docker", "br-", "veth", "cni", "flannel", "virbr", "tailscale", "zt", "wg", "lxc"} {
			if strings.HasPrefix(name, pfx) {
				skip = true
			}
		}
		if skip {
			continue
		}
		addrs, err := ifc.Addrs()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			ipn, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip, ok := netip.AddrFromSlice(ipn.IP)
			if !ok {
				continue
			}
			ip = ip.Unmap()
			if !ip.Is4() || ip.IsLinkLocalUnicast() || ip.IsLoopback() {
				continue
			}
			ones, _ := ipn.Mask.Size()
			if ones < 16 || ones > 30 {
				continue
			}
			out = append(out, LocalSubnet{Interface: name, Prefix: netip.PrefixFrom(ip, ones).Masked(), Addr: ip})
		}
	}
	return out, nil
}

// InterfaceFor returns the local interface whose network contains addr ("" if none).
func InterfaceFor(addr netip.Addr) string {
	subs, err := LocalSubnets()
	if err != nil {
		return ""
	}
	for _, s := range subs {
		if s.Prefix.Contains(addr) {
			return s.Interface
		}
	}
	return ""
}

// ResolveHost returns the address of a host name (IPv4 preferred) or the literal IP.
func ResolveHost(ctx context.Context, host string) (string, error) {
	if a, err := netip.ParseAddr(host); err == nil {
		return a.Unmap().String(), nil
	}
	lctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	addrs, err := net.DefaultResolver.LookupNetIP(lctx, "ip", host)
	if err != nil {
		return "", fmt.Errorf("Hostname %s nicht auflösbar: %w", host, err)
	}
	for _, a := range addrs {
		if a.Unmap().Is4() {
			return a.Unmap().String(), nil
		}
	}
	if len(addrs) > 0 {
		return addrs[0].String(), nil
	}
	return "", fmt.Errorf("Hostname %s nicht auflösbar", host)
}
