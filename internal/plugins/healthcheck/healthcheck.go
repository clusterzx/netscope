// Package healthcheck runs configurable checks (TCP connect, HTTP status/body, TLS
// handshake, ICMP) with flap damping, records outages and latency time series and emits
// health.down / health.degraded / health.up events.
package healthcheck

import (
	"context"
	"crypto/tls"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	probing "github.com/prometheus-community/pro-bing"

	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/timeseries"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin is the health check processor.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "healthcheck",
		Kind: plugin.KindProcessor,
		Name: "Health-Checks",
		Description: "Führt die konfigurierten Checks (TCP, HTTP, TLS, ICMP) im jeweiligen Intervall aus, dämpft Flattern über " +
			"Schwellwerte, führt Ausfallhistorie und Verfügbarkeit (24 h / 7 d / 30 d) und erzeugt Events bei Zustandswechseln.",
		Version:            "1.0.0",
		DefaultEnabled:     true,
		DefaultSchedule:    "@every 30s",
		DefaultTimeout:     5 * time.Minute,
		DefaultConcurrency: 32,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "icmp_privileged", Type: plugin.FieldBool, Label: "ICMP mit Raw-Sockets", Default: true,
			Description: "Benötigt root bzw. NET_RAW (im Container gegeben)."},
		{Key: "user_agent", Type: plugin.FieldString, Label: "HTTP User-Agent", Default: "NetScope-Healthcheck/1.0"},
		{Key: "log_results", Type: plugin.FieldBool, Label: "Jedes Ergebnis protokollieren", Default: false,
			Description: "Sonst werden nur Zustandswechsel und Fehler ins Laufprotokoll geschrieben."},
	}}
}

// Result of one check execution.
type Result struct {
	OK        bool
	Degraded  bool
	LatencyMs float64
	Error     string
}

type runner struct {
	userAgent       string
	icmpPrivileged  bool
	resolveDeviceIP func(ctx context.Context, id int64) (string, error)
}

func (r *runner) target(ctx context.Context, c *Check) (string, error) {
	if c.Target != "" {
		return c.Target, nil
	}
	if c.DeviceID > 0 && r.resolveDeviceIP != nil {
		ip, err := r.resolveDeviceIP(ctx, c.DeviceID)
		if err != nil {
			return "", err
		}
		if ip == "" {
			return "", errors.New("Gerät hat keine IP-Adresse")
		}
		return ip, nil
	}
	return "", errors.New("kein Ziel")
}

func (r *runner) execute(ctx context.Context, c *Check) Result {
	timeout := time.Duration(c.TimeoutS) * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var res Result
	switch c.Type {
	case TypeTCP:
		res = r.tcp(ctx, c)
	case TypeHTTP:
		res = r.http(ctx, c)
	case TypeTLS:
		res = r.tls(ctx, c)
	case TypeICMP:
		res = r.icmp(ctx, c, timeout)
	default:
		res = Result{Error: "unbekannter Typ " + c.Type}
	}
	if res.OK && c.Config.DegradedMs > 0 && res.LatencyMs > float64(c.Config.DegradedMs) {
		res.Degraded = true
		if res.Error == "" {
			res.Error = fmt.Sprintf("Antwortzeit %.0f ms über %d ms", res.LatencyMs, c.Config.DegradedMs)
		}
	}
	return res
}

func (r *runner) tcp(ctx context.Context, c *Check) Result {
	host, err := r.target(ctx, c)
	if err != nil {
		return Result{Error: err.Error()}
	}
	start := time.Now()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(c.Port)))
	if err != nil {
		return Result{Error: shortErr(err)}
	}
	conn.Close()
	return Result{OK: true, LatencyMs: ms(time.Since(start))}
}

func (r *runner) httpURL(ctx context.Context, c *Check) (string, error) {
	if c.Config.URL != "" {
		return c.Config.URL, nil
	}
	host, err := r.target(ctx, c)
	if err != nil {
		return "", err
	}
	scheme := "http"
	if c.Port == 443 || c.Port == 8443 || c.Port == 9443 || c.Port == 8006 {
		scheme = "https"
	}
	return fmt.Sprintf("%s://%s/", scheme, net.JoinHostPort(host, strconv.Itoa(c.Port))), nil
}

func (r *runner) http(ctx context.Context, c *Check) Result {
	u, err := r.httpURL(ctx, c)
	if err != nil {
		return Result{Error: err.Error()}
	}
	match, err := ParseStatus(c.Config.ExpectStatus)
	if err != nil {
		return Result{Error: err.Error()}
	}
	tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: !c.Config.VerifyTLS}, //nolint:gosec // user choice
		DisableKeepAlives: true, Proxy: nil}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr}
	if !c.Config.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	}
	req, err := http.NewRequestWithContext(ctx, c.Config.Method, u, nil)
	if err != nil {
		return Result{Error: err.Error()}
	}
	req.Header.Set("User-Agent", r.userAgent)
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return Result{Error: shortErr(err)}
	}
	defer resp.Body.Close()
	latency := ms(time.Since(start))
	if !match(resp.StatusCode) {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
		return Result{LatencyMs: latency, Error: fmt.Sprintf("HTTP-Status %d nicht erwartet (%s)", resp.StatusCode, expectText(c.Config.ExpectStatus))}
	}
	if c.Config.BodyMatch != "" && c.Config.Method != "HEAD" {
		re, err := regexp.Compile(c.Config.BodyMatch)
		if err != nil {
			return Result{Error: err.Error()}
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err != nil {
			return Result{LatencyMs: latency, Error: shortErr(err)}
		}
		if !re.Match(body) {
			return Result{LatencyMs: latency, Error: "Antwort enthält den erwarteten Text nicht"}
		}
	}
	return Result{OK: true, LatencyMs: latency}
}

func expectText(s string) string {
	if strings.TrimSpace(s) == "" {
		return "200-399"
	}
	return s
}

func (r *runner) tls(ctx context.Context, c *Check) Result {
	host, err := r.target(ctx, c)
	if err != nil {
		return Result{Error: err.Error()}
	}
	sni := c.Config.ServerName
	if sni == "" && net.ParseIP(host) == nil {
		sni = host
	}
	start := time.Now()
	d := tls.Dialer{Config: &tls.Config{ServerName: sni, InsecureSkipVerify: !c.Config.VerifyTLS}} //nolint:gosec // user choice
	conn, err := d.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(c.Port)))
	if err != nil {
		return Result{Error: shortErr(err)}
	}
	defer conn.Close()
	latency := ms(time.Since(start))
	state := conn.(*tls.Conn).ConnectionState()
	if len(state.PeerCertificates) == 0 {
		return Result{LatencyMs: latency, Error: "kein Zertifikat erhalten"}
	}
	leaf := state.PeerCertificates[0]
	left := time.Until(leaf.NotAfter)
	if left < 0 {
		return Result{LatencyMs: latency, Error: "Zertifikat abgelaufen am " + leaf.NotAfter.Local().Format("02.01.2006")}
	}
	res := Result{OK: true, LatencyMs: latency}
	if c.Config.MinDays > 0 && left < time.Duration(c.Config.MinDays)*24*time.Hour {
		res.Degraded = true
		res.Error = fmt.Sprintf("Zertifikat läuft in %d Tagen ab", int(left.Hours()/24))
	}
	return res
}

func (r *runner) icmp(ctx context.Context, c *Check, timeout time.Duration) Result {
	host, err := r.target(ctx, c)
	if err != nil {
		return Result{Error: err.Error()}
	}
	pinger, err := probing.NewPinger(host)
	if err != nil {
		return Result{Error: shortErr(err)}
	}
	pinger.Count = c.Config.Count
	pinger.Interval = 200 * time.Millisecond
	pinger.Timeout = timeout
	pinger.SetPrivileged(r.icmpPrivileged || runtime.GOOS == "windows")
	if err := pinger.RunWithContext(ctx); err != nil {
		return Result{Error: shortErr(err)}
	}
	st := pinger.Statistics()
	if st.PacketsRecv == 0 {
		return Result{Error: fmt.Sprintf("keine Antwort (%d Pakete gesendet)", st.PacketsSent)}
	}
	res := Result{OK: true, LatencyMs: ms(st.AvgRtt)}
	if st.PacketLoss > 0 {
		res.Degraded = true
		res.Error = fmt.Sprintf("%.0f %% Paketverlust", st.PacketLoss)
	}
	return res
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

func shortErr(err error) string {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return "Zeitüberschreitung"
	}
	s := err.Error()
	if i := strings.LastIndex(s, ": "); i > 0 && len(s) > 120 {
		s = s[i+2:]
	}
	return s
}

// Transition is the state machine result of one check execution.
type Transition struct {
	NewState   string
	Changed    bool
	OpenOutage string // state of a new outage ("" = none)
	CloseOpen  bool   // close the currently open outage
}

// Evaluate applies flap damping: a check changes to down/degraded after failThreshold
// consecutive bad results and back to up after recoverThreshold good ones. A check in
// state unknown becomes up immediately on success.
func Evaluate(c *Check, res Result) Transition {
	class := StateUp
	switch {
	case !res.OK:
		class = StateDown
	case res.Degraded:
		class = StateDegraded
	}
	if class == StateUp {
		c.ConsecutiveOK++
		c.ConsecutiveFail = 0
	} else {
		c.ConsecutiveFail++
		c.ConsecutiveOK = 0
	}
	t := Transition{NewState: c.State}
	switch {
	case class == StateUp && c.State == StateUnknown:
		t.NewState = StateUp
	case class == StateUp && (c.State == StateDown || c.State == StateDegraded) && c.ConsecutiveOK >= c.RecoverThreshold:
		t.NewState, t.CloseOpen = StateUp, true
	case class != StateUp && c.State != class && c.ConsecutiveFail >= c.FailThreshold:
		t.NewState, t.OpenOutage = class, class
		t.CloseOpen = c.State == StateDown || c.State == StateDegraded
	}
	t.Changed = t.NewState != c.State
	return t
}

// Run implements plugin.Runner: executes all due checks.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	now := time.Now()
	rows, err := rc.DB.R.QueryContext(ctx, checkSelect+` WHERE c.enabled = 1 AND (c.last_check_at IS NULL OR c.last_check_at + c.interval_s * 1000 - 5000 <= ?)`,
		now.UnixMilli())
	if err != nil {
		return err
	}
	var due []*Check
	for rows.Next() {
		c, err := scanCheck(rows)
		if err != nil {
			rows.Close()
			return err
		}
		due = append(due, c)
	}
	rows.Close()
	if len(due) == 0 {
		rc.SetStat("checks", 0)
		return plugin.ErrNoChanges
	}
	r := &runner{userAgent: rc.Settings.String("user_agent"), icmpPrivileged: rc.Settings.Bool("icmp_privileged"),
		resolveDeviceIP: func(ctx context.Context, id int64) (string, error) {
			d, err := rc.Inventory.Device(ctx, id)
			if err != nil {
				return "", err
			}
			return d.PrimaryIP, nil
		}}
	logAll := rc.Settings.Bool("log_results")
	var failed, changed atomic.Int64
	err = plugin.ForEach(ctx, rc.Parallelism(), due, func(ctx context.Context, c *Check) error {
		res := r.execute(ctx, c)
		if ctx.Err() != nil {
			return nil
		}
		tr, err := record(ctx, rc, c, res)
		if err != nil {
			rc.Log.Warn("Ergebnis speichern fehlgeschlagen", "check", c.Name, "err", err)
			return nil
		}
		if !res.OK {
			failed.Add(1)
		}
		if tr.Changed {
			changed.Add(1)
			rc.Log.Info("Zustandswechsel", "check", c.Name, "state", tr.NewState, "error", res.Error)
		} else if logAll {
			rc.Log.Info("Check ausgeführt", "check", c.Name, "ok", res.OK, "latency_ms", res.LatencyMs, "error", res.Error)
		}
		return nil
	})
	rc.SetStat("checks", len(due))
	rc.SetStat("failed", failed.Load())
	rc.SetStat("state_changes", changed.Load())
	if len(due) > 0 {
		// latencies and last results changed: status boards refresh
		rc.Live("health", "round", map[string]any{"checks": len(due), "failed": failed.Load(), "stateChanges": changed.Load()})
	}
	if err == nil && failed.Load() == 0 && changed.Load() == 0 {
		return plugin.ErrNoChanges // routine round: not kept in the run history
	}
	return err
}

// record stores the result, applies the state machine and emits events.
func record(ctx context.Context, rc *plugin.RunContext, c *Check, res Result) (Transition, error) {
	prevState, since := c.State, c.StateSince
	tr := Evaluate(c, res)
	now := time.Now()
	var latency any
	if res.LatencyMs > 0 {
		latency = res.LatencyMs
	}
	err := rc.DB.Tx(ctx, func(tx *sql.Tx) error {
		stateSince := db.NullMs(since)
		if tr.Changed {
			stateSince = now.UnixMilli()
		}
		if _, err := tx.ExecContext(ctx, `UPDATE health_checks SET state = ?, state_since = ?, consecutive_fail = ?, consecutive_ok = ?,
			last_check_at = ?, last_ok = ?, last_latency_ms = ?, last_error = ? WHERE id = ?`, tr.NewState, stateSince, c.ConsecutiveFail,
			c.ConsecutiveOK, now.UnixMilli(), db.Bool(res.OK && !res.Degraded), latency, res.Error, c.ID); err != nil {
			return err
		}
		if tr.CloseOpen {
			if _, err := tx.ExecContext(ctx, "UPDATE health_outages SET ended_at = ? WHERE check_id = ? AND ended_at IS NULL", now.UnixMilli(), c.ID); err != nil {
				return err
			}
		}
		if tr.OpenOutage != "" {
			if _, err := tx.ExecContext(ctx, "INSERT INTO health_outages(check_id, state, started_at, reason) VALUES (?,?,?,?)",
				c.ID, tr.OpenOutage, now.UnixMilli(), res.Error); err != nil {
				return err
			}
		}
		if res.OK {
			id, err := timeseries.SeriesID(ctx, tx, MetricLatency, c.DeviceID, seriesKey(c.ID), "ms")
			if err != nil {
				return err
			}
			if err := timeseries.Append(ctx, tx, id, now, res.LatencyMs, res.LatencyMs, res.LatencyMs); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return tr, err
	}
	c.State = tr.NewState
	if tr.Changed {
		live := map[string]any{"checkId": c.ID, "state": tr.NewState, "previousState": prevState, "deviceId": c.DeviceID}
		if res.LatencyMs > 0 {
			live["latencyMs"] = res.LatencyMs
		}
		rc.Live("health", "state", live)
	}
	if !tr.Changed || (prevState == StateUnknown && tr.NewState == StateUp) {
		return tr, nil
	}
	ev := plugin.Event{DeviceID: c.DeviceID, Payload: map[string]any{"check_id": c.ID, "check_name": c.Name, "check_type": c.Type}}
	switch tr.NewState {
	case StateDown:
		ev.Type = plugin.EvHealthDown
		ev.Title = "Check ausgefallen: " + c.Name
		ev.Message = res.Error
		ev.Payload["error"] = res.Error
	case StateDegraded:
		ev.Type = plugin.EvHealthDegraded
		ev.Title = "Check beeinträchtigt: " + c.Name
		ev.Message = res.Error
		ev.Payload["error"] = res.Error
		ev.Payload["latency_ms"] = res.LatencyMs
	case StateUp:
		ev.Type = plugin.EvHealthUp
		ev.Title = "Check wieder OK: " + c.Name
		if since != nil {
			d := now.Sub(*since)
			ev.Payload["down_seconds"] = int64(d.Seconds())
			ev.Message = fmt.Sprintf("Nach %s wieder erreichbar", d.Round(time.Second))
		}
	}
	if _, err := rc.Events.Emit(ctx, ev); err != nil {
		rc.Log.Warn("Event konnte nicht gespeichert werden", "err", err)
	}
	return tr, nil
}

// RunNow executes one check immediately (API "Jetzt prüfen") and stores the result.
func RunNow(ctx context.Context, rc *plugin.RunContext, id int64) (*Check, Result, error) {
	c, err := GetCheck(ctx, rc.DB, id)
	if err != nil {
		return nil, Result{}, err
	}
	r := &runner{userAgent: rc.Settings.String("user_agent"), icmpPrivileged: rc.Settings.Bool("icmp_privileged"),
		resolveDeviceIP: func(ctx context.Context, id int64) (string, error) {
			d, err := rc.Inventory.Device(ctx, id)
			if err != nil {
				return "", err
			}
			return d.PrimaryIP, nil
		}}
	res := r.execute(ctx, c)
	if _, err := record(ctx, rc, c, res); err != nil {
		return nil, res, err
	}
	c, err = GetCheck(ctx, rc.DB, id)
	return c, res, err
}
