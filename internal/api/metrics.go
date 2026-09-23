package api

import (
	"fmt"
	"math"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type httpStat struct {
	count   int64
	seconds float64
}

var (
	httpMu    sync.Mutex
	httpStats = map[string]*httpStat{}
)

func metricsHTTP(method string, status int, d time.Duration) {
	key := method + "\x00" + strconv.Itoa(status)
	httpMu.Lock()
	st := httpStats[key]
	if st == nil {
		st = &httpStat{}
		httpStats[key] = st
	}
	st.count++
	st.seconds += d.Seconds()
	httpMu.Unlock()
}

func escLabel(v string) string {
	return strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`).Replace(v)
}

type promWriter struct {
	b    strings.Builder
	seen map[string]bool
}

func (p *promWriter) sample(name, help, typ string, labels map[string]string, v float64) {
	if !p.seen[name] {
		p.seen[name] = true
		fmt.Fprintf(&p.b, "# HELP %s %s\n# TYPE %s %s\n", name, help, name, typ)
	}
	p.b.WriteString(name)
	if len(labels) > 0 {
		keys := make([]string, 0, len(labels))
		for k := range labels {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		p.b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				p.b.WriteByte(',')
			}
			fmt.Fprintf(&p.b, `%s="%s"`, k, escLabel(labels[k]))
		}
		p.b.WriteByte('}')
	}
	if math.IsNaN(v) {
		p.b.WriteString(" NaN\n")
		return
	}
	fmt.Fprintf(&p.b, " %s\n", strconv.FormatFloat(v, 'g', -1, 64))
}

// handleMetrics serves Prometheus metrics. Unless enabled as public in the system
// settings, a session or API token is required.
func (s *Server) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if !s.Settings.System().MetricsPublic {
		if p, _ := s.authenticate(w, r); p == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="netscope"`)
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Anmeldung erforderlich (API-Token)", nil)
			return
		}
	}
	ctx := r.Context()
	p := &promWriter{seen: map[string]bool{}}
	p.sample("netscope_build_info", "Build-Information", "gauge", map[string]string{"version": s.Version, "go": runtime.Version()}, 1)
	p.sample("netscope_uptime_seconds", "Laufzeit seit dem Start", "gauge", nil, time.Since(s.StartedAt).Seconds())
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	p.sample("netscope_go_goroutines", "Anzahl Goroutinen", "gauge", nil, float64(runtime.NumGoroutine()))
	p.sample("netscope_go_memory_alloc_bytes", "Belegter Heap", "gauge", nil, float64(ms.Alloc))
	p.sample("netscope_go_memory_sys_bytes", "Vom Betriebssystem bezogener Speicher", "gauge", nil, float64(ms.Sys))
	p.sample("netscope_db_size_bytes", "Größe der Datenbank inkl. WAL", "gauge", nil, float64(s.DB.Size()))
	p.sample("netscope_sse_clients", "Verbundene Live-Clients", "gauge", nil, float64(s.Bus.Subscribers()))
	httpMu.Lock()
	keys := make([]string, 0, len(httpStats))
	for k := range httpStats {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		method, code, _ := strings.Cut(k, "\x00")
		st := httpStats[k]
		p.sample("netscope_http_requests_total", "HTTP-Anfragen an die API", "counter", map[string]string{"method": method, "code": code}, float64(st.count))
		p.sample("netscope_http_request_seconds_total", "Summierte Antwortzeit der API", "counter", map[string]string{"method": method, "code": code}, st.seconds)
	}
	httpMu.Unlock()
	rows, err := s.DB.R.QueryContext(ctx, "SELECT state, online, COUNT(*) FROM devices GROUP BY state, online")
	if err == nil {
		for rows.Next() {
			var (
				state  string
				online int
				n      int
			)
			if rows.Scan(&state, &online, &n) == nil {
				p.sample("netscope_devices", "Geräte nach Zustand und Erreichbarkeit", "gauge",
					map[string]string{"state": state, "online": strconv.FormatBool(online == 1)}, float64(n))
			}
		}
		rows.Close()
	}
	if counts, err := s.Events.OpenCounts(ctx); err == nil {
		for sev, n := range counts {
			p.sample("netscope_events_open", "Nicht quittierte Events nach Schweregrad", "gauge", map[string]string{"severity": sev}, float64(n))
		}
	}
	rows, err = s.DB.R.QueryContext(ctx, "SELECT state, COUNT(*) FROM health_checks WHERE enabled = 1 GROUP BY state")
	if err == nil {
		for rows.Next() {
			var (
				state string
				n     int
			)
			if rows.Scan(&state, &n) == nil {
				p.sample("netscope_health_checks", "Health-Checks nach Zustand", "gauge", map[string]string{"state": state}, float64(n))
			}
		}
		rows.Close()
	}
	var cves int
	if s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*) FROM device_cves c WHERE c.gone_at IS NULL
		AND NOT EXISTS (SELECT 1 FROM cve_ignores i WHERE i.device_id = c.device_id AND i.cve_id = c.cve_id)`).Scan(&cves) == nil {
		p.sample("netscope_device_cves_active", "Aktive, nicht ignorierte CVE-Treffer", "gauge", nil, float64(cves))
	}
	var pending int
	if s.DB.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM notifications WHERE status IN ('pending','sending')").Scan(&pending) == nil {
		p.sample("netscope_notifications_pending", "Ausstehende Benachrichtigungen", "gauge", nil, float64(pending))
	}
	for _, m := range s.Host.Metrics() {
		p.sample(m.Name, m.Help, m.Type, m.Labels, m.Value)
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(p.b.String()))
}
