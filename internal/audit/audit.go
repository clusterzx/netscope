// Package audit records every manual change (who, when, what, before/after).
package audit

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"netscope/internal/db"
)

// Entry is an audit log entry.
type Entry struct {
	ID         int64     `json:"id"`
	TS         time.Time `json:"ts"`
	Actor      string    `json:"actor"`
	ActorType  string    `json:"actorType"`
	IP         string    `json:"ip"`
	Action     string    `json:"action"`
	EntityType string    `json:"entityType"`
	EntityID   string    `json:"entityId"`
	Summary    string    `json:"summary"`
	Before     any       `json:"before,omitempty"`
	After      any       `json:"after,omitempty"`
}

// Log is the audit log.
type Log struct{ db *db.DB }

// New creates the audit log.
func New(d *db.DB) *Log { return &Log{db: d} }

// Record writes an entry. before/after are marshalled to JSON (nil = omitted).
func (l *Log) Record(ctx context.Context, actor, actorType, ip, action, entityType, entityID, summary string, before, after any) error {
	enc := func(v any) any {
		if v == nil {
			return nil
		}
		b, err := json.Marshal(v)
		if err != nil {
			return nil
		}
		return string(b)
	}
	if actorType == "" {
		actorType = "system"
	}
	_, err := l.db.W.ExecContext(ctx, `INSERT INTO audit_log(ts, actor, actor_type, ip, action, entity_type, entity_id, summary, before, after)
		VALUES (?,?,?,?,?,?,?,?,?,?)`, db.Now(), actor, actorType, ip, action, entityType, entityID, summary, enc(before), enc(after))
	return err
}

// Filter selects entries.
type Filter struct {
	EntityType string
	EntityID   string
	Action     string
	Text       string
	Limit      int
	Offset     int
}

// List returns entries, newest first, and the total count.
func (l *Log) List(ctx context.Context, f Filter) ([]Entry, int, error) {
	var conds []string
	var args []any
	if f.EntityType != "" {
		conds = append(conds, "entity_type = ?")
		args = append(args, f.EntityType)
	}
	if f.EntityID != "" {
		conds = append(conds, "entity_id = ?")
		args = append(args, f.EntityID)
	}
	if f.Action != "" {
		conds = append(conds, "action LIKE ?")
		args = append(args, strings.ReplaceAll(f.Action, "*", "%"))
	}
	if f.Text != "" {
		conds = append(conds, "(summary LIKE ? OR actor LIKE ?)")
		p := "%" + f.Text + "%"
		args = append(args, p, p)
	}
	where := "1=1"
	if len(conds) > 0 {
		where = strings.Join(conds, " AND ")
	}
	var total int
	if err := l.db.R.QueryRowContext(ctx, "SELECT COUNT(*) FROM audit_log WHERE "+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := l.db.R.QueryContext(ctx, `SELECT id, ts, actor, actor_type, ip, action, entity_type, entity_id, summary, before, after
		FROM audit_log WHERE `+where+` ORDER BY id DESC LIMIT ? OFFSET ?`, append(args, limit, f.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var (
			e             Entry
			ts            int64
			before, after sql.NullString
		)
		if err := rows.Scan(&e.ID, &ts, &e.Actor, &e.ActorType, &e.IP, &e.Action, &e.EntityType, &e.EntityID, &e.Summary, &before, &after); err != nil {
			return nil, 0, err
		}
		e.TS = db.Time(ts)
		if before.Valid {
			var v any
			if json.Unmarshal([]byte(before.String), &v) == nil {
				e.Before = v
			}
		}
		if after.Valid {
			var v any
			if json.Unmarshal([]byte(after.String), &v) == nil {
				e.After = v
			}
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}
