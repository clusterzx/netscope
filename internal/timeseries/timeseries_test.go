package timeseries

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"netscope/internal/db"
)

func TestAppendQueryDownsample(t *testing.T) {
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "ts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	id, err := SeriesID(ctx, d.W, "icmp.rtt_ms", 0, "", "ms")
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := SeriesID(ctx, d.W, "icmp.rtt_ms", 0, "", "ms")
	if id != id2 {
		t.Fatal("series not reused")
	}
	now := time.Now().Truncate(time.Hour)
	start := now.Add(-3 * time.Hour)
	// one sample per minute for 3 hours: value = minute index
	for i := 0; i < 180; i++ {
		v := float64(i)
		if err := Append(ctx, d.W, id, start.Add(time.Duration(i)*time.Minute), v, v, v+1); err != nil {
			t.Fatal(err)
		}
	}
	pts, res, err := Query(ctx, d.R, id, start, now, 1000)
	if err != nil || res != ResRaw || len(pts) != 180 {
		t.Fatalf("raw query: %v %s %d", err, res, len(pts))
	}
	pts, _, _ = Query(ctx, d.R, id, start, now, 18)
	if len(pts) > 19 {
		t.Fatalf("bucketed query returned %d points", len(pts))
	}
	n5, n1, err := Downsample(ctx, d, now)
	if err != nil {
		t.Fatal(err)
	}
	if n5 == 0 || n1 == 0 {
		t.Fatalf("downsample produced %d/%d rows", n5, n1)
	}
	var cnt, sum int
	if err := d.R.QueryRow("SELECT COUNT(*), SUM(count) FROM ts_5m WHERE series_id = ?", id).Scan(&cnt, &sum); err != nil {
		t.Fatal(err)
	}
	// samples older than now-10min are aggregated: 170 samples in 34 buckets
	if cnt != 34 || sum != 170 {
		t.Fatalf("5m buckets %d samples %d", cnt, sum)
	}
	var avg float64
	_ = d.R.QueryRow("SELECT avg FROM ts_1h WHERE series_id = ? ORDER BY bucket LIMIT 1", id).Scan(&avg)
	if avg != 29.5 { // mean of 0..59
		t.Fatalf("hourly avg %v", avg)
	}
	// idempotent
	n5b, _, _ := Downsample(ctx, d, now)
	if n5b != 0 {
		t.Fatalf("second downsample wrote %d rows", n5b)
	}
	if _, err := Prune(ctx, d, now.Add(8*24*time.Hour), RawRetention, FiveMinRetention, HourRetention); err != nil {
		t.Fatal(err)
	}
	_ = d.R.QueryRow("SELECT COUNT(*) FROM ts_raw").Scan(&cnt)
	if cnt != 0 {
		t.Fatalf("raw not pruned: %d", cnt)
	}
}
