package api

import (
	"context"
	"fmt"
	"net/http"
	"net/netip"
	"slices"
	"strconv"
	"strings"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
	"netscope/internal/pluginhost"
	"netscope/internal/rules"
	"netscope/internal/vault"
)

type ruleTestRequest struct {
	Rule  *rules.Rule    `json:"rule,omitempty"`
	Event rules.SimInput `json:"event"`
}

type ruleOrderRequest struct {
	IDs []int64 `json:"ids"` // rule ids in evaluation order; unlisted rules follow in their current order
}

type notificationList struct {
	Total int                      `json:"total"`
	Items []rules.NotificationView `json:"items"`
}

type credentialView struct {
	vault.CredentialMeta
	UsedBy []credentialUse `json:"usedBy"`
}

// credentialUse is a plugin whose settings reference a credential.
type credentialUse struct {
	ID   string `json:"id"`   // plugin id (link target /plugins/<id>)
	Name string `json:"name"` // plugin name
}

// deviceCredential is a credential that applies to a device.
type deviceCredential struct {
	plugin.CredentialMatch
	// UsedBy lists the plugins that use the credential for this device: explicitly
	// selected or picked automatically (empty credential selection).
	UsedBy []credentialUse `json:"usedBy"`
}

func (s *Server) registerRules() {
	s.add(&route{Method: "GET", Path: "/api/v1/rules", Tag: "Regeln", Summary: "Benachrichtigungsregeln", Scope: scopeRead,
		Resp: []rules.Rule{}, handler: s.handleRules})
	s.add(&route{Method: "POST", Path: "/api/v1/rules", Tag: "Regeln", Summary: "Regel anlegen", Scope: scopeWrite,
		Body: rules.Rule{}, Resp: rules.Rule{}, Status: http.StatusCreated, handler: s.handleSaveRule})
	s.add(&route{Method: "PUT", Path: "/api/v1/rules/order", Tag: "Regeln", Summary: "Auswertungsreihenfolge der Regeln setzen (atomar)",
		Scope: scopeWrite, Body: ruleOrderRequest{}, Resp: []rules.Rule{}, handler: s.handleRuleOrder})
	s.add(&route{Method: "GET", Path: "/api/v1/rules/{id}", Tag: "Regeln", Summary: "Eine Regel", Scope: scopeRead,
		Resp: rules.Rule{}, handler: s.handleRule})
	s.add(&route{Method: "PUT", Path: "/api/v1/rules/{id}", Tag: "Regeln", Summary: "Regel ändern", Scope: scopeWrite,
		Body: rules.Rule{}, Resp: rules.Rule{}, handler: s.handleSaveRule})
	s.add(&route{Method: "DELETE", Path: "/api/v1/rules/{id}", Tag: "Regeln", Summary: "Regel löschen", Scope: scopeWrite,
		Resp: okResponse{}, handler: s.handleDeleteRule})
	s.add(&route{Method: "POST", Path: "/api/v1/rules/test", Tag: "Regeln",
		Summary: "Regel mit simuliertem Event testen (gespeicherte Regel per ID-Route oder ungespeicherte im Body)", Scope: scopeWrite,
		Body: ruleTestRequest{}, Resp: rules.SimResult{}, handler: s.handleTestRule})
	s.add(&route{Method: "POST", Path: "/api/v1/rules/{id}/test", Tag: "Regeln", Summary: "Gespeicherte Regel mit simuliertem Event testen",
		Scope: scopeWrite, Body: rules.SimInput{}, Resp: rules.SimResult{}, handler: s.handleTestSavedRule})
	s.add(&route{Method: "GET", Path: "/api/v1/notifications", Tag: "Regeln", Summary: "Benachrichtigungsverlauf", Scope: scopeRead,
		Params: []param{{Name: "status"}, {Name: "publisher"}, {Name: "event", Type: "integer"}, {Name: "rule", Type: "integer"}, {Name: "limit", Type: "integer"},
			{Name: "offset", Type: "integer"}}, Resp: notificationList{}, handler: s.handleNotifications})
	s.add(&route{Method: "GET", Path: "/api/v1/publishers", Tag: "Regeln", Summary: "Verfügbare Publisher", Scope: scopeRead,
		Resp: []pluginhost.PublisherInfo{}, handler: s.handlePublishers})
}

func (s *Server) registerCredentials() {
	s.add(&route{Method: "GET", Path: "/api/v1/credentials", Tag: "Credentials", Summary: "Vault-Einträge (ohne Secrets)", Scope: scopeRead,
		Resp: []credentialView{}, handler: s.handleCredentials})
	s.add(&route{Method: "GET", Path: "/api/v1/credentials/types", Tag: "Credentials", Summary: "Credential-Typen mit Formular-Schema",
		Scope: scopeRead, Resp: []plugin.CredentialType{}, handler: s.handleCredentialTypes})
	s.add(&route{Method: "POST", Path: "/api/v1/credentials", Tag: "Credentials", Summary: "Credential anlegen (verschlüsselt gespeichert)",
		Scope: scopeWrite, Body: vault.CredentialInput{}, Resp: credentialView{}, Status: http.StatusCreated, handler: s.handleSaveCredential})
	s.add(&route{Method: "GET", Path: "/api/v1/credentials/{id}", Tag: "Credentials", Summary: "Ein Credential (ohne Secrets)", Scope: scopeRead,
		Resp: credentialView{}, handler: s.handleCredential})
	s.add(&route{Method: "PUT", Path: "/api/v1/credentials/{id}", Tag: "Credentials",
		Summary: "Credential ändern (Secret-Felder mit ******** bleiben unverändert)", Scope: scopeWrite,
		Body: vault.CredentialInput{}, Resp: credentialView{}, handler: s.handleSaveCredential})
	s.add(&route{Method: "DELETE", Path: "/api/v1/credentials/{id}", Tag: "Credentials", Summary: "Credential löschen (nur wenn unbenutzt)",
		Scope: scopeWrite, Resp: okResponse{}, handler: s.handleDeleteCredential})
}

func (s *Server) handleRules(w http.ResponseWriter, r *http.Request) {
	list, err := s.Rules.List(r.Context())
	s.respond(w, r, list, err)
}

func (s *Server) handleRule(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rule, err := s.Rules.Get(r.Context(), id)
	s.respond(w, r, rule, err)
}

func (s *Server) handleSaveRule(w http.ResponseWriter, r *http.Request) {
	id, err := optionalID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var rule rules.Rule
	if err := decodeLenient(r, &rule); err != nil {
		s.fail(w, r, err)
		return
	}
	var before *rules.Rule
	if id > 0 {
		if before, err = s.Rules.Get(r.Context(), id); err != nil {
			s.fail(w, r, err)
			return
		}
		rule.Builtin = before.Builtin
	} else {
		rule.Builtin = ""
	}
	rule.ID = id
	if err := s.Rules.Save(r.Context(), &rule); err != nil {
		s.fail(w, r, err)
		return
	}
	action, status := "rule.update", http.StatusOK
	if id == 0 {
		action, status = "rule.create", http.StatusCreated
	}
	s.record(r, action, "rule", strconv.FormatInt(rule.ID, 10), "Regel „"+rule.Name+"“", before, rule)
	writeJSON(w, status, rule)
}

func (s *Server) handleRuleOrder(w http.ResponseWriter, r *http.Request) {
	var req ruleOrderRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	before, err := s.Rules.List(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	list, err := s.Rules.Reorder(r.Context(), req.IDs)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	order := func(l []*rules.Rule) []int64 {
		ids := make([]int64, len(l))
		for i, x := range l {
			ids[i] = x.ID
		}
		return ids
	}
	s.record(r, "rule.reorder", "rule", "", "Regelreihenfolge geändert", order(before), order(list))
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	before, err := s.Rules.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Rules.Delete(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "rule.delete", "rule", strconv.FormatInt(id, 10), "Regel „"+before.Name+"“ gelöscht", before, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleTestRule(w http.ResponseWriter, r *http.Request) {
	var req ruleTestRequest
	if err := decodeLenient(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	if req.Rule == nil {
		s.fail(w, r, fmt.Errorf("Regel fehlt"))
		return
	}
	res, err := s.Rules.Simulate(r.Context(), req.Rule, req.Event)
	if err == nil && req.Event.Deliver {
		s.record(r, "rule.test", "rule", strconv.FormatInt(req.Rule.ID, 10), "Regeltest mit Versand", nil, req.Event)
	}
	s.respond(w, r, res, err)
}

func (s *Server) handleTestSavedRule(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rule, err := s.Rules.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in rules.SimInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.Rules.Simulate(r.Context(), rule, in)
	if err == nil && in.Deliver {
		s.record(r, "rule.test", "rule", strconv.FormatInt(id, 10), "Regeltest mit Versand", nil, in)
	}
	s.respond(w, r, res, err)
}

func (s *Server) handleNotifications(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	list, total, err := s.Rules.Notifications(r.Context(), rules.NotificationFilter{Status: q.Get("status"), Publisher: q.Get("publisher"),
		EventID: qInt64(r, "event"), RuleID: qInt64(r, "rule"), Limit: qInt(r, "limit", 100), Offset: qInt(r, "offset", 0)})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, notificationList{Total: total, Items: list})
}

func (s *Server) handlePublishers(w http.ResponseWriter, r *http.Request) {
	list := s.Host.Publishers()
	if list == nil {
		list = []pluginhost.PublisherInfo{}
	}
	writeJSON(w, http.StatusOK, list)
}

// credentialUsers lists the plugins (among ids) that use a credential of the given type:
// explicitly referenced or picked automatically by a visible multi credential field
// left empty.
func (s *Server) credentialUsers(ids []string, id int64, typ string) []credentialUse {
	out := []credentialUse{}
	for _, pid := range ids {
		p, _ := s.Host.Plugin(pid)
		cfg, _ := s.Host.Config(pid)
		schema := p.Schema()
		st := plugin.NewSettings(cfg.Settings)
		for _, f := range schema.Fields {
			if f.Type != plugin.FieldCredentialRef || (len(f.CredentialTypes) > 0 && !slices.Contains(f.CredentialTypes, typ)) ||
				!schema.Visible(f, cfg.Settings) {
				continue
			}
			sel := st.CredentialIDs(f.Key)
			if slices.Contains(sel, id) || (f.Multi && len(sel) == 0) {
				out = append(out, credentialUse{ID: pid, Name: p.Info().Name})
				break
			}
		}
	}
	return out
}

// pluginsCovering returns the plugins that connect to a device: scanners whose scope
// covers it and importers with the device among their configured hosts.
func (s *Server) pluginsCovering(ctx context.Context, dev *plugin.DeviceInfo) []string {
	subnets, _ := s.Inventory.Subnets(ctx)
	addrs := map[string]bool{}
	for _, ip := range append([]string{dev.PrimaryIP}, dev.IPs...) {
		if a, err := netip.ParseAddr(ip); err == nil {
			addrs[a.Unmap().String()] = true
		}
	}
	inSubnet := func(sc plugin.Scope) bool {
		for _, sn := range subnets {
			if !sc.AllSubnets && len(sc.Subnets) > 0 && !slices.Contains(sc.Subnets, sn.CIDR.String()) {
				continue
			}
			for ip := range addrs {
				if a, err := netip.ParseAddr(ip); err == nil && sn.CIDR.Contains(a) {
					return true
				}
			}
		}
		return false
	}
	isEndpoint := func(p plugin.Plugin, cfg *pluginhost.Config) bool {
		ep, ok := p.(plugin.EndpointProvider)
		if !ok {
			return false
		}
		for _, host := range ep.Endpoints(plugin.NewSettings(cfg.Settings)) {
			if ip, err := netutil.ResolveHost(ctx, host); err == nil && addrs[ip] {
				return true
			}
		}
		return false
	}
	var out []string
	for _, pid := range s.Host.IDs() {
		p, _ := s.Host.Plugin(pid)
		cfg, _ := s.Host.Config(pid)
		mode := p.Info().Targets
		switch {
		case mode == plugin.TargetNone:
			if !isEndpoint(p, cfg) {
				continue
			}
		case cfg.Scope.DeviceRestricted():
			t, err := s.Inventory.ResolveTargets(ctx, cfg.Scope, mode)
			if err != nil || !slices.ContainsFunc(t.Devices, func(d plugin.DeviceInfo) bool { return d.ID == dev.ID }) {
				continue
			}
		case !inSubnet(cfg.Scope):
			continue
		}
		out = append(out, pid)
	}
	return out
}

func (s *Server) handleDeviceCredentials(w http.ResponseWriter, r *http.Request) {
	id, ok := s.deviceID(w, r)
	if !ok {
		return
	}
	dev, err := s.Inventory.Device(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	list, err := s.Host.CredentialProvider().Applicable(r.Context(), plugin.CredentialTarget{DeviceID: id}, nil, nil)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	plugins := s.pluginsCovering(r.Context(), dev)
	out := make([]deviceCredential, 0, len(list))
	for _, m := range list {
		out = append(out, deviceCredential{CredentialMatch: m, UsedBy: s.credentialUsers(plugins, m.ID, m.Type)})
	}
	writeJSON(w, http.StatusOK, out)
}

// credentialUsage lists the plugins whose settings reference a credential.
func (s *Server) credentialUsage(id int64) []credentialUse {
	out := []credentialUse{}
	for _, pid := range s.Host.IDs() {
		p, _ := s.Host.Plugin(pid)
		cfg, _ := s.Host.Config(pid)
		st := plugin.NewSettings(cfg.Settings)
		for _, f := range p.Schema().Fields {
			if f.Type != plugin.FieldCredentialRef {
				continue
			}
			for _, cid := range st.CredentialIDs(f.Key) {
				if cid == id && (len(out) == 0 || out[len(out)-1].ID != pid) {
					out = append(out, credentialUse{ID: pid, Name: p.Info().Name})
				}
			}
		}
	}
	return out
}

func (s *Server) handleCredentials(w http.ResponseWriter, r *http.Request) {
	list, err := s.Vault.ListCredentials(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	out := make([]credentialView, 0, len(list))
	for _, c := range list {
		out = append(out, credentialView{CredentialMeta: c, UsedBy: s.credentialUsage(c.ID)})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleCredentialTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, plugin.CredentialTypes())
}

func (s *Server) handleCredential(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	m, err := s.Vault.Meta(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, credentialView{CredentialMeta: *m, UsedBy: s.credentialUsage(id)})
}

func (s *Server) handleSaveCredential(w http.ResponseWriter, r *http.Request) {
	id, err := optionalID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in vault.CredentialInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if in.Scope != nil {
		if err := s.Host.ValidateScope(r.Context(), in.Scope); err != nil {
			s.fail(w, r, err)
			return
		}
	}
	var before *vault.CredentialMeta
	if id == 0 {
		id, err = s.Vault.CreateCredential(r.Context(), in)
	} else {
		before, _ = s.Vault.Meta(r.Context(), id)
		err = s.Vault.UpdateCredential(r.Context(), id, in)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	m, err := s.Vault.Meta(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	action, status := "credential.update", http.StatusOK
	if before == nil {
		action, status = "credential.create", http.StatusCreated
	}
	// the audit log only sees public fields and the names of set secrets
	s.record(r, action, "credential", strconv.FormatInt(id, 10), "Credential „"+m.Name+"“ ("+m.Type+")", before, m)
	writeJSON(w, status, credentialView{CredentialMeta: *m, UsedBy: s.credentialUsage(id)})
}

func (s *Server) handleDeleteCredential(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if used := s.credentialUsage(id); len(used) > 0 {
		names := make([]string, len(used))
		for i, u := range used {
			names[i] = u.Name
		}
		writeError(w, http.StatusConflict, "in_use", "Credential wird noch verwendet von: "+strings.Join(names, ", "), nil)
		return
	}
	m, err := s.Vault.Meta(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Vault.DeleteCredential(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "credential.delete", "credential", strconv.FormatInt(id, 10), "Credential „"+m.Name+"“ gelöscht", m, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}
