package snmp

import (
	"context"
	"math"
	"strconv"
	"testing"
	"time"

	"github.com/gosnmp/gosnmp"

	"netscope/internal/plugin"
	"netscope/internal/plugin/plugintest"
)

type port struct {
	idx                     int
	name, alias             string
	ifType                  int
	speed                   uint
	admin, oper             int // 1 up, 2 down
	in, out, errors, discrd uint64
}

// agentPDUs builds an IF-MIB recording.
func agentPDUs(uptime uint32, ports []port) []gosnmp.SnmpPDU {
	pdus := []gosnmp.SnmpPDU{{Name: oidSysUpTime, Type: gosnmp.TimeTicks, Value: uptime}}
	for _, p := range ports {
		i := "." + strconv.Itoa(p.idx)
		pdus = append(pdus,
			gosnmp.SnmpPDU{Name: oidIfDescr + i, Type: gosnmp.OctetString, Value: []byte(p.name)},
			gosnmp.SnmpPDU{Name: oidIfType + i, Type: gosnmp.Integer, Value: p.ifType},
			gosnmp.SnmpPDU{Name: oidIfSpeed + i, Type: gosnmp.Gauge32, Value: uint(min(uint64(p.speed)*1_000_000, math.MaxUint32))},
			gosnmp.SnmpPDU{Name: oidIfAdminStatus + i, Type: gosnmp.Integer, Value: p.admin},
			gosnmp.SnmpPDU{Name: oidIfOperStatus + i, Type: gosnmp.Integer, Value: p.oper},
			gosnmp.SnmpPDU{Name: oidIfInOctets + i, Type: gosnmp.Counter32, Value: uint(p.in % (1 << 32))},
			gosnmp.SnmpPDU{Name: oidIfOutOctets + i, Type: gosnmp.Counter32, Value: uint(p.out % (1 << 32))},
			gosnmp.SnmpPDU{Name: oidIfInErrors + i, Type: gosnmp.Counter32, Value: uint(p.errors)},
			gosnmp.SnmpPDU{Name: oidIfOutErrors + i, Type: gosnmp.Counter32, Value: uint(0)},
			gosnmp.SnmpPDU{Name: oidIfInDiscards + i, Type: gosnmp.Counter32, Value: uint(p.discrd)},
			gosnmp.SnmpPDU{Name: oidIfOutDiscards + i, Type: gosnmp.Counter32, Value: uint(0)},
			gosnmp.SnmpPDU{Name: oidIfName + i, Type: gosnmp.OctetString, Value: []byte(p.name)},
			gosnmp.SnmpPDU{Name: oidIfHCInOctets + i, Type: gosnmp.Counter64, Value: p.in},
			gosnmp.SnmpPDU{Name: oidIfHCOutOctets + i, Type: gosnmp.Counter64, Value: p.out},
			gosnmp.SnmpPDU{Name: oidIfHighSpeed + i, Type: gosnmp.Gauge32, Value: p.speed},
			gosnmp.SnmpPDU{Name: oidIfAlias + i, Type: gosnmp.OctetString, Value: []byte(p.alias)},
		)
	}
	return pdus
}

func metricsOf(obs []plugin.Observation) map[string]float64 {
	out := map[string]float64{}
	for _, o := range obs {
		for _, m := range o.Metrics {
			out[m.Name+"|"+m.Key] = m.Avg
		}
	}
	return out
}

func TestTrafficRun(t *testing.T) {
	t0 := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	now := t0
	tr := &Traffic{now: func() time.Time { return now }}
	dataDir := t.TempDir()
	run := func(uptime uint32, ports []port) (*plugintest.Sink, *plugintest.Events) {
		t.Helper()
		agent := newFakeAgent(t, "public", agentPDUs(uptime, ports))
		rc, sink, evs := plugintest.RunContext(t, tr, map[string]any{"credentials": []any{1}, "port": agent.port(), "timeout": "1s", "retries": 0})
		rc.DataDir = dataDir
		rc.Creds = plugintest.Creds{1: v2c(1, "public", "public")}
		rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 7, Name: "core-sw", PrimaryIP: "127.0.0.1"}}}
		if err := tr.Run(context.Background(), rc); err != nil {
			t.Fatal(err)
		}
		return sink, evs
	}
	ports := []port{
		{1, "Gi0/1", "Uplink Firewall", 6, 1000, 1, 1, 1_000_000, 2_000_000, 5, 0},
		{2, "Gi0/2", "", 6, 100, 1, 1, 10, 20, 0, 0},
		{3, "Gi0/3", "", 6, 1000, 2, 2, 0, 0, 0, 0},     // switched off
		{10, "Vlan1", "", 135, 0, 1, 1, 500, 500, 0, 0}, // virtual
	}

	// 1. first poll: counters only
	sink, evs := run(100_000, ports)
	if len(sink.All()) != 0 || len(evs.Events) != 0 {
		t.Fatalf("first poll: %+v %+v", sink.All(), evs.Events)
	}

	// 2. five minutes later: 950 Mbit/s in on the uplink, 30 errors, port 2 loses its link
	now = t0.Add(5 * time.Minute)
	ports[0].in += 35_625_000_000
	ports[0].out += 375_000_000
	ports[0].errors += 30
	ports[1].oper = 2
	sink, evs = run(130_000, ports)
	m := metricsOf(sink.All())
	if math.Abs(m["if.in_bps|Gi0/1"]-950e6) > 1 || math.Abs(m["if.out_bps|Gi0/1"]-10e6) > 1 || math.Abs(m["if.util_pct|Gi0/1"]-95) > 0.01 ||
		math.Abs(m["if.errors|Gi0/1"]-6) > 0.01 || m["if.in_bps|Gi0/2"] != 0 {
		t.Errorf("metrics %v", m)
	}
	if _, ok := m["if.in_bps|Gi0/3"]; ok {
		t.Error("switched-off port measured")
	}
	if _, ok := m["if.in_bps|Vlan1"]; ok {
		t.Error("virtual interface measured")
	}
	if len(evs.Events) != 1 || evs.Events[0].Type != plugin.EvInterfaceSaturated || evs.Events[0].DeviceID != 7 ||
		evs.Events[0].Payload["direction"] != "eingehend" {
		t.Fatalf("events after poll 2 (port 2 without description stays quiet): %+v", evs.Events)
	}

	// 3. reboot: no rates across the reset; the uplink goes down → event
	now = t0.Add(10 * time.Minute)
	ports[0].oper = 2
	ports[0].in, ports[0].out = 100, 200
	sink, evs = run(3_000, ports)
	if len(metricsOf(sink.All())) != 0 {
		t.Errorf("rates across a reboot: %v", metricsOf(sink.All()))
	}
	if len(evs.Events) != 1 || evs.Events[0].Type != plugin.EvInterfaceDown || evs.Events[0].Payload["alias"] != "Uplink Firewall" {
		t.Fatalf("down event %+v", evs.Events)
	}
}

func TestTrafficDeltaAndSelection(t *testing.T) {
	if d, ok := delta(4_294_967_000, 200, false); !ok || d != 496 {
		t.Errorf("32-bit wrap: %d %v", d, ok)
	}
	if _, ok := delta(5_000_000_000_000, 100, true); ok {
		t.Error("64-bit counter reset counted")
	}
	s := plugin.NewSettings(map[string]any{"interfaces": "physical", "exclude": []any{"ether9*"}})
	for ifc, want := range map[Interface]bool{
		{Name: "ether1", Type: 6, OperStatus: "up"}:                     true,
		{Name: "ether9-mgmt", Type: 6}:                                  false,
		{Name: "bond0", Type: 161}:                                      true,
		{Name: "lo", Type: 24}:                                          false,
		{Name: "vlan10", Type: 135, OperStatus: "up"}:                   false,
		{Name: "ether2", Type: 6, AdminStatus: "down"}:                  false,
		{Name: "wlan0", Type: 71}:                                       true,
		{Name: "ETHER99", Type: 6, AdminStatus: "up", OperStatus: "up"}: false,
	} {
		if got := selected(s, ifc); got != want {
			t.Errorf("%s: %v", ifc.Name, got)
		}
	}
	if !selected(plugin.NewSettings(map[string]any{"interfaces": "up"}), Interface{Name: "vlan10", Type: 135, OperStatus: "up"}) {
		t.Error("up mode")
	}
	keys := ifKeys([]Interface{{Index: 1, Name: "eth0"}, {Index: 2, Name: "eth0"}, {Index: 3, Name: "eth1"}, {Index: 4}})
	if keys[1] != "eth0#1" || keys[2] != "eth0#2" || keys[3] != "eth1" || keys[4] != "#4" {
		t.Errorf("keys %v", keys)
	}
}

func TestTrafficWaitsForSilentDevices(t *testing.T) {
	t0 := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	now := t0
	tr := &Traffic{now: func() time.Time { return now }}
	dataDir := t.TempDir()
	agent := newFakeAgent(t, "public", agentPDUs(100, []port{{1, "eth0", "", 6, 1000, 1, 1, 0, 0, 0, 0}}))
	run := func(community string) map[string]any {
		t.Helper()
		rc, _, _ := plugintest.RunContext(t, tr, map[string]any{"credentials": []any{1}, "port": agent.port(), "timeout": "1s", "retries": 0})
		rc.DataDir = dataDir
		rc.Creds = plugintest.Creds{1: v2c(1, "c", community)}
		rc.Targets = plugin.Targets{Devices: []plugin.DeviceInfo{{ID: 3, Name: "phone", PrimaryIP: "127.0.0.1"}}}
		_ = tr.Run(context.Background(), rc)
		return rc.Stats()
	}
	if st := run("wrong"); st["no_answer"] != int64(1) {
		t.Fatalf("first run %v", st)
	}
	now = t0.Add(5 * time.Minute)
	if st := run("public"); st["waiting"] != 1 || st["targets"] != 0 {
		t.Fatalf("silent device asked again after 5 minutes: %v", st)
	}
	// the wait survives a restart (state file)
	tr = &Traffic{now: func() time.Time { return now }}
	now = t0.Add(61 * time.Minute)
	if st := run("public"); st["answered"] != int64(1) {
		t.Fatalf("retry after an hour %v", st)
	}
	// once it answers, an outage does not pause the polling
	now = t0.Add(66 * time.Minute)
	run("wrong")
	now = t0.Add(71 * time.Minute)
	if st := run("public"); st["answered"] != int64(1) {
		t.Fatalf("device with fresh counters skipped: %v", st)
	}
}
