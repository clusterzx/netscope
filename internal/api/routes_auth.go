package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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

// loginResponse is either the signed-in user or the request for a second factor.
type loginResponse struct {
	User      *auth.User      `json:"user,omitempty"`
	Principal *auth.Principal `json:"principal,omitempty"`
	MFA       *mfaChallenge   `json:"mfa,omitempty"`
}

type mfaChallenge struct {
	Challenge string   `json:"challenge"`
	Methods   []string `json:"methods"` // totp | passkey | recovery
}

type codeRequest struct {
	Challenge string `json:"challenge"`
	Code      string `json:"code"`
}

type challengeRequest struct {
	Challenge string `json:"challenge"`
}

type passkeyLoginRequest struct {
	Challenge  string          `json:"challenge"`
	Credential json.RawMessage `json:"credential"`
}

type passwordRequest struct {
	Current string `json:"current"`
	New     string `json:"new"`
}

type passwordConfirm struct {
	Password string `json:"password"`
}

type totpSetupRequest struct {
	// Password is needed when an active TOTP would be replaced.
	Password string `json:"password,omitempty"`
}

type totpConfirmRequest struct {
	Code string `json:"code"`
}

type recoveryCodesResponse struct {
	// RecoveryCodes are shown once (empty when the user already has unused codes).
	RecoveryCodes []string `json:"recoveryCodes"`
}

type mfaResponse struct {
	auth.MFAStatus
	// PasskeysAvailable: this address allows passkeys (HTTPS with a host name).
	PasskeysAvailable bool   `json:"passkeysAvailable"`
	RPID              string `json:"rpId,omitempty"`
}

type passkeyRequest struct {
	Name       string          `json:"name"`
	Credential json.RawMessage `json:"credential"`
}

type passkeyCreated struct {
	Passkey       *auth.Passkey `json:"passkey"`
	RecoveryCodes []string      `json:"recoveryCodes"`
}

type renameRequest struct {
	Name string `json:"name"`
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

// webauthnOptions are the options for navigator.credentials (base64url encoded fields).
type webauthnOptions map[string]any

func (s *Server) registerAuth() {
	s.add(&route{Method: "POST", Path: "/api/v1/auth/login", Tag: "Auth", Summary: "Anmelden (setzt Session-Cookie oder verlangt den zweiten Faktor)",
		Scope: scopePublic, Body: loginRequest{}, Resp: loginResponse{}, handler: s.handleLogin})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/login/totp", Tag: "Auth", Summary: "Anmeldung mit TOTP-Code abschließen", Scope: scopePublic,
		Body: codeRequest{}, Resp: loginResponse{}, handler: s.handleLoginTOTP})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/login/recovery", Tag: "Auth", Summary: "Anmeldung mit Wiederherstellungscode abschließen",
		Scope: scopePublic, Body: codeRequest{}, Resp: loginResponse{}, handler: s.handleLoginRecovery})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/login/passkey/options", Tag: "Auth", Summary: "Passkey-Anmeldung beginnen (Optionen für navigator.credentials.get)",
		Scope: scopePublic, Body: challengeRequest{}, Resp: webauthnOptions{}, handler: s.handlePasskeyLoginOptions})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/login/passkey", Tag: "Auth", Summary: "Anmeldung mit Passkey abschließen", Scope: scopePublic,
		Body: passkeyLoginRequest{}, Resp: loginResponse{}, handler: s.handlePasskeyLogin})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/logout", Tag: "Auth", Summary: "Abmelden", Scope: scopePublic, Resp: okResponse{},
		handler: s.handleLogout})
	s.add(&route{Method: "GET", Path: "/api/v1/auth/me", Tag: "Auth", Summary: "Aktueller Benutzer mit Rolle und Berechtigungen", Scope: scopeRead,
		Setup: true, Resp: meResponse{}, handler: s.handleMe})
	s.add(&route{Method: "PUT", Path: "/api/v1/auth/password", Tag: "Auth", Summary: "Eigenes Passwort ändern (beendet andere Sessions)", Scope: scopeWrite,
		Setup: true, Body: passwordRequest{}, Resp: okResponse{}, handler: s.handlePassword})

	s.add(&route{Method: "GET", Path: "/api/v1/auth/2fa", Tag: "Auth", Summary: "Eigene Zwei-Faktor-Anmeldung: TOTP, Passkeys, Wiederherstellungscodes",
		Scope: scopeRead, Setup: true, Resp: mfaResponse{}, handler: s.handleMFA})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/2fa/totp", Tag: "Auth", Summary: "TOTP einrichten (Geheimnis und QR-Code; aktiv erst nach Bestätigung)",
		Scope: scopeWrite, Setup: true, Body: totpSetupRequest{}, Resp: auth.TOTPSetup{}, handler: s.handleTOTPSetup})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/2fa/totp/confirm", Tag: "Auth", Summary: "TOTP mit dem ersten Code aktivieren",
		Scope: scopeWrite, Setup: true, Body: totpConfirmRequest{}, Resp: recoveryCodesResponse{}, handler: s.handleTOTPConfirm})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/2fa/totp/disable", Tag: "Auth", Summary: "TOTP abschalten (Passwort erforderlich)",
		Scope: scopeWrite, Setup: true, Body: passwordConfirm{}, Resp: okResponse{}, handler: s.handleTOTPDisable})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/2fa/recovery", Tag: "Auth", Summary: "Neue Wiederherstellungscodes erzeugen (alte werden ungültig)",
		Scope: scopeWrite, Setup: true, Body: passwordConfirm{}, Resp: recoveryCodesResponse{}, handler: s.handleRecoveryCodes})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/passkeys/options", Tag: "Auth", Summary: "Passkey-Registrierung beginnen (Optionen für navigator.credentials.create)",
		Scope: scopeWrite, Setup: true, Resp: webauthnOptions{}, handler: s.handlePasskeyOptions})
	s.add(&route{Method: "POST", Path: "/api/v1/auth/passkeys", Tag: "Auth", Summary: "Passkey registrieren", Scope: scopeWrite, Setup: true,
		Body: passkeyRequest{}, Resp: passkeyCreated{}, Status: http.StatusCreated, handler: s.handlePasskeyCreate})
	s.add(&route{Method: "PATCH", Path: "/api/v1/auth/passkeys/{id}", Tag: "Auth", Summary: "Passkey umbenennen", Scope: scopeWrite, Setup: true,
		Params: idParam, Body: renameRequest{}, Resp: okResponse{}, handler: s.handlePasskeyRename})
	s.add(&route{Method: "DELETE", Path: "/api/v1/auth/passkeys/{id}", Tag: "Auth", Summary: "Passkey entfernen", Scope: scopeWrite, Setup: true,
		Params: idParam, Resp: okResponse{}, handler: s.handlePasskeyDelete})

	s.add(&route{Method: "GET", Path: "/api/v1/tokens", Tag: "Auth", Summary: "API-Tokens auflisten (eigene; mit Benutzerverwaltung alle)", Scope: scopeRead,
		Resp: []auth.Token{}, handler: s.handleTokens})
	s.add(&route{Method: "POST", Path: "/api/v1/tokens", Tag: "Auth", Summary: "API-Token erstellen (Klartext nur in dieser Antwort)", Scope: scopeWrite,
		Perm: auth.PermTokensCreate, Body: tokenRequest{}, Resp: tokenCreated{}, Status: http.StatusCreated, handler: s.handleCreateToken})
	s.add(&route{Method: "DELETE", Path: "/api/v1/tokens/{id}", Tag: "Auth", Summary: "API-Token widerrufen (eigene; mit Benutzerverwaltung alle)",
		Scope: scopeWrite, Resp: okResponse{}, handler: s.handleDeleteToken})
}

// relyingParty returns the passkey relying party of a request: the host name the browser
// uses. Browsers allow passkeys only in a secure context (HTTPS, or localhost) and never
// for an IP address; the ID stays empty then.
func relyingParty(r *http.Request) auth.RP {
	c := client(r)
	hostname := c.Host
	if h, _, err := net.SplitHostPort(c.Host); err == nil {
		hostname = h
	}
	hostname = strings.ToLower(strings.Trim(hostname, "[]"))
	if hostname == "" || net.ParseIP(hostname) != nil {
		return auth.RP{}
	}
	local := hostname == "localhost" || strings.HasSuffix(hostname, ".localhost")
	if c.Scheme != "https" && !local {
		return auth.RP{}
	}
	origin := c.Scheme + "://" + c.Host
	// the origin the browser reports has to be the address the request went to
	if o := r.Header.Get("Origin"); o != "" && !strings.EqualFold(o, origin) {
		return auth.RP{}
	}
	return auth.RP{ID: hostname, Origin: origin}
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Auth.Login(r.Context(), req.Username, req.Password, relyingParty(r).ID, client(r).IP, r.UserAgent())
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidCredentials):
			_ = s.Audit.Record(r.Context(), req.Username, "user", client(r).IP, "auth.login_failed", "user", "", "Fehlgeschlagene Anmeldung", nil, nil)
		case errors.Is(err, auth.ErrAccountDisabled):
			_ = s.Audit.Record(r.Context(), req.Username, "user", client(r).IP, "auth.login_failed", "user", "", "Anmeldung mit deaktiviertem Konto", nil, nil)
		}
		s.fail(w, r, err)
		return
	}
	if res.Challenge != "" {
		writeJSON(w, http.StatusOK, loginResponse{MFA: &mfaChallenge{Challenge: res.Challenge, Methods: res.Methods}})
		return
	}
	s.finishLogin(w, r, res, "")
}

// finishLogin sets the session cookie and answers with the signed-in user.
func (s *Server) finishLogin(w http.ResponseWriter, r *http.Request, res *auth.LoginResult, method string) {
	s.setSessionCookie(w, r, res.Token, res.Expires)
	p, _, _, err := s.Auth.Session(r.Context(), res.Token)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u, _ := s.Auth.User(r.Context(), p.UserID)
	summary := "Anmeldung"
	if method != "" {
		summary += " mit " + method
	}
	_ = s.Audit.Record(r.Context(), p.Username, "user", client(r).IP, "auth.login", "user", strconv.FormatInt(p.UserID, 10), summary, nil, nil)
	writeJSON(w, http.StatusOK, loginResponse{User: u, Principal: p})
}

func (s *Server) handleLoginTOTP(w http.ResponseWriter, r *http.Request) {
	var req codeRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Auth.LoginTOTP(r.Context(), req.Challenge, req.Code, client(r).IP, r.UserAgent())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.finishLogin(w, r, res, "TOTP")
}

func (s *Server) handleLoginRecovery(w http.ResponseWriter, r *http.Request) {
	var req codeRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Auth.LoginRecovery(r.Context(), req.Challenge, req.Code, client(r).IP, r.UserAgent())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.finishLogin(w, r, res, "Wiederherstellungscode")
}

func (s *Server) handlePasskeyLoginOptions(w http.ResponseWriter, r *http.Request) {
	var req challengeRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	opts, err := s.Auth.BeginPasskeyLogin(r.Context(), req.Challenge, relyingParty(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) handlePasskeyLogin(w http.ResponseWriter, r *http.Request) {
	var req passkeyLoginRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Auth.FinishPasskeyLogin(r.Context(), req.Challenge, relyingParty(r), req.Credential, client(r).IP, r.UserAgent())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.finishLogin(w, r, res, "Passkey")
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

// sessionOnly refuses changes to the own account through API tokens.
func sessionOnly(w http.ResponseWriter, p *auth.Principal) bool {
	if p.Kind != "session" {
		writeError(w, http.StatusForbidden, "forbidden", "Das geht nur in der Oberfläche, nicht mit einem API-Token", nil)
		return false
	}
	return true
}

func (s *Server) handlePassword(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !sessionOnly(w, p) {
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
	if p.Admin {
		_ = os.Remove(filepath.Join(s.Config.DataDir, "admin-initial-password.txt"))
	}
	s.record(r, "auth.password", "user", strconv.FormatInt(p.UserID, 10), "Passwort geändert", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

// ---------------------------------------------------------------- second factor

func (s *Server) handleMFA(w http.ResponseWriter, r *http.Request) {
	st, err := s.Auth.MFA(r.Context(), principal(r).UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rp := relyingParty(r)
	writeJSON(w, http.StatusOK, mfaResponse{MFAStatus: *st, PasskeysAvailable: rp.ID != "", RPID: rp.ID})
}

func (s *Server) handleTOTPSetup(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !sessionOnly(w, p) {
		return
	}
	var req totpSetupRequest
	if r.ContentLength != 0 {
		if err := decode(r, &req); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	st, err := s.Auth.MFA(r.Context(), p.UserID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if st.TOTP {
		// setting up again replaces the active secret
		if err := s.Auth.VerifyPassword(r.Context(), p.UserID, req.Password); err != nil {
			s.fail(w, r, plugin.FieldErr("password", "Passwort ist falsch"))
			return
		}
	}
	host := client(r).Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	setup, err := s.Auth.BeginTOTP(r.Context(), p.UserID, host)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, setup)
}

func (s *Server) handleTOTPConfirm(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !sessionOnly(w, p) {
		return
	}
	var req totpConfirmRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	codes, err := s.Auth.ConfirmTOTP(r.Context(), p.UserID, req.Code)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "auth.totp_enabled", "user", strconv.FormatInt(p.UserID, 10), "TOTP eingerichtet", nil, nil)
	writeJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: nonNil(codes)})
}

func (s *Server) handleTOTPDisable(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !sessionOnly(w, p) {
		return
	}
	var req passwordConfirm
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Auth.DisableTOTP(r.Context(), p.UserID, req.Password); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "auth.totp_disabled", "user", strconv.FormatInt(p.UserID, 10), "TOTP abgeschaltet", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleRecoveryCodes(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !sessionOnly(w, p) {
		return
	}
	var req passwordConfirm
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	codes, err := s.Auth.RegenerateRecoveryCodes(r.Context(), p.UserID, req.Password)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "auth.recovery_codes", "user", strconv.FormatInt(p.UserID, 10), "Neue Wiederherstellungscodes erzeugt", nil, nil)
	writeJSON(w, http.StatusOK, recoveryCodesResponse{RecoveryCodes: codes})
}

func (s *Server) handlePasskeyOptions(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !sessionOnly(w, p) {
		return
	}
	opts, err := s.Auth.BeginPasskeyRegistration(r.Context(), p.UserID, relyingParty(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, opts)
}

func (s *Server) handlePasskeyCreate(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !sessionOnly(w, p) {
		return
	}
	var req passkeyRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	pk, codes, err := s.Auth.FinishPasskeyRegistration(r.Context(), p.UserID, relyingParty(r), req.Name, req.Credential)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "auth.passkey_added", "user", strconv.FormatInt(p.UserID, 10), "Passkey „"+pk.Name+"“ für "+pk.RPID+" registriert", nil, nil)
	writeJSON(w, http.StatusCreated, passkeyCreated{Passkey: pk, RecoveryCodes: nonNil(codes)})
}

func (s *Server) handlePasskeyRename(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var req renameRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Auth.RenamePasskey(r.Context(), p.UserID, id, req.Name); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handlePasskeyDelete(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !sessionOnly(w, p) {
		return
	}
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Auth.DeletePasskey(r.Context(), p.UserID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "auth.passkey_removed", "user", strconv.FormatInt(p.UserID, 10), "Passkey entfernt", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func nonNil(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

// ---------------------------------------------------------------- API tokens

// tokenOwner limits token operations to the caller's own tokens unless it manages users.
func tokenOwner(p *auth.Principal) int64 {
	if p.Has(auth.PermUsersManage) {
		return 0
	}
	return p.UserID
}

func (s *Server) handleTokens(w http.ResponseWriter, r *http.Request) {
	list, err := s.Auth.ListTokens(r.Context(), tokenOwner(principal(r)))
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
	if err := s.Auth.DeleteToken(r.Context(), id, tokenOwner(principal(r))); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "token.delete", "token", strconv.FormatInt(id, 10), "API-Token widerrufen", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}
