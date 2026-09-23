package topology

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugins/snmp"
)

// device is the part of a device the derivation needs.
type device struct {
	id       int64
	hostname string // effective hostname
	display  string // display name
	tags     []string
	macs     int // number of known MACs
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
}

// loadInput reads the inventory state. It only uses the read pool.
func loadInput(ctx context.Context, d *db.DB) (*input, error) {
	in := &input{devices: map[int64]*device{}, macOwner: map[string]int64{}, ipOwner: map[string]int64{}, inSubnet: map[int64][]int64{}}
	q := d.R
	if err := each(ctx, q, "SELECT id, hostname, display_name FROM devices", func(r *sql.Rows) error {
		dv := &device{}
		if err := r.Scan(&dv.id, &dv.hostname, &dv.display); err != nil {
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
