package api

import (
	"net/http"
	"strconv"

	"netscope/internal/plugins/cve"
)

type vulnList struct {
	Total      int           `json:"total"`
	Items      []cve.VulnRow `json:"items"`
	Disclaimer string        `json:"disclaimer"`
}

type deviceCVEList struct {
	Items      []cve.DeviceCVE `json:"items"`
	Disclaimer string          `json:"disclaimer"`
}

type cveDetail struct {
	CVE        *cve.CVEInfo    `json:"cve"`
	Devices    []cve.DeviceCVE `json:"devices"`
	Disclaimer string          `json:"disclaimer"`
}

type ignoreRequest struct {
	DeviceID int64  `json:"deviceId"`
	CVE      string `json:"cve"`
	Ignored  bool   `json:"ignored"`
	Note     string `json:"note,omitempty"`
}

type vulnStatus struct {
	*cve.Status
	Summary    map[string]int `json:"summary"`
	Disclaimer string         `json:"disclaimer"`
}

func (s *Server) registerVulns() {
	s.add(&route{Method: "GET", Path: "/api/v1/vulnerabilities", Tag: "Schwachstellen", Summary: "CVE-Liste über alle Geräte (heuristischer Abgleich)",
		Scope: scopeRead, Params: []param{{Name: "min", Type: "number", Desc: "Mindest-CVSS"}, {Name: "q", Desc: "CVE-ID oder Beschreibung"},
			{Name: "product"}, {Name: "device", Type: "integer"}, {Name: "ignored", Type: "boolean", Desc: "auch als irrelevant markierte"},
			{Name: "sort", Desc: "score | published | devices | cve | first_seen, - für absteigend"}, {Name: "limit", Type: "integer"},
			{Name: "offset", Type: "integer"}, {Name: "site", Desc: "nur ein Standort (Zentrale): Kürzel, local = diese Instanz"}},
		Resp: vulnList{}, handler: s.handleVulns})
	s.add(&route{Method: "GET", Path: "/api/v1/vulnerabilities/status", Tag: "Schwachstellen", Summary: "NVD-Sync-Status und Übersicht",
		Scope: scopeRead, Resp: vulnStatus{}, handler: s.handleVulnStatus})
	s.add(&route{Method: "POST", Path: "/api/v1/vulnerabilities/ignore", Tag: "Schwachstellen", Summary: "CVE für ein Gerät als irrelevant markieren (oder zurücknehmen)",
		Scope: scopeWrite, Body: ignoreRequest{}, Resp: okResponse{}, handler: s.handleIgnoreCVE})
	s.add(&route{Method: "GET", Path: "/api/v1/vulnerabilities/{cve}", Tag: "Schwachstellen", Summary: "Details einer CVE mit betroffenen Geräten",
		Scope: scopeRead, Resp: cveDetail{}, handler: s.handleVulnDetail})
	s.add(&route{Method: "GET", Path: "/api/v1/devices/{id}/cves", Tag: "Geräte", Summary: "CVEs eines Geräts", Scope: scopeRead,
		Params: []param{{Name: "ignored", Type: "boolean"}}, Resp: deviceCVEList{}, handler: s.handleDeviceCVEs})
}

func (s *Server) handleVulns(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := cve.Filter{Text: q.Get("q"), Product: q.Get("product"), DeviceID: qInt64(r, "device"), IncludeIgnored: qBool(r, "ignored"),
		Sort: q.Get("sort"), Limit: qInt(r, "limit", 100), Offset: qInt(r, "offset", 0)}
	if v := q.Get("min"); v != "" {
		if n, err := strconv.ParseFloat(v, 64); err == nil {
			f.MinScore = n
		}
	}
	site, err := s.siteFilter(r.Context(), q.Get("site"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	f.Site = site
	items, total, err := cve.ListVulnerabilities(r.Context(), s.DB, f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if items == nil {
		items = []cve.VulnRow{}
	}
	writeJSON(w, http.StatusOK, vulnList{Total: total, Items: items, Disclaimer: cve.Disclaimer})
}

func (s *Server) handleVulnStatus(w http.ResponseWriter, r *http.Request) {
	st, err := cve.SyncStatus(r.Context(), s.DB)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sum, err := cve.Summary(r.Context(), s.DB)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, vulnStatus{Status: st, Summary: sum, Disclaimer: cve.Disclaimer})
}

func (s *Server) handleVulnDetail(w http.ResponseWriter, r *http.Request) {
	id, err := cve.NormalizeCVEID(r.PathValue("cve"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	info, devs, err := cve.CVEDetail(r.Context(), s.DB, id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if devs == nil {
		devs = []cve.DeviceCVE{}
	}
	writeJSON(w, http.StatusOK, cveDetail{CVE: info, Devices: devs, Disclaimer: cve.Disclaimer})
}

func (s *Server) handleIgnoreCVE(w http.ResponseWriter, r *http.Request) {
	var req ignoreRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	id, err := cve.NormalizeCVEID(req.CVE)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if err := cve.SetIgnored(r.Context(), s.DB, req.DeviceID, id, req.Ignored, req.Note, actorName(r)); err != nil {
		s.fail(w, r, err)
		return
	}
	summary := id + " als irrelevant markiert: " + s.Inventory.Name(r.Context(), req.DeviceID)
	if !req.Ignored {
		summary = id + " wieder als relevant markiert: " + s.Inventory.Name(r.Context(), req.DeviceID)
	}
	s.record(r, "cve.ignore", "device", strconv.FormatInt(req.DeviceID, 10), summary, nil, req)
	writeJSON(w, http.StatusOK, okResponse{OK: true})
}

func (s *Server) handleDeviceCVEs(w http.ResponseWriter, r *http.Request) {
	id, ok := s.deviceID(w, r)
	if !ok {
		return
	}
	items, err := cve.DeviceCVEs(r.Context(), s.DB, id, qBool(r, "ignored"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if items == nil {
		items = []cve.DeviceCVE{}
	}
	writeJSON(w, http.StatusOK, deviceCVEList{Items: items, Disclaimer: cve.Disclaimer})
}
