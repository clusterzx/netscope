package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"netscope/internal/auth"
)

// Sign-in through LDAP (same login form) and OIDC (redirect to the identity provider), and
// their configuration.

const oidcStateCookie = "ns_oidc"

type oidcSaveRequest struct {
	Config auth.OIDCConfig `json:"config"`
	// ClientSecret: omitted keeps the stored secret, "" removes it.
	ClientSecret *string `json:"clientSecret,omitempty"`
}

type ldapSaveRequest struct {
	Config auth.LDAPConfig `json:"config"`
	// BindPassword: omitted keeps the stored password, "" removes it.
	BindPassword *string `json:"bindPassword,omitempty"`
}

type ldapTestRequest struct {
	Config       auth.LDAPConfig `json:"config"`
	BindPassword *string         `json:"bindPassword,omitempty"`
	// Username (search) and Password (sign-in) of a test account, both optional.
	Username string `json:"username,omitempty"`
	Password string `json:"password,omitempty"`
}

type oidcTestRequest struct {
	Config auth.OIDCConfig `json:"config"`
}

type externalAuthResponse struct {
	*auth.ExternalAuth
	// RedirectURL to register at the identity provider (derived from this request).
	RedirectURL string `json:"redirectUrl"`
}

func (s *Server) registerExternalAuth() {
	s.add(&route{Method: "GET", Path: "/api/v1/auth/providers", Tag: "Auth", Summary: "Anmeldeverfahren für die Login-Seite (OIDC, LDAP)",
		Scope: scopePublic, Resp: auth.Providers{}, handler: s.handleProviders})
	s.add(&route{Method: "GET", Path: "/api/v1/auth/oidc/start", Tag: "Auth", Summary: "Anmeldung per OIDC beginnen (Weiterleitung zum Identity Provider)",
		Scope: scopePublic, Params: []param{{Name: "next", Desc: "Seite nach der Anmeldung"}}, handler: s.handleOIDCStart})
	s.add(&route{Method: "GET", Path: "/api/v1/auth/oidc/callback", Tag: "Auth", Summary: "Rückkehr vom Identity Provider (setzt das Session-Cookie)",
		Scope: scopePublic, handler: s.handleOIDCCallback})
	s.add(&route{Method: "GET", Path: "/api/v1/system/auth", Tag: "System", Summary: "Anmeldung über LDAP und OIDC: Einstellungen",
		Scope: scopeRead, Perm: auth.PermUsersManage, Resp: externalAuthResponse{}, handler: s.handleExternalAuth})
	s.add(&route{Method: "PUT", Path: "/api/v1/system/auth/oidc", Tag: "System", Summary: "OIDC-Anmeldung einrichten",
		Scope: scopeWrite, Perm: auth.PermUsersManage, Body: oidcSaveRequest{}, Resp: auth.OIDCConfig{}, handler: s.handleSaveOIDC})
	s.add(&route{Method: "PUT", Path: "/api/v1/system/auth/ldap", Tag: "System", Summary: "LDAP-Anmeldung einrichten",
		Scope: scopeWrite, Perm: auth.PermUsersManage, Body: ldapSaveRequest{}, Resp: auth.LDAPConfig{}, handler: s.handleSaveLDAP})
	s.add(&route{Method: "POST", Path: "/api/v1/system/auth/oidc/test", Tag: "System", Summary: "OIDC-Einstellungen prüfen (Discovery, Schlüssel)",
		Scope: scopeWrite, Perm: auth.PermUsersManage, Body: oidcTestRequest{}, Resp: auth.OIDCTest{}, handler: s.handleTestOIDC})
	s.add(&route{Method: "POST", Path: "/api/v1/system/auth/ldap/test", Tag: "System", Summary: "LDAP-Einstellungen prüfen (Verbindung, Dienstkonto, Testbenutzer)",
		Scope: scopeWrite, Perm: auth.PermUsersManage, Body: ldapTestRequest{}, Resp: auth.LDAPTest{}, handler: s.handleTestLDAP})
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.Auth.Providers(r.Context()))
}

// localPath accepts only paths of this application as target after the sign-in.
func localPath(p string) string {
	if !strings.HasPrefix(p, "/") || strings.HasPrefix(p, "//") || strings.HasPrefix(p, "/\\") || strings.HasPrefix(p, "/login") ||
		strings.HasPrefix(p, "/api/") {
		return "/"
	}
	return p
}

// oidcRedirectURI is the callback address of this instance as the browser sees it.
func oidcRedirectURI(r *http.Request) string {
	c := client(r)
	return c.Scheme + "://" + c.Host + "/api/v1/auth/oidc/callback"
}

func loginError(w http.ResponseWriter, r *http.Request, msg, next string) {
	q := url.Values{"error": {msg}}
	if next != "" && next != "/" {
		q.Set("next", next)
	}
	http.Redirect(w, r, "/login?"+q.Encode(), http.StatusFound)
}

func (s *Server) handleOIDCStart(w http.ResponseWriter, r *http.Request) {
	next := localPath(r.URL.Query().Get("next"))
	authURL, state, err := s.Auth.OIDCStart(r.Context(), oidcRedirectURI(r), next)
	if err != nil {
		s.Log.Warn("OIDC-Anmeldung", "error", err)
		loginError(w, r, err.Error(), next)
		return
	}
	// binds the sign-in to this browser: the callback must come back with the same state
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: state, Path: "/api/v1/auth/oidc", MaxAge: int((10 * time.Minute).Seconds()),
		HttpOnly: true, Secure: client(r).Scheme == "https", SameSite: http.SameSiteLaxMode})
	http.Redirect(w, r, authURL, http.StatusFound)
}

func (s *Server) handleOIDCCallback(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	state := q.Get("state")
	http.SetCookie(w, &http.Cookie{Name: oidcStateCookie, Value: "", Path: "/api/v1/auth/oidc", MaxAge: -1, HttpOnly: true,
		Secure: client(r).Scheme == "https", SameSite: http.SameSiteLaxMode})
	c, err := r.Cookie(oidcStateCookie)
	if err != nil || state == "" || c.Value != state {
		loginError(w, r, "Die Anmeldung wurde in einem anderen Browser begonnen oder ist abgelaufen – bitte erneut versuchen", "")
		return
	}
	if e := q.Get("error"); e != "" {
		msg := "Der Identity Provider hat die Anmeldung abgelehnt: " + e
		if d := q.Get("error_description"); d != "" {
			msg += " (" + d + ")"
		}
		_, _, _ = s.Auth.OIDCCallback(r.Context(), state, "", client(r).IP, r.UserAgent()) // drop the pending sign-in
		loginError(w, r, msg, "")
		return
	}
	res, next, err := s.Auth.OIDCCallback(r.Context(), state, q.Get("code"), client(r).IP, r.UserAgent())
	if err != nil {
		s.Log.Warn("OIDC-Anmeldung fehlgeschlagen", "error", err)
		_ = s.Audit.Record(r.Context(), "", "user", client(r).IP, "auth.login_failed", "user", "", "Fehlgeschlagene Anmeldung per OIDC: "+err.Error(), nil, nil)
		loginError(w, r, err.Error(), next)
		return
	}
	s.setSessionCookie(w, r, res.Token, res.Expires)
	if p, _, _, err := s.Auth.Session(r.Context(), res.Token); err == nil {
		_ = s.Audit.Record(r.Context(), p.Username, "user", client(r).IP, "auth.login", "user", strconv.FormatInt(p.UserID, 10),
			"Anmeldung per OIDC", nil, nil)
	}
	http.Redirect(w, r, localPath(next), http.StatusFound)
}

func (s *Server) handleExternalAuth(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.Auth.ExternalConfig(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, externalAuthResponse{ExternalAuth: cfg, RedirectURL: oidcRedirectURI(r)})
}

func (s *Server) handleSaveOIDC(w http.ResponseWriter, r *http.Request) {
	var req oidcSaveRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.Auth.SaveOIDC(r.Context(), req.Config, req.ClientSecret)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	logged := req.Config
	logged.CA = ""
	s.record(r, "auth.oidc", "system", "oidc", "OIDC-Anmeldung geändert", nil, logged)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleSaveLDAP(w http.ResponseWriter, r *http.Request) {
	var req ldapSaveRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	out, err := s.Auth.SaveLDAP(r.Context(), req.Config, req.BindPassword)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	logged := req.Config
	logged.CA = ""
	s.record(r, "auth.ldap", "system", "ldap", "LDAP-Anmeldung geändert", nil, logged)
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleTestOIDC(w http.ResponseWriter, r *http.Request) {
	var req oidcTestRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Auth.TestOIDC(r.Context(), req.Config)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleTestLDAP(w http.ResponseWriter, r *http.Request) {
	var req ldapTestRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Auth.TestLDAP(r.Context(), req.Config, req.BindPassword, req.Username, req.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}
