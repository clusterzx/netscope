package rack

import (
	"context"
	"database/sql"
	"strings"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// rank orders device types by their place in the network: the upstream side of a rack
// connection becomes the parent of the relation.
func rank(d *DeviceRef) int {
	if d == nil {
		return 0
	}
	switch d.Type {
	case "router", "firewall":
		return 5
	case "switch":
		return 4
	case "access-point":
		return 3
	case "hypervisor", "server", "nas":
		return 2
	}
	return 1
}

type edge struct {
	parent, child         int64
	parentPort, childPort []string
}

func addPort(list []string, p string) []string {
	if p == "" {
		return list
	}
	for _, x := range list {
		if x == p {
			return list
		}
	}
	return append(list, p)
}

// links computes the relations the racks describe: for every port of a mounted device the
// device plugged in or reached by cable.
func (st *state) links() map[[2]int64]*edge {
	out := map[[2]int64]*edge{}
	for _, it := range st.items {
		if it.Kind != KindDevice || it.DeviceID == 0 || st.devices[it.DeviceID] == nil {
			continue
		}
		self := st.devices[it.DeviceID]
		for _, p := range st.portNames(it) {
			other, otherPort := st.peer(it.ID, p.Name)
			if other == nil || other.ID == self.ID {
				continue
			}
			parent, child, pp, cp := self, other, p.Name, otherPort
			if rank(other) > rank(self) || (rank(other) == rank(self) && other.ID < self.ID && otherPort != "") {
				parent, child, pp, cp = other, self, otherPort, p.Name
			}
			k := [2]int64{parent.ID, child.ID}
			e := out[k]
			if e == nil {
				// the same pair seen from the other side (cable between two devices)
				if rev := out[[2]int64{child.ID, parent.ID}]; rev != nil {
					continue
				}
				e = &edge{parent: parent.ID, child: child.ID}
				out[k] = e
			}
			e.parentPort, e.childPort = addPort(e.parentPort, pp), addPort(e.childPort, cp)
		}
	}
	return out
}

// Sync writes the connections of the racks as relations (source "rack"): new ones are
// added, changed ports updated and those no longer in the racks removed.
func (s *Service) Sync(ctx context.Context) error {
	st, err := s.load(ctx)
	if err != nil {
		return err
	}
	want := st.links()
	changed := map[int64]bool{}
	err = s.db.Tx(ctx, func(tx *sql.Tx) error {
		rows, err := tx.QueryContext(ctx, "SELECT id, parent_id, child_id, kind, parent_port, child_port FROM relations WHERE source = ?", Source)
		if err != nil {
			return err
		}
		type have struct {
			id                 int64
			parent, child      int64
			kind, ppGot, cpGot string
		}
		var existing []have
		for rows.Next() {
			var h have
			if err := rows.Scan(&h.id, &h.parent, &h.child, &h.kind, &h.ppGot, &h.cpGot); err != nil {
				rows.Close()
				return err
			}
			existing = append(existing, h)
		}
		rows.Close()
		now := db.Now()
		done := map[[2]int64]bool{}
		for _, h := range existing {
			e := want[[2]int64{h.parent, h.child}]
			if e == nil || h.kind != plugin.RelSwitchPort || done[[2]int64{h.parent, h.child}] {
				if _, err := tx.ExecContext(ctx, "DELETE FROM relations WHERE id = ?", h.id); err != nil {
					return err
				}
				changed[h.parent], changed[h.child] = true, true
				continue
			}
			done[[2]int64{h.parent, h.child}] = true
			pp, cp := strings.Join(e.parentPort, ", "), strings.Join(e.childPort, ", ")
			if pp != h.ppGot || cp != h.cpGot {
				if _, err := tx.ExecContext(ctx, "UPDATE relations SET parent_port = ?, child_port = ?, last_seen = ? WHERE id = ?", pp, cp, now, h.id); err != nil {
					return err
				}
				changed[h.parent], changed[h.child] = true, true
			}
		}
		for k, e := range want {
			if done[k] {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO relations(parent_id, child_id, kind, source, parent_port, child_port, label, protected, first_seen, last_seen)
				VALUES (?,?,?,?,?,?,'',1,?,?) ON CONFLICT(parent_id, child_id, kind, source) DO UPDATE SET
				parent_port = excluded.parent_port, child_port = excluded.child_port, last_seen = excluded.last_seen`,
				e.parent, e.child, plugin.RelSwitchPort, Source, strings.Join(e.parentPort, ", "), strings.Join(e.childPort, ", "), now, now); err != nil {
				return err
			}
			changed[e.parent], changed[e.child] = true, true
		}
		return nil
	})
	if err != nil {
		return err
	}
	s.publish(changed)
	return nil
}
