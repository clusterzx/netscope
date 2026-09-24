// Package api is the JSON API (/api/v1), the SSE stream, the Prometheus endpoint, the
// OpenAPI spec (/api/openapi.json, docs at /api/docs) and the embedded web UI.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"netscope/internal/audit"
	"netscope/internal/auth"
	"netscope/internal/bus"
	"netscope/internal/config"
	"netscope/internal/db"
	"netscope/internal/events"
	"netscope/internal/federation"
	"netscope/internal/inventory"
	"netscope/internal/logging"
	"netscope/internal/plugin"
	"netscope/internal/pluginhost"
	"netscope/internal/rules"
	"netscope/internal/settings"
	"netscope/internal/tunnel"
	"netscope/internal/vault"
)

// Deps are the services the API uses.
type Deps struct {
	Config    *config.Config
	DB        *db.DB
	Bus       *bus.Bus
	Log       *slog.Logger
	Logs      *logging.Ring
	LevelVar  *slog.LevelVar
	Auth      *auth.Service
	Vault     *vault.Vault
	Settings  *settings.Store
	Inventory *inventory.Store
	Events    *events.Store
	Rules     *rules.Engine
	Host      *pluginhost.Host
	Tunnels   *tunnel.Manager // nil in tests without tunnels
	// Federation joins this instance with a central instance or sites (nil in tests).
	Federation *federation.Service
	Audit      *audit.Log
	Version    string
	StartedAt  time.Time
	// Restore is called with the path of a validated database file; the application
	// swaps the database and restarts its services.
	Restore func(path string)
	// UI serves the embedded single page application (nil = no UI).
	UI http.Handler
}

// Server is the HTTP API.
type Server struct {
	Deps
	routes []*route
	mux    *http.ServeMux
	start  time.Time
}

// Scopes of routes.
const (
	scopePublic = "public"
	scopeRead   = "read"
	scopeWrite  = "write"
)

type param struct {
	Name     string
	In       string // query | path
	Type     string // string | integer | boolean | number
	Desc     string
	Required bool
}

type route struct {
	Method  string
	Path    string
	Tag     string
	Summary string
	Scope   string
	Params  []param
	Body    any
	Resp    any
	Status  int    // success status (default 200)
	Content string // non-JSON response content type
	handler http.HandlerFunc
}

// New builds the server and registers all routes.
func New(d Deps) *Server {
	s := &Server{Deps: d, mux: http.NewServeMux(), start: time.Now()}
	s.registerAuth()
	s.registerSystem()
	s.registerDevices()
	s.registerMeta()
	s.registerTunnels()
	s.registerFederation()
	s.registerPlugins()
	s.registerEvents()
	s.registerRules()
	s.registerCredentials()
	s.registerHealth()
	s.registerVulns()
	s.registerTopology()
	s.registerReports()
	s.registerDashboard()
	s.mux.HandleFunc("GET /api/v1/stream", s.withAuth(scopeRead, s.handleStream))
	s.mux.HandleFunc("GET /metrics", s.handleMetrics)
	s.mux.HandleFunc("GET /api/openapi.json", s.handleOpenAPI)
	s.mux.HandleFunc("GET /api/docs", s.handleDocs)
	s.mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "not_found", "Unbekannter API-Endpunkt", nil)
	})
	if d.UI != nil {
		s.mux.Handle("/", d.UI)
	}
	return s
}

func (s *Server) add(r *route) {
	s.routes = append(s.routes, r)
	s.mux.HandleFunc(r.Method+" "+r.Path, s.withAuth(r.Scope, r.handler))
}

// Handler returns the root handler with all middleware.
func (s *Server) Handler() http.Handler {
	return s.recoverer(s.proxyAware(s.accessLog(s.securityHeaders(s.mux))))
}

// ---------------------------------------------------------------- request context

type ctxKey int

const (
	keyPrincipal ctxKey = iota
	keyClient
)

type clientInfo struct {
	IP     string `json:"ip"`
	Scheme string `json:"scheme"`
	Host   string `json:"host"`
}

func client(r *http.Request) clientInfo {
	if c, ok := r.Context().Value(keyClient).(clientInfo); ok {
		return c
	}
	return clientInfo{IP: r.RemoteAddr, Scheme: "http", Host: r.Host}
}

func principal(r *http.Request) *auth.Principal {
	p, _ := r.Context().Value(keyPrincipal).(*auth.Principal)
	return p
}

func (s *Server) trusted(ip netip.Addr) bool {
	for _, p := range s.Config.Proxies {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// proxyAware resolves the client address, scheme and host honouring X-Forwarded-* headers
// from trusted proxies only (Traefik terminates TLS).
func (s *Server) proxyAware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		ci := clientInfo{IP: host, Scheme: "http", Host: r.Host}
		if r.TLS != nil {
			ci.Scheme = "https"
		}
		if addr, err := netip.ParseAddr(host); err == nil && s.trusted(addr.Unmap()) {
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				parts := strings.Split(xff, ",")
				// walk from the right: the first address that is not a trusted proxy is the client
				for i := len(parts) - 1; i >= 0; i-- {
					cand := strings.TrimSpace(parts[i])
					a, err := netip.ParseAddr(cand)
					if err != nil {
						break
					}
					ci.IP = a.Unmap().String()
					if !s.trusted(a.Unmap()) {
						break
					}
				}
			} else if xr := r.Header.Get("X-Real-IP"); xr != "" {
				if a, err := netip.ParseAddr(strings.TrimSpace(xr)); err == nil {
					ci.IP = a.Unmap().String()
				}
			}
			if p := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])); p == "https" || p == "http" {
				ci.Scheme = p
			}
			if h := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Host"), ",")[0]); h != "" {
				ci.Host = h
			}
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), keyClient, ci)))
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (s *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if r.URL.Path == "/api/v1/stream" || !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/metrics" {
			return
		}
		d := time.Since(start)
		metricsHTTP(r.Method, sw.status, d)
		lvl := slog.LevelDebug
		if sw.status >= 500 {
			lvl = slog.LevelError
		}
		s.Log.Log(r.Context(), lvl, "http", "method", r.Method, "path", r.URL.Path, "status", sw.status,
			"ms", d.Milliseconds(), "client", client(r).IP)
	})
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "same-origin")
		if r.URL.Path != "/api/docs" {
			h.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self' 'unsafe-inline'; script-src 'self' 'unsafe-inline'; connect-src 'self'; frame-ancestors 'none'")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if err, ok := rec.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(rec)
				}
				s.Log.Error("panic in handler", "path", r.URL.Path, "panic", fmt.Sprint(rec), "stack", string(debug.Stack()))
				writeError(w, http.StatusInternalServerError, "internal", "Interner Fehler", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ---------------------------------------------------------------- authentication

const (
	sessionCookie = "ns_session"
	csrfHeader    = "X-NetScope-CSRF"
)

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, exp time.Time) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: token, Path: "/", Expires: exp, HttpOnly: true,
		Secure: client(r).Scheme == "https", SameSite: http.SameSiteLaxMode})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true,
		Secure: client(r).Scheme == "https", SameSite: http.SameSiteLaxMode})
}

// authenticate resolves the principal from a bearer token or the session cookie.
func (s *Server) authenticate(w http.ResponseWriter, r *http.Request) (*auth.Principal, bool) {
	if h := r.Header.Get("Authorization"); h != "" {
		tok, ok := strings.CutPrefix(h, "Bearer ")
		if !ok {
			return nil, false
		}
		p, err := s.Auth.TokenPrincipal(r.Context(), strings.TrimSpace(tok), client(r).IP)
		if err != nil {
			return nil, false
		}
		return p, false
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return nil, false
	}
	p, exp, renewed, err := s.Auth.Session(r.Context(), c.Value)
	if err != nil {
		return nil, false
	}
	if renewed {
		s.setSessionCookie(w, r, c.Value, exp)
	}
	return p, true
}

func (s *Server) withAuth(scope string, h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if scope == scopePublic {
			h(w, r)
			return
		}
		p, viaCookie := s.authenticate(w, r)
		if p == nil {
			writeError(w, http.StatusUnauthorized, "unauthenticated", "Anmeldung erforderlich", nil)
			return
		}
		unsafe := r.Method != http.MethodGet && r.Method != http.MethodHead
		if viaCookie && unsafe && r.Header.Get(csrfHeader) == "" {
			writeError(w, http.StatusForbidden, "csrf", "CSRF-Header fehlt", nil)
			return
		}
		if (scope == scopeWrite || unsafe) && !p.CanWrite() {
			writeError(w, http.StatusForbidden, "forbidden", "Dieses Token darf nur lesen", nil)
			return
		}
		h(w, r.WithContext(context.WithValue(r.Context(), keyPrincipal, p)))
	}
}

// ---------------------------------------------------------------- JSON helpers

type apiError struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Fields  []plugin.FieldError `json:"fields,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string, fields []plugin.FieldError) {
	writeJSON(w, status, map[string]any{"error": apiError{Code: code, Message: msg, Fields: fields}})
}

func isInternal(err error) bool {
	msg := err.Error()
	for _, s := range []string{"SQL logic error", "database is locked", "sqlite", "disk I/O", "no such table", "constraint failed",
		"sql: ", "out of memory", "interrupted"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	return false
}

// fail maps an error to an HTTP response.
func (s *Server) fail(w http.ResponseWriter, r *http.Request, err error) {
	var ve *plugin.ValidationError
	var ce *pluginhost.ConfigError
	switch {
	case errors.Is(err, db.ErrNotFound):
		writeError(w, http.StatusNotFound, "not_found", "Nicht gefunden", nil)
	case errors.As(err, &ve):
		writeError(w, http.StatusBadRequest, "validation", ve.Error(), ve.Errors)
	case errors.As(err, &ce):
		writeError(w, http.StatusBadRequest, "validation", ce.Error(), []plugin.FieldError{{Field: ce.Field, Message: ce.Message}})
	case errors.Is(err, auth.ErrInvalidCredentials):
		writeError(w, http.StatusUnauthorized, "invalid_credentials", err.Error(), nil)
	case errors.Is(err, auth.ErrRateLimited):
		writeError(w, http.StatusTooManyRequests, "rate_limited", err.Error(), nil)
	case errors.Is(err, auth.ErrUnauthenticated):
		writeError(w, http.StatusUnauthorized, "unauthenticated", err.Error(), nil)
	case errors.Is(err, pluginhost.ErrAlreadyQueued):
		writeError(w, http.StatusConflict, "conflict", err.Error(), nil)
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusServiceUnavailable, "timeout", "Anfrage abgebrochen oder Zeitüberschreitung", nil)
	case isInternal(err):
		s.Log.Error("request failed", "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "Interner Fehler (Details im Log)", nil)
	default:
		writeError(w, http.StatusBadRequest, "bad_request", err.Error(), nil)
	}
}

const maxBody = 10 << 20

func decode(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("leerer Request-Body")
		}
		return fmt.Errorf("ungültiges JSON: %w", err)
	}
	return nil
}

// decodeLenient decodes entity objects that clients typically round-trip from a GET
// (read-only fields like ids and counters are ignored instead of rejected).
func decodeLenient(r *http.Request, v any) error {
	dec := json.NewDecoder(io.LimitReader(r.Body, maxBody))
	if err := dec.Decode(v); err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("leerer Request-Body")
		}
		return fmt.Errorf("ungültiges JSON: %w", err)
	}
	return nil
}

func pathID(r *http.Request, name string) (int64, error) {
	v, err := strconv.ParseInt(r.PathValue(name), 10, 64)
	if err != nil || v <= 0 {
		return 0, fmt.Errorf("ungültige ID %q", r.PathValue(name))
	}
	return v, nil
}

func qInt(r *http.Request, name string, def int) int {
	if v := r.URL.Query().Get(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func qInt64(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.URL.Query().Get(name), 10, 64)
	return n
}

func qBool(r *http.Request, name string) bool {
	switch strings.ToLower(r.URL.Query().Get(name)) {
	case "1", "true", "yes", "ja":
		return true
	}
	return false
}

// qTime parses RFC3339, a date (YYYY-MM-DD, local midnight) or unix milliseconds.
func (s *Server) qTime(r *http.Request, name string) (time.Time, error) {
	v := r.URL.Query().Get(name)
	if v == "" {
		return time.Time{}, nil
	}
	if t, err := time.Parse(time.RFC3339, v); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02T15:04", v, s.Config.Location); err == nil {
		return t, nil
	}
	if t, err := time.ParseInLocation("2006-01-02", v, s.Config.Location); err == nil {
		return t, nil
	}
	if ms, err := strconv.ParseInt(v, 10, 64); err == nil {
		return time.UnixMilli(ms), nil
	}
	return time.Time{}, fmt.Errorf("%s: ungültiger Zeitpunkt %q", name, v)
}

// record writes an audit entry for a manual change.
func (s *Server) record(r *http.Request, action, entityType, entityID, summary string, before, after any) {
	name, typ := principal(r).Actor()
	if err := s.Audit.Record(r.Context(), name, typ, client(r).IP, action, entityType, entityID, summary, before, after); err != nil {
		s.Log.Error("audit", "err", err)
	}
}

func actorName(r *http.Request) string {
	n, _ := principal(r).Actor()
	return n
}

// ---------------------------------------------------------------- SSE

func (s *Server) handleStream(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, "internal", "Streaming nicht unterstützt", nil)
		return
	}
	var topics []string
	if t := r.URL.Query().Get("topics"); t != "" {
		topics = strings.Split(t, ",")
	}
	sub := s.Bus.Subscribe(256, topics...)
	defer sub.Close()
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	_, _ = fmt.Fprintf(w, "retry: 3000\n: connected\n\n")
	fl.Flush()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			if _, err := fmt.Fprintf(w, ": ping %d\n\n", time.Now().Unix()); err != nil {
				return
			}
			fl.Flush()
		case msg, ok := <-sub.C:
			if !ok {
				return
			}
			b, err := json.Marshal(msg)
			if err != nil {
				continue
			}
			if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", msg.ID, msg.Topic, b); err != nil {
				return
			}
			fl.Flush()
		}
	}
}
