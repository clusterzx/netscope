package api

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"netscope/internal/auth"
	"netscope/internal/inventory"
	"netscope/internal/plugins/healthcheck"
	"netscope/internal/timeseries"
)

type healthBoard struct {
	Summary map[string]int       `json:"summary"`
	Checks  []*healthcheck.Check `json:"checks"`
	Outages []healthcheck.Outage `json:"outages"`
}

type checkRunResult struct {
	Check  *healthcheck.Check `json:"check"`
	OK     bool               `json:"ok"`
	Error  string             `json:"error,omitempty"`
	Millis float64            `json:"latencyMs"`
}

type graphEdgeRequest struct {
	ParentID   int64  `json:"parentId"`
	ChildID    int64  `json:"childId"`
	Kind       string `json:"kind"`
	ParentPort string `json:"parentPort"`
	ChildPort  string `json:"childPort"`
	Label      string `json:"label"`
}

func (s *Server) registerHealth() {
	s.add(&route{Method: "GET", Path: "/api/v1/health/board", Tag: "Health", Summary: "Statusboard aller Checks mit Verfügbarkeit und Ausfällen",
		Scope: scopeRead, Resp: healthBoard{}, handler: s.handleHealthBoard})
	s.add(&route{Method: "GET", Path: "/api/v1/health-checks", Tag: "Health", Summary: "Health-Checks", Scope: scopeRead,
		Params: []param{{Name: "device", Type: "integer"}}, Resp: []healthcheck.Check{}, handler: s.handleChecks})
	s.add(&route{Method: "POST", Path: "/api/v1/health-checks", Tag: "Health", Summary: "Health-Check anlegen", Scope: scopeWrite,
		Body: healthcheck.Check{}, Resp: healthcheck.Check{}, Status: http.StatusCreated, Perm: auth.PermHealthManage, handler: s.handleSaveCheck})
	s.add(&route{Method: "GET", Path: "/api/v1/health-checks/{id}", Tag: "Health", Summary: "Ein Health-Check", Scope: scopeRead,
		Resp: healthcheck.Check{}, handler: s.handleCheck})
	s.add(&route{Method: "PUT", Path: "/api/v1/health-checks/{id}", Tag: "Health", Summary: "Health-Check ändern", Scope: scopeWrite,
		Body: healthcheck.Check{}, Resp: healthcheck.Check{}, Perm: auth.PermHealthManage, handler: s.handleSaveCheck})
	s.add(&route{Method: "DELETE", Path: "/api/v1/health-checks/{id}", Tag: "Health", Summary: "Health-Check löschen", Scope: scopeWrite,
		Resp: okResponse{}, Perm: auth.PermHealthManage, handler: s.handleDeleteCheck})
	s.add(&route{Method: "POST", Path: "/api/v1/health-checks/{id}/run", Tag: "Health", Summary: "Check sofort ausführen", Scope: scopeWrite,
		Resp: checkRunResult{}, Perm: auth.PermHealthManage, handler: s.handleRunCheck})
	s.add(&route{Method: "GET", Path: "/api/v1/health-checks/{id}/outages", Tag: "Health", Summary: "Ausfallhistorie eines Checks", Scope: scopeRead,
		Params: []param{{Name: "limit", Type: "integer"}}, Resp: []healthcheck.Outage{}, handler: s.handleCheckOutages})
	s.add(&route{Method: "GET", Path: "/api/v1/health-checks/{id}/latency", Tag: "Health", Summary: "Latenz-Zeitreihe eines Checks", Scope: scopeRead,
		Params: []param{{Name: "from"}, {Name: "to"}, {Name: "points", Type: "integer"}}, Resp: seriesResponse{}, handler: s.handleCheckLatency})
}

func (s *Server) registerTopology() {
	s.add(&route{Method: "GET", Path: "/api/v1/topology", Tag: "Topologie", Summary: "Topologie-Graph (Knoten und Kanten)", Scope: scopeRead,
		Params: []param{{Name: "subnet"}, {Name: "tag"}, {Name: "q", Desc: "Geräte-Filter"}, {Name: "site", Desc: "nur ein Standort (Zentrale)"},
			{Name: "containers", Type: "boolean"},
			{Name: "ignored", Type: "boolean"}}, Resp: inventory.Graph{}, handler: s.handleTopology})
	s.add(&route{Method: "GET", Path: "/api/v1/topology/edges", Tag: "Topologie", Summary: "Manuelle Kanten", Scope: scopeRead,
		Resp: []inventory.Relation{}, handler: s.handleManualEdges})
	s.add(&route{Method: "POST", Path: "/api/v1/topology/edges", Tag: "Topologie", Summary: "Manuelle (geschützte) Kante anlegen", Scope: scopeWrite,
		Body: graphEdgeRequest{}, Resp: idResponse{}, Status: http.StatusCreated, Perm: auth.PermDevicesEdit, handler: s.handleAddEdge})
	s.add(&route{Method: "DELETE", Path: "/api/v1/topology/edges/{id}", Tag: "Topologie", Summary: "Kante löschen", Scope: scopeWrite,
		Resp: okResponse{}, Perm: auth.PermDevicesEdit, handler: s.handleDeleteEdge})
}

func (s *Server) handleHealthBoard(w http.ResponseWriter, r *http.Request) {
	sum, err := healthcheck.Summary(r.Context(), s.DB)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	checks, err := healthcheck.ListChecks(r.Context(), s.DB, 0)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	outages, err := healthcheck.Outages(r.Context(), s.DB, 0, 50)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	// latency sparklines: one series per check
	series := map[string]int64{}
	rows, err := s.DB.R.QueryContext(r.Context(), "SELECT id, key FROM ts_series WHERE metric = ?", healthcheck.MetricLatency)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	for rows.Next() {
		var id int64
		var key string
		if rows.Scan(&id, &key) == nil {
			series[key] = id
		}
	}
	rows.Close()
	now := time.Now()
	for _, c := range checks {
		if sid, ok := series[healthcheck.SeriesKey(c.ID)]; ok {
			pts, _, err := timeseries.Query(r.Context(), s.DB.R, sid, now.Add(-24*time.Hour), now, 48)
			if err != nil {
				s.fail(w, r, err)
				return
			}
			c.Latency24h = pts
		}
	}
	writeJSON(w, http.StatusOK, healthBoard{Summary: sum, Checks: checks, Outages: outages})
}

func (s *Server) handleChecks(w http.ResponseWriter, r *http.Request) {
	list, err := healthcheck.ListChecks(r.Context(), s.DB, qInt64(r, "device"))
	s.respond(w, r, list, err)
}

func (s *Server) handleCheck(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := healthcheck.GetCheck(r.Context(), s.DB, id)
	s.respond(w, r, c, err)
}

func (s *Server) handleSaveCheck(w http.ResponseWriter, r *http.Request) {
	id, err := optionalID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var c healthcheck.Check
	if err := decodeLenient(r, &c); err != nil {
		s.fail(w, r, err)
		return
	}
	var before *healthcheck.Check
	if id > 0 {
		if before, err = healthcheck.GetCheck(r.Context(), s.DB, id); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	c.ID = id
	if c.DeviceID > 0 && strings.TrimSpace(c.Target) == "" {
		// the check would run from here against an address of the site's network
		if err := s.siteDeviceError(r.Context(), c.DeviceID, "Health-Checks"); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	if err := healthcheck.SaveCheck(r.Context(), s.DB, &c); err != nil {
		s.fail(w, r, err)
		return
	}
	saved, err := healthcheck.GetCheck(r.Context(), s.DB, c.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	action, status := "health.update", http.StatusOK
	if id == 0 {
		action, status = "health.create", http.StatusCreated
	}
	s.record(r, action, "healthcheck", strconv.FormatInt(saved.ID, 10), "Health-Check „"+saved.Name+"“", before, saved)
	writeJSON(w, status, saved)
}

func (s *Server) handleDeleteCheck(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	before, err := healthcheck.GetCheck(r.Context(), s.DB, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := healthcheck.DeleteCheck(r.Context(), s.DB, id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "health.delete", "healthcheck", strconv.FormatInt(id, 10), "Health-Check „"+before.Name+"“ gelöscht", before, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleRunCheck(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rc, ok := s.Host.Context("healthcheck")
	if !ok {
		s.fail(w, r, errors.New("Health-Check-Plugin nicht vorhanden"))
		return
	}
	c, res, err := healthcheck.RunNow(r.Context(), rc, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, checkRunResult{Check: c, OK: res.OK && !res.Degraded, Error: res.Error, Millis: res.LatencyMs})
}

func (s *Server) handleCheckOutages(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	list, err := healthcheck.Outages(r.Context(), s.DB, id, qInt(r, "limit", 100))
	s.respond(w, r, list, err)
}

func (s *Server) handleCheckLatency(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := healthcheck.GetCheck(r.Context(), s.DB, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := seriesResponse{Series: []timeseries.Series{}, Points: []timeseries.Point{}, Metric: healthcheck.MetricLatency, Key: healthcheck.SeriesKey(id)}
	var sid int64
	err = s.DB.R.QueryRowContext(r.Context(), "SELECT id FROM ts_series WHERE metric = ? AND key = ? AND IFNULL(device_id, 0) = ?",
		out.Metric, out.Key, c.DeviceID).Scan(&sid)
	if err != nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	from, err := s.qTime(r, "from")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	to, err := s.qTime(r, "to")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if to.IsZero() {
		to = time.Now()
	}
	if from.IsZero() {
		from = to.Add(-24 * time.Hour)
	}
	pts, res, err := timeseries.Query(r.Context(), s.DB.R, sid, from, to, qInt(r, "points", 300))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out.Points, out.Resolution = pts, res
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleTopology(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	g, err := s.Inventory.Graph(r.Context(), inventory.GraphFilter{Subnet: q.Get("subnet"), Tag: q.Get("tag"), Query: withSite(q.Get("q"), q.Get("site")),
		IncludeContainers: qBool(r, "containers"), IncludeIgnored: qBool(r, "ignored")})
	s.respond(w, r, g, err)
}

func (s *Server) handleManualEdges(w http.ResponseWriter, r *http.Request) {
	list, err := s.Inventory.ManualRelations(r.Context())
	s.respond(w, r, list, err)
}

func (s *Server) handleAddEdge(w http.ResponseWriter, r *http.Request) {
	var req graphEdgeRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	id, err := s.Inventory.AddManualRelation(r.Context(), req.ParentID, req.ChildID, req.Kind, req.ParentPort, req.ChildPort, req.Label)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "topology.edge_create", "relation", strconv.FormatInt(id, 10), "Manuelle Kante "+s.Inventory.Name(r.Context(), req.ParentID)+
		" → "+s.Inventory.Name(r.Context(), req.ChildID), nil, req)
	writeJSON(w, http.StatusCreated, idResponse{ID: id})
}

func (s *Server) handleDeleteEdge(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rel, err := s.Inventory.DeleteRelation(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "topology.edge_delete", "relation", strconv.FormatInt(id, 10), "Kante "+rel.ParentName+" → "+rel.ChildName+" gelöscht", rel, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}
