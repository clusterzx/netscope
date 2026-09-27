package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// The NVD mirror (nvd_cves, nvd_cpe_matches, nvd_feeds – public data of several hundred
// MB) lives in its own file next to the database and is attached to every connection as
// schema "nvd". SQLite resolves unqualified table names in attached databases too, so the
// CVE plugin's queries are unchanged. Backups (VACUUM INTO) copy only the main database:
// after a restore the mirror is still in place, a new installation downloads it again.

// nvdVersion is the schema version of the mirror file; a different version drops the
// tables (the CVE plugin downloads the mirror again).
const nvdVersion = 1

// walLimit is the size a WAL is truncated to when it restarts after a checkpoint.
const walLimit = 64 << 20

// nvdTables in the order they are moved and dropped.
var nvdTables = []string{"nvd_cves", "nvd_cpe_matches", "nvd_feeds", "nvd_kev", "nvd_epss"}

const nvdTablesSQL = `
CREATE TABLE IF NOT EXISTS nvd.nvd_cves (
    id            TEXT PRIMARY KEY,
    published     INTEGER,
    last_modified INTEGER,
    status        TEXT NOT NULL DEFAULT '',
    cvss_score    REAL,
    cvss_vector   TEXT NOT NULL DEFAULT '',
    cvss_version  TEXT NOT NULL DEFAULT '',
    severity      TEXT NOT NULL DEFAULT '',
    description   TEXT NOT NULL DEFAULT '',
    refs          TEXT NOT NULL DEFAULT '[]',
    cwes          TEXT NOT NULL DEFAULT '[]'
);
CREATE TABLE IF NOT EXISTS nvd.nvd_cpe_matches (
    cve_id     TEXT NOT NULL,
    part       TEXT NOT NULL,
    vendor     TEXT NOT NULL,
    product    TEXT NOT NULL,
    version    TEXT NOT NULL,
    upd        TEXT NOT NULL DEFAULT '*',
    start_incl TEXT NOT NULL DEFAULT '',
    start_excl TEXT NOT NULL DEFAULT '',
    end_incl   TEXT NOT NULL DEFAULT '',
    end_excl   TEXT NOT NULL DEFAULT '',
    vulnerable INTEGER NOT NULL DEFAULT 1
);
CREATE TABLE IF NOT EXISTS nvd.nvd_feeds (
    name          TEXT PRIMARY KEY,
    last_modified TEXT NOT NULL DEFAULT '',
    sha256        TEXT NOT NULL DEFAULT '',
    size          INTEGER NOT NULL DEFAULT 0,
    cve_count     INTEGER NOT NULL DEFAULT 0,
    synced_at     INTEGER,
    status        TEXT NOT NULL DEFAULT '',
    error         TEXT NOT NULL DEFAULT ''
);
-- CISA Known Exploited Vulnerabilities; dates as YYYY-MM-DD like the catalog
CREATE TABLE IF NOT EXISTS nvd.nvd_kev (
    cve_id      TEXT PRIMARY KEY,
    vendor      TEXT NOT NULL DEFAULT '',
    product     TEXT NOT NULL DEFAULT '',
    name        TEXT NOT NULL DEFAULT '',
    description TEXT NOT NULL DEFAULT '',
    action      TEXT NOT NULL DEFAULT '',
    date_added  TEXT NOT NULL DEFAULT '',
    due_date    TEXT NOT NULL DEFAULT '',
    ransomware  INTEGER NOT NULL DEFAULT 0,
    notes       TEXT NOT NULL DEFAULT ''
);
-- FIRST EPSS: probability of exploitation within 30 days (0..1) and its percentile
CREATE TABLE IF NOT EXISTS nvd.nvd_epss (
    cve_id     TEXT PRIMARY KEY,
    score      REAL NOT NULL,
    percentile REAL NOT NULL
) WITHOUT ROWID;`

// indexes are created after a move (faster than maintaining them while copying)
const nvdIndexSQL = `
CREATE INDEX IF NOT EXISTS nvd.nvd_cves_score ON nvd_cves(cvss_score);
CREATE INDEX IF NOT EXISTS nvd.nvd_cpe_matches_vp ON nvd_cpe_matches(vendor, product);
CREATE INDEX IF NOT EXISTS nvd.nvd_cpe_matches_cve ON nvd_cpe_matches(cve_id);`

// NVDPath returns the file of the NVD mirror that belongs to a database file
// (netscope.db → netscope-nvd.db).
func NVDPath(path string) string {
	return strings.TrimSuffix(path, filepath.Ext(path)) + "-nvd.db"
}

var sqliteDriver = func() driver.Driver {
	d, _ := sql.Open("sqlite", "")
	defer d.Close()
	return d.Driver()
}()

// connector opens a connection and attaches the NVD mirror.
type connector struct {
	dsn      string
	nvd      string
	readOnly bool
}

func (c *connector) Driver() driver.Driver { return sqliteDriver }

func (c *connector) Connect(ctx context.Context) (driver.Conn, error) {
	conn, err := sqliteDriver.Open(c.dsn)
	if err != nil {
		return nil, err
	}
	ex, ok := conn.(driver.ExecerContext)
	if !ok {
		_ = conn.Close()
		return nil, fmt.Errorf("sqlite: connection cannot execute statements")
	}
	if _, err := ex.ExecContext(ctx, "ATTACH DATABASE ? AS nvd", []driver.NamedValue{{Ordinal: 1, Value: c.nvd}}); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("NVD-Spiegel %s anhängen: %w", c.nvd, err)
	}
	if !c.readOnly {
		for _, q := range []string{"PRAGMA nvd.journal_mode=WAL", "PRAGMA nvd.synchronous=NORMAL",
			fmt.Sprintf("PRAGMA nvd.journal_size_limit=%d", walLimit)} {
			if _, err := ex.ExecContext(ctx, q, nil); err != nil {
				_ = conn.Close()
				return nil, fmt.Errorf("NVD-Spiegel: %w", err)
			}
		}
	}
	return conn, nil
}

// ensureNVD creates the mirror tables and moves the mirror out of the main database of an
// older installation (or a restored older backup).
func (d *DB) ensureNVD(ctx context.Context) error {
	var ver int
	if err := d.W.QueryRowContext(ctx, "PRAGMA nvd.user_version").Scan(&ver); err != nil {
		return err
	}
	if ver != 0 && ver != nvdVersion {
		for _, t := range nvdTables {
			if _, err := d.W.ExecContext(ctx, "DROP TABLE IF EXISTS nvd."+t); err != nil {
				return err
			}
		}
	}
	if _, err := d.W.ExecContext(ctx, nvdTablesSQL); err != nil {
		return fmt.Errorf("NVD-Spiegel anlegen: %w", err)
	}
	var inMain int
	if err := d.W.QueryRowContext(ctx, `SELECT COUNT(*) FROM main.sqlite_master WHERE type = 'table'
		AND name IN ('nvd_cves', 'nvd_cpe_matches', 'nvd_feeds')`).Scan(&inMain); err != nil {
		return err
	}
	if inMain > 0 {
		if err := d.moveNVD(ctx); err != nil {
			return fmt.Errorf("NVD-Spiegel verschieben: %w", err)
		}
	}
	if _, err := d.W.ExecContext(ctx, nvdIndexSQL); err != nil {
		return fmt.Errorf("NVD-Indizes: %w", err)
	}
	_, err := d.W.ExecContext(ctx, fmt.Sprintf("PRAGMA nvd.user_version = %d", nvdVersion))
	return err
}

// moveNVD copies the mirror tables of the main database into the mirror file (unless it
// already holds a mirror), drops them from the main database and shrinks it. A new
// database only drops the empty tables migration 0001 creates.
func (d *DB) moveNVD(ctx context.Context) error {
	var have, old int
	if err := d.W.QueryRowContext(ctx, "SELECT COUNT(*) FROM nvd.nvd_cves").Scan(&have); err != nil {
		return err
	}
	if err := d.W.QueryRowContext(ctx, "SELECT COUNT(*) FROM main.sqlite_master WHERE type = 'table' AND name = 'nvd_cves'").Scan(&old); err != nil {
		return err
	}
	if old > 0 {
		if err := d.W.QueryRowContext(ctx, "SELECT EXISTS (SELECT 1 FROM main.nvd_cves)").Scan(&old); err != nil {
			return err
		}
	}
	start := time.Now()
	if have == 0 && old > 0 {
		slog.Info("NVD-Spiegel wird einmalig aus der Datenbank in eine eigene Datei verschoben", "file", NVDPath(d.Path))
	}
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		for _, t := range nvdTables {
			var exists int
			if err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM main.sqlite_master WHERE type = 'table' AND name = ?", t).Scan(&exists); err != nil {
				return err
			}
			if exists == 0 {
				continue
			}
			if have == 0 {
				if _, err := tx.ExecContext(ctx, "INSERT INTO nvd."+t+" SELECT * FROM main."+t); err != nil {
					return err
				}
			}
			if _, err := tx.ExecContext(ctx, "DROP TABLE main."+t); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil || old == 0 {
		return err
	}
	// give the space back to the file system
	if _, err := d.W.ExecContext(ctx, "VACUUM main"); err != nil {
		return fmt.Errorf("vacuum: %w", err)
	}
	slog.Info("NVD-Spiegel liegt in eigener Datei", "file", NVDPath(d.Path), "dauer", time.Since(start).Round(time.Second).String(),
		"datenbank_bytes", d.Size())
	return nil
}

// NVDSize returns the size of the NVD mirror file plus its WAL in bytes.
func (d *DB) NVDSize() int64 {
	var total int64
	for _, p := range []string{NVDPath(d.Path), NVDPath(d.Path) + "-wal"} {
		if st, err := os.Stat(p); err == nil {
			total += st.Size()
		}
	}
	return total
}
