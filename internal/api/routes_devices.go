package api

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"netscope/internal/events"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/pluginhost"
	"netscope/internal/plugins/healthcheck"
	"netscope/internal/timeseries"
)

type deviceList struct {
	Total  int                   `json:"total"`
	Items  []inventory.DeviceRow `json:"items"`
	TookMs int64                 `json:"tookMs"`
}

type createDeviceRequest struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	MAC  string `json:"mac"`
}

type idResponse struct {
	ID int64 `json:"id"`
}

type bulkResponse struct {
	Affected int                       `json:"affected"`
	Outcome  *pluginhost.ActionOutcome `json:"outcome,omitempty"`
}

type bulkRequest struct {
	inventory.BulkAction
	Plugin string         `json:"plugin,omitempty"` // for action "plugin_action"
	Name   string         `json:"name,omitempty"`
	Params map[string]any `json:"params,omitempty"`
}

type mergeRequest struct {
	Target  int64   `json:"target"`
	Sources []int64 `json:"sources"`
}

type splitRequest struct {
	MACs []string `json:"macs"`
}

type scanRequest struct {
	Plugins []string `json:"plugins"`
}

type scanResponse struct {
	Runs map[string]int64 `json:"runs"`
}

type actionRequest struct {
	Params map[string]any `json:"params,omitempty"`
}

type eventList struct {
	Total int            `json:"total"`
	Items []events.Event `json:"items"`
}

type seriesResponse struct {
	Series     []timeseries.Series `json:"series"`
	Points     []timeseries.Point  `json:"points"`
	Resolution string              `json:"resolution"`
	Metric     string              `json:"metric"`
	Key        string              `json:"key"`
}

func (s *Server) registerDevices() {
	idp := []param{{Name: "history", Type: "boolean", Desc: "auch historische Einträge"}}
	s.add(&route{Method: "GET", Path: "/api/v1/devices", Tag: "Geräte", Summary: "Geräteliste mit Filter-Query-Sprache", Scope: scopeRead,
		Params: []param{{Name: "q", Desc: "Filter, z. B. tag:iot port:22 os:linux cve>=7 seen<24h"}, {Name: "sort", Desc: "Feld, - für absteigend"},
			{Name: "limit", Type: "integer"}, {Name: "offset", Type: "integer"}, {Name: "ports", Type: "boolean", Desc: "Portliste mitliefern"}},
		Resp: deviceList{}, handler: s.handleDevices})
	s.add(&route{Method: "POST", Path: "/api/v1/devices", Tag: "Geräte", Summary: "Gerät manuell anlegen", Scope: scopeWrite,
		Body: createDeviceRequest{}, Resp: idResponse{}, Status: http.StatusCreated, handler: s.handleCreateDevice})
	s.add(&route{Method: "POST", Path: "/api/v1/devices/bulk", Tag: "Geräte", Summary: "Massenaktion (Tags, Gruppe, Zustand, Kritikalität, Löschen, Plugin-Aktion wie WOL)",
		Scope: scopeWrite, Body: bulkRequest{}, Resp: bulkResponse{}, handler: s.handleBulk})
	s.add(&route{Method: "POST", Path: "/api/v1/devices/merge", Tag: "Geräte", Summary: "Geräte zusammenführen", Scope: scopeWrite,
		Body: mergeRequest{}, Resp: okResponse{}, handler: s.handleMerge})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}", Tag: "Geräte", Summary: "Gerätedetail", Scope: scopeRead,
		Resp: inventory.DeviceDetail{}, handler: s.handleDevice})
	s.add(&route{Method: "PATCH", Path: "/api/v1/devices/{id}", Tag: "Geräte", Summary: "Manuelle Daten ändern", Scope: scopeWrite,
		Body: inventory.DeviceUpdate{}, Resp: inventory.DeviceDetail{}, handler: s.handleUpdateDevice})
	s.add(&route{Method: "DELETE", Path: "/api/v1/devices/{id}", Tag: "Geräte", Summary: "Gerät löschen (Events bleiben)", Scope: scopeWrite,
		Resp: okResponse{}, handler: s.handleDeleteDevice})
	s.add(&route{Method: "POST", Path: "/api/v1/devices/{id}/split", Tag: "Geräte", Summary: "MAC-Adressen in ein neues Gerät abspalten",
		Scope: scopeWrite, Body: splitRequest{}, Resp: idResponse{}, handler: s.handleSplit})
	s.add(&route{Method: "POST", Path: "/api/v1/devices/{id}/scan", Tag: "Geräte", Summary: "Scanner jetzt für dieses Gerät ausführen",
		Scope: scopeWrite, Body: scanRequest{}, Resp: scanResponse{}, Status: http.StatusAccepted, handler: s.handleScanDevice})
	s.add(&route{Method: "POST", Path: "/api/v1/devices/{id}/actions/{plugin}/{action}", Tag: "Geräte", Summary: "Geräteaktion eines Plugins (z. B. WOL)",
		Scope: scopeWrite, Body: actionRequest{}, Resp: pluginhost.ActionOutcome{}, handler: s.handleDeviceAction})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/ports", Tag: "Geräte", Summary: "Ports & Dienste", Scope: scopeRead, Params: idp,
		Resp: []inventory.PortView{}, handler: s.handleDevicePorts})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/http", Tag: "Geräte", Summary: "HTTP-Endpunkte und erkannte Web-Apps", Scope: scopeRead,
		Resp: []inventory.HTTPView{}, handler: s.handleDeviceHTTP})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/certificates", Tag: "Geräte", Summary: "Zertifikate", Scope: scopeRead, Params: idp,
		Resp: []inventory.CertView{}, handler: s.handleDeviceCerts})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/packages", Tag: "Geräte", Summary: "Installierte Pakete", Scope: scopeRead,
		Params: []param{{Name: "q"}, {Name: "limit", Type: "integer"}, {Name: "offset", Type: "integer"}},
		Resp:   inventory.PackageList{}, handler: s.handleDevicePackages})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/containers", Tag: "Geräte", Summary: "Container und Images", Scope: scopeRead, Params: idp,
		Resp: inventory.ContainerData{}, handler: s.handleDeviceContainers})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/inventory", Tag: "Geräte", Summary: "Strukturiertes Inventar je Quelle (SSH, SNMP, UPnP …)",
		Scope: scopeRead, Resp: map[string]any{}, handler: s.handleDeviceInventory})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/health", Tag: "Geräte", Summary: "Health-Checks des Geräts", Scope: scopeRead,
		Resp: []healthcheck.Check{}, handler: s.handleDeviceHealth})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/events", Tag: "Geräte", Summary: "Events des Geräts", Scope: scopeRead,
		Params: []param{{Name: "limit", Type: "integer"}, {Name: "offset", Type: "integer"}}, Resp: eventList{}, handler: s.handleDeviceEvents})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/timeline", Tag: "Geräte", Summary: "Historie (Events und Zustandsänderungen)",
		Scope: scopeRead, Params: []param{{Name: "limit", Type: "integer"}}, Resp: []inventory.TimelineEntry{}, handler: s.handleTimeline})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/relations", Tag: "Geräte", Summary: "Beziehungen (Eltern/Kinder, Topologie)",
		Scope: scopeRead, Resp: []inventory.Relation{}, handler: s.handleDeviceRelations})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/observations", Tag: "Geräte", Summary: "Rohdaten pro Plugin", Scope: scopeRead,
		Params: []param{{Name: "plugin", Desc: "leer = letzte Beobachtung je Plugin"}, {Name: "limit", Type: "integer"}},
		Resp:   []inventory.ObservationView{}, handler: s.handleObservations})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/timeseries", Tag: "Geräte", Summary: "Zeitreihen (z. B. icmp.rtt_ms, icmp.loss_pct)",
		Scope: scopeRead, Params: []param{{Name: "metric"}, {Name: "key"}, {Name: "from"}, {Name: "to"}, {Name: "points", Type: "integer"}},
		Resp: seriesResponse{}, handler: s.handleDeviceSeries})
	s.add(&route{Method: "GET", Path: "/api/v1/tags", Tag: "Geräte", Summary: "Alle Tags mit Anzahl", Scope: scopeRead,
		Resp: []inventory.TagCount{}, handler: s.handleTags})
	s.add(&route{Method: "GET", Path: "/api/v1/certificates", Tag: "Geräte", Summary: "Alle aktiven Zertifikate nach Ablaufdatum", Scope: scopeRead,
		Params: []param{{Name: "days", Type: "integer", Desc: "nur Ablauf innerhalb N Tagen"}}, Resp: []inventory.CertView{}, handler: s.handleAllCerts})
}

func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	q := r.URL.Query()
	res, err := s.Inventory.List(r.Context(), inventory.ListOptions{Query: q.Get("q"), Sort: q.Get("sort"), Limit: qInt(r, "limit", 0),
		Offset: qInt(r, "offset", 0), WithPorts: qBool(r, "ports")})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, deviceList{Total: res.Total, Items: res.Items, TookMs: time.Since(start).Milliseconds()})
}

func (s *Server) handleCreateDevice(w http.ResponseWriter, r *http.Request) {
	var req createDeviceRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	id, err := s.Inventory.CreateManual(r.Context(), req.Name, req.IP, req.MAC)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "device.create", "device", strconv.FormatInt(id, 10), "Gerät manuell angelegt", nil, req)
	writeJSON(w, http.StatusCreated, idResponse{ID: id})
}

func (s *Server) handleDevice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d, err := s.Inventory.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleUpdateDevice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var u inventory.DeviceUpdate
	if err := decode(r, &u); err != nil {
		s.fail(w, r, err)
		return
	}
	before, after, err := s.Inventory.Update(r.Context(), id, u)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "device.update", "device", strconv.FormatInt(id, 10), "Gerät geändert: "+s.Inventory.Name(r.Context(), id), before, after)
	d, err := s.Inventory.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDeleteDevice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	name := s.Inventory.Name(r.Context(), id)
	before, _ := s.Inventory.ManualSnapshot(r.Context(), s.DB.R, id)
	if err := s.Inventory.Delete(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "device.delete", "device", strconv.FormatInt(id, 10), "Gerät gelöscht: "+name, before, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleBulk(w http.ResponseWriter, r *http.Request) {
	var req bulkRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if req.Action == "plugin_action" {
		out, err := s.Host.RunAction(r.Context(), req.Plugin, req.Name, req.Params, req.IDs, actorName(r), 15*time.Second)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		s.record(r, "device.bulk_action", "device", "", fmt.Sprintf("Aktion %s/%s für %d Geräte", req.Plugin, req.Name, len(req.IDs)), nil, req)
		writeJSON(w, http.StatusOK, bulkResponse{Affected: len(req.IDs), Outcome: out})
		return
	}
	n, err := s.Inventory.Bulk(r.Context(), req.BulkAction)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "device.bulk", "device", "", fmt.Sprintf("Massenaktion %s für %d Geräte", req.Action, n), nil, req.BulkAction)
	writeJSON(w, http.StatusOK, bulkResponse{Affected: n})
}

func (s *Server) handleMerge(w http.ResponseWriter, r *http.Request) {
	var req mergeRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	names := []string{}
	for _, id := range req.Sources {
		names = append(names, s.Inventory.Name(r.Context(), id))
	}
	target := s.Inventory.Name(r.Context(), req.Target)
	if err := s.Inventory.Merge(r.Context(), req.Target, req.Sources); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "device.merge", "device", strconv.FormatInt(req.Target, 10),
		fmt.Sprintf("%s zusammengeführt in %s", strings.Join(names, ", "), target), req, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleSplit(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req splitRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	nid, err := s.Inventory.Split(r.Context(), id, req.MACs)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "device.split", "device", strconv.FormatInt(id, 10), fmt.Sprintf("MACs %s in neues Gerät %d abgespalten",
		strings.Join(req.MACs, ", "), nid), nil, req)
	writeJSON(w, http.StatusOK, idResponse{ID: nid})
}

func (s *Server) handleScanDevice(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if _, err := s.Inventory.Device(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	var req scanRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if len(req.Plugins) == 0 {
		s.fail(w, r, errors.New("keine Scanner ausgewählt"))
		return
	}
	out := scanResponse{Runs: map[string]int64{}}
	for _, pid := range req.Plugins {
		p, ok := s.Host.Plugin(pid)
		if !ok {
			s.fail(w, r, fmt.Errorf("Plugin %q existiert nicht", pid))
			return
		}
		if _, ok := p.(plugin.Runner); !ok || p.Info().Targets == plugin.TargetNone {
			s.fail(w, r, fmt.Errorf("%s kann nicht für einzelne Geräte ausgeführt werden", p.Info().Name))
			return
		}
		runID, err := s.Host.Trigger(r.Context(), pid, pluginhost.TriggerOptions{Trigger: pluginhost.TriggerDevice,
			Scope: &plugin.Scope{Devices: []int64{id}}, RequestedBy: actorName(r)})
		if err != nil {
			s.fail(w, r, err)
			return
		}
		out.Runs[pid] = runID
	}
	s.record(r, "device.scan", "device", strconv.FormatInt(id, 10), "Scan gestartet: "+strings.Join(req.Plugins, ", "), nil, nil)
	writeJSON(w, http.StatusAccepted, out)
}

func (s *Server) handleDeviceAction(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req actionRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	pid, action := r.PathValue("plugin"), r.PathValue("action")
	out, err := s.Host.RunAction(r.Context(), pid, action, req.Params, []int64{id}, actorName(r), 15*time.Second)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "device.action", "device", strconv.FormatInt(id, 10), fmt.Sprintf("Aktion %s/%s ausgeführt", pid, action), nil, req.Params)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deviceID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return 0, false
	}
	return id, true
}

func (s *Server) handleDevicePorts(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.deviceID(w, r); ok {
		list, err := s.Inventory.Ports(r.Context(), id, qBool(r, "history"))
		s.respond(w, r, list, err)
	}
}

func (s *Server) handleDeviceHTTP(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.deviceID(w, r); ok {
		list, err := s.Inventory.HTTPServices(r.Context(), id)
		s.respond(w, r, list, err)
	}
}

func (s *Server) handleDeviceCerts(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.deviceID(w, r); ok {
		list, err := s.Inventory.Certificates(r.Context(), id, qBool(r, "history"))
		s.respond(w, r, list, err)
	}
}

func (s *Server) handleDevicePackages(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.deviceID(w, r); ok {
		list, err := s.Inventory.Packages(r.Context(), id, r.URL.Query().Get("q"), qInt(r, "limit", 200), qInt(r, "offset", 0))
		s.respond(w, r, list, err)
	}
}

func (s *Server) handleDeviceContainers(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.deviceID(w, r); ok {
		list, err := s.Inventory.Containers(r.Context(), id, qBool(r, "history"))
		s.respond(w, r, list, err)
	}
}

func (s *Server) handleDeviceInventory(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.deviceID(w, r); ok {
		inv, err := s.Inventory.Inventory(r.Context(), id)
		s.respond(w, r, inv, err)
	}
}

func (s *Server) handleDeviceHealth(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.deviceID(w, r); ok {
		list, err := healthcheck.ListChecks(r.Context(), s.DB, id)
		s.respond(w, r, list, err)
	}
}

func (s *Server) handleDeviceEvents(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.deviceID(w, r); ok {
		list, total, err := s.Events.List(r.Context(), events.Filter{DeviceID: id, Limit: qInt(r, "limit", 100), Offset: qInt(r, "offset", 0)})
		if err != nil {
			s.fail(w, r, err)
			return
		}
		writeJSON(w, http.StatusOK, eventList{Total: total, Items: list})
	}
}

func (s *Server) handleTimeline(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.deviceID(w, r); ok {
		list, err := s.Inventory.Timeline(r.Context(), id, qInt(r, "limit", 300))
		s.respond(w, r, list, err)
	}
}

func (s *Server) handleDeviceRelations(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.deviceID(w, r); ok {
		list, err := s.Inventory.DeviceRelations(r.Context(), id)
		s.respond(w, r, list, err)
	}
}

func (s *Server) handleObservations(w http.ResponseWriter, r *http.Request) {
	if id, ok := s.deviceID(w, r); ok {
		list, err := s.Inventory.Observations(r.Context(), id, r.URL.Query().Get("plugin"), qInt(r, "limit", 20))
		s.respond(w, r, list, err)
	}
}

func (s *Server) handleDeviceSeries(w http.ResponseWriter, r *http.Request) {
	id, ok := s.deviceID(w, r)
	if !ok {
		return
	}
	series, err := timeseries.ListSeries(r.Context(), s.DB.R, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := seriesResponse{Series: series, Points: []timeseries.Point{}, Metric: r.URL.Query().Get("metric"), Key: r.URL.Query().Get("key")}
	if out.Metric == "" {
		writeJSON(w, http.StatusOK, out)
		return
	}
	var sid int64
	for _, se := range series {
		if se.Metric == out.Metric && se.Key == out.Key {
			sid = se.ID
		}
	}
	if sid == 0 {
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

func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	list, err := s.Inventory.Tags(r.Context())
	s.respond(w, r, list, err)
}

func (s *Server) handleAllCerts(w http.ResponseWriter, r *http.Request) {
	list, err := s.Inventory.AllCertificates(r.Context(), qInt(r, "days", 0))
	s.respond(w, r, list, err)
}

// respond writes v or the error.
func (s *Server) respond(w http.ResponseWriter, r *http.Request, v any, err error) {
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}
