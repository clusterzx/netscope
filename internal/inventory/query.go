package inventory

import (
	"fmt"
	"net/netip"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"

	"netscope/internal/netutil"
)

// QueryField documents a filter field for the UI help.
type QueryField struct {
	Field       string `json:"field"`
	Ops         string `json:"ops"`
	Description string `json:"description"`
	Example     string `json:"example"`
}

// QueryFields lists the fields of the filter query language.
func QueryFields() []QueryField {
	return []QueryField{
		{"(Text)", "", "Freitext in Name, Hostname, IP, MAC, Hersteller, Modell, OS, Aufstellort, Besitzer, Notizen, Tags", "nas"},
		{"tag", ":", "Tag (Platzhalter * erlaubt)", "tag:iot"},
		{"group", ":", "Mitglied einer Gruppe (manuell oder regelbasiert)", "group:server"},
		{"port", ": = > < >= <=", "Offener Port, optional mit /tcp oder /udp", "port:22 port:161/udp"},
		{"service", ":", "Dienstname laut nmap", "service:http"},
		{"product", ":", "Produkt eines Dienstes (Teilstring)", "product:openssh"},
		{"version", ":", "Version eines Dienstes", "version:9.*"},
		{"os", ":", "Betriebssystem (Teilstring)", "os:linux"},
		{"vendor", ":", "Hersteller (Teilstring)", "vendor:ubiquiti"},
		{"model", ":", "Modell (Teilstring)", "model:rt6"},
		{"type", ":", "Gerätetyp", "type:camera"},
		{"name", ":", "Anzeigename oder Hostname (Teilstring)", "name:pi"},
		{"hostname", ":", "Hostname (Teilstring)", "hostname:*.lan"},
		{"ip", ":", "IP-Adresse, Platzhalter oder CIDR", "ip:192.168.8.0/24"},
		{"subnet", ":", "Subnetz (CIDR oder Name)", "subnet:iot"},
		{"mac", ":", "MAC-Adresse oder Präfix", "mac:dc:a6:32*"},
		{"state", ":", "known | unknown | ignored", "state:unknown"},
		{"crit", ": >=", "Kritikalität low | normal | high | critical", "crit>=high"},
		{"is", ":", "online | offline | new | known | unknown | ignored | randomized", "is:online"},
		{"online", ":", "yes | no", "online:no"},
		{"has", ":", "notes | cve | cert | health | ports | containers | packages | parent | children | tags | http | hostname", "has:cve"},
		{"cve", ": >= > <= <", "Höchster CVSS-Wert oder konkrete CVE", "cve>=7 cve:CVE-2024-6387"},
		{"seen", "< >", "Letzte Sichtung vor weniger/mehr als (m, h, d, w)", "seen<24h"},
		{"first", "< >", "Erstsichtung vor weniger/mehr als", "first<7d"},
		{"cert", "< : ", "Zertifikat läuft in weniger als N Tagen ab, oder expired | selfsigned | weak", "cert<30d"},
		{"app", ":", "Erkannte Web-Anwendung", "app:grafana"},
		{"title", ":", "HTTP-Seitentitel", "title:login"},
		{"container", ":", "Container-Name oder Image", "container:frigate"},
		{"package", ":", "Installiertes Paket", "package:openssl"},
		{"health", ":", "Zustand eines Health-Checks: up | down | degraded | unknown", "health:down"},
		{"source", ":", "Plugin, das Daten geliefert hat", "source:proxmox"},
		{"parent", ":", "Eltern-Gerät (ID oder Name)", "parent:pve1"},
		{"location", ":", "Aufstellort (manuelles Feld)", "location:keller"},
		{"site", ":", "NetScope-Standort, der das Gerät liefert (Zentrale); local = diese Instanz", "site:colo"},
		{"owner", ":", "Besitzer", "owner:anna"},
		{"id", ": > <", "Geräte-ID", "id:42"},
		{"cf.<feld>", ": = > < >= <=", "Custom Field", "cf.rack:A1"},
	}
}

type term struct {
	neg    bool
	field  string
	op     string
	values []string
	text   bool
}

// Query is a parsed filter query. Terms are combined with AND; values separated by |
// are alternatives (OR); a leading - or ! negates a term.
type Query struct {
	terms []term
	Raw   string
}

var knownFields = map[string]bool{}

func init() {
	for _, f := range []string{"tag", "group", "port", "service", "product", "version", "os", "vendor", "model", "type", "name",
		"hostname", "ip", "subnet", "mac", "state", "crit", "criticality", "is", "online", "has", "cve", "cvss", "seen", "first",
		"cert", "app", "title", "container", "package", "pkg", "health", "source", "parent", "location", "owner", "notes", "id", "site"} {
		knownFields[f] = true
	}
}

// ParseQuery parses the filter language.
func ParseQuery(s string) (*Query, error) {
	q := &Query{Raw: s}
	rs := []rune(s)
	i := 0
	for i < len(rs) {
		for i < len(rs) && unicode.IsSpace(rs[i]) {
			i++
		}
		if i >= len(rs) {
			break
		}
		start := i
		t := term{}
		if (rs[i] == '-' || rs[i] == '!') && i+1 < len(rs) && !unicode.IsSpace(rs[i+1]) {
			t.neg = true
			i++
		}
		// field candidate
		j := i
		for j < len(rs) && (unicode.IsLetter(rs[j]) || unicode.IsDigit(rs[j]) || rs[j] == '_' || rs[j] == '.' || rs[j] == '-') {
			j++
		}
		field := strings.ToLower(string(rs[i:j]))
		op := ""
		if j < len(rs) {
			for _, cand := range []string{">=", "<=", "!=", ":", "=", ">", "<"} {
				if strings.HasPrefix(string(rs[j:min(j+2, len(rs))]), cand) {
					op = cand
					break
				}
			}
		}
		isField := op != "" && (knownFields[field] || (strings.HasPrefix(field, "cf.") && len(field) > 3))
		if isField {
			t.field, t.op = field, op
			i = j + len([]rune(op))
		} else {
			t.text = true
		}
		val, next, err := readValue(rs, i)
		if err != nil {
			return nil, err
		}
		i = next
		if t.text {
			// free text: take the token verbatim (including any ':' of MACs/IPv6)
			if val == "" && next == start {
				i++
				continue
			}
			t.values = []string{strings.TrimSuffix(val, "\x00quoted")}
		} else {
			if val == "" {
				return nil, fmt.Errorf("Feld %q ohne Wert", field)
			}
			if strings.Contains(val, "\x00quoted") {
				t.values = []string{strings.TrimSuffix(val, "\x00quoted")}
			} else {
				for _, v := range strings.Split(val, "|") {
					if v = strings.TrimSpace(v); v != "" {
						t.values = append(t.values, v)
					}
				}
			}
		}
		if len(t.values) == 0 {
			continue
		}
		q.terms = append(q.terms, t)
	}
	return q, nil
}

// readValue reads a quoted or bare value starting at i.
func readValue(rs []rune, i int) (string, int, error) {
	if i < len(rs) && rs[i] == '"' {
		var b strings.Builder
		i++
		for i < len(rs) {
			if rs[i] == '\\' && i+1 < len(rs) {
				b.WriteRune(rs[i+1])
				i += 2
				continue
			}
			if rs[i] == '"' {
				return b.String() + "\x00quoted", i + 1, nil
			}
			b.WriteRune(rs[i])
			i++
		}
		return "", i, fmt.Errorf("fehlendes schließendes Anführungszeichen")
	}
	j := i
	for j < len(rs) && !unicode.IsSpace(rs[j]) {
		j++
	}
	return string(rs[i:j]), j, nil
}

// Empty reports whether the query has no terms.
func (q *Query) Empty() bool { return q == nil || len(q.terms) == 0 }

// GroupResolver returns a manual group id or the query of a query group by name.
type GroupResolver func(name string) (manualID int64, query string, err error)

type compiler struct {
	now    time.Time
	groups GroupResolver
	depth  int
}

// Compile translates the query into an SQL condition on alias d (devices).
func (q *Query) Compile(now time.Time, groups GroupResolver) (string, []any, error) {
	c := &compiler{now: now, groups: groups}
	return c.compile(q)
}

func (c *compiler) compile(q *Query) (string, []any, error) {
	if q.Empty() {
		return "1=1", nil, nil
	}
	var parts []string
	var args []any
	for _, t := range q.terms {
		var alts []string
		for _, v := range t.values {
			cond, a, err := c.term(t, v)
			if err != nil {
				return "", nil, err
			}
			alts = append(alts, cond)
			args = append(args, a...)
		}
		cond := "(" + strings.Join(alts, " OR ") + ")"
		if t.neg {
			cond = "NOT " + cond
		}
		parts = append(parts, cond)
	}
	return strings.Join(parts, " AND "), args, nil
}

// likePattern converts a value with * wildcards to a LIKE pattern. Without wildcards,
// substring=true searches anywhere, otherwise the match is exact (case-insensitive).
func likePattern(v string, substring bool) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	esc := r.Replace(v)
	if strings.Contains(v, "*") {
		return strings.ReplaceAll(esc, "*", "%")
	}
	if substring {
		return "%" + esc + "%"
	}
	return esc
}

const likeEsc = ` ESCAPE '\'`

func numOp(op string) (string, error) {
	switch op {
	case ":", "=":
		return "=", nil
	case "!=":
		return "<>", nil
	case ">", "<", ">=", "<=":
		return op, nil
	}
	return "", fmt.Errorf("Operator %q nicht unterstützt", op)
}

// parseAge parses 30m, 24h, 7d, 2w, 1y (plain numbers are hours).
func parseAge(v string) (time.Duration, error) {
	v = strings.ToLower(strings.TrimSpace(v))
	if v == "" {
		return 0, fmt.Errorf("Dauer fehlt")
	}
	unit := v[len(v)-1]
	num := v[:len(v)-1]
	mult := time.Hour
	switch unit {
	case 's':
		mult = time.Second
	case 'm':
		mult = time.Minute
	case 'h':
		mult = time.Hour
	case 'd':
		mult = 24 * time.Hour
	case 'w':
		mult = 7 * 24 * time.Hour
	case 'y':
		mult = 365 * 24 * time.Hour
	default:
		num = v
	}
	f, err := strconv.ParseFloat(num, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("ungültige Dauer %q (z. B. 30m, 24h, 7d)", v)
	}
	return time.Duration(f * float64(mult)), nil
}

func boolValue(v string) (bool, error) {
	switch strings.ToLower(v) {
	case "yes", "ja", "true", "1", "y", "on":
		return true, nil
	case "no", "nein", "false", "0", "n", "off":
		return false, nil
	}
	return false, fmt.Errorf("ja/nein erwartet, nicht %q", v)
}

var critRankSQL = "(CASE d.criticality WHEN 'low' THEN 0 WHEN 'normal' THEN 1 WHEN 'high' THEN 2 WHEN 'critical' THEN 3 END)"
var critRanks = map[string]int{"low": 0, "normal": 1, "high": 2, "critical": 3}

// correlatedDevice matches "SELECT 1 FROM <from> WHERE <alias>.<col> = d.id [AND <rest>]".
var correlatedDevice = regexp.MustCompile(`(?s)^SELECT 1 FROM (.+?) WHERE (\w+)\.(device_id|child_id|parent_id) = d\.id(?: AND (.*))?$`)

// existsSQL turns a correlated "does the device have …" subquery into an uncorrelated
// "d.id IN (SELECT device_id …)", which SQLite evaluates once instead of once per device
// (port:22 would otherwise walk the port index for every device). The IS NOT NULL guard
// keeps a negated term (NOT …) correct for nullable device columns.
func existsSQL(sub string) string {
	m := correlatedDevice.FindStringSubmatch(sub)
	if m == nil {
		return "EXISTS (" + sub + ")"
	}
	col := m[2] + "." + m[3]
	where := col + " IS NOT NULL"
	if m[4] != "" {
		where += " AND " + m[4]
	}
	return "d.id IN (SELECT " + col + " FROM " + m[1] + " WHERE " + where + ")"
}

const activeCVE = `SELECT MAX(c.cvss_score) FROM device_cves c WHERE c.device_id = d.id AND c.gone_at IS NULL
	AND NOT EXISTS (SELECT 1 FROM cve_ignores i WHERE i.device_id = c.device_id AND i.cve_id = c.cve_id)`

func cidrRange(v string) ([]byte, []byte, bool) {
	p, err := netip.ParsePrefix(v)
	if err != nil {
		return nil, nil, false
	}
	p = p.Masked()
	lo := p.Addr()
	hi := lo
	if lo.Is4() {
		b := lo.As4()
		host := 32 - p.Bits()
		v := uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
		v |= uint32((uint64(1) << uint(host)) - 1)
		hi = netip.AddrFrom4([4]byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)})
	} else {
		b := lo.As16()
		host := 128 - p.Bits()
		for i := 15; i >= 0 && host > 0; i-- {
			n := min(host, 8)
			b[i] |= byte((1 << uint(n)) - 1)
			host -= n
		}
		hi = netip.AddrFrom16(b)
	}
	return netutil.IPKey(lo.String()), netutil.IPKey(hi.String()), true
}

func (c *compiler) term(t term, v string) (string, []any, error) {
	if t.text {
		p := likePattern(v, true)
		cond := `(d.display_name LIKE ?` + likeEsc + ` OR d.hostname LIKE ?` + likeEsc + ` OR d.vendor LIKE ?` + likeEsc +
			` OR d.model LIKE ?` + likeEsc + ` OR d.os LIKE ?` + likeEsc + ` OR d.location LIKE ?` + likeEsc +
			` OR d.owner LIKE ?` + likeEsc + ` OR d.notes LIKE ?` + likeEsc + ` OR d.type LIKE ?` + likeEsc +
			` OR EXISTS (SELECT 1 FROM device_ips i WHERE i.device_id = d.id AND i.gone_at IS NULL AND i.ip LIKE ?` + likeEsc + `)` +
			` OR EXISTS (SELECT 1 FROM device_macs m WHERE m.device_id = d.id AND m.mac LIKE ?` + likeEsc + `)` +
			` OR EXISTS (SELECT 1 FROM device_tags t WHERE t.device_id = d.id AND t.tag LIKE ?` + likeEsc + `))`
		args := make([]any, 12)
		for i := range args {
			args[i] = p
		}
		return cond, args, nil
	}
	f, op := t.field, t.op
	textField := func(col string, substring bool) (string, []any, error) {
		if op != ":" && op != "=" && op != "!=" {
			return "", nil, fmt.Errorf("Feld %s unterstützt nur ':'", f)
		}
		cond := col + " LIKE ?" + likeEsc
		if op == "!=" {
			cond = "NOT (" + cond + ")"
		}
		return cond, []any{likePattern(v, substring && op == ":")}, nil
	}
	exists := func(sub string, args ...any) (string, []any, error) {
		return existsSQL(sub), args, nil
	}
	switch {
	case strings.HasPrefix(f, "cf."):
		key := strings.TrimPrefix(f, "cf.")
		path := `$."` + strings.ReplaceAll(key, `"`, "") + `"`
		if op == ":" {
			return "CAST(json_extract(d.custom, ?) AS TEXT) LIKE ?" + likeEsc, []any{path, likePattern(v, true)}, nil
		}
		o, err := numOp(op)
		if err != nil {
			return "", nil, err
		}
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			return "CAST(json_extract(d.custom, ?) AS REAL) " + o + " ?", []any{path, n}, nil
		}
		return "CAST(json_extract(d.custom, ?) AS TEXT) " + o + " ?", []any{path, v}, nil
	}
	switch f {
	case "tag":
		return exists("SELECT 1 FROM device_tags t WHERE t.device_id = d.id AND t.tag LIKE ?"+likeEsc, likePattern(strings.ToLower(v), false))
	case "group":
		if c.groups == nil {
			return "", nil, fmt.Errorf("Gruppen können hier nicht verwendet werden")
		}
		id, gq, err := c.groups(v)
		if err != nil {
			return "", nil, err
		}
		if id > 0 {
			return exists("SELECT 1 FROM group_members gm WHERE gm.device_id = d.id AND gm.group_id = ?", id)
		}
		if c.depth > 4 {
			return "", nil, fmt.Errorf("Gruppen-Verschachtelung zu tief (Zyklus?)")
		}
		sub, err := ParseQuery(gq)
		if err != nil {
			return "", nil, fmt.Errorf("Gruppe %s: %w", v, err)
		}
		c.depth++
		cond, args, err := c.compile(sub)
		c.depth--
		if err != nil {
			return "", nil, fmt.Errorf("Gruppe %s: %w", v, err)
		}
		return "(" + cond + ")", args, nil
	case "port":
		portStr, proto, _ := strings.Cut(strings.ToLower(v), "/")
		n, err := strconv.Atoi(portStr)
		if err != nil || n < 0 || n > 65535 {
			return "", nil, fmt.Errorf("ungültiger Port %q", v)
		}
		o, err := numOp(op)
		if err != nil {
			return "", nil, err
		}
		sub := "SELECT 1 FROM ports p WHERE p.device_id = d.id AND p.gone_at IS NULL AND p.port " + o + " ?"
		args := []any{n}
		if proto != "" {
			if proto != "tcp" && proto != "udp" {
				return "", nil, fmt.Errorf("ungültiges Protokoll %q", proto)
			}
			sub += " AND p.proto = ?"
			args = append(args, proto)
		}
		return exists(sub, args...)
	case "service":
		return exists("SELECT 1 FROM ports p WHERE p.device_id = d.id AND p.gone_at IS NULL AND p.service LIKE ?"+likeEsc, likePattern(v, false))
	case "product":
		return exists("SELECT 1 FROM ports p WHERE p.device_id = d.id AND p.gone_at IS NULL AND p.product LIKE ?"+likeEsc, likePattern(v, true))
	case "version":
		return exists("SELECT 1 FROM ports p WHERE p.device_id = d.id AND p.gone_at IS NULL AND p.version LIKE ?"+likeEsc, likePattern(v, false))
	case "os":
		return textField("d.os", true)
	case "vendor":
		return textField("d.vendor", true)
	case "model":
		return textField("d.model", true)
	case "type":
		return textField("d.type", false)
	case "hostname":
		return textField("d.hostname", true)
	case "location":
		return textField("d.location", true)
	case "owner":
		return textField("d.owner", true)
	case "notes":
		return textField("d.notes", true)
	case "name":
		p := likePattern(v, true)
		return "(d.display_name LIKE ?" + likeEsc + " OR d.hostname LIKE ?" + likeEsc + ")", []any{p, p}, nil
	case "ip", "subnet":
		if lo, hi, ok := cidrRange(v); ok {
			return exists("SELECT 1 FROM device_ips i WHERE i.device_id = d.id AND i.gone_at IS NULL AND i.ip_key BETWEEN ? AND ?", lo, hi)
		}
		if f == "subnet" {
			return exists(`SELECT 1 FROM device_ips i JOIN subnets s ON s.id = i.subnet_id WHERE i.device_id = d.id AND i.gone_at IS NULL
				AND (s.name LIKE ?`+likeEsc+` OR s.cidr = ?)`, likePattern(v, false), v)
		}
		return exists("SELECT 1 FROM device_ips i WHERE i.device_id = d.id AND i.gone_at IS NULL AND i.ip LIKE ?"+likeEsc, likePattern(v, false))
	case "mac":
		val := strings.ToLower(v)
		if n, ok := netutil.NormalizeMAC(val); ok {
			val = n
		} else {
			val = strings.ReplaceAll(val, "-", ":")
		}
		return exists("SELECT 1 FROM device_macs m WHERE m.device_id = d.id AND m.mac LIKE ?"+likeEsc, likePattern(val, false))
	case "state":
		switch v {
		case "known", "unknown", "ignored":
		default:
			return "", nil, fmt.Errorf("state: known, unknown oder ignored erwartet")
		}
		if op == "!=" {
			return "d.state <> ?", []any{v}, nil
		}
		return "d.state = ?", []any{v}, nil
	case "crit", "criticality":
		r, ok := critRanks[strings.ToLower(v)]
		if !ok {
			return "", nil, fmt.Errorf("Kritikalität: low, normal, high oder critical erwartet")
		}
		o, err := numOp(op)
		if err != nil {
			return "", nil, err
		}
		return critRankSQL + " " + o + " ?", []any{r}, nil
	case "online":
		b, err := boolValue(v)
		if err != nil {
			return "", nil, err
		}
		return "d.online = ?", []any{b}, nil
	case "is":
		switch strings.ToLower(v) {
		case "online":
			return "d.online = 1", nil, nil
		case "offline":
			return "d.online = 0", nil, nil
		case "new":
			return "d.first_seen >= ?", []any{c.now.Add(-24 * time.Hour).UnixMilli()}, nil
		case "known", "unknown", "ignored":
			return "d.state = ?", []any{strings.ToLower(v)}, nil
		case "randomized":
			return exists("SELECT 1 FROM device_macs m WHERE m.device_id = d.id AND m.randomized = 1")
		case "critical":
			return "d.criticality = 'critical'", nil, nil
		}
		return "", nil, fmt.Errorf("is: online, offline, new, known, unknown, ignored, randomized oder critical erwartet")
	case "has":
		switch strings.ToLower(v) {
		case "notes":
			return "d.notes <> ''", nil, nil
		case "hostname":
			return "d.hostname <> ''", nil, nil
		case "cve", "cves":
			return "(" + activeCVE + ") IS NOT NULL", nil, nil
		case "cert", "certs":
			return exists("SELECT 1 FROM certificates x WHERE x.device_id = d.id AND x.gone_at IS NULL")
		case "health":
			return exists("SELECT 1 FROM health_checks h WHERE h.device_id = d.id")
		case "ports":
			return exists("SELECT 1 FROM ports p WHERE p.device_id = d.id AND p.gone_at IS NULL")
		case "http":
			return exists("SELECT 1 FROM http_services h WHERE h.device_id = d.id AND h.gone_at IS NULL")
		case "containers":
			return exists("SELECT 1 FROM containers x WHERE x.device_id = d.id AND x.gone_at IS NULL")
		case "packages":
			return exists("SELECT 1 FROM packages x WHERE x.device_id = d.id AND x.gone_at IS NULL")
		case "parent":
			return exists("SELECT 1 FROM relations r WHERE r.child_id = d.id")
		case "children":
			return exists("SELECT 1 FROM relations r WHERE r.parent_id = d.id")
		case "tags":
			return exists("SELECT 1 FROM device_tags t WHERE t.device_id = d.id")
		case "mac":
			return exists("SELECT 1 FROM device_macs m WHERE m.device_id = d.id")
		}
		return "", nil, fmt.Errorf("has: unbekannter Wert %q", v)
	case "cve", "cvss":
		if strings.HasPrefix(strings.ToUpper(v), "CVE-") {
			return exists(`SELECT 1 FROM device_cves c WHERE c.device_id = d.id AND c.gone_at IS NULL AND c.cve_id = ?
				AND NOT EXISTS (SELECT 1 FROM cve_ignores i WHERE i.device_id = c.device_id AND i.cve_id = c.cve_id)`, strings.ToUpper(v))
		}
		n, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return "", nil, fmt.Errorf("cve: Zahl oder CVE-ID erwartet")
		}
		o := op
		if o == ":" {
			o = ">="
		}
		if o, err = numOp(o); err != nil {
			return "", nil, err
		}
		return "IFNULL((" + activeCVE + "), -1) " + o + " ?", []any{n}, nil
	case "seen", "first":
		col := "d.last_seen"
		if f == "first" {
			col = "d.first_seen"
		}
		age, err := parseAge(v)
		if err != nil {
			return "", nil, err
		}
		cut := c.now.Add(-age).UnixMilli()
		switch op {
		case "<", "<=", ":":
			return col + " >= ?", []any{cut}, nil
		case ">", ">=":
			return "(" + col + " IS NULL OR " + col + " < ?)", []any{cut}, nil
		}
		return "", nil, fmt.Errorf("%s unterstützt < und >", f)
	case "cert":
		switch strings.ToLower(v) {
		case "expired":
			return exists("SELECT 1 FROM certificates x WHERE x.device_id = d.id AND x.gone_at IS NULL AND x.not_after < ?", c.now.UnixMilli())
		case "selfsigned", "self-signed":
			return exists("SELECT 1 FROM certificates x WHERE x.device_id = d.id AND x.gone_at IS NULL AND x.self_signed = 1")
		case "weak":
			return exists("SELECT 1 FROM certificates x WHERE x.device_id = d.id AND x.gone_at IS NULL AND (x.weak_protocols <> '[]' OR x.weak_ciphers <> '[]')")
		case "invalid":
			return exists("SELECT 1 FROM certificates x WHERE x.device_id = d.id AND x.gone_at IS NULL AND x.chain_valid = 0")
		}
		days := strings.TrimSuffix(strings.ToLower(v), "d")
		n, err := strconv.Atoi(days)
		if err != nil {
			return "", nil, fmt.Errorf("cert: Tage (z. B. cert<30d) oder expired/selfsigned/weak/invalid erwartet")
		}
		cut := c.now.Add(time.Duration(n) * 24 * time.Hour).UnixMilli()
		switch op {
		case "<", "<=", ":":
			return exists("SELECT 1 FROM certificates x WHERE x.device_id = d.id AND x.gone_at IS NULL AND x.not_after < ?", cut)
		case ">", ">=":
			return exists("SELECT 1 FROM certificates x WHERE x.device_id = d.id AND x.gone_at IS NULL AND x.not_after >= ?", cut)
		}
		return "", nil, fmt.Errorf("cert unterstützt < und >")
	case "app":
		return exists(`SELECT 1 FROM http_services h, json_each(h.apps) j WHERE h.device_id = d.id AND h.gone_at IS NULL
			AND json_extract(j.value, '$.name') LIKE ?`+likeEsc, likePattern(v, true))
	case "title":
		return exists("SELECT 1 FROM http_services h WHERE h.device_id = d.id AND h.gone_at IS NULL AND h.title LIKE ?"+likeEsc, likePattern(v, true))
	case "container":
		p := likePattern(v, true)
		return exists("SELECT 1 FROM containers x WHERE x.device_id = d.id AND x.gone_at IS NULL AND (x.name LIKE ?"+likeEsc+" OR x.image LIKE ?"+likeEsc+")", p, p)
	case "package", "pkg":
		return exists("SELECT 1 FROM packages x WHERE x.device_id = d.id AND x.gone_at IS NULL AND x.name LIKE ?"+likeEsc, likePattern(v, false))
	case "health":
		switch v {
		case "up", "down", "degraded", "unknown":
		default:
			return "", nil, fmt.Errorf("health: up, down, degraded oder unknown erwartet")
		}
		return exists("SELECT 1 FROM health_checks h WHERE h.device_id = d.id AND h.enabled = 1 AND h.state = ?", v)
	case "source":
		return `(d.created_source = ? OR EXISTS (SELECT 1 FROM external_refs r WHERE r.device_id = d.id AND r.source = ?)
			OR EXISTS (SELECT 1 FROM device_presence p WHERE p.device_id = d.id AND p.plugin_id = ?)
			OR EXISTS (SELECT 1 FROM device_facts f WHERE f.device_id = d.id AND f.source = ? AND f.gone_at IS NULL))`, []any{v, v, v, v}, nil
	case "parent":
		if id, err := strconv.ParseInt(v, 10, 64); err == nil {
			return exists("SELECT 1 FROM relations r WHERE r.child_id = d.id AND r.parent_id = ?", id)
		}
		p := likePattern(v, false)
		return exists(`SELECT 1 FROM relations r JOIN devices pd ON pd.id = r.parent_id WHERE r.child_id = d.id
			AND (pd.display_name LIKE ?`+likeEsc+` OR pd.hostname LIKE ?`+likeEsc+`)`, p, p)
	case "site":
		if op != ":" && op != "=" {
			return "", nil, fmt.Errorf("site unterstützt nur ':'")
		}
		switch strings.ToLower(v) {
		case "local", "lokal":
			return "d.site_id IS NULL", nil, nil
		case "*":
			return "d.site_id IS NOT NULL", nil, nil
		}
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return "d.site_id = ?", []any{n}, nil
		}
		p := likePattern(v, false)
		return "d.site_id IN (SELECT st.id FROM sites st WHERE st.slug LIKE ?" + likeEsc + " OR st.name LIKE ?" + likeEsc + ")", []any{p, p}, nil
	case "id":
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return "", nil, fmt.Errorf("id: Zahl erwartet")
		}
		o, err := numOp(op)
		if err != nil {
			return "", nil, err
		}
		return "d.id " + o + " ?", []any{n}, nil
	}
	return "", nil, fmt.Errorf("unbekanntes Feld %q", f)
}

// FieldNames returns the known field names (for error messages and completion).
func FieldNames() []string {
	out := make([]string, 0, len(knownFields))
	for f := range knownFields {
		out = append(out, f)
	}
	sort.Strings(out)
	return out
}
