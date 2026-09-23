package netutil

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"io"
	"net/netip"
	"os"
	"strings"
)

// ParseRouteTable parses the Linux /proc/net/route format and returns the default
// gateway per interface (routes with destination 0.0.0.0 and the gateway flag).
func ParseRouteTable(r io.Reader) map[string]netip.Addr {
	out := map[string]netip.Addr{}
	sc := bufio.NewScanner(r)
	first := true
	for sc.Scan() {
		if first { // header
			first = false
			continue
		}
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[1] != "00000000" {
			continue
		}
		flags, err := hex.DecodeString(padHex(f[3]))
		if err != nil || len(flags) < 2 || flags[len(flags)-1]&0x02 == 0 { // RTF_GATEWAY
			continue
		}
		gw, err := hex.DecodeString(f[2])
		if err != nil || len(gw) != 4 {
			continue
		}
		// the kernel prints the address in host byte order (little endian on x86/arm)
		v := binary.LittleEndian.Uint32(gw)
		a := netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
		if _, exists := out[f[0]]; !exists {
			out[f[0]] = a
		}
	}
	return out
}

func padHex(s string) string {
	if len(s)%2 == 1 {
		return "0" + s
	}
	return s
}

// DefaultGateways returns the IPv4 default gateway per interface of this host (Linux;
// other systems return an empty map).
func DefaultGateways() map[string]netip.Addr {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return map[string]netip.Addr{}
	}
	defer f.Close()
	return ParseRouteTable(f)
}
