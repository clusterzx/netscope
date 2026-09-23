// Package timeseries stores metric samples (ping latency, packet loss, check latency)
// with three resolutions: raw (7 days), 5-minute aggregates (90 days) and hourly
// aggregates (1 year). Downsample and Prune are run by the cleanup processor.
package timeseries

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"netscope/internal/db"
	"netscope/internal/settings"
)

// Default retentions.
const (
	RawRetention     = 7 * 24 * time.Hour
	FiveMinRetention = 90 * 24 * time.Hour
	HourRetention    = 365 * 24 * time.Hour
	fiveMin          = int64(5 * time.Minute / time.Millisecond)
	hour             = int64(time.Hour / time.Millisecond)
	// lag protects buckets that may still receive late samples.
	lag5m = int64(10 * time.Minute / time.Millisecond)
	lag1h = int64(time.Hour / time.Millisecond)
)

// Series describes a stored series.
type Series struct {
	ID       int64  `json:"id"`
	Metric   string `json:"metric"`
	DeviceID int64  `json:"deviceId,omitempty"`
	Key      string `json:"key,omitempty"`
	Unit     string `json:"unit,omitempty"`
}

// Point is one (possibly aggregated) sample.
type Point struct {
	T     time.Time `json:"t"`
	Min   float64   `json:"min"`
	Avg   float64   `json:"avg"`
	Max   float64   `json:"max"`
	Count int       `json:"count"`
}

// SeriesID returns the id of a series, creating it if needed.
func SeriesID(ctx context.Context, q db.Querier, metric string, deviceID int64, key, unit string) (int64, error) {
	var dev any
	if deviceID > 0 {
		dev = deviceID
	}
	var id int64
	err := q.QueryRowContext(ctx, "SELECT id FROM ts_series WHERE metric = ? AND IFNULL(device_id, 0) = ? AND key = ?",
		metric, deviceID, key).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return 0, err
	}
	res, err := q.ExecContext(ctx, "INSERT INTO ts_series(metric, device_id, key, unit, created_at) VALUES (?,?,?,?,?)",
		metric, dev, key, unit, db.Now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// Append stores a raw sample.
func Append(ctx context.Context, q db.Querier, seriesID int64, t time.Time, min, avg, max float64) error {
	_, err := q.ExecContext(ctx, "INSERT OR REPLACE INTO ts_raw(series_id, ts, min, avg, max) VALUES (?,?,?,?,?)",
		seriesID, t.UnixMilli(), min, avg, max)
	return err
}

// ListSeries returns the series of a device (deviceID 0 = global series).
func ListSeries(ctx context.Context, q db.Querier, deviceID int64) ([]Series, error) {
	rows, err := q.QueryContext(ctx, "SELECT id, metric, IFNULL(device_id, 0), key, unit FROM ts_series WHERE IFNULL(device_id, 0) = ? ORDER BY metric, key", deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Series{}
	for rows.Next() {
		var s Series
		if err := rows.Scan(&s.ID, &s.Metric, &s.DeviceID, &s.Key, &s.Unit); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Resolution names.
const (
	ResRaw = "raw"
	Res5m  = "5m"
	Res1h  = "1h"
)

// Query returns points of a series in [from, to]. The table is chosen by range and
// retention; points are further bucketed so that at most maxPoints are returned.
func Query(ctx context.Context, q db.Querier, seriesID int64, from, to time.Time, maxPoints int) ([]Point, string, error) {
	if maxPoints <= 0 {
		maxPoints = 500
	}
	now := time.Now()
	span := to.Sub(from)
	res := ResRaw
	switch {
	case from.Before(now.Add(-FiveMinRetention)) || span > 60*24*time.Hour:
		res = Res1h
	case from.Before(now.Add(-RawRetention)) || span > 2*24*time.Hour:
		res = Res5m
	}
	fromMs, toMs := from.UnixMilli(), to.UnixMilli()
	bucket := (toMs - fromMs) / int64(maxPoints)
	var query string
	switch res {
	case ResRaw:
		if bucket < 1 {
			bucket = 1
		}
		query = `SELECT (ts / ?) * ? AS b, MIN(min), AVG(avg), MAX(max), COUNT(*) FROM ts_raw
			WHERE series_id = ? AND ts BETWEEN ? AND ? GROUP BY b ORDER BY b`
	case Res5m:
		if bucket < fiveMin {
			bucket = fiveMin
		}
		query = `SELECT (bucket / ?) * ? AS b, MIN(min), SUM(avg * count) / SUM(count), MAX(max), SUM(count) FROM ts_5m
			WHERE series_id = ? AND bucket BETWEEN ? AND ? GROUP BY b ORDER BY b`
	default:
		if bucket < hour {
			bucket = hour
		}
		query = `SELECT (bucket / ?) * ? AS b, MIN(min), SUM(avg * count) / SUM(count), MAX(max), SUM(count) FROM ts_1h
			WHERE series_id = ? AND bucket BETWEEN ? AND ? GROUP BY b ORDER BY b`
	}
	rows, err := q.QueryContext(ctx, query, bucket, bucket, seriesID, fromMs, toMs)
	if err != nil {
		return nil, res, err
	}
	defer rows.Close()
	out := []Point{}
	for rows.Next() {
		var (
			b          int64
			p          Point
			mn, av, mx sql.NullFloat64
		)
		if err := rows.Scan(&b, &mn, &av, &mx, &p.Count); err != nil {
			return nil, res, err
		}
		p.T, p.Min, p.Avg, p.Max = time.UnixMilli(b), mn.Float64, av.Float64, mx.Float64
		out = append(out, p)
	}
	return out, res, rows.Err()
}

const (
	wm5mKey = "timeseries.watermark.5m"
	wm1hKey = "timeseries.watermark.1h"
)

// Downsample aggregates raw samples into 5-minute buckets and those into hourly buckets.
// It is idempotent: watermarks remember the last completed bucket.
func Downsample(ctx context.Context, d *db.DB, now time.Time) (int64, int64, error) {
	var n5, n1 int64
	err := d.Tx(ctx, func(tx *sql.Tx) error {
		var wm5 int64
		if _, err := settings.GetJSON(ctx, tx, wm5mKey, &wm5); err != nil {
			return err
		}
		end5 := (now.UnixMilli() - lag5m) / fiveMin * fiveMin
		if end5 > wm5 {
			res, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO ts_5m(series_id, bucket, min, avg, max, count)
				SELECT series_id, (ts / ?) * ? AS b, MIN(min), AVG(avg), MAX(max), COUNT(*)
				FROM ts_raw WHERE ts >= ? AND ts < ? GROUP BY series_id, b`, fiveMin, fiveMin, wm5, end5)
			if err != nil {
				return fmt.Errorf("downsample 5m: %w", err)
			}
			n5, _ = res.RowsAffected()
			if err := settings.SetJSON(ctx, tx, wm5mKey, end5); err != nil {
				return err
			}
		}
		var wm1 int64
		if _, err := settings.GetJSON(ctx, tx, wm1hKey, &wm1); err != nil {
			return err
		}
		end1 := (now.UnixMilli() - lag1h) / hour * hour
		if end1 > end5 {
			end1 = end5 / hour * hour
		}
		if end1 > wm1 {
			res, err := tx.ExecContext(ctx, `INSERT OR REPLACE INTO ts_1h(series_id, bucket, min, avg, max, count)
				SELECT series_id, (bucket / ?) * ? AS b, MIN(min), SUM(avg * count) / SUM(count), MAX(max), SUM(count)
				FROM ts_5m WHERE bucket >= ? AND bucket < ? GROUP BY series_id, b`, hour, hour, wm1, end1)
			if err != nil {
				return fmt.Errorf("downsample 1h: %w", err)
			}
			n1, _ = res.RowsAffected()
			if err := settings.SetJSON(ctx, tx, wm1hKey, end1); err != nil {
				return err
			}
		}
		return nil
	})
	return n5, n1, err
}

// Prune deletes samples older than the retentions and removes empty series.
func Prune(ctx context.Context, d *db.DB, now time.Time, raw, fiveMinutes, hourly time.Duration) (int64, error) {
	var total int64
	for _, st := range []struct {
		q   string
		ret time.Duration
	}{
		{"DELETE FROM ts_raw WHERE ts < ?", raw},
		{"DELETE FROM ts_5m WHERE bucket < ?", fiveMinutes},
		{"DELETE FROM ts_1h WHERE bucket < ?", hourly},
	} {
		res, err := d.W.ExecContext(ctx, st.q, now.Add(-st.ret).UnixMilli())
		if err != nil {
			return total, err
		}
		n, _ := res.RowsAffected()
		total += n
	}
	_, err := d.W.ExecContext(ctx, `DELETE FROM ts_series WHERE created_at < ?
		AND NOT EXISTS (SELECT 1 FROM ts_raw r WHERE r.series_id = ts_series.id)
		AND NOT EXISTS (SELECT 1 FROM ts_5m f WHERE f.series_id = ts_series.id)
		AND NOT EXISTS (SELECT 1 FROM ts_1h h WHERE h.series_id = ts_series.id)`, now.Add(-24*time.Hour).UnixMilli())
	return total, err
}
