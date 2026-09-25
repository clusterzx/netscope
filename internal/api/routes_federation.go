package api

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"netscope/internal/auth"
	"netscope/internal/federation"
	"netscope/internal/federation/wire"
)

// federationView is the federation state of this instance.
type federationView struct {
	Settings federation.SettingsView `json:"settings"`
	// LocalName names this instance in the site selector.
	LocalName string `json:"localName"`
	// Site is the delivery state (site role).
	Site *federation.SiteStatus `json:"site,omitempty"`
	// Sites are the connected sites (central role).
	Sites []siteRef `json:"sites"`
	// Protocol is the federation protocol this build speaks.
	Protocol int `json:"protocol"`
}

// siteRef is a site for selectors.
type siteRef struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	Connected bool   `json:"connected"`
	// Contacted: the site has reported at least once (a new site is not "disconnected").
	Contacted bool   `json:"contacted"`
	LinkURL   string `json:"linkUrl,omitempty"`
}

// siteCreated is a new site with its token (shown once).
type siteCreated struct {
	Site  *federation.Site `json:"site"`
	Token string           `json:"token"`
}

// siteToken is a new token of a site (shown once).
type siteToken struct {
	Token string `json:"token"`
}

// maxIngestBody limits a decompressed delivery of a site.
const maxIngestBody = 64 << 20

func (s *Server) registerFederation() {
	s.add(&route{Method: "GET", Path: "/api/v1/federation", Tag: "Verbund", Summary: "Rolle der Instanz, Zustand der Anbindung und Standorte",
		Scope: scopeRead, Resp: federationView{}, handler: s.handleFederation})
	s.add(&route{Method: "PUT", Path: "/api/v1/federation", Tag: "Verbund",
		Summary: "Rolle festlegen (eigenständig, Standort, Zentrale) und die Zentrale eines Standorts eintragen", Scope: scopeWrite,
		Body: federation.SettingsInput{}, Resp: federationView{}, Perm: auth.PermSitesManage, handler: s.handleUpdateFederation})
	s.add(&route{Method: "POST", Path: "/api/v1/federation/test", Tag: "Verbund", Summary: "Standort: Verbindung zur Zentrale prüfen",
		Scope: scopeWrite, Resp: federation.TestResult{}, Perm: auth.PermSitesManage, handler: s.handleTestFederation})
	s.add(&route{Method: "POST", Path: "/api/v1/federation/resync", Tag: "Verbund",
		Summary: "Standort: vollständigen Abgleich mit der Zentrale anstoßen", Scope: scopeWrite, Status: http.StatusNoContent,
		Perm: auth.PermSitesManage, handler: s.handleResyncFederation})
	s.add(&route{Method: "GET", Path: "/api/v1/sites", Tag: "Verbund", Summary: "Zentrale: Standorte mit Verbindungszustand und letzter Meldung",
		Scope: scopeRead, Resp: []federation.Site{}, handler: s.handleSites})
	s.add(&route{Method: "POST", Path: "/api/v1/sites", Tag: "Verbund", Summary: "Zentrale: Standort anlegen (liefert das Token einmalig)",
		Scope: scopeWrite, Body: federation.SiteInput{}, Resp: siteCreated{}, Status: http.StatusCreated, Perm: auth.PermSitesManage, handler: s.handleCreateSite})
	s.add(&route{Method: "GET", Path: "/api/v1/sites/{id}", Tag: "Verbund", Summary: "Zentrale: ein Standort", Scope: scopeRead,
		Params: []param{{Name: "id", In: "path", Type: "integer", Required: true}}, Resp: federation.Site{}, handler: s.handleSite})
	s.add(&route{Method: "PATCH", Path: "/api/v1/sites/{id}", Tag: "Verbund", Summary: "Zentrale: Standort umbenennen oder Adresse ändern",
		Scope: scopeWrite, Params: []param{{Name: "id", In: "path", Type: "integer", Required: true}}, Body: federation.SiteInput{},
		Resp: federation.Site{}, Perm: auth.PermSitesManage, handler: s.handleUpdateSite})
	s.add(&route{Method: "POST", Path: "/api/v1/sites/{id}/token", Tag: "Verbund",
		Summary: "Zentrale: neues Token für einen Standort (das alte gilt sofort nicht mehr)", Scope: scopeWrite,
		Params: []param{{Name: "id", In: "path", Type: "integer", Required: true}}, Resp: siteToken{}, Perm: auth.PermSitesManage, handler: s.handleSiteToken})
	s.add(&route{Method: "DELETE", Path: "/api/v1/sites/{id}", Tag: "Verbund",
		Summary: "Zentrale: Standort mit allen gelieferten Geräten und Events entfernen", Scope: scopeWrite, Status: http.StatusNoContent,
		Params: []param{{Name: "id", In: "path", Type: "integer", Required: true}}, Perm: auth.PermSitesManage, handler: s.handleDeleteSite})
	s.add(&route{Method: "POST", Path: wire.IngestPath, Tag: "Verbund",
		Summary: "Zentrale: Lieferung eines Standorts annehmen (nur mit Standort-Token nss_…; gzip erlaubt)", Scope: scopePublic,
		Body: wire.Batch{}, Resp: wire.Response{}, handler: s.handleIngest})
}

var errNoFederation = errors.New("Verbund nicht verfügbar")

func (s *Server) federationState(r *http.Request) (*federationView, error) {
	if s.Federation == nil {
		return nil, errNoFederation
	}
	f := s.Federation
	out := &federationView{Settings: f.Current(), LocalName: f.LocalName(), Sites: []siteRef{}, Protocol: wire.Protocol}
	switch f.Role() {
	case federation.RoleSite:
		st := f.SiteStatus(r.Context())
		out.Site = &st
	case federation.RoleCentral:
		sites, err := f.Sites(r.Context())
		if err != nil {
			return nil, err
		}
		for _, st := range sites {
			out.Sites = append(out.Sites, siteRef{ID: st.ID, Name: st.Name, Slug: st.Slug, Connected: st.Connected,
				Contacted: st.LastContact != nil, LinkURL: st.LinkURL})
		}
	}
	return out, nil
}

func (s *Server) handleFederation(w http.ResponseWriter, r *http.Request) {
	out, err := s.federationState(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpdateFederation(w http.ResponseWriter, r *http.Request) {
	if s.Federation == nil {
		s.fail(w, r, errNoFederation)
		return
	}
	var in federation.SettingsInput
	if err := decodeLenient(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	before := s.Federation.Current()
	after, err := s.Federation.Update(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "federation.update", "federation", "", fmt.Sprintf("Verbund: Rolle %s", roleLabel(after.Role)), before, after)
	out, err := s.federationState(r)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func roleLabel(role string) string {
	switch role {
	case federation.RoleSite:
		return "Standort"
	case federation.RoleCentral:
		return "Zentrale"
	}
	return "eigenständig"
}

func (s *Server) handleTestFederation(w http.ResponseWriter, r *http.Request) {
	if s.Federation == nil {
		s.fail(w, r, errNoFederation)
		return
	}
	writeJSON(w, http.StatusOK, s.Federation.Test(r.Context()))
}

func (s *Server) handleResyncFederation(w http.ResponseWriter, r *http.Request) {
	if s.Federation == nil {
		s.fail(w, r, errNoFederation)
		return
	}
	if err := s.Federation.Resync(r.Context()); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "federation.resync", "federation", "", "Vollständiger Abgleich mit der Zentrale angestoßen", nil, nil)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleSites(w http.ResponseWriter, r *http.Request) {
	if s.Federation == nil {
		s.fail(w, r, errNoFederation)
		return
	}
	list, err := s.Federation.Sites(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, list)
}

func (s *Server) handleSite(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil || s.Federation == nil {
		s.fail(w, r, errors.Join(err, errNoFederationIfNil(s.Federation)))
		return
	}
	st, err := s.Federation.Site(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, st)
}

func errNoFederationIfNil(f *federation.Service) error {
	if f == nil {
		return errNoFederation
	}
	return nil
}

func (s *Server) handleCreateSite(w http.ResponseWriter, r *http.Request) {
	if s.Federation == nil {
		s.fail(w, r, errNoFederation)
		return
	}
	var in federation.SiteInput
	if err := decode(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	st, token, err := s.Federation.CreateSite(r.Context(), in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "site.create", "site", fmt.Sprint(st.ID), "Standort "+st.Name+" angelegt", nil, in)
	writeJSON(w, http.StatusCreated, siteCreated{Site: st, Token: token})
}

func (s *Server) handleUpdateSite(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil || s.Federation == nil {
		s.fail(w, r, errors.Join(err, errNoFederationIfNil(s.Federation)))
		return
	}
	var in federation.SiteInput
	if err := decodeLenient(r, &in); err != nil {
		s.fail(w, r, err)
		return
	}
	before, err := s.Federation.Site(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	st, err := s.Federation.UpdateSite(r.Context(), id, in)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "site.update", "site", fmt.Sprint(id), "Standort "+st.Name+" geändert",
		federation.SiteInput{Name: before.Name, URL: before.URL}, in)
	writeJSON(w, http.StatusOK, st)
}

func (s *Server) handleSiteToken(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil || s.Federation == nil {
		s.fail(w, r, errors.Join(err, errNoFederationIfNil(s.Federation)))
		return
	}
	token, err := s.Federation.RotateSiteToken(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "site.token", "site", fmt.Sprint(id), "Neues Token für einen Standort erzeugt", nil, nil)
	writeJSON(w, http.StatusOK, siteToken{Token: token})
}

func (s *Server) handleDeleteSite(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil || s.Federation == nil {
		s.fail(w, r, errors.Join(err, errNoFederationIfNil(s.Federation)))
		return
	}
	st, err := s.Federation.Site(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := s.Federation.DeleteSite(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "site.delete", "site", fmt.Sprint(id), fmt.Sprintf("Standort %s mit %d Geräten entfernt", st.Name, st.Devices), st, nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleIngest accepts a delivery of a site. It authenticates with the site token only
// (never with sessions or API tokens) and answers with the acknowledged sequence number.
func (s *Server) handleIngest(w http.ResponseWriter, r *http.Request) {
	if s.Federation == nil {
		writeError(w, http.StatusNotFound, "not_found", federation.ErrNotCentral.Error(), nil)
		return
	}
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "Standort-Token fehlt", nil)
		return
	}
	id, name, err := s.Federation.AuthenticateSite(r.Context(), strings.TrimSpace(tok))
	if errors.Is(err, federation.ErrNotCentral) {
		writeError(w, http.StatusNotFound, "not_central", err.Error(), nil)
		return
	}
	if err != nil {
		writeError(w, http.StatusUnauthorized, "unauthenticated", "Standort-Token ungültig", nil)
		return
	}
	var body io.Reader = r.Body
	if strings.EqualFold(r.Header.Get("Content-Encoding"), "gzip") {
		zr, err := gzip.NewReader(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "bad_request", "gzip: "+err.Error(), nil)
			return
		}
		defer zr.Close()
		body = zr
	}
	var batch wire.Batch
	if err := json.NewDecoder(io.LimitReader(body, maxIngestBody)).Decode(&batch); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "ungültige Lieferung: "+err.Error(), nil)
		return
	}
	resp, err := s.Federation.Ingest(r.Context(), id, name, &batch, client(r).IP)
	var pe *federation.ProtocolError
	switch {
	case errors.As(err, &pe):
		writeError(w, http.StatusConflict, "protocol", pe.Error(), nil)
		return
	case err != nil:
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// siteDeviceError rejects scans and actions for a device of a site: they would run from
// here against addresses of another network (and the central instance never triggers
// anything at a site).
func (s *Server) siteDeviceError(ctx context.Context, id int64, what string) error {
	ref, err := s.Inventory.DeviceSite(ctx, id)
	if err != nil || ref == nil {
		return err
	}
	return fmt.Errorf("Das Gerät gehört zum Standort %s – %s laufen dort, nicht in der Zentrale", ref.Name, what)
}

// withSite restricts a device query to a site ("" or "all" = no restriction).
func withSite(query, site string) string {
	site = strings.TrimSpace(site)
	if site == "" || site == "all" {
		return query
	}
	return strings.TrimSpace(query + ` site:"` + strings.ReplaceAll(site, `"`, "") + `"`)
}

// siteFilter resolves the site parameter of list endpoints: nil = all, 0 = this
// instance, otherwise the site with that slug, name or id.
func (s *Server) siteFilter(ctx context.Context, v string) (*int64, error) {
	v = strings.TrimSpace(v)
	switch strings.ToLower(v) {
	case "", "all":
		return nil, nil
	case "local", "lokal":
		zero := int64(0)
		return &zero, nil
	}
	var id int64
	err := s.DB.R.QueryRowContext(ctx, "SELECT id FROM sites WHERE slug = ? OR name = ? COLLATE NOCASE OR CAST(id AS TEXT) = ?", v, v, v).Scan(&id)
	if err != nil {
		return nil, fmt.Errorf("unbekannter Standort %q", v)
	}
	return &id, nil
}
