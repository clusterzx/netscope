package mdns

import (
	"errors"
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strings"
	"sync"

	"golang.org/x/net/dns/dnsmessage"
)

// Limits that keep a hostile or chatty network from exhausting memory.
const (
	maxPacket      = 9000 // largest mDNS message (RFC 6762, section 17)
	maxPackets     = 5000 // packets processed per session
	maxEntries     = 4096 // entries per record map
	maxTXTItems    = 64   // key/value pairs kept per TXT record
	servicesDomain = "_services._dns-sd._udp.local."
)

type srvRecord struct {
	target string // lower-case host name
	port   uint16
}

// collector accumulates the records of all responses of one session. Names are keyed in
// lower case (DNS names are case-insensitive), display keeps the original spelling.
type collector struct {
	mu      sync.Mutex
	packets int
	sources map[netip.Addr]bool     // addresses that sent responses
	types   map[string]bool         // service types from the DNS-SD enumeration
	ptr     map[string][]string     // service type → instance names
	srv     map[string]srvRecord    // instance → SRV
	txt     map[string][]string     // instance → TXT strings
	addrs   map[string][]netip.Addr // host name → IPv4 addresses
	reverse map[netip.Addr]string   // address → name from a reverse PTR record
	origin  map[string]netip.Addr   // owner name → source of the first packet carrying it
	display map[string]string       // lower-case name → original spelling
}

func newCollector() *collector {
	return &collector{
		sources: map[netip.Addr]bool{},
		types:   map[string]bool{},
		ptr:     map[string][]string{},
		srv:     map[string]srvRecord{},
		txt:     map[string][]string{},
		addrs:   map[string][]netip.Addr{},
		reverse: map[netip.Addr]string{},
		origin:  map[string]netip.Addr{},
		display: map[string]string{},
	}
}

func (c *collector) name(n dnsmessage.Name) string {
	s := n.String()
	k := strings.ToLower(s)
	if _, ok := c.display[k]; !ok && len(c.display) < maxEntries*4 {
		c.display[k] = s
	}
	return k
}

func (c *collector) seenAt(owner string, src netip.Addr) {
	if _, ok := c.origin[owner]; !ok && len(c.origin) < maxEntries {
		c.origin[owner] = src
	}
}

// add parses one packet received from src. Queries and malformed packets are ignored;
// records parsed before a malformed record are kept.
func (c *collector) add(src netip.Addr, pkt []byte) {
	if len(pkt) > maxPacket {
		return
	}
	var p dnsmessage.Parser
	h, err := p.Start(pkt)
	if err != nil || !h.Response || h.OpCode != 0 || h.RCode != dnsmessage.RCodeSuccess {
		return
	}
	if err := p.SkipAllQuestions(); err != nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.packets >= maxPackets {
		return
	}
	c.packets++
	sections := []struct {
		header func() (dnsmessage.ResourceHeader, error)
		skip   func() error
	}{
		{p.AnswerHeader, p.SkipAnswer},
		{p.AuthorityHeader, p.SkipAuthority},
		{p.AdditionalHeader, p.SkipAdditional},
	}
	records := 0
	defer func() {
		if records > 0 {
			c.sources[src] = true
		}
	}()
	for _, sec := range sections {
		for {
			rh, err := sec.header()
			if errors.Is(err, dnsmessage.ErrSectionDone) {
				break
			}
			if err != nil {
				return
			}
			if rh.Class&0x7FFF != dnsmessage.ClassINET || rh.TTL == 0 { // TTL 0: goodbye
				if sec.skip() != nil {
					return
				}
				continue
			}
			if err := c.record(&p, rh, src, sec.skip); err != nil {
				return
			}
			records++
		}
	}
}

// record stores one resource record.
func (c *collector) record(p *dnsmessage.Parser, rh dnsmessage.ResourceHeader, src netip.Addr, skip func() error) error {
	owner := c.name(rh.Name)
	switch rh.Type {
	case dnsmessage.TypePTR:
		r, err := p.PTRResource()
		if err != nil {
			return err
		}
		target := c.name(r.PTR)
		switch {
		case owner == servicesDomain:
			if len(c.types) < maxEntries {
				c.types[target] = true
			}
		case strings.HasSuffix(owner, ".in-addr.arpa."):
			if ip, ok := parseReverse(owner); ok {
				if _, exists := c.reverse[ip]; !exists && len(c.reverse) < maxEntries {
					c.reverse[ip] = trimDot(c.display[target])
				}
			}
		default:
			if len(c.ptr) < maxEntries && len(c.ptr[owner]) < maxEntries && !contains(c.ptr[owner], target) {
				c.ptr[owner] = append(c.ptr[owner], target)
			}
			c.seenAt(target, src)
		}
	case dnsmessage.TypeSRV:
		r, err := p.SRVResource()
		if err != nil {
			return err
		}
		if len(c.srv) < maxEntries {
			c.srv[owner] = srvRecord{target: c.name(r.Target), port: r.Port}
		}
		c.seenAt(owner, src)
	case dnsmessage.TypeTXT:
		r, err := p.TXTResource()
		if err != nil {
			return err
		}
		if len(c.txt) < maxEntries {
			c.txt[owner] = r.TXT
		}
		c.seenAt(owner, src)
	case dnsmessage.TypeA:
		r, err := p.AResource()
		if err != nil {
			return err
		}
		a := netip.AddrFrom4(r.A)
		if len(c.addrs) < maxEntries && !containsAddr(c.addrs[owner], a) {
			c.addrs[owner] = append(c.addrs[owner], a)
		}
	default:
		return skip()
	}
	return nil
}

// serviceTypes returns the service types learned from the enumeration (without
// sub-types), sorted.
func (c *collector) serviceTypes() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for t := range c.types {
		if !strings.Contains(t, "._sub.") {
			out = append(out, c.display[t])
		}
	}
	sort.Strings(out)
	return out
}

// responders returns the addresses that sent responses so far.
func (c *collector) responders() []netip.Addr {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]netip.Addr, 0, len(c.sources))
	for a := range c.sources {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Less(out[j]) })
	return out
}

// Service is one DNS-SD service instance of a host.
type Service struct {
	Type     string            `json:"type"`
	Instance string            `json:"instance"`
	Port     int               `json:"port,omitempty"`
	Target   string            `json:"target,omitempty"`
	TXT      map[string]string `json:"txt,omitempty"`
}

// host is the aggregated view of one IPv4 address.
type host struct {
	IP       netip.Addr
	Present  bool      // the address itself sent responses
	Reverse  string    // name from the reverse PTR record
	ANames   []string  // names with an A record pointing here
	SRVNames []string  // SRV targets without A record
	Services []Service // sorted by type and instance
}

// hosts aggregates the collected records per IPv4 address inside scope.
func (c *collector) hosts(scope func(netip.Addr) bool) []*host {
	c.mu.Lock()
	defer c.mu.Unlock()
	byIP := map[netip.Addr]*host{}
	get := func(a netip.Addr) *host {
		h := byIP[a]
		if h == nil {
			h = &host{IP: a}
			byIP[a] = h
		}
		return h
	}
	for name, addrs := range c.addrs {
		for _, a := range addrs {
			if scope(a) {
				h := get(a)
				h.ANames = appendUnique(h.ANames, trimDot(c.display[name]))
			}
		}
	}
	for a, name := range c.reverse {
		if scope(a) && name != "" {
			get(a).Reverse = name
		}
	}
	instances := map[string]bool{}
	for _, list := range c.ptr {
		for _, inst := range list {
			instances[inst] = true
		}
	}
	for inst := range c.srv {
		instances[inst] = true
	}
	for inst := range c.txt {
		instances[inst] = true
	}
	for inst := range instances {
		typ, label, ok := splitInstance(c.display[inst])
		if !ok {
			continue
		}
		svc := Service{Type: typ, Instance: label, TXT: parseTXT(c.txt[inst])}
		var ip netip.Addr
		srv, hasSRV := c.srv[inst]
		if hasSRV {
			svc.Port = int(srv.port)
			svc.Target = trimDot(c.display[srv.target])
			for _, a := range c.addrs[srv.target] {
				if scope(a) {
					ip = a
					break
				}
			}
		}
		if !ip.IsValid() {
			if src, ok := c.origin[inst]; ok && scope(src) {
				ip = src
			}
		}
		if !ip.IsValid() {
			continue
		}
		h := get(ip)
		h.Services = append(h.Services, svc)
		if hasSRV && len(c.addrs[srv.target]) == 0 && svc.Target != "" {
			h.SRVNames = appendUnique(h.SRVNames, svc.Target)
		}
	}
	for a := range c.sources {
		if scope(a) {
			get(a).Present = true
		}
	}
	out := make([]*host, 0, len(byIP))
	for _, h := range byIP {
		sort.Strings(h.ANames)
		sort.Strings(h.SRVNames)
		sort.Slice(h.Services, func(i, j int) bool {
			if h.Services[i].Type != h.Services[j].Type {
				return h.Services[i].Type < h.Services[j].Type
			}
			return h.Services[i].Instance < h.Services[j].Instance
		})
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP.Less(out[j].IP) })
	return out
}

// splitInstance splits "<instance>.<_service>.<_proto>.<domain>." into the service type
// ("_http._tcp") and the instance label.
func splitInstance(name string) (typ, instance string, ok bool) {
	labels := strings.Split(trimDot(name), ".")
	for i := len(labels) - 1; i >= 2; i-- {
		proto := strings.ToLower(labels[i])
		if (proto == "_tcp" || proto == "_udp") && strings.HasPrefix(labels[i-1], "_") {
			return strings.ToLower(labels[i-1]) + "." + proto, strings.Join(labels[:i-1], "."), true
		}
	}
	return "", "", false
}

// parseTXT converts DNS-SD key/value strings to a map (RFC 6763, section 6).
func parseTXT(items []string) map[string]string {
	var out map[string]string
	for _, it := range items {
		if it == "" {
			continue
		}
		if out == nil {
			out = map[string]string{}
		}
		if len(out) >= maxTXTItems {
			break
		}
		k, v, _ := strings.Cut(it, "=")
		if k == "" {
			continue
		}
		if _, dup := out[k]; !dup { // the first occurrence wins
			out[k] = v
		}
	}
	return out
}

// parseReverse turns "d.c.b.a.in-addr.arpa." into the address a.b.c.d.
func parseReverse(name string) (netip.Addr, bool) {
	parts := strings.Split(strings.TrimSuffix(name, ".in-addr.arpa."), ".")
	if len(parts) != 4 {
		return netip.Addr{}, false
	}
	a, err := netip.ParseAddr(parts[3] + "." + parts[2] + "." + parts[1] + "." + parts[0])
	if err != nil || !a.Is4() {
		return netip.Addr{}, false
	}
	return a, true
}

// reverseName returns the in-addr.arpa name of an IPv4 address.
func reverseName(a netip.Addr) string {
	b := a.As4()
	return fmt.Sprintf("%d.%d.%d.%d.in-addr.arpa.", b[3], b[2], b[1], b[0])
}

var hexLabel = regexp.MustCompile(`^[0-9A-Fa-f]{12,}$`)

// junk host names carry no information.
var junkNames = map[string]bool{"localhost": true, "none": true, "(none)": true, "linux": true, "android": true, "unknown": true}

// hostname picks the most meaningful name: the reverse PTR answer, then names with an
// A record, then SRV targets. Names derived from the address, generic names and random
// hex identifiers (except from the reverse answer) are skipped.
func (h *host) hostname() string {
	dashed := strings.ReplaceAll(h.IP.String(), ".", "-")
	best, bestScore := "", -1
	consider := func(name string, score int) {
		if name == "" {
			return
		}
		first := strings.ToLower(strings.SplitN(name, ".", 2)[0])
		if junkNames[first] || strings.Contains(first, dashed) || strings.HasSuffix(strings.ToLower(name), ".arpa") {
			return
		}
		if hexLabel.MatchString(first) {
			if score < 3 {
				return
			}
			score = 1
		}
		if score > bestScore || (score == bestScore && strings.ToLower(name) < strings.ToLower(best)) {
			best, bestScore = name, score
		}
	}
	consider(h.Reverse, 3)
	for _, n := range h.ANames {
		consider(n, 2)
	}
	for _, n := range h.SRVNames {
		consider(n, 1)
	}
	return best
}

// names returns all names of the host, sorted.
func (h *host) names() []string {
	var out []string
	for _, n := range append(append([]string{h.Reverse}, h.ANames...), h.SRVNames...) {
		if n != "" {
			out = appendUnique(out, n)
		}
	}
	sort.Strings(out)
	return out
}

// serviceTypes returns the sorted, unique service types of the host.
func (h *host) serviceTypes() []string {
	var out []string
	for _, s := range h.Services {
		out = appendUnique(out, s.Type)
	}
	sort.Strings(out)
	return out
}

func trimDot(s string) string { return strings.TrimSuffix(s, ".") }

func contains(list []string, s string) bool {
	for _, it := range list {
		if it == s {
			return true
		}
	}
	return false
}

func containsAddr(list []netip.Addr, a netip.Addr) bool {
	for _, it := range list {
		if it == a {
			return true
		}
	}
	return false
}

func appendUnique(list []string, s string) []string {
	if contains(list, s) {
		return list
	}
	return append(list, s)
}

// packetCount returns the number of processed response packets.
func (c *collector) packetCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.packets
}
