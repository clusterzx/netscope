package healthcheck

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/timeseries"
)

// Check types.
const (
	TypeTCP  = "tcp"
	TypeHTTP = "http"
	TypeTLS  = "tls"
	TypeICMP = "icmp"
)

// States.
const (
	StateUnknown  = "unknown"
	StateUp       = "up"
	StateDown     = "down"
	StateDegraded = "degraded"
)

// CheckConfig holds type specific options.
type CheckConfig struct {
	URL             string `json:"url,omitempty"`             // http: full URL (default scheme://target:port/)
	Method          string `json:"method,omitempty"`          // http: GET | HEAD
	ExpectStatus    string `json:"expectStatus,omitempty"`    // http: e.g. "200-399" or "200,204"
	BodyMatch       string `json:"bodyMatch,omitempty"`       // http: regular expression
	VerifyTLS       bool   `json:"verifyTls,omitempty"`       // http/tls: verify certificate chain
	FollowRedirects bool   `json:"followRedirects,omitempty"` // http
	ServerName      string `json:"serverName,omitempty"`      // tls: SNI
	MinDays         int    `json:"minDays,omitempty"`         // tls: degraded if the certificate expires sooner
	Count           int    `json:"count,omitempty"`           // icmp: packets
	DegradedMs      int    `json:"degradedMs,omitempty"`      // degraded above this latency (0 = off)
}

// Check is a configured health check with its current state.
type Check struct {
	ID               int64              `json:"id"`
	DeviceID         int64              `json:"deviceId,omitempty"`
	DeviceName       string             `json:"deviceName,omitempty"`
	Name             string             `json:"name"`
	Type             string             `json:"type"`
	Target           string             `json:"target"`
	Port             int                `json:"port"`
	Config           CheckConfig        `json:"config"`
	IntervalS        int                `json:"intervalSeconds"`
	TimeoutS         int                `json:"timeoutSeconds"`
	FailThreshold    int                `json:"failThreshold"`
	RecoverThreshold int                `json:"recoverThreshold"`
	Enabled          bool               `json:"enabled"`
	State            string             `json:"state"`
	StateSince       *time.Time         `json:"stateSince,omitempty"`
	ConsecutiveFail  int                `json:"consecutiveFail"`
	ConsecutiveOK    int                `json:"consecutiveOk"`
	LastCheckAt      *time.Time         `json:"lastCheckAt,omitempty"`
	LastOK           *bool              `json:"lastOk,omitempty"`
	LastLatencyMs    *float64           `json:"lastLatencyMs,omitempty"`
	LastError        string             `json:"lastError,omitempty"`
	CreatedAt        time.Time          `json:"createdAt"`
	UpdatedAt        time.Time          `json:"updatedAt"`
	Availability     map[string]float64 `json:"availability,omitempty"`
	// Latency24h is the latency of the last 24 hours (about 48 points; status board only).
	Latency24h []timeseries.Point `json:"latency24h,omitempty"`
}

// Outage is a down or degraded period of a check.
type Outage struct {
	ID        int64      `json:"id"`
	CheckID   int64      `json:"checkId"`
	CheckName string     `json:"checkName,omitempty"`
	DeviceID  int64      `json:"deviceId,omitempty"`
	State     string     `json:"state"`
	StartedAt time.Time  `json:"startedAt"`
	EndedAt   *time.Time `json:"endedAt,omitempty"`
	Seconds   int64      `json:"seconds"`
	Reason    string     `json:"reason"`
}

// ParseStatus parses "200-399" / "200,204,301" into a matcher.
func ParseStatus(s string) (func(int) bool, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "200-399"
	}
	type rng struct{ lo, hi int }
	var rs []rng
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		lo, hi, isRange := strings.Cut(part, "-")
		a, err := strconv.Atoi(strings.TrimSpace(lo))
		if err != nil {
			return nil, fmt.Errorf("ungültiger Statuscode %q", part)
		}
		b := a
		if isRange {
			if b, err = strconv.Atoi(strings.TrimSpace(hi)); err != nil {
				return nil, fmt.Errorf("ungültiger Statusbereich %q", part)
			}
		}
		if a < 100 || b > 599 || a > b {
			return nil, fmt.Errorf("Statuscode außerhalb 100–599: %q", part)
		}
		rs = append(rs, rng{a, b})
	}
	return func(code int) bool {
		for _, r := range rs {
			if code >= r.lo && code <= r.hi {
				return true
			}
		}
		return false
	}, nil
}

// Validate checks and normalizes a check definition.
func (c *Check) Validate() error {
	var errs []plugin.FieldError
	add := func(field, msg string) { errs = append(errs, plugin.FieldError{Field: field, Message: msg}) }
	c.Name = strings.TrimSpace(c.Name)
	c.Target = strings.TrimSpace(c.Target)
	if c.Name == "" {
		add("name", "Name erforderlich")
	}
	switch c.Type {
	case TypeTCP, TypeTLS:
		if c.Port < 1 || c.Port > 65535 {
			add("port", "Port 1–65535 erforderlich")
		}
	case TypeHTTP:
		if c.Config.URL != "" {
			u, err := url.Parse(c.Config.URL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				add("config.url", "URL muss mit http:// oder https:// beginnen")
			}
		} else if c.Port < 1 || c.Port > 65535 {
			add("port", "URL oder Port erforderlich")
		}
		if c.Config.Method == "" {
			c.Config.Method = "GET"
		}
		if c.Config.Method != "GET" && c.Config.Method != "HEAD" {
			add("config.method", "Methode GET oder HEAD")
		}
		if _, err := ParseStatus(c.Config.ExpectStatus); err != nil {
			add("config.expectStatus", err.Error())
		}
		if c.Config.BodyMatch != "" {
			if _, err := regexp.Compile(c.Config.BodyMatch); err != nil {
				add("config.bodyMatch", "Body-Regex: "+err.Error())
			}
		}
	case TypeICMP:
		if c.Config.Count == 0 {
			c.Config.Count = 3
		}
		if c.Config.Count < 1 || c.Config.Count > 20 {
			add("config.count", "Anzahl Pings 1–20")
		}
	default:
		add("type", "Typ muss tcp, http, tls oder icmp sein")
	}
	if c.Target == "" && c.DeviceID == 0 && !(c.Type == TypeHTTP && c.Config.URL != "") {
		add("target", "Ziel (Host/IP) oder Gerät erforderlich")
	}
	if c.Target != "" && strings.ContainsAny(c.Target, " /") && net.ParseIP(c.Target) == nil {
		add("target", "Ziel muss ein Hostname oder eine IP sein")
	}
	if c.IntervalS == 0 {
		c.IntervalS = 60
	}
	if c.IntervalS < 30 || c.IntervalS > 86400 {
		add("intervalSeconds", "Intervall 30 Sekunden bis 24 Stunden")
	}
	if c.TimeoutS == 0 {
		c.TimeoutS = 10
	}
	if c.TimeoutS < 1 || c.TimeoutS > 120 {
		add("timeoutSeconds", "Timeout 1–120 Sekunden")
	}
	if c.FailThreshold == 0 {
		c.FailThreshold = 3
	}
	if c.RecoverThreshold == 0 {
		c.RecoverThreshold = 2
	}
	if c.FailThreshold < 1 || c.FailThreshold > 20 {
		add("failThreshold", "Schwellwert 1–20")
	}
	if c.RecoverThreshold < 1 || c.RecoverThreshold > 20 {
		add("recoverThreshold", "Schwellwert 1–20")
	}
	if c.Config.DegradedMs < 0 {
		add("config.degradedMs", "negative Werte sind nicht erlaubt")
	}
	if c.Config.MinDays < 0 {
		add("config.minDays", "negative Werte sind nicht erlaubt")
	}
	if len(errs) > 0 {
		return &plugin.ValidationError{Errors: errs}
	}
	return nil
}

const checkSelect = `SELECT c.id, IFNULL(c.device_id, 0), COALESCE(NULLIF(d.display_name, ''), NULLIF(d.hostname, ''), d.primary_ip, ''),
	c.name, c.type, c.target, c.port, c.config, c.interval_s, c.timeout_s, c.fail_threshold, c.recover_threshold, c.enabled, c.state,
	c.state_since, c.consecutive_fail, c.consecutive_ok, c.last_check_at, c.last_ok, c.last_latency_ms, c.last_error, c.created_at, c.updated_at
	FROM health_checks c LEFT JOIN devices d ON d.id = c.device_id`

func scanCheck(rows *sql.Rows) (*Check, error) {
	var (
		c                     Check
		cfg                   string
		since, lastAt, lastOK sql.NullInt64
		latency               sql.NullFloat64
		cre, upd              int64
	)
	if err := rows.Scan(&c.ID, &c.DeviceID, &c.DeviceName, &c.Name, &c.Type, &c.Target, &c.Port, &cfg, &c.IntervalS, &c.TimeoutS,
		&c.FailThreshold, &c.RecoverThreshold, &c.Enabled, &c.State, &since, &c.ConsecutiveFail, &c.ConsecutiveOK, &lastAt, &lastOK,
		&latency, &c.LastError, &cre, &upd); err != nil {
		return nil, err
	}
	_ = json.Unmarshal([]byte(cfg), &c.Config)
	c.StateSince, c.LastCheckAt = db.NullTime(since), db.NullTime(lastAt)
	if lastOK.Valid {
		v := lastOK.Int64 == 1
		c.LastOK = &v
	}
	if latency.Valid {
		v := latency.Float64
		c.LastLatencyMs = &v
	}
	c.CreatedAt, c.UpdatedAt = db.Time(cre), db.Time(upd)
	return &c, nil
}

// ListChecks returns checks (deviceID 0 = all) including their availability.
func ListChecks(ctx context.Context, d *db.DB, deviceID int64) ([]*Check, error) {
	q := checkSelect
	var args []any
	if deviceID > 0 {
		q += " WHERE c.device_id = ?"
		args = append(args, deviceID)
	}
	rows, err := d.R.QueryContext(ctx, q+" ORDER BY c.name COLLATE NOCASE, c.id", args...)
	if err != nil {
		return nil, err
	}
	var out []*Check
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, c)
	}
	rows.Close()
	now := time.Now()
	for _, c := range out {
		if c.Availability, err = Availability(ctx, d, c.ID, c.CreatedAt, now); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []*Check{}
	}
	return out, nil
}

// GetCheck returns one check.
func GetCheck(ctx context.Context, d *db.DB, id int64) (*Check, error) {
	rows, err := d.R.QueryContext(ctx, checkSelect+" WHERE c.id = ?", id)
	if err != nil {
		return nil, err
	}
	if !rows.Next() {
		rows.Close()
		return nil, db.ErrNotFound
	}
	c, err := scanCheck(rows)
	rows.Close()
	if err != nil {
		return nil, err
	}
	c.Availability, err = Availability(ctx, d, c.ID, c.CreatedAt, time.Now())
	return c, err
}

// SaveCheck creates (ID 0) or updates a check definition (state is kept on update).
func SaveCheck(ctx context.Context, d *db.DB, c *Check) error {
	if err := c.Validate(); err != nil {
		return err
	}
	var dev any
	if c.DeviceID > 0 {
		var n int
		if err := d.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM devices WHERE id = ?", c.DeviceID).Scan(&n); err != nil || n == 0 {
			return fmt.Errorf("Gerät %d existiert nicht", c.DeviceID)
		}
		dev = c.DeviceID
	}
	cfg, _ := json.Marshal(c.Config)
	now := db.Now()
	if c.ID == 0 {
		res, err := d.W.ExecContext(ctx, `INSERT INTO health_checks(device_id, name, type, target, port, config, interval_s, timeout_s,
			fail_threshold, recover_threshold, enabled, state, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,'unknown',?,?)`,
			dev, c.Name, c.Type, c.Target, c.Port, string(cfg), c.IntervalS, c.TimeoutS, c.FailThreshold, c.RecoverThreshold,
			db.Bool(c.Enabled), now, now)
		if err != nil {
			return err
		}
		c.ID, _ = res.LastInsertId()
		return nil
	}
	res, err := d.W.ExecContext(ctx, `UPDATE health_checks SET device_id = ?, name = ?, type = ?, target = ?, port = ?, config = ?,
		interval_s = ?, timeout_s = ?, fail_threshold = ?, recover_threshold = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		dev, c.Name, c.Type, c.Target, c.Port, string(cfg), c.IntervalS, c.TimeoutS, c.FailThreshold, c.RecoverThreshold,
		db.Bool(c.Enabled), now, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	return nil
}

// DeleteCheck removes a check and its history.
func DeleteCheck(ctx context.Context, d *db.DB, id int64) error {
	res, err := d.W.ExecContext(ctx, "DELETE FROM health_checks WHERE id = ?", id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return db.ErrNotFound
	}
	_, err = d.W.ExecContext(ctx, "DELETE FROM ts_series WHERE metric = ? AND key = ?", MetricLatency, seriesKey(id))
	return err
}

// Outages returns the most recent outages (checkID 0 = all checks).
func Outages(ctx context.Context, d *db.DB, checkID int64, limit int) ([]Outage, error) {
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	q := `SELECT o.id, o.check_id, c.name, IFNULL(c.device_id, 0), o.state, o.started_at, o.ended_at, o.reason
		FROM health_outages o JOIN health_checks c ON c.id = o.check_id`
	var args []any
	if checkID > 0 {
		q += " WHERE o.check_id = ?"
		args = append(args, checkID)
	}
	rows, err := d.R.QueryContext(ctx, q+" ORDER BY o.started_at DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Outage{}
	now := time.Now()
	for rows.Next() {
		var (
			o     Outage
			start int64
			end   sql.NullInt64
		)
		if err := rows.Scan(&o.ID, &o.CheckID, &o.CheckName, &o.DeviceID, &o.State, &start, &end, &o.Reason); err != nil {
			return nil, err
		}
		o.StartedAt, o.EndedAt = db.Time(start), db.NullTime(end)
		stop := now
		if o.EndedAt != nil {
			stop = *o.EndedAt
		}
		o.Seconds = int64(stop.Sub(o.StartedAt).Seconds())
		out = append(out, o)
	}
	return out, rows.Err()
}

// Windows are the availability windows.
var Windows = []struct {
	Key string
	D   time.Duration
}{{"24h", 24 * time.Hour}, {"7d", 7 * 24 * time.Hour}, {"30d", 30 * 24 * time.Hour}}

// Availability returns the share of time (percent) a check was not down, per window.
// Degraded periods count as available. The window starts at the check's creation at the earliest.
func Availability(ctx context.Context, d *db.DB, checkID int64, created, now time.Time) (map[string]float64, error) {
	out := map[string]float64{}
	longest := Windows[len(Windows)-1].D
	rows, err := d.R.QueryContext(ctx, "SELECT started_at, ended_at FROM health_outages WHERE check_id = ? AND state = 'down' AND (ended_at IS NULL OR ended_at >= ?)",
		checkID, now.Add(-longest).UnixMilli())
	if err != nil {
		return nil, err
	}
	type span struct{ a, b time.Time }
	var spans []span
	for rows.Next() {
		var (
			s int64
			e sql.NullInt64
		)
		if err := rows.Scan(&s, &e); err != nil {
			rows.Close()
			return nil, err
		}
		end := now
		if e.Valid {
			end = time.UnixMilli(e.Int64)
		}
		spans = append(spans, span{time.UnixMilli(s), end})
	}
	rows.Close()
	for _, w := range Windows {
		start := now.Add(-w.D)
		if created.After(start) {
			start = created
		}
		total := now.Sub(start)
		if total <= 0 {
			out[w.Key] = 100
			continue
		}
		var down time.Duration
		for _, sp := range spans {
			a, b := sp.a, sp.b
			if a.Before(start) {
				a = start
			}
			if b.After(now) {
				b = now
			}
			if b.After(a) {
				down += b.Sub(a)
			}
		}
		v := 100 * (1 - float64(down)/float64(total))
		if v < 0 {
			v = 0
		}
		out[w.Key] = float64(int(v*1000)) / 1000
	}
	return out, nil
}

// Summary counts enabled checks per state.
func Summary(ctx context.Context, d *db.DB) (map[string]int, error) {
	rows, err := d.R.QueryContext(ctx, "SELECT state, COUNT(*) FROM health_checks WHERE enabled = 1 GROUP BY state")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{StateUp: 0, StateDown: 0, StateDegraded: 0, StateUnknown: 0}
	for rows.Next() {
		var (
			s string
			n int
		)
		if err := rows.Scan(&s, &n); err != nil {
			return nil, err
		}
		out[s] = n
	}
	return out, rows.Err()
}

// MetricLatency is the time series of check latencies.
const MetricLatency = "health.latency_ms"

func seriesKey(id int64) string { return fmt.Sprintf("check:%d", id) }

// SeriesKey returns the time series key of a check (metric MetricLatency).
func SeriesKey(id int64) string { return seriesKey(id) }
