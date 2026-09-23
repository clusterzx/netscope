package snmp

import (
	"context"
	"net"
	"net/netip"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

// fakeAgent is a minimal SNMPv2c agent serving a recording (GET, GETNEXT, GETBULK).
// Requests with another community are dropped like real agents do.
type fakeAgent struct {
	conn      net.PacketConn
	community string
	pdus      []gosnmp.SnmpPDU
	dropped   atomic.Int64
	answered  atomic.Int64
}

func oidLess(a, b string) bool {
	pa, pb := strings.Split(strings.TrimPrefix(a, "."), "."), strings.Split(strings.TrimPrefix(b, "."), ".")
	for i := 0; i < len(pa) && i < len(pb); i++ {
		x, _ := strconv.ParseUint(pa[i], 10, 64)
		y, _ := strconv.ParseUint(pb[i], 10, 64)
		if x != y {
			return x < y
		}
	}
	return len(pa) < len(pb)
}

func newFakeAgent(t *testing.T, community string, pdus []gosnmp.SnmpPDU) *fakeAgent {
	t.Helper()
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	// recordings may contain duplicate OIDs: keep the last value like an agent would
	byOID := map[string]gosnmp.SnmpPDU{}
	for _, p := range pdus {
		byOID[p.Name] = p
	}
	sorted := make([]gosnmp.SnmpPDU, 0, len(byOID))
	for _, p := range byOID {
		sorted = append(sorted, p)
	}
	sort.Slice(sorted, func(i, j int) bool { return oidLess(sorted[i].Name, sorted[j].Name) })
	a := &fakeAgent{conn: conn, community: community, pdus: sorted}
	go a.serve()
	return a
}

func (a *fakeAgent) port() int { return a.conn.LocalAddr().(*net.UDPAddr).Port }

func (a *fakeAgent) next(oid string) (gosnmp.SnmpPDU, bool) {
	i := sort.Search(len(a.pdus), func(i int) bool { return oidLess(oid, a.pdus[i].Name) })
	if i < len(a.pdus) {
		return a.pdus[i], true
	}
	return gosnmp.SnmpPDU{Name: oid, Type: gosnmp.EndOfMibView}, false
}

func (a *fakeAgent) serve() {
	buf := make([]byte, 65535)
	for {
		n, addr, err := a.conn.ReadFrom(buf)
		if err != nil {
			return
		}
		dec := &gosnmp.GoSNMP{Version: gosnmp.Version2c}
		req, err := dec.SnmpDecodePacket(buf[:n])
		if err != nil || req.Community != a.community {
			a.dropped.Add(1)
			continue
		}
		var vars []gosnmp.SnmpPDU
		switch req.PDUType {
		case gosnmp.GetRequest:
			for _, v := range req.Variables {
				i := sort.Search(len(a.pdus), func(i int) bool { return !oidLess(a.pdus[i].Name, v.Name) })
				if i < len(a.pdus) && a.pdus[i].Name == v.Name {
					vars = append(vars, a.pdus[i])
				} else {
					vars = append(vars, gosnmp.SnmpPDU{Name: v.Name, Type: gosnmp.NoSuchObject})
				}
			}
		case gosnmp.GetNextRequest:
			for _, v := range req.Variables {
				p, _ := a.next(v.Name)
				vars = append(vars, p)
			}
		case gosnmp.GetBulkRequest:
			for _, v := range req.Variables {
				cur := v.Name
				for r := uint32(0); r < req.MaxRepetitions; r++ {
					p, ok := a.next(cur)
					vars = append(vars, p)
					if !ok {
						break
					}
					cur = p.Name
				}
			}
		default:
			continue
		}
		resp := &gosnmp.SnmpPacket{Version: gosnmp.Version2c, Community: req.Community, PDUType: gosnmp.GetResponse,
			RequestID: req.RequestID, Variables: vars}
		out, err := resp.MarshalMsg()
		if err != nil {
			continue
		}
		a.answered.Add(1)
		_, _ = a.conn.WriteTo(out, addr)
	}
}

func v2c(id int64, name, community string) *plugin.Credential {
	return &plugin.Credential{ID: id, Name: name, Type: plugin.CredSNMPv2c, Secret: map[string]string{"community": community}}
}

func TestRunWithFakeAgent(t *testing.T) {
	agent := newFakeAgent(t, "geheim", loadSnmprec(t, "librenms-procurve.snmprec"))
	p := &Plugin{}
	settings := map[string]any{"credentials": []any{1, 2}, "port": agent.port(), "timeout": "1s", "retries": 0}
	rc, sink, _ := plugintest.RunContext(t, p, settings)
	rc.Creds = plugintest.Creds{1: v2c(1, "falsch", "public"), 2: v2c(2, "richtig", "geheim")}
	rc.Inventory = &plugintest.Inventory{SubnetsList: []plugin.SubnetTarget{{ID: 1, CIDR: netip.MustParsePrefix("10.0.0.0/8")}}}
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 10, Name: "switch", PrimaryIP: "127.0.0.1"}}}
	ctx := context.Background()
	if err := p.Run(ctx, rc); err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) < 2 {
		t.Fatalf("observations = %d", len(obs))
	}
	dev := obs[0]
	inv, ok := dev.Inventory.(Inventory)
	if !ok || dev.DeviceID != 10 || !dev.Present || dev.Vendor != "HPE" || dev.Hostname != "<private>" || dev.IP != "127.0.0.1" {
		t.Fatalf("device observation = %+v", dev)
	}
	if !reflect2(inv.Walked, []string{TableSystem, TableInterfaces, TableARP, TableFDB, TableLLDP}) || len(inv.Errors) != 0 {
		t.Errorf("walked = %v, errors = %v", inv.Walked, inv.Errors)
	}
	if len(inv.FDB) != 1060 || len(inv.LLDP) != 7 || len(inv.Interfaces) == 0 || inv.System.Version != "2c" || inv.System.Enterprise != 11 {
		t.Errorf("inventory: fdb %d, lldp %d, interfaces %d, system %+v", len(inv.FDB), len(inv.LLDP), len(inv.Interfaces), inv.System)
	}
	for _, n := range obs[1:] {
		if n.Present || n.Create || n.DeviceID != 0 || len(n.MACs) != 1 || !strings.HasPrefix(n.IP, "10.") {
			t.Errorf("neighbour observation = %+v", n)
		}
	}
	if agent.dropped.Load() == 0 {
		t.Error("the wrong community must have been tried first")
	}
	store, err := loadCredStore(filepath.Join(rc.DataDir, "credentials.json"))
	if err != nil || store.get(10) != 2 {
		t.Errorf("remembered = %d, %v", store.get(10), err)
	}

	// second run: remembered credential first, no timeout on the wrong community
	dropped := agent.dropped.Load()
	rc2, sink2, _ := plugintest.RunContext(t, p, settings)
	rc2.DataDir, rc2.Creds, rc2.Inventory, rc2.Targets = rc.DataDir, rc.Creds, rc.Inventory, rc.Targets
	start := time.Now()
	if err := p.Run(ctx, rc2); err != nil {
		t.Fatal(err)
	}
	if agent.dropped.Load() != dropped || len(sink2.All()) != len(obs) || time.Since(start) > 900*time.Millisecond {
		t.Errorf("second run: dropped %d -> %d, observations %d, took %v", dropped, agent.dropped.Load(), len(sink2.All()), time.Since(start))
	}
}

func TestRunTableSelection(t *testing.T) {
	agent := newFakeAgent(t, "public", loadSnmprec(t, "librenms-routeros-crs317.snmprec"))
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"credentials": []any{1}, "port": agent.port(), "timeout": "1s",
		"walk_interfaces": false, "walk_arp": false})
	rc.Creds = plugintest.Creds{1: v2c(1, "public", "public")}
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 3, PrimaryIP: "127.0.0.1"}}}
	if err := p.Run(context.Background(), rc); err != nil {
		t.Fatal(err)
	}
	obs := sink.All()
	if len(obs) != 1 {
		t.Fatalf("observations = %d (no ARP import without walk_arp)", len(obs))
	}
	inv := obs[0].Inventory.(Inventory)
	if len(inv.Interfaces) != 0 || len(inv.ARP) != 0 || !reflect2(inv.Walked, []string{TableSystem, TableFDB, TableLLDP}) {
		t.Errorf("walked = %v, interfaces = %d, arp = %d", inv.Walked, len(inv.Interfaces), len(inv.ARP))
	}
	// interface names are still resolved for FDB and LLDP
	if len(inv.FDB) != 41 || inv.FDB[0].IfName == "" || len(inv.LLDP) != 18 || inv.LLDP[0].LocalPort == "" {
		t.Errorf("fdb = %d, lldp = %d", len(inv.FDB), len(inv.LLDP))
	}
	if obs[0].Vendor != "MikroTik" {
		t.Errorf("vendor = %q", obs[0].Vendor)
	}
}

func TestRunNoAnswer(t *testing.T) {
	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close() // swallows requests
	p := &Plugin{}
	rc, sink, _ := plugintest.RunContext(t, p, map[string]any{"credentials": []any{1}, "port": conn.LocalAddr().(*net.UDPAddr).Port,
		"timeout": "1s", "retries": 0})
	rc.Creds = plugintest.Creds{1: v2c(1, "public", "public")}
	rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 3, PrimaryIP: "127.0.0.1"}, {ID: 4}}}
	if err := p.Run(context.Background(), rc); err == nil || len(sink.All()) != 0 {
		t.Fatalf("err = %v, observations = %d", err, len(sink.All()))
	}
	if rc.Stats()["no_answer"] != 1 || rc.Stats()["targets"] != 1 {
		t.Errorf("stats = %v", rc.Stats())
	}
}

func TestNewClient(t *testing.T) {
	cc := clientConfig{port: 161, timeout: time.Second, retries: 1, maxRepetitions: 25}
	ctx := context.Background()
	g, err := newClient(ctx, "10.0.0.1", v2c(1, "c", "public"), cc)
	if err != nil || g.Version != gosnmp.Version2c || g.Community != "public" || g.MaxRepetitions != 25 {
		t.Fatalf("v2c = %+v, %v", g, err)
	}
	v3 := &plugin.Credential{ID: 2, Name: "v3", Type: plugin.CredSNMPv3,
		Public: map[string]string{"username": "nsv3", "security_level": "authPriv", "auth_protocol": "SHA256", "priv_protocol": "AES256"},
		Secret: map[string]string{"auth_password": "authpass123", "priv_password": "privpass123"}}
	g, err = newClient(ctx, "10.0.0.1", v3, cc)
	if err != nil || g.Version != gosnmp.Version3 || g.MsgFlags != gosnmp.AuthPriv {
		t.Fatalf("v3 = %+v, %v", g, err)
	}
	usm := g.SecurityParameters.(*gosnmp.UsmSecurityParameters)
	if usm.UserName != "nsv3" || usm.AuthenticationProtocol != gosnmp.SHA256 || usm.PrivacyProtocol != gosnmp.AES256 {
		t.Errorf("usm = %+v", usm)
	}
	v3.Public["security_level"] = "authNoPriv"
	if g, err = newClient(ctx, "10.0.0.1", v3, cc); err != nil || g.MsgFlags != gosnmp.AuthNoPriv {
		t.Errorf("authNoPriv = %v", err)
	}
	for _, bad := range []*plugin.Credential{
		{Name: "x", Type: plugin.CredSNMPv2c},
		{Name: "x", Type: plugin.CredSNMPv3, Public: map[string]string{"username": "u"}}, // authPriv without passwords
		{Name: "x", Type: plugin.CredSNMPv3, Public: map[string]string{"username": "u", "security_level": "authNoPriv", "auth_protocol": "XYZ"},
			Secret: map[string]string{"auth_password": "p"}},
		{Name: "x", Type: plugin.CredSSH},
	} {
		if _, err := newClient(ctx, "10.0.0.1", bad, cc); err == nil {
			t.Errorf("%+v must fail", bad)
		}
	}
}

func TestSchema(t *testing.T) {
	p := &Plugin{}
	if err := p.Schema().Check(); err != nil {
		t.Fatal(err)
	}
	cfg := loadConfig(plugin.NewSettings(p.Schema().Defaults()), "/data/plugins/snmp")
	if cfg.client != (clientConfig{port: 161, timeout: 3 * time.Second, retries: 1, maxRepetitions: 25}) ||
		cfg.collect != (collectOptions{interfaces: true, arp: true, fdb: true, lldp: true}) || !cfg.importARP {
		t.Errorf("defaults = %+v", cfg)
	}
	info := p.Info()
	if info.ID != "snmp" || info.DefaultEnabled || info.DefaultSchedule != "*/30 * * * *" || info.DefaultConcurrency != 16 ||
		info.DefaultTimeout != 20*time.Minute || info.Targets != plugin.TargetDevices || info.Presence {
		t.Errorf("info = %+v", info)
	}
}

func reflect2(a, b []string) bool {
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
