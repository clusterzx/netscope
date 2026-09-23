package cve

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// Match types stored in device_cves.match_type.
const (
	MatchExact     = "exact"     // the NVD lists exactly this version
	MatchRange     = "range"     // the version lies inside a vulnerable version range
	MatchHeuristic = "heuristic" // distribution package, banner with distro patches, OS guess or "all versions"
)

func matchRank(t string) int {
	switch t {
	case MatchExact:
		return 3
	case MatchRange:
		return 2
	case MatchHeuristic:
		return 1
	}
	return 0
}

// Entry kinds (where a device-side CPE comes from).
const (
	kindPort    = "port"
	kindOS      = "os"
	kindHTTP    = "http"
	kindPackage = "package"
)

// entry is a device-side CPE derived from one inventory record.
type entry struct {
	cpe       CPE    // canonical vendor/product, version (and update) to match
	key       string // canonical CPE 2.3 string, stored in device_cves.cpe
	kind      string
	source    string // plugin that reported the data (ports.source, packages.source, ...)
	product   string // display name: nmap product, web app, package or OS name
	version   string // display version: banner or package version
	heuristic bool
	enabled   bool  // kind enabled in the settings (disabled kinds only count as "still present")
	history   int64 // first time the device had data of this kind
	changed   int64 // when this record appeared (or was last refreshed, for web apps)
	full      []vtok
}

// device is the matching input of one device.
type device struct {
	id       int64
	name     string
	state    string
	osSource string
	entries  []entry
	prev     int64 // last evaluation (0 = never matched)
}

// distroProducts are distribution CPEs. The NVD lists whole distribution releases as
// "vulnerable" whenever one of their packages was, without any patch level, so matching
// them would flag every host of a release. Packages are matched instead.
var distroProducts = map[string]bool{
	"canonical:ubuntu_linux": true, "debian:debian_linux": true, "fedoraproject:fedora": true,
	"redhat:enterprise_linux": true, "redhat:enterprise_linux_server": true, "redhat:enterprise_linux_desktop": true,
	"redhat:enterprise_linux_workstation": true, "redhat:enterprise_linux_eus": true, "centos:centos": true,
	"rockylinux:rocky_linux": true, "almalinux:almalinux": true, "oracle:linux": true, "opensuse:leap": true,
	"opensuse:tumbleweed": true, "suse:linux_enterprise_server": true, "suse:linux_enterprise_desktop": true,
	"alpinelinux:alpine_linux": true, "archlinux:arch_linux": true, "amazon:linux": true, "amazon:linux_2": true,
	"raspberrypi:raspberry_pi_os": true, "linuxmint:linux_mint": true, "gentoo:linux": true,
}

// fingerprintSources report the operating system from a TCP/IP stack fingerprint.
var fingerprintSources = map[string]bool{"nmap": true}

// genericOSProducts are general-purpose operating systems whose CVEs depend on the patch
// level, which a network fingerprint cannot see.
var genericOSProducts = map[string]bool{
	"apple:iphone_os": true, "apple:ipados": true, "apple:mac_os_x": true, "apple:macos": true, "apple:mac_os": true,
	"apple:tvos": true, "apple:watchos": true, "apple:visionos": true, "google:android": true,
	"microsoft:windows": true, "microsoft:windows_10": true, "microsoft:windows_11": true,
	"microsoft:windows_7": true, "microsoft:windows_8.1": true, "microsoft:windows_xp": true,
	"microsoft:windows_server_2012": true, "microsoft:windows_server_2016": true, "microsoft:windows_server_2019": true,
	"microsoft:windows_server_2022": true, "microsoft:windows_server_2025": true,
	"freebsd:freebsd": true, "openbsd:openbsd": true, "netbsd:netbsd": true, "oracle:solaris": true,
	"sun:sunos": true, "ibm:aix": true, "hp:hp-ux": true,
}

// isOSGuess reports whether an OS fact is a fingerprint guess rather than a fact read
// from the device (SSH, SNMP, APIs).
func isOSGuess(source string, info plugin.OSInfo) bool {
	return fingerprintSources[source] || info.Accuracy < 100
}

// ambiguousOSName reports nmap names that list several versions or products.
func ambiguousOSName(name string) bool {
	return strings.Contains(name, " - ") || strings.Contains(name, " or ") || strings.Contains(name, ", ")
}

func buildEntry(c CPE, kind, source, product, version string, heuristic bool, changed int64) (entry, bool) {
	if !specific(c.Version) || !hasDigit(c.Version) {
		return entry{}, false
	}
	c.Vendor, c.Product = canonical(c.Vendor, c.Product)
	full := c.Version
	if specific(c.Update) {
		full += " " + c.Update
	}
	if product == "" {
		product = c.Product
	}
	if version == "" {
		version = c.Version
	}
	return entry{cpe: c, key: c.String(), kind: kind, source: source, product: product, version: version,
		heuristic: heuristic, changed: changed, full: tokenize(full)}, true
}

// ---------------------------------------------------------------- collection

// idFilter appends "AND <col> IN (…)" for a chunk of ids.
func idFilter(col string, ids []int64) (string, []any) {
	if ids == nil {
		return "", nil
	}
	return " AND " + col + " IN (" + db.Placeholders(len(ids)) + ")", db.Int64Args(ids)
}

// chunks splits ids (nil = one pass over all devices).
func chunks(ids []int64, n int) [][]int64 {
	if ids == nil {
		return [][]int64{nil}
	}
	var out [][]int64
	for len(ids) > 0 {
		k := min(n, len(ids))
		out = append(out, ids[:k])
		ids = ids[k:]
	}
	return out
}

func queryEach(ctx context.Context, q db.Querier, query string, args []any, fn func(*sql.Rows) error) error {
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// collect loads the devices (all if ids is nil) and derives their CPEs.
func collect(ctx context.Context, d *db.DB, ids []int64, cfg config) (map[int64]*device, []int64, error) {
	devs := map[int64]*device{}
	var order []int64
	for _, ch := range chunks(ids, 500) {
		where, args := idFilter("id", ch)
		err := queryEach(ctx, d.R, `SELECT id, display_name, hostname, primary_ip, primary_mac, state, os_source FROM devices WHERE 1=1`+where, args,
			func(r *sql.Rows) error {
				var (
					dv                     device
					disp, host, ip, mac, s string
				)
				if err := r.Scan(&dv.id, &disp, &host, &ip, &mac, &s, &dv.osSource); err != nil {
					return err
				}
				dv.state = s
				dv.name = deviceName(disp, host, ip, mac, dv.id)
				devs[dv.id] = &dv
				order = append(order, dv.id)
				return nil
			})
		if err != nil {
			return nil, nil, err
		}
	}
	if len(devs) == 0 {
		return devs, order, nil
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	byID := func(id int64) *device { return devs[id] }
	// history of each data kind per device
	type histKey struct {
		dev  int64
		kind string
	}
	hist := map[histKey]int64{}
	loadHist := func(kind, query string) error {
		for _, ch := range chunks(ids, 500) {
			where, args := idFilter("device_id", ch)
			q := strings.Replace(query, "{where}", where, 1)
			if err := queryEach(ctx, d.R, q, args, func(r *sql.Rows) error {
				var (
					dev   int64
					src   string
					first sql.NullInt64
				)
				if err := r.Scan(&dev, &src, &first); err != nil {
					return err
				}
				hist[histKey{dev, kind + "\x00" + src}] = first.Int64
				return nil
			}); err != nil {
				return err
			}
		}
		return nil
	}
	if err := loadHist(kindPort, `SELECT device_id, '', MIN(first_seen) FROM ports WHERE 1=1{where} GROUP BY device_id`); err != nil {
		return nil, nil, err
	}
	if err := loadHist(kindHTTP, `SELECT device_id, '', MIN(first_seen) FROM http_services WHERE 1=1{where} GROUP BY device_id`); err != nil {
		return nil, nil, err
	}
	if err := loadHist(kindOS, `SELECT device_id, source, MIN(first_seen) FROM device_facts WHERE kind = 'os'{where} GROUP BY device_id, source`); err != nil {
		return nil, nil, err
	}
	for _, ch := range chunks(ids, 500) {
		where, args := idFilter("device_id", ch)
		// ports: nmap CPE 2.2 URIs
		err := queryEach(ctx, d.R, `SELECT device_id, product, version, extra_info, cpes, source, first_seen FROM ports
			WHERE gone_at IS NULL AND cpes <> '[]'`+where, args, func(r *sql.Rows) error {
			var (
				dev                                int64
				product, version, extra, cpes, src string
				first                              int64
			)
			if err := r.Scan(&dev, &product, &version, &extra, &cpes, &src, &first); err != nil {
				return err
			}
			dv := byID(dev)
			if dv == nil {
				return nil
			}
			var list []string
			if json.Unmarshal([]byte(cpes), &list) != nil {
				return nil
			}
			var parsed []CPE
			apps := 0
			for _, s := range list {
				c, err := ParseCPE(s)
				if err != nil || distroProducts[c.VendorProduct()] {
					continue
				}
				if c.Part == "a" {
					apps++
				}
				parsed = append(parsed, c)
			}
			heur := hasDistroHint(version + " " + extra)
			for _, c := range parsed {
				if !specific(c.Version) {
					// nmap often reports the version only in the version field
					if c.Part != "a" || apps != 1 || serviceVersion(version) == "" {
						continue
					}
					c.Version = serviceVersion(version)
				}
				e, ok := buildEntry(c, kindPort, src, product, version, heur, first)
				if !ok {
					continue
				}
				e.enabled = true
				e.history = hist[histKey{dev, kindPort + "\x00"}]
				dv.entries = append(dv.entries, e)
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		// operating system of the effective OS source
		err = queryEach(ctx, d.R, `SELECT device_id, source, value, extra, first_seen FROM device_facts
			WHERE kind = 'os' AND gone_at IS NULL`+where, args, func(r *sql.Rows) error {
			var (
				dev                  int64
				src, value, extraRaw string
				first                int64
			)
			if err := r.Scan(&dev, &src, &value, &extraRaw, &first); err != nil {
				return err
			}
			dv := byID(dev)
			if dv == nil || src != dv.osSource {
				return nil
			}
			var info plugin.OSInfo
			if json.Unmarshal([]byte(extraRaw), &info) != nil {
				return nil
			}
			// Fingerprint guesses that cannot name a patch level stay "present but
			// disabled": they are not matched, and dropping them raises no cve.resolved.
			guess := isOSGuess(src, info)
			ambiguous := guess && ambiguousOSName(info.Name) // "Android 9 - 10", "A, B, or C"
			for _, s := range info.CPEs {
				c, err := ParseCPE(s)
				if err != nil || distroProducts[c.VendorProduct()] {
					continue
				}
				enabled := cfg.includeOS
				if c.Vendor == "linux" && c.Product == "linux_kernel" {
					// A TCP/IP fingerprint says nothing about the kernel's patch level, and
					// distribution kernels ("6.8.0-45-generic") are versioned by ABI.
					if guess {
						enabled = false
					} else {
						up, _, _ := strings.Cut(c.Version, "-")
						if !kernelVersionUsable(up, c.Version) {
							continue
						}
						c.Version = up
					}
				}
				if guess && !cfg.includeOSGuesses && (ambiguous || genericOSProducts[c.VendorProduct()]) {
					// iOS, Android, Windows … are patched monthly: every CVE of the release would match
					enabled = false
				}
				e, ok := buildEntry(c, kindOS, src, value, "", guess, first)
				if !ok {
					continue
				}
				e.enabled = enabled
				e.history = hist[histKey{dev, kindOS + "\x00" + src}]
				dv.entries = append(dv.entries, e)
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
		// detected web applications
		err = queryEach(ctx, d.R, `SELECT device_id, apps, source, last_seen FROM http_services
			WHERE gone_at IS NULL AND apps <> '[]'`+where, args, func(r *sql.Rows) error {
			var (
				dev       int64
				apps, src string
				last      int64
			)
			if err := r.Scan(&dev, &apps, &src, &last); err != nil {
				return err
			}
			dv := byID(dev)
			if dv == nil {
				return nil
			}
			var list []plugin.DetectedApp
			if json.Unmarshal([]byte(apps), &list) != nil {
				return nil
			}
			for _, a := range list {
				if a.CPE == "" {
					continue
				}
				c, err := ParseCPE(a.CPE)
				if err != nil {
					continue
				}
				if !specific(c.Version) {
					c.Version = serviceVersion(a.Version)
				}
				e, ok := buildEntry(c, kindHTTP, src, a.Name, a.Version, a.Confidence == "low", last)
				if !ok {
					continue
				}
				e.enabled = cfg.includeHTTP
				e.history = hist[histKey{dev, kindHTTP + "\x00"}]
				dv.entries = append(dv.entries, e)
			}
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	if err := collectPackages(ctx, d, ids, devs, cfg); err != nil {
		return nil, nil, err
	}
	// previous evaluation: the last time a CVE row of the device was confirmed
	for _, ch := range chunks(ids, 500) {
		where, args := idFilter("device_id", ch)
		err := queryEach(ctx, d.R, `SELECT device_id, MAX(last_seen) FROM device_cves WHERE 1=1`+where+` GROUP BY device_id`, args,
			func(r *sql.Rows) error {
				var dev, last int64
				if err := r.Scan(&dev, &last); err != nil {
					return err
				}
				if dv := byID(dev); dv != nil {
					dv.prev = last
				}
				return nil
			})
		if err != nil {
			return nil, nil, err
		}
	}
	for _, dv := range devs {
		dv.entries = dedupeEntries(dv.entries)
	}
	return devs, order, nil
}

// collectPackages derives CPEs from package inventories via the curated mapping table.
func collectPackages(ctx context.Context, d *db.DB, ids []int64, devs map[int64]*device, cfg config) error {
	filter, fargs := packageFilter()
	type kernelPick struct {
		e        entry
		upstream string
		full     string
	}
	kernels := map[int64]*kernelPick{}
	withPkgs := map[int64]bool{}
	for _, ch := range chunks(ids, 500) {
		where, args := idFilter("device_id", ch)
		err := queryEach(ctx, d.R, `SELECT device_id, manager, name, version, source, first_seen FROM packages
			WHERE gone_at IS NULL AND `+filter+where, append(append([]any{}, fargs...), args...), func(r *sql.Rows) error {
			var (
				dev                         int64
				manager, name, version, src string
				first                       int64
			)
			if err := r.Scan(&dev, &manager, &name, &version, &src, &first); err != nil {
				return err
			}
			dv := devs[dev]
			if dv == nil {
				return nil
			}
			rule, ok := lookupPackage(name)
			if !ok {
				return nil
			}
			up := UpstreamVersion(manager, version)
			c := CPE{Part: rule.part, Vendor: rule.vendor, Product: rule.product, Version: up, Update: cpeAny}
			e, ok := buildEntry(c, kindPackage, src, name, version, true, first)
			if !ok {
				return nil
			}
			e.enabled = cfg.includePackages
			withPkgs[dev] = true
			if rule.kernel {
				// several kernels are usually installed; the newest one is most likely running
				cur := kernels[dev]
				if cur == nil || CompareVersions(up, cur.upstream) > 0 {
					kernels[dev] = &kernelPick{e: e, upstream: up, full: epochRe.ReplaceAllString(version, "")}
				}
				return nil
			}
			dv.entries = append(dv.entries, e)
			return nil
		})
		if err != nil {
			return err
		}
	}
	for dev, k := range kernels {
		if kernelVersionUsable(k.upstream, k.full) {
			devs[dev].entries = append(devs[dev].entries, k.e)
		}
	}
	if len(withPkgs) == 0 {
		return nil
	}
	pkgDevs := make([]int64, 0, len(withPkgs))
	for id := range withPkgs {
		pkgDevs = append(pkgDevs, id)
	}
	hist := map[int64]int64{}
	for _, ch := range chunks(pkgDevs, 500) {
		err := queryEach(ctx, d.R, `SELECT device_id, MIN(first_seen) FROM packages WHERE device_id IN (`+db.Placeholders(len(ch))+`) GROUP BY device_id`,
			db.Int64Args(ch), func(r *sql.Rows) error {
				var dev, first int64
				if err := r.Scan(&dev, &first); err != nil {
					return err
				}
				hist[dev] = first
				return nil
			})
		if err != nil {
			return err
		}
	}
	for _, id := range pkgDevs {
		for i := range devs[id].entries {
			if devs[id].entries[i].kind == kindPackage {
				devs[id].entries[i].history = hist[id]
			}
		}
	}
	return nil
}

// dedupeEntries merges entries with the same CPE: a non-heuristic source wins, the
// oldest history and appearance count.
func dedupeEntries(in []entry) []entry {
	idx := map[string]int{}
	var out []entry
	for _, e := range in {
		i, ok := idx[e.key]
		if !ok {
			idx[e.key] = len(out)
			out = append(out, e)
			continue
		}
		cur := &out[i]
		if (cur.heuristic && !e.heuristic && e.enabled) || (!cur.enabled && e.enabled) {
			h, c := min(cur.history, e.history), min(cur.changed, e.changed)
			*cur = e
			cur.history, cur.changed = h, c
			continue
		}
		cur.history = min(cur.history, e.history)
		cur.changed = min(cur.changed, e.changed)
		cur.enabled = cur.enabled || e.enabled
	}
	return out
}

func deviceName(display, hostname, ip, mac string, id int64) string {
	switch {
	case display != "":
		return display
	case hostname != "":
		return hostname
	case ip != "":
		return ip
	case mac != "":
		return mac
	}
	return fmt.Sprintf("Gerät %d", id)
}

// ---------------------------------------------------------------- matching

// criteria is a vulnerable cpeMatch row with pre-tokenised versions.
type criteria struct {
	cve      string
	part     string
	product  string
	version  string
	upd      string
	vtoks    []vtok // version
	fullToks []vtok // version + update (if specific)
	bounds   [4][]vtok
	has      [4]bool
}

const (
	bStartIncl = iota
	bStartExcl
	bEndIncl
	bEndExcl
)

// matcher caches the criteria of the products seen in one run.
type matcher struct {
	d     *db.DB
	cache map[vendorProduct][]criteria
}

func newMatcher(d *db.DB) *matcher { return &matcher{d: d, cache: map[vendorProduct][]criteria{}} }

func (m *matcher) criteria(ctx context.Context, vp vendorProduct) ([]criteria, error) {
	if c, ok := m.cache[vp]; ok {
		return c, nil
	}
	var out []criteria
	err := queryEach(ctx, m.d.R, `SELECT cve_id, part, version, upd, start_incl, start_excl, end_incl, end_excl FROM nvd_cpe_matches
		WHERE vendor = ? AND product = ? AND vulnerable = 1`, []any{vp.vendor, vp.product}, func(r *sql.Rows) error {
		var (
			c              criteria
			si, se, ei, ee string
		)
		if err := r.Scan(&c.cve, &c.part, &c.version, &c.upd, &si, &se, &ei, &ee); err != nil {
			return err
		}
		c.product = vp.product
		c.vtoks = tokenize(c.version)
		c.fullToks = c.vtoks
		if specific(c.upd) {
			c.fullToks = tokenize(c.version + " " + c.upd)
		}
		for i, b := range []string{si, se, ei, ee} {
			if b != "" && b != cpeAny && b != cpeNA {
				c.bounds[i], c.has[i] = tokenize(b), true
			}
		}
		out = append(out, c)
		return nil
	})
	if err != nil {
		return nil, err
	}
	m.cache[vp] = out
	return out, nil
}

func tokensEqual(a, b []vtok) bool {
	c, _ := compareTokens(a, b)
	return c == 0
}

// isUpdateSuffix reports whether toks is an update designation like "p1" or "rc2".
func isUpdateSuffix(toks []vtok) bool {
	switch len(toks) {
	case 1:
		return !toks[0].num && updateMarkers[toks[0].s] && toks[0].s != "a" && toks[0].s != "b"
	case 2:
		return !toks[0].num && updateMarkers[toks[0].s] && toks[1].num
	}
	return false
}

// exactMatch compares a device version with an explicitly listed vulnerable version.
func exactMatch(e *entry, c *criteria) bool {
	switch {
	case specific(c.upd):
		return tokensEqual(e.full, c.fullToks)
	case c.upd == cpeNA:
		return !specific(e.cpe.Update) && tokensEqual(e.full, c.vtoks)
	}
	// update ANY: "9.6p1" is version 9.6 with update p1
	if tokensEqual(e.full, c.vtoks) {
		return true
	}
	n := len(c.vtoks)
	if n == 0 || len(e.full) <= n {
		return false
	}
	for i := 0; i < n; i++ {
		if e.full[i] != c.vtoks[i] {
			return false
		}
	}
	return isUpdateSuffix(e.full[n:])
}

// rangeMatch checks the version bounds. A device version that is less precise than a
// bound it would have to be compared with ("4.15" against "4.15.18") is not matched.
func rangeMatch(e *entry, c *criteria) bool {
	ambiguous := false
	for i := 0; i < 4; i++ {
		if !c.has[i] {
			continue
		}
		cmp, prefix := compareTokens(e.full, c.bounds[i])
		if prefix {
			ambiguous = true
			continue
		}
		switch i {
		case bStartIncl:
			if cmp < 0 {
				return false
			}
		case bStartExcl:
			if cmp <= 0 {
				return false
			}
		case bEndIncl:
			if cmp > 0 {
				return false
			}
		case bEndExcl:
			if cmp >= 0 {
				return false
			}
		}
	}
	return !ambiguous
}

// evaluate returns the match type of a device CPE against one criteria row.
func evaluate(e *entry, c *criteria) (string, bool) {
	var typ string
	switch {
	case c.version == cpeNA || c.version == "":
		return "", false
	case specific(c.version):
		if !exactMatch(e, c) {
			return "", false
		}
		typ = MatchExact
	case !c.has[0] && !c.has[1] && !c.has[2] && !c.has[3]:
		// "all versions": only meaningful for device firmware and hardware
		if e.kind == kindPackage || !(c.part == "h" || strings.HasSuffix(c.product, "_firmware")) {
			return "", false
		}
		typ = MatchHeuristic
	default:
		if !rangeMatch(e, c) {
			return "", false
		}
		typ = MatchRange
	}
	if e.heuristic {
		typ = MatchHeuristic
	}
	return typ, true
}

// hit is a matched CVE of a device.
type hit struct {
	cve string
	e   *entry
	typ string
}

func hitKey(cve, cpe string) string { return cve + "\x00" + cpe }

// matchDevice computes the CVE hits of one device.
func (m *matcher) matchDevice(ctx context.Context, dv *device) (map[string]*hit, error) {
	hits := map[string]*hit{}
	if dv.state == "ignored" {
		return hits, nil
	}
	for i := range dv.entries {
		e := &dv.entries[i]
		if !e.enabled {
			continue
		}
		for _, vp := range aliases(e.cpe.Vendor, e.cpe.Product) {
			crits, err := m.criteria(ctx, vp)
			if err != nil {
				return nil, err
			}
			for j := range crits {
				c := &crits[j]
				typ, ok := evaluate(e, c)
				if !ok {
					continue
				}
				k := hitKey(c.cve, e.key)
				if cur, ok := hits[k]; !ok || matchRank(typ) > matchRank(cur.typ) {
					hits[k] = &hit{cve: c.cve, e: e, typ: typ}
				}
			}
		}
	}
	return hits, nil
}

// ---------------------------------------------------------------- reconcile

// cveInfo is the NVD data needed for device rows and events.
type cveInfo struct {
	score       *float64
	vector      string
	severity    string
	description string
}

func loadCVEInfo(ctx context.Context, d *db.DB, ids []string, into map[string]*cveInfo) error {
	var need []string
	for _, id := range ids {
		if _, ok := into[id]; !ok {
			need = append(need, id)
		}
	}
	for len(need) > 0 {
		k := min(500, len(need))
		ch := need[:k]
		need = need[k:]
		err := queryEach(ctx, d.R, `SELECT id, cvss_score, cvss_vector, severity, description FROM nvd_cves WHERE id IN (`+db.Placeholders(len(ch))+`)`,
			db.StringArgs(ch), func(r *sql.Rows) error {
				var (
					id    string
					score sql.NullFloat64
					ci    cveInfo
				)
				if err := r.Scan(&id, &score, &ci.vector, &ci.severity, &ci.description); err != nil {
					return err
				}
				if score.Valid {
					v := score.Float64
					ci.score = &v
				}
				into[id] = &ci
				return nil
			})
		if err != nil {
			return err
		}
	}
	return nil
}

func (ci *cveInfo) scoreValue() float64 {
	if ci == nil || ci.score == nil {
		return 0
	}
	return *ci.score
}

// eventSeverity maps the NVD severity (or the score) to an event severity.
func eventSeverity(ci *cveInfo) plugin.Severity {
	if ci != nil {
		switch plugin.Severity(ci.severity) {
		case plugin.SevCritical, plugin.SevHigh, plugin.SevMedium, plugin.SevLow:
			return plugin.Severity(ci.severity)
		}
	}
	return plugin.SeverityFromCVSS(ci.scoreValue())
}

type curRow struct {
	id                       int64
	cve, cpe                 string
	matchType                string
	source, product, version string
	score                    sql.NullFloat64
}

// unchanged reports whether an active row already holds the values of a hit.
func (r curRow) unchanged(h *hit, score *float64) bool {
	if r.matchType != h.typ || r.source != h.e.source || r.product != h.e.product || r.version != h.e.version {
		return false
	}
	if score == nil {
		return !r.score.Valid
	}
	return r.score.Valid && r.score.Float64 == *score
}

// matchStats summarises a match pass.
type matchStats struct {
	Devices  int
	CPEs     int
	New      int
	Resolved int
	Active   int
	Events   int
}

// matchRun holds the state of one match pass.
type matchRun struct {
	p       *Plugin
	rc      *plugin.RunContext
	cfg     config
	changed map[string]struct{}
	now     int64
	info    map[string]*cveInfo
	stats   matchStats
}

// match matches the given devices (all if ids is nil) and reconciles device_cves.
func (p *Plugin) match(ctx context.Context, rc *plugin.RunContext, cfg config, ids []int64, changed map[string]struct{}) (_ *matchStats, retErr error) {
	p.matchMu.Lock()
	defer p.matchMu.Unlock()
	d := rc.DB
	start := p.now()
	full := ids == nil
	if full {
		defer func() {
			if retErr != nil {
				_ = saveMatchState(context.WithoutCancel(ctx), d, start, p.now(), 0, retErr)
			}
		}()
	}
	lastFull, err := lastFullMatch(ctx, d)
	if err != nil {
		return nil, err
	}
	devs, order, err := collect(ctx, d, ids, cfg)
	if err != nil {
		return nil, fmt.Errorf("Geräte-CPEs laden: %w", err)
	}
	p.mu.Lock()
	for _, dv := range devs {
		dv.prev = max(dv.prev, lastFull, p.evaluated[dv.id])
	}
	p.mu.Unlock()
	if changed == nil {
		changed = map[string]struct{}{}
	}
	mr := &matchRun{p: p, rc: rc, cfg: cfg, changed: changed, now: start.UnixMilli(), info: map[string]*cveInfo{}}
	m := newMatcher(d)
	const chunk = 100
	for i := 0; i < len(order); i += chunk {
		if err := ctx.Err(); err != nil {
			return &mr.stats, err
		}
		part := order[i:min(i+chunk, len(order))]
		hits := make(map[int64]map[string]*hit, len(part))
		var cves []string
		for _, id := range part {
			dv := devs[id]
			h, err := m.matchDevice(ctx, dv)
			if err != nil {
				return &mr.stats, err
			}
			hits[id] = h
			for _, x := range h {
				cves = append(cves, x.cve)
			}
			mr.stats.CPEs += len(dv.entries)
		}
		if err := loadCVEInfo(ctx, d, cves, mr.info); err != nil {
			return &mr.stats, err
		}
		events, err := mr.reconcile(ctx, part, devs, hits)
		if err != nil {
			return &mr.stats, fmt.Errorf("CVE-Treffer speichern: %w", err)
		}
		p.mu.Lock()
		for _, id := range part {
			p.evaluated[id] = mr.now
		}
		p.mu.Unlock()
		mr.stats.Devices += len(part)
		mr.emit(ctx, events)
		rc.Progress(min(i+chunk, len(order)), len(order))
	}
	if full {
		if err := saveMatchState(ctx, d, start, p.now(), len(order), nil); err != nil {
			rc.Log.Warn("Abgleich-Status nicht gespeichert", "error", err)
		}
		p.mu.Lock()
		for id := range p.evaluated {
			if devs[id] == nil { // deleted or merged device
				delete(p.evaluated, id)
			}
		}
		p.mu.Unlock()
	}
	return &mr.stats, nil
}

// reconcile writes the hits of a chunk of devices in one transaction and returns the
// events to emit after the commit.
func (mr *matchRun) reconcile(ctx context.Context, ids []int64, devs map[int64]*device, hits map[int64]map[string]*hit) ([]plugin.Event, error) {
	var events []plugin.Event
	var stats matchStats
	err := mr.rc.DB.Tx(ctx, func(tx *sql.Tx) error {
		events, stats = nil, matchStats{}
		cur := map[int64]map[string]curRow{}
		err := queryEach(ctx, tx, `SELECT id, device_id, cve_id, cpe, match_type, source, product, version, cvss_score
			FROM device_cves WHERE gone_at IS NULL AND device_id IN (`+db.Placeholders(len(ids))+`)`, db.Int64Args(ids), func(r *sql.Rows) error {
			var (
				row curRow
				dev int64
			)
			if err := r.Scan(&row.id, &dev, &row.cve, &row.cpe, &row.matchType, &row.source, &row.product, &row.version, &row.score); err != nil {
				return err
			}
			if cur[dev] == nil {
				cur[dev] = map[string]curRow{}
			}
			cur[dev][hitKey(row.cve, row.cpe)] = row
			return nil
		})
		if err != nil {
			return err
		}
		var curCVEs []string
		for _, rows := range cur {
			for _, r := range rows {
				curCVEs = append(curCVEs, r.cve)
			}
		}
		// read pool: safe while the write transaction is open
		if err := loadCVEInfo(ctx, mr.rc.DB, curCVEs, mr.info); err != nil {
			return err
		}
		ignored := map[string]bool{}
		err = queryEach(ctx, tx, `SELECT device_id, cve_id FROM cve_ignores WHERE device_id IN (`+db.Placeholders(len(ids))+`)`,
			db.Int64Args(ids), func(r *sql.Rows) error {
				var (
					dev int64
					cve string
				)
				if err := r.Scan(&dev, &cve); err != nil {
					return err
				}
				ignored[fmt.Sprintf("%d\x00%s", dev, cve)] = true
				return nil
			})
		if err != nil {
			return err
		}
		ins, err := tx.PrepareContext(ctx, `INSERT INTO device_cves(device_id, cve_id, cpe, source, product, version, match_type, cvss_score,
			first_seen, last_seen) VALUES (?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer ins.Close()
		upd, err := tx.PrepareContext(ctx, `UPDATE device_cves SET source = ?, product = ?, version = ?, match_type = ?, cvss_score = ? WHERE id = ?`)
		if err != nil {
			return err
		}
		defer upd.Close()
		closeRow, err := tx.PrepareContext(ctx, `UPDATE device_cves SET gone_at = ? WHERE id = ?`)
		if err != nil {
			return err
		}
		defer closeRow.Close()
		for _, id := range ids {
			dv := devs[id]
			rows := cur[id]
			before := map[string]bool{}
			for _, r := range rows {
				before[r.cve] = true
			}
			best := map[string]*hit{}
			for k, h := range hits[id] {
				ci := mr.info[h.cve]
				var (
					score    any
					scorePtr *float64
				)
				if ci != nil && ci.score != nil {
					score, scorePtr = *ci.score, ci.score
				}
				if r, ok := rows[k]; ok {
					// last_seen of all active rows is refreshed below in one statement
					if !r.unchanged(h, scorePtr) {
						if _, err := upd.ExecContext(ctx, h.e.source, h.e.product, h.e.version, h.typ, score, r.id); err != nil {
							return err
						}
					}
				} else if _, err := ins.ExecContext(ctx, id, h.cve, h.e.key, h.e.source, h.e.product, h.e.version, h.typ, score, mr.now, mr.now); err != nil {
					return err
				}
				if b := best[h.cve]; b == nil || matchRank(h.typ) > matchRank(b.typ) {
					best[h.cve] = h
				}
			}
			present := map[string]bool{} // device CPE -> kind enabled
			for _, e := range dv.entries {
				present[e.key] = present[e.key] || e.enabled
			}
			gone := map[string][]curRow{}
			for k, r := range rows {
				if _, ok := hits[id][k]; ok {
					continue
				}
				if _, err := closeRow.ExecContext(ctx, mr.now, r.id); err != nil {
					return err
				}
				if best[r.cve] == nil {
					gone[r.cve] = append(gone[r.cve], r)
				}
			}
			stats.Active += len(best)
			for cve, h := range best {
				if before[cve] {
					continue
				}
				stats.New++
				if mr.wantNew(dv, h, ignored[fmt.Sprintf("%d\x00%s", id, cve)]) {
					events = append(events, mr.newEvent(dv, h))
				}
			}
			for cve, rs := range gone {
				stats.Resolved++
				if mr.wantResolved(dv, cve, rs, present, ignored[fmt.Sprintf("%d\x00%s", id, cve)]) {
					events = append(events, mr.resolvedEvent(dv, cve))
				}
			}
		}
		// confirm all rows that are still active in one statement
		_, err = tx.ExecContext(ctx, `UPDATE device_cves SET last_seen = ? WHERE gone_at IS NULL AND device_id IN (`+
			db.Placeholders(len(ids))+`)`, append([]any{mr.now}, db.Int64Args(ids)...)...)
		return err
	})
	if err != nil {
		return nil, err
	}
	mr.stats.New += stats.New
	mr.stats.Resolved += stats.Resolved
	mr.stats.Active += stats.Active
	return events, nil
}

// wantNew decides whether a newly matched CVE raises cve.new. No events are raised for
// the first evaluation of a device or for the first data of a kind on a device (initial
// import), nor for matches that only appear because of history loaded into the mirror
// or changed settings: a match is news if the NVD published/changed the CVE since the
// last sync, or if the device's software appeared or changed since its last evaluation.
func (mr *matchRun) wantNew(dv *device, h *hit, ignored bool) bool {
	if ignored || dv.state == "ignored" || dv.prev == 0 {
		return false
	}
	if mr.info[h.cve].scoreValue() < mr.cfg.minScore {
		return false
	}
	if h.typ == MatchHeuristic && !mr.cfg.heuristicEvents {
		return false
	}
	if h.e.history > dv.prev {
		return false
	}
	if _, ok := mr.changed[h.cve]; ok {
		return true
	}
	return h.e.changed > dv.prev
}

// wantResolved decides whether a CVE that no longer matches raises cve.resolved: the
// software behind it was updated or removed, or the NVD changed the CVE. CVEs that
// vanish because a source was disabled or the CVE was rejected stay silent.
func (mr *matchRun) wantResolved(dv *device, cve string, rows []curRow, present map[string]bool, ignored bool) bool {
	if ignored || dv.state == "ignored" {
		return false
	}
	ci := mr.info[cve]
	if ci == nil || ci.scoreValue() < mr.cfg.minScore {
		return false // rejected/removed from the mirror, or below the threshold
	}
	heuristicOnly := true
	for _, r := range rows {
		if r.matchType != MatchHeuristic {
			heuristicOnly = false
		}
		if enabled, ok := present[r.cpe]; ok && !enabled {
			return false // the source kind was disabled in the settings
		}
	}
	if heuristicOnly && !mr.cfg.heuristicEvents {
		return false
	}
	if _, ok := mr.changed[cve]; ok {
		return true
	}
	for _, r := range rows {
		if _, ok := present[r.cpe]; !ok {
			return true
		}
	}
	return false
}

var matchTypeText = map[string]string{
	MatchExact:     "exakte Version",
	MatchRange:     "Versionsbereich",
	MatchHeuristic: "heuristisch",
}

func (mr *matchRun) newEvent(dv *device, h *hit) plugin.Event {
	ci := mr.info[h.cve]
	title := fmt.Sprintf("%s auf %s", h.cve, dv.name)
	var cvss any
	if ci != nil && ci.score != nil {
		title = fmt.Sprintf("%s (CVSS %.1f) auf %s", h.cve, *ci.score, dv.name)
		cvss = *ci.score
	}
	var msg strings.Builder
	fmt.Fprintf(&msg, "%s %s – Abgleich: %s.", h.e.product, h.e.version, matchTypeText[h.typ])
	if h.typ == MatchHeuristic {
		msg.WriteString(" Die Distribution hat die Lücke möglicherweise bereits per Backport geschlossen.")
	}
	vector := ""
	if ci != nil {
		vector = ci.vector
		if desc := truncate(ci.description, 400); desc != "" {
			msg.WriteString("\n")
			msg.WriteString(desc)
		}
	}
	return plugin.Event{
		Type: plugin.EvCVENew, Severity: eventSeverity(ci), DeviceID: dv.id, RunID: mr.rc.RunID,
		Title: title, Message: msg.String(),
		Payload: map[string]any{"cve": h.cve, "cvss": cvss, "vector": vector, "product": h.e.product,
			"version": h.e.version, "cpe": h.e.key, "match_type": h.typ},
		DedupKey: fmt.Sprintf("cve:%d:%s", dv.id, h.cve),
	}
}

func (mr *matchRun) resolvedEvent(dv *device, cve string) plugin.Event {
	ci := mr.info[cve]
	var cvss any
	if ci != nil && ci.score != nil {
		cvss = *ci.score
	}
	return plugin.Event{
		Type: plugin.EvCVEResolved, DeviceID: dv.id, RunID: mr.rc.RunID,
		Title:    fmt.Sprintf("%s auf %s trifft nicht mehr zu", cve, dv.name),
		Message:  "Die betroffene Software wurde aktualisiert, entfernt oder die NVD-Angaben haben sich geändert.",
		Payload:  map[string]any{"cve": cve, "cvss": cvss},
		DedupKey: fmt.Sprintf("cve.resolved:%d:%s", dv.id, cve),
	}
}

func (mr *matchRun) emit(ctx context.Context, events []plugin.Event) {
	if mr.rc.Events == nil {
		return
	}
	for _, ev := range events {
		id, err := mr.rc.Events.Emit(ctx, ev)
		if err != nil {
			mr.rc.Log.Warn("Event konnte nicht gespeichert werden", "type", ev.Type, "error", err)
			continue
		}
		if id > 0 {
			mr.stats.Events++
		}
	}
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n])) + " …"
}

// ---------------------------------------------------------------- match state

// lastFullMatch returns the start of the last completed full match (ms, 0 = never).
func lastFullMatch(ctx context.Context, d *db.DB) (int64, error) {
	var v sql.NullInt64
	err := d.R.QueryRowContext(ctx, "SELECT synced_at FROM nvd_feeds WHERE name = ?", stateMatch).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return v.Int64, err
}

// saveMatchState stores the last full match in the "_match" row: synced_at = start,
// last_modified = finish (RFC 3339), size = devices, cve_count = active device CVEs.
func saveMatchState(ctx context.Context, d *db.DB, start, finish time.Time, devices int, matchErr error) error {
	var active int
	if err := d.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_cves WHERE gone_at IS NULL").Scan(&active); err != nil {
		return err
	}
	status, msg := "ok", ""
	if matchErr != nil {
		status, msg = "error", matchErr.Error()
	}
	// start and device count only advance on success
	var synced, size any
	if matchErr == nil {
		synced, size = start.UnixMilli(), devices
	}
	_, err := d.W.ExecContext(ctx, `INSERT INTO nvd_feeds(name, last_modified, size, cve_count, synced_at, status, error)
		VALUES (?,?,COALESCE(?, 0),?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET last_modified = excluded.last_modified, cve_count = excluded.cve_count,
		size = COALESCE(?, nvd_feeds.size), synced_at = COALESCE(excluded.synced_at, nvd_feeds.synced_at),
		status = excluded.status, error = excluded.error`,
		stateMatch, finish.UTC().Format(time.RFC3339), size, active, synced, status, msg, size)
	return err
}
