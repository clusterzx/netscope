package api

import (
	"fmt"
	"net/http"
	"strconv"

	"netscope/internal/auth"
	"netscope/internal/rack"
)

type adoptResponse struct {
	Ports int `json:"ports"`
}

func (s *Server) registerRacks() {
	idp := []param{{Name: "id", In: "path", Type: "integer", Required: true}}
	s.add(&route{Method: "GET", Path: "/api/v1/racks", Tag: "Racks", Summary: "Racks mit Belegung", Scope: scopeRead,
		Resp: []rack.Summary{}, handler: s.handleRacks})
	s.add(&route{Method: "POST", Path: "/api/v1/racks", Tag: "Racks", Summary: "Rack anlegen", Scope: scopeWrite,
		Body: rack.RackInput{}, Resp: rack.Rack{}, Status: http.StatusCreated, Perm: auth.PermDevicesEdit, handler: s.handleCreateRack})
	s.add(&route{Method: "GET", Path: "/api/v1/racks/{id}", Tag: "Racks", Summary: "Rack mit eingebauten Elementen, Ports und Kabeln", Scope: scopeRead,
		Params: idp, Resp: rack.View{}, handler: s.handleRack})
	s.add(&route{Method: "PUT", Path: "/api/v1/racks/{id}", Tag: "Racks", Summary: "Rack ändern", Scope: scopeWrite, Params: idp,
		Body: rack.RackInput{}, Resp: rack.Rack{}, Perm: auth.PermDevicesEdit, handler: s.handleUpdateRack})
	s.add(&route{Method: "DELETE", Path: "/api/v1/racks/{id}", Tag: "Racks", Summary: "Rack mit allem Eingebauten löschen", Scope: scopeWrite, Params: idp,
		Resp: okResponse{}, Perm: auth.PermDevicesEdit, handler: s.handleDeleteRack})
	s.add(&route{Method: "POST", Path: "/api/v1/racks/{id}/items", Tag: "Racks", Summary: "Gerät oder passives Element einbauen", Scope: scopeWrite,
		Params: idp, Body: rack.ItemInput{}, Resp: idResponse{}, Status: http.StatusCreated, Perm: auth.PermDevicesEdit, handler: s.handleAddRackItem})
	s.add(&route{Method: "PUT", Path: "/api/v1/rack-items/{id}", Tag: "Racks", Summary: "Element ändern oder verschieben (auch in ein anderes Rack)",
		Scope: scopeWrite, Params: idp, Body: rack.ItemInput{}, Resp: okResponse{}, Perm: auth.PermDevicesEdit, handler: s.handleUpdateRackItem})
	s.add(&route{Method: "DELETE", Path: "/api/v1/rack-items/{id}", Tag: "Racks", Summary: "Element ausbauen", Scope: scopeWrite, Params: idp,
		Resp: okResponse{}, Perm: auth.PermDevicesEdit, handler: s.handleDeleteRackItem})
	s.add(&route{Method: "PUT", Path: "/api/v1/rack-items/{id}/port", Tag: "Racks", Summary: "Beschriftung und angeschlossenes Gerät eines Ports",
		Scope: scopeWrite, Params: idp, Body: rack.PortInput{}, Resp: okResponse{}, Perm: auth.PermDevicesEdit, handler: s.handleSetRackPort})
	s.add(&route{Method: "POST", Path: "/api/v1/rack-items/{id}/adopt", Tag: "Racks", Summary: "Erkannte Verbindungen an freien Ports übernehmen",
		Scope: scopeWrite, Params: idp, Resp: adoptResponse{}, Perm: auth.PermDevicesEdit, handler: s.handleAdoptRackItem})
	s.add(&route{Method: "POST", Path: "/api/v1/rack-cables", Tag: "Racks", Summary: "Patchkabel zwischen zwei Ports", Scope: scopeWrite,
		Body: rack.CableInput{}, Resp: idResponse{}, Status: http.StatusCreated, Perm: auth.PermDevicesEdit, handler: s.handleAddRackCable})
	s.add(&route{Method: "DELETE", Path: "/api/v1/rack-cables/{id}", Tag: "Racks", Summary: "Patchkabel entfernen", Scope: scopeWrite, Params: idp,
		Resp: okResponse{}, Perm: auth.PermDevicesEdit, handler: s.handleDeleteRackCable})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/rack", Tag: "Geräte", Summary: "Einbauort im Rack und Rack-Ports, die zum Gerät führen",
		Scope: scopeRead, Params: idp, Resp: rack.DeviceInfo{}, handler: s.handleDeviceRack})
}

func (s *Server) handleRacks(w http.ResponseWriter, r *http.Request) {
	list, err := s.Racks.List(r.Context())
	s.respond(w, r, list, err)
}

func (s *Server) handleRack(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	v, err := s.Racks.View(r.Context(), id)
	s.respond(w, r, v, err)
}

func (s *Server) handleCreateRack(w http.ResponseWriter, r *http.Request) {
	var in rack.RackInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	rk, err := s.Racks.Create(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "rack.create", "rack", strconv.FormatInt(rk.ID, 10), "Rack "+rk.Name+" angelegt", nil, rk)
	writeJSON(w, http.StatusCreated, rk)
}

func (s *Server) handleUpdateRack(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in rack.RackInput
	if err := decodeLenient(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	before, err := s.Racks.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rk, err := s.Racks.Update(r.Context(), id, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "rack.update", "rack", strconv.FormatInt(id, 10), "Rack "+rk.Name+" geändert", before, rk)
	writeJSON(w, http.StatusOK, rk)
}

func (s *Server) handleDeleteRack(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	rk, err := s.Racks.Delete(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "rack.delete", "rack", strconv.FormatInt(id, 10), "Rack "+rk.Name+" gelöscht", rk, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleAddRackItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in rack.ItemInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	item, err := s.Racks.AddItem(r.Context(), id, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordItem(r, "rack.item_create", item, "%s eingebaut (HE %d)", in.Position, in)
	writeJSON(w, http.StatusCreated, idResponse{ID: item})
}

// recordItem writes an audit entry naming the item and its rack.
func (s *Server) recordItem(r *http.Request, action string, item int64, format string, position int, after any) {
	ref, err := s.Racks.Item(r.Context(), item)
	if err != nil {
		return
	}
	s.recordRef(r, action, ref, fmt.Sprintf(format, ref.Name, position), after)
}

func (s *Server) recordRef(r *http.Request, action string, ref *rack.ItemRef, summary string, after any) {
	rackName := ""
	if rk, err := s.Racks.Get(r.Context(), ref.RackID); err == nil {
		rackName = rk.Name
	}
	s.record(r, action, "rack", strconv.FormatInt(ref.RackID, 10), "Rack "+rackName+": "+summary, nil, after)
}

func (s *Server) handleUpdateRackItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in rack.ItemInput
	if err := decodeLenient(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Racks.UpdateItem(r.Context(), id, in); err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordItem(r, "rack.item_update", id, "%s geändert (HE %d)", in.Position, in)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleDeleteRackItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ref, err := s.Racks.DeleteItem(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordRef(r, "rack.item_delete", ref, ref.Name+" ausgebaut", nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleSetRackPort(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	var in rack.PortInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Racks.SetPort(r.Context(), id, in); err != nil {
		s.fail(w, r, err)
		return
	}
	if ref, err := s.Racks.Item(r.Context(), id); err == nil {
		summary := ref.Name + " Port " + in.Port + " ohne Gerät"
		if in.DeviceID > 0 {
			summary = ref.Name + " Port " + in.Port + " → " + s.Inventory.Name(r.Context(), in.DeviceID)
		}
		s.recordRef(r, "rack.port_update", ref, summary, in)
	}
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleAdoptRackItem(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	n, err := s.Racks.Adopt(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if ref, err := s.Racks.Item(r.Context(), id); err == nil && n > 0 {
		s.recordRef(r, "rack.port_adopt", ref, fmt.Sprintf("%s: %d erkannte Verbindungen übernommen", ref.Name, n), nil)
	}
	writeJSON(w, http.StatusOK, adoptResponse{Ports: n})
}

func (s *Server) handleAddRackCable(w http.ResponseWriter, r *http.Request) {
	var in rack.CableInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	id, err := s.Racks.AddCable(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordCable(r, "rack.cable_create", "Patchkabel %s → %s", in.A, in.B, in)
	writeJSON(w, http.StatusCreated, idResponse{ID: id})
}

func (s *Server) recordCable(r *http.Request, action, format string, a, b rack.End, data any) {
	ra, err := s.Racks.Item(r.Context(), a.ItemID)
	if err != nil {
		return
	}
	nb := strconv.FormatInt(b.ItemID, 10)
	if rb, err := s.Racks.Item(r.Context(), b.ItemID); err == nil {
		nb = rb.Name
	}
	s.recordRef(r, action, ra, fmt.Sprintf(format, ra.Name+" "+a.Port, nb+" "+b.Port), data)
}

func (s *Server) handleDeleteRackCable(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	c, err := s.Racks.DeleteCable(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordCable(r, "rack.cable_delete", "Patchkabel %s → %s entfernt", c.A, c.B, nil)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleDeviceRack(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	info, err := s.Racks.Device(r.Context(), id)
	s.respond(w, r, info, err)
}
