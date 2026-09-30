package netutil

import (
	"bufio"
	"io"
	"net/netip"
	"os"
	"strings"
)

// SystemDNS returns the name servers of the system (/etc/resolv.conf).
func SystemDNS() []string {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return nil
	}
	defer f.Close()
	return ParseResolvConf(f)
}

// ParseResolvConf returns the nameserver addresses of a resolv.conf.
func ParseResolvConf(r io.Reader) []string {
	var out []string
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		// fe80::1%eth0: the zone stays with the address
		if a, err := netip.ParseAddr(fields[1]); err == nil {
			out = append(out, a.String())
		}
	}
	return out
}
