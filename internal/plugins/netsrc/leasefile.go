package netsrc

import (
	"bufio"
	"encoding/csv"
	"io"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Parsers for lease files and ARP output that routers keep on disk (read over SSH).

// ParseISCLeases reads an ISC dhcpd lease file (dhcpd.leases). The file is append-only:
// the last block of an address wins. Only active leases that have not ended by now are
// returned.
func ParseISCLeases(r io.Reader, now time.Time) []Client {
	type lease struct {
		c      Client
		active bool
		never  bool
	}
	byIP := map[string]*lease{}
	var order []string
	var cur *lease
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "lease ") && strings.HasSuffix(line, "{"):
			ip := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(line, "lease "), "{"))
			cur = &lease{c: Client{IP: ip, Kind: KindDHCP}}
		case line == "}":
			if cur != nil {
				if _, ok := byIP[cur.c.IP]; !ok {
					order = append(order, cur.c.IP)
				}
				byIP[cur.c.IP] = cur
			}
			cur = nil
		case cur == nil:
		case strings.HasPrefix(line, "binding state "):
			cur.active = strings.TrimSuffix(strings.TrimPrefix(line, "binding state "), ";") == "active"
		case strings.HasPrefix(line, "hardware ethernet "):
			cur.c.MAC = strings.TrimSuffix(strings.TrimPrefix(line, "hardware ethernet "), ";")
		case strings.HasPrefix(line, "client-hostname "):
			cur.c.Hostname = unquote(strings.TrimSuffix(strings.TrimPrefix(line, "client-hostname "), ";"))
		case strings.HasPrefix(line, "ends "):
			v := strings.TrimSuffix(strings.TrimPrefix(line, "ends "), ";")
			if v == "never" {
				cur.never = true
			} else if t, ok := iscTime(v); ok {
				cur.c.Expires = t
			}
		case strings.HasPrefix(line, "cltt "):
			if t, ok := iscTime(strings.TrimSuffix(strings.TrimPrefix(line, "cltt "), ";")); ok {
				cur.c.LastSeen = t
			}
		}
	}
	var out []Client
	for _, ip := range order {
		l := byIP[ip]
		if !l.active || l.c.MAC == "" || (!l.never && !l.c.Expires.IsZero() && l.c.Expires.Before(now)) {
			continue
		}
		out = append(out, l.c)
	}
	return out
}

// iscTime parses "4 2026/09/24 10:00:00" (weekday, UTC) or "epoch 1790000000; # …".
func iscTime(v string) (time.Time, bool) {
	f := strings.Fields(v)
	if len(f) >= 2 && f[0] == "epoch" {
		n, err := strconv.ParseInt(f[1], 10, 64)
		return time.Unix(n, 0).UTC(), err == nil
	}
	if len(f) < 3 {
		return time.Time{}, false
	}
	t, err := time.Parse("2006/01/02 15:04:05", f[1]+" "+f[2])
	return t.UTC(), err == nil
}

func unquote(s string) string {
	s = strings.TrimSpace(s)
	if u, err := strconv.Unquote(s); err == nil {
		return u
	}
	return strings.Trim(s, `"`)
}

// ParseKeaLeases reads a Kea memfile (CSV with a header: address,hwaddr,…,expire,…,
// hostname,state,…). Later lines of an address replace earlier ones; released
// (valid_lifetime 0), declined and expired leases are left out.
func ParseKeaLeases(r io.Reader, now time.Time) []Client {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = true
	header, err := cr.Read()
	if err != nil {
		return nil
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.TrimSpace(h)] = i
	}
	get := func(rec []string, name string) string {
		if i, ok := col[name]; ok && i < len(rec) {
			return strings.TrimSpace(rec[i])
		}
		return ""
	}
	type lease struct {
		c  Client
		ok bool
	}
	byIP := map[string]*lease{}
	var order []string
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			continue
		}
		ip := get(rec, "address")
		if ip == "" {
			continue
		}
		l := &lease{c: Client{IP: ip, MAC: get(rec, "hwaddr"), Hostname: keaUnescape(get(rec, "hostname")), Kind: KindDHCP}}
		life, _ := strconv.ParseInt(get(rec, "valid_lifetime"), 10, 64)
		exp, _ := strconv.ParseInt(get(rec, "expire"), 10, 64)
		state := get(rec, "state")
		if exp > 0 {
			l.c.Expires = time.Unix(exp, 0).UTC()
		}
		l.ok = life > 0 && (state == "" || state == "0") && (exp == 0 || l.c.Expires.After(now))
		if _, seen := byIP[ip]; !seen {
			order = append(order, ip)
		}
		byIP[ip] = l
	}
	var out []Client
	for _, ip := range order {
		if l := byIP[ip]; l.ok && l.c.MAC != "" {
			out = append(out, l.c)
		}
	}
	return out
}

// keaUnescape reverts Kea's CSV escaping of commas (&#x2c).
func keaUnescape(s string) string {
	return strings.TrimSuffix(strings.ReplaceAll(s, "&#x2c", ","), ".")
}

// arpBSD matches a line of FreeBSD's "arp -an":
// ? (192.168.1.10) at 00:11:22:33:44:55 on em1 expires in 1178 seconds [ethernet]
var arpBSD = regexp.MustCompile(`^\S+ \(([0-9a-fA-F.:]+)\) at ([0-9a-fA-F:]{11,17}) on (\S+)(?: (permanent|expires in (\d+) seconds))?`)

// ParseARPBSD reads the output of "arp -an" on FreeBSD (pfSense, OPNsense). Incomplete
// entries are skipped; permanent ones are the firewall's own addresses and left out.
func ParseARPBSD(r io.Reader, now time.Time) []Client {
	var out []Client
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		m := arpBSD.FindStringSubmatch(strings.TrimSpace(sc.Text()))
		if m == nil || m[4] == "permanent" {
			continue
		}
		c := Client{IP: m[1], MAC: m[2], Interface: m[3], Kind: KindARP, LastSeen: now}
		out = append(out, c)
	}
	return out
}
