package openwrt

import (
	"bufio"
	"bytes"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/netutil"
)

// lease is an active DHCPv4 lease.
type lease struct {
	MAC      string
	IP       string
	Hostname string
	ClientID string // dnsmasq client identifier ("01:aa:bb:…", "ff:<iaid>:<duid>")
	DUID     string // LuCI: DUID derived from the client identifier
	Expires  time.Time
	Infinite bool
}

// staticHost is a static lease (uci section of type host).
type staticHost struct {
	Section   string
	Name      string
	MACs      []string
	IP        string
	DNS       bool
	LeaseTime string
}

// parseLeases parses a dnsmasq lease file ("expiry mac ip hostname clientid" per line;
// expiry 0 = infinite, hostname/clientid "*" = none). DHCPv6 lines and the "duid" line
// are skipped.
func parseLeases(data []byte) []lease {
	var out []lease
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 4 || f[0] == "duid" {
			continue
		}
		ts, err := strconv.ParseInt(f[0], 10, 64)
		if err != nil {
			continue
		}
		ip, err := netip.ParseAddr(f[2])
		if err != nil || !ip.Is4() {
			continue // DHCPv6 lease: second field is the IAID
		}
		mac, ok := netutil.NormalizeMAC(f[1])
		if !ok {
			continue
		}
		l := lease{MAC: mac, IP: ip.String()}
		if f[3] != "*" {
			l.Hostname = f[3]
		}
		if len(f) > 4 && f[4] != "*" {
			l.ClientID = f[4]
		}
		if ts == 0 {
			l.Infinite = true
		} else {
			l.Expires = time.Unix(ts, 0).UTC()
		}
		out = append(out, l)
	}
	return out
}

// uciSection is a section of a uci configuration.
type uciSection struct {
	Name    string // "lan", "cfg0bfe29" or "@host[0]" (uci show notation)
	Type    string
	Options map[string][]string
}

// parseUCIShow parses the output of "uci show <config>":
//
//	dhcp.@host[0]=host
//	dhcp.@host[0].mac='A8:A1:59:3C:7E:10' 'A8:A1:59:3C:7E:11'
//	dhcp.lan.start='100'
//
// Sections are returned in order of appearance.
func parseUCIShow(data []byte) []uciSection {
	var (
		out   []uciSection
		index = map[string]int{}
	)
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		parts := strings.SplitN(key, ".", 3)
		switch len(parts) {
		case 2: // config.section=type
			if _, seen := index[parts[1]]; !seen {
				index[parts[1]] = len(out)
				out = append(out, uciSection{Name: parts[1], Options: map[string][]string{}})
			}
			vals := uciValues(value)
			if len(vals) > 0 {
				out[index[parts[1]]].Type = vals[0]
			}
		case 3: // config.section.option=value
			i, seen := index[parts[1]]
			if !seen {
				i = len(out)
				index[parts[1]] = i
				out = append(out, uciSection{Name: parts[1], Options: map[string][]string{}})
			}
			out[i].Options[parts[2]] = uciValues(value)
		}
	}
	return out
}

// uciValues splits a uci show value into its elements. Values are single-quoted, lists
// are several quoted values separated by spaces and an embedded quote is escaped by
// closing and reopening the quoting:
//
//	dhcp.@host[3].name='Bob'\''s iPad'
func uciValues(s string) []string {
	var (
		out     []string
		cur     strings.Builder
		inQuote bool
		have    bool
	)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQuote:
			if c == '\'' {
				inQuote = false
			} else {
				cur.WriteByte(c)
			}
		case c == '\'':
			inQuote, have = true, true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			have = true
		case c == ' ' || c == '\t':
			if have {
				out = append(out, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteByte(c)
			have = true
		}
	}
	if have {
		out = append(out, cur.String())
	}
	return out
}

// first returns the first value of an option.
func (s uciSection) first(opt string) string {
	if v := s.Options[opt]; len(v) > 0 {
		return strings.TrimSpace(v[0])
	}
	return ""
}

// staticHosts extracts static leases from dhcp sections. The mac option may be a list or
// a single value holding several MACs separated by spaces.
func staticHosts(sections []uciSection) []staticHost {
	var out []staticHost
	for _, s := range sections {
		if s.Type != "host" {
			continue
		}
		h := staticHost{Section: s.Name, Name: s.first("name"), DNS: s.first("dns") == "1", LeaseTime: s.first("leasetime")}
		seen := map[string]bool{}
		for _, v := range s.Options["mac"] {
			for _, m := range strings.Fields(v) {
				if mac, ok := netutil.NormalizeMAC(m); ok && !seen[mac] {
					seen[mac] = true
					h.MACs = append(h.MACs, mac)
				}
			}
		}
		if ip, err := netip.ParseAddr(s.first("ip")); err == nil && ip.Is4() {
			h.IP = ip.String()
		}
		if len(h.MACs) == 0 {
			continue
		}
		out = append(out, h)
	}
	return out
}

// leaseFiles returns the lease files of all dnsmasq instances (default /tmp/dhcp.leases).
func leaseFiles(sections []uciSection) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range sections {
		if s.Type != "dnsmasq" {
			continue
		}
		if f := s.first("leasefile"); f != "" && !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	if len(out) == 0 {
		out = []string{"/tmp/dhcp.leases"}
	}
	return out
}

// entry is what the router knows about one client: an active lease, a static lease or both.
type entry struct {
	MACs   []string
	Static *staticHost
	Lease  *lease
}

// hostname prefers the configured static name over the name the client sent.
func (e *entry) hostname() string {
	if e.Static != nil && e.Static.Name != "" {
		return e.Static.Name
	}
	if e.Lease != nil {
		return e.Lease.Hostname
	}
	return ""
}

// ip prefers the address of the active lease.
func (e *entry) ip() string {
	if e.Lease != nil {
		return e.Lease.IP
	}
	if e.Static != nil {
		return e.Static.IP
	}
	return ""
}

// mergeEntries joins leases and static hosts by MAC. With includeStatic=false static
// hosts only mark leases as static but are not imported on their own.
func mergeEntries(leases []lease, hosts []staticHost, includeStatic bool) []*entry {
	var out []*entry
	byMAC := map[string]*entry{}
	for i := range hosts {
		h := &hosts[i]
		e := &entry{MACs: h.MACs, Static: h}
		for _, m := range h.MACs {
			if _, dup := byMAC[m]; !dup {
				byMAC[m] = e
			}
		}
		if includeStatic {
			out = append(out, e)
		}
	}
	for i := range leases {
		l := &leases[i]
		if e, ok := byMAC[l.MAC]; ok {
			if e.Lease == nil {
				e.Lease = l
				if !includeStatic {
					out = append(out, e)
				}
				continue
			}
			if e.Lease.MAC == l.MAC {
				continue // duplicate lease of the same MAC (several lease files)
			}
			// a second MAC of the same static host holds its own lease
			out = append(out, &entry{MACs: []string{l.MAC}, Static: e.Static, Lease: l})
			continue
		}
		e := &entry{MACs: []string{l.MAC}, Lease: l}
		byMAC[l.MAC] = e
		out = append(out, e)
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, errA := netip.ParseAddr(out[i].ip())
		b, errB := netip.ParseAddr(out[j].ip())
		if errA != nil || errB != nil {
			return errA == nil
		}
		return a.Less(b)
	})
	return out
}
