package api

import (
	"bytes"
	"errors"
	"fmt"
	"net/http"
	"time"

	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/federation"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/pluginhost"
	"netscope/internal/plugins/healthcheck"
	"netscope/internal/reports"
)

type sendReportRequest struct {
	Publishers []string   `json:"publishers"`
	From       *time.Time `json:"from,omitempty"`
	To         *time.Time `json:"to,omitempty"`
}

type deviceCounts struct {
	Total   int `json:"total"`
	Online  int `json:"online"`
	Offline int `json:"offline"`
	New24h  int `json:"new24h"`
	Unknown int `json:"unknown"`
	Ignored int `json:"ignored"`
}

type pluginStatus struct {
	ID         string      `json:"id"`
	Name       string      `json:"name"`
	Kind       plugin.Kind `json:"kind"`
	Enabled    bool        `json:"enabled"`
	Running    bool        `json:"running"`
	LastStatus string      `json:"lastStatus,omitempty"`
	LastRunAt  *time.Time  `json:"lastRunAt,omitempty"`
	LastError  string      `json:"lastError,omitempty"`
	NextRun    *time.Time  `json:"nextRun,omitempty"`
}

type dashboard struct {
	Devices         deviceCounts          `json:"devices"`
	OpenEvents      map[string]int        `json:"openEvents"`
	CriticalEvents  []events.Event        `json:"criticalEvents"`
	Plugins         []pluginStatus        `json:"plugins"`
	PluginsFailed   int                   `json:"pluginsFailed"`
	Health          map[string]int        `json:"health"`
	Availability24h *float64              `json:"availability24h,omitempty"`
	CVEs            map[string]int        `json:"cves"`
	TopCVEs         []topCVE              `json:"topCves"`
	Certificates    []inventory.CertView  `json:"certificates"`
	ActiveRuns      []*pluginhost.RunView `json:"activeRuns"`
	Subnets         []inventory.Subnet    `json:"subnets"`
	// Sites are the connected NetScope sites (central instance).
	Sites       []federation.Site `json:"sites"`
	GeneratedAt time.Time         `json:"generatedAt"`
}

type topCVE struct {
	CVE     string  `json:"cve"`
	CVSS    float64 `json:"cvss"`
	Devices int     `json:"devices"`
}

func (s *Server) registerReports() {
	s.add(&route{Method: "GET", Path: "/api/v1/reports/inventory", Tag: "Reports", Summary: "Inventar-Export (csv, json, pdf)", Scope: scopeRead,
		Params:  []param{{Name: "format", Desc: "csv | json | pdf"}, {Name: "q", Desc: "Geräte-Filter"}, {Name: "site", Desc: "nur ein Standort (Zentrale)"}},
		Content: "application/octet-stream",
		handler: s.handleInventoryReport})
	s.add(&route{Method: "GET", Path: "/api/v1/reports/changes", Tag: "Reports", Summary: "Änderungsbericht für einen Zeitraum (json, md, pdf)",
		Scope: scopeRead, Params: []param{{Name: "from"}, {Name: "to"}, {Name: "format", Desc: "json | md | pdf"}},
		Resp: reports.ChangeReport{}, handler: s.handleChangeReport})
	s.add(&route{Method: "POST", Path: "/api/v1/reports/send", Tag: "Reports", Summary: "Änderungsbericht jetzt über Publisher versenden",
		Scope: scopeWrite, Body: sendReportRequest{}, Resp: okResponse{}, Status: http.StatusAccepted, handler: s.handleSendReport})
}

func (s *Server) registerDashboard() {
	s.add(&route{Method: "GET", Path: "/api/v1/dashboard", Tag: "Dashboard", Summary: "Kennzahlen für das Dashboard", Scope: scopeRead,
		Params: []param{{Name: "site", Desc: "Geräte, Events und CVEs nur eines Standorts (Zentrale)"}}, Resp: dashboard{}, handler: s.handleDashboard})
}

func (s *Server) handleInventoryReport(w http.ResponseWriter, r *http.Request) {
	format := r.URL.Query().Get("format")
	if format == "" {
		format = "csv"
	}
	devs, err := reports.Inventory(r.Context(), s.Inventory, withSite(r.URL.Query().Get("q"), r.URL.Query().Get("site")))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	stamp := time.Now().In(s.Config.Location).Format("20060102-1504")
	var buf bytes.Buffer
	switch format {
	case "csv":
		fields, err := s.Inventory.CustomFields(r.Context())
		if err != nil {
			s.fail(w, r, err)
			return
		}
		keys := make([]string, 0, len(fields))
		for _, f := range fields {
			keys = append(keys, f.Key)
		}
		if err := reports.WriteCSV(&buf, devs, keys); err != nil {
			s.fail(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	case "json":
		if err := reports.WriteJSON(&buf, devs); err != nil {
			s.fail(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
	case "pdf":
		if err := reports.InventoryPDF(&buf, devs, s.Config.Location); err != nil {
			s.fail(w, r, err)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
	default:
		s.fail(w, r, fmt.Errorf("unbekanntes Format %q (csv, json, pdf)", format))
		return
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="netscope-inventar-%s.%s"`, stamp, format))
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) reportRange(r *http.Request) (time.Time, time.Time, error) {
	from, err := s.qTime(r, "from")
	if err != nil {
		return from, from, err
	}
	to, err := s.qTime(r, "to")
	if err != nil {
		return from, to, err
	}
	if to.IsZero() {
		to = time.Now()
	}
	if from.IsZero() {
		from = to.Add(-7 * 24 * time.Hour)
	}
	if !from.Before(to) {
		return from, to, errors.New("from muss vor to liegen")
	}
	return from, to, nil
}

func (s *Server) handleChangeReport(w http.ResponseWriter, r *http.Request) {
	from, to, err := s.reportRange(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rep, err := reports.BuildChangeReport(r.Context(), s.DB, from, to)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	format := r.URL.Query().Get("format")
	if format == "" || format == "json" {
		writeJSON(w, http.StatusOK, rep)
		return
	}
	base := s.Settings.System().PublicURL
	b, ctype, err := rep.ToBytes(format, s.Config.Location, base)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ext := format
	if ext == "markdown" {
		ext = "md"
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="netscope-aenderungen-%s-%s.%s"`,
		from.In(s.Config.Location).Format("20060102"), to.In(s.Config.Location).Format("20060102"), ext))
	_, _ = w.Write(b)
}

func (s *Server) handleSendReport(w http.ResponseWriter, r *http.Request) {
	var req sendReportRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if len(req.Publishers) == 0 {
		s.fail(w, r, errors.New("mindestens einen Publisher wählen"))
		return
	}
	to := time.Now()
	if req.To != nil {
		to = *req.To
	}
	from := to.Add(-7 * 24 * time.Hour)
	if req.From != nil {
		from = *req.From
	}
	if !from.Before(to) {
		s.fail(w, r, errors.New("from muss vor to liegen"))
		return
	}
	for _, id := range req.Publishers {
		p, ok := s.Host.Plugin(id)
		if !ok || p.Info().Kind != plugin.KindPublisher {
			s.fail(w, r, fmt.Errorf("Publisher %q existiert nicht", id))
			return
		}
	}
	rep, err := reports.BuildChangeReport(r.Context(), s.DB, from, to)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	base := s.Settings.System().PublicURL
	if base != "" {
		base += "/reports"
	}
	title := fmt.Sprintf("NetScope Änderungsbericht %s–%s", from.In(s.Config.Location).Format("02.01."), to.In(s.Config.Location).Format("02.01.2006"))
	body := rep.Markdown(s.Config.Location, base)
	extra, err := rep.NotificationExtra(s.Config.Location, s.Settings.System().PublicURL)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	now := db.Now()
	for _, id := range req.Publishers {
		if _, err := s.DB.W.ExecContext(r.Context(), `INSERT INTO notifications(publisher_id, kind, priority, status, event_ids, title, body,
			extra, deliver_after, created_at) VALUES (?, ?, 'normal', 'pending', '[]', ?, ?, ?, ?, ?)`, id, plugin.NotifyReport, title, body, extra, now, now); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	s.record(r, "report.send", "report", "", "Änderungsbericht versendet an "+fmt.Sprint(req.Publishers), nil, nil)
	writeJSON(w, http.StatusAccepted, okResponse{OK: true})
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	d := dashboard{GeneratedAt: time.Now(), CVEs: map[string]int{"critical": 0, "high": 0, "medium": 0, "low": 0}, TopCVEs: []topCVE{},
		Sites: []federation.Site{}}
	site, err := s.siteFilter(ctx, r.URL.Query().Get("site"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	devWhere, devArgs := "1=1", []any{}
	if site != nil {
		if *site == 0 {
			devWhere = "site_id IS NULL"
		} else {
			devWhere, devArgs = "site_id = ?", []any{*site}
		}
	}
	if err := s.DB.R.QueryRowContext(ctx, `SELECT COUNT(*), IFNULL(SUM(online = 1 AND state <> 'ignored'), 0),
		IFNULL(SUM(online = 0 AND state <> 'ignored'), 0), IFNULL(SUM(first_seen >= ? AND state <> 'ignored'), 0),
		IFNULL(SUM(state = 'unknown'), 0), IFNULL(SUM(state = 'ignored'), 0) FROM devices WHERE `+devWhere,
		append([]any{time.Now().Add(-24 * time.Hour).UnixMilli()}, devArgs...)...).
		Scan(&d.Devices.Total, &d.Devices.Online, &d.Devices.Offline, &d.Devices.New24h, &d.Devices.Unknown, &d.Devices.Ignored); err != nil {
		s.fail(w, r, err)
		return
	}
	if d.OpenEvents, err = s.Events.OpenCounts(ctx); err != nil {
		s.fail(w, r, err)
		return
	}
	no := false
	if d.CriticalEvents, _, err = s.Events.List(ctx, events.Filter{MinSeverity: plugin.SevHigh, Acked: &no, Limit: 10, Site: site}); err != nil {
		s.fail(w, r, err)
		return
	}
	views, err := s.Host.Views(ctx)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for _, v := range views {
		ps := pluginStatus{ID: v.Info.ID, Name: v.Info.Name, Kind: v.Info.Kind, Enabled: v.Config.Enabled, Running: v.Running != nil, NextRun: v.NextRun}
		if v.LastRun != nil {
			ps.LastStatus, ps.LastRunAt, ps.LastError = v.LastRun.Status, v.LastRun.FinishedAt, v.LastRun.Error
			if v.Config.Enabled && (v.LastRun.Status == "failed" || v.LastRun.Status == "timeout") {
				d.PluginsFailed++
			}
		}
		d.Plugins = append(d.Plugins, ps)
	}
	if d.Health, err = healthcheck.Summary(ctx, s.DB); err != nil {
		s.fail(w, r, err)
		return
	}
	checks, err := healthcheck.ListChecks(ctx, s.DB, 0)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(checks) > 0 {
		sum, n := 0.0, 0
		for _, c := range checks {
			if c.Enabled {
				sum += c.Availability["24h"]
				n++
			}
		}
		if n > 0 {
			v := sum / float64(n)
			d.Availability24h = &v
		}
	}
	rows, err := s.DB.R.QueryContext(ctx, `SELECT c.cve_id, MAX(IFNULL(c.cvss_score, 0)), COUNT(DISTINCT c.device_id) FROM device_cves c
		WHERE c.gone_at IS NULL AND NOT EXISTS (SELECT 1 FROM cve_ignores i WHERE i.device_id = c.device_id AND i.cve_id = c.cve_id)
		AND c.device_id IN (SELECT id FROM devices WHERE `+devWhere+`)
		GROUP BY c.cve_id ORDER BY 2 DESC, 3 DESC`, devArgs...)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for rows.Next() {
		var t topCVE
		if err := rows.Scan(&t.CVE, &t.CVSS, &t.Devices); err != nil {
			rows.Close()
			s.fail(w, r, err)
			return
		}
		d.CVEs[string(plugin.SeverityFromCVSS(t.CVSS))] += t.Devices
		if len(d.TopCVEs) < 10 {
			d.TopCVEs = append(d.TopCVEs, t)
		}
	}
	rows.Close()
	certs, err := s.Inventory.AllCertificates(ctx, 30)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if len(certs) > 10 {
		certs = certs[:10]
	}
	d.Certificates = certs
	if d.ActiveRuns, err = s.Host.ActiveRuns(ctx); err != nil {
		s.fail(w, r, err)
		return
	}
	if d.Subnets, err = s.Inventory.ListSubnets(ctx); err != nil {
		s.fail(w, r, err)
		return
	}
	if s.Federation != nil && s.Federation.Role() == federation.RoleCentral {
		if d.Sites, err = s.Federation.Sites(ctx); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, d)
}
