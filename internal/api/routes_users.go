package api

import (
	"net/http"
	"strconv"

	"netscope/internal/auth"
)

// idParam is the numeric id in the path.
var idParam = []param{{Name: "id", In: "path", Type: "integer", Required: true}}

type userCreated struct {
	User *auth.User `json:"user"`
	// Password is the generated start password (shown once; empty if one was given).
	Password string `json:"password,omitempty"`
}

type passwordResponse struct {
	Password string `json:"password"`
}

func (s *Server) registerUsers() {
	s.add(&route{Method: "GET", Path: "/api/v1/permissions", Tag: "Benutzer", Summary: "Katalog der Berechtigungen für Rollen", Scope: scopeRead,
		Resp: []auth.Permission{}, handler: s.handlePermissions})
	s.add(&route{Method: "GET", Path: "/api/v1/users", Tag: "Benutzer", Summary: "Benutzer auflisten", Scope: scopeRead,
		Perm: auth.PermUsersManage, Resp: []auth.User{}, handler: s.handleUsers})
	s.add(&route{Method: "POST", Path: "/api/v1/users", Tag: "Benutzer", Summary: "Benutzer anlegen (ohne Passwort wird eins erzeugt; Änderung beim ersten Login)",
		Scope: scopeWrite, Perm: auth.PermUsersManage, Body: auth.UserInput{}, Resp: userCreated{}, Status: http.StatusCreated, handler: s.handleCreateUser})
	s.add(&route{Method: "GET", Path: "/api/v1/users/{id}", Tag: "Benutzer", Summary: "Benutzer", Scope: scopeRead, Perm: auth.PermUsersManage,
		Params: idParam, Resp: auth.User{}, handler: s.handleUser})
	s.add(&route{Method: "PUT", Path: "/api/v1/users/{id}", Tag: "Benutzer", Summary: "Benutzer ändern (Name, Rolle, deaktiviert)", Scope: scopeWrite,
		Perm: auth.PermUsersManage, Params: idParam, Body: auth.UserInput{}, Resp: auth.User{}, handler: s.handleUpdateUser})
	s.add(&route{Method: "DELETE", Path: "/api/v1/users/{id}", Tag: "Benutzer", Summary: "Benutzer löschen (mit Tokens und Sessions)", Scope: scopeWrite,
		Perm: auth.PermUsersManage, Params: idParam, Resp: okResponse{}, handler: s.handleDeleteUser})
	s.add(&route{Method: "POST", Path: "/api/v1/users/{id}/password", Tag: "Benutzer", Summary: "Neues Start-Passwort erzeugen (Änderung beim nächsten Login)",
		Scope: scopeWrite, Perm: auth.PermUsersManage, Params: idParam, Resp: passwordResponse{}, handler: s.handleUserPassword})
	s.add(&route{Method: "POST", Path: "/api/v1/users/{id}/2fa/reset", Tag: "Benutzer", Summary: "Zweiten Faktor zurücksetzen (TOTP, Passkeys, Codes)",
		Scope: scopeWrite, Perm: auth.PermUsersManage, Params: idParam, Resp: okResponse{}, handler: s.handleUserResetMFA})

	s.add(&route{Method: "GET", Path: "/api/v1/roles", Tag: "Benutzer", Summary: "Rollen auflisten", Scope: scopeRead, Perm: auth.PermUsersManage,
		Resp: []auth.Role{}, handler: s.handleRoles})
	s.add(&route{Method: "POST", Path: "/api/v1/roles", Tag: "Benutzer", Summary: "Rolle anlegen", Scope: scopeWrite, Perm: auth.PermUsersManage,
		Body: auth.RoleInput{}, Resp: auth.Role{}, Status: http.StatusCreated, handler: s.handleCreateRole})
	s.add(&route{Method: "PUT", Path: "/api/v1/roles/{id}", Tag: "Benutzer", Summary: "Rolle ändern (Administrator: nur Beschreibung und 2FA-Pflicht)",
		Scope: scopeWrite, Perm: auth.PermUsersManage, Params: idParam, Body: auth.RoleInput{}, Resp: auth.Role{}, handler: s.handleUpdateRole})
	s.add(&route{Method: "DELETE", Path: "/api/v1/roles/{id}", Tag: "Benutzer", Summary: "Rolle löschen (nur ohne Benutzer)", Scope: scopeWrite,
		Perm: auth.PermUsersManage, Params: idParam, Resp: okResponse{}, handler: s.handleDeleteRole})
}

func (s *Server) handlePermissions(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, auth.Permissions)
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.Auth.Users(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.Auth.User(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var in auth.UserInput
	if err := decodeLenient(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	u, pw, err := s.Auth.CreateUser(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	in.Password = ""
	s.record(r, "user.create", "user", strconv.FormatInt(u.ID, 10), "Benutzer „"+u.Username+"“ ("+u.RoleName+") angelegt", nil, in)
	writeJSON(w, http.StatusCreated, userCreated{User: u, Password: pw})
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in auth.UserInput
	if err := decodeLenient(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	before, err := s.Auth.User(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.Auth.UpdateUser(r.Context(), principal(r).UserID, id, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "user.update", "user", strconv.FormatInt(id, 10), "Benutzer „"+u.Username+"“ geändert", userAudit(before), userAudit(u))
	writeJSON(w, http.StatusOK, u)
}

// userAudit is the part of a user worth diffing in the audit log.
func userAudit(u *auth.User) map[string]any {
	return map[string]any{"username": u.Username, "displayName": u.DisplayName, "email": u.Email, "role": u.RoleName, "disabled": u.Disabled}
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u, err := s.Auth.User(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Auth.DeleteUser(r.Context(), principal(r).UserID, id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "user.delete", "user", strconv.FormatInt(id, 10), "Benutzer „"+u.Username+"“ gelöscht", userAudit(u), nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleUserPassword(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	pw, err := s.Auth.SetTemporaryPassword(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "user.password_reset", "user", strconv.FormatInt(id, 10), "Neues Start-Passwort vergeben", nil, nil)
	writeJSON(w, http.StatusOK, passwordResponse{Password: pw})
}

func (s *Server) handleUserResetMFA(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Auth.ResetMFA(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "user.mfa_reset", "user", strconv.FormatInt(id, 10), "Zweiten Faktor zurückgesetzt", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleRoles(w http.ResponseWriter, r *http.Request) {
	list, err := s.Auth.Roles(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleCreateRole(w http.ResponseWriter, r *http.Request) {
	var in auth.RoleInput
	if err := decodeLenient(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	role, err := s.Auth.CreateRole(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "role.create", "role", strconv.FormatInt(role.ID, 10), "Rolle „"+role.Name+"“ angelegt", nil, in)
	writeJSON(w, http.StatusCreated, role)
}

func (s *Server) handleUpdateRole(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in auth.RoleInput
	if err := decodeLenient(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	before, err := s.Auth.Role(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	role, err := s.Auth.UpdateRole(r.Context(), id, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "role.update", "role", strconv.FormatInt(id, 10), "Rolle „"+role.Name+"“ geändert", roleAudit(before), roleAudit(role))
	writeJSON(w, http.StatusOK, role)
}

func roleAudit(r *auth.Role) map[string]any {
	return map[string]any{"name": r.Name, "description": r.Description, "permissions": r.Permissions, "require2fa": r.Require2FA}
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	role, err := s.Auth.Role(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Auth.DeleteRole(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "role.delete", "role", strconv.FormatInt(id, 10), "Rolle „"+role.Name+"“ gelöscht", roleAudit(role), nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}
