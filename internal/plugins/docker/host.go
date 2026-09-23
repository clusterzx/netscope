package docker

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"strings"
	"time"

	"netscope/internal/netutil"
)

// procRoute is the Linux IPv4 routing table.
var procRoute = "/proc/net/route"

// localIPv4 determines the address of the local host (replaced in tests).
var localIPv4 = primaryIPv4

// defaultRouteIface returns the interface of the IPv4 default route with the lowest
// metric from /proc/net/route content.
func defaultRouteIface(data []byte) string {
	best, bestMetric := "", -1
	sc := bufio.NewScanner(bytes.NewReader(data))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		// Iface Destination Gateway Flags RefCnt Use Metric Mask MTU Window IRTT
		if len(f) < 8 || f[0] == "Iface" || f[1] != "00000000" || f[7] != "00000000" {
			continue
		}
		flags, err := strconv.ParseUint(f[3], 16, 32)
		if err != nil || flags&0x1 == 0 { // RTF_UP
			continue
		}
		metric, err := strconv.Atoi(f[6])
		if err != nil {
			continue
		}
		if bestMetric < 0 || metric < bestMetric {
			best, bestMetric = f[0], metric
		}
	}
	return best
}

// ifaceIPv4 returns the first global IPv4 address of an interface.
func ifaceIPv4(name string) string {
	ifc, err := net.InterfaceByName(name)
	if err != nil {
		return ""
	}
	addrs, err := ifc.Addrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		ipn, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		if ip, ok := netip.AddrFromSlice(ipn.IP); ok {
			ip = ip.Unmap()
			if ip.Is4() && !ip.IsLoopback() && !ip.IsLinkLocalUnicast() {
				return ip.String()
			}
		}
	}
	return ""
}

// primaryIPv4 returns the IPv4 address of the interface holding the default route. It
// reads /proc/net/route (Linux), then asks the kernel which source address it would use
// for an outside destination (no packet is sent), then falls back to the first local subnet.
func primaryIPv4() (string, error) {
	if data, err := os.ReadFile(procRoute); err == nil {
		if ifc := defaultRouteIface(data); ifc != "" {
			if ip := ifaceIPv4(ifc); ip != "" {
				return ip, nil
			}
		}
	}
	if conn, err := net.Dial("udp4", "192.0.2.1:9"); err == nil {
		addr, _ := conn.LocalAddr().(*net.UDPAddr)
		conn.Close()
		if addr != nil {
			if ip, ok := netip.AddrFromSlice(addr.IP); ok && ip.Unmap().Is4() && !ip.IsLoopback() && !ip.IsUnspecified() {
				return ip.Unmap().String(), nil
			}
		}
	}
	if subs, err := netutil.LocalSubnets(); err == nil && len(subs) > 0 {
		return subs[0].Addr.String(), nil
	}
	return "", errors.New("keine lokale IPv4-Adresse gefunden")
}

// resolveHost returns the address of a host name (IPv4 preferred) or the literal IP.
func resolveHost(ctx context.Context, host string) (string, error) {
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
