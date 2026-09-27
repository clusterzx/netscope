package api

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"netscope/internal/agent"
	"netscope/internal/agent/proto"
	"netscope/internal/auth"
)

type agentsResponse struct {
	Agents []agent.Agent `json:"agents"`
	// Binaries are the agent builds this instance hands out (empty: not in this build).
	Binaries []agent.Binary `json:"binaries"`
	Version  string         `json:"version"`
	// BaseURL is the address agents are installed against (public URL or this request).
	BaseURL string `json:"baseUrl"`
}

type enrollmentCreated struct {
	Enrollment *agent.Enrollment `json:"enrollment"`
	Token      string            `json:"token"` // shown once
	// Command installs the agent on Linux (Docker adds --docker), CommandWindows in an
	// administrator PowerShell.
	Command        string `json:"command"`
	CommandDocker  string `json:"commandDocker"`
	CommandWindows string `json:"commandWindows"`
}

// maxAgentBody bounds an uncompressed agent report (the script output is limited to 17 MB).
const maxAgentBody = 32 << 20

func (s *Server) registerAgents() {
	// agent protocol: own authentication (installation token, agent secret)
	s.add(&route{Method: "POST", Path: proto.PathEnroll, Tag: "Agents", Summary: "Agent anmelden (Installations-Token nse_…)",
		Scope: scopePublic, Body: proto.EnrollRequest{}, Resp: proto.EnrollResponse{}, handler: s.handleAgentEnroll})
	s.add(&route{Method: "GET", Path: proto.PathPoll, Tag: "Agents", Summary: "Agent: Einstellungen, Aktualisierung anfordern, Update (Long Poll; Agent-Secret nsag_…)",
		Scope: scopePublic, Params: []param{{Name: "wait", Type: "integer", Desc: "Sekunden (höchstens 55)"}, {Name: "version"}},
		Resp: proto.PollResponse{}, handler: s.handleAgentPoll})
	s.add(&route{Method: "POST", Path: proto.PathInventory, Tag: "Agents", Summary: "Agent: Inventar liefern (Agent-Secret, gzip erlaubt)",
		Scope: scopePublic, Body: proto.InventoryReport{}, Resp: okResponse{}, handler: s.handleAgentInventory})
	s.add(&route{Method: "POST", Path: proto.PathMetrics, Tag: "Agents", Summary: "Agent: Messwerte liefern (Agent-Secret, gzip erlaubt)",
		Scope: scopePublic, Body: proto.MetricsReport{}, Resp: okResponse{}, handler: s.handleAgentMetrics})

	s.add(&route{Method: "GET", Path: "/api/v1/agents", Tag: "Agents", Summary: "Agents mit Status und verfügbaren Builds", Scope: scopeRead,
		Params: []param{{Name: "device", Type: "integer", Desc: "nur der Agent dieses Geräts"}}, Resp: agentsResponse{}, handler: s.handleAgents})
	s.add(&route{Method: "GET", Path: "/api/v1/agents/{id}", Tag: "Agents", Summary: "Agent", Scope: scopeRead, Params: idParam,
		Resp: agent.Agent{}, handler: s.handleAgent})
	s.add(&route{Method: "POST", Path: "/api/v1/agents/{id}/refresh", Tag: "Agents", Summary: "Inventar jetzt anfordern", Scope: scopeWrite,
		Perm: auth.PermDevicesScan, Params: idParam, Resp: okResponse{}, handler: s.handleAgentRefresh})
	s.add(&route{Method: "DELETE", Path: "/api/v1/agents/{id}", Tag: "Agents", Summary: "Agent entfernen (beendet den Dienst auf dem System; das Gerät bleibt)",
		Scope: scopeWrite, Perm: auth.PermAgentsManage, Params: idParam, Resp: okResponse{}, handler: s.handleAgentDelete})
	s.add(&route{Method: "GET", Path: "/api/v1/agent-enrollments", Tag: "Agents", Summary: "Installations-Tokens", Scope: scopeRead,
		Perm: auth.PermAgentsManage, Resp: []agent.Enrollment{}, handler: s.handleEnrollments})
	s.add(&route{Method: "POST", Path: "/api/v1/agent-enrollments", Tag: "Agents", Summary: "Installations-Token und -befehl erzeugen (Token nur in dieser Antwort)",
		Scope: scopeWrite, Perm: auth.PermAgentsManage, Body: agent.EnrollmentInput{}, Resp: enrollmentCreated{}, Status: http.StatusCreated,
		handler: s.handleCreateEnrollment})
	s.add(&route{Method: "DELETE", Path: "/api/v1/agent-enrollments/{id}", Tag: "Agents", Summary: "Installations-Token widerrufen (installierte Agents laufen weiter)",
		Scope: scopeWrite, Perm: auth.PermAgentsManage, Params: idParam, Resp: okResponse{}, handler: s.handleRevokeEnrollment})

	// downloads for install.sh / install.ps1 and the self-update (contain no secrets)
	s.mux.HandleFunc("GET /agent/install.sh", s.handleAgentInstall)
	s.mux.HandleFunc("GET /agent/install.ps1", s.handleAgentInstallWindows)
	s.mux.HandleFunc("GET "+proto.PathBinary+"{file}", s.handleAgentBinary)
}

func (s *Server) agentsAvailable(w http.ResponseWriter) bool {
	if s.Agents == nil {
		writeError(w, http.StatusNotFound, "not_found", "Agents sind in dieser Instanz nicht verfügbar", nil)
		return false
	}
	return true
}

// agentBase is the address agents reach this instance at.
func (s *Server) agentBase(r *http.Request) string {
	if u := strings.TrimRight(s.Settings.System().PublicURL, "/"); u != "" {
		return u
	}
	c := client(r)
	return c.Scheme + "://" + c.Host
}

// decodeAgent reads a (possibly gzip-compressed) JSON body.
func decodeAgent(r *http.Request, v any) error {
	var body io.Reader = r.Body
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			return fmt.Errorf("gzip: %w", err)
		}
		defer zr.Close()
		body = zr
	}
	if err := json.NewDecoder(io.LimitReader(body, maxAgentBody)).Decode(v); err != nil {
		return fmt.Errorf("ungültige Lieferung: %w", err)
	}
	return nil
}

func (s *Server) agentFail(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, agent.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "unauthenticated", err.Error(), nil)
	case errors.Is(err, agent.ErrInvalidToken):
		writeError(w, http.StatusForbidden, "invalid_token", err.Error(), nil)
	case errors.Is(err, agent.ErrDisabled):
		writeError(w, http.StatusServiceUnavailable, "disabled", err.Error(), nil)
	case errors.Is(err, agent.ErrNoDevice):
		writeError(w, http.StatusConflict, "no_device", err.Error(), nil)
	default:
		s.fail(w, r, err)
	}
}

// agentSession authenticates an agent request.
func (s *Server) agentSession(w http.ResponseWriter, r *http.Request) (*agent.Session, bool) {
	if !s.agentsAvailable(w) {
		return nil, false
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "Agent-Secret fehlt", nil)
		return nil, false
	}
	sess, err := s.Agents.Authenticate(r.Context(), strings.TrimSpace(tok), client(r).IP)
	if err != nil {
		s.agentFail(w, r, err)
		return nil, false
	}
	return sess, true
}

func (s *Server) handleAgentEnroll(w http.ResponseWriter, r *http.Request) {
	if !s.agentsAvailable(w) {
		return
	}
	var req proto.EnrollRequest
	if err := decodeAgent(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	resp, err := s.Agents.Enroll(r.Context(), req, client(r).IP)
	if err != nil {
		s.agentFail(w, r, err)
		return
	}
	_ = s.Audit.Record(r.Context(), "agent "+req.Host.Hostname, "system", client(r).IP, "agent.enroll", "agent",
		strconv.FormatInt(resp.AgentID, 10), "Agent auf „"+req.Host.Hostname+"“ angemeldet", nil, req.Host)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAgentPoll(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.agentSession(w, r)
	if !ok {
		return
	}
	wait := time.Duration(qInt(r, "wait", 0)) * time.Second
	resp, err := s.Agents.Poll(r.Context(), sess, wait, r.URL.Query().Get("version"))
	if err != nil {
		s.agentFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleAgentInventory(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.agentSession(w, r)
	if !ok {
		return
	}
	var rep proto.InventoryReport
	if err := decodeAgent(r, &rep); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Agents.ReportInventory(r.Context(), sess, rep); err != nil {
		s.agentFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleAgentMetrics(w http.ResponseWriter, r *http.Request) {
	sess, ok := s.agentSession(w, r)
	if !ok {
		return
	}
	var rep proto.MetricsReport
	if err := decodeAgent(r, &rep); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Agents.ReportMetrics(r.Context(), sess, rep); err != nil {
		s.agentFail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleAgents(w http.ResponseWriter, r *http.Request) {
	if !s.agentsAvailable(w) {
		return
	}
	list, err := s.Agents.Agents(r.Context(), int64(qInt(r, "device", 0)))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, agentsResponse{Agents: list, Binaries: s.Agents.Binaries(), Version: s.Version, BaseURL: s.agentBase(r)})
}

func (s *Server) handleAgent(w http.ResponseWriter, r *http.Request) {
	if !s.agentsAvailable(w) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	a, err := s.Agents.Agent(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) handleAgentRefresh(w http.ResponseWriter, r *http.Request) {
	if !s.agentsAvailable(w) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Agents.RequestRefresh(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleAgentDelete(w http.ResponseWriter, r *http.Request) {
	if !s.agentsAvailable(w) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	a, err := s.Agents.Agent(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Agents.Delete(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "agent.delete", "agent", strconv.FormatInt(id, 10), "Agent auf „"+a.Hostname+"“ entfernt", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleEnrollments(w http.ResponseWriter, r *http.Request) {
	if !s.agentsAvailable(w) {
		return
	}
	list, err := s.Agents.Enrollments(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// installCommand is the one-liner for an installation token.
func installCommand(base, token string, docker bool) string {
	cmd := "curl -fsSL " + base + "/agent/install.sh | sudo sh -s -- --token " + token
	if docker {
		cmd += " --docker"
	}
	return cmd
}

// installCommandWindows is the PowerShell one-liner (administrator PowerShell). Windows
// PowerShell 5.1 may still default to TLS 1.0/1.1, so https switches TLS 1.2 on first.
func installCommandWindows(base, token string) string {
	cmd := "& ([scriptblock]::Create((irm '" + strings.ReplaceAll(base, "'", "''") + "/agent/install.ps1'))) -Token " + token
	if strings.HasPrefix(base, "https:") {
		cmd = "[Net.ServicePointManager]::SecurityProtocol = 'Tls12'; " + cmd
	}
	return cmd
}

func (s *Server) handleCreateEnrollment(w http.ResponseWriter, r *http.Request) {
	if !s.agentsAvailable(w) {
		return
	}
	var in agent.EnrollmentInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	e, token, err := s.Agents.CreateEnrollment(r.Context(), in, actorName(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "agent.enrollment_create", "agent_enrollment", strconv.FormatInt(e.ID, 10), "Installations-Token „"+e.Name+"“ erzeugt", nil, in)
	base := s.agentBase(r)
	writeJSON(w, http.StatusCreated, enrollmentCreated{Enrollment: e, Token: token, Command: installCommand(base, token, false),
		CommandDocker: installCommand(base, token, true), CommandWindows: installCommandWindows(base, token)})
}

func (s *Server) handleRevokeEnrollment(w http.ResponseWriter, r *http.Request) {
	if !s.agentsAvailable(w) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Agents.RevokeEnrollment(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "agent.enrollment_revoke", "agent_enrollment", strconv.FormatInt(id, 10), "Installations-Token widerrufen", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleAgentInstall(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/x-shellscript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, agent.InstallScript(s.agentBase(r)))
}

func (s *Server) handleAgentInstallWindows(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = io.WriteString(w, agent.InstallScriptWindows(s.agentBase(r)))
}

func (s *Server) handleAgentBinary(w http.ResponseWriter, r *http.Request) {
	if s.Agents == nil {
		http.NotFound(w, r)
		return
	}
	file := r.PathValue("file")
	platform, sum := strings.CutSuffix(file, ".sha256")
	b := s.Agents.Binary(platform)
	if b == nil {
		writeError(w, http.StatusNotFound, "not_found", "Kein Agent-Build für "+platform+" in dieser Instanz (Plattformen: "+
			strings.Join(agent.Platforms, ", ")+")", nil)
		return
	}
	if sum {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "%s  netscope-agent-%s\n", b.SHA256, platform)
		return
	}
	f, err := os.Open(s.Agents.BinaryPath(platform))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", `attachment; filename="netscope-agent-`+platform+`"`)
	http.ServeContent(w, r, "", fi.ModTime(), f)
}
