package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/netutil"
	"netscope/internal/plugin"
	"netscope/internal/timeseries"
)

// ErrNoIdentity is returned for observations without MAC, IP, device id or reference.
var ErrNoIdentity = errors.New("Beobachtung ohne Identität (MAC, IP, Geräte-ID oder Referenz)")

// Observe applies one observation (see plugin.Observation) inside a single transaction
// and returns the device id (0 if the observation did not match and may not create).
func (s *Store) Observe(ctx context.Context, pluginID string, runID int64, obs *plugin.Observation) (int64, error) {
	if obs == nil {
		return 0, errors.New("nil observation")
	}
	o := normalizeObservation(obs)
	if o.DeviceID == 0 && len(o.MACs) == 0 && o.IP == "" && o.Ref == nil {
		return 0, ErrNoIdentity
	}
	var g *ingest
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		g = &ingest{s: s, ctx: ctx, tx: tx, plugin: pluginID, run: runID, now: time.Now(), obs: o}
		return g.process()
	})
	if err != nil {
		return 0, fmt.Errorf("observe %s: %w", describeTarget(o), err)
	}
	if g.devID > 0 {
		if g.created {
			s.publishDevice("created", g.devID)
		} else {
			s.publishDevice("updated", g.devID)
		}
	}
	for _, id := range g.merged {
		s.publishMerged(id, g.devID)
	}
	s.emit(g.changes)
	return g.devID, nil
}

func describeTarget(o *plugin.Observation) string {
	switch {
	case o.Target != "":
		return o.Target
	case o.IP != "":
		return o.IP
	case len(o.MACs) > 0:
		return o.MACs[0]
	case o.Ref != nil:
		return o.Ref.Source + ":" + o.Ref.ID
	}
	return fmt.Sprintf("device %d", o.DeviceID)
}

func normalizeObservation(in *plugin.Observation) *plugin.Observation {
	o := *in
	var macs []string
	seen := map[string]bool{}
	for _, m := range in.MACs {
		if n, ok := netutil.NormalizeMAC(m); ok && !netutil.IsMulticastMAC(n) && !seen[n] {
			seen[n] = true
			macs = append(macs, n)
		}
	}
	o.MACs = macs
	if ip, ok := normalizeHostIP(in.IP); ok {
		o.IP = ip
	} else {
		o.IP = ""
	}
	var ips []string
	seenIP := map[string]bool{o.IP: true}
	for _, raw := range in.IPs {
		if ip, ok := normalizeHostIP(raw); ok && !seenIP[ip] {
			seenIP[ip] = true
			ips = append(ips, ip)
		}
	}
	o.IPs = ips
	o.Hostname = strings.TrimSuffix(strings.TrimSpace(in.Hostname), ".")
	o.Vendor = strings.TrimSpace(in.Vendor)
	o.Model = strings.TrimSpace(in.Model)
	o.DeviceType = strings.TrimSpace(in.DeviceType)
	return &o
}

func normalizeHostIP(s string) (string, bool) {
	ip, ok := netutil.NormalizeIP(s)
	if !ok {
		return "", false
	}
	a := netip.MustParseAddr(ip)
	if a.IsLoopback() || a.IsMulticast() || a.IsLinkLocalUnicast() || a.IsLinkLocalMulticast() {
		return "", false
	}
	return ip, true
}

type deviceRow struct {
	online          bool
	onlineChangedAt sql.NullInt64
	firstSeen       sql.NullInt64
	lastSeen        sql.NullInt64
	state           string
}

type ingest struct {
	s       *Store
	ctx     context.Context
	tx      *sql.Tx
	plugin  string
	run     int64
	now     time.Time
	obs     *plugin.Observation
	devID   int64
	created bool
	dev     deviceRow
	changes []plugin.Change
	merged  []int64
}

func (g *ingest) nowMs() int64 { return g.now.UnixMilli() }

func (g *ingest) runID() any {
	if g.run > 0 {
		return g.run
	}
	return nil
}

func (g *ingest) change(typ plugin.ChangeType, key string, old, nw any, initial bool) {
	g.changes = append(g.changes, plugin.Change{Type: typ, DeviceID: g.devID, PluginID: g.plugin, RunID: g.run,
		At: g.now, Initial: initial, Key: key, Old: old, New: nw})
}

func (g *ingest) exec(q string, args ...any) error {
	_, err := g.tx.ExecContext(g.ctx, q, args...)
	return err
}

func (g *ingest) process() error {
	if err := g.resolve(); err != nil {
		return err
	}
	if g.devID == 0 {
		return nil
	}
	if err := g.tx.QueryRowContext(g.ctx, "SELECT online, online_changed_at, first_seen, last_seen, state FROM devices WHERE id = ?", g.devID).
		Scan(&g.dev.online, &g.dev.onlineChangedAt, &g.dev.firstSeen, &g.dev.lastSeen, &g.dev.state); err != nil {
		return fmt.Errorf("load device %d: %w", g.devID, err)
	}
	steps := []func() error{
		g.applyMACs, g.applyIPs, g.applyPresence, g.applyFirstSeen, g.applyRef, g.applyFacts,
		g.applyPorts, g.applyHTTP, g.applyTLS, g.applyPackages, g.applyContainers,
		g.applyInventory, g.applyMetrics, g.applyRelations, g.applyManual,
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return err
		}
	}
	if err := g.recomputeEffective(); err != nil {
		return err
	}
	if err := updatePrimary(g.ctx, g.tx, g.devID); err != nil {
		return err
	}
	if err := g.storeObservation(); err != nil {
		return err
	}
	if err := g.exec("UPDATE devices SET updated_at = ? WHERE id = ?", g.nowMs(), g.devID); err != nil {
		return err
	}
	if g.created {
		snap, err := snapshot(g.ctx, g.tx, g.devID)
		if err != nil {
			return err
		}
		snap.Source = g.plugin
		// Devices found by the very first successful run of a plugin are the initial
		// inventory, not "new" devices (avoids a notification flood after installation).
		initial := false
		if g.run > 0 {
			var earlier int
			if err := g.tx.QueryRowContext(g.ctx, `SELECT EXISTS (SELECT 1 FROM runs WHERE plugin_id = ? AND status = 'success' AND id < ?)`,
				g.plugin, g.run).Scan(&earlier); err != nil {
				return err
			}
			initial = earlier == 0
		}
		// device.created goes first so processors see the device before its details
		g.changes = append([]plugin.Change{{Type: plugin.ChangeDeviceCreated, DeviceID: g.devID, PluginID: g.plugin,
			RunID: g.run, At: g.now, Initial: initial, New: snap}}, g.changes...)
	}
	return nil
}

func snapshot(ctx context.Context, q db.Querier, id int64) (*plugin.DeviceSnapshot, error) {
	snap := &plugin.DeviceSnapshot{ID: id}
	err := q.QueryRowContext(ctx, "SELECT primary_ip, primary_mac, hostname, vendor, state FROM devices WHERE id = ?", id).
		Scan(&snap.IP, &snap.MAC, &snap.Hostname, &snap.Vendor, &snap.State)
	return snap, err
}

// resolve finds or creates the device: DeviceID > MAC > external ref > IP.
func (g *ingest) resolve() error {
	o := g.obs
	ctx, tx := g.ctx, g.tx
	if o.DeviceID > 0 {
		var id int64
		if err := tx.QueryRowContext(ctx, "SELECT id FROM devices WHERE id = ?", o.DeviceID).Scan(&id); err != nil {
			return fmt.Errorf("Gerät %d: %w", o.DeviceID, db.NotFound(err))
		}
		g.devID = id
		return nil
	}
	for _, mac := range o.MACs {
		var id int64
		err := tx.QueryRowContext(ctx, "SELECT device_id FROM device_macs WHERE mac = ?", mac).Scan(&id)
		if err == nil {
			g.devID = id
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if o.Ref != nil && o.Ref.Source != "" && o.Ref.ID != "" {
		var id int64
		err := tx.QueryRowContext(ctx, "SELECT device_id FROM external_refs WHERE source = ? AND ref = ?", o.Ref.Source, o.Ref.ID).Scan(&id)
		if err == nil {
			g.devID = id
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	}
	if o.IP != "" {
		owner, ownerMACs, err := ipOwner(ctx, tx, o.IP)
		if err != nil {
			return err
		}
		// Without a MAC the IP is the identity. A device known only by IP learns its MAC.
		if owner > 0 && (len(o.MACs) == 0 || ownerMACs == 0) {
			g.devID = owner
			return nil
		}
		// owner has another MAC: this is a different device on a known IP (handled in applyIPs)
	}
	if !(o.Present || o.Create) {
		return nil
	}
	first := g.nowMs()
	if o.FirstSeen != nil && !o.FirstSeen.IsZero() && o.FirstSeen.Before(g.now) {
		first = o.FirstSeen.UnixMilli()
	}
	var lastSeen any
	online := 0
	if o.Present {
		lastSeen, online = g.nowMs(), 1
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO devices(created_source, first_seen, last_seen, online, online_changed_at, created_at, updated_at)
		VALUES (?,?,?,?,?,?,?)`, g.plugin, first, lastSeen, online, g.nowMs(), g.nowMs(), g.nowMs())
	if err != nil {
		return err
	}
	g.devID, _ = res.LastInsertId()
	g.created = true
	return nil
}

// ipOwner returns the device currently holding ip and how many MACs it has.
func ipOwner(ctx context.Context, q db.Querier, ip string) (int64, int, error) {
	var id int64
	err := q.QueryRowContext(ctx, `SELECT device_id FROM device_ips WHERE ip = ? AND gone_at IS NULL
		ORDER BY last_seen DESC LIMIT 1`, ip).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}
	var n int
	err = q.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_macs WHERE device_id = ?", id).Scan(&n)
	return id, n, err
}

func (g *ingest) applyMACs() error {
	for _, mac := range g.obs.MACs {
		var owner int64
		err := g.tx.QueryRowContext(g.ctx, "SELECT device_id FROM device_macs WHERE mac = ?", mac).Scan(&owner)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			vendor := ""
			if len(g.obs.MACs) == 1 {
				vendor = g.obs.Vendor
			}
			if err := g.exec(`INSERT INTO device_macs(mac, device_id, vendor, randomized, source, first_seen, last_seen)
				VALUES (?,?,?,?,?,?,?)`, mac, g.devID, vendor, db.Bool(netutil.IsRandomizedMAC(mac)), g.plugin, g.nowMs(), g.nowMs()); err != nil {
				return err
			}
			if !g.created {
				g.change(plugin.ChangeMACAdded, mac, nil, mac, false)
			}
		case err != nil:
			return err
		case owner == g.devID:
			if g.obs.Present || g.obs.Create {
				if err := g.exec("UPDATE device_macs SET last_seen = ? WHERE mac = ?", g.nowMs(), mac); err != nil {
					return err
				}
			}
			if g.obs.Vendor != "" && len(g.obs.MACs) == 1 {
				if err := g.exec("UPDATE device_macs SET vendor = ? WHERE mac = ? AND vendor = ''", g.obs.Vendor, mac); err != nil {
					return err
				}
			}
		default:
			// The MAC belongs to another device (e.g. a VM reporting a bridge MAC). Never
			// steal MACs silently; merging is a manual decision.
			g.s.log.Debug("mac belongs to another device", "mac", mac, "device", owner, "observed_device", g.devID, "plugin", g.plugin)
		}
	}
	return nil
}

func (g *ingest) applyIPs() error {
	o := g.obs
	ips := o.IPs
	if o.IP != "" {
		ips = append([]string{o.IP}, ips...)
	}
	mac := ""
	if len(o.MACs) > 0 {
		mac = o.MACs[0]
	}
	for _, ip := range ips {
		observedAt := ip == o.IP
		// Is the IP held by another device?
		rows, err := g.tx.QueryContext(g.ctx, `SELECT id, device_id, mac FROM device_ips WHERE ip = ? AND gone_at IS NULL AND device_id <> ?`, ip, g.devID)
		if err != nil {
			return err
		}
		type other struct {
			rowID, dev int64
			mac        string
		}
		var others []other
		for rows.Next() {
			var ot other
			if err := rows.Scan(&ot.rowID, &ot.dev, &ot.mac); err != nil {
				rows.Close()
				return err
			}
			others = append(others, ot)
		}
		rows.Close()
		if len(others) > 0 && !(o.Present && observedAt) {
			// Only live observations may move an address between devices.
			g.s.log.Debug("ip held by another device, not assigned", "ip", ip, "device", g.devID, "plugin", g.plugin)
			continue
		}
		for _, ot := range others {
			var macCount int
			if err := g.tx.QueryRowContext(g.ctx, "SELECT COUNT(*) FROM device_macs WHERE device_id = ?", ot.dev).Scan(&macCount); err != nil {
				return err
			}
			if macCount == 0 && mac != "" {
				merge, err := isMergeableShadow(g.ctx, g.tx, ot.dev)
				if err != nil {
					return err
				}
				if merge {
					// A device known only by this IP is the same host we now see with a MAC.
					if err := g.s.mergeInto(g.ctx, g.tx, g.devID, []int64{ot.dev}, g.nowMs()); err != nil {
						return err
					}
					g.merged = append(g.merged, ot.dev)
					continue
				}
			}
			if err := g.exec("UPDATE device_ips SET gone_at = ? WHERE id = ?", g.nowMs(), ot.rowID); err != nil {
				return err
			}
			if macCount > 0 && mac != "" {
				oldMAC := ot.mac
				if oldMAC == "" {
					_ = g.tx.QueryRowContext(g.ctx, "SELECT primary_mac FROM devices WHERE id = ?", ot.dev).Scan(&oldMAC)
				}
				if oldMAC != mac {
					g.change(plugin.ChangeMACChanged, ip, nil, &plugin.MACChange{IP: ip, OldMAC: oldMAC, NewMAC: mac, OldDeviceID: ot.dev}, false)
				}
			}
			if err := updatePrimary(g.ctx, g.tx, ot.dev); err != nil {
				return err
			}
		}
		var rowID int64
		err = g.tx.QueryRowContext(g.ctx, "SELECT id FROM device_ips WHERE device_id = ? AND ip = ? AND gone_at IS NULL", g.devID, ip).Scan(&rowID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			rowMAC := ""
			if observedAt {
				rowMAC = mac
			}
			var runID any
			if o.Present && observedAt {
				runID = g.runID()
			}
			if err := g.exec(`INSERT INTO device_ips(device_id, ip, ip_key, mac, subnet_id, source, first_seen, last_seen, last_run_id)
				VALUES (?,?,?,?,?,?,?,?,?)`, g.devID, ip, netutil.IPKey(ip), rowMAC, g.s.subnetFor(ip), g.plugin, g.nowMs(), g.nowMs(), runID); err != nil {
				return err
			}
			if !g.created {
				g.change(plugin.ChangeIPAdded, ip, nil, ip, false)
			}
		case err != nil:
			return err
		default:
			set := "last_seen = ?, source = ?"
			args := []any{g.nowMs(), g.plugin}
			if observedAt && mac != "" {
				set += ", mac = ?"
				args = append(args, mac)
			}
			if o.Present && observedAt && g.run > 0 {
				set += ", last_run_id = ?"
				args = append(args, g.run)
			}
			args = append(args, rowID)
			if err := g.exec("UPDATE device_ips SET "+set+" WHERE id = ?", args...); err != nil {
				return err
			}
		}
	}
	return nil
}

// isMergeableShadow reports whether a device carries no manual data and no MAC, so it
// can be merged automatically into the device that now answers on its IP.
func isMergeableShadow(ctx context.Context, q db.Querier, id int64) (bool, error) {
	var n int
	err := q.QueryRowContext(ctx, `SELECT COUNT(*) FROM devices d WHERE d.id = ?
		AND d.display_name = '' AND d.notes = '' AND d.location = '' AND d.owner = '' AND d.state = 'unknown'
		AND d.custom IN ('', '{}')
		AND NOT EXISTS (SELECT 1 FROM device_tags t WHERE t.device_id = d.id)
		AND NOT EXISTS (SELECT 1 FROM group_members m WHERE m.device_id = d.id)
		AND NOT EXISTS (SELECT 1 FROM device_facts f WHERE f.device_id = d.id AND f.source = 'manual' AND f.gone_at IS NULL)
		AND NOT EXISTS (SELECT 1 FROM external_refs r WHERE r.device_id = d.id)`, id).Scan(&n)
	return n == 1, err
}

func (g *ingest) applyPresence() error {
	if !g.obs.Present {
		return nil
	}
	if err := g.exec(`UPDATE devices SET last_seen = ?, online = 1,
		online_changed_at = CASE WHEN online = 0 THEN ? ELSE online_changed_at END WHERE id = ?`, g.nowMs(), g.nowMs(), g.devID); err != nil {
		return err
	}
	if !g.created && !g.dev.online && g.dev.lastSeen.Valid {
		since := db.NullTime(g.dev.onlineChangedAt)
		g.change(plugin.ChangeDeviceOnline, "", since, nil, false)
	}
	return g.exec(`INSERT INTO device_presence(device_id, plugin_id, last_seen, last_run_id, missed) VALUES (?,?,?,?,0)
		ON CONFLICT(device_id, plugin_id) DO UPDATE SET last_seen = excluded.last_seen, last_run_id = excluded.last_run_id, missed = 0`,
		g.devID, g.plugin, g.nowMs(), g.runID())
}

func (g *ingest) applyFirstSeen() error {
	fs := g.obs.FirstSeen
	if fs == nil || fs.IsZero() || fs.After(g.now) {
		return nil
	}
	return g.exec("UPDATE devices SET first_seen = ? WHERE id = ? AND (first_seen IS NULL OR first_seen > ?)",
		fs.UnixMilli(), g.devID, fs.UnixMilli())
}

func (g *ingest) applyRef() error {
	r := g.obs.Ref
	if r == nil || r.Source == "" || r.ID == "" {
		return nil
	}
	data := "{}"
	if r.Data != nil {
		b, err := json.Marshal(r.Data)
		if err != nil {
			return err
		}
		data = string(b)
	}
	return g.exec(`INSERT INTO external_refs(source, ref, device_id, data, first_seen, last_seen) VALUES (?,?,?,?,?,?)
		ON CONFLICT(source, ref) DO UPDATE SET device_id = excluded.device_id, data = excluded.data, last_seen = excluded.last_seen`,
		r.Source, r.ID, g.devID, data, g.nowMs(), g.nowMs())
}

func (g *ingest) applyInventory() error {
	if g.obs.Inventory == nil {
		return nil
	}
	b, err := json.Marshal(g.obs.Inventory)
	if err != nil {
		return err
	}
	return g.exec(`INSERT INTO device_inventory(device_id, source, data, run_id, collected_at) VALUES (?,?,?,?,?)
		ON CONFLICT(device_id, source) DO UPDATE SET data = excluded.data, run_id = excluded.run_id, collected_at = excluded.collected_at`,
		g.devID, g.plugin, string(b), g.runID(), g.nowMs())
}

func (g *ingest) applyMetrics() error {
	for _, m := range g.obs.Metrics {
		if m.Name == "" {
			continue
		}
		id, err := timeseries.SeriesID(g.ctx, g.tx, m.Name, g.devID, m.Key, m.Unit)
		if err != nil {
			return err
		}
		if err := timeseries.Append(g.ctx, g.tx, id, g.now, m.Min, m.Avg, m.Max); err != nil {
			return err
		}
	}
	return nil
}

func (g *ingest) storeObservation() error {
	b, err := json.Marshal(g.obs)
	if err != nil {
		return err
	}
	var raw any
	if g.obs.Raw != "" {
		limit := g.s.settings.System().ObservationRawMaxKB * 1024
		r := g.obs.Raw
		if limit > 0 && len(r) > limit {
			r = r[:limit] + "\n… (gekürzt)"
		}
		if limit > 0 {
			raw = r
		}
	}
	return g.exec(`INSERT INTO observations(run_id, plugin_id, device_id, target, ts, data, raw) VALUES (?,?,?,?,?,?,?)`,
		g.runID(), g.plugin, g.devID, describeTarget(g.obs), g.nowMs(), string(b), raw)
}

// updatePrimary recomputes primary IP (IPv4 in a known subnet, most recently seen) and MAC.
func updatePrimary(ctx context.Context, tx *sql.Tx, id int64) error {
	var ip, mac string
	err := tx.QueryRowContext(ctx, `SELECT ip, mac FROM device_ips WHERE device_id = ? AND gone_at IS NULL
		ORDER BY (subnet_id IS NULL), (instr(ip, ':') > 0), last_seen DESC LIMIT 1`, id).Scan(&ip, &mac)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	if mac == "" {
		err = tx.QueryRowContext(ctx, "SELECT mac FROM device_macs WHERE device_id = ? ORDER BY last_seen DESC LIMIT 1", id).Scan(&mac)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return err
		}
	} else {
		// the MAC must still belong to this device
		var n int
		_ = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM device_macs WHERE device_id = ? AND mac = ?", id, mac).Scan(&n)
		if n == 0 {
			mac = ""
			_ = tx.QueryRowContext(ctx, "SELECT mac FROM device_macs WHERE device_id = ? ORDER BY last_seen DESC LIMIT 1", id).Scan(&mac)
		}
	}
	key := []byte{}
	if ip != "" {
		key = netutil.IPKey(ip)
	}
	_, err = tx.ExecContext(ctx, "UPDATE devices SET primary_ip = ?, ip_key = ?, primary_mac = ? WHERE id = ?", ip, key, mac, id)
	return err
}
