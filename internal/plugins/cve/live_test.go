package cve

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin/plugintest"
)

// TestLiveSync runs a real sync against the NVD feeds. It needs network access and
// several minutes, so it only runs with NETSCOPE_NVD_LIVE=1:
//
//	NETSCOPE_NVD_LIVE=1 NETSCOPE_NVD_START_YEAR=2002 NETSCOPE_NVD_DB=/tmp/nvd.db go test -run TestLiveSync -v -timeout 2h ./internal/plugins/cve/
func TestLiveSync(t *testing.T) {
	if os.Getenv("NETSCOPE_NVD_LIVE") != "1" {
		t.Skip("NETSCOPE_NVD_LIVE=1 setzen, um gegen die echten NVD-Feeds zu synchronisieren")
	}
	ctx := context.Background()
	path := os.Getenv("NETSCOPE_NVD_DB")
	if path == "" {
		path = filepath.Join(t.TempDir(), "live.db")
	}
	d, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	startYear := firstFeedYear
	if v := os.Getenv("NETSCOPE_NVD_START_YEAR"); v != "" {
		if startYear, err = strconv.Atoi(v); err != nil {
			t.Fatal(err)
		}
	}
	p := New()
	rc, _, _ := plugintest.RunContext(t, p, map[string]any{"start_year": startYear})
	rc.DB = d
	before := d.Size()

	var peakHeap, peakSys atomic.Uint64
	stop := make(chan struct{})
	go func() {
		var ms runtime.MemStats
		tick := time.NewTicker(200 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-stop:
				return
			case <-tick.C:
				runtime.ReadMemStats(&ms)
				if ms.HeapAlloc > peakHeap.Load() {
					peakHeap.Store(ms.HeapAlloc)
				}
				if ms.Sys > peakSys.Load() {
					peakSys.Store(ms.Sys)
				}
			}
		}
	}()
	start := time.Now()
	st, err := p.sync(ctx, rc, loadConfig(rc.Settings), false)
	close(stop)
	if err != nil {
		t.Fatalf("sync: %v", err)
	}
	dur := time.Since(start)
	if err := d.Checkpoint(ctx); err != nil {
		t.Log(err)
	}
	_, _ = d.W.ExecContext(ctx, "PRAGMA wal_checkpoint(TRUNCATE)")
	var cves, matches int64
	_ = d.R.QueryRow("SELECT COUNT(*) FROM nvd_cves").Scan(&cves)
	_ = d.R.QueryRow("SELECT COUNT(*) FROM nvd_cpe_matches").Scan(&matches)
	t.Logf("mode=%s feeds checked=%d downloaded=%d download=%.1f MB read=%d written=%d cves=%d cpe_matches=%d",
		st.Mode, st.Checked, st.Downloaded, float64(st.Bytes)/1e6, st.Read, st.Written, cves, matches)
	t.Logf("duration=%s db_before=%.1f MB db_after=%.1f MB peak_heap=%.1f MB peak_sys=%.1f MB",
		dur.Round(time.Second), float64(before)/1e6, float64(d.Size())/1e6, float64(peakHeap.Load())/1e6, float64(peakSys.Load())/1e6)

	// second run: incremental
	start = time.Now()
	st, err = p.sync(ctx, rc, loadConfig(rc.Settings), false)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	t.Logf("second sync: mode=%s downloaded=%d written=%d changed=%d duration=%s", st.Mode, st.Downloaded, st.Written,
		len(st.Changed), time.Since(start).Round(time.Millisecond))
}

// TestLiveMatch matches 500 devices with realistic inventories against a fully synced
// mirror (NETSCOPE_NVD_DB, e.g. from TestLiveSync; the devices are added to that file):
//
//	NETSCOPE_NVD_LIVE=1 NETSCOPE_NVD_DB=/tmp/nvd.db go test -run TestLiveMatch -v ./internal/plugins/cve/
func TestLiveMatch(t *testing.T) {
	path := os.Getenv("NETSCOPE_NVD_DB")
	if os.Getenv("NETSCOPE_NVD_LIVE") != "1" || path == "" {
		t.Skip("NETSCOPE_NVD_LIVE=1 und NETSCOPE_NVD_DB (synchronisierte Datenbank) setzen")
	}
	ctx := context.Background()
	d, err := db.Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if empty, _ := mirrorEmpty(ctx, d); empty {
		t.Skip("NVD-Kopie ist leer")
	}
	now := time.Now().UnixMilli()
	services := [][3]string{
		{"OpenSSH", "9.6p1 Ubuntu 3ubuntu13.5", "cpe:/a:openbsd:openssh:9.6p1"},
		{"OpenSSH", "8.9p1 Ubuntu 3ubuntu0.10", "cpe:/a:openbsd:openssh:8.9p1"},
		{"OpenSSH", "7.4", "cpe:/a:openbsd:openssh:7.4"},
		{"Dropbear sshd", "2020.81", "cpe:/a:matt_johnston:dropbear_ssh_server:2020.81"},
		{"nginx", "1.18.0", "cpe:/a:igor_sysoev:nginx:1.18.0"},
		{"nginx", "1.25.3", "cpe:/a:igor_sysoev:nginx:1.25.3"},
		{"Apache httpd", "2.4.52", "cpe:/a:apache:http_server:2.4.52"},
		{"lighttpd", "1.4.59", "cpe:/a:lighttpd:lighttpd:1.4.59"},
		{"dnsmasq", "2.85", "cpe:/a:thekelleys:dnsmasq:2.85"},
		{"Samba smbd", "4.6.2", "cpe:/a:samba:samba:4.6.2"},
		{"MySQL", "5.7.33", "cpe:/a:mysql:mysql:5.7.33"},
		{"PostgreSQL DB", "13.4", "cpe:/a:postgresql:postgresql:13.4"},
	}
	pkgs := [][2]string{
		{"openssh-server", "1:9.6p1-3ubuntu13.5"}, {"libssl3t64", "3.0.13-0ubuntu3.4"}, {"curl", "8.5.0-2ubuntu10.6"},
		{"libc6", "2.39-0ubuntu8.3"}, {"sudo", "1.9.15p5-3ubuntu5.24.04.1"}, {"bash", "5.2.21-2ubuntu4"},
		{"systemd", "255.4-1ubuntu8.4"}, {"vim-common", "2:9.1.0016-1ubuntu7.3"}, {"git", "1:2.43.0-1ubuntu7.1"},
		{"xz-utils", "5.6.1+really5.4.5-1build0.1"}, {"zlib1g", "1:1.3.dfsg-3.1ubuntu2.1"}, {"libexpat1", "2.6.1-2build1"},
		{"libsqlite3-0", "3.45.1-1ubuntu2"}, {"python3.12", "3.12.3-1ubuntu0.1"}, {"perl-base", "5.38.2-3.2build2"},
		{"polkitd", "124-2ubuntu1"}, {"linux-image-6.8.0-45-generic", "6.8.0-45.45"}, {"docker.io", "24.0.7-0ubuntu4.1"},
		{"containerd", "1.7.12-0ubuntu4.1"}, {"runc", "1.1.12-0ubuntu3.1"}, {"libxml2", "2.9.14+dfsg-1.3ubuntu3"},
		{"tar", "1.35+dfsg-3build1"}, {"wget", "1.21.4-1ubuntu4.1"}, {"less", "590-2ubuntu2.1"}, {"rsync", "3.2.7-1ubuntu1"},
		{"libgnutls30t64", "3.8.3-1.1ubuntu3.2"}, {"libkrb5-3", "1.20.1-6ubuntu2.1"}, {"libpam0g", "1.5.3-5ubuntu5.1"},
		{"util-linux", "2.39.3-9ubuntu6.1"}, {"e2fsprogs", "1.47.0-2.4~exp1ubuntu4.1"}, {"libarchive13t64", "3.7.2-2ubuntu0.1"},
		{"gnupg", "2.4.4-2ubuntu17"}, {"libgcrypt20", "1.10.3-2build1"}, {"tcpdump", "4.99.4-3ubuntu4"},
	}
	tx, err := d.W.Begin()
	if err != nil {
		t.Fatal(err)
	}
	var first int64
	for i := 0; i < 500; i++ {
		res, err := tx.Exec(`INSERT INTO devices(display_name, created_source, first_seen, state, created_at, updated_at) VALUES (?, 'live', ?, 'known', ?, ?)`,
			"live-"+strconv.Itoa(i), now, now, now)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		if i == 0 {
			first = id
		}
		for k := 0; k < 3; k++ {
			s := services[(i+k*5)%len(services)]
			if _, err := tx.Exec(`INSERT INTO ports(device_id, ip, proto, port, state, product, version, cpes, source, first_seen, last_seen)
				VALUES (?, ?, 'tcp', ?, 'open', ?, ?, ?, 'nmap', ?, ?)`, id, "10.9."+strconv.Itoa(i/250)+"."+strconv.Itoa(i%250), 20+k,
				s[0], s[1], `["`+s[2]+`"]`, now, now); err != nil {
				t.Fatal(err)
			}
		}
		if i%2 == 0 { // every second device is an SSH-inventoried Ubuntu host
			for _, pk := range pkgs {
				if _, err := tx.Exec(`INSERT INTO packages(device_id, manager, name, version, source, first_seen, last_seen) VALUES (?, 'dpkg', ?, ?, 'ssh', ?, ?)`,
					id, pk[0], pk[1], now, now); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = d.W.Exec("DELETE FROM devices WHERE created_source = 'live'")
	}()
	p := New()
	rc, _, _ := plugintest.RunContext(t, p, nil)
	rc.DB = d
	start := time.Now()
	ms, err := p.match(ctx, rc, loadConfig(rc.Settings), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("500 devices: %s, %d device CPEs, %d active device CVEs", time.Since(start).Round(time.Millisecond), ms.CPEs, ms.Active)
	start = time.Now()
	if _, err := p.match(ctx, rc, loadConfig(rc.Settings), nil, nil); err != nil {
		t.Fatal(err)
	}
	t.Logf("re-match: %s", time.Since(start).Round(time.Millisecond))
	for _, id := range []int64{first, first + 1} {
		rows, err := d.R.Query(`SELECT match_type, product, COUNT(*) FROM device_cves WHERE device_id = ? AND gone_at IS NULL GROUP BY 1, 2 ORDER BY 3 DESC`, id)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var typ, product string
			var n int
			_ = rows.Scan(&typ, &product, &n)
			t.Logf("device %d: %-10s %-30s %d", id, typ, product, n)
		}
		rows.Close()
	}
}
