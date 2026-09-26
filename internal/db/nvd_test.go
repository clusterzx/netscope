package db

import (
	"compress/gzip"
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tablesIn(t *testing.T, c interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}, schema string) []string {
	t.Helper()
	rows, err := c.QueryContext(context.Background(), "SELECT name FROM "+schema+".sqlite_master WHERE type = 'table' AND name LIKE 'nvd_%' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		_ = rows.Scan(&n)
		out = append(out, n)
	}
	return out
}

func TestNVDInOwnFile(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "netscope.db")
	d, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	if got := tablesIn(t, d.W, "main"); len(got) != 0 {
		t.Fatalf("mirror tables in the main database: %v", got)
	}
	if got := tablesIn(t, d.W, "nvd"); strings.Join(got, ",") != "nvd_cpe_matches,nvd_cves,nvd_feeds" {
		t.Fatalf("mirror tables: %v", got)
	}
	// unqualified names reach the mirror, from the write and the read pool
	if _, err := d.W.Exec("INSERT INTO nvd_cves(id, description) VALUES ('CVE-2024-1', 'x')"); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.R.QueryRow("SELECT COUNT(*) FROM nvd_cves").Scan(&n); err != nil || n != 1 {
		t.Fatalf("read pool: %d %v", n, err)
	}
	if _, err := os.Stat(NVDPath(path)); err != nil {
		t.Fatalf("mirror file: %v", err)
	}

	// backups leave the mirror out and are compressed
	dst := filepath.Join(t.TempDir(), "b.db.gz")
	if err := d.BackupGzip(ctx, dst); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Open(dst)
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	plain := filepath.Join(t.TempDir(), "b.db")
	out, _ := os.Create(plain)
	_, _ = io.Copy(out, zr)
	out.Close()
	f.Close()
	b, err := sql.Open("sqlite", dsn(plain, true))
	if err != nil {
		t.Fatal(err)
	}
	defer b.Close()
	if got := tablesIn(t, b, "main"); len(got) != 0 {
		t.Fatalf("backup contains the mirror: %v", got)
	}
	if v, err := Validate(ctx, plain); err != nil || v == 0 {
		t.Fatalf("backup invalid: %d %v", v, err)
	}
	d.Close()
}

func TestNVDMovedFromMainDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "netscope.db")
	d, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	d.Close()
	// an older installation: the mirror inside the main database, no mirror file
	if err := os.Remove(NVDPath(path)); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		t.Fatal(err)
	}
	old := strings.ReplaceAll(strings.ReplaceAll(nvdTablesSQL, "IF NOT EXISTS nvd.", ""), "nvd.", "")
	if _, err := raw.Exec(old); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`INSERT INTO nvd_cves(id, description) VALUES ('CVE-2024-1', 'eins'), ('CVE-2024-2', 'zwei');
		INSERT INTO nvd_cpe_matches(cve_id, part, vendor, product, version) VALUES ('CVE-2024-1', 'a', 'openbsd', 'openssh', '*');
		INSERT INTO nvd_feeds(name, cve_count) VALUES ('2024', 2)`); err != nil {
		t.Fatal(err)
	}
	raw.Close()

	d, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if got := tablesIn(t, d.W, "main"); len(got) != 0 {
		t.Fatalf("mirror left in the main database: %v", got)
	}
	var cves, matches, feeds int
	_ = d.R.QueryRow("SELECT (SELECT COUNT(*) FROM nvd.nvd_cves), (SELECT COUNT(*) FROM nvd.nvd_cpe_matches), (SELECT COUNT(*) FROM nvd.nvd_feeds)").
		Scan(&cves, &matches, &feeds)
	if cves != 2 || matches != 1 || feeds != 1 {
		t.Fatalf("moved: %d cves, %d matches, %d feeds", cves, matches, feeds)
	}
	var idx int
	_ = d.R.QueryRow("SELECT COUNT(*) FROM nvd.sqlite_master WHERE type = 'index' AND name = 'nvd_cpe_matches_vp'").Scan(&idx)
	if idx != 1 {
		t.Fatal("index of the matcher missing")
	}
}
