// Package logging sets up slog with a configurable level and an in-memory ring buffer
// that backs the log viewer in the UI (and streams new lines over the bus).
package logging

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

	"netscope/internal/bus"
)

// Entry is a captured log line.
type Entry struct {
	Seq    uint64         `json:"seq"`
	Time   time.Time      `json:"time"`
	Level  string         `json:"level"`
	Msg    string         `json:"msg"`
	Plugin string         `json:"plugin,omitempty"`
	Attrs  map[string]any `json:"attrs,omitempty"`
}

// Ring stores the most recent log entries.
type Ring struct {
	mu      sync.Mutex
	entries []Entry
	size    int
	next    int
	full    bool
	seq     uint64
	bus     *bus.Bus
}

// NewRing creates a ring buffer with the given capacity.
func NewRing(size int) *Ring { return &Ring{entries: make([]Entry, size), size: size} }

// AttachBus streams new entries to the bus (topic "log").
func (r *Ring) AttachBus(b *bus.Bus) {
	r.mu.Lock()
	r.bus = b
	r.mu.Unlock()
}

func (r *Ring) add(e Entry) {
	r.mu.Lock()
	r.seq++
	e.Seq = r.seq
	r.entries[r.next] = e
	r.next = (r.next + 1) % r.size
	if r.next == 0 {
		r.full = true
	}
	b := r.bus
	r.mu.Unlock()
	if b != nil {
		b.Publish(bus.TopicLog, "line", e)
	}
}

// Query returns entries (oldest first) filtered by minimum level, plugin and text.
func (r *Ring) Query(minLevel slog.Level, plugin, text string, limit int) []Entry {
	r.mu.Lock()
	defer r.mu.Unlock()
	var all []Entry
	if r.full {
		all = append(all, r.entries[r.next:]...)
	}
	all = append(all, r.entries[:r.next]...)
	text = strings.ToLower(text)
	var out []Entry
	for _, e := range all {
		var lvl slog.Level
		_ = lvl.UnmarshalText([]byte(e.Level))
		if lvl < minLevel {
			continue
		}
		if plugin != "" && e.Plugin != plugin {
			continue
		}
		if text != "" && !strings.Contains(strings.ToLower(e.Msg), text) {
			continue
		}
		out = append(out, e)
	}
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// Handler writes to an inner handler and captures entries into the ring.
type Handler struct {
	inner  slog.Handler
	ring   *Ring
	level  *slog.LevelVar
	attrs  []slog.Attr
	groups []string
}

// New creates the root logger.
func New(w io.Writer, format string, level *slog.LevelVar, ring *Ring) *slog.Logger {
	opts := &slog.HandlerOptions{Level: level}
	var inner slog.Handler
	if format == "text" {
		inner = slog.NewTextHandler(w, opts)
	} else {
		inner = slog.NewJSONHandler(w, opts)
	}
	return slog.New(&Handler{inner: inner, ring: ring, level: level})
}

// Enabled implements slog.Handler.
func (h *Handler) Enabled(ctx context.Context, l slog.Level) bool { return l >= h.level.Level() }

// Handle implements slog.Handler.
func (h *Handler) Handle(ctx context.Context, rec slog.Record) error {
	if h.ring != nil {
		e := Entry{Time: rec.Time, Level: rec.Level.String(), Msg: rec.Message}
		collect := func(a slog.Attr) {
			if a.Key == "plugin" {
				e.Plugin = a.Value.String()
				return
			}
			if e.Attrs == nil {
				e.Attrs = map[string]any{}
			}
			key := a.Key
			if len(h.groups) > 0 {
				key = strings.Join(h.groups, ".") + "." + key
			}
			e.Attrs[key] = a.Value.Resolve().Any()
			if err, ok := e.Attrs[key].(error); ok {
				e.Attrs[key] = err.Error()
			}
		}
		for _, a := range h.attrs {
			collect(a)
		}
		rec.Attrs(func(a slog.Attr) bool { collect(a); return true })
		h.ring.add(e)
	}
	return h.inner.Handle(ctx, rec)
}

// WithAttrs implements slog.Handler.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	c := *h
	c.inner = h.inner.WithAttrs(attrs)
	c.attrs = append(append([]slog.Attr{}, h.attrs...), attrs...)
	return &c
}

// WithGroup implements slog.Handler.
func (h *Handler) WithGroup(name string) slog.Handler {
	c := *h
	c.inner = h.inner.WithGroup(name)
	c.groups = append(append([]string{}, h.groups...), name)
	return &c
}

// Tee returns a logger that writes to base and additionally calls sink for every record
// (used to persist run logs).
func Tee(base *slog.Logger, sink func(rec slog.Record, attrs []slog.Attr)) *slog.Logger {
	return slog.New(&teeHandler{inner: base.Handler(), sink: sink})
}

type teeHandler struct {
	inner slog.Handler
	sink  func(rec slog.Record, attrs []slog.Attr)
	attrs []slog.Attr
}

func (t *teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	// Run logs keep info and above even when the global level is higher.
	return l >= slog.LevelInfo || t.inner.Enabled(ctx, l)
}

func (t *teeHandler) Handle(ctx context.Context, rec slog.Record) error {
	t.sink(rec, t.attrs)
	if t.inner.Enabled(ctx, rec.Level) {
		return t.inner.Handle(ctx, rec)
	}
	return nil
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{inner: t.inner.WithAttrs(attrs), sink: t.sink, attrs: append(append([]slog.Attr{}, t.attrs...), attrs...)}
}

func (t *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{inner: t.inner.WithGroup(name), sink: t.sink, attrs: t.attrs}
}
