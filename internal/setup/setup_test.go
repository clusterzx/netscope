package setup

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"netscope/internal/db"
)

func TestCodeLifecycle(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	d, err := db.Open(ctx, filepath.Join(dir, "s.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s, err := Load(ctx, d, dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s.Pending() {
		t.Fatal("new database must be pending")
	}
	if s.CheckCode("") {
		t.Fatal("no code prepared yet: nothing matches")
	}
	code, err := s.PrepareCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 14 || strings.Count(code, "-") != 2 {
		t.Fatalf("code format %q", code)
	}
	b, err := os.ReadFile(filepath.Join(dir, CodeFile))
	if err != nil || strings.TrimSpace(string(b)) != code {
		t.Fatalf("code file %q, %v", b, err)
	}
	if info, _ := os.Stat(filepath.Join(dir, CodeFile)); info.Mode().Perm() != 0o600 {
		t.Fatalf("code file mode %v", info.Mode())
	}
	if !s.CheckCode(strings.ToLower(strings.ReplaceAll(code, "-", " "))) {
		t.Fatal("code must match regardless of case and separators")
	}
	if s.CheckCode(code[:13]) || s.CheckCode("AAAA-AAAA-AAAA") {
		t.Fatal("wrong code matched")
	}
	// a restart keeps the code of the file
	s2, _ := Load(ctx, d, dir)
	again, _ := s2.PrepareCode()
	if again != code {
		t.Fatalf("restart changed the code: %s → %s", code, again)
	}
	if err := s2.Complete(ctx, ModeWizard); err != nil {
		t.Fatal(err)
	}
	if s2.Pending() || s2.CheckCode(code) {
		t.Fatal("completed setup must not accept the code")
	}
	if _, err := os.Stat(filepath.Join(dir, CodeFile)); !os.IsNotExist(err) {
		t.Fatal("code file must be removed")
	}
	s3, _ := Load(ctx, d, dir)
	if s3.Pending() || s3.State().Mode != ModeWizard {
		t.Fatalf("state not stored: %+v", s3.State())
	}
}

// An existing installation (users present) counts as set up after the migration.
func TestMigrationMarksExistingInstallations(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	path := filepath.Join(dir, "m.db")
	d, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	// simulate a database from before the wizard: user present, migration 13 not applied
	if _, err := d.W.ExecContext(ctx, `INSERT INTO users(username, password_hash, role_id, created_at, updated_at)
		VALUES ('admin', 'x', (SELECT id FROM roles WHERE builtin = 'admin'), 1, 1)`); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.ExecContext(ctx, "DELETE FROM settings WHERE key = 'setup'"); err != nil {
		t.Fatal(err)
	}
	if _, err := d.W.ExecContext(ctx, "DELETE FROM schema_migrations WHERE version = 13"); err != nil {
		t.Fatal(err)
	}
	d.Close()
	d, err = db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	s, err := Load(ctx, d, dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Pending() || s.State().Mode != ModeMigration {
		t.Fatalf("existing installation must count as set up: %+v", s.State())
	}
}
