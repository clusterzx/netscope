package csv

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"netscope/internal/inventory"
	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/reports"
)

func fixture(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// runCSV runs the importer and captures its log.
func runCSV(t *testing.T, settings map[string]any, inv *plugintest.Inventory, sink plugin.Sink) ([]plugin.Observation, *plugin.RunContext, string, error) {
	t.Helper()
	p := &Plugin{}
	rc, rec, _ := plugintest.RunContext(t, p, settings)
	var logs bytes.Buffer
	rc.Log = slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
	if inv != nil {
		rc.Inventory = inv
	}
	if sink != nil {
		rc.Sink = sink
	}
	err := p.Run(context.Background(), rc)
	return rec.All(), rc, logs.String(), err
}

func byTarget(obs []plugin.Observation) map[string]plugin.Observation {
	out := map[string]plugin.Observation{}
	for _, o := range obs {
		out[o.Target] = o
	}
	return out
}

func TestRunInventoryExport(t *testing.T) {
	obs, rc, logs, err := runCSV(t, map[string]any{"file": fixture(t, "inventory-export.csv")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 7 {
		t.Fatalf("got %d observations, want 7\n%s", len(obs), logs)
	}
	rows := byTarget(obs)
	router := rows["Zeile 2"]
	want := plugin.Observation{
		MACs: []string{"94:83:c4:1a:2b:3c"}, IP: "192.168.8.1", IPs: nil, Target: "Zeile 2", Create: true,
		Hostname: "console.gl-inet.com", Vendor: "GL Technologies (Hong Kong) Limited", Model: "GL-MT6000",
		OS: &plugin.OSInfo{Name: "OpenWrt 23.05"},
		Manual: &plugin.ManualData{DisplayName: "GL-MT6000 Router", Type: "router", Location: "Flur", Owner: "Tobias",
			Notes: "Hauptrouter, Firmware 4.6", Criticality: "critical", State: "known", Tags: []string{"infrastruktur", "netzwerk"},
			Custom: map[string]any{"seriennummer": "SN-7F3A92", "garantie_bis": "2027-05-31", "backup": false}},
	}
	if !reflect.DeepEqual(router, want) {
		t.Errorf("router:\n got %+v\n     %+v\nwant %+v\n     %+v", router, router.Manual, want, want.Manual)
	}
	nas := rows["Zeile 3"]
	if nas.Manual.Notes != "RAID 5\nBackups jede Nacht um 02:00" || nas.Manual.Custom["backup"] != true || nas.Manual.Criticality != "high" {
		t.Errorf("nas: %+v", nas.Manual)
	}
	// the multi-line notes of the NAS shift the following line numbers
	pve := rows["Zeile 5"]
	if !reflect.DeepEqual(pve.MACs, []string{"a8:a1:59:3c:7e:10", "a8:a1:59:3c:7e:11"}) || pve.IP != "192.168.8.20" ||
		!reflect.DeepEqual(pve.IPs, []string{"10.20.0.1"}) || pve.Manual.DisplayName != "" || pve.Manual.Type != "hypervisor" ||
		pve.Hostname != "pve1" || pve.Manual.Custom != nil {
		t.Errorf("pve1: %+v %+v", pve, pve.Manual)
	}
	if cam := rows["Zeile 7"]; cam.MACs != nil || cam.IP != "192.168.9.20" || cam.Manual.Type != "camera" {
		t.Errorf("camera (IP only): %+v", cam)
	}
	if anon := rows["Zeile 8"]; anon.Manual.State != "unknown" || anon.Manual.DisplayName != "" || anon.OS != nil || anon.Vendor != "" {
		t.Errorf("unnamed device: %+v %+v", anon, anon.Manual)
	}
	if printer := rows["Zeile 9"]; printer.Vendor != "Brother Industries, LTD." || printer.Manual.State != "ignored" || printer.Manual.Criticality != "low" {
		t.Errorf("printer: %+v %+v", printer, printer.Manual)
	}
	if st := rc.Stats(); st["imported"] != 7 || st["rows"] != 7 || st["errors"] != nil {
		t.Errorf("stats: %v\n%s", st, logs)
	}
	if strings.Contains(logs, "Unbekannte Spalten") {
		t.Errorf("export columns must all be known or deliberately ignored:\n%s", logs)
	}
}

func TestRunGermanExcel(t *testing.T) {
	obs, rc, logs, err := runCSV(t, map[string]any{"file": fixture(t, "geraete-excel.csv")}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := byTarget(obs)
	if len(obs) != 4 {
		t.Fatalf("got %d observations, want 4\n%s", len(obs), logs)
	}
	nas := rows["Zeile 2"]
	wantManual := &plugin.ManualData{DisplayName: "NAS Keller", Type: "nas", Location: "Keller", Owner: "Tobias",
		Notes: "Größe: 4x8 TB", Criticality: "high", State: "known", Tags: []string{"backup", "storage"},
		Custom: map[string]any{"inventarnummer": "INV-0042"}}
	if !reflect.DeepEqual(nas.Manual, wantManual) || !reflect.DeepEqual(nas.MACs, []string{"00:11:32:ab:cd:ef"}) || nas.Vendor != "Synology" {
		t.Errorf("nas:\n got %+v\nwant %+v", nas.Manual, wantManual)
	}
	if pi := rows["Zeile 3"]; !reflect.DeepEqual(pi.MACs, []string{"b8:27:eb:5d:12:a0"}) || pi.Manual.Type != "server" || pi.Manual.Criticality != "low" {
		t.Errorf("octopi: %+v %+v", pi, pi.Manual)
	}
	// invalid MAC: imported by IP; Windows-1252 € decoded
	if printer := rows["Zeile 5"]; printer.MACs != nil || printer.IP != "192.168.8.41" || printer.Manual.Notes != "Toner je 89,90 €" || printer.Manual.Type != "printer" {
		t.Errorf("printer: %+v %+v", printer, printer.Manual)
	}
	if tv := rows["Zeile 6"]; tv.Manual.Type != "tv" || tv.Manual.State != "" || tv.Manual.Criticality != "" || tv.IP != "" {
		t.Errorf("tv: %+v %+v", tv, tv.Manual)
	}
	for _, want := range []string{"zeile=4", "zeile=5", "zeile=6", "Windows-1252", "Garantie"} {
		if !strings.Contains(logs, want) {
			t.Errorf("log misses %q:\n%s", want, logs)
		}
	}
	if st := rc.Stats(); st["imported"] != 4 || st["skipped"] != 1 || st["errors"] != 4 {
		t.Errorf("stats: %v", st)
	}
}

// zeroSink simulates an inventory without matching devices.
type zeroSink struct{}

func (zeroSink) Observe(ctx context.Context, o *plugin.Observation) (int64, error) {
	if o.Create {
		return 1, nil
	}
	return 0, nil
}

func TestRunExistingAndMissing(t *testing.T) {
	inv := &plugintest.Inventory{List: []plugin.DeviceInfo{
		{ID: 3, PrimaryIP: "192.168.8.5", MACs: []string{"00:11:32:ab:cd:ef"}},
		{ID: 4, PrimaryIP: "192.168.9.20"},
	}}
	_, rc, _, err := runCSV(t, map[string]any{"file": fixture(t, "inventory-export.csv"), "overwrite": true}, inv, nil)
	if err != nil {
		t.Fatal(err)
	}
	if st := rc.Stats(); st["updated"] != 2 || st["imported"] != 5 {
		t.Errorf("stats: %v", st)
	}

	obs, rc, _, err := runCSV(t, map[string]any{"file": fixture(t, "inventory-export.csv"), "create_missing": false}, nil, zeroSink{})
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 0 || rc.Stats()["skipped"] != 7 {
		t.Errorf("create_missing=false: %v", rc.Stats())
	}
}

func TestRunOverwriteFlag(t *testing.T) {
	obs, _, _, err := runCSV(t, map[string]any{"file": fixture(t, "inventory-export.csv"), "overwrite": true}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, o := range obs {
		if !o.Manual.Overwrite {
			t.Errorf("%s: overwrite not set", o.Target)
		}
	}
}

// TestRoundTripCoreExport imports a file written by the core's inventory export.
func TestRoundTripCoreExport(t *testing.T) {
	first := time.Date(2026, 9, 1, 8, 0, 0, 0, time.UTC)
	devs := []reports.InventoryDevice{
		{DeviceRow: inventory.DeviceRow{ID: 1, DisplayName: "NAS; Keller", Hostname: "nas", IP: "192.168.8.5", IPs: []string{"192.168.8.5", "fd00::5"},
			MAC: "00:11:32:ab:cd:ef", MACs: []string{"00:11:32:ab:cd:ef", "00:11:32:ab:cd:f0"}, Vendor: "Synology Incorporated", Model: "DS920+",
			Type: "nas", OS: "DSM 7.2", Location: "Keller", Owner: "Tobias", State: "known", Criticality: "high", Online: true,
			FirstSeen: &first, LastSeen: &first, Tags: []string{"backup", "storage"},
			Custom: map[string]any{"seriennummer": "2089QVR123456", "rack_units": float64(2), "backup": true}},
			Notes: "RAID 5, \"Hot Spare\"\nzweite Zeile"},
		{DeviceRow: inventory.DeviceRow{ID: 2, IP: "192.168.9.20", IPs: []string{"192.168.9.20"}, State: "unknown", Criticality: "normal",
			Custom: map[string]any{}}},
	}
	var buf bytes.Buffer
	if err := reports.WriteCSV(&buf, devs, []string{"backup", "rack_units", "seriennummer"}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "export.csv")
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	obs, _, logs, err := runCSV(t, map[string]any{"file": path}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(obs) != 2 {
		t.Fatalf("got %d observations\n%s", len(obs), logs)
	}
	nas := obs[0]
	want := plugin.Observation{
		MACs: []string{"00:11:32:ab:cd:ef", "00:11:32:ab:cd:f0"}, IP: "192.168.8.5", IPs: []string{"fd00::5"}, Target: "Zeile 2", Create: true,
		Hostname: "nas", Vendor: "Synology Incorporated", Model: "DS920+", OS: &plugin.OSInfo{Name: "DSM 7.2"},
		Manual: &plugin.ManualData{DisplayName: "NAS; Keller", Type: "nas", Location: "Keller", Owner: "Tobias",
			Notes: "RAID 5, \"Hot Spare\"\nzweite Zeile", Criticality: "high", State: "known", Tags: []string{"backup", "storage"},
			Custom: map[string]any{"seriennummer": "2089QVR123456", "rack_units": "2", "backup": true}},
	}
	if !reflect.DeepEqual(nas, want) {
		t.Errorf("round trip:\n got %+v\n     %+v\nwant %+v\n     %+v", nas, nas.Manual, want, want.Manual)
	}
	if o := obs[1]; o.IP != "192.168.9.20" || o.MACs != nil || o.Manual.State != "unknown" || o.Manual.Custom != nil {
		t.Errorf("second device: %+v %+v", o, o.Manual)
	}
	if strings.Contains(logs, "level=WARN") {
		t.Errorf("round trip must not warn:\n%s", logs)
	}
}

func TestRunErrors(t *testing.T) {
	dir := t.TempDir()
	write := func(name, content string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := map[string]struct {
		path string
		want string
	}{
		"missing":   {filepath.Join(dir, "missing.csv"), "nicht lesbar"},
		"empty":     {write("empty.csv", ""), "leer"},
		"no header": {write("foreign.csv", "Name;Farbe\nNAS;blau\n"), "Kopfzeile"},
	}
	for name, tc := range cases {
		_, _, _, err := runCSV(t, map[string]any{"file": tc.path}, nil, nil)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want %q", name, err, tc.want)
		}
	}
	// header only: nothing to do, no error
	if _, _, _, err := runCSV(t, map[string]any{"file": write("header.csv", "mac,ip\n")}, nil, nil); err != nil {
		t.Errorf("header only: %v", err)
	}
	// invalid custom key and tab delimiter
	p := write("tab.csv", "ip\tcf.Serien-Nr\tcustom:raum_nr\n192.168.8.9\tX1\t12\n")
	obs, _, logs, err := runCSV(t, map[string]any{"file": p, "delimiter": "tab"}, nil, nil)
	if err != nil || len(obs) != 1 {
		t.Fatalf("tab file: %v %d", err, len(obs))
	}
	if !reflect.DeepEqual(obs[0].Manual.Custom, map[string]any{"raum_nr": "12"}) || !strings.Contains(logs, "cf.Serien-Nr") {
		t.Errorf("custom fields: %v\n%s", obs[0].Manual.Custom, logs)
	}
}

func TestHelpers(t *testing.T) {
	if got := sniffDelimiter("\"a;b\",c,d\nx;y;z;w"); got != ',' {
		t.Errorf("sniff comma: %q", got)
	}
	if got := sniffDelimiter("mac;ip;name\n"); got != ';' {
		t.Errorf("sniff semicolon: %q", got)
	}
	if got, latin := decodeText([]byte{'G', 'r', 0xF6, 0xDF, 'e', ' ', 0x80, ' ', 0x84, 'x', 0x93}); got != "Größe € „x“" || !latin {
		t.Errorf("decodeText = %q", got)
	}
	if got, latin := decodeText([]byte("\xef\xbb\xbfmac")); got != "mac" || latin {
		t.Errorf("BOM: %q", got)
	}
	for in, want := range map[string]string{"Drucker": "printer", "Access Point": "access-point", "smart-home": "smart-home",
		"USV": "ups", "Spielkonsole": "game-console", "Eigener Typ": "Eigener Typ", "": ""} {
		if got := normalizeType(in); got != want {
			t.Errorf("normalizeType(%q) = %q, want %q", in, got, want)
		}
	}
	cols, _ := mapHeader([]string{"MAC-Adresse", "IP Adresse", "Kritikalität", "Display Name", "first_seen", "Zustand"})
	var fields []string
	for _, c := range cols {
		fields = append(fields, c.Field)
	}
	if !reflect.DeepEqual(fields, []string{colMAC, colIP, colCriticality, colName, colIgnored, colState}) {
		t.Errorf("mapHeader: %v", fields)
	}
}
