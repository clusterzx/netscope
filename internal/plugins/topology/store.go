package topology

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"netscope/internal/db"
)

// writeStats counts the changes of one reconcile.
type writeStats struct {
	created, updated, removed, kept int
}

// upsertSQL inserts or refreshes an edge of this source; the EXISTS guards skip edges
// whose devices were deleted since the input was read.
const upsertSQL = `INSERT INTO relations(parent_id, child_id, kind, source, parent_port, child_port, label, data, first_seen, last_seen)
	SELECT ?, ?, ?, '` + source + `', ?, ?, '', ?, ?, ?
	WHERE EXISTS (SELECT 1 FROM devices WHERE id = ?) AND EXISTS (SELECT 1 FROM devices WHERE id = ?)
	ON CONFLICT(parent_id, child_id, kind, source) DO UPDATE SET
	parent_port = excluded.parent_port, child_port = excluded.child_port, data = excluded.data, last_seen = excluded.last_seen`

// writeEdges reconciles the relations of source "topology" with the derived edges in one
// transaction: current edges are inserted or refreshed, edges that are no longer derived
// (and not kept by the retention) are deleted. Relations of other sources are never
// touched.
func writeEdges(ctx context.Context, d *db.DB, in *input, res result, now time.Time) (writeStats, error) {
	var st writeStats
	existing := map[edgeKey]bool{}
	var remove []int64
	current := map[edgeKey]bool{}
	for _, e := range res.edges {
		current[e.key()] = true
	}
	for _, r := range in.relations {
		if r.source != source {
			continue
		}
		k := edgeKey{r.parent, r.child, r.kind}
		existing[k] = true
		switch {
		case current[k]:
		case res.keep[r.id]:
			st.kept++
		default:
			remove = append(remove, r.id)
		}
	}
	nowMs := now.UnixMilli()
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		stmt, err := tx.PrepareContext(ctx, upsertSQL)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for _, e := range res.edges {
			data, err := json.Marshal(e.data)
			if err != nil {
				return err
			}
			if _, err := stmt.ExecContext(ctx, e.parent, e.child, e.kind, e.parentPort, e.childPort, string(data), nowMs, nowMs,
				e.parent, e.child); err != nil {
				return err
			}
			if existing[e.key()] {
				st.updated++
			} else {
				st.created++
			}
		}
		for start := 0; start < len(remove); start += 500 {
			chunk := remove[start:min(start+500, len(remove))]
			if _, err := tx.ExecContext(ctx, "DELETE FROM relations WHERE source = '"+source+"' AND id IN ("+db.Placeholders(len(chunk))+")",
				db.Int64Args(chunk)...); err != nil {
				return err
			}
		}
		st.removed = len(remove)
		return nil
	})
	return st, err
}
