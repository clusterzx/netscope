package api

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"netscope/internal/auth"
	"netscope/internal/plugin"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type meResponse struct {
	User      *auth.User      `json:"user"`
	Principal *auth.Principal `json:"principal"`
}

type passwordRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

type tokenRequest struct {
	Name      string     `json:"name"`
	Scope     string     `json:"scope"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

type tokenCreated struct {
	Token string      `json:"token"`
	Info  *auth.Token `json:"info"`
}

type okResponse struct {
	OK bool `json:"ok"`
}

func (s *Server) registerAuth() {
	s.add(&route{Method: "POST", Path: "/api/v1/auth/login", Tag: "Auth", Summary: "Anmelden (setzt Session-Cookie)", Scope: scopePublic,
		Body: loginRequest{}, Resp: meResponse{}, handler: s.handleLogin})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/logout", Tag: "Auth", Summary: "Abmelden", Scope: scopePublic, Resp: okResponse{},
		handler: s.handleLogout})
	s.add(&route{Method: "GET", Path: "/api/v1/auth/me", Tag: "Auth", Summary: "Aktueller Benutzer", Scope: scopeRead, Resp: meResponse{},
		handler: s.handleMe})
	s.add(&route{Method: "PUT", Path: "/api/v1/auth/password", Tag: "Auth", Summary: "Passwort ändern (beendet andere Sessions)", Scope: scopeWrite,
		Body: passwordRequest{}, Resp: okResponse{}, handler: s.handlePassword})
	s.add(&route{Method: "GET", Path: "/api/v1/tokens", Tag: "Auth", Summary: "API-Tokens auflisten", Scope: scopeRead, Resp: []auth.Token{},
		handler: s.handleTokens})
	s.add(&route{Method: "POST", Path: "/api/v1/tokens", Tag: "Auth", Summary: "API-Token erstellen (Klartext nur in dieser Antwort)", Scope: scopeWrite,
		Body: tokenRequest{}, Resp: tokenCreated{}, Status: http.StatusCreated, handler: s.handleCreateToken})
	s.add(&route{Method: "DELETE", Path: "/api/v1/tokens/{id}", Tag: "Auth", Summary: "API-Token widerrufen", Scope: scopeWrite, Resp: okResponse{},
		handler: s.handleDeleteToken})
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	token, exp, err := s.Auth.Login(r.Context(), req.Username, req.Password, client(r).IP, r.UserAgent())
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			_ = s.Audit.Record(r.Context(), req.Username, "user", client(r).IP, "auth.login_failed", "user", "", "Fehlgeschlagene Anmeldung", nil, nil)
		}
		s.fail(w, r, err)
		return
	}
	s.setSessionCookie(w, r, token, exp)
	p, _, _, err := s.Auth.Session(r.Context(), token)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u, _ := s.Auth.User(r.Context(), p.UserID)
	_ = s.Audit.Record(r.Context(), p.Username, "user", client(r).IP, "auth.login", "user", strconv.FormatInt(p.UserID, 10), "Anmeldung", nil, nil)
	writeJSON(w, http.StatusOK, meResponse{User: u, Principal: p})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(sessionCookie); err == nil && c.Value != "" {
		_ = s.Auth.Logout(r.Context(), c.Value)
	}
	s.clearSessionCookie(w, r)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	u, err := s.Auth.User(r.Context(), p.UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, meResponse{User: u, Principal: p})
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.Kind != "session" {
		writeError(w, http.StatusForbidden, "forbidden", "Das Passwort kann nur in der Oberfläche geändert werden", nil)
		return
	}
	var req passwordRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if len(req.New) < auth.MinPasswordLength {
		s.fail(w, r, plugin.FieldErr("new", fmt.Sprintf("das Passwort muss mindestens %d Zeichen haben", auth.MinPasswordLength)))
		return
	}
	if err := s.Auth.ChangePassword(r.Context(), p.UserID, p.SessionID, req.Current, req.New); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			// a wrong current password is a form error, not a failed login (no 401 → no logout in the UI)
			err = plugin.FieldErr("current", "Aktuelles Passwort ist falsch")
		}
		s.fail(w, r, err)
		return
	}
	// the generated start password is no longer valid
	_ = os.Remove(filepath.Join(s.Config.DataDir, "admin-initial-password.txt"))
	s.record(r, "auth.password", "user", strconv.FormatInt(p.UserID, 10), "Passwort geändert", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.Auth.ListTokens(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateToken(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	plain, tok, err := s.Auth.CreateToken(r.Context(), principal(r).UserID, req.Name, req.Scope, req.ExpiresAt)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "token.create", "token", strconv.FormatInt(tok.ID, 10), "API-Token „"+tok.Name+"“ ("+tok.Scope+") erstellt", nil, tok)
	writeJSON(w, http.StatusCreated, tokenCreated{Token: plain, Info: tok})
}

func (s *Server) handleDeleteToken(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Auth.DeleteToken(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "token.delete", "token", strconv.FormatInt(id, 10), "API-Token widerrufen", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}
