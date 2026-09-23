package oui

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

func parseFixture(t *testing.T, name string, parse func(r *os.File, tb *table) (int, error)) *table {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	tb := newTable()
	n, err := parse(f, tb)
	if err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if n == 0 || n != tb.len() {
		t.Fatalf("%s: %d entries, table has %d", name, n, tb.len())
	}
	return tb
}

func csvParser(r *os.File, tb *table) (int, error)  { return parseIEEECSV(r, tb) }
func tabParser(r *os.File, tb *table) (int, error)  { return parseTabFile(r, tb) }
func nmapParser(r *os.File, tb *table) (int, error) { return parseNmapFile(r, tb) }

func hexOf(mac string) string { return strings.ToUpper(strings.ReplaceAll(mac, ":", "")) }

func expectLookup(t *testing.T, tb *table, mac, want string) {
	t.Helper()
	got, _ := tb.lookup(hexOf(mac))
	if got != want {
		t.Errorf("%s: got %q, want %q", mac, got, want)
	}
}

func TestParseIEEE(t *testing.T) {
	tb := parseFixture(t, "oui.csv", csvParser)
	expectLookup(t, tb, "94:83:c4:a8:a4:0a", "GL Technologies (Hong Kong) Limited")
	expectLookup(t, tb, "5c:41:5a:76:dc:a0", "Amazon.com, LLC")                    // quoted field with comma
	expectLookup(t, tb, "78:22:88:6d:2a:6a", "SHENZHEN BILIAN ELECTRONIC CO.，LTD") // non-ASCII comma
	expectLookup(t, tb, "38:8a:21:00:00:01", `UAB "Teltonika Telematics"`)         // escaped quotes
	expectLookup(t, tb, "bc:24:11:13:ce:ce", "Proxmox Server Solutions GmbH")      // not in arp-scan's 2022 file
	expectLookup(t, tb, "00:00:00:00:00:01", "")

	mam := parseFixture(t, "mam.csv", csvParser)
	expectLookup(t, mam, "c8:5c:e2:70:00:01", "SYNERGY SYSTEMS AND SOLUTIONS")
	expectLookup(t, mam, "c8:5c:e2:a0:00:01", "San Telequip (P) Ltd.,")

	s36 := parseFixture(t, "oui36.csv", csvParser)
	expectLookup(t, s36, "8c:1f:64:af:a1:23", "DATA ELECTRONIC DEVICES, INC")
	expectLookup(t, s36, "70:b3:d5:f2:f0:01", "TELEPLATFORMS")
}

func TestParseIEEERejectsOtherFormats(t *testing.T) {
	for _, name := range []string{"ieee-oui.txt", "nmap-mac-prefixes"} {
		f, err := os.Open(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		_, err = parseIEEECSV(f, newTable())
		f.Close()
		if err == nil {
			t.Errorf("%s accepted as IEEE CSV", name)
		}
	}
}

func TestParseArpScanFiles(t *testing.T) {
	tb := parseFixture(t, "ieee-oui.txt", tabParser)
	expectLookup(t, tb, "a8:a1:59:77:91:0e", "ASRock Incorporation")
	expectLookup(t, tb, "ac:15:a2:ac:12:68", "TP-Link Corporation Limited")
	expectLookup(t, tb, "00:50:c2:dc:71:00", "AGT Holdings Limited") // 36-bit IAB entry
	expectLookup(t, tb, "00:50:c2:12:34:56", "IEEE Registration Authority")

	mv := parseFixture(t, "mac-vendor.txt", tabParser)
	expectLookup(t, mv, "00:00:5e:00:01:07", "VRRP (last octet is VRID)")
	expectLookup(t, mv, "00:00:0c:07:ac:01", "HSRP (last octet is group number)")
	expectLookup(t, mv, "52:54:00:12:34:56", "QEMU")
}

func TestParseNmap(t *testing.T) {
	tb := parseFixture(t, "nmap-mac-prefixes", nmapParser)
	expectLookup(t, tb, "08:00:27:aa:bb:cc", "Oracle VirtualBox virtual NIC")
	expectLookup(t, tb, "dc:a6:32:ba:1f:c4", "Raspberry Pi Trading")
	expectLookup(t, tb, "c8:5c:e2:71:00:00", "Synergy Systems AND Solutions")
	expectLookup(t, tb, "8c:1f:64:af:a0:00", "Data Electronic Devices")
}

// testPlugin uses the fixtures as arp-scan and nmap fallback files.
func testPlugin() *Plugin {
	abs := func(n string) string { p, _ := filepath.Abs(filepath.Join("testdata", n)); return p }
	return &Plugin{fallbacks: []layer{
		{name: "arp-scan", sources: []source{{abs("ieee-oui.txt"), parseTabFile}, {abs("mac-vendor.txt"), parseTabFile}}},
		{name: "nmap", sources: []source{{abs("nmap-mac-prefixes"), parseNmapFile}}},
	}}
}

func copyFixture(t *testing.T, dir, name string) {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLayers(t *testing.T) {
	p := testPlugin()
	dir := t.TempDir()
	noLog := func(string, ...any) {}
	db, err := p.database(dir, noLog)
	if err != nil {
		t.Fatal(err)
	}
	check := func(mac, vendor, layer string) {
		t.Helper()
		v, l := db.lookup(mac)
		if v != vendor || l != layer {
			t.Errorf("%s: got %q/%q, want %q/%q", mac, v, l, vendor, layer)
		}
	}
	// Without downloaded IEEE files: arp-scan first, nmap as fallback.
	check("ac:15:a2:ac:12:68", "TP-Link Corporation Limited", "arp-scan")
	check("bc:24:11:13:ce:ce", "Proxmox Server Solutions GmbH", "nmap")
	check("70:b3:d5:f2:f0:01", "TELEPLATFORMS", "arp-scan")
	check("70:b3:d5:12:30:01", "", "") // only "IEEE Registration Authority" placeholders

	for _, f := range ieeeFiles {
		copyFixture(t, dir, f)
	}
	db, err = p.database(dir, noLog)
	if err != nil {
		t.Fatal(err)
	}
	check("ac:15:a2:ac:12:68", "TP-Link Systems Inc", "IEEE")
	check("c8:5c:e2:70:00:01", "SYNERGY SYSTEMS AND SOLUTIONS", "IEEE") // MA-M beats the MA-L placeholder
	check("74:1a:e0:90:00:01", "", "")                                  // MA-M "Private", MA-L placeholder
	check("52:54:00:12:34:56", "QEMU", "arp-scan")
	check("08:00:27:aa:bb:cc", "Oracle VirtualBox virtual NIC", "nmap")
}

func TestNoData(t *testing.T) {
	p := &Plugin{fallbacks: []layer{}}
	rc, _, _ := plugintest.RunContext(t, p, nil)
	if err := p.Run(context.Background(), rc); !errors.Is(err, ErrNoData) {
		t.Fatalf("expected ErrNoData, got %v", err)
	}
}

func TestRun(t *testing.T) {
	p := testPlugin()
	rc, sink, _ := plugintest.RunContext(t, p, nil)
	rc.Targets.Devices = []plugin.DeviceInfo{
		{ID: 1, PrimaryMAC: "94:83:c4:a8:a4:0a", MACs: []string{"94:83:c4:a8:a4:0a"}},
		{ID: 2, PrimaryMAC: "12:d5:c5:c7:a3:40", MACs: []string{"12:d5:c5:c7:a3:40", "a8:a1:59:77:91:0e"}}, // random primary
		{ID: 3, MACs: []string{"02:00:00:00:00:01"}},
		{ID: 4},
		{ID: 5, PrimaryMAC: "00:11:22:33:44:e4"},
	}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	got := map[int64]plugin.Observation{}
	for _, o := range sink.All() {
		got[o.DeviceID] = o
	}
	if len(got) != 3 {
		t.Fatalf("observations: %+v", sink.All())
	}
	if o := got[1]; o.Vendor != "GL Technologies (Hong Kong) Limited" || o.MACs[0] != "94:83:c4:a8:a4:0a" || o.Present {
		t.Errorf("device 1: %+v", o)
	}
	if o := got[2]; o.Vendor != "ASRock Incorporation" || o.MACs[0] != "a8:a1:59:77:91:0e" {
		t.Errorf("device 2: %+v", o)
	}
	if o := got[5]; o.Vendor != "CIMSYS Inc" {
		t.Errorf("device 5: %+v", o)
	}
	if rc.Stats()["hosts"] != 3 {
		t.Errorf("stats: %v", rc.Stats())
	}
}

func TestHandleChanges(t *testing.T) {
	p := testPlugin()
	rc, sink, _ := plugintest.RunContext(t, p, nil)
	rc.RunID = 0
	changes := []plugin.Change{
		{Type: plugin.ChangeMACAdded, DeviceID: 11, Key: "dc:15:c8:22:c6:b3", New: "dc:15:c8:22:c6:b3"},
		{Type: plugin.ChangeDeviceCreated, DeviceID: 12, New: &plugin.DeviceSnapshot{ID: 12, MAC: "dc:a6:32:ba:1f:c4"}},
		{Type: plugin.ChangeDeviceCreated, DeviceID: 13, New: &plugin.DeviceSnapshot{ID: 13, IP: "192.168.8.99"}},
		{Type: plugin.ChangeMACAdded, DeviceID: 14, New: "d6:b1:65:03:b8:71"}, // randomized
		{Type: plugin.ChangePortOpened, DeviceID: 15},
	}
	if err := p.HandleChanges(context.Background(), rc, changes); err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) != 2 || obs[0].DeviceID != 11 || obs[0].Vendor != "AVM Audiovisuelles Marketing und Computersysteme GmbH" ||
		obs[1].DeviceID != 12 || obs[1].Vendor != "Raspberry Pi Trading Ltd" || obs[1].MACs[0] != "dc:a6:32:ba:1f:c4" {
		t.Fatalf("observations: %+v", obs)
	}
}

func TestHandleChangesUsesInventory(t *testing.T) {
	p := testPlugin()
	rc, sink, _ := plugintest.RunContext(t, p, nil)
	// A second NIC of a known device appears: the vendor stays the one of the primary MAC.
	rc.Inventory = &plugintest.Inventory{List: []plugin.DeviceInfo{
		{ID: 21, PrimaryMAC: "a8:a1:59:77:91:0e", MACs: []string{"a8:a1:59:77:91:0e", "c8:9e:43:83:96:e3"}},
	}}
	changes := []plugin.Change{
		{Type: plugin.ChangeMACAdded, DeviceID: 21, Key: "c8:9e:43:83:96:e3", New: "c8:9e:43:83:96:e3"},
		{Type: plugin.ChangeMACAdded, DeviceID: 22, New: "c8:9e:43:83:96:e3"}, // not in the inventory: changed MAC
	}
	if err := p.HandleChanges(context.Background(), rc, changes); err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) != 2 || obs[0].Vendor != "ASRock Incorporation" || obs[0].MACs[0] != "a8:a1:59:77:91:0e" ||
		obs[1].DeviceID != 22 || obs[1].Vendor != "NETGEAR" {
		t.Fatalf("observations: %+v", obs)
	}
}

func ieeeServer(t *testing.T, override map[string]string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.UserAgent(), "Mozilla/5.0") {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		name := filepath.Base(r.URL.Path)
		if body, ok := override[name]; ok {
			_, _ = w.Write([]byte(body))
			return
		}
		b, err := os.ReadFile(filepath.Join("testdata", name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(b)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestDownload(t *testing.T) {
	srv := ieeeServer(t, nil)
	dir := t.TempDir()
	urls := []string{srv.URL + "/oui/oui.csv", srv.URL + "/oui28/mam.csv", srv.URL + "/oui36/oui36.csv"}
	counts, err := download(context.Background(), srv.Client(), urls, dir, 3)
	if err != nil {
		t.Fatal(err)
	}
	if counts["oui.csv"] != 27 || counts["mam.csv"] != 5 || counts["oui36.csv"] != 5 {
		t.Fatalf("counts: %v", counts)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("files: %v", entries)
	}

	// A broken download keeps the previous files and leaves no temporary files behind.
	bad := ieeeServer(t, map[string]string{"mam.csv": "<html>Service Unavailable</html>"})
	badURLs := []string{bad.URL + "/oui/oui.csv", bad.URL + "/oui28/mam.csv"}
	if _, err := download(context.Background(), bad.Client(), badURLs, dir, 3); err == nil {
		t.Fatal("expected validation error")
	}
	entries, _ = os.ReadDir(dir)
	if len(entries) != 3 {
		t.Fatalf("files after failed update: %v", entries)
	}
	if _, err := download(context.Background(), srv.Client(), urls, dir, 1000); err == nil ||
		!strings.Contains(err.Error(), "mindestens 1000") {
		t.Fatalf("expected minimum entry error, got %v", err)
	}
	if _, err := download(context.Background(), srv.Client(), []string{srv.URL + "/x/oui.txt"}, dir, 1); err == nil {
		t.Fatal("non-CSV URL accepted")
	}
}

func TestUpdateActionReloads(t *testing.T) {
	srv := ieeeServer(t, nil)
	p := testPlugin()
	rc, _, _ := plugintest.RunContext(t, p, map[string]any{"urls": []any{srv.URL + "/oui/oui.csv"}})
	db, err := p.database(rc.DataDir, rc.Log.Warn)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := db.lookup("ac:15:a2:ac:12:68"); v != "TP-Link Corporation Limited" {
		t.Fatalf("before update: %q", v)
	}
	// minEntries is 1000 for real downloads; the fixture has 27 entries.
	if _, err := p.RunAction(context.Background(), rc, "update", nil); err == nil || !strings.Contains(err.Error(), "mindestens") {
		t.Fatalf("expected validation error for the small fixture, got %v", err)
	}
	counts, err := download(context.Background(), srv.Client(), []string{srv.URL + "/oui/oui.csv"}, rc.DataDir, 3)
	if err != nil || counts["oui.csv"] != 27 {
		t.Fatalf("download: %v %v", counts, err)
	}
	db, err = p.database(rc.DataDir, rc.Log.Warn)
	if err != nil {
		t.Fatal(err)
	}
	if v, l := db.lookup("ac:15:a2:ac:12:68"); v != "TP-Link Systems Inc" || l != "IEEE" {
		t.Fatalf("after update: %q from %q", v, l)
	}
	if _, err := p.RunAction(context.Background(), rc, "nope", nil); err == nil {
		t.Fatal("unknown action accepted")
	}
}

func TestThousands(t *testing.T) {
	for n, want := range map[int]string{0: "0", 999: "999", 1000: "1.000", 54012: "54.012", 1234567: "1.234.567"} {
		if got := thousands(n); got != want {
			t.Errorf("%d: %q", n, got)
		}
	}
}
