package cve

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// Disclaimer is shown by the API and UI next to every CVE list.
const Disclaimer = "Der CVE-Abgleich ist heuristisch. Versionen stammen aus Dienst-Bannern (nmap), der Web-Erkennung, " +
	"der Betriebssystem-Erkennung und Paketlisten und werden mit den CPE-Angaben der NVD verglichen. Distributionen " +
	"spielen Sicherheitskorrekturen oft zurück (Backports), ohne die Versionsnummer zu erhöhen – Treffer vom Typ " +
	"„heuristisch“ sind deshalb häufig Fehlalarme. Umgekehrt fehlen Treffer, wenn ein Dienst seine Version nicht " +
	"preisgibt, die Zuordnung zu einem NVD-Produkt fehlt oder die NVD-Daten unvollständig sind. Treffer vor Maßnahmen " +
	"anhand der Hinweise von Hersteller bzw. Distribution prüfen und nicht zutreffende CVEs als irrelevant markieren."

// MatchTypeLabels are the German labels of the match types.
var MatchTypeLabels = map[string]string{
	MatchExact:     "Exakte Version",
	MatchRange:     "Versionsbereich",
	MatchHeuristic: "Heuristisch",
}

// Filter selects rows for ListVulnerabilities.
type Filter struct {
	MinScore       float64 `json:"minScore,omitempty"`
	Text           string  `json:"text,omitempty"`    // substring of CVE ID or description
	Product        string  `json:"product,omitempty"` // substring of product name or CPE
	DeviceID       int64   `json:"deviceId,omitempty"`
	IncludeIgnored bool    `json:"includeIgnored,omitempty"`
	// Sort: score | published | devices | cve | first_seen; prefix "-" sorts descending.
	// Default "-score".
	Sort   string `json:"sort,omitempty"`
	Limit  int    `json:"limit,omitempty"` // default 100, max 1000
	Offset int    `json:"offset,omitempty"`
	// Site restricts to the devices of a NetScope site (0 = this instance, nil = all).
	Site *int64 `json:"site,omitempty"`
}

// VulnRow is one CVE with the devices it currently affects.
type VulnRow struct {
	CVE            string     `json:"cve"`
	CVSS           *float64   `json:"cvss"`
	Severity       string     `json:"severity"` // critical | high | medium | low | none | unknown
	Vector         string     `json:"vector"`
	CVSSVersion    string     `json:"cvssVersion"`
	Description    string     `json:"description"`
	Published      *time.Time `json:"published"`
	LastModified   *time.Time `json:"lastModified"`
	Devices        int        `json:"devices"`        // affected devices (with the filter applied)
	IgnoredDevices int        `json:"ignoredDevices"` // of those, marked irrelevant (only with IncludeIgnored)
	AllIgnored     bool       `json:"allIgnored"`
	Products       []string   `json:"products"`
	MatchTypes     []string   `json:"matchTypes"`
	FirstSeen      time.Time  `json:"firstSeen"`
}

// DeviceCVE is an active CVE match of a device.
type DeviceCVE struct {
	ID          int64       `json:"id"`
	DeviceID    int64       `json:"deviceId"`
	DeviceName  string      `json:"deviceName"`
	CVE         string      `json:"cve"`
	CVSS        *float64    `json:"cvss"`
	Severity    string      `json:"severity"`
	Vector      string      `json:"vector"`
	CVSSVersion string      `json:"cvssVersion"`
	Description string      `json:"description"`
	Published   *time.Time  `json:"published"`
	Refs        []Reference `json:"refs"`
	CWEs        []string    `json:"cwes"`
	Product     string      `json:"product"`
	Version     string      `json:"version"`
	CPE         string      `json:"cpe"`
	Source      string      `json:"source"`
	MatchType   string      `json:"matchType"` // exact | range | heuristic
	FirstSeen   time.Time   `json:"firstSeen"`
	LastSeen    time.Time   `json:"lastSeen"`
	Ignored     bool        `json:"ignored"`
	IgnoreNote  string      `json:"ignoreNote,omitempty"`
	IgnoredBy   string      `json:"ignoredBy,omitempty"`
	IgnoredAt   *time.Time  `json:"ignoredAt,omitempty"`
}

// CPEMatch is a vulnerable configuration entry of a CVE.
type CPEMatch struct {
	Criteria              string `json:"criteria"`
	Part                  string `json:"part"`
	Vendor                string `json:"vendor"`
	Product               string `json:"product"`
	Version               string `json:"version"`
	Update                string `json:"update"`
	VersionStartIncluding string `json:"versionStartIncluding,omitempty"`
	VersionStartExcluding string `json:"versionStartExcluding,omitempty"`
	VersionEndIncluding   string `json:"versionEndIncluding,omitempty"`
	VersionEndExcluding   string `json:"versionEndExcluding,omitempty"`
}

// CVEInfo is the mirrored NVD record of a CVE.
type CVEInfo struct {
	ID           string      `json:"id"`
	Status       string      `json:"status"` // NVD vulnStatus
	Published    *time.Time  `json:"published"`
	LastModified *time.Time  `json:"lastModified"`
	CVSS         *float64    `json:"cvss"`
	Vector       string      `json:"vector"`
	CVSSVersion  string      `json:"cvssVersion"`
	Severity     string      `json:"severity"`
	Description  string      `json:"description"`
	Refs         []Reference `json:"refs"`
	CWEs         []string    `json:"cwes"`
	CPEMatches   []CPEMatch  `json:"cpeMatches"`
	// CPEMatchesTotal is the number of vulnerable CPE entries (CPEMatches is capped).
	CPEMatchesTotal int    `json:"cpeMatchesTotal"`
	InMirror        bool   `json:"inMirror"` // false: only known from device matches (e.g. rejected since)
	URL             string `json:"url"`      // NVD detail page
}

// FeedStatus is the sync state of one NVD feed.
type FeedStatus struct {
	Name         string     `json:"name"`         // year or "modified"
	LastModified string     `json:"lastModified"` // from the feed's .meta file
	SHA256       string     `json:"sha256"`
	Size         int64      `json:"size"`     // compressed download size in bytes
	CVECount     int        `json:"cveCount"` // CVEs in the feed (without rejected ones)
	SyncedAt     *time.Time `json:"syncedAt"` // last successful check
	Status       string     `json:"status"`   // ok | syncing | error | pending
	Error        string     `json:"error,omitempty"`
}

// MatchStatus describes the last full match.
type MatchStatus struct {
	At         *time.Time `json:"at"`       // start of the last successful full match
	Finished   *time.Time `json:"finished"` // end of the last attempt
	Devices    int        `json:"devices"`
	ActiveCVEs int        `json:"activeCves"` // active device CVE rows
	Status     string     `json:"status"`     // ok | error
	Error      string     `json:"error,omitempty"`
}

// Status is the state of the local NVD mirror.
type Status struct {
	Empty bool         `json:"empty"`
	Feeds []FeedStatus `json:"feeds"`
	// CVECount counts the mirrored CVEs that carry vulnerable CPE data (only those can
	// match); rejected CVEs and CVEs the NVD has not analysed are not stored.
	CVECount      int64        `json:"cveCount"`
	CPEMatchCount int64        `json:"cpeMatchCount"`
	LastSync      *time.Time   `json:"lastSync"`
	SyncMode      string       `json:"syncMode,omitempty"` // initial | full | incremental
	SyncStatus    string       `json:"syncStatus,omitempty"`
	SyncError     string       `json:"syncError,omitempty"`
	LastMatch     *MatchStatus `json:"lastMatch"`
	Disclaimer    string       `json:"disclaimer"`
}

// ---------------------------------------------------------------- helpers

var cveIDRe = regexp.MustCompile(`^CVE-\d{4}-\d{4,}$`)

// NormalizeCVEID validates and upper-cases a CVE ID.
func NormalizeCVEID(id string) (string, error) {
	id = strings.ToUpper(strings.TrimSpace(id))
	if !cveIDRe.MatchString(id) {
		return "", fmt.Errorf("ungültige CVE-ID %q", id)
	}
	return id, nil
}

// severityOf normalises the stored NVD severity, deriving it from the score if missing.
func severityOf(stored string, score *float64) string {
	switch s := strings.ToLower(stored); s {
	case "critical", "high", "medium", "low", "none":
		return s
	}
	if score == nil {
		return "unknown"
	}
	if *score == 0 {
		return "none"
	}
	return string(plugin.SeverityFromCVSS(*score))
}

func likeArg(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(strings.TrimSpace(s)) + "%"
}

func floatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func jsonList(s sql.NullString) []string {
	var out []string
	_ = db.Unmarshal(s.String, &out)
	var clean []string
	for _, v := range out {
		if v != "" {
			clean = append(clean, v)
		}
	}
	if clean == nil {
		clean = []string{}
	}
	sort.Strings(clean)
	return clean
}

// ---------------------------------------------------------------- list

var sortColumns = map[string]string{
	"score":      "COALESCE(MAX(n.cvss_score), MAX(c.cvss_score), -1)",
	"published":  "MAX(n.published)",
	"devices":    "COUNT(DISTINCT c.device_id)",
	"cve":        "c.cve_id",
	"first_seen": "MIN(c.first_seen)",
}

// ListVulnerabilities returns the active CVEs across all devices, grouped per CVE, and
// the total number of CVEs matching the filter.
func ListVulnerabilities(ctx context.Context, d *db.DB, f Filter) ([]VulnRow, int, error) {
	conds := []string{"c.gone_at IS NULL"}
	var args []any
	if !f.IncludeIgnored {
		conds = append(conds, "i.device_id IS NULL")
	}
	if f.MinScore > 0 {
		conds = append(conds, "COALESCE(n.cvss_score, c.cvss_score, 0) >= ?")
		args = append(args, f.MinScore)
	}
	if t := strings.TrimSpace(f.Text); t != "" {
		conds = append(conds, `(c.cve_id LIKE ? ESCAPE '\' OR n.description LIKE ? ESCAPE '\')`)
		args = append(args, likeArg(t), likeArg(t))
	}
	if pr := strings.TrimSpace(f.Product); pr != "" {
		conds = append(conds, `(c.product LIKE ? ESCAPE '\' OR c.cpe LIKE ? ESCAPE '\')`)
		args = append(args, likeArg(pr), likeArg(pr))
	}
	if f.DeviceID > 0 {
		conds = append(conds, "c.device_id = ?")
		args = append(args, f.DeviceID)
	}
	if f.Site != nil {
		if *f.Site == 0 {
			conds = append(conds, "c.device_id IN (SELECT id FROM devices WHERE site_id IS NULL)")
		} else {
			conds = append(conds, "c.device_id IN (SELECT id FROM devices WHERE site_id = ?)")
			args = append(args, *f.Site)
		}
	}
	from := ` FROM device_cves c
		LEFT JOIN nvd_cves n ON n.id = c.cve_id
		LEFT JOIN cve_ignores i ON i.device_id = c.device_id AND i.cve_id = c.cve_id
		WHERE ` + strings.Join(conds, " AND ")
	var total int
	if err := d.R.QueryRowContext(ctx, "SELECT COUNT(DISTINCT c.cve_id)"+from, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	key, desc := strings.TrimPrefix(f.Sort, "-"), strings.HasPrefix(f.Sort, "-")
	if f.Sort == "" {
		key, desc = "score", true
	}
	col, ok := sortColumns[key]
	if !ok {
		return nil, 0, fmt.Errorf("unbekannte Sortierung %q", f.Sort)
	}
	dir := "ASC"
	if desc {
		dir = "DESC"
	}
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	limit = min(limit, 1000)
	q := `SELECT c.cve_id, MAX(n.cvss_score), MAX(c.cvss_score), MAX(n.severity), MAX(n.cvss_vector), MAX(n.cvss_version),
		MAX(n.description), MAX(n.published), MAX(n.last_modified),
		COUNT(DISTINCT c.device_id), COUNT(DISTINCT CASE WHEN i.device_id IS NOT NULL THEN c.device_id END),
		json_group_array(DISTINCT c.product), json_group_array(DISTINCT c.match_type), MIN(c.first_seen)` + from +
		` GROUP BY c.cve_id ORDER BY ` + col + ` ` + dir + `, c.cve_id DESC LIMIT ? OFFSET ?`
	rows, err := d.R.QueryContext(ctx, q, append(args, limit, max(f.Offset, 0))...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []VulnRow{}
	for rows.Next() {
		var (
			r                      VulnRow
			nScore, cScore         sql.NullFloat64
			sev, vector, ver, desc sql.NullString
			published, modified    sql.NullInt64
			products, types        sql.NullString
			first                  int64
		)
		if err := rows.Scan(&r.CVE, &nScore, &cScore, &sev, &vector, &ver, &desc, &published, &modified,
			&r.Devices, &r.IgnoredDevices, &products, &types, &first); err != nil {
			return nil, 0, err
		}
		r.CVSS = floatPtr(nScore)
		if r.CVSS == nil {
			r.CVSS = floatPtr(cScore)
		}
		r.Severity = severityOf(sev.String, r.CVSS)
		r.Vector, r.CVSSVersion, r.Description = vector.String, ver.String, desc.String
		r.Published, r.LastModified = db.NullTime(published), db.NullTime(modified)
		r.Products, r.MatchTypes = jsonList(products), jsonList(types)
		r.AllIgnored = r.Devices > 0 && r.IgnoredDevices == r.Devices
		r.FirstSeen = db.Time(first)
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// ---------------------------------------------------------------- device rows

const deviceCVESelect = `SELECT c.id, c.device_id, d.display_name, d.hostname, d.primary_ip, d.primary_mac, c.cve_id,
	n.cvss_score, c.cvss_score, n.severity, n.cvss_vector, n.cvss_version, n.description, n.published, n.refs, n.cwes,
	c.product, c.version, c.cpe, c.source, c.match_type, c.first_seen, c.last_seen,
	i.device_id IS NOT NULL, i.note, i.created_by, i.created_at
	FROM device_cves c
	JOIN devices d ON d.id = c.device_id
	LEFT JOIN nvd_cves n ON n.id = c.cve_id
	LEFT JOIN cve_ignores i ON i.device_id = c.device_id AND i.cve_id = c.cve_id
	WHERE c.gone_at IS NULL`

func queryDeviceCVEs(ctx context.Context, d *db.DB, where string, args ...any) ([]DeviceCVE, error) {
	rows, err := d.R.QueryContext(ctx, deviceCVESelect+where+` ORDER BY COALESCE(n.cvss_score, c.cvss_score, -1) DESC, c.cve_id DESC, c.device_id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []DeviceCVE{}
	for rows.Next() {
		var (
			r                                  DeviceCVE
			disp, host, ip, mac                string
			nScore, cScore                     sql.NullFloat64
			sev, vector, ver, desc, refs, cwes sql.NullString
			published, ignoredAt               sql.NullInt64
			note, by                           sql.NullString
			first, last                        int64
		)
		if err := rows.Scan(&r.ID, &r.DeviceID, &disp, &host, &ip, &mac, &r.CVE, &nScore, &cScore, &sev, &vector, &ver, &desc,
			&published, &refs, &cwes, &r.Product, &r.Version, &r.CPE, &r.Source, &r.MatchType, &first, &last,
			&r.Ignored, &note, &by, &ignoredAt); err != nil {
			return nil, err
		}
		r.DeviceName = deviceName(disp, host, ip, mac, r.DeviceID)
		r.CVSS = floatPtr(nScore)
		if r.CVSS == nil {
			r.CVSS = floatPtr(cScore)
		}
		r.Severity = severityOf(sev.String, r.CVSS)
		r.Vector, r.CVSSVersion, r.Description = vector.String, ver.String, desc.String
		r.Published = db.NullTime(published)
		r.Refs = []Reference{}
		_ = db.Unmarshal(refs.String, &r.Refs)
		r.CWEs = []string{}
		_ = db.Unmarshal(cwes.String, &r.CWEs)
		r.FirstSeen, r.LastSeen = db.Time(first), db.Time(last)
		if r.Ignored {
			r.IgnoreNote, r.IgnoredBy, r.IgnoredAt = note.String, by.String, db.NullTime(ignoredAt)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// DeviceCVEs returns the active CVE matches of a device, highest score first.
func DeviceCVEs(ctx context.Context, d *db.DB, deviceID int64, includeIgnored bool) ([]DeviceCVE, error) {
	where := " AND c.device_id = ?"
	if !includeIgnored {
		where += " AND i.device_id IS NULL"
	}
	return queryDeviceCVEs(ctx, d, where, deviceID)
}

// ---------------------------------------------------------------- detail

// maxDetailMatches caps the CPE entries returned by CVEDetail.
const maxDetailMatches = 500

// CVEDetail returns the NVD record of a CVE and its active device matches (ignored ones
// included and flagged). It returns db.ErrNotFound for unknown CVEs.
func CVEDetail(ctx context.Context, d *db.DB, id string) (*CVEInfo, []DeviceCVE, error) {
	id, err := NormalizeCVEID(id)
	if err != nil {
		return nil, nil, err
	}
	info := &CVEInfo{ID: id, URL: "https://nvd.nist.gov/vuln/detail/" + id, Refs: []Reference{}, CWEs: []string{}, CPEMatches: []CPEMatch{}}
	var (
		published, modified sql.NullInt64
		score               sql.NullFloat64
		refs, cwes          string
	)
	err = d.R.QueryRowContext(ctx, `SELECT status, published, last_modified, cvss_score, cvss_vector, cvss_version, severity, description, refs, cwes
		FROM nvd_cves WHERE id = ?`, id).Scan(&info.Status, &published, &modified, &score, &info.Vector, &info.CVSSVersion,
		&info.Severity, &info.Description, &refs, &cwes)
	switch {
	case err == nil:
		info.InMirror = true
		info.Published, info.LastModified, info.CVSS = db.NullTime(published), db.NullTime(modified), floatPtr(score)
		info.Severity = severityOf(info.Severity, info.CVSS)
		_ = db.Unmarshal(refs, &info.Refs)
		_ = db.Unmarshal(cwes, &info.CWEs)
	case errors.Is(err, sql.ErrNoRows):
		info.Severity = "unknown"
	default:
		return nil, nil, err
	}
	devs, err := queryDeviceCVEs(ctx, d, " AND c.cve_id = ?", id)
	if err != nil {
		return nil, nil, err
	}
	if !info.InMirror && len(devs) == 0 {
		return nil, nil, db.ErrNotFound
	}
	if info.InMirror {
		if err := d.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM nvd_cpe_matches WHERE cve_id = ?", id).Scan(&info.CPEMatchesTotal); err != nil {
			return nil, nil, err
		}
		err := queryEach(ctx, d.R, `SELECT part, vendor, product, version, upd, start_incl, start_excl, end_incl, end_excl
			FROM nvd_cpe_matches WHERE cve_id = ? ORDER BY vendor, product, version LIMIT ?`, []any{id, maxDetailMatches}, func(r *sql.Rows) error {
			var m CPEMatch
			if err := r.Scan(&m.Part, &m.Vendor, &m.Product, &m.Version, &m.Update, &m.VersionStartIncluding, &m.VersionStartExcluding,
				&m.VersionEndIncluding, &m.VersionEndExcluding); err != nil {
				return err
			}
			m.Criteria = CPE{Part: m.Part, Vendor: m.Vendor, Product: m.Product, Version: m.Version, Update: m.Update}.String()
			info.CPEMatches = append(info.CPEMatches, m)
			return nil
		})
		if err != nil {
			return nil, nil, err
		}
	}
	return info, devs, nil
}

// ---------------------------------------------------------------- ignore

// SetIgnored marks a CVE as irrelevant for a device (or removes the mark). The mark
// survives re-matching; it hides the CVE from lists, counts and events.
func SetIgnored(ctx context.Context, d *db.DB, deviceID int64, cveID string, ignored bool, note, by string) error {
	id, err := NormalizeCVEID(cveID)
	if err != nil {
		return err
	}
	var n int
	if err := d.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE id = ?", deviceID).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return db.ErrNotFound
	}
	if !ignored {
		_, err := d.W.ExecContext(ctx, "DELETE FROM cve_ignores WHERE device_id = ? AND cve_id = ?", deviceID, id)
		return err
	}
	_, err = d.W.ExecContext(ctx, `INSERT INTO cve_ignores(device_id, cve_id, note, created_by, created_at) VALUES (?,?,?,?,?)
		ON CONFLICT(device_id, cve_id) DO UPDATE SET note = excluded.note, created_by = excluded.created_by, created_at = excluded.created_at`,
		deviceID, id, strings.TrimSpace(note), by, db.Now())
	return err
}

// ---------------------------------------------------------------- status

// SyncStatus returns the state of the NVD mirror and of the last full match.
func SyncStatus(ctx context.Context, d *db.DB) (*Status, error) {
	states, err := loadFeedStates(ctx, d.R)
	if err != nil {
		return nil, err
	}
	st := &Status{Feeds: []FeedStatus{}, Disclaimer: Disclaimer}
	for name, s := range states {
		if strings.HasPrefix(name, "_") {
			continue
		}
		st.Feeds = append(st.Feeds, FeedStatus{Name: name, LastModified: s.LastModified, SHA256: s.SHA256, Size: s.Size,
			CVECount: s.CVECount, SyncedAt: msPtr(s.SyncedAt), Status: s.Status, Error: s.Error})
	}
	sort.Slice(st.Feeds, func(i, j int) bool {
		a, b := st.Feeds[i].Name, st.Feeds[j].Name
		ya, errA := strconv.Atoi(a)
		yb, errB := strconv.Atoi(b)
		switch {
		case errA != nil && errB != nil:
			return a < b
		case errA != nil:
			return true // "modified" first
		case errB != nil:
			return false
		}
		return ya > yb
	})
	if s, ok := states[stateSync]; ok {
		st.CVECount, st.CPEMatchCount = int64(s.CVECount), s.Size
		st.LastSync, st.SyncMode, st.SyncStatus, st.SyncError = msPtr(s.SyncedAt), s.LastModified, s.Status, s.Error
	} else {
		if err := d.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM nvd_cves").Scan(&st.CVECount); err != nil {
			return nil, err
		}
		if err := d.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM nvd_cpe_matches").Scan(&st.CPEMatchCount); err != nil {
			return nil, err
		}
	}
	if s, ok := states[stateMatch]; ok {
		ms := &MatchStatus{At: msPtr(s.SyncedAt), Devices: int(s.Size), ActiveCVEs: s.CVECount, Status: s.Status, Error: s.Error}
		if t, err := time.Parse(time.RFC3339, s.LastModified); err == nil {
			ms.Finished = &t
		}
		st.LastMatch = ms
	}
	if st.Empty, err = mirrorEmpty(ctx, d); err != nil {
		return nil, err
	}
	return st, nil
}

func msPtr(ms int64) *time.Time {
	if ms == 0 {
		return nil
	}
	t := time.UnixMilli(ms)
	return &t
}

// Summary counts the active, non-ignored device CVEs (device × CVE) by severity for the
// dashboard. Keys: critical, high, medium, low, none, unknown, total, devices.
func Summary(ctx context.Context, d *db.DB) (map[string]int, error) {
	out := map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0, "none": 0, "unknown": 0, "total": 0, "devices": 0}
	devices := map[int64]bool{}
	err := queryEach(ctx, d.R, `SELECT c.device_id, MAX(n.severity), COALESCE(MAX(n.cvss_score), MAX(c.cvss_score))
		FROM device_cves c LEFT JOIN nvd_cves n ON n.id = c.cve_id
		WHERE c.gone_at IS NULL AND NOT EXISTS (SELECT 1 FROM cve_ignores i WHERE i.device_id = c.device_id AND i.cve_id = c.cve_id)
		GROUP BY c.device_id, c.cve_id`, nil, func(r *sql.Rows) error {
		var (
			dev   int64
			sev   sql.NullString
			score sql.NullFloat64
		)
		if err := r.Scan(&dev, &sev, &score); err != nil {
			return err
		}
		out[severityOf(sev.String, floatPtr(score))]++
		out["total"]++
		devices[dev] = true
		return nil
	})
	out["devices"] = len(devices)
	return out, err
}
