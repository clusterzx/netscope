package arpscan

import (
	"net/netip"
	"regexp"
	"strconv"
	"strings"

	"netscope/internal/netutil"
)

// reply is one response line of arp-scan's default output format:
//
//	IP<TAB>MAC[ (HdrMAC)]<TAB>Vendor[ (802.2 LLC/SNAP)][ (802.1Q VLAN=n)][ (ARP Proto=0x..)][ (DUP: n)][<TAB>RTT=x ms]
type reply struct {
	IP     netip.Addr
	MAC    string // normalized ARP sender hardware address
	HdrMAC string // Ethernet source address, only if it differs from MAC (proxy ARP)
	Vendor string // "" when arp-scan does not know the vendor
	VLAN   int    // 802.1Q VLAN id, 0 if untagged
	Dup    int    // response number for duplicate responses (> 1), 0 otherwise
	Line   string // original output line
}

var (
	dupSuffix   = regexp.MustCompile(` \(DUP: (\d+)\)$`)
	protoSuffix = regexp.MustCompile(` \(ARP Proto=0x[0-9A-Fa-f]+\)$`)
	vlanSuffix  = regexp.MustCompile(` \(802\.1Q VLAN=(\d+)\)$`)
)

const llcSuffix = " (802.2 LLC/SNAP)"

// stripSuffixes removes the annotations arp-scan appends to the last column (in reverse
// order of how they are written) and returns the remaining text.
func stripSuffixes(s string, r *reply) string {
	if m := dupSuffix.FindStringSubmatch(s); m != nil {
		r.Dup, _ = strconv.Atoi(m[1])
		s = s[:len(s)-len(m[0])]
	}
	if m := protoSuffix.FindString(s); m != "" {
		s = s[:len(s)-len(m)]
	}
	if m := vlanSuffix.FindStringSubmatch(s); m != nil {
		r.VLAN, _ = strconv.Atoi(m[1])
		s = s[:len(s)-len(m[0])]
	}
	return strings.TrimSuffix(s, llcSuffix)
}

// parseLine parses one line of arp-scan output. Header, footer and warning lines are
// rejected (ok = false).
func parseLine(line string) (reply, bool) {
	line = strings.TrimRight(line, "\r\n")
	fields := strings.Split(line, "\t")
	if len(fields) < 2 {
		return reply{}, false
	}
	ip, err := netip.ParseAddr(strings.TrimSpace(fields[0]))
	if err != nil || !ip.Is4() {
		return reply{}, false
	}
	r := reply{IP: ip, Line: line}
	macField := fields[1]
	if len(fields) >= 3 {
		vendor := strings.TrimSpace(stripSuffixes(fields[2], &r))
		if vendor != "" && !strings.HasPrefix(vendor, "(Unknown") {
			r.Vendor = vendor
		}
	} else {
		// --quiet output: the annotations follow the MAC column
		macField = stripSuffixes(macField, &r)
	}
	macField = strings.TrimSpace(macField)
	hdr := ""
	if m, h, ok := strings.Cut(macField, " ("); ok {
		macField, hdr = m, strings.TrimSuffix(h, ")")
	}
	mac, ok := netutil.NormalizeMAC(macField)
	if !ok {
		return reply{}, false
	}
	r.MAC = mac
	if hdr != "" {
		if h, ok := netutil.NormalizeMAC(hdr); ok && h != mac {
			r.HdrMAC = h
		}
	}
	return r, true
}
