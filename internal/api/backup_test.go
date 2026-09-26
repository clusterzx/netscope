package api

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"netscope/internal/db"
)

func TestBackupCompressedWithoutNVD(t *testing.T) {
	h := newHarness(t)
	h.login(t)
	resp, body := h.do(t, "POST", "/api/v1/system/backups", nil, csrf, "1")
	expect(t, "create backup", resp, body, 201, "")
	var b backupInfo
	_ = json.Unmarshal(body, &b)
	if !strings.HasSuffix(b.Name, ".db.gz") || b.Size == 0 {
		t.Fatalf("backup: %+v", b)
	}
	resp, body = h.do(t, "GET", "/api/v1/system/backups", nil)
	expect(t, "list", resp, body, 200, b.Name)

	resp, data := h.do(t, "GET", "/api/v1/system/backups/"+b.Name, nil, csrf, "1")
	expect(t, "download", resp, nil, 200, "")
	if resp.Header.Get("Content-Type") != "application/gzip" || len(data) < 2 || data[0] != 0x1f || data[1] != 0x8b {
		t.Fatalf("download is not gzip: %s", resp.Header.Get("Content-Type"))
	}

	// restore staging unpacks it; plain files from older versions pass through
	dir := t.TempDir()
	staged := filepath.Join(dir, "restore.db")
	if err := stageBackup(staged, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if v, err := db.Validate(context.Background(), staged); err != nil || v == 0 {
		t.Fatalf("staged backup invalid: %d %v", v, err)
	}
	plain, _ := os.ReadFile(staged)
	again := filepath.Join(dir, "again.db")
	if err := stageBackup(again, bytes.NewReader(plain)); err != nil {
		t.Fatal(err)
	}
	if b2, _ := os.ReadFile(again); !bytes.Equal(b2, plain) {
		t.Fatal("plain backup changed while staging")
	}
	d, err := db.Open(context.Background(), staged)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var n int
	if err := d.R.QueryRow("SELECT COUNT(*) FROM main.sqlite_master WHERE name LIKE 'nvd_%'").Scan(&n); err != nil || n != 0 {
		t.Fatalf("backup contains NVD tables: %d %v", n, err)
	}
}
