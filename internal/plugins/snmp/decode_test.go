package snmp

import (
	"net/netip"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/gosnmp/gosnmp"
)

func TestDecodeSystem(t *testing.T) {
	s := decodeSystem(loadSnmprec(t, "netsnmp-alpine.snmprec"))
	want := System{
		Descr:         "Linux netscope 7.0.14-8-pve #1 SMP PREEMPT_DYNAMIC PMX 7.0.14-8 (2026-07-28T10:28Z) x86_64",
		ObjectID:      ".1.3.6.1.4.1.8072.3.2.10",
		Enterprise:    8072,
		Vendor:        "net-snmp",
		UptimeSeconds: 301,
		Contact:       "admin@example.org",
		Name:          "ns-snmp-fixture",
		Location:      "Homelab Rack 1",
	}
	if s != want {
		t.Errorf("net-snmp = %+v", s)
	}
	for file, vendor := range map[string]string{
		"librenms-procurve.snmprec":        "HPE",
		"librenms-routeros-crs317.snmprec": "MikroTik",
		"librenms-arubaos-cx.snmprec":      "Aruba",
		"librenms-dlink-dgs1510.snmprec":   "D-Link",
		"librenms-edgeswitch-us8.snmprec":  "Ubiquiti",
	} {
		s := decodeSystem(loadSnmprec(t, file))
		if s.Vendor != vendor || s.ObjectID == "" {
			t.Errorf("%s: %+v", file, s)
		}
	}
	if n := enterpriseNumber(".1.3.6.1.4.1.41112.1.6"); n != 41112 {
		t.Errorf("enterprise = %d", n)
	}
	if n := enterpriseNumber(".1.3.6.1.2.1"); n != 0 {
		t.Errorf("enterprise = %d", n)
	}
}

func TestVendorTable(t *testing.T) {
	want := map[int]string{9: "Cisco", 11: "HPE", 2636: "Juniper", 41112: "Ubiquiti", 14988: "MikroTik", 4526: "NETGEAR", 11863: "TP-Link",
		171: "D-Link", 674: "Dell", 318: "APC", 6574: "Synology", 24681: "QNAP", 12356: "Fortinet", 30065: "Arista", 2011: "Huawei",
		8072: "net-snmp", 311: "Microsoft", 6876: "VMware", 207: "Allied Telesis", 1916: "Extreme Networks", 25506: "H3C", 3955: "Linksys"}
	for n, name := range want {
		if v, ok := lookupVendor(n); !ok || v.Name != name {
			t.Errorf("%d = %+v", n, v)
		}
	}
	if len(vendors) < 30 {
		t.Errorf("only %d vendors", len(vendors))
	}
	if v, _ := lookupVendor(8072); !v.Agent {
		t.Error("net-snmp is agent software")
	}
}

func TestDecodeInterfaces(t *testing.T) {
	ifs := decodeInterfaces(loadSnmprec(t, "netsnmp-alpine.snmprec"))
	want := []Interface{
		{Index: 1, Name: "lo", Descr: "lo", Type: 24, SpeedMbps: 10, AdminStatus: "up", OperStatus: "up"},
		{Index: 2, Name: "eth0", Descr: "eth0", Type: 6, MAC: "bc:24:11:27:22:b5", SpeedMbps: 10000, AdminStatus: "up", OperStatus: "up"},
		{Index: 3, Name: "docker0", Descr: "docker0", Type: 6, MAC: "7e:3f:ab:f0:4d:80", SpeedMbps: 10000, AdminStatus: "up", OperStatus: "up"},
		{Index: 39, Name: "veth9ba14f9", Descr: "veth9ba14f9", Type: 6, MAC: "96:a8:8b:27:6b:0e", SpeedMbps: 10000, AdminStatus: "up", OperStatus: "up"},
	}
	if !reflect.DeepEqual(ifs, want) {
		t.Errorf("net-snmp interfaces:\n got %+v\nwant %+v", ifs, want)
	}
	ifs = decodeInterfaces(loadSnmprec(t, "librenms-procurve.snmprec"))
	byName := map[string]Interface{}
	for _, i := range ifs {
		byName[i.Name] = i
	}
	if trk := byName["Trk1"]; trk.Index != 962 || trk.Type != 161 {
		t.Errorf("Trk1 = %+v", trk)
	}
	if p3 := byName["3"]; p3.Alias != "S:CAM1" || p3.SpeedMbps != 100 || p3.OperStatus != "up" || p3.MAC != "70:10:6f:8f:78:7d" {
		t.Errorf("port 3 = %+v", p3)
	}
}

func TestDecodeARP(t *testing.T) {
	pdus := loadSnmprec(t, "netsnmp-alpine.snmprec")
	ifs := decodeInterfaces(pdus)
	arp := decodeARP(pdus, ifs)
	if len(arp) != 26 {
		t.Fatalf("arp entries = %d, want 26", len(arp))
	}
	if arp[0] != (ARPEntry{IfIndex: 2, IfName: "eth0", IP: "192.168.8.1", MAC: "94:83:c4:a8:a4:0a", Type: "dynamic"}) {
		t.Errorf("first = %+v", arp[0])
	}
	// ipNetToPhysicalTable gives the same result when ipNetToMediaTable is missing
	var physOnly []gosnmp.SnmpPDU
	for _, p := range pdus {
		if !strings.HasPrefix(p.Name, ".1.3.6.1.2.1.4.22.") {
			physOnly = append(physOnly, p)
		}
	}
	if got := decodeARP(physOnly, ifs); !reflect.DeepEqual(got, arp) {
		t.Errorf("ipNetToPhysical:\n got %+v\nwant %+v", got[:3], arp[:3])
	}
	// LibreNMS recording: 0.0.0.0 entries are skipped
	for _, e := range decodeARP(loadSnmprec(t, "librenms-procurve.snmprec"), nil) {
		if e.IP == "0.0.0.0" {
			t.Errorf("unspecified address not skipped: %+v", e)
		}
	}
}

func fdbByMAC(list []FDBEntry) map[string]FDBEntry {
	m := map[string]FDBEntry{}
	for _, e := range list {
		m[e.MAC+"|"+itoa(e.VLAN)] = e
	}
	return m
}

func TestDecodeFDB(t *testing.T) {
	// HP ProCurve: Q-BRIDGE with a trunk (Trk1) carrying the uplink MACs
	pdus := loadSnmprec(t, "librenms-procurve.snmprec")
	fdb := decodeFDB(pdus, decodeInterfaces(pdus))
	perPort := map[string]int{}
	for _, e := range fdb {
		if e.BridgePort == 0 {
			t.Fatalf("CPU entries must be skipped: %+v", e)
		}
		perPort[e.IfName]++
	}
	if len(fdb) != 1060 || perPort["Trk1"] != 1019 || perPort["3"] != 1 {
		t.Errorf("entries = %d, per port = %v", len(fdb), perPort)
	}
	// the recording has no dot1qTpFdbStatus column; fdbId 5 is shared by VLAN 5 and 19
	if e, ok := fdbByMAC(fdb)["ac:cc:8e:0a:fb:c2|5"]; !ok || e != (FDBEntry{MAC: "ac:cc:8e:0a:fb:c2", VLAN: 5, BridgePort: 3, IfIndex: 3, IfName: "3"}) {
		t.Errorf("port 3 entry = %+v", e)
	}

	// Ubiquiti EdgeSwitch: bridge port 1 = ifIndex 1000001, VLANs via fdb ids
	pdus = loadSnmprec(t, "librenms-edgeswitch-us8.snmprec")
	fdb = decodeFDB(pdus, decodeInterfaces(pdus))
	vlans := map[int]bool{}
	for _, e := range fdb {
		vlans[e.VLAN] = true
		if e.BridgePort == 1 && (e.IfIndex != 1000001 || e.IfName != "GigabitEthernet 1/1") {
			t.Errorf("port 1 = %+v", e)
		}
	}
	// 10 entries on bridge port 0 (CPU) are skipped
	if len(fdb) != 313 || !vlans[1] || !vlans[10] {
		t.Errorf("edgeswitch: %d entries, vlans %v", len(fdb), vlans)
	}

	// MikroTik CRS317: BRIDGE-MIB without VLAN, dot1dTpFdbPort carries ifIndexes
	pdus = loadSnmprec(t, "librenms-routeros-crs317.snmprec")
	fdb = decodeFDB(pdus, decodeInterfaces(pdus))
	perPort = map[string]int{}
	for _, e := range fdb {
		if e.VLAN != 0 {
			t.Errorf("vlan = %d", e.VLAN)
		}
		perPort[e.IfName]++
	}
	if len(fdb) != 41 || perPort["server-trunk"] != 31 || perPort["sfp-sfpplus9"] != 2 {
		t.Errorf("routeros: %d entries, per port %v", len(fdb), perPort)
	}

	// AOS-CX: plain BRIDGE-MIB
	pdus = loadSnmprec(t, "librenms-arubaos-cx.snmprec")
	if fdb = decodeFDB(pdus, decodeInterfaces(pdus)); len(fdb) == 0 || fdb[0].IfName == "" {
		t.Errorf("aruba: %+v", fdb)
	}
	// net-snmp has no bridge
	if fdb := decodeFDB(loadSnmprec(t, "netsnmp-alpine.snmprec"), nil); len(fdb) != 0 {
		t.Errorf("net-snmp fdb = %+v", fdb)
	}
}

func TestDecodeLLDP(t *testing.T) {
	pdus := loadSnmprec(t, "librenms-procurve.snmprec")
	nb := decodeLLDP(pdus, decodeInterfaces(pdus))
	if len(nb) != 7 {
		t.Fatalf("procurve neighbours = %d", len(nb))
	}
	axis := nb[0]
	if axis.LocalPortNum != 27 || axis.LocalPort != "27" || axis.LocalIfIndex != 27 || axis.ChassisMAC != "b8:a4:4f:b9:3d:1a" ||
		axis.PortDesc != "eth0" || axis.SysName != "axis" || !reflect.DeepEqual(axis.ManagementAddresses, []string{"10.1.1.1"}) ||
		axis.RemotePort() != "eth0" {
		t.Errorf("axis = %+v", axis)
	}
	phone := nb[4]
	if phone.ChassisIDSubtype != "networkAddress" || phone.ChassisID != "10.56.32.113" || phone.ChassisMAC != "" ||
		phone.PortMAC != "00:04:f2:c6:70:78" || phone.SysName != "Polycom SoundPoint IP 335" {
		t.Errorf("phone = %+v", phone)
	}
	if ap := nb[5]; ap.LocalPort != "A1" || ap.LocalIfIndex != 55 || ap.PortIDSubtype != "interfaceName" || ap.RemotePort() != "1/7/18" {
		t.Errorf("aruba ap = %+v", ap)
	}

	// MikroTik: single-component index, local interface from mtxrNeighborInterfaceID,
	// capabilities as integer bitmap, chassis id as text
	pdus = loadSnmprec(t, "librenms-routeros-crs317.snmprec")
	nb = decodeLLDP(pdus, decodeInterfaces(pdus))
	if len(nb) != 18 {
		t.Fatalf("routeros neighbours = %d", len(nb))
	}
	var router *LLDPNeighbor
	for i := range nb {
		// neighbour 8: lldpRemPortId "Guest", mtxrNeighborInterfaceID 15
		if nb[i].ChassisMAC == "64:d1:54:f2:a4:16" && nb[i].PortID == "Guest" && nb[i].LocalIfIndex == 15 {
			router = &nb[i]
		}
		if nb[i].LocalIfIndex == 0 || nb[i].LocalPort == "" {
			t.Errorf("unresolved local port: %+v", nb[i])
		}
	}
	if router == nil || router.SysName != "router.jason.corp.local" || router.LocalPort != "sfp-sfpplus15" ||
		!reflect.DeepEqual(router.Capabilities, []string{"bridge", "router"}) {
		t.Errorf("router = %+v", router)
	}

	// EdgeSwitch and D-Link: lldpLocPortNum is the bridge port number
	pdus = loadSnmprec(t, "librenms-edgeswitch-us8.snmprec")
	nb = decodeLLDP(pdus, decodeInterfaces(pdus))
	if len(nb) != 1 || nb[0].LocalIfIndex != 1000001 || nb[0].LocalPort != "GigabitEthernet 1/1" || nb[0].SysName != "US8Location" {
		t.Errorf("edgeswitch = %+v", nb)
	}
	pdus = loadSnmprec(t, "librenms-dlink-dgs1510.snmprec")
	nb = decodeLLDP(pdus, decodeInterfaces(pdus))
	if len(nb) != 2 || nb[0].LocalPort != "eth1/0/24" || nb[0].RemotePort() != "eth1/0/24" || nb[0].SysName != "Kos16c" {
		t.Errorf("dlink = %+v", nb)
	}
	pdus = loadSnmprec(t, "librenms-arubaos-cx.snmprec")
	if nb = decodeLLDP(pdus, decodeInterfaces(pdus)); len(nb) != 18 {
		t.Errorf("aruba neighbours = %d", len(nb))
	}
	if nb := decodeLLDP(loadSnmprec(t, "netsnmp-alpine.snmprec"), nil); len(nb) != 0 {
		t.Errorf("net-snmp lldp = %+v", nb)
	}
}

func TestDecodeLLDPLocal(t *testing.T) {
	pdus := []gosnmp.SnmpPDU{
		{Name: oidLldpLocChassisIDSub, Type: gosnmp.Integer, Value: 4},
		{Name: oidLldpLocChassisID, Type: gosnmp.OctetString, Value: []byte{0x64, 0xd1, 0x54, 0xf2, 0xa4, 0x10}},
		{Name: oidLldpLocSysName, Type: gosnmp.OctetString, Value: []byte("core-sw")},
	}
	l := decodeLLDPLocal(pdus)
	if l == nil || *l != (LLDPLocal{ChassisIDSubtype: "macAddress", ChassisID: "64:d1:54:f2:a4:10", ChassisMAC: "64:d1:54:f2:a4:10", SysName: "core-sw"}) {
		t.Errorf("local = %+v", l)
	}
	if decodeLLDPLocal(loadSnmprec(t, "netsnmp-alpine.snmprec")) != nil {
		t.Error("no LLDP data must give nil")
	}
}

func TestLLDPIDAndCapabilities(t *testing.T) {
	cases := []struct {
		subtype string
		raw     []byte
		id, mac string
	}{
		{"macAddress", []byte{0xb8, 0xa4, 0x4f, 0xb9, 0x3d, 0x1a}, "b8:a4:4f:b9:3d:1a", "b8:a4:4f:b9:3d:1a"},
		{"macAddress", []byte("64:D1:54:F2:A4:16"), "64:d1:54:f2:a4:16", "64:d1:54:f2:a4:16"},
		{"networkAddress", []byte{1, 192, 168, 8, 1}, "192.168.8.1", ""},
		{"interfaceName", []byte("eth0"), "eth0", ""},
		{"local", []byte("1/1/10"), "1/1/10", ""},
	}
	for _, c := range cases {
		if id, mac := lldpID(c.subtype, c.raw); id != c.id || mac != c.mac {
			t.Errorf("%s %x = %q %q", c.subtype, c.raw, id, mac)
		}
	}
	if got := decodeCapabilities([]byte{0x28}); !reflect.DeepEqual(got, []string{"bridge", "router"}) {
		t.Errorf("caps = %v", got)
	}
	if got := decodeCapabilities([]byte("01 7")); got != nil {
		t.Errorf("malformed caps = %v", got)
	}
}

func TestObservations(t *testing.T) {
	pdus := loadSnmprec(t, "netsnmp-alpine.snmprec")
	inv := Inventory{System: decodeSystem(pdus), Interfaces: decodeInterfaces(pdus)}
	inv.ARP = decodeARP(pdus, inv.Interfaces)
	o := deviceObservation(5, "192.168.8.123", inv)
	if o.DeviceID != 5 || !o.Present || o.Hostname != "ns-snmp-fixture" || o.Vendor != "" || o.IP != "192.168.8.123" ||
		o.Attrs["snmp.sysObjectID"] != ".1.3.6.1.4.1.8072.3.2.10" || o.Attrs["snmp.sysLocation"] != "Homelab Rack 1" ||
		o.Attrs["snmp.sysContact"] != "admin@example.org" || !strings.HasPrefix(o.Attrs["snmp.sysDescr"], "Linux netscope") || len(o.Attrs) != 4 {
		t.Errorf("device observation = %+v", o)
	}
	if _, isInv := o.Inventory.(Inventory); !isInv {
		t.Errorf("inventory type %T", o.Inventory)
	}
	hp := decodeSystem(loadSnmprec(t, "librenms-procurve.snmprec"))
	if o := deviceObservation(1, "10.0.0.1", Inventory{System: hp}); o.Vendor != "HPE" || o.Hostname != "<private>" {
		t.Errorf("hp observation = %+v", o)
	}

	nb := neighborObservations(inv, []netip.Prefix{netip.MustParsePrefix("192.168.8.0/24")})
	if len(nb) != 26 {
		t.Fatalf("neighbours = %d", len(nb))
	}
	for _, n := range nb {
		if n.Present || n.Create || len(n.MACs) != 1 || n.IP == "" || n.DeviceID != 0 {
			t.Errorf("neighbour = %+v", n)
		}
	}
	if nb[0].IP != "192.168.8.1" || nb[0].MACs[0] != "94:83:c4:a8:a4:0a" {
		t.Errorf("first neighbour = %+v", nb[0])
	}
	if got := neighborObservations(inv, []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8")}); len(got) != 0 {
		t.Errorf("outside subnets = %d", len(got))
	}
	if got := neighborObservations(inv, nil); got != nil {
		t.Errorf("without subnets = %d", len(got))
	}
	// own interface MACs are never imported as neighbours
	inv.ARP = append(inv.ARP, ARPEntry{IP: "192.168.8.200", MAC: "bc:24:11:27:22:b5"})
	for _, n := range neighborObservations(inv, []netip.Prefix{netip.MustParsePrefix("192.168.8.0/24")}) {
		if n.IP == "192.168.8.200" {
			t.Error("own MAC imported")
		}
	}
}

func itoa(n int) string { return strconv.Itoa(n) }
