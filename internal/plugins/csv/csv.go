// Package csv imports devices from a CSV file with a header row (English or German column
// names). The inventory export of the core (reports API) uses the same columns, so an
// export can be imported again.
package csv

import (
	"context"
	stdcsv "encoding/csv"
	"errors"
	"fmt"
	"io"
	"net/netip"
	"os"
	"strings"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

func init() { plugin.Register(&Plugin{}) }

// maxFileSize caps the imported file.
const maxFileSize = 64 << 20

// Plugin is the generic CSV importer.
type Plugin struct{}

// Info implements plugin.Plugin.
func (p *Plugin) Info() plugin.Info {
	return plugin.Info{
		ID:          "csv",
		Kind:        plugin.KindImporter,
		Name:        "CSV-Import",
		Description: "Importiert Geräte aus einer CSV-Datei mit Kopfzeile (deutsche oder englische Spaltennamen). Der Inventar-Export unter Berichte liefert dasselbe Format und lässt sich so wieder einlesen.",
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
		{Key: "file", Type: plugin.FieldString, Widget: "file", Label: "CSV-Datei", Required: true,
			Description: "Erste Zeile = Spaltennamen, jede Zeile braucht eine MAC- oder IP-Adresse. Spalten: mac, macs, ip, ips, name (Anzeigename), hostname, " +
				"type/typ, vendor/hersteller, model, os, location/aufstellort/standort, owner/besitzer, tags (getrennt durch ; oder |), state/zustand " +
				"(bekannt, unbekannt, ignoriert), criticality/kritikalität (niedrig, normal, hoch, kritisch), notes/notizen sowie cf.<schlüssel> " +
				"oder custom:<schlüssel> für eigene Felder. Unbekannte Spalten (z. B. id, online, first_seen) werden ignoriert.",
			Validation: &plugin.Validation{Format: "path"}},
		{Key: "delimiter", Type: plugin.FieldEnum, Label: "Trennzeichen", Default: "auto", Options: []plugin.Option{
			{Value: "auto", Label: "Automatisch erkennen"},
			{Value: "comma", Label: "Komma (,)"},
			{Value: "semicolon", Label: "Semikolon (;)"},
			{Value: "tab", Label: "Tabulator"},
		}},
		{Key: "overwrite", Type: plugin.FieldBool, Label: "Vorhandene Angaben überschreiben", Default: false,
			Description: "Aus: Anzeigename, Typ, Aufstellort, Besitzer, Notizen, Zustand, Kritikalität und eigene Felder werden nur gefüllt, wenn sie leer sind."},
		{Key: "create_missing", Type: plugin.FieldBool, Label: "Fehlende Geräte anlegen", Default: true,
			Description: "Aus: Zeilen ohne passendes Gerät im Inventar werden übersprungen."},
	}}
}

// Run implements plugin.Runner.
func (p *Plugin) Run(ctx context.Context, rc *plugin.RunContext) error {
	s := rc.Settings
	path := strings.TrimSpace(s.String("file"))
	if path == "" {
		return errors.New("keine Datei angegeben")
	}
	data, err := readFile(path)
	if err != nil {
		return fmt.Errorf("Datei nicht lesbar: %w", err)
	}
	text, latin := decodeText(data)
	if latin {
		rc.Log.Info("Datei ist nicht UTF-8-kodiert, wird als Windows-1252 gelesen")
	}
	delim := sniffDelimiter(text)
	switch s.String("delimiter") {
	case "comma":
		delim = ','
	case "semicolon":
		delim = ';'
	case "tab":
		delim = '\t'
	}
	r := stdcsv.NewReader(strings.NewReader(text))
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.ReuseRecord = false

	header, err := r.Read()
	if err != nil {
		if errors.Is(err, io.EOF) {
			return errors.New("die Datei ist leer")
		}
		return fmt.Errorf("Kopfzeile nicht lesbar: %w", err)
	}
	cols, bad := mapHeader(header)
	if !usable(cols) {
		return fmt.Errorf("keine verwendbare Kopfzeile: Spalte mac/macs oder ip/ips fehlt (gelesen: %s, Trennzeichen %q)",
			strings.Join(header, ", "), string(delim))
	}
	if len(bad) > 0 {
		rc.Log.Warn("Eigene Felder mit ungültigem Schlüssel werden ignoriert (erlaubt: a-z, 0-9, _)", "spalten", strings.Join(bad, ", "))
	}
	var unknown []string
	for _, c := range cols {
		if c.Field == "" && c.Custom == "" && c.Header != "" {
			unknown = append(unknown, c.Header)
		}
	}
	if len(unknown) > 0 {
		rc.Log.Info("Unbekannte Spalten werden ignoriert", "spalten", strings.Join(unknown, ", "))
	}

	type record struct {
		line   int
		fields []string
	}
	var records []record
	lastLine := 0
	for {
		fields, err := r.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		line, _ := r.FieldPos(0)
		if err != nil {
			var pe *stdcsv.ParseError
			if errors.As(err, &pe) && pe.StartLine > lastLine {
				lastLine = pe.StartLine
				rc.AddStat("errors", 1)
				rc.Log.Warn("Zeile nicht lesbar", "zeile", pe.StartLine, "error", pe.Err)
				continue
			}
			return fmt.Errorf("CSV nicht lesbar: %w", err)
		}
		lastLine = line
		records = append(records, record{line: line, fields: fields})
	}

	im := &importer{rc: rc, cols: cols, overwrite: s.Bool("overwrite"), create: s.Bool("create_missing")}
	for _, k := range []string{"imported", "updated", "skipped"} {
		rc.SetStat(k, 0)
	}
	rc.SetStat("rows", len(records))
	rc.Progress(0, len(records))
	for i, rec := range records {
		if err := ctx.Err(); err != nil {
			return err
		}
		im.importRow(ctx, rec.line, rec.fields)
		rc.Progress(i+1, len(records))
	}
	st := rc.Stats()
	rc.Log.Info("CSV-Import abgeschlossen", "zeilen", len(records), "neu", st["imported"], "aktualisiert", st["updated"],
		"uebersprungen", st["skipped"], "fehler", st["errors"])
	return nil
}

func readFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFileSize {
		return nil, fmt.Errorf("Datei größer als %d MB", maxFileSize>>20)
	}
	return data, nil
}

type importer struct {
	rc        *plugin.RunContext
	cols      []column
	overwrite bool
	create    bool
}

// rowError logs a problem of one row (1-based line numbers of the file).
func (im *importer) rowError(line int, msg string, args ...any) {
	im.rc.AddStat("errors", 1)
	im.rc.Log.Warn(fmt.Sprintf("Zeile %d: %s", line, msg), append([]any{"zeile", line}, args...)...)
}

// importRow maps one record to an observation and writes it.
func (im *importer) importRow(ctx context.Context, line int, fields []string) {
	vals := map[string]string{}
	custom := map[string]any{}
	for i, c := range im.cols {
		if i >= len(fields) {
			break
		}
		v := strings.TrimSpace(fields[i])
		if v == "" {
			continue
		}
		switch {
		case c.Custom != "":
			custom[c.Custom] = customValue(v)
		case c.Field != "" && c.Field != colIgnored:
			if vals[c.Field] == "" {
				vals[c.Field] = v
			}
		}
	}

	var macs []string
	seenMAC := map[string]bool{}
	for _, raw := range append(splitList(vals[colMAC], true), splitList(vals[colMACs], true)...) {
		mac, ok := netutil.NormalizeMAC(raw)
		if !ok {
			im.rowError(line, "ungültige MAC-Adresse wird ignoriert", "wert", raw)
			continue
		}
		if !seenMAC[mac] {
			seenMAC[mac] = true
			macs = append(macs, mac)
		}
	}
	var ips []string
	seenIP := map[string]bool{}
	for _, raw := range append(splitList(vals[colIP], true), splitList(vals[colIPs], true)...) {
		a, err := netip.ParseAddr(raw)
		if err != nil {
			im.rowError(line, "ungültige IP-Adresse wird ignoriert", "wert", raw)
			continue
		}
		ip := a.Unmap().String()
		if !seenIP[ip] {
			seenIP[ip] = true
			ips = append(ips, ip)
		}
	}
	if len(macs) == 0 && len(ips) == 0 {
		im.rowError(line, "weder gültige MAC- noch IP-Adresse, Zeile übersprungen")
		im.rc.AddStat("skipped", 1)
		return
	}

	m := &plugin.ManualData{
		Overwrite:   im.overwrite,
		DisplayName: vals[colName],
		Type:        normalizeType(vals[colType]),
		Location:    vals[colLocation],
		Owner:       vals[colOwner],
		Notes:       vals[colNotes],
	}
	if v := vals[colState]; v != "" {
		if st, ok := stateValues[strings.ToLower(v)]; ok {
			m.State = st
		} else {
			im.rowError(line, "unbekannter Zustand wird ignoriert (bekannt, unbekannt, ignoriert)", "wert", v)
		}
	}
	if v := vals[colCriticality]; v != "" {
		if c, ok := criticalityValues[strings.ToLower(v)]; ok {
			m.Criticality = c
		} else {
			im.rowError(line, "unbekannte Kritikalität wird ignoriert (niedrig, normal, hoch, kritisch)", "wert", v)
		}
	}
	for _, t := range splitList(vals[colTags], false) {
		if t = strings.TrimSpace(t); t != "" {
			m.Tags = append(m.Tags, t)
		}
	}
	if len(custom) > 0 {
		m.Custom = custom
	}
	obs := &plugin.Observation{
		MACs:     macs,
		Target:   fmt.Sprintf("Zeile %d", line),
		Create:   im.create,
		Hostname: vals[colHostname],
		Vendor:   vals[colVendor],
		Model:    vals[colModel],
		Manual:   m,
	}
	if len(ips) > 0 {
		obs.IP = ips[0]
	}
	if len(ips) > 1 {
		obs.IPs = ips[1:]
	}
	if os := vals[colOS]; os != "" {
		obs.OS = &plugin.OSInfo{Name: os}
	}

	existed := im.exists(ctx, macs, ips)
	id, err := im.rc.Sink.Observe(ctx, obs)
	if err != nil {
		im.rowError(line, "Gerät konnte nicht gespeichert werden", "error", err)
		return
	}
	switch {
	case id == 0:
		im.rc.AddStat("skipped", 1)
		im.rc.Log.Info(fmt.Sprintf("Zeile %d: kein passendes Gerät im Inventar, übersprungen", line), "zeile", line)
	case existed:
		im.rc.AddStat("updated", 1)
	default:
		im.rc.AddStat("imported", 1)
	}
}

// exists reports whether the row matches a known device (for the statistics).
func (im *importer) exists(ctx context.Context, macs, ips []string) bool {
	inv := im.rc.Inventory
	if inv == nil {
		return false
	}
	for _, mac := range macs {
		if d, err := inv.DeviceByMAC(ctx, mac); err == nil && d != nil {
			return true
		}
	}
	if len(ips) > 0 {
		if d, err := inv.DeviceByIP(ctx, ips[0]); err == nil && d != nil && (len(macs) == 0 || len(d.MACs) == 0) {
			return true
		}
	}
	return false
}

// customValue converts "true"/"false" (the export of bool fields) to bool; other values
// stay text (number, date and URL fields accept text).
func customValue(v string) any {
	switch strings.ToLower(v) {
	case "true":
		return true
	case "false":
		return false
	}
	return v
}
