// Package inventory owns the device data model: it applies observations to the state
// tables (temporal rows with first_seen/last_seen/gone_at), derives state changes,
// tracks presence, and serves device queries, merge/split and diffs.
package inventory

import (
	"context"
	"log/slog"
	"net/netip"
	"sort"
	"sync"

	"netscope/internal/bus"
	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/settings"
)

// Store is the inventory service.
type Store struct {
	db       *db.DB
	bus      *bus.Bus
	settings *settings.Store
	log      *slog.Logger

	subMu   sync.RWMutex
	subnets []subnetEntry

	hookMu    sync.RWMutex
	onChanges func([]plugin.Change)
}

type subnetEntry struct {
	id     int64
	prefix netip.Prefix
}

// New creates the store and loads the subnet cache.
func New(ctx context.Context, d *db.DB, b *bus.Bus, st *settings.Store, log *slog.Logger) (*Store, error) {
	s := &Store{db: d, bus: b, settings: st, log: log}
	if err := s.reloadSubnets(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// DB exposes the database (used by the API for read models).
func (s *Store) DB() *db.DB { return s.db }

// OnChanges registers the consumer of derived changes (the plugin host dispatcher).
func (s *Store) OnChanges(fn func([]plugin.Change)) {
	s.hookMu.Lock()
	s.onChanges = fn
	s.hookMu.Unlock()
}

func (s *Store) emit(changes []plugin.Change) {
	if len(changes) == 0 {
		return
	}
	s.hookMu.RLock()
	fn := s.onChanges
	s.hookMu.RUnlock()
	if fn != nil {
		fn(changes)
	}
}

func (s *Store) publishDevice(typ string, id int64) {
	if s.bus != nil && id > 0 {
		s.bus.Publish(bus.TopicDevice, typ, map[string]any{"id": id})
	}
}

// publishMerged announces that a device was merged into another one.
func (s *Store) publishMerged(id, target int64) {
	if s.bus != nil && id > 0 {
		s.bus.Publish(bus.TopicDevice, "deleted", map[string]any{"id": id, "mergedInto": target})
	}
}

func (s *Store) reloadSubnets(ctx context.Context) error {
	rows, err := s.db.R.QueryContext(ctx, "SELECT id, cidr FROM subnets")
	if err != nil {
		return err
	}
	defer rows.Close()
	var list []subnetEntry
	for rows.Next() {
		var (
			id   int64
			cidr string
		)
		if err := rows.Scan(&id, &cidr); err != nil {
			return err
		}
		p, err := netip.ParsePrefix(cidr)
		if err != nil {
			continue
		}
		list = append(list, subnetEntry{id: id, prefix: p})
	}
	// most specific first
	sort.Slice(list, func(i, j int) bool { return list[i].prefix.Bits() > list[j].prefix.Bits() })
	s.subMu.Lock()
	s.subnets = list
	s.subMu.Unlock()
	return rows.Err()
}

// subnetFor returns the id of the most specific configured subnet containing ip.
func (s *Store) subnetFor(ip string) any {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return nil
	}
	s.subMu.RLock()
	defer s.subMu.RUnlock()
	for _, e := range s.subnets {
		if e.prefix.Contains(a) {
			return e.id
		}
	}
	return nil
}
