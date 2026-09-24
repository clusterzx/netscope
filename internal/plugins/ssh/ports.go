package ssh

import (
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"

	"netscope/internal/plugin"
)

// portOverride is an explicit SSH port for an address or a network.
type portOverride struct {
	prefix netip.Prefix
	port   int
}

// parsePortOverrides parses entries "address=port" or "network/bits=port"; the most
// specific entry comes first.
func parsePortOverrides(list []string) ([]portOverride, error) {
	var out []portOverride
	for _, raw := range list {
		entry := strings.TrimSpace(raw)
		if entry == "" {
			continue
		}
		target, portStr, ok := strings.Cut(entry, "=")
		if !ok {
			return nil, fmt.Errorf("%q: Adresse=Port erwartet, z. B. 192.168.1.1=2222", entry)
		}
		port, err := strconv.Atoi(strings.TrimSpace(portStr))
		if err != nil || port < 1 || port > 65535 {
			return nil, fmt.Errorf("%q: Port 1–65535 erwartet", entry)
		}
		target = strings.TrimSpace(target)
		var p netip.Prefix
		if strings.Contains(target, "/") {
			if p, err = netip.ParsePrefix(target); err != nil {
				return nil, fmt.Errorf("%q: ungültiges Netz", entry)
			}
			p = p.Masked()
		} else {
			a, err := netip.ParseAddr(target)
			if err != nil {
				return nil, fmt.Errorf("%q: ungültige IP-Adresse", entry)
			}
			p = netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen())
		}
		out = append(out, portOverride{prefix: p, port: port})
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].prefix.Bits() > out[j].prefix.Bits() })
	return out, nil
}

// portPlan decides address and port per device.
type portPlan struct {
	port        int  // configured default port
	requireOpen bool // skip devices whose known open ports contain no SSH port
	detect      bool // use an SSH service nmap found on another port
	overrides   []portOverride
}

// override returns the explicit port of an address.
func (pp portPlan) override(ip string) (int, bool) {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return 0, false
	}
	a = a.Unmap()
	for _, o := range pp.overrides {
		if o.prefix.Contains(a) {
			return o.port, true
		}
	}
	return 0, false
}

// addresses lists the addresses of a device, primary first.
func addresses(d plugin.DeviceInfo) []string {
	var out []string
	seen := map[string]bool{}
	for _, ip := range append([]string{d.PrimaryIP}, d.IPs...) {
		if ip != "" && !seen[ip] {
			seen[ip] = true
			out = append(out, ip)
		}
	}
	return out
}

// openPort returns the address a known open TCP port matching fn is on (primary address
// preferred) and the port.
func openPort(d plugin.DeviceInfo, fn func(plugin.PortRef) bool) (string, int) {
	ip, port := "", 0
	for _, pr := range d.Ports {
		if pr.Proto != "tcp" || !fn(pr) {
			continue
		}
		better := ip == "" || (pr.IP == d.PrimaryIP && ip != d.PrimaryIP) || (pr.IP == ip && pr.Port < port)
		if better {
			ip, port = pr.IP, pr.Port
		}
	}
	return ip, port
}

func isSSHService(pr plugin.PortRef) bool {
	s := strings.ToLower(strings.TrimSuffix(pr.Service, "?"))
	return s == "ssh" || strings.Contains(strings.ToLower(pr.Product), "openssh") || strings.Contains(strings.ToLower(pr.Product), "dropbear")
}

// planTargets selects address and port per device: an explicit port of one of its
// addresses first, then the configured port if it is open, then an SSH service nmap
// found on another port; require_open skips devices whose known ports contain none.
func planTargets(devices []plugin.DeviceInfo, pp portPlan) (targets []target, skipped int) {
	for _, d := range devices {
		addrs := addresses(d)
		if len(addrs) == 0 {
			skipped++
			continue
		}
		explicit := false
		for _, a := range addrs {
			if port, ok := pp.override(a); ok {
				targets = append(targets, target{dev: d, ip: a, port: port})
				explicit = true
				break
			}
		}
		if explicit {
			continue
		}
		if len(d.Ports) == 0 {
			targets = append(targets, target{dev: d, ip: addrs[0], port: pp.port})
			continue
		}
		if ip, _ := openPort(d, func(pr plugin.PortRef) bool { return pr.Port == pp.port }); ip != "" {
			targets = append(targets, target{dev: d, ip: ip, port: pp.port})
			continue
		}
		if pp.detect {
			if ip, port := openPort(d, isSSHService); ip != "" {
				targets = append(targets, target{dev: d, ip: ip, port: port})
				continue
			}
		}
		if pp.requireOpen {
			skipped++
			continue
		}
		targets = append(targets, target{dev: d, ip: addrs[0], port: pp.port})
	}
	return targets, skipped
}
