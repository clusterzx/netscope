package topology

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/netip"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"netscope/internal/db"
	"netscope/internal/netutil"
	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
	"netscope/internal/plugins/snmp"
)

// fixture builds inventory rows directly in a real database.
type fixture struct {
	t       *testing.T
	ctx     context.Context
	d       *db.DB
	now     time.Time
	ids     map[string]int64
	names   map[int64]string
	subnets map[int64]netip.Prefix
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	d, err := db.Open(ctx, filepath.Join(t.TempDir(), "topo.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &fixture{t: t, ctx: ctx, d: d, now: time.Now(), ids: map[string]int64{}, names: map[int64]string{}, subnets: map[int64]netip.Prefix{}}
}

func (f *fixture) exec(q string, args ...any) {
	f.t.Helper()
	if _, err := f.d.W.ExecContext(f.ctx, q, args...); err != nil {
		f.t.Fatalf("%s: %v", q, err)
	}
}

func (f *fixture) subnet(cidr, gateway string) {
	f.t.Helper()
	res, err := f.d.W.ExecContext(f.ctx, "INSERT INTO subnets(cidr, gateway, created_at, updated_at) VALUES (?,?,?,?)", cidr, gateway, 1, 1)
	if err != nil {
		f.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	f.subnets[id] = netip.MustParsePrefix(cidr)
}

// device creates a device with a hostname, MACs and IPs ("mac:…" and "ip:…" items).
func (f *fixture) device(name string, items ...string) int64 {
	f.t.Helper()
	res, err := f.d.W.ExecContext(f.ctx, "INSERT INTO devices(hostname, created_at, updated_at) VALUES (?,?,?)", name, 1, 1)
	if err != nil {
		f.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	f.ids[name], f.names[id] = id, name
	for _, it := range items {
		switch {
		case strings.HasPrefix(it, "mac:"):
			mac, ok := netutil.NormalizeMAC(strings.TrimPrefix(it, "mac:"))
			if !ok {
				f.t.Fatalf("bad mac %s", it)
			}
			f.exec("INSERT INTO device_macs(mac, device_id, source, first_seen, last_seen) VALUES (?,?,?,?,?)", mac, id, "test", 1, 1)
		case strings.HasPrefix(it, "ip:"):
			ip := strings.TrimPrefix(it, "ip:")
			var sn any
			for sid, p := range f.subnets {
				if p.Contains(netip.MustParseAddr(ip)) {
					sn = sid
				}
			}
			f.exec("INSERT INTO device_ips(device_id, ip, ip_key, subnet_id, source, first_seen, last_seen) VALUES (?,?,?,?,?,?,?)",
				id, ip, netutil.IPKey(ip), sn, "test", 1, 1)
		case strings.HasPrefix(it, "tag:"):
			f.exec("INSERT INTO device_tags(device_id, tag) VALUES (?,?)", id, strings.TrimPrefix(it, "tag:"))
		default:
			f.t.Fatalf("unknown item %s", it)
		}
	}
	return id
}

func (f *fixture) inventory(name string, inv snmp.Inventory, age time.Duration) {
	f.t.Helper()
	b, err := json.Marshal(inv)
	if err != nil {
		f.t.Fatal(err)
	}
	f.exec("INSERT INTO device_inventory(device_id, source, data, collected_at) VALUES (?,?,?,?)",
		f.ids[name], "snmp", string(b), f.now.Add(-age).UnixMilli())
}

func (f *fixture) relation(parent, child, kind, src string, lastSeen time.Time) int64 {
	f.t.Helper()
	res, err := f.d.W.ExecContext(f.ctx, `INSERT INTO relations(parent_id, child_id, kind, source, protected, first_seen, last_seen)
		VALUES (?,?,?,?,?,?,?)`, f.ids[parent], f.ids[child], kind, src, db.Bool(src == "manual"), lastSeen.UnixMilli(), lastSeen.UnixMilli())
	if err != nil {
		f.t.Fatal(err)
	}
	id, _ := res.LastInsertId()
	return id
}

// edges lists relations as "kind parent>child parentPort|childPort" (topology) or
// "src:kind parent>child" (other sources).
func (f *fixture) edges() []string {
	f.t.Helper()
	rows, err := f.d.R.QueryContext(f.ctx, "SELECT parent_id, child_id, kind, source, parent_port, child_port FROM relations")
	if err != nil {
		f.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var (
			p, c              int64
			kind, src, pp, cp string
		)
		if err := rows.Scan(&p, &c, &kind, &src, &pp, &cp); err != nil {
			f.t.Fatal(err)
		}
		if src == source {
			out = append(out, fmt.Sprintf("%s %s>%s %s|%s", kind, f.names[p], f.names[c], pp, cp))
		} else {
			out = append(out, fmt.Sprintf("%s:%s %s>%s", src, kind, f.names[p], f.names[c]))
		}
	}
	sort.Strings(out)
	return out
}

func (f *fixture) run(settings map[string]any) *plugin.RunContext {
	f.t.Helper()
	rc, _, evs := plugintest.RunContext(f.t, &Plugin{}, settings)
	rc.DB = f.d
	if err := (&Plugin{}).Run(f.ctx, rc); err != nil {
		f.t.Fatal(err)
	}
	if len(evs.Events) > 0 {
		f.t.Errorf("topology must not emit events: %+v", evs.Events)
	}
	return rc
}

// fdb builds an FDB: port name -> MACs (ifIndex = position+1).
func fdb(ports ...any) []snmp.FDBEntry {
	var out []snmp.FDBEntry
	for i := 0; i+1 < len(ports); i += 2 {
		name := ports[i].(string)
		for _, mac := range ports[i+1].([]string) {
			m, _ := netutil.NormalizeMAC(mac)
			out = append(out, snmp.FDBEntry{MAC: m, VLAN: 1, BridgePort: i/2 + 1, IfIndex: i/2 + 1, IfName: name, Status: "learned"})
		}
	}
	return out
}

func macs(prefix string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s:%02x", prefix, i)
	}
	return out
}

func TestRebuild(t *testing.T) {
	old := time.Now().Add(-48 * time.Hour)
	recent := time.Now().Add(-time.Hour)
	cases := []struct {
		name     string
		settings map[string]any
		setup    func(f *fixture)
		want     []string
	}{
		{
			name: "access ports, uplink and gateway",
			setup: func(f *fixture) {
				f.subnet("192.168.8.0/24", "192.168.8.1")
				f.device("router", "mac:00:00:5e:00:01:01", "ip:192.168.8.1")
				f.device("sw1", "mac:02:00:00:00:00:01", "ip:192.168.8.2")
				f.device("pc", "mac:aa:00:00:00:00:01", "ip:192.168.8.10")
				f.device("nas", "mac:aa:00:00:00:00:02", "ip:192.168.8.11")
				f.device("printer", "mac:aa:00:00:00:00:03", "ip:192.168.8.12")
				f.inventory("sw1", snmp.Inventory{FDB: fdb(
					"Gi1", []string{"aa:00:00:00:00:01"},
					"Gi2", []string{"aa:00:00:00:00:02"},
					"Gi24", append([]string{"00:00:5e:00:01:01"}, macs("bb:00:00:00:00", 5)...),
				)}, time.Minute)
			},
			want: []string{
				"l3 router>printer |",
				"l3 router>sw1 |",
				"switch_port sw1>nas Gi2|",
				"switch_port sw1>pc Gi1|",
			},
		},
		{
			name:     "uplink threshold is configurable",
			settings: map[string]any{"uplink_mac_threshold": 10, "attach_to_gateway": false},
			setup: func(f *fixture) {
				f.subnet("192.168.8.0/24", "192.168.8.1")
				f.device("router", "mac:00:00:5e:00:01:01", "ip:192.168.8.1")
				f.device("sw1", "mac:02:00:00:00:00:01")
				f.inventory("sw1", snmp.Inventory{FDB: fdb("Gi24", append([]string{"00:00:5e:00:01:01"}, macs("bb:00:00:00:00", 5)...))}, time.Minute)
			},
			want: []string{"switch_port sw1>router Gi24|"},
		},
		{
			name: "a device seen on several switches hangs at the port with the fewest MACs",
			setup: func(f *fixture) {
				f.device("sw1", "mac:02:00:00:00:00:01")
				f.device("sw2", "mac:02:00:00:00:00:02")
				f.device("tv", "mac:aa:00:00:00:00:05")
				f.inventory("sw1", snmp.Inventory{FDB: fdb("1", []string{"aa:00:00:00:00:05", "cc:00:00:00:00:01", "cc:00:00:00:00:02"})}, time.Minute)
				f.inventory("sw2", snmp.Inventory{FDB: fdb("1", []string{"cc:00:00:00:00:03"}, "2", []string{"aa:00:00:00:00:05"})}, time.Minute)
			},
			want: []string{"switch_port sw2>tv 2|"},
		},
		{
			name: "LLDP seen from both sides gives one edge with both ports",
			setup: func(f *fixture) {
				f.device("core", "mac:02:00:00:00:00:01", "ip:10.0.0.2")
				f.device("edge", "mac:02:00:00:00:00:02", "ip:10.0.0.3")
				f.inventory("core", snmp.Inventory{
					FDB: fdb("Gi1", macs("cc:00:00:00:01", 3), "Gi24", []string{"02:00:00:00:00:02", "cc:00:00:00:00:09"}),
					LLDP: []snmp.LLDPNeighbor{{LocalPortNum: 2, LocalPort: "Gi24", LocalIfIndex: 2, ChassisIDSubtype: "macAddress",
						ChassisID: "02:00:00:00:00:02", ChassisMAC: "02:00:00:00:00:02", PortIDSubtype: "interfaceName", PortID: "Port1",
						Capabilities: []string{"bridge"}}},
				}, time.Minute)
				f.inventory("edge", snmp.Inventory{
					FDB: fdb("Port1", []string{"02:00:00:00:00:01"}),
					LLDP: []snmp.LLDPNeighbor{{LocalPortNum: 1, LocalPort: "Port1", LocalIfIndex: 1, ChassisIDSubtype: "local",
						ChassisID: "core", PortIDSubtype: "local", PortID: "24", PortDesc: "Gi24", SysName: "core.lan", Capabilities: []string{"bridge"}}},
				}, time.Minute)
			},
			want: []string{"lldp core>edge Gi24|Port1"},
		},
		{
			name: "LLDP neighbours matched by management address, system name and port MAC",
			setup: func(f *fixture) {
				f.device("sw1", "mac:02:00:00:00:00:01")
				f.device("ap", "ip:10.0.0.20")
				f.device("nas.lan")
				f.device("pve", "mac:aa:00:00:00:00:10")
				f.inventory("sw1", snmp.Inventory{FDB: fdb("1", macs("cc:00:00:00:01", 2)), LLDP: []snmp.LLDPNeighbor{
					{LocalPortNum: 5, LocalPort: "5", ChassisIDSubtype: "macAddress", ChassisMAC: "dd:00:00:00:00:01", ChassisID: "dd:00:00:00:00:01",
						ManagementAddresses: []string{"10.0.0.20"}, Capabilities: []string{"bridge", "wlanAccessPoint"}},
					{LocalPortNum: 6, LocalPort: "6", ChassisIDSubtype: "local", ChassisID: "x", SysName: "NAS", Capabilities: []string{"stationOnly"}},
					{LocalPortNum: 7, LocalPort: "7", ChassisIDSubtype: "macAddress", ChassisMAC: "dd:00:00:00:00:02", ChassisID: "dd:00:00:00:00:02",
						PortIDSubtype: "macAddress", PortMAC: "aa:00:00:00:00:10", PortID: "aa:00:00:00:00:10", PortDesc: "enp1s0"},
					{LocalPortNum: 8, LocalPort: "8", ChassisIDSubtype: "macAddress", ChassisMAC: "dd:00:00:00:00:03", SysName: "unbekannt"},
				}}, time.Minute)
			},
			want: []string{"lldp sw1>ap 5|", "lldp sw1>nas.lan 6|", "lldp sw1>pve 7|enp1s0"},
		},
		{
			name: "edges of other sources are respected and never touched",
			setup: func(f *fixture) {
				f.subnet("192.168.8.0/24", "192.168.8.1")
				f.device("router", "ip:192.168.8.1")
				f.device("sw1", "mac:02:00:00:00:00:01")
				f.device("pc", "mac:aa:00:00:00:00:01", "ip:192.168.8.10")
				f.device("host", "mac:aa:00:00:00:00:02", "ip:192.168.8.20")
				f.device("vm", "mac:aa:00:00:00:00:03", "ip:192.168.8.21")
				f.device("gone", "mac:aa:00:00:00:00:04")
				f.inventory("sw1", snmp.Inventory{FDB: fdb("1", []string{"aa:00:00:00:00:01"}, "7", []string{"aa:00:00:00:00:02", "aa:00:00:00:00:03"})}, time.Minute)
				f.relation("router", "pc", plugin.RelManual, "manual", old)
				f.relation("host", "vm", plugin.RelRunsOn, "proxmox", old)
				f.relation("sw1", "gone", plugin.RelSwitchPort, source, old)
				f.relation("host", "gone", "container", "docker", old)
			},
			want: []string{
				"docker:container host>gone",
				"manual:manual router>pc",
				"proxmox:runs_on host>vm",
				"switch_port sw1>host 7|",
			},
		},
		{
			name:     "excluded tags",
			settings: map[string]any{"exclude_tags": []any{"Keine Topologie"}},
			setup: func(f *fixture) {
				f.subnet("192.168.8.0/24", "192.168.8.1")
				f.device("router", "ip:192.168.8.1")
				f.device("sw1", "mac:02:00:00:00:00:01")
				f.device("secret", "mac:aa:00:00:00:00:01", "ip:192.168.8.10", "tag:keine-topologie")
				f.device("pc", "mac:aa:00:00:00:00:02", "ip:192.168.8.11")
				f.inventory("sw1", snmp.Inventory{FDB: fdb("1", []string{"aa:00:00:00:00:01"}, "2", []string{"aa:00:00:00:00:02"})}, time.Minute)
				f.relation("sw1", "secret", plugin.RelSwitchPort, source, recent)
			},
			want: []string{"switch_port sw1>pc 2|"},
		},
		{
			name:     "no gateway edges when disabled",
			settings: map[string]any{"attach_to_gateway": false},
			setup: func(f *fixture) {
				f.subnet("192.168.8.0/24", "192.168.8.1")
				f.device("router", "ip:192.168.8.1")
				f.device("pc", "ip:192.168.8.10")
				f.relation("router", "pc", plugin.RelL3, source, recent)
			},
			want: nil,
		},
		{
			name: "vanished edges are kept during the retention, stale ones removed",
			setup: func(f *fixture) {
				f.device("sw1", "mac:02:00:00:00:00:01")
				f.device("laptop", "mac:aa:00:00:00:00:01")
				f.device("old-pc", "mac:aa:00:00:00:00:02")
				f.device("moved", "mac:aa:00:00:00:00:03")
				f.inventory("sw1", snmp.Inventory{FDB: fdb("1", []string{"cc:00:00:00:00:01"}, "24", append([]string{"aa:00:00:00:00:03"}, macs("bb:00:00:00:00", 5)...))}, time.Minute)
				f.relation("sw1", "laptop", plugin.RelSwitchPort, source, recent)
				f.relation("sw1", "old-pc", plugin.RelSwitchPort, source, old)
				f.relation("sw1", "moved", plugin.RelSwitchPort, source, recent) // now seen behind the uplink
			},
			want: []string{"switch_port sw1>laptop |"},
		},
		{
			name:     "retention off",
			settings: map[string]any{"edge_retention": "0s"},
			setup: func(f *fixture) {
				f.device("sw1", "mac:02:00:00:00:00:01")
				f.device("laptop", "mac:aa:00:00:00:00:01")
				f.inventory("sw1", snmp.Inventory{FDB: fdb("1", []string{"cc:00:00:00:00:01"})}, time.Minute)
				f.relation("sw1", "laptop", plugin.RelSwitchPort, source, recent)
			},
			want: nil,
		},
		{
			name: "switches seeing each other without LLDP: one edge, larger FDB upstream",
			setup: func(f *fixture) {
				f.device("sw1", "mac:02:00:00:00:00:01")
				f.device("sw2", "mac:02:00:00:00:00:02")
				f.inventory("sw1", snmp.Inventory{FDB: fdb("1", macs("cc:00:00:00:01", 3), "2", macs("cc:00:00:00:02", 3),
					"10", []string{"02:00:00:00:00:02", "cc:00:00:00:00:99"})}, time.Minute)
				f.inventory("sw2", snmp.Inventory{FDB: fdb("uplink", []string{"02:00:00:00:00:01", "cc:00:00:00:01:00"})}, time.Minute)
			},
			want: []string{"switch_port sw1>sw2 10|uplink"},
		},
		{
			name: "stale SNMP inventories and unknown MACs are ignored",
			setup: func(f *fixture) {
				f.device("sw1", "mac:02:00:00:00:00:01")
				f.device("pc", "mac:aa:00:00:00:00:01")
				f.inventory("sw1", snmp.Inventory{FDB: fdb("1", []string{"aa:00:00:00:00:01"})}, 8*24*time.Hour)
			},
			want: nil,
		},
		{
			name: "IP-only devices are found through ARP, gateway downstream of a device gets no cycle",
			setup: func(f *fixture) {
				f.subnet("192.168.8.0/24", "192.168.8.1")
				f.device("router", "mac:00:00:5e:00:01:01", "ip:192.168.8.1")
				f.device("sw1", "mac:02:00:00:00:00:01", "ip:192.168.8.2")
				f.device("cam", "ip:192.168.8.50")
				f.inventory("sw1", snmp.Inventory{
					FDB: fdb("1", []string{"00:00:5e:00:01:01"}, "2", []string{"aa:00:00:00:00:50"}),
					ARP: []snmp.ARPEntry{{IP: "192.168.8.50", MAC: "aa:00:00:00:00:50"}},
				}, time.Minute)
			},
			want: []string{"switch_port sw1>cam 2|", "switch_port sw1>router 1|"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			c.setup(f)
			f.run(c.settings)
			got := f.edges()
			want := append([]string(nil), c.want...)
			sort.Strings(want)
			if strings.Join(got, "\n") != strings.Join(want, "\n") {
				t.Errorf("edges:\n got %q\nwant %q", got, want)
			}
		})
	}
}

func TestRebuildIdempotent(t *testing.T) {
	f := newFixture(t)
	f.subnet("192.168.8.0/24", "192.168.8.1")
	f.device("router", "ip:192.168.8.1")
	f.device("sw1", "mac:02:00:00:00:00:01", "ip:192.168.8.2")
	f.device("pc", "mac:aa:00:00:00:00:01", "ip:192.168.8.10")
	f.inventory("sw1", snmp.Inventory{FDB: fdb("1", []string{"aa:00:00:00:00:01"})}, time.Minute)
	rc := f.run(nil)
	if s := rc.Stats(); s["created"] != 2 || s["switch_port"] != 1 || s["l3"] != 1 {
		t.Errorf("first run stats = %v", s)
	}
	first := firstSeen(t, f)
	time.Sleep(5 * time.Millisecond)
	rc = f.run(nil)
	if s := rc.Stats(); s["created"] != 0 || s["removed"] != 0 {
		t.Errorf("second run stats = %v", s)
	}
	if again := firstSeen(t, f); fmt.Sprint(again) != fmt.Sprint(first) {
		t.Errorf("first_seen changed: %v -> %v", first, again)
	}
	var data string
	if err := f.d.R.QueryRow("SELECT data FROM relations WHERE kind = 'switch_port'").Scan(&data); err != nil || !strings.Contains(data, `"via":"fdb"`) {
		t.Errorf("data = %s, %v", data, err)
	}
}

func firstSeen(t *testing.T, f *fixture) map[string]int64 {
	t.Helper()
	rows, err := f.d.R.Query("SELECT kind, parent_id, child_id, first_seen FROM relations")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var (
			kind      string
			p, c, fst int64
		)
		if err := rows.Scan(&kind, &p, &c, &fst); err != nil {
			t.Fatal(err)
		}
		out[fmt.Sprintf("%s %d>%d", kind, p, c)] = fst
	}
	return out
}

func loadGolden(t *testing.T, name string) snmp.Inventory {
	t.Helper()
	var inv snmp.Inventory
	if err := json.Unmarshal(plugintest.Fixture(t, name), &inv); err != nil {
		t.Fatal(err)
	}
	return inv
}

// TestRealRecordings runs the derivation on inventories decoded from real switch
// recordings (see ../snmp/golden_test.go).
func TestRealRecordings(t *testing.T) {
	f := newFixture(t)
	hp := loadGolden(t, "procurve.inventory.json")
	var trunkMAC string
	for _, e := range hp.FDB {
		if e.IfName == "Trk1" {
			trunkMAC = e.MAC
			break
		}
	}
	f.subnet("10.56.0.0/16", "")
	f.device("procurve", "mac:70:10:6f:8f:78:7f")
	f.device("camera", "mac:ac:cc:8e:0a:fb:c2") // FDB port 3
	f.device("axis", "mac:b8:a4:4f:b9:3d:1a")   // LLDP chassis MAC on port 27
	f.device("phone", "ip:10.56.32.113")        // LLDP chassis network address on port 44
	f.device("behind-trunk", "mac:"+trunkMAC)   // on the uplink trunk
	f.inventory("procurve", hp, time.Minute)

	ros := loadGolden(t, "routeros-crs317.inventory.json")
	f.device("crs317", "mac:cc:2d:e0:a3:3d:d5")
	f.device("vm-a", "mac:00:50:56:8f:32:19")      // sfp-sfpplus9 with one other MAC
	f.device("gw-router", "mac:64:d1:54:f2:a4:16") // LLDP neighbour (bridge, router)
	f.device("ip-phone", "mac:00:17:95:b0:f3:81")  // LLDP neighbour (telephone)
	f.inventory("crs317", ros, time.Minute)

	f.run(nil)
	got := strings.Join(f.edges(), "\n")
	for _, want := range []string{
		"switch_port procurve>camera 3|",
		"lldp procurve>axis 27|eth0",
		"lldp procurve>phone 44|1",
		"switch_port crs317>vm-a sfp-sfpplus9|",
		// the router is seen on several ports (one per VLAN interface): the lowest ifIndex wins
		"lldp crs317>gw-router sfp-sfpplus13|Phones",
		"lldp crs317>ip-phone sfp-sfpplus14|SW PORT",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "behind-trunk") {
		t.Errorf("device on the trunk must not be attached:\n%s", got)
	}
}

// TestPerformance500 derives and stores a topology of 500 devices, 8 switches.
func TestPerformance500(t *testing.T) {
	if testing.Short() {
		t.Skip("performance test (make test runs it separately)")
	}
	f := newFixture(t)
	f.subnet("10.1.0.0/16", "10.1.0.1")
	f.device("gw", "mac:00:00:5e:00:01:01", "ip:10.1.0.1")
	var invs []snmp.Inventory
	for s := 0; s < 8; s++ {
		f.device(fmt.Sprintf("sw%d", s), fmt.Sprintf("mac:02:00:00:00:00:%02x", s), fmt.Sprintf("ip:10.1.1.%d", s+2))
		invs = append(invs, snmp.Inventory{})
	}
	tx, err := f.d.W.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 491; i++ {
		mac := fmt.Sprintf("aa:00:00:00:%02x:%02x", i/256, i%256)
		ip := fmt.Sprintf("10.1.%d.%d", 2+i/200, 1+i%200)
		res, err := tx.Exec("INSERT INTO devices(hostname, created_at, updated_at) VALUES (?,?,?)", fmt.Sprintf("host%d", i), 1, 1)
		if err != nil {
			t.Fatal(err)
		}
		id, _ := res.LastInsertId()
		if _, err := tx.Exec("INSERT INTO device_macs(mac, device_id, source, first_seen, last_seen) VALUES (?,?,?,?,?)", mac, id, "t", 1, 1); err != nil {
			t.Fatal(err)
		}
		if _, err := tx.Exec("INSERT INTO device_ips(device_id, ip, ip_key, subnet_id, source, first_seen, last_seen) VALUES (?,?,?,1,?,?,?)",
			id, ip, netutil.IPKey(ip), "t", 1, 1); err != nil {
			t.Fatal(err)
		}
		if i%10 == 9 {
			continue // not in any FDB -> l3
		}
		s := i % 8
		port := i / 8 % 48
		invs[s].FDB = append(invs[s].FDB, snmp.FDBEntry{MAC: mac, VLAN: 1, BridgePort: port + 1, IfIndex: port + 1, IfName: fmt.Sprintf("Gi%d", port+1)})
		// every device is also visible on the uplink of the core switch
		invs[0].FDB = append(invs[0].FDB, snmp.FDBEntry{MAC: mac, VLAN: 1, BridgePort: 52, IfIndex: 52, IfName: "Te1"})
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	for s, inv := range invs {
		f.inventory(fmt.Sprintf("sw%d", s), inv, time.Minute)
	}
	for round := 0; round < 2; round++ {
		start := time.Now()
		rc := f.run(nil)
		took := time.Since(start)
		t.Logf("round %d: %v, stats %v", round, took, rc.Stats())
		if rc.Stats()["switch_port"].(int) < 400 || rc.Stats()["l3"].(int) < 50 {
			t.Errorf("stats = %v", rc.Stats())
		}
		if took > 2*time.Second {
			t.Errorf("rebuild took %v", took)
		}
	}
}

// countingHandler counts "Topologie aktualisiert" log records (= rebuilds).
type countingHandler struct {
	slog.Handler
	n *atomic.Int64
}

func (h countingHandler) Handle(ctx context.Context, r slog.Record) error {
	if r.Message == "Topologie aktualisiert" {
		h.n.Add(1)
	}
	return nil
}

func (h countingHandler) WithAttrs(as []slog.Attr) slog.Handler { return h }

func TestHandleRunFinished(t *testing.T) {
	f := newFixture(t)
	f.subnet("192.168.8.0/24", "192.168.8.1")
	f.device("router", "ip:192.168.8.1")
	f.device("pc", "ip:192.168.8.10")
	p := &Plugin{debounce: 30 * time.Millisecond}
	rc, _, _ := plugintest.RunContext(t, p, nil)
	rc.DB = f.d
	var rebuilds atomic.Int64
	rc.Log = slog.New(countingHandler{Handler: slog.NewTextHandler(io.Discard, nil), n: &rebuilds})
	ctx := context.Background()
	now := time.Now()

	for _, run := range []plugin.RunSummary{
		{PluginID: "nmap", Status: "success", Finished: now},
		{PluginID: "snmp", Status: "failed", Finished: now},
	} {
		if err := p.HandleRunFinished(ctx, rc, run); err != nil {
			t.Fatal(err)
		}
	}
	if rebuilds.Load() != 0 {
		t.Fatalf("rebuilds = %d for irrelevant runs", rebuilds.Load())
	}
	// three runs finishing together: one rebuild
	var wg sync.WaitGroup
	for _, id := range []string{"snmp", "arpscan", "proxmox"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			if err := p.HandleRunFinished(ctx, rc, plugin.RunSummary{PluginID: id, Status: "success", Finished: now}); err != nil {
				t.Error(err)
			}
		}(id)
		time.Sleep(5 * time.Millisecond)
	}
	wg.Wait()
	if rebuilds.Load() != 1 {
		t.Errorf("rebuilds = %d, want 1", rebuilds.Load())
	}
	if got := f.edges(); len(got) != 1 || got[0] != "l3 router>pc |" {
		t.Errorf("edges = %v", got)
	}
	// a run that finished before the last rebuild started is already covered
	if err := p.HandleRunFinished(ctx, rc, plugin.RunSummary{PluginID: "snmp", Status: "success", Finished: now}); err != nil {
		t.Fatal(err)
	}
	if rebuilds.Load() != 1 {
		t.Errorf("covered run triggered a rebuild")
	}
	// cancelled while waiting: no rebuild, no error
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if err := p.HandleRunFinished(cctx, rc, plugin.RunSummary{PluginID: "snmp", Status: "success", Finished: time.Now().Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	if rebuilds.Load() != 1 {
		t.Errorf("cancelled trigger rebuilt")
	}
}

func TestInfoAndSchema(t *testing.T) {
	p := &Plugin{}
	if err := p.Schema().Check(); err != nil {
		t.Fatal(err)
	}
	info := p.Info()
	if info.ID != "topology" || info.Kind != plugin.KindProcessor || !info.DefaultEnabled || info.DefaultSchedule != "*/15 * * * *" ||
		info.Targets != plugin.TargetNone {
		t.Errorf("info = %+v", info)
	}
	var _ plugin.Runner = p
	var _ plugin.RunFinishedHandler = p
	o := loadOptions(plugin.NewSettings(p.Schema().Defaults()), time.Now())
	if o.uplinkThreshold != 4 || !o.attachGateway || o.retention != 24*time.Hour || len(o.excludeTags) != 0 {
		t.Errorf("defaults = %+v", o)
	}
	rc, _, _ := plugintest.RunContext(t, p, nil)
	if err := p.Run(context.Background(), rc); err == nil {
		t.Error("run without database must fail")
	}
}
