// Package plugintest provides fakes for unit-testing plugins without the core.
package plugintest

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"testing"

	"netscope/internal/plugin"
)

// Sink records observations.
type Sink struct {
	mu           sync.Mutex
	Observations []plugin.Observation
	// DeviceIDs maps observation index to the returned fake device id.
	next int64
}

// Observe implements plugin.Sink.
func (s *Sink) Observe(ctx context.Context, o *plugin.Observation) (int64, error) {
	if o == nil {
		return 0, errors.New("nil observation")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Observations = append(s.Observations, *o)
	s.next++
	return s.next, nil
}

// All returns a copy of the recorded observations.
func (s *Sink) All() []plugin.Observation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]plugin.Observation(nil), s.Observations...)
}

// Events records emitted events.
type Events struct {
	mu     sync.Mutex
	Events []plugin.Event
}

// Emit implements plugin.EventEmitter.
func (e *Events) Emit(ctx context.Context, ev plugin.Event) (int64, error) {
	if _, ok := plugin.LookupEvent(ev.Type); !ok {
		return 0, errors.New("unknown event type " + ev.Type)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.Events = append(e.Events, ev)
	return int64(len(e.Events)), nil
}

// Creds is an in-memory credential provider.
type Creds map[int64]*plugin.Credential

// Get implements plugin.CredentialProvider.
func (c Creds) Get(ctx context.Context, id int64) (*plugin.Credential, error) {
	if cr, ok := c[id]; ok {
		return cr, nil
	}
	return nil, plugin.ErrNoCredential
}

// Applicable implements plugin.CredentialProvider: every credential applies everywhere,
// ordered by the allowed list (or by id).
func (c Creds) Applicable(ctx context.Context, t plugin.CredentialTarget, types []string, allowed []int64) ([]plugin.CredentialMatch, error) {
	var out []plugin.CredentialMatch
	for id, cr := range c {
		if len(types) > 0 && !slices.Contains(types, cr.Type) {
			continue
		}
		if len(allowed) > 0 && !slices.Contains(allowed, id) {
			continue
		}
		out = append(out, plugin.CredentialMatch{ID: id, Name: cr.Name, Type: cr.Type, Rank: plugin.RankEverywhere, Reason: "überall"})
	}
	sort.Slice(out, func(i, j int) bool {
		if len(allowed) > 0 {
			return slices.Index(allowed, out[i].ID) < slices.Index(allowed, out[j].ID)
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

// Inventory is an in-memory inventory reader.
type Inventory struct {
	List        []plugin.DeviceInfo
	SubnetsList []plugin.SubnetTarget
}

// Device implements plugin.InventoryReader.
func (i *Inventory) Device(ctx context.Context, id int64) (*plugin.DeviceInfo, error) {
	for _, d := range i.List {
		if d.ID == id {
			dd := d
			return &dd, nil
		}
	}
	return nil, errors.New("not found")
}

// Devices implements plugin.InventoryReader (the query is ignored).
func (i *Inventory) Devices(ctx context.Context, query string) ([]plugin.DeviceInfo, error) {
	return append([]plugin.DeviceInfo(nil), i.List...), nil
}

// DeviceByMAC implements plugin.InventoryReader.
func (i *Inventory) DeviceByMAC(ctx context.Context, mac string) (*plugin.DeviceInfo, error) {
	for _, d := range i.List {
		for _, m := range d.MACs {
			if m == mac {
				dd := d
				return &dd, nil
			}
		}
	}
	return nil, errors.New("not found")
}

// DeviceByIP implements plugin.InventoryReader.
func (i *Inventory) DeviceByIP(ctx context.Context, ip string) (*plugin.DeviceInfo, error) {
	for _, d := range i.List {
		for _, a := range append([]string{d.PrimaryIP}, d.IPs...) {
			if a == ip {
				dd := d
				return &dd, nil
			}
		}
	}
	return nil, errors.New("not found")
}

// Subnets implements plugin.InventoryReader.
func (i *Inventory) Subnets(ctx context.Context) ([]plugin.SubnetTarget, error) {
	return append([]plugin.SubnetTarget(nil), i.SubnetsList...), nil
}

// RunContext builds a RunContext for p with the given settings (validated against the
// schema; the test fails on validation errors).
func RunContext(t testing.TB, p plugin.Plugin, settings map[string]any) (*plugin.RunContext, *Sink, *Events) {
	t.Helper()
	vals, err := p.Schema().Validate(settings, nil, nil)
	if err != nil {
		t.Fatalf("settings: %v", err)
	}
	sink, evs := &Sink{}, &Events{}
	info := p.Info()
	rc := &plugin.RunContext{
		RunID:       1,
		PluginID:    info.ID,
		Trigger:     "manual",
		Settings:    plugin.NewSettings(vals),
		Scope:       plugin.DefaultScope(),
		Params:      map[string]any{},
		Concurrency: max(info.DefaultConcurrency, 1),
		Log:         slog.New(slog.NewTextHandler(testWriter{t}, &slog.HandlerOptions{Level: slog.LevelDebug})).With("plugin", info.ID),
		Sink:        sink,
		Events:      evs,
		Creds:       Creds{},
		Inventory:   &Inventory{},
		DataDir:     t.TempDir(),
	}
	return rc, sink, evs
}

type testWriter struct{ t testing.TB }

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(string(p))
	return len(p), nil
}

// Fixture reads a file below testdata/.
func Fixture(t testing.TB, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("fixture %s: %v", name, err)
	}
	return b
}
