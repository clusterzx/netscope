package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Migration is one embedded SQL migration file.
type Migration struct {
	Version int
	Name    string
	SQL     string
}

// Migrations returns the embedded migrations sorted by version.
func Migrations() ([]Migration, error) {
	entries, err := fs.ReadDir(migrationsFS, "migrations")
	if err != nil {
		return nil, err
	}
	var out []Migration
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".sql") {
			continue
		}
		prefix, _, ok := strings.Cut(name, "_")
		if !ok {
			return nil, fmt.Errorf("migration %s: missing version prefix", name)
		}
		v, err := strconv.Atoi(prefix)
		if err != nil {
			return nil, fmt.Errorf("migration %s: bad version: %w", name, err)
		}
		b, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return nil, err
		}
		out = append(out, Migration{Version: v, Name: name, SQL: string(b)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	for i := 1; i < len(out); i++ {
		if out[i].Version == out[i-1].Version {
			return nil, fmt.Errorf("duplicate migration version %d", out[i].Version)
		}
	}
	return out, nil
}

func (d *DB) migrate(ctx context.Context) error {
	if _, err := d.W.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at INTEGER NOT NULL)`); err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}
	migs, err := Migrations()
	if err != nil {
		return err
	}
	applied := map[int]bool{}
	rows, err := d.W.QueryContext(ctx, "SELECT version FROM schema_migrations")
	if err != nil {
		return err
	}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			rows.Close()
			return err
		}
		applied[v] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, m := range migs {
		if applied[m.Version] {
			continue
		}
		err := d.Tx(ctx, func(tx *sql.Tx) error {
			if _, err := tx.ExecContext(ctx, m.SQL); err != nil {
				return err
			}
			_, err := tx.ExecContext(ctx, "INSERT INTO schema_migrations(version, name, applied_at) VALUES (?,?,?)",
				m.Version, m.Name, Now())
			return err
		})
		if err != nil {
			return fmt.Errorf("apply migration %s: %w", m.Name, err)
		}
	}
	return nil
}

// SchemaVersion returns the highest applied migration version.
func (d *DB) SchemaVersion(ctx context.Context) (int, error) {
	var v sql.NullInt64
	err := d.R.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&v)
	return int(v.Int64), err
}

// Validate checks that the file at path is a NetScope database that passes integrity checks.
// It is used before restoring a backup.
func Validate(ctx context.Context, path string) (int, error) {
	c, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		return 0, err
	}
	defer c.Close()
	var res string
	if err := c.QueryRowContext(ctx, "PRAGMA integrity_check").Scan(&res); err != nil {
		return 0, fmt.Errorf("not a sqlite database: %w", err)
	}
	if res != "ok" {
		return 0, fmt.Errorf("integrity check failed: %s", res)
	}
	var v sql.NullInt64
	if err := c.QueryRowContext(ctx, "SELECT MAX(version) FROM schema_migrations").Scan(&v); err != nil {
		return 0, fmt.Errorf("not a NetScope database: %w", err)
	}
	migs, err := Migrations()
	if err != nil {
		return 0, err
	}
	if len(migs) > 0 && int(v.Int64) > migs[len(migs)-1].Version {
		return 0, fmt.Errorf("backup schema version %d is newer than this build (%d)", v.Int64, migs[len(migs)-1].Version)
	}
	return int(v.Int64), nil
}
