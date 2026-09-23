package cleanup

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin/plugintest"
)

func TestRetention(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "c.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	now := time.Now()
	old := now.Add(-400 * 24 * time.Hour).UnixMilli()
	recent := now.Add(-time.Hour).UnixMilli()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.W.Exec(q, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec("INSERT INTO devices(id, created_at, updated_at) VALUES (1, 0, 0)")
	exec("INSERT INTO observations(plugin_id, device_id, ts, data) VALUES ('x', 1, ?, '{}'), ('x', 1, ?, '{}')", old, recent)
	exec(`INSERT INTO ports(device_id, ip, proto, port, state, source, first_seen, last_seen, gone_at) VALUES
		(1, '1.1.1.1', 'tcp', 22, 'open', 'nmap', ?, ?, ?), (1, '1.1.1.1', 'tcp', 80, 'open', 'nmap', ?, ?, NULL)`, old, old, old, old, recent)
	exec("INSERT INTO events(ts, type, category, severity, title) VALUES (?, 'device.new', 'device', 'info', 'old event')", old)
	exec("INSERT INTO runs(plugin_id, trigger, status, created_at) VALUES ('x', 'manual', 'success', ?)", old)
	dataRoot := t.TempDir()
	backups := filepath.Join(dataRoot, "backups")
	_ = os.MkdirAll(backups, 0o750)
	for i := 0; i < 3; i++ {
		p := filepath.Join(backups, "b"+string(rune('a'+i))+".db")
		_ = os.WriteFile(p, []byte("x"), 0o640)
		_ = os.Chtimes(p, now.Add(-time.Duration(i)*time.Hour), now.Add(-time.Duration(i)*time.Hour))
	}
	rc, _, _ := plugintest.RunContext(t, &Plugin{}, map[string]any{"backups_keep": 2})
	rc.DB = d
	rc.Env.DataRoot = dataRoot
	if err := (&Plugin{}).Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	count := func(q string) int {
		var n int
		_ = d.R.QueryRow(q).Scan(&n)
		return n
	}
	if n := count("SELECT COUNT(*) FROM observations"); n != 1 {
		t.Errorf("observations left %d", n)
	}
	if n := count("SELECT COUNT(*) FROM ports"); n != 1 {
		t.Errorf("ports left %d (active rows must stay)", n)
	}
	if n := count("SELECT COUNT(*) FROM events"); n != 1 {
		t.Errorf("events must never be deleted: %d", n)
	}
	if n := count("SELECT COUNT(*) FROM runs"); n != 0 {
		t.Errorf("runs left %d", n)
	}
	entries, _ := os.ReadDir(backups)
	if len(entries) != 2 {
		t.Errorf("backups left %d", len(entries))
	}
}
