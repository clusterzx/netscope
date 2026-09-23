package inventory

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/netip"
	"sort"
	"strings"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
)

// Scalar fact kinds whose effective value is stored on the device row.
const (
	FactHostname = "hostname"
	FactVendor   = "vendor"
	FactModel    = "model"
	FactType     = "type"
	FactOS       = "os"
	// SourceManual marks values entered by the user.
	SourceManual = "manual"
)

var (
	// hardware vendor from the MAC first: UPnP/mDNS may report the vendor of an app
	vendorPriority = []string{SourceManual, "snmp", "oui", "arpscan", "nmap", "upnp", "proxmox"}
	modelPriority  = []string{SourceManual, "upnp", "snmp", "proxmox", "mdns", "ssh"}
	typePriority   = []string{SourceManual, "proxmox", "upnp", "snmp", "mdns", "http", "ssh", "nmap", "oui"}
	osPriority     = []string{SourceManual, "ssh", "snmp", "proxmox", "nmap", "http"}
)

// setFact stores the value of a scalar fact for one source (temporal: a changed value
// closes the old row). An empty value closes the fact. It reports whether anything changed.
func setFact(ctx context.Context, tx *sql.Tx, devID int64, kind, source, value, extra string, runID any, now int64) (bool, error) {
	var (
		id       int64
		curValue string
	)
	err := tx.QueryRowContext(ctx, "SELECT id, value FROM device_facts WHERE device_id = ? AND kind = ? AND source = ? AND gone_at IS NULL",
		devID, kind, source).Scan(&id, &curValue)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return false, err
	}
	exists := err == nil
	if extra == "" {
		extra = "{}"
	}
	switch {
	case value == "" && exists:
		_, err := tx.ExecContext(ctx, "UPDATE device_facts SET gone_at = ? WHERE id = ?", now, id)
		return true, err
	case value == "":
		return false, nil
	case exists && curValue == value:
		_, err := tx.ExecContext(ctx, "UPDATE device_facts SET last_seen = ?, extra = ?, run_id = COALESCE(?, run_id) WHERE id = ?", now, extra, runID, id)
		return false, err
	case exists:
		if _, err := tx.ExecContext(ctx, "UPDATE device_facts SET gone_at = ? WHERE id = ?", now, id); err != nil {
			return false, err
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO device_facts(device_id, kind, source, value, extra, run_id, first_seen, last_seen)
		VALUES (?,?,?,?,?,?,?,?)`, devID, kind, source, value, extra, runID, now, now)
	return true, err
}

func (g *ingest) applyFacts() error {
	o := g.obs
	set := func(kind, value, extra string) error {
		if value == "" {
			return nil
		}
		_, err := setFact(g.ctx, g.tx, g.devID, kind, g.plugin, value, extra, g.runID(), g.nowMs())
		return err
	}
	if err := set(FactHostname, o.Hostname, ""); err != nil {
		return err
	}
	if err := set(FactVendor, o.Vendor, ""); err != nil {
		return err
	}
	if err := set(FactModel, o.Model, ""); err != nil {
		return err
	}
	if err := set(FactType, o.DeviceType, ""); err != nil {
		return err
	}
	if o.OS != nil && strings.TrimSpace(o.OS.Name) != "" {
		b, _ := json.Marshal(o.OS)
		if err := set(FactOS, strings.TrimSpace(o.OS.Name), string(b)); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(o.Attrs))
	for k := range o.Attrs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if _, err := setFact(g.ctx, g.tx, g.devID, "attr:"+k, g.plugin, strings.TrimSpace(o.Attrs[k]), "", g.runID(), g.nowMs()); err != nil {
			return err
		}
	}
	for _, kind := range o.Clear {
		switch kind {
		case FactHostname, FactVendor, FactModel, FactType, FactOS:
			if _, err := setFact(g.ctx, g.tx, g.devID, kind, g.plugin, "", "", nil, g.nowMs()); err != nil {
				return err
			}
		}
	}
	return nil
}

type factRow struct {
	kind, source, value, extra string
	lastSeen                   int64
}

func rank(priority []string, source string) int {
	for i, p := range priority {
		if p == source {
			return i
		}
	}
	return len(priority)
}

func pickFact(rows []factRow, priority []string) (factRow, bool) {
	if len(rows) == 0 {
		return factRow{}, false
	}
	sort.SliceStable(rows, func(i, j int) bool {
		ri, rj := rank(priority, rows[i].source), rank(priority, rows[j].source)
		if ri != rj {
			return ri < rj
		}
		if rows[i].kind == FactOS && rows[i].source == rows[j].source {
			return osAccuracy(rows[i].extra) > osAccuracy(rows[j].extra)
		}
		return rows[i].lastSeen > rows[j].lastSeen
	})
	return rows[0], true
}

func osAccuracy(extra string) int {
	var o plugin.OSInfo
	_ = json.Unmarshal([]byte(extra), &o)
	return o.Accuracy
}

// effectiveValues computes the effective scalar values of a device from its facts.
func (s *Store) effectiveValues(ctx context.Context, q db.Querier, devID int64) (map[string]factRow, error) {
	rows, err := q.QueryContext(ctx, `SELECT kind, source, value, extra, last_seen FROM device_facts
		WHERE device_id = ? AND gone_at IS NULL AND kind IN ('hostname','vendor','model','type','os')`, devID)
	if err != nil {
		return nil, err
	}
	byKind := map[string][]factRow{}
	for rows.Next() {
		var f factRow
		if err := rows.Scan(&f.kind, &f.source, &f.value, &f.extra, &f.lastSeen); err != nil {
			rows.Close()
			return nil, err
		}
		byKind[f.kind] = append(byKind[f.kind], f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	hostPrio := s.settings.System().HostnamePriority
	if len(hostPrio) == 0 || hostPrio[0] != SourceManual {
		hostPrio = append([]string{SourceManual}, hostPrio...)
	}
	prios := map[string][]string{FactHostname: hostPrio, FactVendor: vendorPriority, FactModel: modelPriority,
		FactType: typePriority, FactOS: osPriority}
	out := map[string]factRow{}
	for kind, prio := range prios {
		if f, ok := pickFact(byKind[kind], prio); ok {
			out[kind] = f
		}
	}
	return out, nil
}

// sameHostname reports whether two hostnames name the same host: case-insensitive and
// ignoring a local domain suffix ("iPhone.lan" from DNS and "iPhone" from DHCP). IP
// addresses are compared as a whole.
func sameHostname(a, b string) bool {
	if strings.EqualFold(a, b) {
		return true
	}
	short := func(h string) string {
		h = strings.TrimSuffix(h, ".")
		if _, err := netip.ParseAddr(h); err == nil {
			return h
		}
		name, _, _ := strings.Cut(h, ".")
		return name
	}
	return strings.EqualFold(short(a), short(b))
}

// recomputeEffective updates the effective columns and records hostname/os changes.
func (g *ingest) recomputeEffective() error {
	var oldOSSource string
	if err := g.tx.QueryRowContext(g.ctx, "SELECT os_source FROM devices WHERE id = ?", g.devID).Scan(&oldOSSource); err != nil {
		return err
	}
	oldHost, oldOS, err := g.s.applyEffective(g.ctx, g.tx, g.devID)
	if err != nil {
		return err
	}
	var newHost, newOS, newOSSource string
	if err := g.tx.QueryRowContext(g.ctx, "SELECT hostname, os, os_source FROM devices WHERE id = ?", g.devID).
		Scan(&newHost, &newOS, &newOSSource); err != nil {
		return err
	}
	if !g.created && oldHost != "" && newHost != "" && !sameHostname(oldHost, newHost) {
		g.change(plugin.ChangeHostname, "", oldHost, newHost, false)
	}
	// Only a changed report of the same source is an OS change (e.g. an upgrade seen by
	// SSH). A better source taking over ("Linux 4.15 - 5.19" guessed by nmap → "Proxmox
	// VE 9.2" from the API) changes the data quality, not the device.
	if !g.created && oldOS != "" && newOS != "" && oldOS != newOS && oldOSSource == newOSSource {
		g.change(plugin.ChangeOS, "", oldOS, newOS, false)
	}
	return nil
}

// applyEffective writes the effective values to the device row and returns the previous
// hostname and OS.
func (s *Store) applyEffective(ctx context.Context, tx *sql.Tx, devID int64) (oldHost, oldOS string, err error) {
	if err := tx.QueryRowContext(ctx, "SELECT hostname, os FROM devices WHERE id = ?", devID).Scan(&oldHost, &oldOS); err != nil {
		return "", "", err
	}
	eff, err := s.effectiveValues(ctx, tx, devID)
	if err != nil {
		return "", "", err
	}
	_, err = tx.ExecContext(ctx, `UPDATE devices SET hostname = ?, hostname_source = ?, vendor = ?, model = ?, type = ?, os = ?, os_source = ?
		WHERE id = ?`, eff[FactHostname].value, eff[FactHostname].source, eff[FactVendor].value, eff[FactModel].value,
		eff[FactType].value, eff[FactOS].value, eff[FactOS].source, devID)
	return oldHost, oldOS, err
}

// SetManualFact stores (or clears with "") a manual override for hostname, vendor,
// model, type or os and refreshes the effective values.
func (s *Store) setManualFact(ctx context.Context, tx *sql.Tx, devID int64, kind, value string) error {
	if _, err := setFact(ctx, tx, devID, kind, SourceManual, strings.TrimSpace(value), "", nil, time.Now().UnixMilli()); err != nil {
		return err
	}
	_, _, err := s.applyEffective(ctx, tx, devID)
	return err
}

// RecomputeAll refreshes the effective values of all devices (after the hostname priority
// setting changed).
func (s *Store) RecomputeAll(ctx context.Context) error {
	rows, err := s.db.R.QueryContext(ctx, "SELECT id FROM devices")
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	return s.db.Tx(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, _, err := s.applyEffective(ctx, tx, id); err != nil {
				return err
			}
		}
		return nil
	})
}
