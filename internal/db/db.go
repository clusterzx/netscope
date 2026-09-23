// Package db wraps the SQLite database (modernc.org/sqlite, no CGO).
//
// SQLite allows exactly one writer. DB therefore keeps two pools: W with a single
// connection for all writes (serialised in-process instead of fighting over the file
// lock) and R with several connections for concurrent reads (WAL mode).
package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// DB bundles the write and read pools.
type DB struct {
	W    *sql.DB
	R    *sql.DB
	Path string
}

// Open opens (and creates) the database file and applies all migrations.
func Open(ctx context.Context, path string) (*DB, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return nil, fmt.Errorf("create db dir: %w", err)
		}
	}
	w, err := sql.Open("sqlite", dsn(path, false))
	if err != nil {
		return nil, err
	}
	w.SetMaxOpenConns(1)
	w.SetMaxIdleConns(1)
	w.SetConnMaxLifetime(0)
	if err := w.PingContext(ctx); err != nil {
		w.Close()
		return nil, fmt.Errorf("open db %s: %w", path, err)
	}
	d := &DB{W: w, Path: path}
	if err := d.migrate(ctx); err != nil {
		w.Close()
		return nil, err
	}
	r, err := sql.Open("sqlite", dsn(path, true))
	if err != nil {
		w.Close()
		return nil, err
	}
	n := runtime.NumCPU()
	if n < 4 {
		n = 4
	}
	r.SetMaxOpenConns(n)
	r.SetMaxIdleConns(n)
	d.R = r
	return d, nil
}

func dsn(path string, readOnly bool) string {
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(15000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "synchronous(NORMAL)")
	q.Add("_pragma", "foreign_keys(1)")
	q.Add("_pragma", "temp_store(MEMORY)")
	q.Add("_pragma", "cache_size(-16000)")
	if readOnly {
		q.Add("_pragma", "query_only(1)")
	}
	q.Set("_txlock", "immediate")
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	p := filepath.ToSlash(path)
	if !strings.HasPrefix(p, "/") {
		p = "/" + p // windows drive paths: file:/D:/x.db
	}
	return "file:" + p + "?" + q.Encode()
}

// Close checkpoints the WAL into the main file and closes both pools.
func (d *DB) Close() error {
	var errs []error
	if d.R != nil {
		errs = append(errs, d.R.Close())
	}
	if d.W != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := d.W.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)"); err != nil {
			errs = append(errs, fmt.Errorf("wal checkpoint: %w", err))
		}
		errs = append(errs, d.W.Close())
	}
	return errors.Join(errs...)
}

// Checkpoint runs a passive WAL checkpoint.
func (d *DB) Checkpoint(ctx context.Context) error {
	_, err := d.W.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)")
	return err
}

// Tx runs fn inside a write transaction on the single writer connection.
// fn must only use tx (never d.W) to avoid deadlocking the writer pool.
func (d *DB) Tx(ctx context.Context, fn func(tx *sql.Tx) error) (err error) {
	tx, err := d.W.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback()
			panic(p)
		}
		if err != nil {
			_ = tx.Rollback()
			return
		}
		err = tx.Commit()
	}()
	return fn(tx)
}

// Backup writes a consistent copy of the database to dst (VACUUM INTO).
func (d *DB) Backup(ctx context.Context, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return err
	}
	_ = os.Remove(dst)
	_, err := d.W.ExecContext(ctx, "VACUUM INTO ?", dst)
	return err
}

// Size returns the size of the database file plus its WAL in bytes.
func (d *DB) Size() int64 {
	var total int64
	for _, p := range []string{d.Path, d.Path + "-wal"} {
		if st, err := os.Stat(p); err == nil {
			total += st.Size()
		}
	}
	return total
}

// Querier is implemented by *sql.DB and *sql.Tx.
type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Now returns the current time as unix milliseconds.
func Now() int64 { return time.Now().UnixMilli() }

// Ms converts a time to unix milliseconds (0 for the zero time).
func Ms(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UnixMilli()
}

// Time converts unix milliseconds to time.Time (zero for 0).
func Time(ms int64) time.Time {
	if ms == 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms)
}

// NullTime converts a nullable millisecond column to *time.Time.
func NullTime(v sql.NullInt64) *time.Time {
	if !v.Valid || v.Int64 == 0 {
		return nil
	}
	t := time.UnixMilli(v.Int64)
	return &t
}

// NullMs converts *time.Time to a nullable millisecond value.
func NullMs(t *time.Time) any {
	if t == nil || t.IsZero() {
		return nil
	}
	return t.UnixMilli()
}

// JSON marshals v; nil slices become "[]" and nil maps "{}" according to empty.
func JSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "null"
	}
	s := string(b)
	if s == "null" {
		switch v.(type) {
		case []string, []int, []int64, []any:
			return "[]"
		case map[string]string, map[string]any:
			return "{}"
		}
	}
	return s
}

// Unmarshal decodes JSON text into v, ignoring empty input.
func Unmarshal(s string, v any) error {
	if s == "" || s == "null" {
		return nil
	}
	return json.Unmarshal([]byte(s), v)
}

// ErrNotFound is returned by lookups that found no row.
var ErrNotFound = errors.New("not found")

// NotFound maps sql.ErrNoRows to ErrNotFound.
func NotFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

// Placeholders returns "?,?,?" for n parameters.
func Placeholders(n int) string {
	if n <= 0 {
		return ""
	}
	return strings.TrimSuffix(strings.Repeat("?,", n), ",")
}

// Int64Args converts ids to query args.
func Int64Args(ids []int64) []any {
	out := make([]any, len(ids))
	for i, id := range ids {
		out[i] = id
	}
	return out
}

// StringArgs converts strings to query args.
func StringArgs(ss []string) []any {
	out := make([]any, len(ss))
	for i, s := range ss {
		out[i] = s
	}
	return out
}

// Bool converts a bool to the integer representation stored in SQLite.
func Bool(b bool) int {
	if b {
		return 1
	}
	return 0
}
