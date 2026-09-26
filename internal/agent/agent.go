// Package agent is the server side of the NetScope agent: installation tokens, the agents
// that enrolled with them, the agent protocol (internal/agent/proto) and the conversion of
// what agents deliver into observations. Inventory output is parsed with the code of the
// SSH inventory; utilisation samples become time series of the device.
package agent

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"netscope/internal/agent/proto"
	"netscope/internal/bus"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/pluginhost"
	"netscope/internal/plugins/agents"
	"netscope/internal/plugins/ssh"
)

var (
	// ErrInvalidToken: the installation token is unknown, revoked, expired or used up.
	ErrInvalidToken = errors.New("Installations-Token ungültig, abgelaufen, widerrufen oder aufgebraucht")
	// ErrUnauthenticated: unknown agent secret (agent removed).
	ErrUnauthenticated = errors.New("Agent unbekannt – in NetScope entfernt?")
	// ErrDisabled: the agent plugin is switched off.
	ErrDisabled = errors.New("NetScope-Agents sind in dieser Instanz deaktiviert (Plugin „NetScope-Agent“)")
	// ErrNoDevice: measurements before the first inventory; the agent keeps them.
	ErrNoDevice = errors.New("noch kein Inventar – Messwerte folgen danach")
)

// Deps are the services the agent service uses.
type Deps struct {
	DB        *db.DB
	Bus       *bus.Bus
	Log       *slog.Logger
	Inventory *inventory.Store
	Events    *events.Store
	Host      *pluginhost.Host
	// BinDir holds the agent binaries netscope-agent-linux-<arch> (built into the image).
	BinDir  string
	Version string
}

// Service manages agents.
type Service struct {
	Deps
	mu      sync.Mutex
	waiters map[int64]chan struct{} // wakes the long poll of an agent
	bins    binCache
	cancel  context.CancelFunc
	done    chan struct{}
	closing chan struct{} // closed by Stop: ends the long polls
	once    sync.Once
}

// New creates the service.
func New(d Deps) *Service {
	return &Service{Deps: d, waiters: map[int64]chan struct{}{}, closing: make(chan struct{})}
}

// Start runs the monitor (agents that stopped reporting) until Stop.
func (s *Service) Start(ctx context.Context) {
	ctx, s.cancel = context.WithCancel(ctx)
	s.done = make(chan struct{})
	agents.RefreshAll = s.RefreshAll
	go func() {
		defer close(s.done)
		t := time.NewTicker(30 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := s.checkOffline(ctx); err != nil && ctx.Err() == nil {
					s.Log.Error("Agents prüfen", "err", err)
				}
			}
		}
	}()
}

// Stop ends the monitor and the waiting long polls.
func (s *Service) Stop() {
	s.once.Do(func() { close(s.closing) })
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
	agents.RefreshAll = nil
}

// ---------------------------------------------------------------- settings

type options struct {
	enabled                                 bool
	inventory, sample, report, offline, cmd time.Duration
	packages, docker                        bool
	diskThreshold                           int
}

func (s *Service) options() options {
	st := options{enabled: true, inventory: time.Hour, sample: time.Minute, report: 5 * time.Minute, offline: 5 * time.Minute,
		cmd: 30 * time.Second, packages: true, docker: true, diskThreshold: 90}
	cfg, ok := s.Host.Config(agents.ID)
	if !ok || cfg == nil {
		return st
	}
	ps := plugin.NewSettings(cfg.Settings)
	st.enabled = cfg.Enabled
	for key, dst := range map[string]*time.Duration{"inventory_interval": &st.inventory, "sample_interval": &st.sample,
		"report_interval": &st.report, "offline_after": &st.offline} {
		if d := ps.Duration(key); d > 0 {
			*dst = d
		}
	}
	st.packages, st.docker = ps.Bool("collect_packages"), ps.Bool("collect_docker")
	st.diskThreshold = ps.Int("disk_threshold")
	return st
}

func (st options) proto() proto.Config {
	return proto.Config{InventoryInterval: st.inventory, SampleInterval: st.sample, ReportInterval: st.report,
		Packages: st.packages, Docker: st.docker, CommandTimeout: st.cmd}
}

// ---------------------------------------------------------------- tokens

func newToken(prefix string) (plain, hash string, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", "", err
	}
	plain = prefix + base64.RawURLEncoding.EncodeToString(b)
	return plain, hashToken(plain), nil
}

func hashToken(t string) string {
	sum := sha256.Sum256([]byte(t))
	return hex.EncodeToString(sum[:])
}

// Enrollment is an installation token (without the token itself).
type Enrollment struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Prefix    string     `json:"prefix"`
	Tags      []string   `json:"tags"`
	MaxUses   *int       `json:"maxUses,omitempty"`
	Uses      int        `json:"uses"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	RevokedAt *time.Time `json:"revokedAt,omitempty"`
	CreatedBy string     `json:"createdBy"`
	CreatedAt time.Time  `json:"createdAt"`
	// Usable: neither revoked, expired nor used up.
	Usable bool `json:"usable"`
	Agents int  `json:"agents"`
}

// EnrollmentInput creates an installation token.
type EnrollmentInput struct {
	Name      string     `json:"name"`
	Tags      []string   `json:"tags"`
	MaxUses   *int       `json:"maxUses,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// CreateEnrollment creates an installation token; the plain token is returned once.
func (s *Service) CreateEnrollment(ctx context.Context, in EnrollmentInput, actor string) (*Enrollment, string, error) {
	in.Name = strings.TrimSpace(in.Name)
	switch {
	case in.Name == "" || len([]rune(in.Name)) > 100:
		return nil, "", plugin.FieldErr("name", "1–100 Zeichen")
	case in.MaxUses != nil && *in.MaxUses < 1:
		return nil, "", plugin.FieldErr("maxUses", "mindestens 1 (leer = unbegrenzt)")
	case in.ExpiresAt != nil && in.ExpiresAt.Before(time.Now()):
		return nil, "", plugin.FieldErr("expiresAt", "liegt in der Vergangenheit")
	}
	tags := []string{}
	for _, t := range in.Tags {
		if t = strings.TrimSpace(t); t != "" {
			tags = append(tags, t)
		}
	}
	plain, hash, err := newToken(proto.EnrollPrefix)
	if err != nil {
		return nil, "", err
	}
	res, err := s.DB.W.ExecContext(ctx, `INSERT INTO agent_enrollments(name, token_hash, token_prefix, tags, max_uses, expires_at,
		created_by, created_at) VALUES (?,?,?,?,?,?,?,?)`, in.Name, hash, plain[:10], db.JSON(tags), in.MaxUses, db.NullMs(in.ExpiresAt), actor, db.Now())
	if err != nil {
		return nil, "", err
	}
	id, _ := res.LastInsertId()
	e, err := s.Enrollment(ctx, id)
	return e, plain, err
}

const enrollmentSelect = `SELECT e.id, e.name, e.token_prefix, e.tags, e.max_uses, e.uses, e.expires_at, e.revoked_at, e.created_by,
	e.created_at, (SELECT COUNT(*) FROM agents a WHERE a.enrollment_id = e.id) FROM agent_enrollments e`

func scanEnrollment(sc interface{ Scan(...any) error }) (Enrollment, error) {
	var (
		e                Enrollment
		tags             string
		maxUses          sql.NullInt64
		expires, revoked sql.NullInt64
		created          int64
	)
	if err := sc.Scan(&e.ID, &e.Name, &e.Prefix, &tags, &maxUses, &e.Uses, &expires, &revoked, &e.CreatedBy, &created, &e.Agents); err != nil {
		return e, err
	}
	e.Tags = []string{}
	_ = db.Unmarshal(tags, &e.Tags)
	if maxUses.Valid {
		n := int(maxUses.Int64)
		e.MaxUses = &n
	}
	e.ExpiresAt, e.RevokedAt, e.CreatedAt = db.NullTime(expires), db.NullTime(revoked), db.Time(created)
	e.Usable = e.RevokedAt == nil && (e.ExpiresAt == nil || e.ExpiresAt.After(time.Now())) && (e.MaxUses == nil || e.Uses < *e.MaxUses)
	return e, nil
}

// Enrollment returns an installation token.
func (s *Service) Enrollment(ctx context.Context, id int64) (*Enrollment, error) {
	e, err := scanEnrollment(s.DB.R.QueryRowContext(ctx, enrollmentSelect+" WHERE e.id = ?", id))
	if err != nil {
		return nil, db.NotFound(err)
	}
	return &e, nil
}

// Enrollments lists the installation tokens, newest first.
func (s *Service) Enrollments(ctx context.Context) ([]Enrollment, error) {
	rows, err := s.DB.R.QueryContext(ctx, enrollmentSelect+" ORDER BY e.id DESC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Enrollment{}
	for rows.Next() {
		e, err := scanEnrollment(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// RevokeEnrollment makes an installation token unusable; enrolled agents keep working.
func (s *Service) RevokeEnrollment(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, "UPDATE agent_enrollments SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL", db.Now(), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		if _, err := s.Enrollment(ctx, id); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------- agents

// Agent is an enrolled agent.
type Agent struct {
	ID              int64      `json:"id"`
	DeviceID        *int64     `json:"deviceId,omitempty"`
	DeviceName      string     `json:"deviceName,omitempty"`
	EnrollmentID    *int64     `json:"enrollmentId,omitempty"`
	EnrollmentName  string     `json:"enrollmentName,omitempty"`
	MachineID       string     `json:"machineId"`
	Hostname        string     `json:"hostname"`
	OS              string     `json:"os"`
	Arch            string     `json:"arch"`
	Kernel          string     `json:"kernel"`
	Version         string     `json:"version"`
	Docker          bool       `json:"docker"`
	IP              string     `json:"ip"`
	EnrolledAt      time.Time  `json:"enrolledAt"`
	LastSeenAt      *time.Time `json:"lastSeenAt,omitempty"`
	LastInventoryAt *time.Time `json:"lastInventoryAt,omitempty"`
	LastMetricsAt   *time.Time `json:"lastMetricsAt,omitempty"`
	LastError       string     `json:"lastError,omitempty"`
	// Online: the agent reported within the configured time.
	Online bool `json:"online"`
	// Outdated: the instance offers a newer version (the agent updates itself).
	Outdated  bool     `json:"outdated"`
	FullDisks []string `json:"fullDisks"`
}

const agentSelect = `SELECT a.id, a.device_id, COALESCE(NULLIF(d.display_name, ''), NULLIF(d.hostname, ''), d.primary_ip, ''),
	a.enrollment_id, COALESCE(e.name, ''), a.machine_id, a.hostname, a.os, a.arch, a.kernel, a.version, a.docker, a.ip,
	a.enrolled_at, a.last_seen_at, a.last_inventory_at, a.last_metrics_at, a.last_error, a.offline, a.full_disks
	FROM agents a LEFT JOIN devices d ON d.id = a.device_id LEFT JOIN agent_enrollments e ON e.id = a.enrollment_id`

func (s *Service) scanAgent(sc interface{ Scan(...any) error }, st options) (Agent, error) {
	var (
		a                  Agent
		dev, enr           sql.NullInt64
		enrolled           int64
		seen, inv, metrics sql.NullInt64
		offline            bool
		full               string
	)
	err := sc.Scan(&a.ID, &dev, &a.DeviceName, &enr, &a.EnrollmentName, &a.MachineID, &a.Hostname, &a.OS, &a.Arch, &a.Kernel,
		&a.Version, &a.Docker, &a.IP, &enrolled, &seen, &inv, &metrics, &a.LastError, &offline, &full)
	if err != nil {
		return a, err
	}
	if dev.Valid {
		a.DeviceID = &dev.Int64
	}
	if enr.Valid {
		a.EnrollmentID = &enr.Int64
	}
	a.EnrolledAt = db.Time(enrolled)
	a.LastSeenAt, a.LastInventoryAt, a.LastMetricsAt = db.NullTime(seen), db.NullTime(inv), db.NullTime(metrics)
	a.Online = !offline && a.LastSeenAt != nil && time.Since(*a.LastSeenAt) < st.offline
	a.Outdated = s.offer(a.Arch, a.Version) != nil
	a.FullDisks = []string{}
	_ = db.Unmarshal(full, &a.FullDisks)
	return a, nil
}

// Agents lists the agents (deviceID > 0: only the agent of that device).
func (s *Service) Agents(ctx context.Context, deviceID int64) ([]Agent, error) {
	q, args := agentSelect, []any{}
	if deviceID > 0 {
		q += " WHERE a.device_id = ?"
		args = append(args, deviceID)
	}
	rows, err := s.DB.R.QueryContext(ctx, q+" ORDER BY a.hostname COLLATE NOCASE, a.id", args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	st := s.options()
	out := []Agent{}
	for rows.Next() {
		a, err := s.scanAgent(rows, st)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// Agent returns one agent.
func (s *Service) Agent(ctx context.Context, id int64) (*Agent, error) {
	a, err := s.scanAgent(s.DB.R.QueryRowContext(ctx, agentSelect+" WHERE a.id = ?", id), s.options())
	if err != nil {
		return nil, db.NotFound(err)
	}
	return &a, nil
}

// Delete removes an agent: its secret stops working and the agent service on the host
// ends itself. The device stays.
func (s *Service) Delete(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, "DELETE FROM agents WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	s.wake(id)
	s.publish("deleted", id)
	return nil
}

// RequestRefresh asks an agent for an inventory now.
func (s *Service) RequestRefresh(ctx context.Context, id int64) error {
	res, err := s.DB.W.ExecContext(ctx, "UPDATE agents SET refresh = 1 WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	s.wake(id)
	return nil
}

// RefreshAll asks every agent for an inventory (manual run of the agent plugin).
func (s *Service) RefreshAll(ctx context.Context) (int, error) {
	ids, err := s.ids(ctx, "SELECT id FROM agents")
	if err != nil {
		return 0, err
	}
	if _, err := s.DB.W.ExecContext(ctx, "UPDATE agents SET refresh = 1"); err != nil {
		return 0, err
	}
	for _, id := range ids {
		s.wake(id)
	}
	return len(ids), nil
}

func (s *Service) ids(ctx context.Context, q string, args ...any) ([]int64, error) {
	rows, err := s.DB.R.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (s *Service) publish(typ string, id int64) {
	if s.Bus != nil {
		s.Bus.Publish(bus.TopicAgent, typ, map[string]any{"id": id})
	}
}

// ---------------------------------------------------------------- protocol

// Enroll registers an agent with an installation token. A host that enrolls again (same
// machine id, e.g. reinstalled) keeps its agent and gets a new secret.
func (s *Service) Enroll(ctx context.Context, req proto.EnrollRequest, ip string) (*proto.EnrollResponse, error) {
	st := s.options()
	if !st.enabled {
		return nil, ErrDisabled
	}
	if !strings.HasPrefix(req.Token, proto.EnrollPrefix) {
		return nil, ErrInvalidToken
	}
	secret, hash, err := newToken(proto.AgentPrefix)
	if err != nil {
		return nil, err
	}
	h := req.Host
	var id int64
	err = s.DB.Tx(ctx, func(tx *sql.Tx) error {
		var enr int64
		var stored string
		err := tx.QueryRowContext(ctx, `SELECT id, token_hash FROM agent_enrollments WHERE token_hash = ? AND revoked_at IS NULL
			AND (expires_at IS NULL OR expires_at > ?) AND (max_uses IS NULL OR uses < max_uses)`,
			hashToken(req.Token), db.Now()).Scan(&enr, &stored)
		if errors.Is(err, sql.ErrNoRows) || (err == nil && subtle.ConstantTimeCompare([]byte(stored), []byte(hashToken(req.Token))) != 1) {
			return ErrInvalidToken
		}
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, "UPDATE agent_enrollments SET uses = uses + 1 WHERE id = ?", enr); err != nil {
			return err
		}
		now := db.Now()
		if h.MachineID != "" {
			err := tx.QueryRowContext(ctx, "SELECT id FROM agents WHERE machine_id = ? ORDER BY id LIMIT 1", h.MachineID).Scan(&id)
			if err != nil && !errors.Is(err, sql.ErrNoRows) {
				return err
			}
		}
		if id > 0 {
			_, err := tx.ExecContext(ctx, `UPDATE agents SET token_hash = ?, enrollment_id = ?, hostname = ?, os = ?, arch = ?, kernel = ?,
				version = ?, docker = ?, ip = ?, enrolled_at = ?, last_seen_at = ?, offline = 0, last_error = '' WHERE id = ?`,
				hash, enr, h.Hostname, h.OS, h.Arch, h.Kernel, h.Version, db.Bool(h.Docker), ip, now, now, id)
			return err
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO agents(enrollment_id, machine_id, hostname, os, arch, kernel, version, docker,
			token_hash, ip, enrolled_at, last_seen_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`,
			enr, h.MachineID, h.Hostname, h.OS, h.Arch, h.Kernel, h.Version, db.Bool(h.Docker), hash, ip, now, now)
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.Log.Info("Agent angemeldet", "agent", id, "host", h.Hostname, "ip", ip, "version", h.Version)
	s.publish("enrolled", id)
	return &proto.EnrollResponse{AgentID: id, Secret: secret, Config: st.proto()}, nil
}

// Session is an authenticated agent request.
type Session struct {
	id       int64
	deviceID int64
	hostname string
	arch     string
	version  string
	offline  bool
	seen     *time.Time
	enroll   sql.NullInt64
}

// Authenticate resolves an agent secret and records the contact.
func (s *Service) Authenticate(ctx context.Context, secret, ip string) (*Session, error) {
	if !strings.HasPrefix(secret, proto.AgentPrefix) {
		return nil, ErrUnauthenticated
	}
	var (
		a      Session
		dev    sql.NullInt64
		seen   sql.NullInt64
		stored string
	)
	h := hashToken(secret)
	err := s.DB.R.QueryRowContext(ctx, `SELECT id, device_id, hostname, arch, version, offline, last_seen_at, enrollment_id, token_hash
		FROM agents WHERE token_hash = ?`, h).Scan(&a.id, &dev, &a.hostname, &a.arch, &a.version, &a.offline, &seen, &a.enroll, &stored)
	if err != nil || subtle.ConstantTimeCompare([]byte(stored), []byte(h)) != 1 {
		return nil, ErrUnauthenticated
	}
	a.deviceID, a.seen = dev.Int64, db.NullTime(seen)
	if _, err := s.DB.W.ExecContext(ctx, "UPDATE agents SET last_seen_at = ?, ip = ?, offline = 0 WHERE id = ?", db.Now(), ip, a.id); err != nil {
		return nil, err
	}
	if a.offline {
		s.back(ctx, &a)
	}
	return &a, nil
}

// back raises agent.online for an agent that was reported missing.
func (s *Service) back(ctx context.Context, a *Session) {
	payload := map[string]any{"agent_id": a.id, "hostname": a.hostname}
	if a.seen != nil {
		payload["down_seconds"] = int64(time.Since(*a.seen).Seconds())
	}
	if _, err := s.Events.Emit(ctx, agents.ID, plugin.Event{Type: plugin.EvAgentOnline, DeviceID: a.deviceID,
		Title: "Agent auf " + a.hostname + " meldet sich wieder", Payload: payload}); err != nil {
		s.Log.Error("agent.online", "err", err)
	}
	s.publish("updated", a.id)
}

// maxWait bounds a long poll.
const maxWait = 55 * time.Second

// Poll answers the long poll of an agent: at once when there is a refresh request or an
// update, otherwise after wait (or when woken).
func (s *Service) Poll(ctx context.Context, a *Session, wait time.Duration, version string) (*proto.PollResponse, error) {
	if version != "" && version != a.version {
		if _, err := s.DB.W.ExecContext(ctx, "UPDATE agents SET version = ? WHERE id = ?", version, a.id); err != nil {
			return nil, err
		}
		a.version = version
		s.publish("updated", a.id)
	}
	wait = min(max(wait, 0), maxWait)
	ch := s.waiter(a.id)
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		resp := &proto.PollResponse{Config: s.options().proto(), Update: s.offer(a.arch, a.version)}
		res, err := s.DB.W.ExecContext(ctx, "UPDATE agents SET refresh = 0 WHERE id = ? AND refresh = 1", a.id)
		if err != nil {
			return nil, err
		}
		n, _ := res.RowsAffected()
		resp.Refresh = n > 0
		if resp.Refresh || resp.Update != nil {
			return resp, nil
		}
		select {
		case <-ch:
			if _, err := s.Agent(ctx, a.id); err != nil {
				return nil, ErrUnauthenticated // removed while waiting
			}
		case <-deadline.C:
			return resp, nil
		case <-ctx.Done():
			return resp, nil
		case <-s.closing:
			return resp, nil
		}
	}
}

func (s *Service) waiter(id int64) chan struct{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	ch, ok := s.waiters[id]
	if !ok {
		ch = make(chan struct{}, 1)
		s.waiters[id] = ch
	}
	return ch
}

func (s *Service) wake(id int64) {
	select {
	case s.waiter(id) <- struct{}{}:
	default:
	}
}

// ReportInventory stores the inventory an agent collected.
func (s *Service) ReportInventory(ctx context.Context, a *Session, r proto.InventoryReport) error {
	if !s.options().enabled {
		return ErrDisabled
	}
	h := r.Host
	if r.Error != "" && r.Stdout == "" {
		_, err := s.DB.W.ExecContext(ctx, "UPDATE agents SET last_error = ? WHERE id = ?", "Erfassung: "+r.Error, a.id)
		return err
	}
	subnets, err := s.Inventory.Subnets(ctx)
	if err != nil {
		return err
	}
	prefixes := make([]netip.Prefix, 0, len(subnets))
	for _, sn := range subnets {
		prefixes = append(prefixes, sn.CIDR)
	}
	obs, sections := ssh.AgentObservation([]byte(r.Stdout), []byte(r.Stderr), r.Truncated, prefixes)
	if sections == 0 {
		_, err := s.DB.W.ExecContext(ctx, "UPDATE agents SET last_error = ? WHERE id = ?", "Erfassung lieferte keine Daten", a.id)
		return err
	}
	ref := h.MachineID
	if ref == "" {
		ref = "agent-" + strconv.FormatInt(a.id, 10)
	}
	obs.DeviceID = a.deviceID
	obs.Ref = &plugin.ExternalRef{Source: agents.ID, ID: ref, Data: map[string]any{"agentId": a.id, "version": h.Version}}
	obs.Create, obs.Present = true, true
	obs.Target = h.Hostname
	if obs.Attrs == nil {
		obs.Attrs = map[string]string{}
	}
	obs.Attrs["agent.version"] = h.Version
	devID, err := s.Inventory.Observe(ctx, agents.ID, 0, obs)
	if err != nil {
		return err
	}
	if a.deviceID == 0 && devID > 0 && a.enroll.Valid {
		s.applyEnrollmentTags(ctx, a.enroll.Int64, devID)
	}
	lastErr := ""
	if r.Error != "" {
		lastErr = "Erfassung: " + r.Error
	}
	_, err = s.DB.W.ExecContext(ctx, `UPDATE agents SET device_id = ?, last_inventory_at = ?, hostname = ?, os = ?, kernel = ?,
		version = ?, docker = ?, machine_id = CASE WHEN machine_id = '' THEN ? ELSE machine_id END, last_error = ? WHERE id = ?`,
		nullID(devID), db.Now(), h.Hostname, h.OS, h.Kernel, h.Version, db.Bool(h.Docker), h.MachineID, lastErr, a.id)
	if err != nil {
		return err
	}
	a.deviceID = devID
	s.publish("updated", a.id)
	return nil
}

func nullID(id int64) any {
	if id == 0 {
		return nil
	}
	return id
}

// applyEnrollmentTags puts the tags of the installation token on a newly bound device.
func (s *Service) applyEnrollmentTags(ctx context.Context, enrollment, device int64) {
	var raw string
	if err := s.DB.R.QueryRowContext(ctx, "SELECT tags FROM agent_enrollments WHERE id = ?", enrollment).Scan(&raw); err != nil {
		return
	}
	var tags []string
	if db.Unmarshal(raw, &tags) != nil || len(tags) == 0 {
		return
	}
	if _, err := s.Inventory.Bulk(ctx, inventory.BulkAction{Action: "add_tags", IDs: []int64{device}, Tags: tags}); err != nil {
		s.Log.Warn("Tags des Installations-Tokens setzen", "device", device, "err", err)
	}
}

// Metric names of agent samples.
const (
	MetricCPU   = "host.cpu"    // % busy
	MetricLoad  = "host.load1"  // load average (1 min)
	MetricMem   = "host.mem"    // % used
	MetricSwap  = "host.swap"   // % used
	MetricDisk  = "host.disk"   // % used, key = mount point
	MetricNetRX = "host.net_rx" // bytes/s received, key = interface
	MetricNetTX = "host.net_tx" // bytes/s sent, key = interface
)

// ReportMetrics stores utilisation samples as time series of the agent's device and raises
// disk.full / disk.ok against the threshold of the latest sample.
func (s *Service) ReportMetrics(ctx context.Context, a *Session, r proto.MetricsReport) error {
	st := s.options()
	if !st.enabled {
		return ErrDisabled
	}
	if a.deviceID == 0 {
		return ErrNoDevice
	}
	var ms []plugin.Metric
	add := func(name, key, unit string, v float64, at time.Time) {
		ms = append(ms, plugin.Metric{Name: name, Key: key, Unit: unit, Min: v, Avg: v, Max: v, At: at})
	}
	for _, smp := range r.Samples {
		at := smp.At
		if at.IsZero() || at.After(time.Now().Add(time.Minute)) {
			at = time.Now()
		}
		if smp.CPU != nil {
			add(MetricCPU, "", "%", *smp.CPU, at)
		}
		add(MetricLoad, "", "", smp.Load1, at)
		if smp.MemTotal > 0 {
			add(MetricMem, "", "%", pct(smp.MemUsed, smp.MemTotal), at)
		}
		if smp.SwapTot > 0 {
			add(MetricSwap, "", "%", pct(smp.SwapUsed, smp.SwapTot), at)
		}
		for _, d := range smp.Disks {
			if d.Total > 0 {
				add(MetricDisk, d.Mount, "%", pct(d.Used, d.Total), at)
			}
		}
		for _, n := range smp.Net {
			add(MetricNetRX, n.Interface, "B/s", n.RX, at)
			add(MetricNetTX, n.Interface, "B/s", n.TX, at)
		}
	}
	if len(ms) == 0 {
		return nil
	}
	obs := &plugin.Observation{DeviceID: a.deviceID, Present: true, Metrics: ms, Target: a.hostname}
	if _, err := s.Inventory.Observe(ctx, agents.ID, 0, obs); err != nil {
		return err
	}
	if _, err := s.DB.W.ExecContext(ctx, "UPDATE agents SET last_metrics_at = ? WHERE id = ?", db.Now(), a.id); err != nil {
		return err
	}
	if len(r.Samples) > 0 {
		return s.checkDisks(ctx, a, r.Samples[len(r.Samples)-1], st.diskThreshold)
	}
	return nil
}

func pct(part, total uint64) float64 { return float64(part) / float64(total) * 100 }

// checkDisks raises disk.full once when a file system crosses the threshold and disk.ok
// when it is back two points below it.
func (s *Service) checkDisks(ctx context.Context, a *Session, smp proto.Sample, threshold int) error {
	var raw string
	if err := s.DB.R.QueryRowContext(ctx, "SELECT full_disks FROM agents WHERE id = ?", a.id).Scan(&raw); err != nil {
		return db.NotFound(err)
	}
	full := map[string]bool{}
	var list []string
	_ = db.Unmarshal(raw, &list)
	for _, m := range list {
		full[m] = true
	}
	changed := false
	present := map[string]bool{}
	for _, d := range smp.Disks {
		if d.Total == 0 {
			continue
		}
		present[d.Mount] = true
		used := pct(d.Used, d.Total)
		switch {
		case threshold > 0 && used >= float64(threshold) && !full[d.Mount]:
			full[d.Mount], changed = true, true
			s.emit(ctx, plugin.Event{Type: plugin.EvDiskFull, DeviceID: a.deviceID,
				Title:   fmt.Sprintf("%s: %s zu %.0f %% belegt", a.hostname, d.Mount, used),
				Payload: map[string]any{"mount": d.Mount, "used_pct": round1(used), "free_bytes": d.Total - d.Used, "threshold": threshold}})
		case full[d.Mount] && (threshold == 0 || used < float64(threshold)-2):
			delete(full, d.Mount)
			changed = true
			s.emit(ctx, plugin.Event{Type: plugin.EvDiskOK, DeviceID: a.deviceID,
				Title:   fmt.Sprintf("%s: %s wieder unter der Schwelle (%.0f %%)", a.hostname, d.Mount, used),
				Payload: map[string]any{"mount": d.Mount, "used_pct": round1(used)}})
		}
	}
	for m := range full { // unmounted
		if !present[m] {
			delete(full, m)
			changed = true
		}
	}
	if !changed {
		return nil
	}
	out := []string{}
	for m := range full {
		out = append(out, m)
	}
	_, err := s.DB.W.ExecContext(ctx, "UPDATE agents SET full_disks = ? WHERE id = ?", db.JSON(out), a.id)
	return err
}

func round1(v float64) float64 { return float64(int64(v*10+0.5)) / 10 }

func (s *Service) emit(ctx context.Context, ev plugin.Event) {
	if _, err := s.Events.Emit(ctx, agents.ID, ev); err != nil {
		s.Log.Error("Agent-Event", "type", ev.Type, "err", err)
	}
}

// checkOffline raises agent.offline for agents without contact; the device goes offline
// too unless another presence source (scanner) tracks it.
func (s *Service) checkOffline(ctx context.Context) error {
	st := s.options()
	if !st.enabled {
		return nil
	}
	cut := time.Now().Add(-st.offline).UnixMilli()
	rows, err := s.DB.R.QueryContext(ctx, `SELECT id, device_id, hostname, last_seen_at FROM agents
		WHERE offline = 0 AND last_seen_at IS NOT NULL AND last_seen_at < ?`, cut)
	if err != nil {
		return err
	}
	type gone struct {
		id, device int64
		host       string
		seen       int64
	}
	var list []gone
	for rows.Next() {
		var g gone
		var dev sql.NullInt64
		if err := rows.Scan(&g.id, &dev, &g.host, &g.seen); err != nil {
			rows.Close()
			return err
		}
		g.device = dev.Int64
		list = append(list, g)
	}
	rows.Close()
	for _, g := range list {
		if _, err := s.DB.W.ExecContext(ctx, "UPDATE agents SET offline = 1 WHERE id = ?", g.id); err != nil {
			return err
		}
		s.emit(ctx, plugin.Event{Type: plugin.EvAgentOffline, DeviceID: g.device, Title: "Agent auf " + g.host + " meldet sich nicht",
			Payload: map[string]any{"agent_id": g.id, "hostname": g.host, "last_contact": db.Time(g.seen).Format(time.RFC3339)}})
		if g.device > 0 {
			var tracked int
			if err := s.DB.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_presence WHERE device_id = ? AND plugin_id <> ?",
				g.device, agents.ID).Scan(&tracked); err != nil {
				return err
			}
			if tracked == 0 {
				obs := &plugin.Observation{DeviceID: g.device, Power: &plugin.PowerState{Running: false, Expected: true}, Target: g.host}
				if _, err := s.Inventory.Observe(ctx, agents.ID, 0, obs); err != nil {
					s.Log.Warn("Gerät offline setzen", "device", g.device, "err", err)
				}
			}
		}
		s.publish("updated", g.id)
	}
	return nil
}
