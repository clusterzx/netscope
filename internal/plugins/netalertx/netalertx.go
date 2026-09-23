// Package netalertx imports an existing NetAlertX (or Pi.Alert) inventory once: names,
// types, owners, locations, groups, first sightings and network parents from the
// app.db database or the CSV export of the CSV Backup plugin.
package netalertx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// refSource is the external reference source; the id is the NetAlertX devMac.
const refSource = "netalertx"

// Plugin is the NetAlertX importer.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:          "netalertx",
		Kind:        plugin.KindImporter,
		Name:        "NetAlertX-Import",
		Description: "Übernimmt einmalig den Bestand aus NetAlertX oder Pi.Alert (app.db oder CSV-Export): Namen, Typen, Besitzer, Standorte, Gruppen, Erstsichtung und Netzwerk-Eltern.",
		Version:     "1.0.0",

		DefaultEnabled:     true,
		DefaultSchedule:    "",
		DefaultTimeout:     10 * time.Minute,
		DefaultConcurrency: 1,
		DefaultRetries:     0,
		Targets:            plugin.TargetNone,
	}
}

// Schema implements plugin.Plugin.
func (p *Plugin) Schema() plugin.Schema {
	return plugin.Schema{Fields: []plugin.Field{
		{Key: "file", Type: plugin.FieldString, Widget: "file", Label: "Datei", Required: true,
			Description: "NetAlertX-Datenbank (app.db, auch ältere Pi.Alert-Datenbanken) oder CSV-Export (devices.csv aus Wartung → Backup/Wiederherstellung → CSV-Export).",
			Validation:  &plugin.Validation{Format: "path"}},
		{Key: "format", Type: plugin.FieldEnum, Label: "Format", Default: "auto", Options: []plugin.Option{
			{Value: "auto", Label: "Automatisch erkennen"},
			{Value: "sqlite", Label: "SQLite-Datenbank (app.db)"},
			{Value: "csv", Label: "CSV-Export"},
		}},
		{Key: "overwrite", Type: plugin.FieldBool, Label: "Vorhandene Angaben überschreiben", Default: false,
			Description: "Aus: nur leere Felder (Anzeigename, Typ, Besitzer, Standort, Notizen, Zustand) werden gefüllt."},
		{Key: "import_first_seen", Type: plugin.FieldBool, Label: "Erstsichtung übernehmen", Default: true,
			Description: "Die frühere Erstsichtung aus NetAlertX ersetzt eine spätere in NetScope."},
		{Key: "mark_known", Type: plugin.FieldBool, Label: "Bestätigte Geräte als bekannt markieren", Default: true,
			Description: "Geräte, die NetAlertX nicht mehr als neu führt, erhalten den Zustand „bekannt“."},
		{Key: "archived_as_ignored", Type: plugin.FieldBool, Label: "Archivierte Geräte ignorieren", Default: true,
			Description: "In NetAlertX archivierte Geräte erhalten den Zustand „ignoriert“."},
	}}
}

type importer struct {
	rc              *plugin.RunContext
	overwrite       bool
	firstSeen       bool
	markKnown       bool
	archivedIgnored bool
	loc             *time.Location
	schema          string

	done, total int
}

// imported remembers a device written in the first pass.
type imported struct {
	id    identity
	devID int64
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	s := rc.Settings
	path := strings.TrimSpace(s.String("file"))
	if path == "" {
		return errors.New("keine Datei angegeben")
	}
	if _, err := os.Stat(path); err != nil {
		return fmt.Errorf("Datei nicht lesbar: %w", err)
	}
	format := s.String("format")
	if format == "" || format == "auto" {
		f, err := detectFormat(path)
		if err != nil {
			return fmt.Errorf("Datei nicht lesbar: %w", err)
		}
		format = f
	}
	var (
		ds  *dataset
		err error
	)
	if format == "sqlite" {
		ds, err = readSQLite(ctx, path)
	} else {
		ds, err = readCSV(path)
	}
	if err != nil {
		return fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	loc := rc.Env.Location
	if loc == nil {
		loc = time.Local
	}
	if ds.UTC {
		loc = time.UTC
	}
	rc.Log.Info("NetAlertX-Export gelesen", "datei", filepath.Base(path), "format", ds.Format, "schema", ds.Schema,
		"geraete", len(ds.Devices), "zeitstempel_utc", ds.UTC)

	im := &importer{rc: rc, overwrite: s.Bool("overwrite"), firstSeen: s.Bool("import_first_seen"),
		markKnown: s.Bool("mark_known"), archivedIgnored: s.Bool("archived_as_ignored"), loc: loc, schema: ds.Schema}
	for _, k := range []string{"imported", "updated", "skipped", "relations", "errors"} {
		rc.SetStat(k, 0)
	}
	im.total = len(ds.Devices)
	for _, d := range ds.Devices {
		if pid, ok := identify(d.get("parent_mac")); ok && pid.Key != "" {
			im.total++
		}
	}
	rc.Progress(0, im.total)

	known, err := im.importDevices(ctx, ds.Devices)
	if err != nil {
		return err
	}
	if err := im.importRelations(ctx, ds.Devices, known); err != nil {
		return err
	}
	rc.Progress(im.total, im.total)
	st := rc.Stats()
	rc.Log.Info("NetAlertX-Import abgeschlossen", "neu", st["imported"], "aktualisiert", st["updated"],
		"uebersprungen", st["skipped"], "beziehungen", st["relations"], "fehler", st["errors"])
	if len(ds.Devices) > 0 && len(known) == 0 && st["errors"] != 0 {
		return errors.New("kein Gerät konnte importiert werden")
	}
	return nil
}

func (im *importer) step() {
	im.done++
	im.rc.Progress(im.done, im.total)
}

// exists reports whether NetScope already knows the device (for the statistics).
func (im *importer) exists(ctx context.Context, id identity, ip string) bool {
	inv := im.rc.Inventory
	if inv == nil {
		return false
	}
	if id.MAC != "" {
		if d, err := inv.DeviceByMAC(ctx, id.MAC); err == nil && d != nil {
			return true
		}
	}
	if ip != "" {
		// without MAC match the core only merges into a device that has no MAC yet
		if d, err := inv.DeviceByIP(ctx, ip); err == nil && d != nil && (id.MAC == "" || len(d.MACs) == 0) {
			return true
		}
	}
	return false
}

// importDevices is the first pass: one observation per device.
func (im *importer) importDevices(ctx context.Context, devices []device) (map[string]imported, error) {
	rc := im.rc
	known := map[string]imported{}
	for _, d := range devices {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		id, ok := identify(d.get("mac"))
		if !ok {
			rc.AddStat("skipped", 1)
			im.step()
			continue
		}
		ip := deviceIP(d)
		if id.MAC == "" && ip == "" {
			rc.AddStat("skipped", 1)
			rc.Log.Warn("Gerät ohne gültige MAC- und IP-Adresse übersprungen", "zeile", d.Line, "mac", d.get("mac"), "name", d.get("name"))
			im.step()
			continue
		}
		existed := im.exists(ctx, id, ip)
		obs := im.observation(d, id, ip)
		devID, err := rc.Sink.Observe(ctx, obs)
		im.step()
		if err != nil {
			rc.AddStat("errors", 1)
			rc.Log.Warn("Gerät konnte nicht importiert werden", "zeile", d.Line, "mac", id.Key, "error", err)
			continue
		}
		if devID == 0 {
			rc.AddStat("skipped", 1)
			continue
		}
		if existed {
			rc.AddStat("updated", 1)
		} else {
			rc.AddStat("imported", 1)
		}
		known[id.Key] = imported{id: id, devID: devID}
	}
	return known, nil
}

// observation maps a NetAlertX device to an observation.
func (im *importer) observation(d device, id identity, ip string) *plugin.Observation {
	m := &plugin.ManualData{
		Overwrite:   im.overwrite,
		DisplayName: cleanName(d.get("name")),
		Type:        mapType(d.get("type")),
		Owner:       cleanText(d.get("owner")),
		Location:    cleanText(d.get("location")),
		Notes:       strings.TrimSpace(d.get("comments")),
	}
	if g := cleanText(d.get("group")); g != "" {
		m.Tags = append(m.Tags, g)
	}
	if d.flag("favorite") {
		m.Tags = append(m.Tags, "favorite")
	}
	_, hasNew := d.Fields["new"]
	switch {
	case im.archivedIgnored && d.flag("archived"):
		m.State = "ignored"
	case im.markKnown && hasNew && !d.flag("new"):
		m.State = "known"
	}
	target := m.DisplayName
	if target == "" {
		target = ip
	}
	if target == "" {
		target = id.Key
	}
	inv := map[string]string{"schema": im.schema}
	for k := range d.Fields {
		if v := d.get(k); v != "" {
			inv[k] = v
		}
	}
	obs := &plugin.Observation{
		IP:        ip,
		Target:    target,
		Create:    true,
		Ref:       &plugin.ExternalRef{Source: refSource, ID: id.Key},
		Vendor:    cleanText(d.get("vendor")),
		Hostname:  cleanText(d.get("fqdn")),
		Inventory: inv,
		Manual:    m,
	}
	if id.MAC != "" {
		obs.MACs = []string{id.MAC}
	}
	if t := strings.TrimSpace(d.get("type")); t != "" {
		obs.Attrs = map[string]string{"netalertx.type": t}
	}
	if im.firstSeen {
		if t, ok := parseTime(d.get("first_connection"), im.loc); ok && t.Year() >= 2000 {
			obs.FirstSeen = &t
		}
	}
	return obs
}

// importRelations is the second pass: parent links of devices whose parent exists.
func (im *importer) importRelations(ctx context.Context, devices []device, known map[string]imported) error {
	rc := im.rc
	types := map[string]string{}
	for _, d := range devices {
		if id, ok := identify(d.get("mac")); ok {
			types[id.Key] = d.get("type")
		}
	}
	for _, d := range devices {
		if err := ctx.Err(); err != nil {
			return err
		}
		pid, ok := identify(d.get("parent_mac"))
		if !ok || pid.Key == "" {
			continue
		}
		im.step()
		id, ok := identify(d.get("mac"))
		if !ok {
			continue
		}
		child, ok := known[id.Key]
		if !ok || pid.Key == id.Key {
			continue
		}
		relType := d.get("parent_rel_type")
		if strings.EqualFold(relType, "nic") {
			rc.Log.Debug("NIC-Verknüpfung nicht übernommen (Gerät ist Netzwerkkarte des Elterngeräts)", "mac", id.Key, "parent", pid.Key)
			continue
		}
		other := plugin.DeviceRef{MAC: pid.MAC}
		if _, imported := known[pid.Key]; !imported {
			if pid.MAC == "" || !im.macKnown(ctx, pid.MAC) {
				rc.AddStat("relations_skipped", 1)
				rc.Log.Debug("Elterngerät unbekannt, Beziehung übersprungen", "mac", id.Key, "parent", pid.Key)
				continue
			}
		} else if pid.MAC == "" {
			other = plugin.DeviceRef{Ref: &plugin.ExternalRef{Source: refSource, ID: pid.Key}}
		}
		port := cleanPort(d.get("parent_port"))
		rel := plugin.Relation{Kind: relationKind(types[pid.Key], relType, port), Other: other, RemotePort: port}
		if rel.Kind == plugin.RelWireless {
			rel.Label = d.get("ssid")
		}
		obs := &plugin.Observation{DeviceID: child.devID, Target: id.Key, Relations: []plugin.Relation{rel}}
		if _, err := rc.Sink.Observe(ctx, obs); err != nil {
			rc.AddStat("errors", 1)
			rc.Log.Warn("Beziehung konnte nicht gespeichert werden", "zeile", d.Line, "mac", id.Key, "parent", pid.Key, "error", err)
			continue
		}
		rc.AddStat("relations", 1)
	}
	return nil
}

func (im *importer) macKnown(ctx context.Context, mac string) bool {
	if im.rc.Inventory == nil {
		return false
	}
	d, err := im.rc.Inventory.DeviceByMAC(ctx, mac)
	return err == nil && d != nil
}
