package federation

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"netscope/internal/bus"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/federation/wire"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
)

const (
	// connectedWithin: a site that reported within this time counts as connected.
	connectedWithin = 3 * time.Minute
	// downAfter: without a report for this long a site raises site.down.
	downAfter = 5 * time.Minute
)

// central holds the per-site locks of the central instance.
type central struct {
	mu    sync.Mutex
	locks map[int64]*sync.Mutex
}

func newCentral() *central { return &central{locks: map[int64]*sync.Mutex{}} }

func (c *central) lock(site int64) *sync.Mutex {
	c.mu.Lock()
	defer c.mu.Unlock()
	l, ok := c.locks[site]
	if !ok {
		l = &sync.Mutex{}
		c.locks[site] = l
	}
	return l
}

// Site is a site as seen by the central instance.
type Site struct {
	ID          int64      `json:"id"`
	Name        string     `json:"name"`
	Slug        string     `json:"slug"`
	URL         string     `json:"url"`         // configured URL of the site's web UI
	LinkURL     string     `json:"linkUrl"`     // configured or reported URL (deep links)
	TokenPrefix string     `json:"tokenPrefix"` // first characters of the token
	CreatedAt   time.Time  `json:"createdAt"`
	LastContact *time.Time `json:"lastContact,omitempty"`
	LastIP      string     `json:"lastIp,omitempty"`
	Connected   bool       `json:"connected"`
	Down        bool       `json:"down"`
	// Instance and Status are the last report of the site.
	Instance *wire.Instance `json:"instance,omitempty"`
	Status   *wire.Status   `json:"status,omitempty"`
	// Devices and Online count the devices of the site here.
	Devices int `json:"devices"`
	Online  int `json:"online"`
}

// storedStatus is sites.status.
type storedStatus struct {
	Instance   *wire.Instance `json:"instance,omitempty"`
	Status     *wire.Status   `json:"status,omitempty"`
	ReceivedAt time.Time      `json:"receivedAt"`
}

const siteSelect = `SELECT s.id, s.name, s.slug, s.url, s.token_prefix, s.created_at, s.last_contact_at, s.last_ip, s.down, s.status,
	(SELECT COUNT(*) FROM devices d WHERE d.site_id = s.id), (SELECT COUNT(*) FROM devices d WHERE d.site_id = s.id AND d.online = 1)
	FROM sites s`

func scanSite(r interface{ Scan(...any) error }) (*Site, error) {
	var (
		st      Site
		created int64
		contact sql.NullInt64
		status  string
	)
	if err := r.Scan(&st.ID, &st.Name, &st.Slug, &st.URL, &st.TokenPrefix, &created, &contact, &st.LastIP, &st.Down, &status,
		&st.Devices, &st.Online); err != nil {
		return nil, err
	}
	st.CreatedAt, st.LastContact = db.Time(created), db.NullTime(contact)
	st.Connected = st.LastContact != nil && time.Since(*st.LastContact) < connectedWithin
	var ss storedStatus
	if json.Unmarshal([]byte(status), &ss) == nil {
		st.Instance, st.Status = ss.Instance, ss.Status
	}
	st.LinkURL = inventory.SiteURL(st.URL, status)
	return &st, nil
}

// Sites lists the sites (central instance).
func (s *Service) Sites(ctx context.Context) ([]Site, error) {
	rows, err := s.DB.R.QueryContext(ctx, siteSelect+" ORDER BY s.name COLLATE NOCASE")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Site{}
	for rows.Next() {
		st, err := scanSite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *st)
	}
	return out, rows.Err()
}

// Site returns one site.
func (s *Service) Site(ctx context.Context, id int64) (*Site, error) {
	st, err := scanSite(s.DB.R.QueryRowContext(ctx, siteSelect+" WHERE s.id = ?", id))
	return st, db.NotFound(err)
}

// SiteInput creates or changes a site.
type SiteInput struct {
	Name string `json:"name"`
	URL  string `json:"url"`
}

func (in *SiteInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" {
		return plugin.FieldErr("name", "Name erforderlich")
	}
	if len([]rune(in.Name)) > 64 {
		return plugin.FieldErr("name", "höchstens 64 Zeichen")
	}
	if slugify(in.Name) == "" {
		return plugin.FieldErr("name", "Name braucht mindestens einen Buchstaben oder eine Ziffer")
	}
	in.URL = strings.TrimRight(strings.TrimSpace(in.URL), "/")
	if in.URL != "" {
		u, err := url.Parse(in.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return plugin.FieldErr("url", "gültige http(s)-URL erwartet")
		}
	}
	return nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

// slugify turns a name into the identifier used by the filter language (site:colo).
func slugify(name string) string {
	s := strings.NewReplacer("ä", "ae", "ö", "oe", "ü", "ue", "ß", "ss", "Ä", "ae", "Ö", "oe", "Ü", "ue").Replace(name)
	s = slugRe.ReplaceAllString(strings.ToLower(s), "-")
	return strings.Trim(s, "-")
}

// reserved slugs of the filter language
var reservedSlugs = map[string]bool{"local": true, "lokal": true}

func (s *Service) uniqueSlug(ctx context.Context, q db.Querier, name string, except int64) (string, error) {
	base := slugify(name)
	if reservedSlugs[base] {
		base += "-site"
	}
	for i := 1; ; i++ {
		slug := base
		if i > 1 {
			slug = fmt.Sprintf("%s-%d", base, i)
		}
		var n int
		if err := q.QueryRowContext(ctx, "SELECT COUNT(*) FROM sites WHERE slug = ? AND id <> ?", slug, except).Scan(&n); err != nil {
			return "", err
		}
		if n == 0 {
			return slug, nil
		}
	}
}

func newSiteToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return wire.TokenPrefix + base64.RawURLEncoding.EncodeToString(b), nil
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

func uniqueErr(err error) error {
	if err != nil && strings.Contains(err.Error(), "UNIQUE") && strings.Contains(err.Error(), "name") {
		return plugin.FieldErr("name", "Es gibt bereits einen Standort mit diesem Namen")
	}
	return err
}

// CreateSite adds a site and returns it with its token (shown once).
func (s *Service) CreateSite(ctx context.Context, in SiteInput) (*Site, string, error) {
	if err := in.validate(); err != nil {
		return nil, "", err
	}
	token, err := newSiteToken()
	if err != nil {
		return nil, "", err
	}
	var id int64
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		slug, err := s.uniqueSlug(ctx, tx, in.Name, 0)
		if err != nil {
			return err
		}
		now := db.Now()
		res, err := tx.ExecContext(ctx, `INSERT INTO sites(name, slug, token_hash, token_prefix, url, created_at, updated_at) VALUES (?,?,?,?,?,?,?)`,
			in.Name, slug, hashToken(token), token[:10], in.URL, now, now)
		if err != nil {
			return uniqueErr(err)
		}
		id, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	st, err := s.Site(ctx, id)
	s.publishSites("created", id)
	return st, token, err
}

// UpdateSite renames a site or changes its URL.
func (s *Service) UpdateSite(ctx context.Context, id int64, in SiteInput) (*Site, error) {
	if err := in.validate(); err != nil {
		return nil, err
	}
	err := s.DB.Tx(ctx, func(tx *sql.Tx) error {
		slug, err := s.uniqueSlug(ctx, tx, in.Name, id)
		if err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, "UPDATE sites SET name = ?, slug = ?, url = ?, updated_at = ? WHERE id = ?", in.Name, slug, in.URL, db.Now(), id)
		if err != nil {
			return uniqueErr(err)
		}
		if n, _ := res.RowsAffected(); n == 0 {
			return db.ErrNotFound
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publishSites("updated", id)
	return s.Site(ctx, id)
}

// RotateSiteToken replaces the token of a site (the old one stops working at once).
func (s *Service) RotateSiteToken(ctx context.Context, id int64) (string, error) {
	token, err := newSiteToken()
	if err != nil {
		return "", err
	}
	res, err := s.DB.W.ExecContext(ctx, "UPDATE sites SET token_hash = ?, token_prefix = ?, updated_at = ? WHERE id = ?",
		hashToken(token), token[:10], db.Now(), id)
	if err != nil {
		return "", err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return "", db.ErrNotFound
	}
	s.publishSites("updated", id)
	return token, nil
}

// DeleteSite removes a site with all devices and events it delivered.
func (s *Service) DeleteSite(ctx context.Context, id int64) error {
	l := s.central.lock(id)
	l.Lock()
	defer l.Unlock()
	res, err := s.DB.W.ExecContext(ctx, "DELETE FROM sites WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	s.publishSites("deleted", id)
	return nil
}

func (s *Service) publishSites(typ string, id int64) {
	s.Bus.Publish(bus.TopicSystem, "sites", map[string]any{"type": typ, "id": id})
}

// ErrNotCentral is returned by the ingest endpoint of an instance that is not central.
var ErrNotCentral = errors.New("diese NetScope-Instanz ist nicht als Zentrale eingerichtet")

// AuthenticateSite resolves a site token.
func (s *Service) AuthenticateSite(ctx context.Context, token string) (int64, string, error) {
	if s.Role() != RoleCentral {
		return 0, "", ErrNotCentral
	}
	if !strings.HasPrefix(token, wire.TokenPrefix) {
		return 0, "", errRejected
	}
	h := hashToken(token)
	var (
		id           int64
		name, stored string
	)
	if err := s.DB.R.QueryRowContext(ctx, "SELECT id, name, token_hash FROM sites WHERE token_hash = ?", h).Scan(&id, &name, &stored); err != nil {
		return 0, "", errRejected
	}
	if subtle.ConstantTimeCompare([]byte(stored), []byte(h)) != 1 {
		return 0, "", errRejected
	}
	return id, name, nil
}

// ProtocolError is returned for a batch in a protocol version this build does not speak.
type ProtocolError struct{ Got int }

func (e *ProtocolError) Error() string {
	if e.Got > wire.Protocol {
		return fmt.Sprintf("Der Standort spricht Protokoll %d, diese Zentrale nur %d–%d: Zentrale aktualisieren", e.Got, wire.MinProtocol, wire.Protocol)
	}
	return fmt.Sprintf("Der Standort spricht Protokoll %d, diese Zentrale erst ab %d: Standort aktualisieren", e.Got, wire.MinProtocol)
}

// Ingest applies a batch of a site in order and acknowledges the applied items.
func (s *Service) Ingest(ctx context.Context, siteID int64, siteName string, b *wire.Batch, remoteIP string) (*wire.Response, error) {
	if b.Protocol < wire.MinProtocol || b.Protocol > wire.Protocol {
		return nil, &ProtocolError{Got: b.Protocol}
	}
	l := s.central.lock(siteID)
	l.Lock()
	defer l.Unlock()
	var (
		epoch   string
		applied int64
		down    bool
		contact sql.NullInt64
		status  string
	)
	if err := s.DB.R.QueryRowContext(ctx, "SELECT epoch, last_seq, down, last_contact_at, status FROM sites WHERE id = ?", siteID).
		Scan(&epoch, &applied, &down, &contact, &status); err != nil {
		return nil, db.NotFound(err)
	}
	if b.Epoch != "" && b.Epoch != epoch {
		// a new stream of the site: numbering starts over
		epoch, applied = b.Epoch, 0
	}
	resp := &wire.Response{Protocol: wire.Protocol, Site: siteName}
	for _, it := range b.Items {
		if it.Seq <= applied {
			continue // already applied (the acknowledgement got lost)
		}
		if applied > 0 && it.Seq != applied+1 {
			// items are missing (dropped at the site, or this database is older than the
			// acknowledgements): only a full synchronisation restores a consistent state
			s.Log.Warn("Verbund: Lücke in der Lieferung – vollständiger Abgleich angefordert", "site", siteName,
				"expected", applied+1, "got", it.Seq)
			resp.Resync = true
			break
		}
		if err := s.apply(ctx, siteID, siteName, it); err != nil {
			if ctx.Err() != nil || transient(err) {
				s.Log.Warn("Verbund: Verarbeitung unterbrochen", "site", siteName, "seq", it.Seq, "err", err)
				break
			}
			s.Log.Warn("Verbund: Eintrag des Standorts übersprungen", "site", siteName, "seq", it.Seq, "kind", it.Kind, "err", err)
		}
		applied = it.Seq
	}
	resp.Acked = applied
	ss := storedStatus{}
	_ = json.Unmarshal([]byte(status), &ss)
	inst := b.Instance
	ss.Instance = &inst
	if b.Status != nil {
		ss.Status = b.Status
	}
	ss.ReceivedAt = time.Now()
	now := time.Now()
	bg := context.WithoutCancel(ctx)
	if _, err := s.DB.W.ExecContext(bg, `UPDATE sites SET epoch = ?, last_seq = ?, last_contact_at = ?, last_ip = ?, status = ?, down = 0 WHERE id = ?`,
		epoch, applied, now.UnixMilli(), remoteIP, db.JSON(ss), siteID); err != nil {
		return nil, err
	}
	if down {
		payload := map[string]any{"site": siteName, "site_id": siteID}
		if t := db.NullTime(contact); t != nil {
			payload["down_seconds"] = int64(now.Sub(*t).Seconds())
		}
		if _, err := s.Events.Emit(bg, "core", plugin.Event{Type: plugin.EvSiteUp, Title: "Standort " + siteName + " meldet sich wieder",
			Payload: payload}); err != nil {
			s.Log.Error("site.up", "err", err)
		}
	}
	if len(b.Items) > 0 || down {
		s.publishSites("updated", siteID)
	}
	return resp, nil
}

// transient reports errors worth retrying (the database is busy).
func transient(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "SQLITE_BUSY") || errors.Is(err, context.DeadlineExceeded)
}

// apply applies one item of a site.
func (s *Service) apply(ctx context.Context, siteID int64, siteName string, it wire.Item) error {
	at := it.At
	if at.IsZero() || at.After(time.Now()) {
		at = time.Now() // clock of the site ahead
	}
	switch it.Kind {
	case wire.KindObservation:
		var o wire.Observation
		if err := json.Unmarshal(it.Data, &o); err != nil {
			return err
		}
		_, err := s.Inventory.RemoteObserve(ctx, siteID, o, at)
		return err
	case wire.KindPresence:
		var p wire.Presence
		if err := json.Unmarshal(it.Data, &p); err != nil {
			return err
		}
		return s.Inventory.RemotePresence(ctx, siteID, p, at)
	case wire.KindIPGone:
		var g wire.IPGone
		if err := json.Unmarshal(it.Data, &g); err != nil {
			return err
		}
		return s.Inventory.RemoteIPGone(ctx, siteID, g, at)
	case wire.KindDevice:
		var op wire.DeviceOp
		if err := json.Unmarshal(it.Data, &op); err != nil {
			return err
		}
		return s.Inventory.RemoteDeviceOp(ctx, siteID, op)
	case wire.KindEvent:
		var ev wire.Event
		if err := json.Unmarshal(it.Data, &ev); err != nil {
			return err
		}
		var dev int64
		if ev.Device > 0 {
			ids, err := s.Inventory.SiteDeviceIDs(ctx, siteID, []int64{ev.Device})
			if err != nil {
				return err
			}
			dev = ids[ev.Device]
		}
		_, err := s.Events.Import(ctx, events.Imported{SiteID: siteID, Site: siteName, DeviceID: dev, At: at, Type: ev.Type,
			Severity: ev.Severity, PluginID: ev.Plugin, Title: ev.Title, Message: ev.Message, Payload: ev.Payload, RemoteID: ev.ID})
		if err != nil && strings.Contains(err.Error(), "unbekannter Event-Typ") {
			s.Log.Info("Verbund: Event-Typ des Standorts unbekannt (Zentrale älter?)", "site", siteName, "type", ev.Type)
			return nil
		}
		return err
	case wire.KindSync:
		var sy wire.Sync
		if err := json.Unmarshal(it.Data, &sy); err != nil {
			return err
		}
		switch sy.Phase {
		case wire.SyncBegin:
			s.Log.Info("Verbund: vollständiger Abgleich beginnt", "site", siteName, "reset", sy.Reset)
			if sy.Reset {
				return s.Inventory.ResetSiteMapping(ctx, siteID)
			}
		case wire.SyncEnd:
			removed, err := s.Inventory.FinishSiteSync(ctx, siteID, sy.Devices)
			if err != nil {
				return err
			}
			s.Log.Info("Verbund: vollständiger Abgleich abgeschlossen", "site", siteName, "devices", len(sy.Devices), "removed", removed)
		}
		return nil
	}
	s.Log.Debug("Verbund: unbekannte Eintragsart übersprungen", "site", siteName, "kind", it.Kind)
	return nil
}

// monitorLoop raises site.down for sites that stopped reporting (central instance).
func (s *Service) monitorLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
		if s.Role() != RoleCentral {
			continue
		}
		if err := s.checkSites(s.ctx); err != nil && s.ctx.Err() == nil {
			s.Log.Error("Verbund: Standorte prüfen", "err", err)
		}
	}
}

func (s *Service) checkSites(ctx context.Context) error {
	cut := time.Now().Add(-downAfter).UnixMilli()
	rows, err := s.DB.R.QueryContext(ctx, "SELECT id, name, last_contact_at FROM sites WHERE down = 0 AND last_contact_at IS NOT NULL AND last_contact_at < ?", cut)
	if err != nil {
		return err
	}
	type silent struct {
		id      int64
		name    string
		contact int64
	}
	var list []silent
	for rows.Next() {
		var x silent
		if err := rows.Scan(&x.id, &x.name, &x.contact); err != nil {
			rows.Close()
			return err
		}
		list = append(list, x)
	}
	rows.Close()
	for _, x := range list {
		res, err := s.DB.W.ExecContext(ctx, "UPDATE sites SET down = 1 WHERE id = ? AND down = 0 AND last_contact_at = ?", x.id, x.contact)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n == 0 {
			continue // reported meanwhile
		}
		last := time.UnixMilli(x.contact)
		if _, err := s.Events.Emit(ctx, "core", plugin.Event{Type: plugin.EvSiteDown, Title: "Standort " + x.name + " meldet sich nicht",
			Message: "Letzte Meldung " + last.In(time.Local).Format("02.01.2006 15:04") + ". Der Standort puffert seine Daten und liefert sie nach.",
			Payload: map[string]any{"site": x.name, "site_id": x.id, "last_contact": last.Format(time.RFC3339)}}); err != nil {
			return err
		}
		s.publishSites("updated", x.id)
	}
	return nil
}
