package topology

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/federation/wire"
	"netscope/internal/plugins/snmp"
)

// device is the part of a device the derivation needs.
type device struct {
	id       int64
	hostname string // effective hostname
	display  string // display name
	tags     []string
	macs     int   // number of known MACs
	site     int64 // NetScope site delivering the device (0 = this instance)
}

// switchInv is the SNMP inventory of one device.
type switchInv struct {
	device      int64
	inv         snmp.Inventory
	collectedAt time.Time
}

// subnet is a configured subnet with a gateway.
type subnet struct {
	id      int64
	cidr    string
	gateway string
	site    int64 // 0 = this instance; sites report their subnets in their status
}

// addr is an active address of a device.
type addr struct {
	device int64
	ip     string
	subnet int64
}

// relation is an existing edge.
type relation struct {
	id                    int64
	parent, child         int64
	kind, source          string
	parentPort, childPort string
	lastSeen              time.Time
}

// input is everything read from the database for one rebuild.
type input struct {
	devices     map[int64]*device
	macOwner    map[string]int64 // device_macs
	ipOwner     map[string]int64 // active device_ips (most recently seen owner)
	inSubnet    map[int64][]int64
	subnets     []subnet
	inventories []switchInv
	relations   []relation
	addrs       []addr // all active addresses, oldest sighting first
}

// loadInput reads the inventory state. It only uses the read pool.
func loadInput(ctx context.Context, d *db.DB) (*input, error) {
	in := &input{devices: map[int64]*device{}, macOwner: map[string]int64{}, ipOwner: map[string]int64{}, inSubnet: map[int64][]int64{}}
	q := d.R
	if err := each(ctx, q, "SELECT id, hostname, display_name, IFNULL(site_id, 0) FROM devices", func(r *sql.Rows) error {
		dv := &device{}
		if err := r.Scan(&dv.id, &dv.hostname, &dv.display, &dv.site); err != nil {
			return err
		}
		in.devices[dv.id] = dv
		return nil
	}); err != nil {
		return nil, fmt.Errorf("Geräte: %w", err)
	}
	if err := each(ctx, q, "SELECT device_id, tag FROM device_tags", func(r *sql.Rows) error {
		var (
			id  int64
			tag string
		)
		if err := r.Scan(&id, &tag); err != nil {
			return err
		}
		if dv := in.devices[id]; dv != nil {
			dv.tags = append(dv.tags, tag)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("Tags: %w", err)
	}
	if err := each(ctx, q, "SELECT mac, device_id FROM device_macs", func(r *sql.Rows) error {
		var (
			mac string
			id  int64
		)
		if err := r.Scan(&mac, &id); err != nil {
			return err
		}
		in.macOwner[mac] = id
		if dv := in.devices[id]; dv != nil {
			dv.macs++
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("MACs: %w", err)
	}
	// ordered by last_seen: the most recent owner of an address wins
	if err := each(ctx, q, "SELECT device_id, ip, IFNULL(subnet_id, 0) FROM device_ips WHERE gone_at IS NULL ORDER BY last_seen", func(r *sql.Rows) error {
		var (
			id, sn int64
			ip     string
		)
		if err := r.Scan(&id, &ip, &sn); err != nil {
			return err
		}
		in.ipOwner[ip] = id
		if sn > 0 {
			in.inSubnet[id] = appendUnique(in.inSubnet[id], sn)
		}
		in.addrs = append(in.addrs, addr{device: id, ip: ip, subnet: sn})
		return nil
	}); err != nil {
		return nil, fmt.Errorf("IP-Adressen: %w", err)
	}
	if err := each(ctx, q, "SELECT id, cidr, gateway FROM subnets WHERE enabled = 1", func(r *sql.Rows) error {
		var s subnet
		if err := r.Scan(&s.id, &s.cidr, &s.gateway); err != nil {
			return err
		}
		s.gateway = strings.TrimSpace(s.gateway)
		in.subnets = append(in.subnets, s)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("Subnetze: %w", err)
	}
	// subnets of NetScope sites (central instance), as reported by the sites
	if err := each(ctx, q, "SELECT id, status FROM sites", func(r *sql.Rows) error {
		var (
			site   int64
			status string
		)
		if err := r.Scan(&site, &status); err != nil {
			return err
		}
		var st struct {
			Status *wire.Status `json:"status"`
		}
		if json.Unmarshal([]byte(status), &st) != nil || st.Status == nil {
			return nil
		}
		for i, sn := range st.Status.Subnets {
			if sn.Enabled {
				// ids of site subnets are negative: they never collide with ours
				in.subnets = append(in.subnets, subnet{id: -(site*10000 + int64(i) + 1), cidr: sn.CIDR, gateway: strings.TrimSpace(sn.Gateway), site: site})
			}
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("Standorte: %w", err)
	}
	if err := each(ctx, q, "SELECT device_id, data, collected_at FROM device_inventory WHERE source = 'snmp'", func(r *sql.Rows) error {
		var (
			si   switchInv
			data string
			at   int64
		)
		if err := r.Scan(&si.device, &data, &at); err != nil {
			return err
		}
		if err := json.Unmarshal([]byte(data), &si.inv); err != nil {
			return nil // not an SNMP inventory we understand: ignore the row
		}
		si.collectedAt = db.Time(at)
		in.inventories = append(in.inventories, si)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("SNMP-Inventar: %w", err)
	}
	if err := each(ctx, q, "SELECT id, parent_id, child_id, kind, source, parent_port, child_port, last_seen FROM relations", func(r *sql.Rows) error {
		var (
			rel  relation
			last int64
		)
		if err := r.Scan(&rel.id, &rel.parent, &rel.child, &rel.kind, &rel.source, &rel.parentPort, &rel.childPort, &last); err != nil {
			return err
		}
		rel.lastSeen = db.Time(last)
		in.relations = append(in.relations, rel)
		return nil
	}); err != nil {
		return nil, fmt.Errorf("Beziehungen: %w", err)
	}
	return in, nil
}

func each(ctx context.Context, q db.Querier, query string, fn func(*sql.Rows) error) error {
	rows, err := q.QueryContext(ctx, query)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := fn(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

func appendUnique(list []int64, v int64) []int64 {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

// split separates the input by NetScope site: addresses are only unique within a site
// (the same 192.168.1.1 can be the gateway at two sites), so every site is derived on
// its own. Without sites the input is returned as is.
func (in *input) split() []*input {
	sited := false
	for _, d := range in.devices {
		if d.site != 0 {
			sited = true
			break
		}
	}
	if !sited {
		return []*input{in}
	}
	parts := map[int64]*input{}
	var order []int64
	get := func(site int64) *input {
		p, ok := parts[site]
		if !ok {
			p = &input{devices: map[int64]*device{}, macOwner: map[string]int64{}, ipOwner: map[string]int64{}, inSubnet: map[int64][]int64{}}
			parts[site] = p
			order = append(order, site)
		}
		return p
	}
	get(0)
	for id, d := range in.devices {
		get(d.site).devices[id] = d
	}
	for mac, id := range in.macOwner {
		if d := in.devices[id]; d != nil {
			get(d.site).macOwner[mac] = id
		}
	}
	prefixes := map[int64]netip.Prefix{}
	for _, s := range in.subnets {
		get(s.site).subnets = append(get(s.site).subnets, s)
		if s.site != 0 {
			if pf, err := netip.ParsePrefix(s.cidr); err == nil {
				prefixes[s.id] = pf.Masked()
			}
		}
	}
	for _, a := range in.addrs {
		d := in.devices[a.device]
		if d == nil {
			continue
		}
		p := get(d.site)
		p.ipOwner[a.ip] = a.device
		if d.site == 0 {
			if a.subnet > 0 {
				p.inSubnet[a.device] = appendUnique(p.inSubnet[a.device], a.subnet)
			}
			continue
		}
		ip, err := netip.ParseAddr(a.ip)
		if err != nil {
			continue
		}
		for _, s := range p.subnets {
			if pf, ok := prefixes[s.id]; ok && pf.Contains(ip) {
				p.inSubnet[a.device] = appendUnique(p.inSubnet[a.device], s.id)
			}
		}
	}
	for _, si := range in.inventories {
		if d := in.devices[si.device]; d != nil {
			get(d.site).inventories = append(get(d.site).inventories, si)
		}
	}
	for _, r := range in.relations {
		if d := in.devices[r.parent]; d != nil {
			get(d.site).relations = append(get(d.site).relations, r)
		} else if d := in.devices[r.child]; d != nil {
			get(d.site).relations = append(get(d.site).relations, r)
		}
	}
	out := make([]*input, 0, len(order))
	for _, site := range order {
		out = append(out, parts[site])
	}
	return out
}
