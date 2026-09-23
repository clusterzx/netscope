package cve

import (
	"context"
	"encoding/json"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// clock is a controllable time source.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 9, 1, 4, 30, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
	return c.t
}

func (c *clock) Ms() int64 { return c.Now().UnixMilli() }

func newTestDB(t testing.TB) *db.DB {
	t.Helper()
	d, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "netscope.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

// loadFixtures imports the real feed fixtures like an initial sync.
func loadFixtures(t testing.TB, d *db.DB, names ...string) {
	t.Helper()
	if len(names) == 0 {
		names = []string{"nvdcve-2.0-2024.json.gz", "nvdcve-2.0-2021.json.gz"}
	}
	for _, n := range names {
		if _, _, err := applyFeed(context.Background(), d, filepath.Join("testdata", n), 0, false, false, map[string]struct{}{}, nil); err != nil {
			t.Fatal(err)
		}
	}
}

func newTestPlugin(c *clock) *Plugin {
	p := New()
	p.now = c.Now
	p.retryDelay = time.Millisecond
	return p
}

func testRC(t testing.TB, p *Plugin, d *db.DB, settings map[string]any) (*plugin.RunContext, *plugintest.Events) {
	t.Helper()
	if settings == nil {
		settings = map[string]any{}
	}
	rc, _, evs := plugintest.RunContext(t, p, settings)
	rc.DB = d
	return rc, evs
}

// inventory fixtures written like the core does

func addDevice(t testing.TB, d *db.DB, name string, at int64) int64 {
	t.Helper()
	res, err := d.W.Exec(`INSERT INTO devices(display_name, created_source, first_seen, last_seen, state, created_at, updated_at)
		VALUES (?, 'arpscan', ?, ?, 'known', ?, ?)`, name, at, at, at, at)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func addPort(t testing.TB, d *db.DB, dev int64, port int, product, version string, cpes []string, at int64) int64 {
	t.Helper()
	if cpes == nil {
		cpes = []string{}
	}
	b, _ := json.Marshal(cpes)
	res, err := d.W.Exec(`INSERT INTO ports(device_id, ip, proto, port, state, service, product, version, cpes, source, first_seen, last_seen)
		VALUES (?, '192.168.8.10', 'tcp', ?, 'open', 'svc', ?, ?, ?, 'nmap', ?, ?)`, dev, port, product, version, string(b), at, at)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func closeRow(t testing.TB, d *db.DB, table string, id, at int64) {
	t.Helper()
	if _, err := d.W.Exec("UPDATE "+table+" SET gone_at = ? WHERE id = ?", at, id); err != nil {
		t.Fatal(err)
	}
}

func addPackage(t testing.TB, d *db.DB, dev int64, manager, name, version string, at int64) int64 {
	t.Helper()
	res, err := d.W.Exec(`INSERT INTO packages(device_id, manager, name, version, arch, source, first_seen, last_seen)
		VALUES (?,?,?,?, 'amd64', 'ssh', ?, ?)`, dev, manager, name, version, at, at)
	if err != nil {
		t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

func addHTTPApp(t testing.TB, d *db.DB, dev int64, port int, apps []plugin.DetectedApp, at int64) {
	t.Helper()
	b, _ := json.Marshal(apps)
	if _, err := d.W.Exec(`INSERT INTO http_services(device_id, ip, port, scheme, url, apps, source, first_seen, last_seen)
		VALUES (?, '192.168.8.10', ?, 'http', 'http://x', ?, 'http', ?, ?)`, dev, port, string(b), at, at); err != nil {
		t.Fatal(err)
	}
}

func addOSFact(t testing.TB, d *db.DB, dev int64, source string, os plugin.OSInfo, at int64) {
	t.Helper()
	b, _ := json.Marshal(os)
	if _, err := d.W.Exec(`INSERT INTO device_facts(device_id, kind, source, value, extra, first_seen, last_seen)
		VALUES (?, 'os', ?, ?, ?, ?, ?)`, dev, source, os.Name, string(b), at, at); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.Exec(`UPDATE devices SET os = ?, os_source = ? WHERE id = ?`, os.Name, source, dev); err != nil {
		t.Fatal(err)
	}
}

// activeCVEs returns cve -> match_type of the active rows of a device.
func activeCVEs(t testing.TB, d *db.DB, dev int64) map[string]string {
	t.Helper()
	rows, err := d.R.Query("SELECT cve_id, match_type FROM device_cves WHERE device_id = ? AND gone_at IS NULL", dev)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var cve, typ string
		if err := rows.Scan(&cve, &typ); err != nil {
			t.Fatal(err)
		}
		if cur, ok := out[cve]; !ok || matchRank(typ) > matchRank(cur) {
			out[cve] = typ
		}
	}
	return out
}

func eventsOf(evs *plugintest.Events, typ string) []plugin.Event {
	var out []plugin.Event
	for _, e := range evs.Events {
		if e.Type == typ {
			out = append(out, e)
		}
	}
	return out
}
