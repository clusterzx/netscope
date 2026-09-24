package federation

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"netscope/internal/bus"
	"netscope/internal/db"
	"netscope/internal/federation/wire"
	"netscope/internal/settings"
)

// Delivery limits of a site.
const (
	maxBatchItems = 500
	maxBatchBytes = 4 << 20
	// maxBuffered caps the outbox while the central instance is unreachable; beyond it
	// the state items are dropped and a full synchronisation follows.
	maxBuffered    = 250000
	heartbeat      = time.Minute
	debounce       = 2 * time.Second
	maxBackoff     = 5 * time.Minute
	requestTimeout = 2 * time.Minute
)

// stream is the current delivery stream of a site.
type stream struct {
	Epoch    string    `json:"epoch"`
	Started  time.Time `json:"started"`
	Complete bool      `json:"complete"` // the full synchronisation is queued
}

// siteState is the delivery state of a site (memory only).
type siteState struct {
	mu          sync.Mutex
	kickCh      chan struct{}
	connected   bool
	lastAttempt *time.Time
	lastSuccess *time.Time
	lastError   string
	siteName    string
	failures    int
	retryAt     time.Time
}

func newSiteState() *siteState { return &siteState{kickCh: make(chan struct{}, 1)} }

func (st *siteState) kick() {
	select {
	case st.kickCh <- struct{}{}:
	default:
	}
}

func (st *siteState) reset() {
	st.mu.Lock()
	st.connected, st.lastError, st.siteName, st.failures, st.retryAt = false, "", "", 0, time.Time{}
	st.lastAttempt, st.lastSuccess = nil, nil
	st.mu.Unlock()
}

func (st *siteState) succeeded(site string) {
	now := time.Now()
	st.mu.Lock()
	st.connected, st.lastError, st.siteName, st.failures, st.retryAt = true, "", site, 0, time.Time{}
	st.lastAttempt, st.lastSuccess = &now, &now
	st.mu.Unlock()
}

// failed records an error and returns the delay before the next attempt.
func (st *siteState) failed(err error) time.Duration {
	now := time.Now()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.connected, st.lastError = false, err.Error()
	st.lastAttempt = &now
	st.failures++
	d := min(time.Duration(1<<uint(min(st.failures, 9)))*time.Second*5/4, maxBackoff)
	if errors.Is(err, errRejected) {
		d = maxBackoff
	}
	st.retryAt = now.Add(d)
	return d
}

// SiteStatus is the delivery state shown in the settings of a site.
type SiteStatus struct {
	Active      bool       `json:"active"`
	Connected   bool       `json:"connected"`
	SiteName    string     `json:"siteName,omitempty"` // name at the central instance
	LastAttempt *time.Time `json:"lastAttempt,omitempty"`
	LastSuccess *time.Time `json:"lastSuccess,omitempty"`
	LastError   string     `json:"lastError,omitempty"`
	Buffered    int        `json:"buffered"`
	OldestItem  *time.Time `json:"oldestItem,omitempty"`
	Syncing     bool       `json:"syncing"`
	NextAttempt *time.Time `json:"nextAttempt,omitempty"`
}

// SiteStatus returns the delivery state of this site.
func (s *Service) SiteStatus(ctx context.Context) SiteStatus {
	out := SiteStatus{Active: s.Active()}
	s.site.mu.Lock()
	out.Connected, out.SiteName, out.LastAttempt, out.LastSuccess, out.LastError = s.site.connected, s.site.siteName,
		s.site.lastAttempt, s.site.lastSuccess, s.site.lastError
	if !s.site.retryAt.IsZero() && s.site.retryAt.After(time.Now()) {
		t := s.site.retryAt
		out.NextAttempt = &t
	}
	s.site.mu.Unlock()
	out.Buffered, out.OldestItem = s.buffered(ctx)
	var st stream
	if ok, _ := settings.GetJSON(ctx, s.DB.R, keyStream, &st); ok {
		out.Syncing = !st.Complete
	}
	return out
}

func (s *Service) buffered(ctx context.Context) (int, *time.Time) {
	var (
		n      int
		oldest sql.NullInt64
	)
	_ = s.DB.R.QueryRowContext(ctx, "SELECT IFNULL(MAX(seq) - MIN(seq) + 1, 0), MIN(at) FROM federation_outbox").Scan(&n, &oldest)
	return n, db.NullTime(oldest)
}

// deliveryLoop sends the outbox to the central instance: shortly after new items, at
// least every minute (heartbeat with the site status), with backoff after errors.
func (s *Service) deliveryLoop() {
	defer s.wg.Done()
	wait := 3 * time.Second
	for {
		timer := time.NewTimer(wait)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-s.site.kickCh:
			timer.Stop()
			s.site.mu.Lock()
			retry := s.site.retryAt
			s.site.mu.Unlock()
			if d := time.Until(retry); d > 0 {
				wait = d
				continue
			}
			// collect what follows (a scan delivers many observations at once)
			select {
			case <-s.ctx.Done():
				return
			case <-time.After(debounce):
			}
		case <-timer.C:
		}
		if !s.Active() {
			wait = time.Hour // woken by a settings change
			continue
		}
		wait = s.deliver(s.ctx)
	}
}

// deliver sends batches until the outbox is empty and returns the delay until the next
// round.
func (s *Service) deliver(ctx context.Context) time.Duration {
	cfg, token, ok := s.siteConfig()
	if !ok {
		return time.Hour
	}
	st, err := s.ensureStream(ctx)
	if err != nil {
		s.Log.Error("Verbund: Abgleich vorbereiten", "err", err)
		return s.site.failed(err)
	}
	if err := s.capOutbox(ctx); err != nil {
		s.Log.Error("Verbund: Puffer begrenzen", "err", err)
	}
	for round := 0; round < 40; round++ {
		if ctx.Err() != nil {
			return time.Second
		}
		items, err := s.readOutbox(ctx)
		if err != nil {
			return s.site.failed(err)
		}
		batch := wire.Batch{Protocol: wire.Protocol, Epoch: st.Epoch, Instance: s.instance(), Items: items}
		if round == 0 {
			batch.Status = s.collectStatus(ctx, !st.Complete)
		}
		resp, err := s.post(ctx, cfg, token, &batch)
		if err != nil {
			d := s.site.failed(err)
			s.Log.Warn("Verbund: Zustellung an die Zentrale fehlgeschlagen", "err", err, "retry", d.Round(time.Second).String())
			return d
		}
		s.site.succeeded(resp.Site)
		if resp.Acked > 0 {
			if _, err := s.DB.W.ExecContext(ctx, "DELETE FROM federation_outbox WHERE seq <= ?", resp.Acked); err != nil {
				return s.site.failed(err)
			}
		}
		if resp.Resync {
			s.Log.Warn("Verbund: Die Zentrale verlangt einen vollständigen Abgleich")
			if st, err = s.startStream(ctx, false); err != nil {
				return s.site.failed(err)
			}
			continue
		}
		if len(items) > 0 && items[len(items)-1].Seq > resp.Acked {
			// the central instance stopped early (busy): continue a little later
			return 10 * time.Second
		}
		if len(items) < maxBatchItems {
			// drained: new items wake the loop, otherwise the next heartbeat
			if n, _ := s.buffered(ctx); n > 0 {
				return debounce
			}
			return heartbeat
		}
	}
	return time.Second
}

func (s *Service) readOutbox(ctx context.Context) ([]wire.Item, error) {
	rows, err := s.DB.R.QueryContext(ctx, "SELECT seq, kind, at, data FROM federation_outbox ORDER BY seq LIMIT ?", maxBatchItems)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []wire.Item{}
	size := 0
	for rows.Next() {
		var (
			it   wire.Item
			at   int64
			data string
		)
		if err := rows.Scan(&it.Seq, &it.Kind, &at, &data); err != nil {
			return nil, err
		}
		if len(items) > 0 && size+len(data) > maxBatchBytes {
			break
		}
		size += len(data)
		it.At, it.Data = db.Time(at), json.RawMessage(data)
		items = append(items, it)
	}
	return items, rows.Err()
}

// capOutbox drops the state items of an overfull outbox (the central instance has been
// unreachable for long) and starts a full synchronisation; events are kept.
func (s *Service) capOutbox(ctx context.Context) error {
	n, _ := s.buffered(ctx)
	if n <= maxBuffered {
		return nil
	}
	s.Log.Warn("Verbund: Puffer voll – Beobachtungen verworfen, vollständiger Abgleich folgt", "items", n)
	_, err := s.startStream(ctx, false)
	return err
}

func (s *Service) currentStream(ctx context.Context) (stream, bool, error) {
	var st stream
	ok, err := settings.GetJSON(ctx, s.DB.R, keyStream, &st)
	return st, ok && st.Epoch != "", err
}

// ensureStream returns the current stream and starts one (with a full synchronisation)
// on first contact, after a restore and after an interrupted synchronisation.
func (s *Service) ensureStream(ctx context.Context) (stream, error) {
	var restored bool
	if _, err := settings.GetJSON(ctx, s.DB.R, KeyRestored, &restored); err != nil {
		return stream{}, err
	}
	st, ok, err := s.currentStream(ctx)
	if err != nil {
		return stream{}, err
	}
	if ok && st.Complete && !restored {
		return st, nil
	}
	return s.startStream(ctx, restored)
}

// dropStream forgets the stream (next delivery starts a new one) and the outbox.
func (s *Service) dropStream(ctx context.Context) error {
	return s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, "DELETE FROM federation_outbox"); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, "DELETE FROM settings WHERE key = ?", keyStream)
		return err
	})
}

// Resync starts a new stream with a full synchronisation now.
func (s *Service) Resync(ctx context.Context) error {
	if !s.Active() {
		return errInactive
	}
	if _, err := s.startStream(ctx, false); err != nil {
		return err
	}
	s.site.kick()
	return nil
}

// startStream begins a new delivery stream: the queued state items are replaced by the
// complete state of all devices (events stay queued unless reset). reset tells the
// central instance that device ids changed (restored database).
func (s *Service) startStream(ctx context.Context, reset bool) (stream, error) {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return stream{}, err
	}
	st := stream{Epoch: hex.EncodeToString(b), Started: time.Now()}
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		// queued events stay (renumbered: a stream is numbered without gaps)
		type kept struct {
			at   int64
			data string
		}
		var evs []kept
		if !reset {
			if err := eachRow(ctx, tx, "SELECT at, data FROM federation_outbox WHERE kind = 'event' ORDER BY seq", func(r *sql.Rows) error {
				var k kept
				if err := r.Scan(&k.at, &k.data); err != nil {
					return err
				}
				evs = append(evs, k)
				return nil
			}); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM federation_outbox"); err != nil {
			return err
		}
		for _, k := range evs {
			if _, err := tx.ExecContext(ctx, "INSERT INTO federation_outbox(kind, at, data) VALUES ('event', ?, ?)", k.at, k.data); err != nil {
				return err
			}
		}
		if _, err := tx.ExecContext(ctx, "DELETE FROM settings WHERE key = ?", KeyRestored); err != nil {
			return err
		}
		if err := settings.SetJSON(ctx, tx, keyStream, st); err != nil {
			return err
		}
		return s.Enqueue(ctx, tx, wire.KindSync, st.Started, wire.Sync{Phase: wire.SyncBegin, Reset: reset})
	})
	if err != nil {
		return stream{}, err
	}
	started := time.Now()
	ids, err := s.Inventory.DeviceIDs(ctx)
	if err != nil {
		return stream{}, err
	}
	var present []int64
	for _, id := range ids {
		obs, presence, ok, err := s.Inventory.SnapshotDevice(ctx, id)
		if err != nil {
			return stream{}, err
		}
		if !ok {
			continue // deleted meanwhile (the delete is queued)
		}
		present = append(present, id)
		now := time.Now()
		if err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
			for _, o := range obs {
				if err := s.Enqueue(ctx, tx, wire.KindObservation, now, o); err != nil {
					return err
				}
			}
			return s.Enqueue(ctx, tx, wire.KindPresence, now, presence)
		}); err != nil {
			return stream{}, err
		}
	}
	for _, id := range present {
		rels, err := s.Inventory.SnapshotRelations(ctx, id)
		if err != nil {
			return stream{}, err
		}
		if len(rels) == 0 {
			continue
		}
		now := time.Now()
		if err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
			for _, o := range rels {
				if err := s.Enqueue(ctx, tx, wire.KindObservation, now, o); err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return stream{}, err
		}
	}
	st.Complete = true
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		if err := s.Enqueue(ctx, tx, wire.KindSync, time.Now(), wire.Sync{Phase: wire.SyncEnd, Devices: present}); err != nil {
			return err
		}
		return settings.SetJSON(ctx, tx, keyStream, st)
	})
	if err != nil {
		return stream{}, err
	}
	s.Log.Info("Verbund: vollständiger Abgleich vorbereitet", "devices", len(present), "reset", reset,
		"duration", time.Since(started).Round(time.Millisecond).String())
	return st, nil
}

func (s *Service) instance() wire.Instance {
	host, _ := os.Hostname()
	return wire.Instance{Version: s.Version, URL: s.Settings.System().PublicURL, Hostname: host, StartedAt: s.StartedAt}
}

// collectStatus describes this site for the central site overview.
func (s *Service) collectStatus(ctx context.Context, syncing bool) *wire.Status {
	st := &wire.Status{Subnets: []wire.Subnet{}, Plugins: []wire.PluginStatus{}, Syncing: syncing}
	st.Buffered, st.OldestItem = s.buffered(ctx)
	var online sql.NullInt64
	_ = s.DB.R.QueryRowContext(ctx, "SELECT COUNT(*), SUM(online) FROM devices").Scan(&st.Devices, &online)
	st.Online = int(online.Int64)
	if list, err := s.Inventory.ListSubnets(ctx); err == nil {
		for _, sn := range list {
			st.Subnets = append(st.Subnets, wire.Subnet{CIDR: sn.CIDR, Name: sn.Name, Gateway: sn.Gateway, Access: sn.Access,
				Enabled: sn.Enabled, Devices: sn.DeviceCount})
		}
	}
	if s.Host != nil {
		if views, err := s.Host.Views(ctx); err == nil {
			for _, v := range views {
				if v.Config == nil || !v.Config.Enabled {
					continue
				}
				ps := wire.PluginStatus{ID: v.Info.ID, Name: v.Info.Name, Scheduled: v.Config.Schedule != ""}
				if r := v.LastRun; r != nil {
					ps.LastRun, ps.Status, ps.Error = r.FinishedAt, r.Status, r.Error
				}
				st.Plugins = append(st.Plugins, ps)
			}
		}
	}
	return st
}

// errRejected: the central instance refused the token (no fast retries).
var errRejected = errors.New("die Zentrale lehnt das Token ab – Token in der Zentrale neu erzeugen und hier eintragen")

// post delivers one batch.
func (s *Service) post(ctx context.Context, cfg Settings, token string, batch *wire.Batch) (*wire.Response, error) {
	raw, err := json.Marshal(batch)
	if err != nil {
		return nil, err
	}
	var body bytes.Buffer
	zw := gzip.NewWriter(&body)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, requestTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.CentralURL+wire.IngestPath, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Content-Encoding", "gzip")
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("User-Agent", "NetScope/"+s.Version)
	resp, err := httpClient(cfg.Fingerprint).Do(req)
	if err != nil {
		return nil, fmt.Errorf("Zentrale nicht erreichbar: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	switch resp.StatusCode {
	case http.StatusOK:
		var out wire.Response
		if err := json.Unmarshal(data, &out); err != nil {
			return nil, fmt.Errorf("Antwort der Zentrale unlesbar: %w", err)
		}
		return &out, nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, errRejected
	}
	msg := strings.TrimSpace(string(data))
	var apiErr struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &apiErr) == nil && apiErr.Error.Message != "" {
		msg = apiErr.Error.Message
	}
	if len(msg) > 300 {
		msg = msg[:300] + "…"
	}
	if resp.StatusCode == http.StatusNotFound && msg == "" {
		msg = "kein NetScope unter dieser Adresse oder Version ohne Verbund"
	}
	return nil, fmt.Errorf("Zentrale antwortet mit HTTP %d: %s", resp.StatusCode, msg)
}

// httpClient returns a client; with a fingerprint the certificate of the central
// instance is pinned instead of verified against the system roots.
func httpClient(fingerprint string) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if fingerprint != "" {
		want := fingerprint
		tr.TLSClientConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true, //nolint:gosec // replaced by the pinned fingerprint below
			VerifyConnection: func(cs tls.ConnectionState) error {
				if len(cs.PeerCertificates) == 0 {
					return errors.New("kein Zertifikat")
				}
				if got := CertFingerprint(cs.PeerCertificates[0]); got != want {
					return fmt.Errorf("Zertifikat der Zentrale passt nicht zum hinterlegten Fingerprint (erhalten %s)", got)
				}
				return nil
			},
		}
	}
	return &http.Client{Transport: tr, Timeout: requestTimeout}
}

// CertFingerprint is the SHA-256 fingerprint of a certificate (hex, lower case).
func CertFingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return hex.EncodeToString(sum[:])
}

// TestResult is the outcome of a connection test.
type TestResult struct {
	OK       bool   `json:"ok"`
	Site     string `json:"site,omitempty"` // name at the central instance
	Error    string `json:"error,omitempty"`
	Duration int64  `json:"durationMs"`
}

// Test sends a heartbeat (no items) to the central instance.
func (s *Service) Test(ctx context.Context) TestResult {
	cfg, token, ok := s.siteConfig()
	if !ok {
		return TestResult{Error: errInactive.Error()}
	}
	start := time.Now()
	st, _, _ := s.currentStream(ctx)
	batch := wire.Batch{Protocol: wire.Protocol, Epoch: st.Epoch, Instance: s.instance(), Items: []wire.Item{},
		Status: s.collectStatus(ctx, !st.Complete)}
	resp, err := s.post(ctx, cfg, token, &batch)
	res := TestResult{Duration: time.Since(start).Milliseconds()}
	if err != nil {
		res.Error = err.Error()
		s.site.failed(err)
		return res
	}
	s.site.succeeded(resp.Site)
	s.site.kick()
	s.Bus.Publish(bus.TopicSystem, "federation", map[string]any{"connected": true})
	res.OK, res.Site = true, resp.Site
	return res
}

func eachRow(ctx context.Context, q db.Querier, query string, fn func(*sql.Rows) error, args ...any) error {
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
