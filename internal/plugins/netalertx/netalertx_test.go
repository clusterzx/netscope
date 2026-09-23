package netalertx

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
	_ "time/tzdata" // Europe/Berlin also in minimal build containers

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// buildDB creates a SQLite database in WAL mode (like NetAlertX' app.db) from the
// schema and data fixtures.
func buildDB(t *testing.T, schema, data string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "app.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{"PRAGMA journal_mode=WAL", string(plugintest.Fixture(t, schema)), string(plugintest.Fixture(t, data))} {
		if _, err := db.Exec(stmt); err != nil {
			db.Close()
			t.Fatalf("%s: %v", schema, err)
		}
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func fixturePath(t *testing.T, name string) string {
	t.Helper()
	p, err := filepath.Abs(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func runImport(t *testing.T, settings map[string]any, inv *plugintest.Inventory) ([]plugin.Observation, *plugin.RunContext, error) {
	t.Helper()
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, settings)
	berlin, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	rc.Env.Location = berlin
	if inv != nil {
		rc.Inventory = inv
	}
	err = p.Run(context.Background(), rc)
	return sink.All(), rc, err
}

// split separates first-pass device observations from second-pass relation observations.
func split(obs []plugin.Observation) (devices map[string]plugin.Observation, relations []plugin.Observation) {
	devices = map[string]plugin.Observation{}
	for _, o := range obs {
		if o.DeviceID != 0 {
			relations = append(relations, o)
			continue
		}
		devices[o.Ref.ID] = o
	}
	return devices, relations
}

func TestReadSQLite(t *testing.T) {
	ds, err := readSQLite(context.Background(), buildDB(t, "netalertx-schema.sql", "netalertx-devices.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if ds.Schema != "netalertx" || !ds.UTC || len(ds.Devices) != 13 {
		t.Fatalf("dataset: schema=%s utc=%v devices=%d", ds.Schema, ds.UTC, len(ds.Devices))
	}
	router := ds.Devices[1]
	// DATETIME columns keep their stored text (the driver would convert them otherwise)
	if router.get("first_connection") != "2024-02-10 07:00:05" || router.get("parent_mac") != "internet" ||
		router.get("fqdn") != "console.gl-inet.com" || !router.flag("favorite") || router.flag("new") {
		t.Errorf("router: %v", router.Fields)
	}

	old, err := readSQLite(context.Background(), buildDB(t, "pialert-schema.sql", "pialert-devices.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if old.Schema != "pialert" || old.UTC || len(old.Devices) != 6 {
		t.Fatalf("pialert dataset: schema=%s utc=%v devices=%d", old.Schema, old.UTC, len(old.Devices))
	}
	if nas := old.Devices[2]; nas.get("parent_mac") != "94:83:c4:1a:2b:3c" || nas.get("parent_port") != "2" || nas.get("type") != "NAS" {
		t.Errorf("pialert nas: %v", nas.Fields)
	}
}

func TestReadCSV(t *testing.T) {
	ds, err := readCSV(fixturePath(t, "netalertx-devices.csv"))
	if err != nil {
		t.Fatal(err)
	}
	if ds.Schema != "netalertx" || len(ds.Devices) != 13 {
		t.Fatalf("csv: schema=%s devices=%d", ds.Schema, len(ds.Devices))
	}
	sw := ds.Devices[2]
	if sw.get("vendor") != "TP-LINK TECHNOLOGIES CO.,LTD." || sw.get("comments") != "" || sw.Fields["comments"] != "None" ||
		sw.get("parent_port") != "3" || sw.Line != 4 {
		t.Errorf("switch: line %d %v", sw.Line, sw.Fields)
	}
	old, err := readCSV(fixturePath(t, "pialert-devices.csv"))
	if err != nil || old.Schema != "pialert" || len(old.Devices) != 6 {
		t.Fatalf("pialert csv: %v %+v", err, old)
	}
}

func TestRunSQLite(t *testing.T) {
	path := buildDB(t, "netalertx-schema.sql", "netalertx-devices.sql")
	// the NAS is already known from a scan
	inv := &plugintest.Inventory{List: []plugin.DeviceInfo{{ID: 99, PrimaryIP: "192.168.8.5", MACs: []string{"00:11:32:ab:cd:ef"}}}}
	obs, rc, err := runImport(t, map[string]any{"file": path}, inv)
	if err != nil {
		t.Fatal(err)
	}
	devices, relations := split(obs)
	if len(devices) != 12 {
		t.Fatalf("got %d devices, want 12 (Internet skipped)", len(devices))
	}
	if st := rc.Stats(); st["imported"] != 11 || st["updated"] != 1 || st["skipped"] != 1 || st["relations"] != 10 || st["errors"] != 0 {
		t.Errorf("stats: %v", st)
	}

	router := devices["94:83:c4:1a:2b:3c"]
	wantManual := &plugin.ManualData{DisplayName: "GL-MT6000 Router", Type: "router", Owner: "Tobias", Location: "Flur",
		Notes: "Hauptrouter im Flur", State: "known", Tags: []string{"Always on", "favorite"}}
	if !reflect.DeepEqual(router.Manual, wantManual) {
		t.Errorf("router manual:\n got %+v\nwant %+v", router.Manual, wantManual)
	}
	if !reflect.DeepEqual(router.MACs, []string{"94:83:c4:1a:2b:3c"}) || router.IP != "192.168.8.1" || !router.Create || router.Present ||
		router.Vendor != "GL Technologies (Hong Kong) Limited" || router.Hostname != "console.gl-inet.com" ||
		router.Attrs["netalertx.type"] != "Gateway" || router.Ref.Source != refSource {
		t.Errorf("router: %+v", router)
	}
	if router.FirstSeen == nil || !router.FirstSeen.Equal(time.Date(2024, 2, 10, 7, 0, 5, 0, time.UTC)) {
		t.Errorf("router first seen (UTC database): %v", router.FirstSeen)
	}
	if inv := router.Inventory.(map[string]string); inv["schema"] != "netalertx" || inv["last_connection"] != "2026-09-22 20:40:03" {
		t.Errorf("router inventory: %v", inv)
	}

	phone := devices["7c:2e:0d:4a:91:3f"]
	if phone.Manual.DisplayName != "iPhone-von-Anna" || phone.Manual.State != "" || phone.Manual.Type != "phone" ||
		!reflect.DeepEqual(phone.Manual.Tags, []string{"Personal"}) || phone.Manual.Owner != "Anna" {
		t.Errorf("new phone: %+v", phone.Manual)
	}
	anon := devices["da:a1:19:6e:33:07"]
	if anon.Manual.DisplayName != "" || anon.Manual.Owner != "" || anon.Vendor != "" || anon.Manual.Type != "" || anon.Attrs != nil {
		t.Errorf("placeholders must be dropped: %+v %+v", anon, anon.Manual)
	}
	if printer := devices["30:05:5c:8a:19:b2"]; printer.Manual.State != "ignored" || printer.Manual.Type != "printer" {
		t.Errorf("archived printer: %+v", printer.Manual)
	}
	cam := devices["fa:ce:5f:2b:7d:01"]
	if cam.MACs != nil || cam.IP != "192.168.9.20" || cam.Manual.Type != "camera" || cam.Vendor != "" {
		t.Errorf("synthetic MAC device: %+v", cam)
	}
	for mac, want := range map[string]string{"bc:24:11:2e:c5:6a": "server", "a8:a1:59:3c:7e:10": "hypervisor",
		"f4:7b:09:12:34:56": "tv", "44:17:93:8c:02:1e": "smart-home", "60:22:32:8a:0b:1c": "access-point", "00:11:32:ab:cd:ef": "nas"} {
		if got := devices[mac].Manual.Type; got != want {
			t.Errorf("%s: type %q, want %q", mac, got, want)
		}
	}

	// second pass: one relation observation per device with an imported parent
	ids := map[int64]string{}
	for i, o := range obs[:len(devices)+1] {
		if o.DeviceID == 0 && o.Ref != nil {
			ids[int64(i+1)] = o.Ref.ID // plugintest.Sink returns 1, 2, 3 …
		}
	}
	got := map[string]string{}
	for _, r := range relations {
		rel := r.Relations[0]
		other := rel.Other.MAC
		if rel.Other.Ref != nil {
			other = rel.Other.Ref.ID
		}
		got[ids[r.DeviceID]] = rel.Kind + " " + other + " " + rel.RemotePort + " " + rel.Label
	}
	want := map[string]string{
		"b0:be:76:40:11:22": "switch_port 94:83:c4:1a:2b:3c 3 ",
		"60:22:32:8a:0b:1c": "switch_port b0:be:76:40:11:22 8 ",
		"00:11:32:ab:cd:ef": "switch_port b0:be:76:40:11:22 5 ",
		"a8:a1:59:3c:7e:10": "switch_port b0:be:76:40:11:22 1 ",
		"bc:24:11:2e:c5:6a": "runs_on a8:a1:59:3c:7e:10  ",
		"7c:2e:0d:4a:91:3f": "wireless 60:22:32:8a:0b:1c  Heimnetz",
		"f4:7b:09:12:34:56": "wireless 60:22:32:8a:0b:1c  Heimnetz",
		"30:05:5c:8a:19:b2": "switch_port b0:be:76:40:11:22 7 ",
		"fa:ce:5f:2b:7d:01": "l3 94:83:c4:1a:2b:3c  ",
		"44:17:93:8c:02:1e": "wireless 60:22:32:8a:0b:1c  Heimnetz",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("relations:\n got %v\nwant %v", got, want)
	}
	for _, r := range relations {
		if r.Create || r.Manual != nil || r.Inventory != nil {
			t.Errorf("relation observation must only carry relations: %+v", r)
		}
	}
}

func TestRunPiAlert(t *testing.T) {
	obs, rc, err := runImport(t, map[string]any{"file": buildDB(t, "pialert-schema.sql", "pialert-devices.sql"), "format": "sqlite"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	devices, relations := split(obs)
	if len(devices) != 5 || len(relations) != 3 {
		t.Fatalf("devices=%d relations=%d", len(devices), len(relations))
	}
	router := devices["94:83:c4:1a:2b:3c"]
	// Pi.Alert stored local time (Europe/Berlin, CEST)
	if router.FirstSeen == nil || !router.FirstSeen.Equal(time.Date(2023, 5, 14, 8, 2, 11, 0, time.UTC)) {
		t.Errorf("first seen: %v", router.FirstSeen)
	}
	if router.Manual.Type != "router" || router.Manual.State != "known" || router.Manual.DisplayName != "GL-MT6000" {
		t.Errorf("router: %+v", router.Manual)
	}
	if o := devices["b8:27:eb:5d:12:a0"]; o.Manual.Type != "server" || o.Manual.State != "ignored" || o.Manual.Notes != "Drucker-Steuerung" {
		t.Errorf("octopi: %+v", o.Manual)
	}
	if u := devices["e8:9f:6d:11:22:33"]; u.Manual.DisplayName != "" || u.Vendor != "" || u.Manual.State != "" {
		t.Errorf("unknown device: %+v %+v", u, u.Manual)
	}
	kinds := map[string]int{}
	for _, r := range relations {
		kinds[r.Relations[0].Kind+" "+r.Relations[0].RemotePort]++
	}
	if !reflect.DeepEqual(kinds, map[string]int{"switch_port 2": 1, "l3 ": 2}) {
		t.Errorf("relation kinds: %v", kinds)
	}
	if st := rc.Stats(); st["imported"] != 5 || st["skipped"] != 1 {
		t.Errorf("stats: %v", st)
	}
}

func TestRunCSV(t *testing.T) {
	for _, file := range []string{"netalertx-devices.csv", "pialert-devices.csv"} {
		obs, _, err := runImport(t, map[string]any{"file": fixturePath(t, file)}, nil)
		if err != nil {
			t.Fatalf("%s: %v", file, err)
		}
		devices, relations := split(obs)
		router := devices["94:83:c4:1a:2b:3c"]
		if router.Manual == nil || router.Manual.Type != "router" || router.Manual.State != "known" || router.IP != "192.168.8.1" {
			t.Errorf("%s router: %+v", file, router)
		}
		if len(relations) == 0 {
			t.Errorf("%s: no relations", file)
		}
		if file == "netalertx-devices.csv" {
			if sw := devices["b0:be:76:40:11:22"]; sw.Manual.Notes != "" || sw.Vendor != "TP-LINK TECHNOLOGIES CO.,LTD." {
				t.Errorf("None must be empty: %+v", sw.Manual)
			}
			// CSV exports carry no time zone marker: local time zone of NetScope
			if !router.FirstSeen.Equal(time.Date(2024, 2, 10, 6, 0, 5, 0, time.UTC)) {
				t.Errorf("csv first seen: %v", router.FirstSeen)
			}
		}
	}
}

func TestRunOptions(t *testing.T) {
	obs, _, err := runImport(t, map[string]any{"file": fixturePath(t, "netalertx-devices.csv"), "format": "csv",
		"overwrite": true, "mark_known": false, "archived_as_ignored": false, "import_first_seen": false}, nil)
	if err != nil {
		t.Fatal(err)
	}
	devices, _ := split(obs)
	for key, o := range devices {
		if o.Manual.State != "" || o.FirstSeen != nil || !o.Manual.Overwrite {
			t.Errorf("%s: state=%q firstSeen=%v overwrite=%v", key, o.Manual.State, o.FirstSeen, o.Manual.Overwrite)
		}
	}
}

func TestRelationParentMustExist(t *testing.T) {
	path := filepath.Join(t.TempDir(), "devices.csv")
	csv := "\"devMac\",\"devName\",\"devLastIP\",\"devParentMAC\",\"devParentPort\"\n" +
		"\"aa:bb:cc:00:00:02\",\"Kind\",\"192.168.8.77\",\"aa:bb:cc:00:00:01\",\"None\"\n"
	if err := os.WriteFile(path, []byte(csv), 0o600); err != nil {
		t.Fatal(err)
	}
	obs, rc, err := runImport(t, map[string]any{"file": path}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, rel := split(obs); len(rel) != 0 || rc.Stats()["relations_skipped"] != 1 {
		t.Errorf("unknown parent: relations=%d stats=%v", len(rel), rc.Stats())
	}
	// a parent known from a scan is linked by MAC
	inv := &plugintest.Inventory{List: []plugin.DeviceInfo{{ID: 5, MACs: []string{"aa:bb:cc:00:00:01"}}}}
	obs, _, err = runImport(t, map[string]any{"file": path}, inv)
	if err != nil {
		t.Fatal(err)
	}
	if _, rel := split(obs); len(rel) != 1 || rel[0].Relations[0].Other.MAC != "aa:bb:cc:00:00:01" || rel[0].Relations[0].Kind != plugin.RelL3 {
		t.Errorf("known parent: %+v", rel)
	}
}

func TestRunErrors(t *testing.T) {
	dir := t.TempDir()
	if _, _, err := runImport(t, map[string]any{"file": filepath.Join(dir, "missing.db")}, nil); err == nil {
		t.Error("missing file must fail")
	}
	other := filepath.Join(dir, "other.csv")
	if err := os.WriteFile(other, []byte("name;ip\nnas;192.168.8.5\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := runImport(t, map[string]any{"file": other}, nil); err == nil || !strings.Contains(err.Error(), "devMac") {
		t.Errorf("foreign CSV: %v", err)
	}
	empty := filepath.Join(dir, "empty.db")
	db, err := sql.Open("sqlite", empty)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE Other (x TEXT)"); err != nil {
		t.Fatal(err)
	}
	db.Close()
	if _, _, err := runImport(t, map[string]any{"file": empty}, nil); err == nil || !strings.Contains(err.Error(), "Devices") {
		t.Errorf("database without Devices: %v", err)
	}
}

func TestMapping(t *testing.T) {
	for in, want := range map[string]string{
		"Smartphone": "phone", "Singleboard Computer (SBC)": "server", "IP Camera": "camera", "Game Console": "game-console",
		"Gaming Console": "game-console", "SmartTV": "tv", "TV Decoder": "media-player", "Virtual Assistance": "speaker",
		"Smart Speaker": "speaker", "AP": "access-point", "Access Point": "access-point", "Gateway": "router", "USB LAN Adapter": "other",
		"Domotic": "smart-home", "Mini PC": "desktop", "PC": "desktop", "Custom Thing": "", "": "",
	} {
		if got := mapType(in); got != want {
			t.Errorf("mapType(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"(unknown)": "", "(name not found)": "", "nas (IP match)": "nas", " Drucker ": "Drucker"} {
		if got := cleanName(in); got != want {
			t.Errorf("cleanName(%q) = %q", in, got)
		}
	}
	berlin, _ := time.LoadLocation("Europe/Berlin")
	for in, want := range map[string]time.Time{
		"2024-01-15 12:00:00":       time.Date(2024, 1, 15, 11, 0, 0, 0, time.UTC),
		"2024-07-15 12:00:00":       time.Date(2024, 7, 15, 10, 0, 0, 0, time.UTC),
		"2024-07-15 12:00:00+00:00": time.Date(2024, 7, 15, 12, 0, 0, 0, time.UTC),
		"2024-07-15T12:00:00Z":      time.Date(2024, 7, 15, 12, 0, 0, 0, time.UTC),
		"2024-07-15 12:00:00.123":   time.Date(2024, 7, 15, 10, 0, 0, 123e6, time.UTC),
	} {
		got, ok := parseTime(in, berlin)
		if !ok || !got.Equal(want) {
			t.Errorf("parseTime(%q) = %v, %v; want %v", in, got, ok, want)
		}
	}
	if _, ok := parseTime("None", berlin); ok {
		t.Error("None must not parse")
	}
	if id, ok := identify("Internet"); ok {
		t.Errorf("Internet must be skipped: %+v", id)
	}
	if id, ok := identify("FA:CE:5F:2B:7D:01"); !ok || id.MAC != "" || id.Key != "fa:ce:5f:2b:7d:01" {
		t.Errorf("synthetic MAC: %+v", id)
	}
	if id, ok := identify("00-11-32-AB-CD-EF"); !ok || id.MAC != "00:11:32:ab:cd:ef" || id.Key != id.MAC {
		t.Errorf("real MAC: %+v", id)
	}
}
