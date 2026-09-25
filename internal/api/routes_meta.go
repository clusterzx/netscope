package api

import (
	"net/http"
	"strconv"

	"netscope/internal/auth"
	"netscope/internal/inventory"
)

type groupMembers struct {
	IDs []int64 `json:"ids"`
}

func (s *Server) registerMeta() {
	s.add(&route{Method: "GET", Path: "/api/v1/subnets", Tag: "Subnetze", Summary: "Subnetze (mit Tunnel-Zustand)", Scope: scopeRead,
		Resp: []subnetView{}, handler: s.handleSubnets})
	s.add(&route{Method: "POST", Path: "/api/v1/subnets", Tag: "Subnetze", Summary: "Subnetz anlegen", Scope: scopeWrite,
		Body: inventory.Subnet{}, Resp: subnetView{}, Status: http.StatusCreated, Perm: auth.PermNetworkManage, handler: s.handleSaveSubnet})
	s.add(&route{Method: "PUT", Path: "/api/v1/subnets/{id}", Tag: "Subnetze", Summary: "Subnetz ändern", Scope: scopeWrite,
		Body: inventory.Subnet{}, Resp: subnetView{}, Perm: auth.PermNetworkManage, handler: s.handleSaveSubnet})
	s.add(&route{Method: "DELETE", Path: "/api/v1/subnets/{id}", Tag: "Subnetze", Summary: "Subnetz löschen", Scope: scopeWrite,
		Resp: okResponse{}, Perm: auth.PermNetworkManage, handler: s.handleDeleteSubnet})

	s.add(&route{Method: "GET", Path: "/api/v1/groups", Tag: "Gruppen", Summary: "Gerätegruppen", Scope: scopeRead,
		Resp: []inventory.Group{}, handler: s.handleGroups})
	s.add(&route{Method: "POST", Path: "/api/v1/groups", Tag: "Gruppen", Summary: "Gruppe anlegen (manuell oder regelbasiert per Filter)",
		Scope: scopeWrite, Body: inventory.Group{}, Resp: inventory.Group{}, Status: http.StatusCreated, Perm: auth.PermInventoryConfig, handler: s.handleSaveGroup})
	s.add(&route{Method: "PUT", Path: "/api/v1/groups/{id}", Tag: "Gruppen", Summary: "Gruppe ändern", Scope: scopeWrite,
		Body: inventory.Group{}, Resp: inventory.Group{}, Perm: auth.PermInventoryConfig, handler: s.handleSaveGroup})
	s.add(&route{Method: "DELETE", Path: "/api/v1/groups/{id}", Tag: "Gruppen", Summary: "Gruppe löschen", Scope: scopeWrite,
		Resp: okResponse{}, Perm: auth.PermInventoryConfig, handler: s.handleDeleteGroup})
	s.add(&route{Method: "GET", Path: "/api/v1/groups/{id}/members", Tag: "Gruppen", Summary: "Mitglieder einer Gruppe", Scope: scopeRead,
		Resp: groupMembers{}, handler: s.handleGroupMembers})

	s.add(&route{Method: "GET", Path: "/api/v1/custom-fields", Tag: "Custom Fields", Summary: "Custom-Field-Definitionen", Scope: scopeRead,
		Resp: []inventory.CustomField{}, handler: s.handleCustomFields})
	s.add(&route{Method: "POST", Path: "/api/v1/custom-fields", Tag: "Custom Fields", Summary: "Custom Field anlegen", Scope: scopeWrite,
		Body: inventory.CustomField{}, Resp: inventory.CustomField{}, Status: http.StatusCreated, Perm: auth.PermInventoryConfig, handler: s.handleSaveCustomField})
	s.add(&route{Method: "PUT", Path: "/api/v1/custom-fields/{id}", Tag: "Custom Fields", Summary: "Custom Field ändern", Scope: scopeWrite,
		Body: inventory.CustomField{}, Resp: inventory.CustomField{}, Perm: auth.PermInventoryConfig, handler: s.handleSaveCustomField})
	s.add(&route{Method: "DELETE", Path: "/api/v1/custom-fields/{id}", Tag: "Custom Fields", Summary: "Custom Field löschen (inkl. Werte)",
		Scope: scopeWrite, Resp: okResponse{}, Perm: auth.PermInventoryConfig, handler: s.handleDeleteCustomField})

	s.add(&route{Method: "GET", Path: "/api/v1/views", Tag: "Ansichten", Summary: "Gespeicherte Geräteansichten", Scope: scopeRead,
		Resp: []inventory.SavedView{}, handler: s.handleViews})
	s.add(&route{Method: "POST", Path: "/api/v1/views", Tag: "Ansichten", Summary: "Ansicht speichern", Scope: scopeWrite,
		Body: inventory.SavedView{}, Resp: inventory.SavedView{}, Status: http.StatusCreated, Perm: auth.PermInventoryConfig, handler: s.handleSaveView})
	s.add(&route{Method: "PUT", Path: "/api/v1/views/{id}", Tag: "Ansichten", Summary: "Ansicht ändern", Scope: scopeWrite,
		Body: inventory.SavedView{}, Resp: inventory.SavedView{}, Perm: auth.PermInventoryConfig, handler: s.handleSaveView})
	s.add(&route{Method: "DELETE", Path: "/api/v1/views/{id}", Tag: "Ansichten", Summary: "Ansicht löschen", Scope: scopeWrite,
		Resp: okResponse{}, Perm: auth.PermInventoryConfig, handler: s.handleDeleteView})
}

// optionalID returns the path id for PUT (0 for POST).
func optionalID(r *http.Request) (int64, error) {
	if r.PathValue("id") == "" {
		return 0, nil
	}
	return pathID(r, "id")
}

func (s *Server) handleSubnets(w http.ResponseWriter, r *http.Request) {
	list, err := s.Inventory.ListSubnets(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, s.subnetViews(list))
}

func (s *Server) handleSaveSubnet(w http.ResponseWriter, r *http.Request) {
	id, err := optionalID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var sn inventory.Subnet
	if err := decodeLenient(r, &sn); err != nil {
		s.fail(w, r, err)
		return
	}
	sn.ID = id
	if err := s.checkTunnelSubnet(r.Context(), &sn); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Inventory.SaveSubnet(r.Context(), &sn); err != nil {
		s.fail(w, r, err)
		return
	}
	s.reconcileTunnels()
	action, status := "subnet.update", http.StatusOK
	if id == 0 {
		action, status = "subnet.create", http.StatusCreated
	}
	s.record(r, action, "subnet", strconv.FormatInt(sn.ID, 10), "Subnetz "+sn.CIDR, nil, sn)
	writeJSON(w, status, s.subnetViews([]inventory.Subnet{sn})[0])
}

func (s *Server) handleDeleteSubnet(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Inventory.DeleteSubnet(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.reconcileTunnels()
	s.record(r, "subnet.delete", "subnet", strconv.FormatInt(id, 10), "Subnetz gelöscht", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleGroups(w http.ResponseWriter, r *http.Request) {
	list, err := s.Inventory.ListGroups(r.Context())
	s.respond(w, r, list, err)
}

func (s *Server) handleSaveGroup(w http.ResponseWriter, r *http.Request) {
	id, err := optionalID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var g inventory.Group
	if err := decodeLenient(r, &g); err != nil {
		s.fail(w, r, err)
		return
	}
	g.ID = id
	if err := s.Inventory.SaveGroup(r.Context(), &g); err != nil {
		s.fail(w, r, err)
		return
	}
	action, status := "group.update", http.StatusOK
	if id == 0 {
		action, status = "group.create", http.StatusCreated
	}
	s.record(r, action, "group", strconv.FormatInt(g.ID, 10), "Gruppe "+g.Name, nil, g)
	writeJSON(w, status, g)
}

func (s *Server) handleDeleteGroup(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Inventory.DeleteGroup(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "group.delete", "group", strconv.FormatInt(id, 10), "Gruppe gelöscht", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleGroupMembers(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ids, err := s.Inventory.GroupMemberIDs(r.Context(), id)
	if ids == nil {
		ids = []int64{}
	}
	s.respond(w, r, groupMembers{IDs: ids}, err)
}

func (s *Server) handleCustomFields(w http.ResponseWriter, r *http.Request) {
	list, err := s.Inventory.CustomFields(r.Context())
	s.respond(w, r, list, err)
}

func (s *Server) handleSaveCustomField(w http.ResponseWriter, r *http.Request) {
	id, err := optionalID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var c inventory.CustomField
	if err := decodeLenient(r, &c); err != nil {
		s.fail(w, r, err)
		return
	}
	c.ID = id
	if err := s.Inventory.SaveCustomField(r.Context(), &c); err != nil {
		s.fail(w, r, err)
		return
	}
	action, status := "customfield.update", http.StatusOK
	if id == 0 {
		action, status = "customfield.create", http.StatusCreated
	}
	s.record(r, action, "customfield", strconv.FormatInt(c.ID, 10), "Custom Field "+c.Key, nil, c)
	writeJSON(w, status, c)
}

func (s *Server) handleDeleteCustomField(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Inventory.DeleteCustomField(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "customfield.delete", "customfield", strconv.FormatInt(id, 10), "Custom Field gelöscht", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleViews(w http.ResponseWriter, r *http.Request) {
	list, err := s.Inventory.ListViews(r.Context())
	s.respond(w, r, list, err)
}

func (s *Server) handleSaveView(w http.ResponseWriter, r *http.Request) {
	id, err := optionalID(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var v inventory.SavedView
	if err := decodeLenient(r, &v); err != nil {
		s.fail(w, r, err)
		return
	}
	v.ID = id
	if err := s.Inventory.SaveView(r.Context(), &v); err != nil {
		s.fail(w, r, err)
		return
	}
	status := http.StatusOK
	if id == 0 {
		status = http.StatusCreated
	}
	s.record(r, "view.save", "view", strconv.FormatInt(v.ID, 10), "Ansicht "+v.Name, nil, v)
	writeJSON(w, status, v)
}

func (s *Server) handleDeleteView(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Inventory.DeleteView(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "view.delete", "view", strconv.FormatInt(id, 10), "Ansicht gelöscht", nil, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}
