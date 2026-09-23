package inventory

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"netscope/internal/netutil"
)

// seed inserts n devices with p open ports each (plus tags and a relation chain) directly.
func seed(t testing.TB, s *Store, n, p int) {
	t.Helper()
	ctx := context.Background()
	now := time.Now().UnixMilli()
	err := s.db.Tx(ctx, func(tx *sql.Tx) error {
		for i := 1; i <= n; i++ {
			ip := fmt.Sprintf("10.%d.%d.%d", i/65536, (i/256)%256, i%256)
			mac := fmt.Sprintf("02:00:00:%02x:%02x:%02x", i/65536, (i/256)%256, i%256)
			if _, err := tx.ExecContext(ctx, `INSERT INTO devices(id, hostname, primary_ip, ip_key, primary_mac, vendor, os, online,
				first_seen, last_seen, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, i, fmt.Sprintf("host-%d", i), ip,
				netutil.IPKey(ip), mac, "Vendor", "Linux", i%3 != 0, now, now, now, now); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO device_ips(device_id, ip, ip_key, mac, source, first_seen, last_seen) VALUES (?,?,?,?,?,?,?)`,
				i, ip, netutil.IPKey(ip), mac, "arpscan", now, now); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO device_macs(mac, device_id, source, first_seen, last_seen) VALUES (?,?,?,?,?)`,
				mac, i, "arpscan", now, now); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO device_tags(device_id, tag) VALUES (?, ?)`, i, fmt.Sprintf("t%d", i%10)); err != nil {
				return err
			}
			for j := 0; j < p; j++ {
				if _, err := tx.ExecContext(ctx, `INSERT INTO ports(device_id, ip, proto, port, state, service, product, version, source, first_seen, last_seen)
					VALUES (?,?,?,?,?,?,?,?,?,?,?)`, i, ip, "tcp", 1000+j, "open", "svc", "prod", "1.0", "nmap", now, now); err != nil {
					return err
				}
			}
			if i > 1 {
				parent := (i-1)/10 + 1
				if parent != i {
					if _, err := tx.ExecContext(ctx, `INSERT INTO relations(parent_id, child_id, kind, source, first_seen, last_seen)
						VALUES (?,?,?,?,?,?)`, parent, i, "switch_port", "topology", now, now); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestPerformanceTargets checks the non-functional requirements: device list with 500
// devices × 50 ports < 100 ms, topology graph with 500 nodes < 500 ms (from the database).
func TestPerformanceTargets(t *testing.T) {
	if testing.Short() {
		t.Skip("performance test (make test runs it separately)")
	}
	ctx := context.Background()
	s, _ := newTestStore(t)
	seed(t, s, 500, 50)
	// warm up (statement cache, page cache)
	if _, err := s.List(ctx, ListOptions{WithPorts: true}); err != nil {
		t.Fatal(err)
	}
	// best of 5 runs; when `go test ./...` runs other test binaries in parallel the CPU
	// is contended, so up to 4 rounds (with a pause) are measured before giving up
	best := func(target time.Duration, fn func() error) time.Duration {
		min := time.Hour
		for round := 0; round < 4 && min > target; round++ {
			if round > 0 {
				time.Sleep(time.Second)
			}
			for i := 0; i < 5; i++ {
				start := time.Now()
				if err := fn(); err != nil {
					t.Fatal(err)
				}
				if d := time.Since(start); d < min {
					min = d
				}
			}
		}
		return min
	}
	list := best(100*time.Millisecond, func() error {
		res, err := s.List(ctx, ListOptions{Sort: "name", WithPorts: true})
		if err == nil && (res.Total != 500 || res.Items[0].PortCount != 50 || len(res.Items[0].Ports) != 50) {
			err = fmt.Errorf("unexpected result %d/%d", res.Total, res.Items[0].PortCount)
		}
		return err
	})
	filtered := best(100*time.Millisecond, func() error {
		_, err := s.List(ctx, ListOptions{Query: "tag:t3 port:1025 os:linux seen<24h", Sort: "-ports"})
		return err
	})
	graph := best(500*time.Millisecond, func() error {
		g, err := s.Graph(ctx, GraphFilter{})
		if err == nil && len(g.Nodes) != 500 {
			err = fmt.Errorf("nodes %d", len(g.Nodes))
		}
		return err
	})
	t.Logf("device list %v, filtered list %v, topology %v", list, filtered, graph)
	if list > 100*time.Millisecond {
		t.Errorf("device list took %v (target < 100 ms)", list)
	}
	if filtered > 100*time.Millisecond {
		t.Errorf("filtered device list took %v (target < 100 ms)", filtered)
	}
	if graph > 500*time.Millisecond {
		t.Errorf("topology graph took %v (target < 500 ms)", graph)
	}
}

// the port count variant must really drop the subquery (and keep the column)
func TestDeviceSelectWithoutPortCount(t *testing.T) {
	if deviceSelectNoPortCount == deviceSelect || !strings.Contains(deviceSelectNoPortCount, "0 AS port_count") {
		t.Fatal("port count subquery not replaced")
	}
}
