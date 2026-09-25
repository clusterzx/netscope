package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"netscope/internal/auth"
	"netscope/internal/plugin"
	"netscope/internal/pluginhost"
)

type runRequest struct {
	Scope *plugin.Scope `json:"scope,omitempty"`
}

type runList struct {
	Total int                   `json:"total"`
	Items []*pluginhost.RunView `json:"items"`
}

func (s *Server) registerPlugins() {
	s.add(&route{Method: "GET", Path: "/api/v1/plugins", Tag: "Plugins", Summary: "Alle Plugins mit Schema, Konfiguration und Status",
		Scope: scopeRead, Resp: []pluginhost.PluginView{}, handler: s.handlePlugins})
	s.add(&route{Method: "GET", Path: "/api/v1/plugins/{id}", Tag: "Plugins", Summary: "Ein Plugin", Scope: scopeRead,
		Resp: pluginhost.PluginView{}, handler: s.handlePlugin})
	s.add(&route{Method: "PUT", Path: "/api/v1/plugins/{id}/config", Tag: "Plugins",
		Summary: "Konfiguration ändern (aktiv, Zeitplan, Timeout, Retries, Parallelität, Scope, Einstellungen) – wirkt sofort",
		Scope:   scopeWrite, Body: pluginhost.ConfigInput{}, Resp: pluginhost.PluginView{}, Perm: auth.PermPluginsManage, handler: s.handlePluginConfig})
	s.add(&route{Method: "POST", Path: "/api/v1/plugins/{id}/run", Tag: "Plugins", Summary: "Jetzt ausführen", Scope: scopeWrite,
		Body: runRequest{}, Resp: idResponse{}, Status: http.StatusAccepted, Perm: auth.PermDevicesScan, handler: s.handleRunPlugin})
	s.add(&route{Method: "POST", Path: "/api/v1/plugins/{id}/actions/{action}", Tag: "Plugins", Summary: "Plugin-Aktion ausführen",
		Scope: scopeWrite, Params: []param{{Name: "wait", Type: "integer", Desc: "Sekunden auf das Ende warten (Standard 20, 0 = sofort mit runId antworten)"}},
		Body: actionRequest{}, Resp: pluginhost.ActionOutcome{}, Perm: auth.PermPluginsManage, handler: s.handlePluginAction})
	s.add(&route{Method: "POST", Path: "/api/v1/plugins/{id}/test", Tag: "Plugins", Summary: "Testnachricht über einen Publisher senden",
		Scope: scopeWrite, Resp: okResponse{}, Perm: auth.PermPluginsManage, handler: s.handleTestPublisher})
	s.add(&route{Method: "GET", Path: "/api/v1/runs", Tag: "Läufe", Summary: "Laufhistorie", Scope: scopeRead,
		Params: []param{{Name: "plugin"}, {Name: "status", Desc: "kommagetrennt"}, {Name: "kind"},
			{Name: "scope", Desc: "full = nur Läufe über ganze Subnetze (keine Einzelgeräte)"}, {Name: "before", Type: "integer", Desc: "nur Läufe mit kleinerer ID, z. B. ?plugin=nmap&status=success&before=123&limit=1 = Vorgängerlauf"}, {Name: "limit", Type: "integer"},
			{Name: "offset", Type: "integer"}}, Resp: runList{}, handler: s.handleRuns})
	s.add(&route{Method: "GET", Path: "/api/v1/runs/active", Tag: "Läufe", Summary: "Laufende Läufe", Scope: scopeRead,
		Resp: []pluginhost.RunView{}, handler: s.handleActiveRuns})
	s.add(&route{Method: "GET", Path: "/api/v1/runs/{id}", Tag: "Läufe", Summary: "Ein Lauf", Scope: scopeRead,
		Resp: pluginhost.RunView{}, handler: s.handleRun})
	s.add(&route{Method: "GET", Path: "/api/v1/runs/{id}/logs", Tag: "Läufe", Summary: "Protokoll eines Laufs", Scope: scopeRead,
		Params: []param{{Name: "after", Type: "integer", Desc: "nur Zeilen nach dieser ID"}, {Name: "limit", Type: "integer"}},
		Resp:   []pluginhost.RunLog{}, handler: s.handleRunLogs})
	s.add(&route{Method: "POST", Path: "/api/v1/runs/{id}/cancel", Tag: "Läufe", Summary: "Lauf abbrechen", Scope: scopeWrite,
		Resp: okResponse{}, Perm: auth.PermDevicesScan, handler: s.handleCancelRun})
}

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request) {
	list, err := s.Host.Views(r.Context())
	s.respond(w, r, list, err)
}

func (s *Server) handlePlugin(w http.ResponseWriter, r *http.Request) {
	v, err := s.Host.View(r.Context(), r.PathValue("id"))
	s.respond(w, r, v, err)
}

func (s *Server) handlePluginConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var in pluginhost.ConfigInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	before, after, err := s.Host.UpdateConfig(r.Context(), id, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	p, _ := s.Host.Plugin(id)
	s.record(r, "plugin.config", "plugin", id, "Konfiguration geändert: "+p.Info().Name, before, after)
	v, err := s.Host.View(r.Context(), id)
	s.respond(w, r, v, err)
}

func (s *Server) handleRunPlugin(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req runRequest
	if r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	runID, err := s.Host.Trigger(r.Context(), id, pluginhost.TriggerOptions{Trigger: pluginhost.TriggerManual, Scope: req.Scope,
		RequestedBy: actorName(r)})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "plugin.run", "plugin", id, "Lauf gestartet", nil, req)
	writeJSON(w, http.StatusAccepted, idResponse{ID: runID})
}

func (s *Server) handlePluginAction(w http.ResponseWriter, r *http.Request) {
	id, action := r.PathValue("id"), r.PathValue("action")
	var req actionRequest
	if r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	wait := 20 * time.Second
	if v := r.URL.Query().Get("wait"); v != "" {
		// ?wait=0 returns right after queueing (long actions such as the NVD sync)
		sec, err := strconv.Atoi(v)
		if err != nil || sec < 0 || sec > 60 {
			s.fail(w, r, plugin.FieldErr("wait", "0–60 Sekunden"))
			return
		}
		wait = time.Duration(sec) * time.Second
	}
	out, err := s.Host.RunAction(r.Context(), id, action, req.Params, nil, actorName(r), wait)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "plugin.action", "plugin", id, "Aktion "+action, nil, req.Params)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleTestPublisher(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.Host.TestPublisher(r.Context(), id, actorName(r)); err != nil {
		writeError(w, http.StatusBadGateway, "publish_failed", "Versand fehlgeschlagen: "+err.Error(), nil)
		return
	}
	s.record(r, "plugin.test", "plugin", id, "Testnachricht gesendet", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleRuns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var status []string
	if v := q.Get("status"); v != "" {
		status = strings.Split(v, ",")
	}
	list, total, err := s.Host.Runs(r.Context(), pluginhost.RunFilter{PluginID: q.Get("plugin"), Status: status, Kind: plugin.Kind(q.Get("kind")),
		Before: qInt64(r, "before"), FullScope: q.Get("scope") == "full", Limit: qInt(r, "limit", 50), Offset: qInt(r, "offset", 0)})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, runList{Total: total, Items: list})
}

func (s *Server) handleActiveRuns(w http.ResponseWriter, r *http.Request) {
	list, err := s.Host.ActiveRuns(r.Context())
	s.respond(w, r, list, err)
}

func (s *Server) handleRun(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v, err := s.Host.Run(r.Context(), id)
	s.respond(w, r, v, err)
}

func (s *Server) handleRunLogs(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	logs, err := s.Host.RunLogs(r.Context(), id, qInt64(r, "after"), qInt(r, "limit", 1000))
	s.respond(w, r, logs, err)
}

func (s *Server) handleCancelRun(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Host.Cancel(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "run.cancel", "run", strconv.FormatInt(id, 10), fmt.Sprintf("Lauf %d abgebrochen", id), nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}
