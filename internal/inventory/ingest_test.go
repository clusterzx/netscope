package inventory

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"netscope/internal/db"
	"netscope/internal/plugin"
	"netscope/internal/settings"
)

type recorder struct {
	mu      sync.Mutex
	changes []plugin.Change
}

func (r *recorder) add(c []plugin.Change) {
	r.mu.Lock()
	r.changes = append(r.changes, c...)
	r.mu.Unlock()
}

func (r *recorder) take() []plugin.Change {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := r.changes
	r.changes = nil
	return out
}

func newTestStore(t *testing.T) (*Store, *recorder) {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "inv.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	st, err := settings.Load(ctx, d)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(ctx, d, nil, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SaveSubnet(ctx, &Subnet{CIDR: "192.168.8.0/24", Name: "lan", Interface: "eth0", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	s.OnChanges(rec.add)
	return s, rec
}

func types(cs []plugin.Change) []string {
	var out []string
	for _, c := range cs {
		s := string(c.Type)
		if c.Initial {
			s += "(initial)"
		}
		out = append(out, s)
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func tcp(ports ...int) *plugin.PortScan {
	ps := &plugin.PortScan{Protocol: "tcp", Scanned: []plugin.PortRange{{From: 1, To: 1000}}}
	for _, p := range ports {
		ps.Ports = append(ps.Ports, plugin.Port{Port: p, Proto: "tcp", State: "open", Service: "svc"})
	}
	return ps
}

// TestIngestChanges runs a sequence of observations and checks the derived changes after
// each step (table driven).
func TestIngestChanges(t *testing.T) {
	ctx := context.Background()
	const mac = "aa:bb:cc:00:00:01"
	steps := []struct {
		name   string
		plugin string
		obs    plugin.Observation
		want   []string
	}{
		{"new device via arp", "arpscan", plugin.Observation{MACs: []string{mac}, IP: "192.168.8.10", Present: true, Vendor: "Acme"},
			[]string{"device.created"}},
		{"same again", "arpscan", plugin.Observation{MACs: []string{mac}, IP: "192.168.8.10", Present: true}, nil},
		{"first port scan is initial", "nmap", plugin.Observation{IP: "192.168.8.10", Present: true, Ports: tcp(22, 80)},
			[]string{"port.opened(initial)", "port.opened(initial)"}},
		{"new port", "nmap", plugin.Observation{IP: "192.168.8.10", Present: true, Ports: tcp(22, 80, 443)},
			[]string{"port.opened"}},
		{"version change", "nmap", plugin.Observation{IP: "192.168.8.10", Present: true, Ports: &plugin.PortScan{Protocol: "tcp",
			Scanned: []plugin.PortRange{{From: 1, To: 1000}}, Ports: []plugin.Port{
				{Port: 22, State: "open", Service: "ssh", Product: "OpenSSH", Version: "9.6"},
				{Port: 80, State: "open", Service: "svc"}, {Port: 443, State: "open", Service: "svc"}}}},
			[]string{"port.changed"}},
		{"same version without detection keeps data", "nmap", plugin.Observation{IP: "192.168.8.10", Present: true, Ports: tcp(22, 80, 443)}, nil},
		{"port closed inside range", "nmap", plugin.Observation{IP: "192.168.8.10", Present: true, Ports: tcp(22, 443)},
			[]string{"port.closed"}},
		{"port outside scanned range stays", "nmap", plugin.Observation{IP: "192.168.8.10", Present: true, Ports: &plugin.PortScan{
			Protocol: "tcp", Scanned: []plugin.PortRange{{From: 1000, To: 2000}}}}, nil},
		{"first hostname is not a change", "dns", plugin.Observation{IP: "192.168.8.10", Hostname: "nas.lan"}, nil},
		{"hostname change", "dns", plugin.Observation{IP: "192.168.8.10", Hostname: "storage.lan"}, []string{"hostname.changed"}},
		{"os first", "nmap", plugin.Observation{IP: "192.168.8.10", OS: &plugin.OSInfo{Name: "Linux 5.x", Accuracy: 95}}, nil},
		{"better os source is not a change", "ssh", plugin.Observation{IP: "192.168.8.10", OS: &plugin.OSInfo{Name: "Ubuntu 24.04", Accuracy: 100}}, nil},
		{"os change", "ssh", plugin.Observation{IP: "192.168.8.10", OS: &plugin.OSInfo{Name: "Ubuntu 26.04", Accuracy: 100}}, []string{"os.changed"}},
		{"packages initial", "ssh", plugin.Observation{IP: "192.168.8.10", Packages: &plugin.PackageInventory{Manager: "dpkg",
			Packages: []plugin.Package{{Name: "openssl", Version: "3.0.13-0ubuntu3.4", Arch: "amd64"}, {Name: "curl", Version: "8.5.0-2ubuntu10.5", Arch: "amd64"}}}}, nil},
		{"packages update", "ssh", plugin.Observation{IP: "192.168.8.10", Packages: &plugin.PackageInventory{Manager: "dpkg",
			Packages: []plugin.Package{{Name: "openssl", Version: "3.0.13-0ubuntu3.5", Arch: "amd64"}, {Name: "vim", Version: "9.1", Arch: "amd64"}}}},
			[]string{"packages.changed"}},
		{"containers initial", "docker", plugin.Observation{IP: "192.168.8.10", Containers: &plugin.ContainerInventory{Engine: "docker",
			Containers: []plugin.Container{{ID: "1", Name: "grafana", Image: "grafana/grafana:11.0.0"}}}}, []string{"container.added(initial)"}},
		{"container image change + new", "docker", plugin.Observation{IP: "192.168.8.10", Containers: &plugin.ContainerInventory{Engine: "docker",
			Containers: []plugin.Container{{ID: "2", Name: "grafana", Image: "grafana/grafana:11.1.0"}, {ID: "3", Name: "loki", Image: "grafana/loki"}}}},
			[]string{"container.image", "container.added"}},
		{"container removed", "docker", plugin.Observation{IP: "192.168.8.10", Containers: &plugin.ContainerInventory{Engine: "docker",
			Containers: []plugin.Container{{ID: "2", Name: "grafana", Image: "grafana/grafana:11.1.0"}}}}, []string{"container.removed"}},
		{"cert initial", "tls", plugin.Observation{IP: "192.168.8.10", TLS: &plugin.TLSScan{Scanned: []int{443},
			Certs: []plugin.TLSCert{{Port: 443, Fingerprint: "aa", SubjectCN: "nas", NotAfter: time.Now().Add(90 * 24 * time.Hour)}}}},
			[]string{"cert.added(initial)"}},
		{"cert changed", "tls", plugin.Observation{IP: "192.168.8.10", TLS: &plugin.TLSScan{Scanned: []int{443},
			Certs: []plugin.TLSCert{{Port: 443, Fingerprint: "bb", SubjectCN: "nas", NotAfter: time.Now().Add(90 * 24 * time.Hour)}}}},
			[]string{"cert.changed"}},
		{"cert removed", "tls", plugin.Observation{IP: "192.168.8.10", TLS: &plugin.TLSScan{Scanned: []int{443}}}, []string{"cert.removed"}},
		{"unknown non-present is ignored", "dns", plugin.Observation{IP: "192.168.8.99", Hostname: "ghost"}, nil},
		{"mac change on known ip", "arpscan", plugin.Observation{MACs: []string{"aa:bb:cc:00:00:02"}, IP: "192.168.8.10", Present: true},
			[]string{"device.created", "ip.mac_changed"}},
	}
	for _, st := range steps {
		obs := st.obs
		if _, err := s(t).Observe(ctx, st.plugin, 0, &obs); err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		got := types(rec(t).take())
		if !equal(got, st.want) {
			t.Fatalf("%s: changes %v, want %v", st.name, got, st.want)
		}
	}
	// the manual hostname wins over sources and silences source changes
	id := deviceByMAC(t, mac)
	if _, _, err := s(t).Update(ctx, id, DeviceUpdate{Hostname: strPtr("my-nas")}); err != nil {
		t.Fatal(err)
	}
	obs := plugin.Observation{MACs: []string{mac}, Hostname: "other.lan"}
	if _, err := s(t).Observe(ctx, "dns", 0, &obs); err != nil {
		t.Fatal(err)
	}
	var host, src string
	_ = s(t).db.R.QueryRow("SELECT hostname, hostname_source FROM devices WHERE id = ?", id).Scan(&host, &src)
	if host != "my-nas" || src != "manual" {
		t.Fatalf("manual hostname lost: %s (%s)", host, src)
	}
	if got := types(rec(t).take()); len(got) != 0 {
		t.Fatalf("unexpected changes %v", got)
	}
}

// test globals: the table test above uses helpers bound to one store
var (
	curStore *Store
	curRec   *recorder
)

func s(t *testing.T) *Store {
	if curStore == nil {
		curStore, curRec = newTestStore(t)
		t.Cleanup(func() { curStore, curRec = nil, nil })
	}
	return curStore
}

func rec(t *testing.T) *recorder { s(t); return curRec }

func strPtr(v string) *string { return &v }

func deviceByMAC(t *testing.T, mac string) int64 {
	t.Helper()
	var id int64
	if err := s(t).db.R.QueryRow("SELECT device_id FROM device_macs WHERE mac = ?", mac).Scan(&id); err != nil {
		t.Fatalf("device for %s: %v", mac, err)
	}
	return id
}

func TestPresenceAndIPChange(t *testing.T) {
	ctx := context.Background()
	store, r := newTestStore(t)
	subs, _ := store.Subnets(ctx)
	lan := subs[0]
	targets := plugin.Targets{Subnets: []plugin.SubnetTarget{lan}}
	observe := func(run int64, mac, ip string) {
		t.Helper()
		if _, err := store.Observe(ctx, "arpscan", run, &plugin.Observation{MACs: []string{mac}, IP: ip, Present: true}); err != nil {
			t.Fatal(err)
		}
	}
	finish := func(run int64) []string {
		t.Helper()
		if err := store.RunFinished(ctx, plugin.RunSummary{RunID: run, PluginID: "arpscan", Status: "success", Presence: true, Targets: targets}); err != nil {
			t.Fatal(err)
		}
		return types(r.take())
	}
	observe(1, "aa:00:00:00:00:01", "192.168.8.20")
	observe(1, "aa:00:00:00:00:02", "192.168.8.21")
	r.take()
	if got := finish(1); len(got) != 0 {
		t.Fatalf("run1: %v", got)
	}
	// run 2: device 1 moved to .30, device 2 missing (1st miss)
	observe(2, "aa:00:00:00:00:01", "192.168.8.30")
	if got := types(r.take()); !equal(got, []string{"ip.added"}) {
		t.Fatalf("run2 observe: %v", got)
	}
	if got := finish(2); !equal(got, []string{"ip.changed"}) {
		t.Fatalf("run2 finish: %v", got)
	}
	// run 3: device 2 missing a second time -> offline
	observe(3, "aa:00:00:00:00:01", "192.168.8.30")
	r.take()
	if got := finish(3); !equal(got, []string{"device.offline"}) {
		t.Fatalf("run3 finish: %v", got)
	}
	// run 4: device 2 back
	observe(4, "aa:00:00:00:00:02", "192.168.8.21")
	if got := types(r.take()); !equal(got, []string{"device.online"}) {
		t.Fatalf("run4: %v", got)
	}
	// failed runs do not count misses
	if err := store.RunFinished(ctx, plugin.RunSummary{RunID: 5, PluginID: "arpscan", Status: "failed", Presence: true, Targets: targets}); err != nil {
		t.Fatal(err)
	}
	var missed int
	_ = store.db.R.QueryRow("SELECT missed FROM device_presence WHERE device_id = (SELECT device_id FROM device_macs WHERE mac = 'aa:00:00:00:00:01')").Scan(&missed)
	if missed != 0 {
		t.Fatalf("missed after failed run: %d", missed)
	}
	var primary string
	_ = store.db.R.QueryRow("SELECT primary_ip FROM devices WHERE id = (SELECT device_id FROM device_macs WHERE mac = 'aa:00:00:00:00:01')").Scan(&primary)
	if primary != "192.168.8.30" {
		t.Fatalf("primary ip %s", primary)
	}
}

func TestShadowMergeAndManualSurvives(t *testing.T) {
	ctx := context.Background()
	store, r := newTestStore(t)
	// ip-only device found by nmap without MAC
	shadow, err := store.Observe(ctx, "nmap", 1, &plugin.Observation{IP: "192.168.8.40", Present: true, Ports: tcp(22)})
	if err != nil {
		t.Fatal(err)
	}
	// MAC device elsewhere with manual data
	dev, _ := store.Observe(ctx, "arpscan", 2, &plugin.Observation{MACs: []string{"aa:00:00:00:00:09"}, IP: "192.168.8.41", Present: true})
	if _, _, err := store.Update(ctx, dev, DeviceUpdate{DisplayName: strPtr("Kamera"), Tags: &[]string{"IoT", "cam"}}); err != nil {
		t.Fatal(err)
	}
	r.take()
	// the MAC device now answers on the shadow's IP -> shadow merged
	if _, err := store.Observe(ctx, "arpscan", 3, &plugin.Observation{MACs: []string{"aa:00:00:00:00:09"}, IP: "192.168.8.40", Present: true}); err != nil {
		t.Fatal(err)
	}
	var n int
	_ = store.db.R.QueryRow("SELECT COUNT(*) FROM devices WHERE id = ?", shadow).Scan(&n)
	if n != 0 {
		t.Fatal("shadow device not merged")
	}
	var ports int
	_ = store.db.R.QueryRow("SELECT COUNT(*) FROM ports WHERE device_id = ? AND gone_at IS NULL", dev).Scan(&ports)
	if ports != 1 {
		t.Fatalf("ports not moved: %d", ports)
	}
	// manual data survives scans and imports without overwrite
	if _, err := store.Observe(ctx, "netalertx", 0, &plugin.Observation{MACs: []string{"aa:00:00:00:00:09"},
		Manual: &plugin.ManualData{DisplayName: "Andere", Tags: []string{"imported"}}}); err != nil {
		t.Fatal(err)
	}
	snap, _ := store.ManualSnapshot(ctx, store.db.R, dev)
	if snap.DisplayName != "Kamera" || !equal(snap.Tags, []string{"cam", "imported", "iot"}) {
		t.Fatalf("manual data changed: %+v", snap)
	}
}

func TestMergeAndSplit(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	a, _ := store.Observe(ctx, "arpscan", 1, &plugin.Observation{MACs: []string{"aa:00:00:00:01:01"}, IP: "192.168.8.50", Present: true, Ports: tcp(22)})
	b, _ := store.Observe(ctx, "arpscan", 1, &plugin.Observation{MACs: []string{"aa:00:00:00:01:02"}, IP: "192.168.8.51", Present: true, Ports: tcp(22, 80)})
	if err := store.Merge(ctx, a, []int64{b}); err != nil {
		t.Fatal(err)
	}
	var macs, ips, ports int
	_ = store.db.R.QueryRow("SELECT COUNT(*) FROM device_macs WHERE device_id = ?", a).Scan(&macs)
	_ = store.db.R.QueryRow("SELECT COUNT(*) FROM device_ips WHERE device_id = ? AND gone_at IS NULL", a).Scan(&ips)
	_ = store.db.R.QueryRow("SELECT COUNT(*) FROM ports WHERE device_id = ? AND gone_at IS NULL", a).Scan(&ports)
	if macs != 2 || ips != 2 || ports != 3 {
		t.Fatalf("after merge macs=%d ips=%d ports=%d", macs, ips, ports)
	}
	nb, err := store.Split(ctx, a, []string{"aa:00:00:00:01:02"})
	if err != nil {
		t.Fatal(err)
	}
	_ = store.db.R.QueryRow("SELECT COUNT(*) FROM ports WHERE device_id = ? AND gone_at IS NULL", nb).Scan(&ports)
	if ports != 2 {
		t.Fatalf("split moved %d ports", ports)
	}
	if _, err := store.Split(ctx, a, []string{"aa:00:00:00:01:01"}); err == nil {
		t.Fatal("splitting the last MAC must fail")
	}
}

func TestQueryLanguage(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	mk := func(mac, ip string, ports ...int) int64 {
		id, err := store.Observe(ctx, "arpscan", 1, &plugin.Observation{MACs: []string{mac}, IP: ip, Present: true, Ports: tcp(ports...)})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	cam := mk("aa:00:00:00:02:01", "192.168.8.60", 80, 554)
	srv := mk("aa:00:00:00:02:02", "192.168.8.61", 22, 443)
	_, _, _ = store.Update(ctx, cam, DeviceUpdate{Tags: &[]string{"iot"}, State: strPtr("known")})
	_, _, _ = store.Update(ctx, srv, DeviceUpdate{OS: strPtr("Ubuntu Linux 24.04"), Criticality: strPtr("high")})
	_, _ = store.db.W.Exec("UPDATE devices SET last_seen = ? WHERE id = ?", time.Now().Add(-48*time.Hour).UnixMilli(), srv)
	_, _ = store.db.W.Exec(`INSERT INTO device_cves(device_id, cve_id, cpe, source, match_type, cvss_score, first_seen, last_seen)
		VALUES (?, 'CVE-2024-6387', 'cpe:/a:openbsd:openssh:9.6', 'nmap', 'range', 8.1, 0, 0)`, srv)
	cases := []struct {
		q    string
		want []int64
	}{
		{"", []int64{cam, srv}},
		{"tag:iot", []int64{cam}},
		{"-tag:iot", []int64{srv}},
		{"port:22", []int64{srv}},
		{"port:554|443", []int64{cam, srv}},
		{"port:22/udp", nil},
		{"os:linux", []int64{srv}},
		{"cve>=7", []int64{srv}},
		{"cve>=9", nil},
		{"cve:CVE-2024-6387", []int64{srv}},
		{"seen<24h", []int64{cam}},
		{"seen>24h", []int64{srv}},
		{"crit>=high", []int64{srv}},
		{"state:known", []int64{cam}},
		{"ip:192.168.8.60", []int64{cam}},
		{"ip:192.168.8.0/24 port:80", []int64{cam}},
		{"mac:aa:00:00:00:02:02", []int64{srv}},
		{"aa:00:00:00:02:01", []int64{cam}},
		{"192.168.8.61", []int64{srv}},
		{`os:"ubuntu linux"`, []int64{srv}},
		{"is:online", []int64{cam, srv}},
		{"has:cve", []int64{srv}},
	}
	for _, c := range cases {
		ids, err := store.MatchingIDs(ctx, c.q)
		if err != nil {
			t.Fatalf("%q: %v", c.q, err)
		}
		got := map[int64]bool{}
		for _, id := range ids {
			got[id] = true
		}
		if len(got) != len(c.want) {
			t.Errorf("%q: got %v want %v", c.q, ids, c.want)
			continue
		}
		for _, w := range c.want {
			if !got[w] {
				t.Errorf("%q: got %v want %v", c.q, ids, c.want)
			}
		}
	}
	for _, bad := range []string{"port:abc", "state:weird", "seen<xyz", "cve:high", `name:"open`} {
		if _, err := store.MatchingIDs(ctx, bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
	res, err := store.List(ctx, ListOptions{Query: "", Sort: "-ports", WithPorts: true})
	if err != nil || res.Total != 2 || res.Items[0].PortCount != 2 || len(res.Items[0].Ports) != 2 {
		t.Fatalf("list: %v %+v", err, res)
	}
}

func TestDiffTimesAndRuns(t *testing.T) {
	ctx := context.Background()
	store, _ := newTestStore(t)
	id, _ := store.Observe(ctx, "nmap", 1, &plugin.Observation{MACs: []string{"aa:00:00:00:03:01"}, IP: "192.168.8.70", Present: true, Ports: tcp(22)})
	t1 := time.Now()
	time.Sleep(5 * time.Millisecond)
	_, _ = store.Observe(ctx, "nmap", 2, &plugin.Observation{MACs: []string{"aa:00:00:00:03:01"}, IP: "192.168.8.70", Present: true, Ports: tcp(22, 8080)})
	t2 := time.Now()
	res, err := store.DiffTimes(ctx, t1, t2, id)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Kind != "port" || res.Items[0].Change != "added" {
		t.Fatalf("time diff %+v", res.Items)
	}
	_, _ = store.db.W.Exec("INSERT INTO runs(id, plugin_id, trigger, status, created_at) VALUES (1,'nmap','manual','success',0),(2,'nmap','manual','success',0)")
	res, err = store.DiffRuns(ctx, 1, 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 1 || res.Items[0].Key != "192.168.8.70 8080/tcp" {
		t.Fatalf("run diff %+v", res.Items)
	}
}

func TestFirstRunIsInitialInventory(t *testing.T) {
	ctx := context.Background()
	store, r := newTestStore(t)
	if _, err := store.db.W.Exec(`INSERT INTO runs(id, plugin_id, trigger, status, created_at) VALUES
		(10, 'arpscan', 'schedule', 'running', 0)`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Observe(ctx, "arpscan", 10, &plugin.Observation{MACs: []string{"aa:00:00:00:05:01"}, IP: "192.168.8.80", Present: true}); err != nil {
		t.Fatal(err)
	}
	if got := types(r.take()); !equal(got, []string{"device.created(initial)"}) {
		t.Fatalf("first run: %v", got)
	}
	_, _ = store.db.W.Exec(`UPDATE runs SET status = 'success' WHERE id = 10`)
	_, _ = store.db.W.Exec(`INSERT INTO runs(id, plugin_id, trigger, status, created_at) VALUES (11, 'arpscan', 'schedule', 'running', 0)`)
	if _, err := store.Observe(ctx, "arpscan", 11, &plugin.Observation{MACs: []string{"aa:00:00:00:05:02"}, IP: "192.168.8.81", Present: true}); err != nil {
		t.Fatal(err)
	}
	if got := types(r.take()); !equal(got, []string{"device.created"}) {
		t.Fatalf("later run: %v", got)
	}
}

func TestSameHostname(t *testing.T) {
	cases := []struct {
		a, b string
		same bool
	}{
		{"iPhone.lan", "iPhone", true},
		{"NAS", "nas.fritz.box", true},
		{"host.", "HOST", true},
		{"nas.lan", "nas2.lan", false},
		{"printer", "scanner", false},
		{"192.168.8.10", "192.168.8.11", false},
		{"192.168.8.10", "192", false},
	}
	for _, c := range cases {
		if got := sameHostname(c.a, c.b); got != c.same {
			t.Errorf("sameHostname(%q, %q) = %v", c.a, c.b, got)
		}
	}
}
