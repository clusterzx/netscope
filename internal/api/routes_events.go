package api

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"netscope/internal/auth"
	"netscope/internal/events"
	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/rules"
)

type ackRequest struct {
	IDs    []int64      `json:"ids,omitempty"`
	Filter *eventFilter `json:"filter,omitempty"`
	Note   string       `json:"note,omitempty"`
}

// eventFilter selects the events to acknowledge – the same criteria as the list.
type eventFilter struct {
	Types       []string   `json:"types,omitempty"`
	Categories  []string   `json:"categories,omitempty"`
	MinSeverity string     `json:"minSeverity,omitempty"`
	DeviceID    int64      `json:"deviceId,omitempty"`
	RunID       int64      `json:"runId,omitempty"`
	Text        string     `json:"text,omitempty"`
	From        *time.Time `json:"from,omitempty"`
	To          *time.Time `json:"to,omitempty"`
	Site        string     `json:"site,omitempty"`
}

type ackResponse struct {
	Acknowledged int `json:"acknowledged"`
}

type eventDetail struct {
	events.Event
	Notifications []rules.NotificationView `json:"notifications"`
	PrevRunID     int64                    `json:"prevRunId,omitempty"`
}

func (s *Server) registerEvents() {
	s.add(&route{Method: "GET", Path: "/api/v1/events", Tag: "Events", Summary: "Events mit Filter", Scope: scopeRead,
		Params: []param{{Name: "type", Desc: "kommagetrennt, Muster wie port.* erlaubt"}, {Name: "category"}, {Name: "severity", Desc: "Mindest-Schweregrad"},
			{Name: "device", Type: "integer"}, {Name: "run", Type: "integer"}, {Name: "acked", Type: "boolean"}, {Name: "from"}, {Name: "to"},
			{Name: "q"}, {Name: "site", Desc: "nur ein Standort (Zentrale): Kürzel, local = diese Instanz"},
			{Name: "limit", Type: "integer"}, {Name: "offset", Type: "integer"}},
		Resp: eventList{}, handler: s.handleEvents})
	s.add(&route{Method: "GET", Path: "/api/v1/events/types", Tag: "Events", Summary: "Katalog der Event-Typen", Scope: scopeRead,
		Resp: []plugin.EventSpec{}, handler: s.handleEventTypes})
	s.add(&route{Method: "GET", Path: "/api/v1/events/counts", Tag: "Events", Summary: "Offene Events je Schweregrad", Scope: scopeRead,
		Resp: map[string]int{}, handler: s.handleEventCounts})
	s.add(&route{Method: "POST", Path: "/api/v1/events/ack", Tag: "Events", Summary: "Events quittieren (IDs oder Filter)", Scope: scopeWrite,
		Body: ackRequest{}, Resp: ackResponse{}, Perm: auth.PermEventsAck, handler: s.handleAck})
	s.add(&route{Method: "GET", Path: "/api/v1/events/{id}", Tag: "Events", Summary: "Ein Event inkl. Benachrichtigungen", Scope: scopeRead,
		Resp: eventDetail{}, handler: s.handleEvent})
	s.add(&route{Method: "GET", Path: "/api/v1/diff", Tag: "Diff", Summary: "Zwei Läufe (runA/runB) oder zwei Zeitpunkte (from/to) vergleichen",
		Scope: scopeRead, Params: []param{{Name: "runA", Type: "integer"}, {Name: "runB", Type: "integer"}, {Name: "from"}, {Name: "to"},
			{Name: "device", Type: "integer"}}, Resp: inventory.DiffResult{}, handler: s.handleDiff})
}

func splitList(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := events.Filter{Types: splitList(q.Get("type")), Categories: splitList(q.Get("category")), MinSeverity: plugin.Severity(q.Get("severity")),
		DeviceID: qInt64(r, "device"), RunID: qInt64(r, "run"), Text: q.Get("q"), Limit: qInt(r, "limit", 100), Offset: qInt(r, "offset", 0)}
	if v := q.Get("acked"); v != "" {
		b := qBool(r, "acked")
		f.Acked = &b
	}
	var err error
	if f.Site, err = s.siteFilter(r.Context(), q.Get("site")); err != nil {
		s.fail(w, r, err)
		return
	}
	if f.From, err = s.qTime(r, "from"); err != nil {
		s.fail(w, r, err)
		return
	}
	if f.To, err = s.qTime(r, "to"); err != nil {
		s.fail(w, r, err)
		return
	}
	list, total, err := s.Events.List(r.Context(), f)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, eventList{Total: total, Items: list})
}

func (s *Server) handleEventTypes(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, plugin.Catalog())
}

func (s *Server) handleEventCounts(w http.ResponseWriter, r *http.Request) {
	c, err := s.Events.OpenCounts(r.Context())
	s.respond(w, r, c, err)
}

func (s *Server) handleAck(w http.ResponseWriter, r *http.Request) {
	var req ackRequest
	if err := decode(r, &req); err != nil {
		s.fail(w, r, err)
		return
	}
	var (
		n   int
		err error
	)
	switch {
	case len(req.IDs) > 0:
		n, err = s.Events.Ack(r.Context(), req.IDs, actorName(r), req.Note)
	case req.Filter != nil:
		f := events.Filter{Types: req.Filter.Types, Categories: req.Filter.Categories, MinSeverity: plugin.Severity(req.Filter.MinSeverity),
			DeviceID: req.Filter.DeviceID, RunID: req.Filter.RunID, Text: req.Filter.Text}
		if req.Filter.From != nil {
			f.From = *req.Filter.From
		}
		if req.Filter.To != nil {
			f.To = *req.Filter.To
		}
		if f.Site, err = s.siteFilter(r.Context(), req.Filter.Site); err != nil {
			s.fail(w, r, err)
			return
		}
		n, err = s.Events.AckFilter(r.Context(), f, actorName(r), req.Note)
	default:
		err = errors.New("IDs oder Filter angeben")
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.record(r, "event.ack", "event", "", fmt.Sprintf("%d Event(s) quittiert", n), nil, req)
	writeJSON(w, http.StatusOK, ackResponse{Acknowledged: n})
}

func (s *Server) handleEvent(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	ev, err := s.Events.Get(r.Context(), id)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	notes, _, err := s.Rules.Notifications(r.Context(), rules.NotificationFilter{EventID: id, Limit: 50})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	d := eventDetail{Event: *ev, Notifications: notes}
	if ev.RunID > 0 {
		d.PrevRunID, _ = s.Inventory.PreviousRun(r.Context(), ev.RunID)
	}
	writeJSON(w, http.StatusOK, d)
}

func (s *Server) handleDiff(w http.ResponseWriter, r *http.Request) {
	dev := qInt64(r, "device")
	if a, b := qInt64(r, "runA"), qInt64(r, "runB"); a > 0 || b > 0 {
		if a <= 0 || b <= 0 {
			s.fail(w, r, errors.New("runA und runB angeben"))
			return
		}
		res, err := s.Inventory.DiffRuns(r.Context(), a, b, dev)
		s.respond(w, r, res, err)
		return
	}
	from, err := s.qTime(r, "from")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	to, err := s.qTime(r, "to")
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if from.IsZero() {
		s.fail(w, r, errors.New("from/to oder runA/runB angeben"))
		return
	}
	if to.IsZero() {
		to = time.Now()
	}
	if !from.Before(to) {
		s.fail(w, r, errors.New("from muss vor to liegen"))
		return
	}
	res, err := s.Inventory.DiffTimes(r.Context(), from, to, dev)
	s.respond(w, r, res, err)
}
