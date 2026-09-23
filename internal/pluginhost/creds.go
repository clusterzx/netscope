package pluginhost

import (
	"context"
	"errors"
	"net/netip"
	"slices"
	"sort"
	"strings"
	"sync"

	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/vault"
)

// CredentialProvider returns a provider for one run or call: it decrypts credentials
// through the vault and matches their scopes against the inventory. Resolved groups,
// filters and devices are cached for the lifetime of the provider.
func (h *Host) CredentialProvider() plugin.CredentialProvider {
	return &credProvider{h: h}
}

// ValidateScope checks and normalizes a scope (plugin or credential scope).
func (h *Host) ValidateScope(ctx context.Context, s *plugin.Scope) error {
	return h.validateScope(ctx, s)
}

type credProvider struct {
	h *Host

	mu         sync.Mutex
	groupNames map[int64]string
	groups     map[int64]map[int64]bool
	queries    map[string]map[int64]bool
	devices    map[int64]*plugin.DeviceInfo
	byIP       map[string]int64
}

// Get implements plugin.CredentialProvider.
func (p *credProvider) Get(ctx context.Context, id int64) (*plugin.Credential, error) {
	if id <= 0 {
		return nil, plugin.ErrNoCredential
	}
	return p.h.Vault.Get(ctx, id)
}

// Applicable implements plugin.CredentialProvider.
func (p *credProvider) Applicable(ctx context.Context, t plugin.CredentialTarget, types []string, allowed []int64) ([]plugin.CredentialMatch, error) {
	list, err := p.h.Vault.CredentialScopes(ctx, types)
	if err != nil {
		return nil, err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	dev, err := p.device(ctx, t)
	if err != nil {
		return nil, err
	}
	var addrs []netip.Addr
	for _, s := range append([]string{t.IP}, deviceIPs(dev)...) {
		if a, err := netip.ParseAddr(s); err == nil {
			addrs = append(addrs, a.Unmap())
		}
	}
	var out []plugin.CredentialMatch
	for _, c := range list {
		if len(allowed) > 0 && !slices.Contains(allowed, c.ID) {
			continue
		}
		// tunnel configurations belong to subnets, not to targets
		if len(types) == 0 && c.Type == plugin.CredWireGuard {
			continue
		}
		rank, reason, err := p.match(ctx, c, dev, addrs)
		if err != nil {
			return nil, err
		}
		if rank > 0 {
			out = append(out, plugin.CredentialMatch{ID: c.ID, Name: c.Name, Type: c.Type, Rank: rank, Reason: reason})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Rank != out[j].Rank {
			return out[i].Rank > out[j].Rank
		}
		return slices.Index(allowed, out[i].ID) < slices.Index(allowed, out[j].ID)
	})
	return out, nil
}

// device finds the inventory device of a target (nil if unknown).
func (p *credProvider) device(ctx context.Context, t plugin.CredentialTarget) (*plugin.DeviceInfo, error) {
	inv := p.h.Inventory
	if inv == nil {
		return nil, nil
	}
	if p.devices == nil {
		p.devices, p.byIP = map[int64]*plugin.DeviceInfo{}, map[string]int64{}
	}
	id := t.DeviceID
	if id == 0 && t.IP != "" {
		cached, ok := p.byIP[t.IP]
		if !ok {
			d, err := inv.DeviceByIP(ctx, t.IP)
			if err != nil && !errors.Is(err, db.ErrNotFound) {
				return nil, err
			}
			if d != nil {
				cached = d.ID
				p.devices[d.ID] = d
			}
			p.byIP[t.IP] = cached
		}
		id = cached
	}
	if id == 0 {
		return nil, nil
	}
	if d, ok := p.devices[id]; ok {
		return d, nil
	}
	d, err := inv.Device(ctx, id)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}
	p.devices[id] = d
	return d, nil
}

func deviceIPs(d *plugin.DeviceInfo) []string {
	if d == nil {
		return nil
	}
	return append([]string{d.PrimaryIP}, d.IPs...)
}

// match returns how specifically a credential scope covers a target (0 = not at all).
// Device selections follow the plugin scope semantics: explicit devices, groups and tags
// add up, a filter narrows them (or selects alone), subnets narrow a device selection.
func (p *credProvider) match(ctx context.Context, c vault.CredentialScope, dev *plugin.DeviceInfo, addrs []netip.Addr) (int, string, error) {
	sc := c.Scope
	subnet := func() (string, bool) {
		for _, cidr := range sc.Subnets {
			pfx, err := plugin.ParsePrefix(cidr)
			if err != nil {
				continue
			}
			for _, a := range addrs {
				if pfx.Contains(a) {
					return pfx.String(), true
				}
			}
		}
		return "", false
	}
	if !sc.DeviceRestricted() {
		if sc.AllSubnets || len(sc.Subnets) == 0 {
			return plugin.RankEverywhere, "überall", nil
		}
		if cidr, ok := subnet(); ok {
			return plugin.RankSubnet, "Subnetz " + cidr, nil
		}
		return 0, "", nil
	}
	if dev == nil {
		return 0, "", nil
	}
	var (
		rank   int
		reason string
	)
	onlyQuery := len(sc.Devices) == 0 && len(sc.Groups) == 0 && len(sc.Tags) == 0
	switch {
	case slices.Contains(sc.Devices, dev.ID):
		rank, reason = plugin.RankDevice, "Gerät zugewiesen"
	case onlyQuery:
		rank, reason = plugin.RankSelection, "Filter"
	default:
		for _, g := range sc.Groups {
			members, err := p.group(ctx, g)
			if err != nil {
				return 0, "", err
			}
			if members[dev.ID] {
				rank, reason = plugin.RankSelection, "Gruppe „"+p.groupNames[g]+"“"
				break
			}
		}
		if rank == 0 {
			for _, tag := range sc.Tags {
				if slices.ContainsFunc(dev.Tags, func(t string) bool { return strings.EqualFold(t, tag) }) {
					rank, reason = plugin.RankSelection, "Tag „"+tag+"“"
					break
				}
			}
		}
	}
	if rank == 0 {
		return 0, "", nil
	}
	if sc.Query != "" {
		ids, err := p.query(ctx, sc.Query)
		if err != nil {
			return 0, "", err
		}
		if !ids[dev.ID] {
			return 0, "", nil
		}
	}
	if len(sc.Subnets) > 0 {
		if _, ok := subnet(); !ok {
			return 0, "", nil
		}
	}
	return rank, reason, nil
}

func (p *credProvider) group(ctx context.Context, id int64) (map[int64]bool, error) {
	if p.groups == nil {
		p.groups, p.groupNames = map[int64]map[int64]bool{}, map[int64]string{}
		list, err := p.h.Inventory.ListGroups(ctx)
		if err != nil {
			return nil, err
		}
		for _, g := range list {
			p.groupNames[g.ID] = g.Name
		}
	}
	if m, ok := p.groups[id]; ok {
		return m, nil
	}
	ids, err := p.h.Inventory.GroupMemberIDs(ctx, id)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return nil, err
	}
	m := toSet(ids)
	p.groups[id] = m
	return m, nil
}

func (p *credProvider) query(ctx context.Context, q string) (map[int64]bool, error) {
	if p.queries == nil {
		p.queries = map[string]map[int64]bool{}
	}
	if m, ok := p.queries[q]; ok {
		return m, nil
	}
	ids, err := p.h.Inventory.MatchingIDs(ctx, q)
	if err != nil {
		// a filter that no longer compiles selects nothing instead of failing every run
		ids = nil
	}
	m := toSet(ids)
	p.queries[q] = m
	return m, nil
}

func toSet(ids []int64) map[int64]bool {
	m := make(map[int64]bool, len(ids))
	for _, id := range ids {
		m[id] = true
	}
	return m
}
