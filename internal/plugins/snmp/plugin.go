// Package snmp is the SNMP scanner (v2c/v3 with vault credentials). It reads the system
// group, interfaces, the ARP table, the bridge forwarding table and LLDP neighbours of
// known devices and stores them as Inventory (device_inventory, source "snmp") for the
// topology processor.
package snmp

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// Plugin implements the snmp scanner.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:   "snmp",
		Kind: plugin.KindScanner,
		Name: "SNMP",
		Description: "Fragt Geräte per SNMP v2c/v3 ab: Systemdaten, Interfaces, ARP-Tabelle, Bridge-FDB (MAC → Port) " +
			"und LLDP-Nachbarn als Grundlage für die Topologie.",
		Version:            "1.0.0",
		DefaultEnabled:     false,
		DefaultSchedule:    "*/30 * * * *",
		DefaultTimeout:     20 * time.Minute,
		DefaultConcurrency: 16,
		DefaultRetries:     1,
		Targets:            plugin.TargetDevices,
		Presence:           false,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "credentials", Type: plugin.FieldCredentialRef, Label: "Zugangsdaten", Multi: true, Required: true,
			CredentialTypes: []string{plugin.CredSNMPv2c, plugin.CredSNMPv3}, Group: "Verbindung",
			Description: "Community (v2c) oder USM-Benutzer (v3). Werden der Reihe nach probiert; das funktionierende Credential wird pro Gerät gemerkt."},
		{Key: "port", Type: plugin.FieldInt, Label: "UDP-Port", Default: 161, Group: "Verbindung",
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(65535)}},
		{Key: "timeout", Type: plugin.FieldDuration, Label: "Zeitlimit pro Anfrage", Default: "3s", Group: "Verbindung",
			Validation: &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(60)}},
		{Key: "retries", Type: plugin.FieldInt, Label: "Wiederholungen", Default: 1, Group: "Verbindung",
			Description: "Wiederholungen je Anfrage, wenn keine Antwort kommt.",
			Validation:  &plugin.Validation{Min: plugin.Int64(0), Max: plugin.Int64(10)}},
		{Key: "max_repetitions", Type: plugin.FieldInt, Label: "GETBULK max-repetitions", Default: 25, Group: "Verbindung", Advanced: true,
			Description: "Einträge pro GETBULK-Anfrage. Kleinere Werte helfen bei langsamen oder fehlerhaften Agenten.",
			Validation:  &plugin.Validation{Min: plugin.Int64(1), Max: plugin.Int64(100)}},
		{Key: "walk_interfaces", Type: plugin.FieldBool, Label: "Interfaces abfragen", Default: true, Group: "Umfang",
			Description: "IF-MIB: Name, Beschreibung, Alias, Typ, MAC, Geschwindigkeit, Status."},
		{Key: "walk_arp", Type: plugin.FieldBool, Label: "ARP-Tabelle abfragen", Default: true, Group: "Umfang"},
		{Key: "walk_fdb", Type: plugin.FieldBool, Label: "Bridge-FDB abfragen", Default: true, Group: "Umfang",
			Description: "MAC-Adresstabelle der Switches (BRIDGE-MIB / Q-BRIDGE-MIB) – Grundlage für „Gerät → Switch-Port“."},
		{Key: "walk_lldp", Type: plugin.FieldBool, Label: "LLDP-Nachbarn abfragen", Default: true, Group: "Umfang"},
		{Key: "import_arp_neighbors", Type: plugin.FieldBool, Label: "MACs aus ARP-Tabellen übernehmen", Default: true, Group: "Umfang",
			Description: "Bekannte Geräte, die nur per IP bekannt sind, lernen ihre MAC aus der ARP-Tabelle (nur Adressen in konfigurierten Subnetzen; es werden keine Geräte angelegt).",
			VisibleIf:   &plugin.Condition{Field: "walk_arp", Equals: []any{true}}},
	}}
}

type config struct {
	credIDs   []int64
	client    clientConfig
	collect   collectOptions
	importARP bool
	storeFile string
}

func loadConfig(s plugin.Settings, dataDir string) config {
	c := config{
		credIDs: s.CredentialIDs("credentials"),
		client: clientConfig{
			port:           s.Int("port"),
			timeout:        s.Duration("timeout"),
			retries:        s.Int("retries"),
			maxRepetitions: s.Int("max_repetitions"),
		},
		collect: collectOptions{
			interfaces: s.Bool("walk_interfaces"),
			arp:        s.Bool("walk_arp"),
			fdb:        s.Bool("walk_fdb"),
			lldp:       s.Bool("walk_lldp"),
		},
		storeFile: filepath.Join(dataDir, "credentials.json"),
	}
	c.importARP = s.Bool("import_arp_neighbors") && c.collect.arp
	if c.client.port < 1 || c.client.port > 65535 {
		c.client.port = 161
	}
	if c.client.timeout <= 0 {
		c.client.timeout = 3 * time.Second
	}
	if c.client.retries < 0 {
		c.client.retries = 0
	}
	if c.client.maxRepetitions < 1 {
		c.client.maxRepetitions = 25
	}
	return c
}

type target struct {
	dev plugin.DeviceInfo
	ip  string
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	cfg := loadConfig(rc.Settings, rc.DataDir)
	if len(cfg.credIDs) == 0 {
		return plugin.ErrNoCredential
	}
	var creds []*plugin.Credential
	for _, id := range cfg.credIDs {
		c, err := rc.Creds.Get(ctx, id)
		if err != nil {
			rc.Log.Warn("Credential nicht verfügbar", "credential_id", id, "error", err)
			continue
		}
		if _, err := newClient(ctx, "127.0.0.1", c, cfg.client); err != nil {
			rc.Log.Warn("Credential unbrauchbar", "credential", c.Name, "error", err)
			continue
		}
		creds = append(creds, c)
	}
	if len(creds) == 0 {
		return errors.New("keines der konfigurierten Credentials ist verwendbar")
	}
	var subnets []netip.Prefix
	if rc.Inventory != nil {
		if list, err := rc.Inventory.Subnets(ctx); err != nil {
			rc.Log.Warn("Subnetze nicht lesbar – ARP-Nachbarn werden nicht übernommen", "error", err)
		} else {
			for _, sn := range list {
				subnets = append(subnets, sn.CIDR)
			}
		}
	}
	store, err := loadCredStore(cfg.storeFile)
	if err != nil {
		rc.Log.Warn("gemerkte Credentials nicht lesbar – beginne neu", "error", err)
	}
	var targets []target
	for _, d := range rc.Targets.Devices {
		ip := d.PrimaryIP
		if ip == "" && len(d.IPs) > 0 {
			ip = d.IPs[0]
		}
		if ip != "" {
			targets = append(targets, target{dev: d, ip: ip})
		}
	}
	rc.SetStat("targets", len(targets))
	var (
		ok, failed atomic.Int64
		mu         sync.Mutex
		done       int
	)
	rc.Progress(0, len(targets))
	runErr := plugin.ForEach(ctx, rc.Parallelism(), targets, func(ctx context.Context, t target) error {
		err := p.scan(ctx, rc, cfg, creds, store, subnets, t)
		switch {
		case err == nil:
			ok.Add(1)
			rc.AddStat("answered", 1)
		case ctx.Err() != nil:
		default:
			failed.Add(1)
			rc.AddStat("no_answer", 1)
			rc.Log.Debug("keine SNMP-Antwort", "device", t.dev.Name, "ip", t.ip, "error", err)
		}
		mu.Lock()
		done++
		rc.Progress(done, len(targets))
		mu.Unlock()
		return nil
	})
	if err := store.save(); err != nil {
		rc.Log.Warn("gemerkte Credentials nicht speicherbar", "error", err)
	}
	if runErr != nil {
		return runErr
	}
	rc.Log.Info("SNMP-Abfrage abgeschlossen", "answered", ok.Load(), "no_answer", failed.Load())
	if ok.Load() == 0 && failed.Load() > 0 {
		return fmt.Errorf("kein Gerät hat per SNMP geantwortet (%d ohne Antwort)", failed.Load())
	}
	return nil
}

// scan queries one device.
func (p *Plugin) scan(ctx context.Context, rc *plugin.RunContext, cfg config, creds []*plugin.Credential, store *credStore,
	subnets []netip.Prefix, t target) error {
	ordered := orderedCredentials(store.get(t.dev.ID), creds, func(c *plugin.Credential) int64 { return c.ID })
	var errs []string
	for _, c := range ordered {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		g, err := newClient(ctx, t.ip, c, cfg.client)
		if err != nil {
			errs = append(errs, err.Error())
			continue
		}
		system, err := querySystem(g)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", c.Name, err))
			continue
		}
		store.set(t.dev.ID, c.ID)
		inv := collect(g, system, cfg.collect)
		g.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}
		for table, msg := range inv.Errors {
			rc.Log.Warn("SNMP-Tabelle nicht lesbar", "device", t.dev.Name, "ip", t.ip, "table", table, "error", msg)
		}
		if _, err := rc.Sink.Observe(ctx, deviceObservation(t.dev.ID, t.ip, inv)); err != nil {
			return fmt.Errorf("Beobachtung speichern: %w", err)
		}
		if cfg.importARP {
			n := 0
			for _, o := range neighborObservations(inv, subnets) {
				if _, err := rc.Sink.Observe(ctx, o); err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					rc.Log.Debug("ARP-Nachbar nicht übernommen", "ip", o.IP, "mac", o.MACs[0], "error", err)
					continue
				}
				n++
			}
			rc.AddStat("arp_neighbors", n)
		}
		return nil
	}
	if store.get(t.dev.ID) != 0 {
		store.set(t.dev.ID, 0)
	}
	return errors.New(strings.Join(errs, "; "))
}

// deviceObservation builds the observation of the queried device.
func deviceObservation(devID int64, ip string, inv Inventory) *plugin.Observation {
	o := &plugin.Observation{
		DeviceID:  devID,
		IP:        ip,
		Present:   true,
		Inventory: inv,
		Attrs: map[string]string{
			"snmp.sysDescr":    inv.System.Descr,
			"snmp.sysObjectID": inv.System.ObjectID,
			"snmp.sysLocation": inv.System.Location,
			"snmp.sysContact":  inv.System.Contact,
		},
	}
	if name := strings.TrimSpace(inv.System.Name); name != "" && !strings.EqualFold(name, "localhost") {
		o.Hostname = name
	}
	if v, ok := lookupVendor(inv.System.Enterprise); ok && !v.Agent {
		o.Vendor = v.Name
	}
	return o
}

// neighborObservations turns ARP entries into MAC/IP observations (not present, never
// creating devices) so that devices known only by IP learn their MAC. Only addresses
// inside the configured subnets are used; the device's own interface MACs are skipped.
func neighborObservations(inv Inventory, subnets []netip.Prefix) []*plugin.Observation {
	if len(subnets) == 0 {
		return nil
	}
	own := map[string]bool{}
	for _, i := range inv.Interfaces {
		if i.MAC != "" {
			own[i.MAC] = true
		}
	}
	seen := map[string]bool{}
	var out []*plugin.Observation
	for _, e := range inv.ARP {
		if e.Type == "invalid" || own[e.MAC] || netutil.IsMulticastMAC(e.MAC) || seen[e.IP] {
			continue
		}
		a, err := netip.ParseAddr(e.IP)
		if err != nil {
			continue
		}
		in := false
		for _, p := range subnets {
			if p.Contains(a.Unmap()) {
				in = true
				break
			}
		}
		if !in {
			continue
		}
		seen[e.IP] = true
		out = append(out, &plugin.Observation{MACs: []string{e.MAC}, IP: e.IP, Target: e.IP, Present: false, Create: false})
	}
	return out
}
