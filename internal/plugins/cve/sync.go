package cve

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

const (
	// pseudo rows in nvd_feeds holding plugin state (never shown as feeds)
	stateSync  = "_sync"
	stateMatch = "_match"

	batchSize = 500
	// The modified feed covers the last 8 days; after a longer gap every yearly feed is
	// checked again.
	incrementalMaxAge = 7 * 24 * time.Hour
)

// feedState is a row of nvd_feeds.
type feedState struct {
	Name         string
	LastModified string
	SHA256       string
	Size         int64
	CVECount     int
	SyncedAt     int64
	Status       string
	Error        string
}

func loadFeedStates(ctx context.Context, q db.Querier) (map[string]feedState, error) {
	rows, err := q.QueryContext(ctx, `SELECT name, last_modified, sha256, size, cve_count, synced_at, status, error FROM nvd_feeds`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]feedState{}
	for rows.Next() {
		var (
			s      feedState
			synced sql.NullInt64
		)
		if err := rows.Scan(&s.Name, &s.LastModified, &s.SHA256, &s.Size, &s.CVECount, &synced, &s.Status, &s.Error); err != nil {
			return nil, err
		}
		s.SyncedAt = synced.Int64
		out[s.Name] = s
	}
	return out, rows.Err()
}

func setFeedStatus(ctx context.Context, d *db.DB, name, status, msg string) error {
	_, err := d.W.ExecContext(ctx, `INSERT INTO nvd_feeds(name, status, error) VALUES (?,?,?)
		ON CONFLICT(name) DO UPDATE SET status = excluded.status, error = excluded.error`, name, status, msg)
	return err
}

// saveFeedOK records a successful check (count < 0: the feed was unchanged and not read).
func saveFeedOK(ctx context.Context, d *db.DB, name string, m *feedMeta, size int64, count int, now int64) error {
	if count < 0 {
		_, err := d.W.ExecContext(ctx, `UPDATE nvd_feeds SET last_modified = ?, synced_at = ?, status = 'ok', error = '' WHERE name = ?`,
			m.LastModified, now, name)
		return err
	}
	_, err := d.W.ExecContext(ctx, `INSERT INTO nvd_feeds(name, last_modified, sha256, size, cve_count, synced_at, status, error)
		VALUES (?,?,?,?,?,?,'ok','') ON CONFLICT(name) DO UPDATE SET last_modified = excluded.last_modified, sha256 = excluded.sha256,
		size = excluded.size, cve_count = excluded.cve_count, synced_at = excluded.synced_at, status = 'ok', error = ''`,
		name, m.LastModified, m.SHA256, size, count, now)
	return err
}

// syncStats summarises a sync.
type syncStats struct {
	Mode       string
	Checked    int
	Downloaded int
	Bytes      int64
	Read       int // CVE records read from downloaded feeds (in range, not rejected)
	Written    int // CVE rows inserted or replaced (CVEs with CPE data only)
	Deleted    int
	Failed     []string
	// Changed holds CVEs that are new or were modified by the NVD since they were first
	// loaded. Initial loads of a feed are not recorded (no events for history).
	Changed map[string]struct{}
}

// minCVEYear returns the lowest CVE ID year kept for a start_year setting (0 = all).
func minCVEYear(startYear int) int {
	if startYear <= firstFeedYear {
		return 0
	}
	return startYear
}

// inRange reports whether a CVE ID belongs to the mirrored years.
func inRange(id string, minYear int) bool {
	if minYear == 0 {
		return true
	}
	parts := strings.SplitN(id, "-", 3)
	if len(parts) < 3 {
		return false
	}
	y, err := strconv.Atoi(parts[1])
	return err == nil && y >= minYear
}

func (p *Plugin) fetcher(rc *plugin.RunContext, cfg config) *fetcher {
	client := p.client
	if client == nil {
		client = &http.Client{Timeout: cfg.httpTimeout}
	}
	dir := rc.DataDir
	if dir == "" {
		dir = os.TempDir()
	}
	ua := "NetScope"
	if rc.Env.Version != "" {
		ua += "/" + rc.Env.Version
	}
	return &fetcher{client: client, base: cfg.baseURL, dir: dir, keep: cfg.keepDownloads, userAgent: ua + " (NVD feed mirror)",
		attempts: p.attempts, retryDelay: p.retryDelay}
}

// sync brings the local NVD mirror up to date. force re-downloads every feed and
// rewrites all CVEs regardless of hashes and modification dates.
func (p *Plugin) sync(ctx context.Context, rc *plugin.RunContext, cfg config, force bool) (*syncStats, error) {
	if !p.syncMu.TryLock() {
		return nil, errors.New("eine NVD-Synchronisation läuft bereits")
	}
	defer p.syncMu.Unlock()
	d := rc.DB
	now := p.now()
	st := &syncStats{Changed: map[string]struct{}{}}
	minYear := minCVEYear(cfg.startYear)
	deleted, err := pruneYears(ctx, d, minYear)
	if err != nil {
		return st, fmt.Errorf("alte Jahrgänge entfernen: %w", err)
	}
	st.Deleted += deleted
	states, err := loadFeedStates(ctx, d.R)
	if err != nil {
		return st, err
	}
	curYear := now.UTC().Year()
	var (
		years    []string
		missing  []string
		loaded   = 0
		lastFull int64
	)
	for y := max(cfg.startYear, firstFeedYear); y <= curYear; y++ {
		name := strconv.Itoa(y)
		years = append(years, name)
		s := states[name]
		if s.SHA256 == "" {
			missing = append(missing, name)
			continue
		}
		loaded++
		if lastFull == 0 || s.SyncedAt < lastFull {
			lastFull = s.SyncedAt
		}
	}
	if loaded < len(years) {
		lastFull = 0
	}
	lastCurrent := lastFull
	if m := states[feedModified]; m.Status == "ok" && m.SHA256 != "" && m.SyncedAt > lastCurrent {
		lastCurrent = m.SyncedAt
	}
	var plan []string
	switch {
	case loaded == 0:
		st.Mode = "initial"
		plan = years
	case force || lastCurrent == 0 || now.Sub(time.UnixMilli(lastCurrent)) > incrementalMaxAge:
		st.Mode = "full"
		plan = years
	default:
		st.Mode = "incremental"
		plan = missing
	}
	plan = append(plan, feedModified)
	rc.Log.Info("NVD-Synchronisation", "modus", st.Mode, "feeds", len(plan), "ab_jahr", cfg.startYear)
	f := p.fetcher(rc, cfg)
	f.removeStale()
	var errs []error
	for i, name := range plan {
		if err := ctx.Err(); err != nil {
			return st, err
		}
		progress := func(done, total int) {
			frac := 0
			if total > 0 {
				frac = min(done*1000/total, 1000)
			}
			rc.Progress(i*1000+frac, len(plan)*1000)
		}
		progress(0, 1)
		err := p.syncFeed(ctx, rc, f, name, states[name], minYear, force, curYear, st, progress)
		if err == nil {
			continue
		}
		if ctx.Err() != nil {
			_ = setFeedStatus(context.WithoutCancel(ctx), d, name, "error", "abgebrochen")
			return st, ctx.Err()
		}
		msg := err.Error()
		rc.Log.Error("NVD-Feed fehlgeschlagen", "feed", name, "error", msg)
		_ = setFeedStatus(ctx, d, name, "error", msg)
		st.Failed = append(st.Failed, name)
		errs = append(errs, fmt.Errorf("Feed %s: %w", name, err))
		if rc.Events != nil {
			if _, eerr := rc.Events.Emit(ctx, plugin.Event{Type: plugin.EvNVDSyncFailed, RunID: rc.RunID,
				Title: "NVD-Sync fehlgeschlagen (Feed " + name + ")", Message: msg,
				Payload:  map[string]any{"error": msg, "feed": name},
				DedupKey: "nvd.sync_failed:" + name, DedupWindow: 12 * time.Hour}); eerr != nil {
				rc.Log.Warn("Event konnte nicht gespeichert werden", "error", eerr)
			}
		}
	}
	rc.Progress(len(plan)*1000, len(plan)*1000)
	syncErr := errors.Join(errs...)
	if err := saveSyncState(ctx, d, st, syncErr, p.now().UnixMilli()); err != nil {
		rc.Log.Warn("Sync-Status nicht gespeichert", "error", err)
	}
	rc.SetStat("sync_mode", st.Mode)
	rc.SetStat("feeds_checked", st.Checked)
	rc.SetStat("feeds_downloaded", st.Downloaded)
	rc.SetStat("download_bytes", st.Bytes)
	rc.SetStat("cves_read", st.Read)
	rc.SetStat("cves_written", st.Written)
	rc.SetStat("cves_changed", len(st.Changed))
	if len(st.Failed) > 0 {
		rc.SetStat("feeds_failed", strings.Join(st.Failed, ", "))
	}
	return st, syncErr
}

// syncFeed checks one feed and imports it if it changed.
func (p *Plugin) syncFeed(ctx context.Context, rc *plugin.RunContext, f *fetcher, name string, prev feedState, minYear int, force bool,
	curYear int, st *syncStats, progress func(done, total int)) error {
	d := rc.DB
	if err := setFeedStatus(ctx, d, name, "syncing", ""); err != nil {
		return err
	}
	m, err := f.meta(ctx, name)
	if err != nil {
		now := p.now().UTC()
		if isNotFound(err) && name == strconv.Itoa(curYear) && now.Month() == time.January && now.Day() <= 7 {
			// the feed of a new year appears a few days into January
			rc.Log.Info("Feed des neuen Jahres noch nicht verfügbar", "feed", name)
			return setFeedStatus(ctx, d, name, "pending", "noch nicht veröffentlicht")
		}
		return fmt.Errorf("Meta-Datei: %w", err)
	}
	st.Checked++
	if !force && prev.SHA256 == m.SHA256 {
		return saveFeedOK(ctx, d, name, m, 0, -1, p.now().UnixMilli())
	}
	path, n, err := f.download(ctx, name, m)
	if err != nil {
		return err
	}
	defer f.release(path)
	st.Downloaded++
	st.Bytes += n
	// changes are news only if the feed had been imported before
	track := name == feedModified || prev.SHA256 != ""
	start := time.Now()
	read, written, err := applyFeed(ctx, d, path, minYear, track, force, st.Changed, progress)
	st.Read += read
	st.Written += written
	if err != nil {
		return err
	}
	rc.Log.Info("NVD-Feed importiert", "feed", name, "cves", read, "geschrieben", written, "bytes", n,
		"dauer", time.Since(start).Round(time.Millisecond).String())
	return saveFeedOK(ctx, d, name, m, n, read, p.now().UnixMilli())
}

// applyFeed streams a downloaded feed into the database in batches.
func applyFeed(ctx context.Context, d *db.DB, path string, minYear int, track, force bool, changed map[string]struct{},
	progress func(done, total int)) (read, written int, err error) {
	rd, err := openFeed(path)
	if err != nil {
		return 0, 0, err
	}
	defer rd.Close()
	total := 0
	seen := 0
	batch := make([]*cveRecord, 0, batchSize)
	flush := func() error {
		w, err := writeBatch(ctx, d, batch, track, force, changed)
		written += w
		batch = batch[:0]
		if progress != nil {
			progress(seen, total)
		}
		return err
	}
	_, err = parseFeed(ctx, rd, func(n int) { total = n }, func(r *cveRecord) error {
		seen++
		if !inRange(r.ID, minYear) {
			return nil
		}
		if !r.Rejected {
			read++
		}
		batch = append(batch, r)
		if len(batch) >= batchSize {
			return flush()
		}
		return nil
	})
	if err == nil && len(batch) > 0 {
		err = flush()
	}
	return read, written, err
}

// writeBatch replaces the stored CVEs of a batch in one transaction. A CVE is only
// rewritten if the incoming record is newer than the stored one (or force is set);
// rejected CVEs are deleted.
func writeBatch(ctx context.Context, d *db.DB, batch []*cveRecord, track, force bool, changed map[string]struct{}) (int, error) {
	written := 0
	var news []string
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		prep := func(q string) (*sql.Stmt, error) { return tx.PrepareContext(ctx, q) }
		sel, err := prep("SELECT last_modified FROM nvd_cves WHERE id = ?")
		if err != nil {
			return err
		}
		defer sel.Close()
		delM, err := prep("DELETE FROM nvd_cpe_matches WHERE cve_id = ?")
		if err != nil {
			return err
		}
		defer delM.Close()
		delC, err := prep("DELETE FROM nvd_cves WHERE id = ?")
		if err != nil {
			return err
		}
		defer delC.Close()
		insC, err := prep(`INSERT OR REPLACE INTO nvd_cves(id, published, last_modified, status, cvss_score, cvss_vector, cvss_version,
			severity, description, refs, cwes) VALUES (?,?,?,?,?,?,?,?,?,?,?)`)
		if err != nil {
			return err
		}
		defer insC.Close()
		insM, err := prep(`INSERT INTO nvd_cpe_matches(cve_id, part, vendor, product, version, upd, start_incl, start_excl, end_incl, end_excl, vulnerable)
			VALUES (?,?,?,?,?,?,?,?,?,?,1)`)
		if err != nil {
			return err
		}
		defer insM.Close()
		for _, r := range batch {
			var stored sql.NullInt64
			err := sel.QueryRowContext(ctx, r.ID).Scan(&stored)
			exists := err == nil
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
			// Rejected CVEs and CVEs without vulnerable CPE data (not analysed or deferred by
			// the NVD, about half of the recent ones) can never match a device and are not
			// stored. They are picked up as soon as the NVD adds configurations.
			if r.Rejected || len(r.Matches) == 0 {
				if exists {
					if _, err := delM.ExecContext(ctx, r.ID); err != nil {
						return err
					}
					if _, err := delC.ExecContext(ctx, r.ID); err != nil {
						return err
					}
				}
				continue
			}
			if exists && (stored.Int64 > r.LastModified || (stored.Int64 == r.LastModified && !force)) {
				continue
			}
			if exists {
				if _, err := delM.ExecContext(ctx, r.ID); err != nil {
					return err
				}
			}
			refs := r.Refs
			if refs == nil {
				refs = []Reference{}
			}
			var score any
			if r.Score != nil {
				score = *r.Score
			}
			if _, err := insC.ExecContext(ctx, r.ID, nullMs(r.Published), nullMs(r.LastModified), r.Status, score, r.Vector, r.CVSSVersion,
				r.Severity, r.Description, db.JSON(refs), db.JSON(r.CWEs)); err != nil {
				return err
			}
			for _, m := range r.Matches {
				if _, err := insM.ExecContext(ctx, r.ID, m.Part, m.Vendor, m.Product, m.Version, m.Update,
					m.StartIncl, m.StartExcl, m.EndIncl, m.EndExcl); err != nil {
					return err
				}
			}
			written++
			if track && (!exists || stored.Int64 != r.LastModified) {
				news = append(news, r.ID)
			}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	for _, id := range news {
		changed[id] = struct{}{}
	}
	return written, nil
}

func nullMs(ms int64) any {
	if ms == 0 {
		return nil
	}
	return ms
}

// pruneYears removes CVEs (and feed rows) older than the configured start year.
func pruneYears(ctx context.Context, d *db.DB, minYear int) (int, error) {
	if minYear == 0 {
		return 0, nil
	}
	cut := fmt.Sprintf("CVE-%d-", minYear)
	if _, err := d.W.ExecContext(ctx, `DELETE FROM nvd_feeds WHERE name GLOB '[0-9][0-9][0-9][0-9]' AND CAST(name AS INTEGER) < ?`, minYear); err != nil {
		return 0, err
	}
	total := 0
	for {
		res, err := d.W.ExecContext(ctx, `DELETE FROM nvd_cpe_matches WHERE rowid IN
			(SELECT rowid FROM nvd_cpe_matches WHERE cve_id < ? LIMIT 50000)`, cut)
		if err != nil {
			return total, err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			break
		}
	}
	for {
		res, err := d.W.ExecContext(ctx, `DELETE FROM nvd_cves WHERE id IN (SELECT id FROM nvd_cves WHERE id < ? LIMIT 20000)`, cut)
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		if n == 0 {
			break
		}
		total += int(n)
	}
	return total, nil
}

// saveSyncState stores totals and the outcome of the last sync in the "_sync" row:
// last_modified = mode, cve_count = CVEs in the mirror, size = CPE match rows.
func saveSyncState(ctx context.Context, d *db.DB, st *syncStats, syncErr error, now int64) error {
	var cves, matches int64
	if err := d.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM nvd_cves").Scan(&cves); err != nil {
		return err
	}
	if err := d.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM nvd_cpe_matches").Scan(&matches); err != nil {
		return err
	}
	status, msg := "ok", ""
	if syncErr != nil {
		status, msg = "error", syncErr.Error()
	}
	_, err := d.W.ExecContext(ctx, `INSERT INTO nvd_feeds(name, last_modified, size, cve_count, synced_at, status, error) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(name) DO UPDATE SET last_modified = excluded.last_modified, size = excluded.size, cve_count = excluded.cve_count,
		synced_at = excluded.synced_at, status = excluded.status, error = excluded.error`,
		stateSync, st.Mode, matches, cves, now, status, msg)
	return err
}

// mirrorEmpty reports whether no CPE match data has been imported yet.
func mirrorEmpty(ctx context.Context, d *db.DB) (bool, error) {
	var n int
	err := d.R.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM nvd_cpe_matches)").Scan(&n)
	return n == 0, err
}
