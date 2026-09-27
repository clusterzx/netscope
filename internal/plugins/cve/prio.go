package cve

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// Prioritisation data next to the NVD mirror: the CISA catalog of Known Exploited
// Vulnerabilities (KEV) and the FIRST Exploit Prediction Scoring System (EPSS). Both are
// small enough to download with every sync; a table is only rewritten when the content
// changed (SHA-256 in nvd_feeds), and always as a whole.

const (
	// DefaultKEVURL is the JSON feed of the CISA KEV catalog (CC0).
	DefaultKEVURL = "https://www.cisa.gov/sites/default/files/feeds/known_exploited_vulnerabilities.json"
	// DefaultEPSSURL is the daily EPSS score file of all scored CVEs.
	DefaultEPSSURL = "https://epss.empiricalsecurity.com/epss_scores-current.csv.gz"

	feedKEV  = "kev"
	feedEPSS = "epss"

	maxKEVBytes  = 64 << 20
	maxEPSSBytes = 64 << 20 // compressed; about 3 MB today
)

type kevCatalog struct {
	CatalogVersion  string     `json:"catalogVersion"`
	DateReleased    string     `json:"dateReleased"`
	Count           int        `json:"count"`
	Vulnerabilities []kevEntry `json:"vulnerabilities"`
}

type kevEntry struct {
	CVE         string `json:"cveID"`
	Vendor      string `json:"vendorProject"`
	Product     string `json:"product"`
	Name        string `json:"vulnerabilityName"`
	DateAdded   string `json:"dateAdded"`
	Description string `json:"shortDescription"`
	Action      string `json:"requiredAction"`
	DueDate     string `json:"dueDate"`
	Ransomware  string `json:"knownRansomwareCampaignUse"` // Known | Unknown
	Notes       string `json:"notes"`
}

// prioStats summarises a KEV/EPSS sync.
type prioStats struct {
	KEV      int      // CVEs in the catalog
	KEVAdded []string // newly listed since the last sync (never on the first load)
	EPSS     int      // scored CVEs
	Updated  []string // feeds whose table was rewritten
}

// fetchBytes downloads url (with retries) and returns at most limit bytes.
func (f *fetcher) fetchBytes(ctx context.Context, url string, limit int64) ([]byte, error) {
	var body []byte
	err := f.retry(ctx, func() error {
		resp, err := f.get(ctx, url)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		body, err = io.ReadAll(io.LimitReader(resp.Body, limit+1))
		if err != nil {
			return err
		}
		if int64(len(body)) > limit {
			return fmt.Errorf("Download größer als %d MB", limit>>20)
		}
		return nil
	})
	return body, err
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

// syncPriority updates the KEV and EPSS tables if enabled. Errors of one source do not
// stop the other; they are returned joined.
func (p *Plugin) syncPriority(ctx context.Context, rc *plugin.RunContext, cfg config, force bool) (*prioStats, error) {
	st := &prioStats{}
	if !cfg.kevEnabled && !cfg.epssEnabled {
		return st, nil
	}
	states, err := loadFeedStates(ctx, rc.DB.R)
	if err != nil {
		return st, err
	}
	f := p.fetcher(rc, cfg)
	var errs []error
	run := func(name string, fn func() error) {
		if err := setFeedStatus(ctx, rc.DB, name, "syncing", ""); err != nil {
			errs = append(errs, err)
			return
		}
		err := fn()
		if err == nil || ctx.Err() != nil {
			return
		}
		msg := err.Error()
		rc.Log.Error("Priorisierungsdaten fehlgeschlagen", "feed", name, "error", msg)
		_ = setFeedStatus(ctx, rc.DB, name, "error", msg)
		errs = append(errs, fmt.Errorf("%s: %w", feedLabel(name), err))
		if rc.Events != nil {
			if _, eerr := rc.Events.Emit(ctx, plugin.Event{Type: plugin.EvNVDSyncFailed, RunID: rc.RunID,
				Title: feedLabel(name) + " konnte nicht geladen werden", Message: msg,
				Payload:  map[string]any{"error": msg, "feed": name},
				DedupKey: "nvd.sync_failed:" + name, DedupWindow: 12 * time.Hour}); eerr != nil {
				rc.Log.Warn("Event konnte nicht gespeichert werden", "error", eerr)
			}
		}
	}
	if cfg.kevEnabled {
		run(feedKEV, func() error { return p.syncKEV(ctx, rc, f, cfg.kevURL, states[feedKEV], force, st) })
	}
	if cfg.epssEnabled {
		run(feedEPSS, func() error { return p.syncEPSS(ctx, rc, f, cfg.epssURL, states[feedEPSS], force, st) })
	}
	if err := ctx.Err(); err != nil {
		return st, err
	}
	return st, errors.Join(errs...)
}

// feedLabel names a prioritisation feed for messages.
func feedLabel(name string) string {
	switch name {
	case feedKEV:
		return "CISA-KEV-Katalog"
	case feedEPSS:
		return "EPSS-Werte"
	}
	return "Feed " + name
}

// ---------------------------------------------------------------- KEV

func parseKEV(b []byte) (*kevCatalog, error) {
	var c kevCatalog
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("KEV-Katalog nicht lesbar: %w", err)
	}
	valid := c.Vulnerabilities[:0]
	for _, v := range c.Vulnerabilities {
		id, err := NormalizeCVEID(strings.TrimSpace(v.CVE))
		if err != nil {
			continue
		}
		v.CVE = id
		valid = append(valid, v)
	}
	c.Vulnerabilities = valid
	// an empty catalog is a broken download, never a real state: keep the old data
	if len(c.Vulnerabilities) == 0 {
		return nil, errors.New("KEV-Katalog enthält keine Einträge")
	}
	return &c, nil
}

func (p *Plugin) syncKEV(ctx context.Context, rc *plugin.RunContext, f *fetcher, url string, prev feedState, force bool, st *prioStats) error {
	body, err := f.fetchBytes(ctx, url, maxKEVBytes)
	if err != nil {
		return err
	}
	sum := sha256Hex(body)
	now := p.now().UnixMilli()
	if !force && prev.SHA256 == sum {
		st.KEV = prev.CVECount
		return savePrioFeed(ctx, rc.DB, feedKEV, prev.LastModified, sum, int64(len(body)), prev.CVECount, now)
	}
	cat, err := parseKEV(body)
	if err != nil {
		return err
	}
	var added []string
	err = rc.DB.Tx(ctx, func(tx *sql.Tx) error {
		known := map[string]bool{}
		rows, err := tx.QueryContext(ctx, "SELECT cve_id FROM nvd_kev")
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return err
			}
			known[id] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM nvd_kev"); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO nvd_kev(cve_id, vendor, product, name, description, action,
			date_added, due_date, ransomware, notes) VALUES (?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, v := range cat.Vulnerabilities {
			ransom := strings.EqualFold(strings.TrimSpace(v.Ransomware), "Known")
			if _, err := stmt.ExecContext(ctx, v.CVE, v.Vendor, v.Product, v.Name, v.Description, v.Action,
				v.DateAdded, v.DueDate, ransom, v.Notes); err != nil {
				return err
			}
			// the first load is history, not news
			if len(known) > 0 && !known[v.CVE] {
				added = append(added, v.CVE)
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	st.KEV, st.KEVAdded = len(cat.Vulnerabilities), added
	st.Updated = append(st.Updated, feedKEV)
	rc.Log.Info("KEV-Katalog importiert", "version", cat.CatalogVersion, "cves", st.KEV, "neu", len(added))
	return savePrioFeed(ctx, rc.DB, feedKEV, cat.CatalogVersion, sum, int64(len(body)), st.KEV, now)
}

// ---------------------------------------------------------------- EPSS

// epssRow is one scored CVE.
type epssRow struct {
	cve               string
	score, percentile float64
}

// parseEPSS reads the gzip-compressed EPSS CSV: a "#model_version:…,score_date:…" comment,
// the header "cve,epss,percentile" and one row per CVE. fn is called for every valid row.
func parseEPSS(gz []byte, fn func(epssRow) error) (version string, n int, err error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return "", 0, fmt.Errorf("EPSS-Datei nicht lesbar: %w", err)
	}
	defer zr.Close()
	br := bufio.NewReader(zr)
	if first, _ := br.Peek(1); len(first) == 1 && first[0] == '#' {
		line, err := br.ReadString('\n')
		if err != nil {
			return "", 0, fmt.Errorf("EPSS-Datei: %w", err)
		}
		version = epssVersion(strings.TrimSpace(line))
	}
	r := csv.NewReader(br)
	r.FieldsPerRecord = -1
	r.ReuseRecord = true
	header, err := r.Read()
	if err != nil {
		return "", 0, fmt.Errorf("EPSS-Datei ohne Kopfzeile: %w", err)
	}
	col := map[string]int{}
	for i, h := range header {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	ci, okC := col["cve"]
	si, okS := col["epss"]
	pi, okP := col["percentile"]
	if !okC || !okS || !okP {
		return "", 0, fmt.Errorf("EPSS-Datei: Spalten cve, epss, percentile erwartet, gefunden %q", strings.Join(header, ","))
	}
	for {
		rec, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return version, n, fmt.Errorf("EPSS-Datei Zeile %d: %w", n+3, err)
		}
		if len(rec) <= max(ci, si, pi) {
			continue
		}
		id, err := NormalizeCVEID(strings.TrimSpace(rec[ci]))
		if err != nil {
			continue
		}
		score, err1 := strconv.ParseFloat(strings.TrimSpace(rec[si]), 64)
		pct, err2 := strconv.ParseFloat(strings.TrimSpace(rec[pi]), 64)
		if err1 != nil || err2 != nil || score < 0 || score > 1 || pct < 0 || pct > 1 {
			continue
		}
		if err := fn(epssRow{cve: id, score: score, percentile: pct}); err != nil {
			return version, n, err
		}
		n++
	}
	if n == 0 {
		return version, 0, errors.New("EPSS-Datei enthält keine Werte")
	}
	return version, n, nil
}

// epssVersion turns "#model_version:v2026.06.15,score_date:2026-09-26T12:00:22Z" into
// "v2026.06.15 · 2026-09-26".
func epssVersion(line string) string {
	var model, date string
	for _, part := range strings.Split(strings.TrimPrefix(line, "#"), ",") {
		k, v, _ := strings.Cut(part, ":")
		switch strings.TrimSpace(k) {
		case "model_version":
			model = strings.TrimSpace(v)
		case "score_date":
			date, _, _ = strings.Cut(strings.TrimSpace(v), "T")
		}
	}
	switch {
	case model != "" && date != "":
		return model + " · " + date
	case date != "":
		return date
	}
	return model
}

func (p *Plugin) syncEPSS(ctx context.Context, rc *plugin.RunContext, f *fetcher, url string, prev feedState, force bool, st *prioStats) error {
	body, err := f.fetchBytes(ctx, url, maxEPSSBytes)
	if err != nil {
		return err
	}
	sum := sha256Hex(body)
	now := p.now().UnixMilli()
	if !force && prev.SHA256 == sum {
		st.EPSS = prev.CVECount
		return savePrioFeed(ctx, rc.DB, feedEPSS, prev.LastModified, sum, int64(len(body)), prev.CVECount, now)
	}
	// parse first: a broken file must not empty the table
	if _, _, err := parseEPSS(body, func(epssRow) error { return nil }); err != nil {
		return err
	}
	var (
		version string
		n       int
	)
	start := time.Now()
	err = rc.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM nvd_epss"); err != nil {
			return err
		}
		stmt, err := tx.PrepareContext(ctx, "INSERT OR REPLACE INTO nvd_epss(cve_id, score, percentile) VALUES (?,?,?)")
		if err != nil {
			return err
		}
		defer stmt.Close()
		version, n, err = parseEPSS(body, func(r epssRow) error {
			if n%20000 == 0 && ctx.Err() != nil {
				return ctx.Err()
			}
			_, err := stmt.ExecContext(ctx, r.cve, r.score, r.percentile)
			return err
		})
		return err
	})
	if err != nil {
		return err
	}
	st.EPSS = n
	st.Updated = append(st.Updated, feedEPSS)
	rc.Log.Info("EPSS-Werte importiert", "stand", version, "cves", n, "dauer", time.Since(start).Round(time.Millisecond).String())
	return savePrioFeed(ctx, rc.DB, feedEPSS, version, sum, int64(len(body)), n, now)
}

// savePrioFeed records a KEV/EPSS sync in nvd_feeds (last_modified: catalog version or
// score date, sha256: hash of the download).
func savePrioFeed(ctx context.Context, d *db.DB, name, version, sum string, size int64, count int, now int64) error {
	_, err := d.W.ExecContext(ctx, `INSERT INTO nvd_feeds(name, last_modified, sha256, size, cve_count, synced_at, status, error)
		VALUES (?,?,?,?,?,?,'ok','') ON CONFLICT(name) DO UPDATE SET last_modified = excluded.last_modified, sha256 = excluded.sha256,
		size = excluded.size, cve_count = excluded.cve_count, synced_at = excluded.synced_at, status = 'ok', error = ''`,
		name, version, sum, size, count, now)
	return err
}

// ---------------------------------------------------------------- exploited events

// exploitedEvents raises cve.exploited for every active, relevant device match of CVEs
// that were newly added to the KEV catalog.
func (p *Plugin) exploitedEvents(ctx context.Context, rc *plugin.RunContext, added []string) (int, error) {
	if rc.Events == nil || len(added) == 0 {
		return 0, nil
	}
	type hitRow struct {
		dev                  int64
		name, cve            string
		product, version     string
		score, epss          sql.NullFloat64
		dateAdded, due, desc string
		ransom               bool
	}
	var hits []hitRow
	for len(added) > 0 {
		k := min(500, len(added))
		ch := added[:k]
		added = added[k:]
		err := queryEach(ctx, rc.DB.R, `SELECT c.device_id, d.display_name, d.hostname, d.primary_ip, d.primary_mac, c.cve_id,
			MAX(c.product), MAX(c.version), COALESCE(MAX(n.cvss_score), MAX(c.cvss_score)), MAX(e.score),
			MAX(k.date_added), MAX(k.due_date), MAX(k.ransomware), MAX(k.name)
			FROM device_cves c
			JOIN devices d ON d.id = c.device_id
			JOIN nvd_kev k ON k.cve_id = c.cve_id
			LEFT JOIN nvd_cves n ON n.id = c.cve_id
			LEFT JOIN nvd_epss e ON e.cve_id = c.cve_id
			WHERE c.gone_at IS NULL AND d.state <> 'ignored' AND c.cve_id IN (`+db.Placeholders(len(ch))+`)
			AND NOT EXISTS (SELECT 1 FROM cve_ignores i WHERE i.device_id = c.device_id AND i.cve_id = c.cve_id)
			GROUP BY c.device_id, c.cve_id ORDER BY c.device_id, c.cve_id`, db.StringArgs(ch), func(r *sql.Rows) error {
			var (
				h                   hitRow
				disp, host, ip, mac string
			)
			if err := r.Scan(&h.dev, &disp, &host, &ip, &mac, &h.cve, &h.product, &h.version, &h.score, &h.epss,
				&h.dateAdded, &h.due, &h.ransom, &h.desc); err != nil {
				return err
			}
			h.name = deviceName(disp, host, ip, mac, h.dev)
			hits = append(hits, h)
			return nil
		})
		if err != nil {
			return 0, err
		}
	}
	n := 0
	for _, h := range hits {
		var cvss, epss any
		if h.score.Valid {
			cvss = h.score.Float64
		}
		if h.epss.Valid {
			epss = h.epss.Float64
		}
		var msg strings.Builder
		fmt.Fprintf(&msg, "CISA führt %s seit %s als aktiv ausgenutzt", h.cve, h.dateAdded)
		if h.due != "" {
			fmt.Fprintf(&msg, " (Frist für US-Behörden: %s)", h.due)
		}
		msg.WriteString(".")
		if h.ransom {
			msg.WriteString(" Die Lücke wird auch von Ransomware genutzt.")
		}
		if h.product != "" {
			fmt.Fprintf(&msg, "\nBetroffen: %s %s", h.product, h.version)
		}
		if h.desc != "" {
			fmt.Fprintf(&msg, "\n%s", h.desc)
		}
		id, err := rc.Events.Emit(ctx, plugin.Event{Type: plugin.EvCVEExploited, Severity: plugin.SevCritical, DeviceID: h.dev, RunID: rc.RunID,
			Title:   fmt.Sprintf("%s auf %s wird aktiv ausgenutzt", h.cve, h.name),
			Message: msg.String(),
			Payload: map[string]any{"cve": h.cve, "cvss": cvss, "epss": epss, "product": h.product, "version": h.version,
				"date_added": h.dateAdded, "due_date": h.due, "ransomware": h.ransom},
			DedupKey: fmt.Sprintf("cve.exploited:%d:%s", h.dev, h.cve)})
		if err != nil {
			rc.Log.Warn("Event konnte nicht gespeichert werden", "type", plugin.EvCVEExploited, "error", err)
			continue
		}
		if id > 0 {
			n++
		}
	}
	return n, nil
}
