//go:build linux

package tunnel

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"syscall"
	"time"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// defaultMTU leaves room for the WireGuard overhead on a 1500 byte path.
const defaultMTU = 1420

type linuxNet struct {
	wg *wgctrl.Client
}

func newNetOps() (netOps, error) {
	c, err := wgctrl.New()
	if err != nil {
		return nil, fmt.Errorf("WireGuard-Steuerung nicht verfügbar: %w", err)
	}
	return &linuxNet{wg: c}, nil
}

func (n *linuxNet) Close() error { return n.wg.Close() }

// explain turns netlink errors into actionable messages.
func explain(err error) error {
	switch {
	case errors.Is(err, syscall.EPERM), errors.Is(err, syscall.EACCES):
		return fmt.Errorf("keine Berechtigung, Netzwerk-Interfaces anzulegen – der Container braucht NET_ADMIN (cap_add) und Host-Netzwerk (%w)", err)
	case errors.Is(err, syscall.EOPNOTSUPP), errors.Is(err, syscall.ENODEV), strings.Contains(err.Error(), "not supported"):
		return fmt.Errorf("der Kernel des Hosts unterstützt kein WireGuard (enthalten ab Linux 5.6) (%w)", err)
	}
	return err
}

func wgLink(name string) *netlink.Wireguard {
	la := netlink.NewLinkAttrs()
	la.Name = name
	return &netlink.Wireguard{LinkAttrs: la}
}

func (n *linuxNet) Probe() error {
	_ = n.Remove(probeIface)
	if err := netlink.LinkAdd(wgLink(probeIface)); err != nil {
		return explain(err)
	}
	return n.Remove(probeIface)
}

func ipNet(p netip.Prefix) *net.IPNet {
	return &net.IPNet{IP: p.Addr().AsSlice(), Mask: net.CIDRMask(p.Bits(), p.Addr().BitLen())}
}

func prefixOf(n *net.IPNet) (netip.Prefix, bool) {
	if n == nil {
		return netip.Prefix{}, false
	}
	a, ok := netip.AddrFromSlice(n.IP)
	if !ok {
		return netip.Prefix{}, false
	}
	ones, _ := n.Mask.Size()
	return netip.PrefixFrom(a.Unmap(), ones), true
}

func (n *linuxNet) Apply(name string, s spec) error {
	cfg := s.Config
	link, err := netlink.LinkByName(name)
	if err != nil {
		var nf netlink.LinkNotFoundError
		if !errors.As(err, &nf) {
			return err
		}
		if err := netlink.LinkAdd(wgLink(name)); err != nil {
			return explain(err)
		}
		if link, err = netlink.LinkByName(name); err != nil {
			return err
		}
	} else if link.Type() != "wireguard" {
		return fmt.Errorf("Interface %s existiert bereits und ist kein WireGuard-Interface", name)
	}
	mtu := cfg.MTU
	if mtu == 0 {
		mtu = defaultMTU
	}
	if link.Attrs().MTU != mtu {
		if err := netlink.LinkSetMTU(link, mtu); err != nil {
			return fmt.Errorf("MTU setzen: %w", err)
		}
	}

	ep, err := net.ResolveUDPAddr("udp", cfg.Peer.Endpoint)
	if err != nil {
		return fmt.Errorf("Endpoint %s nicht auflösbar: %w", cfg.Peer.Endpoint, err)
	}
	if a, ok := netip.AddrFromSlice(ep.IP); ok {
		for _, r := range s.Routes {
			if r.Contains(a.Unmap()) {
				return fmt.Errorf("der Endpoint %s liegt im Subnetz %s, das durch den Tunnel geleitet werden soll", ep.IP, r)
			}
		}
	}
	keepalive := time.Duration(cfg.Keepalive()) * time.Second
	allowed := make([]net.IPNet, 0, len(s.Routes))
	for _, r := range s.Routes {
		allowed = append(allowed, *ipNet(r))
	}
	var port *int
	if cfg.ListenPort > 0 {
		port = &cfg.ListenPort
	}
	priv := cfg.PrivateKey
	if err := n.wg.ConfigureDevice(name, wgtypes.Config{
		PrivateKey:   &priv,
		ListenPort:   port,
		ReplacePeers: true,
		Peers: []wgtypes.PeerConfig{{
			PublicKey:                   cfg.Peer.PublicKey,
			PresharedKey:                cfg.Peer.PresharedKey,
			Endpoint:                    ep,
			PersistentKeepaliveInterval: &keepalive,
			ReplaceAllowedIPs:           true,
			AllowedIPs:                  allowed,
		}},
	}); err != nil {
		return fmt.Errorf("WireGuard konfigurieren: %w", err)
	}

	if s.Addresses {
		if err := syncAddrs(link, cfg.Addresses); err != nil {
			return err
		}
	}
	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("Interface aktivieren: %w", err)
	}
	return syncRoutes(link, s.Routes)
}

// syncAddrs sets the tunnel addresses as host addresses (/32, /128), so no route is
// created for the tunnel network itself.
func syncAddrs(link netlink.Link, addrs []netip.Prefix) error {
	want := map[netip.Prefix]bool{}
	for _, a := range addrs {
		want[netip.PrefixFrom(a.Addr(), a.Addr().BitLen())] = true
	}
	have, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		return err
	}
	for _, h := range have {
		p, ok := prefixOf(h.IPNet)
		if !ok || p.Addr().IsLinkLocalUnicast() {
			continue
		}
		if want[p] {
			delete(want, p)
			continue
		}
		if err := netlink.AddrDel(link, &h); err != nil {
			return fmt.Errorf("Adresse %s entfernen: %w", p, err)
		}
	}
	for p := range want {
		if err := netlink.AddrAdd(link, &netlink.Addr{IPNet: ipNet(p)}); err != nil {
			return fmt.Errorf("Adresse %s setzen: %w", p, err)
		}
	}
	return nil
}

// syncRoutes routes exactly the given prefixes through the link. A prefix that already
// has a route through another interface is not taken over.
func syncRoutes(link netlink.Link, routes []netip.Prefix) error {
	idx := link.Attrs().Index
	for _, r := range routes {
		family := netlink.FAMILY_V4
		if r.Addr().Is6() {
			family = netlink.FAMILY_V6
		}
		existing, err := netlink.RouteListFiltered(family, &netlink.Route{Dst: ipNet(r)}, netlink.RT_FILTER_DST)
		if err != nil {
			return err
		}
		for _, e := range existing {
			if e.LinkIndex != idx && e.LinkIndex > 0 {
				other := fmt.Sprintf("Interface %d", e.LinkIndex)
				if l, err := netlink.LinkByIndex(e.LinkIndex); err == nil {
					other = l.Attrs().Name
				}
				return fmt.Errorf("für %s gibt es auf dem Host schon eine Route über %s – das Netz ist bereits anders erreichbar", r, other)
			}
		}
		if err := netlink.RouteReplace(&netlink.Route{LinkIndex: idx, Dst: ipNet(r), Scope: netlink.SCOPE_LINK}); err != nil {
			return fmt.Errorf("Route %s setzen: %w", r, err)
		}
	}
	current, err := netlink.RouteList(link, netlink.FAMILY_ALL)
	if err != nil {
		return err
	}
	for _, c := range current {
		p, ok := prefixOf(c.Dst)
		if !ok || c.Protocol == unix.RTPROT_KERNEL || p.Addr().IsLinkLocalUnicast() || p.Addr().IsMulticast() {
			continue
		}
		keep := false
		for _, r := range routes {
			if r == p {
				keep = true
			}
		}
		if !keep {
			if err := netlink.RouteDel(&c); err != nil {
				return fmt.Errorf("Route %s entfernen: %w", p, err)
			}
		}
	}
	return nil
}

func (n *linuxNet) Remove(name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		var nf netlink.LinkNotFoundError
		if errors.As(err, &nf) {
			return nil
		}
		return err
	}
	return netlink.LinkDel(link)
}

func (n *linuxNet) Links() ([]string, error) {
	list, err := netlink.LinkList()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, l := range list {
		name := l.Attrs().Name
		if strings.HasPrefix(name, ifPrefix) && name != probeIface && name != testIface && l.Type() == "wireguard" {
			out = append(out, name)
		}
	}
	return out, nil
}

func (n *linuxNet) Peer(name string) (peerStats, error) {
	d, err := n.wg.Device(name)
	if err != nil {
		return peerStats{}, err
	}
	if len(d.Peers) == 0 {
		return peerStats{}, nil
	}
	p := d.Peers[0]
	return peerStats{LastHandshake: p.LastHandshakeTime, Rx: p.ReceiveBytes, Tx: p.TransmitBytes}, nil
}
