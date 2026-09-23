package snmp

import (
	"encoding/hex"
	"fmt"
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/gosnmp/gosnmp"

	"netscope/internal/netutil"
)

// The decoders turn raw PDUs into Inventory structures. They are pure functions over
// []gosnmp.SnmpPDU (as returned by BulkWalkAll or loaded from snmprec fixtures), ignore
// PDUs outside their tables and never fail on unexpected types or missing columns.

// DecodeInventory decodes every supported table found in pdus: system group, IF-MIB,
// IP-MIB ARP, BRIDGE/Q-BRIDGE FDB and LLDP. Walked, Errors and System.Version are left
// empty (they describe the query, not the data).
func DecodeInventory(pdus []gosnmp.SnmpPDU) Inventory {
	inv := Inventory{System: decodeSystem(pdus), Interfaces: decodeInterfaces(pdus)}
	inv.ARP = decodeARP(pdus, inv.Interfaces)
	inv.FDB = decodeFDB(pdus, inv.Interfaces)
	inv.LLDPLocal = decodeLLDPLocal(pdus)
	inv.LLDP = decodeLLDP(pdus, inv.Interfaces)
	return inv
}

// ---------------------------------------------------------------- value helpers

// column returns the PDUs below a column OID keyed by their index suffix ("1.4.10.0.0.1").
func column(pdus []gosnmp.SnmpPDU, oid string) map[string]gosnmp.SnmpPDU {
	out := map[string]gosnmp.SnmpPDU{}
	prefix := oid + "."
	for _, p := range pdus {
		name := p.Name
		if !strings.HasPrefix(name, ".") {
			name = "." + name
		}
		if idx, ok := strings.CutPrefix(name, prefix); ok && idx != "" && !isException(p) {
			out[idx] = p
		}
	}
	return out
}

// scalar returns the PDU of a scalar OID (".x.y.0").
func scalar(pdus []gosnmp.SnmpPDU, oid string) (gosnmp.SnmpPDU, bool) {
	for _, p := range pdus {
		name := p.Name
		if !strings.HasPrefix(name, ".") {
			name = "." + name
		}
		if name == oid && !isException(p) {
			return p, true
		}
	}
	return gosnmp.SnmpPDU{}, false
}

func isException(p gosnmp.SnmpPDU) bool {
	return p.Type == gosnmp.NoSuchObject || p.Type == gosnmp.NoSuchInstance || p.Type == gosnmp.EndOfMibView || p.Type == gosnmp.Null
}

// pduInt returns the numeric value of a PDU.
func pduInt(p gosnmp.SnmpPDU) (int64, bool) {
	switch v := p.Value.(type) {
	case int:
		return int64(v), true
	case int64:
		return v, true
	case int32:
		return int64(v), true
	case uint:
		return int64(v), true
	case uint32:
		return int64(v), true
	case uint64:
		return int64(v), true
	case []byte:
		n, err := strconv.ParseInt(strings.TrimSpace(string(v)), 10, 64)
		return n, err == nil
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n, err == nil
	}
	return 0, false
}

// pduBytes returns the raw octets of an OctetString PDU.
func pduBytes(p gosnmp.SnmpPDU) []byte {
	switch v := p.Value.(type) {
	case []byte:
		return v
	case string:
		return []byte(v)
	}
	return nil
}

// pduString returns a printable string (NUL padding removed, invalid UTF-8 replaced).
func pduString(p gosnmp.SnmpPDU) string {
	switch v := p.Value.(type) {
	case []byte:
		return cleanString(v)
	case string:
		return cleanString([]byte(v))
	}
	if n, ok := pduInt(p); ok {
		return strconv.FormatInt(n, 10)
	}
	return ""
}

func cleanString(b []byte) string {
	b = []byte(strings.TrimRight(string(b), "\x00"))
	s := string(b)
	if !utf8.ValidString(s) {
		s = strings.ToValidUTF8(s, "?")
	}
	s = strings.Map(func(r rune) rune {
		if r < 0x20 && r != '\t' && r != '\n' && r != '\r' {
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

// pduMAC decodes a MAC from 6 raw octets or from a textual MAC.
func pduMAC(p gosnmp.SnmpPDU) string {
	return bytesMAC(pduBytes(p))
}

func bytesMAC(b []byte) string {
	if len(b) == 6 {
		if m, ok := netutil.NormalizeMAC(hex.EncodeToString(b)); ok {
			return m
		}
		return ""
	}
	if m, ok := netutil.NormalizeMAC(string(b)); ok {
		return m
	}
	return ""
}

// indexInts splits an index suffix into its numeric components.
func indexInts(idx string) ([]int, bool) {
	parts := strings.Split(idx, ".")
	out := make([]int, len(parts))
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return nil, false
		}
		out[i] = n
	}
	return out, true
}

// macFromIndex decodes 6 index components into a MAC.
func macFromIndex(parts []int) string {
	if len(parts) != 6 {
		return ""
	}
	b := make([]byte, 6)
	for i, n := range parts {
		if n > 255 {
			return ""
		}
		b[i] = byte(n)
	}
	return bytesMAC(b)
}

func ipFromBytes(parts []int) string {
	b := make([]byte, len(parts))
	for i, n := range parts {
		if n > 255 {
			return ""
		}
		b[i] = byte(n)
	}
	a, ok := netip.AddrFromSlice(b)
	if !ok {
		return ""
	}
	return a.Unmap().String()
}

// ---------------------------------------------------------------- system

// decodeSystem decodes the system group.
func decodeSystem(pdus []gosnmp.SnmpPDU) System {
	var s System
	if p, ok := scalar(pdus, oidSysDescr); ok {
		s.Descr = pduString(p)
	}
	if p, ok := scalar(pdus, oidSysObjectID); ok {
		if oid, ok := p.Value.(string); ok {
			s.ObjectID = "." + strings.TrimPrefix(oid, ".")
		}
	}
	if p, ok := scalar(pdus, oidSysUpTime); ok {
		if n, ok := pduInt(p); ok {
			s.UptimeSeconds = n / 100
		}
	}
	if p, ok := scalar(pdus, oidSysContact); ok {
		s.Contact = pduString(p)
	}
	if p, ok := scalar(pdus, oidSysName); ok {
		s.Name = pduString(p)
	}
	if p, ok := scalar(pdus, oidSysLocation); ok {
		s.Location = pduString(p)
	}
	s.Enterprise = enterpriseNumber(s.ObjectID)
	if v, ok := lookupVendor(s.Enterprise); ok {
		s.Vendor = v.Name
	}
	return s
}

// enterpriseNumber extracts <n> from ".1.3.6.1.4.1.<n>…".
func enterpriseNumber(oid string) int {
	rest, ok := strings.CutPrefix(oid, ".1.3.6.1.4.1.")
	if !ok {
		return 0
	}
	first, _, _ := strings.Cut(rest, ".")
	n, err := strconv.Atoi(first)
	if err != nil {
		return 0
	}
	return n
}

// ---------------------------------------------------------------- interfaces

var ifStatus = map[int64]string{1: "up", 2: "down", 3: "testing", 4: "unknown", 5: "dormant", 6: "notPresent", 7: "lowerLayerDown"}

// decodeInterfaces decodes ifTable/ifXTable columns into interfaces sorted by index.
func decodeInterfaces(pdus []gosnmp.SnmpPDU) []Interface {
	byIdx := map[int]*Interface{}
	get := func(idx string) *Interface {
		n, err := strconv.Atoi(idx)
		if err != nil || n <= 0 {
			return nil
		}
		if ifc, ok := byIdx[n]; ok {
			return ifc
		}
		ifc := &Interface{Index: n}
		byIdx[n] = ifc
		return ifc
	}
	for idx, p := range column(pdus, oidIfDescr) {
		if ifc := get(idx); ifc != nil {
			ifc.Descr = pduString(p)
		}
	}
	for idx, p := range column(pdus, oidIfName) {
		if ifc := get(idx); ifc != nil {
			ifc.Name = pduString(p)
		}
	}
	for idx, p := range column(pdus, oidIfAlias) {
		if ifc := get(idx); ifc != nil {
			ifc.Alias = pduString(p)
		}
	}
	for idx, p := range column(pdus, oidIfType) {
		if ifc := get(idx); ifc != nil {
			if n, ok := pduInt(p); ok {
				ifc.Type = int(n)
			}
		}
	}
	for idx, p := range column(pdus, oidIfPhysAddress) {
		if ifc := get(idx); ifc != nil {
			ifc.MAC = pduMAC(p)
		}
	}
	for idx, p := range column(pdus, oidIfSpeed) {
		if ifc := get(idx); ifc != nil {
			if n, ok := pduInt(p); ok && n > 0 && n < 4294967295 {
				ifc.SpeedMbps = n / 1_000_000
			}
		}
	}
	for idx, p := range column(pdus, oidIfHighSpeed) {
		if ifc := get(idx); ifc != nil {
			if n, ok := pduInt(p); ok && n > 0 {
				ifc.SpeedMbps = n
			}
		}
	}
	for idx, p := range column(pdus, oidIfAdminStatus) {
		if ifc := get(idx); ifc != nil {
			if n, ok := pduInt(p); ok {
				ifc.AdminStatus = ifStatus[n]
			}
		}
	}
	for idx, p := range column(pdus, oidIfOperStatus) {
		if ifc := get(idx); ifc != nil {
			if n, ok := pduInt(p); ok {
				ifc.OperStatus = ifStatus[n]
			}
		}
	}
	out := make([]Interface, 0, len(byIdx))
	for _, ifc := range byIdx {
		if ifc.Name == "" {
			ifc.Name = ifc.Descr
		}
		out = append(out, *ifc)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Index < out[j].Index })
	return out
}

// ifNames maps ifIndex to interface name.
func ifNames(ifs []Interface) map[int]string {
	m := make(map[int]string, len(ifs))
	for _, i := range ifs {
		m[i.Index] = i.Name
	}
	return m
}

// ---------------------------------------------------------------- ARP

var arpTypes = map[int64]string{1: "other", 2: "invalid", 3: "dynamic", 4: "static", 5: "local"}

// decodeARP decodes ipNetToMediaTable, or ipNetToPhysicalTable when the former is empty.
// Invalid entries and entries without a usable MAC are skipped.
func decodeARP(pdus []gosnmp.SnmpPDU, ifs []Interface) []ARPEntry {
	names := ifNames(ifs)
	var out []ARPEntry
	seen := map[string]bool{}
	add := func(ifIndex int, ip, mac, typ string) {
		if ip == "" || ip == "0.0.0.0" || ip == "::" || mac == "" || typ == "invalid" {
			return
		}
		key := fmt.Sprintf("%d|%s|%s", ifIndex, ip, mac)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, ARPEntry{IfIndex: ifIndex, IfName: names[ifIndex], IP: ip, MAC: mac, Type: typ})
	}
	types := column(pdus, oidIPNetToMediaType)
	for idx, p := range column(pdus, oidIPNetToMediaPhysAddress) {
		parts, ok := indexInts(idx)
		if !ok || len(parts) != 5 {
			continue
		}
		typ := ""
		if tp, ok := types[idx]; ok {
			if n, ok := pduInt(tp); ok {
				typ = arpTypes[n]
			}
		}
		add(parts[0], ipFromBytes(parts[1:]), pduMAC(p), typ)
	}
	if len(out) == 0 {
		ptypes := column(pdus, oidIPNetToPhysicalType)
		for idx, p := range column(pdus, oidIPNetToPhysicalPhysAddress) {
			parts, ok := indexInts(idx)
			// ifIndex.addrType.len.addr…
			if !ok || len(parts) < 4 || parts[2] != len(parts)-3 || (parts[1] != 1 && parts[1] != 2) {
				continue
			}
			typ := ""
			if tp, ok := ptypes[idx]; ok {
				if n, ok := pduInt(tp); ok {
					typ = arpTypes[n]
				}
			}
			add(parts[0], ipFromBytes(parts[3:]), pduMAC(p), typ)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		a, errA := netip.ParseAddr(out[i].IP)
		b, errB := netip.ParseAddr(out[j].IP)
		if errA == nil && errB == nil && a != b {
			return a.Less(b)
		}
		return out[i].MAC < out[j].MAC
	})
	return out
}

// ---------------------------------------------------------------- FDB

var fdbStatus = map[int64]string{1: "other", 2: "invalid", 3: "learned", 4: "self", 5: "mgmt"}

// basePorts maps bridge port numbers to ifIndex (dot1dBasePortIfIndex).
func basePorts(pdus []gosnmp.SnmpPDU) map[int]int {
	m := map[int]int{}
	for idx, p := range column(pdus, oidDot1dBasePortIfIndex) {
		port, err := strconv.Atoi(idx)
		if n, ok := pduInt(p); err == nil && ok && n > 0 {
			m[port] = int(n)
		}
	}
	return m
}

// decodeFDB decodes the Q-BRIDGE-MIB FDB (VLAN aware) or, when it is empty, the
// BRIDGE-MIB FDB. Bridge ports are mapped to interfaces via dot1dBasePortIfIndex; when
// that table is missing, bridge port = ifIndex is assumed. MikroTik RouterOS reports
// ifIndexes instead of bridge ports in dot1dTpFdbPort. Q-BRIDGE filtering database ids
// are mapped to VLANs via dot1qVlanFdbId (fdbId = VLAN when the mapping is missing).
// Entries with bridge port 0 (CPU) or status invalid are skipped.
func decodeFDB(pdus []gosnmp.SnmpPDU, ifs []Interface) []FDBEntry {
	names := ifNames(ifs)
	portIf := basePorts(pdus)
	portIsIfIndex := decodeSystem(pdus).Enterprise == 14988
	// fdbId -> VLAN; with shared VLAN learning several VLANs use one filtering database:
	// then the VLAN equal to the fdbId, otherwise the lowest VLAN is reported
	fdbVLAN := map[int]int{}
	for idx, p := range column(pdus, oidDot1qVlanFdbID) {
		parts, ok := indexInts(idx)
		n, okN := pduInt(p)
		if !ok || !okN || len(parts) != 2 || parts[1] <= 0 {
			continue
		}
		fdb, vlan := int(n), parts[1]
		if cur, dup := fdbVLAN[fdb]; !dup || (cur != fdb && (vlan == fdb || vlan < cur)) {
			fdbVLAN[fdb] = vlan
		}
	}
	var out []FDBEntry
	seen := map[string]bool{}
	add := func(mac string, vlan, port int, status string) {
		if mac == "" || port <= 0 || status == "invalid" {
			return
		}
		key := fmt.Sprintf("%s|%d", mac, vlan)
		if seen[key] {
			return
		}
		seen[key] = true
		e := FDBEntry{MAC: mac, VLAN: vlan, BridgePort: port, Status: status}
		if ifIndex, ok := portIf[port]; ok && !portIsIfIndex {
			e.IfIndex, e.IfName = ifIndex, names[ifIndex]
		} else if _, known := names[port]; known && (portIsIfIndex || len(portIf) == 0) {
			e.IfIndex, e.IfName = port, names[port]
		}
		out = append(out, e)
	}
	statusOf := func(col map[string]gosnmp.SnmpPDU, idx string) string {
		if p, ok := col[idx]; ok {
			if n, ok := pduInt(p); ok {
				return fdbStatus[n]
			}
		}
		return ""
	}
	qStatus := column(pdus, oidDot1qTpFdbStatus)
	for idx, p := range column(pdus, oidDot1qTpFdbPort) {
		parts, ok := indexInts(idx)
		port, okP := pduInt(p)
		if ok && len(parts) == 8 && parts[1] == 6 {
			parts = append(parts[:1], parts[2:]...) // length-prefixed MAC (AOS-CX)
		}
		if !ok || !okP || len(parts) != 7 {
			continue
		}
		vlan := parts[0]
		if v, ok := fdbVLAN[parts[0]]; ok {
			vlan = v
		}
		add(macFromIndex(parts[1:]), vlan, int(port), statusOf(qStatus, idx))
	}
	if len(out) == 0 {
		dStatus := column(pdus, oidDot1dTpFdbStatus)
		for idx, p := range column(pdus, oidDot1dTpFdbPort) {
			parts, ok := indexInts(idx)
			port, okP := pduInt(p)
			if ok && len(parts) == 7 && parts[0] == 6 {
				parts = parts[1:] // length-prefixed MAC (AOS-CX)
			}
			if !ok || !okP || len(parts) != 6 {
				continue
			}
			add(macFromIndex(parts), 0, int(port), statusOf(dStatus, idx))
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BridgePort != out[j].BridgePort {
			return out[i].BridgePort < out[j].BridgePort
		}
		if out[i].VLAN != out[j].VLAN {
			return out[i].VLAN < out[j].VLAN
		}
		return out[i].MAC < out[j].MAC
	})
	return out
}

// ---------------------------------------------------------------- LLDP

var chassisSubtypes = map[int64]string{1: "chassisComponent", 2: "interfaceAlias", 3: "portComponent", 4: "macAddress",
	5: "networkAddress", 6: "interfaceName", 7: "local"}
var portSubtypes = map[int64]string{1: "interfaceAlias", 2: "portComponent", 3: "macAddress", 4: "networkAddress",
	5: "interfaceName", 6: "agentCircuitId", 7: "local"}
var capabilityNames = []string{"other", "repeater", "bridge", "wlanAccessPoint", "router", "telephone", "docsisCableDevice", "stationOnly"}

// lldpID formats a chassis/port id according to its subtype and returns the MAC if the
// id is one.
func lldpID(subtype string, raw []byte) (id, mac string) {
	switch subtype {
	case "macAddress":
		if m := bytesMAC(raw); m != "" {
			return m, m
		}
	case "networkAddress":
		// first octet: IANA address family (1 = IPv4, 2 = IPv6)
		if len(raw) == 5 && raw[0] == 1 || len(raw) == 17 && raw[0] == 2 {
			if a, ok := netip.AddrFromSlice(raw[1:]); ok {
				return a.Unmap().String(), ""
			}
		}
		if len(raw) == 4 || len(raw) == 16 {
			if a, ok := netip.AddrFromSlice(raw); ok {
				return a.Unmap().String(), ""
			}
		}
	}
	s := cleanString(raw)
	if !isPrintable(raw) {
		s = hex.EncodeToString(raw)
	}
	// some agents send a MAC as text or as 6 raw octets with another subtype
	if m, ok := netutil.NormalizeMAC(s); ok && len(s) >= 12 {
		return m, m
	}
	if len(raw) == 6 && !isPrintable(raw) {
		if m := bytesMAC(raw); m != "" {
			return m, m
		}
	}
	return s, ""
}

func isPrintable(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if r < 0x20 && r != '\t' {
			return false
		}
	}
	return true
}

// decodeCapabilities decodes the LldpSystemCapabilitiesMap BITS value (bit 0 = MSB).
func decodeCapabilities(b []byte) []string {
	if len(b) > 2 {
		return nil // the map has 8 defined bits; longer values are malformed
	}
	var out []string
	for i, name := range capabilityNames {
		byteIdx, bit := i/8, 7-uint(i%8)
		if byteIdx < len(b) && b[byteIdx]&(1<<bit) != 0 {
			out = append(out, name)
		}
	}
	return out
}

// decodeLLDPLocal decodes lldpLocalSystemData.
func decodeLLDPLocal(pdus []gosnmp.SnmpPDU) *LLDPLocal {
	l := &LLDPLocal{}
	if p, ok := scalar(pdus, oidLldpLocChassisIDSub); ok {
		if n, ok := pduInt(p); ok {
			l.ChassisIDSubtype = chassisSubtypes[n]
		}
	}
	if p, ok := scalar(pdus, oidLldpLocChassisID); ok {
		l.ChassisID, l.ChassisMAC = lldpID(l.ChassisIDSubtype, pduBytes(p))
	}
	if p, ok := scalar(pdus, oidLldpLocSysName); ok {
		l.SysName = pduString(p)
	}
	if l.ChassisID == "" && l.SysName == "" {
		return nil
	}
	return l
}

// decodeLLDP decodes lldpRemTable, lldpRemManAddrTable and lldpLocPortTable. Local
// ports are resolved to interfaces by name (ifName/ifDescr/ifAlias), MAC or – as a last
// resort – by lldpLocPortNum = ifIndex.
func decodeLLDP(pdus []gosnmp.SnmpPDU, ifs []Interface) []LLDPNeighbor {
	// local ports
	type locPort struct{ subtype, id, desc string }
	loc := map[int]*locPort{}
	getLoc := func(idx string) *locPort {
		n, err := strconv.Atoi(idx)
		if err != nil {
			return nil
		}
		if l, ok := loc[n]; ok {
			return l
		}
		l := &locPort{}
		loc[n] = l
		return l
	}
	for idx, p := range column(pdus, oidLldpLocPortIDSub) {
		if l := getLoc(idx); l != nil {
			if n, ok := pduInt(p); ok {
				l.subtype = portSubtypes[n]
			}
		}
	}
	for idx, p := range column(pdus, oidLldpLocPortID) {
		if l := getLoc(idx); l != nil {
			raw := pduBytes(p)
			l.id = cleanString(raw)
			if l.subtype == "macAddress" {
				l.id = bytesMAC(raw)
			}
		}
	}
	for idx, p := range column(pdus, oidLldpLocPortDesc) {
		if l := getLoc(idx); l != nil {
			l.desc = pduString(p)
		}
	}
	byName, byMAC, byIndex := map[string]Interface{}, map[string]Interface{}, map[int]Interface{}
	for _, i := range ifs {
		byIndex[i.Index] = i
		for _, n := range []string{i.Name, i.Descr, i.Alias} {
			if n != "" {
				if _, dup := byName[strings.ToLower(n)]; !dup {
					byName[strings.ToLower(n)] = i
				}
			}
		}
		if i.MAC != "" {
			if _, dup := byMAC[i.MAC]; !dup {
				byMAC[i.MAC] = i
			}
		}
	}
	// IEEE 802.1AB: lldpLocPortNum should equal the bridge port number (dot1dBasePort)
	bridgePorts := basePorts(pdus)
	resolveLocal := func(portNum int) (string, int) {
		l := loc[portNum]
		if l != nil {
			if l.subtype == "macAddress" {
				if i, ok := byMAC[l.id]; ok {
					return i.Name, i.Index
				}
			}
			for _, n := range []string{l.id, l.desc} {
				if i, ok := byName[strings.ToLower(n)]; ok && n != "" {
					return i.Name, i.Index
				}
			}
		}
		if ifIndex, ok := bridgePorts[portNum]; ok {
			if i, ok := byIndex[ifIndex]; ok {
				return i.Name, i.Index
			}
		}
		if i, ok := byIndex[portNum]; ok && (l == nil || l.id == "" || l.id == strconv.Itoa(portNum)) {
			return i.Name, i.Index
		}
		if l != nil {
			if l.desc != "" {
				return l.desc, 0
			}
			return l.id, 0
		}
		return "", 0
	}

	// remote systems, keyed by "localPortNum.remIndex" (the time mark changes). RouterOS
	// uses a single neighbour number instead; its local interface comes from
	// mtxrNeighborInterfaceID.
	nb := map[string]*LLDPNeighbor{}
	mikrotik := map[string]int{}
	get := func(idx string) *LLDPNeighbor {
		parts, ok := indexInts(idx)
		if !ok || (len(parts) != 3 && len(parts) != 1) {
			return nil
		}
		key, port := "", 0
		if len(parts) == 3 {
			key, port = fmt.Sprintf("%d.%d", parts[1], parts[2]), parts[1]
		} else {
			key = fmt.Sprintf("mt.%d", parts[0])
			mikrotik[key] = parts[0]
		}
		if n, ok := nb[key]; ok {
			return n
		}
		n := &LLDPNeighbor{LocalPortNum: port}
		nb[key] = n
		return n
	}
	for idx, p := range column(pdus, oidLldpRemChassisIDSub) {
		if n := get(idx); n != nil {
			if v, ok := pduInt(p); ok {
				n.ChassisIDSubtype = chassisSubtypes[v]
			}
		}
	}
	for idx, p := range column(pdus, oidLldpRemPortIDSub) {
		if n := get(idx); n != nil {
			if v, ok := pduInt(p); ok {
				n.PortIDSubtype = portSubtypes[v]
			}
		}
	}
	for idx, p := range column(pdus, oidLldpRemChassisID) {
		if n := get(idx); n != nil {
			n.ChassisID, n.ChassisMAC = lldpID(n.ChassisIDSubtype, pduBytes(p))
		}
	}
	for idx, p := range column(pdus, oidLldpRemPortID) {
		if n := get(idx); n != nil {
			n.PortID, n.PortMAC = lldpID(n.PortIDSubtype, pduBytes(p))
		}
	}
	for idx, p := range column(pdus, oidLldpRemPortDesc) {
		if n := get(idx); n != nil {
			n.PortDesc = pduString(p)
		}
	}
	for idx, p := range column(pdus, oidLldpRemSysName) {
		if n := get(idx); n != nil {
			n.SysName = pduString(p)
		}
	}
	for idx, p := range column(pdus, oidLldpRemSysDesc) {
		if n := get(idx); n != nil {
			n.SysDesc = pduString(p)
		}
	}
	for idx, p := range column(pdus, oidLldpRemSysCapEnable) {
		if n := get(idx); n != nil {
			if p.Type == gosnmp.Integer {
				// RouterOS reports the map as an integer with bit i = capability i
				if v, ok := pduInt(p); ok {
					for i, name := range capabilityNames {
						if v&(1<<uint(i)) != 0 {
							n.Capabilities = append(n.Capabilities, name)
						}
					}
				}
				continue
			}
			n.Capabilities = decodeCapabilities(pduBytes(p))
		}
	}
	// management addresses are encoded in the index of every lldpRemManAddrTable column
	prefix := oidLldpRemManAddrTable + ".1."
	for _, p := range pdus {
		name := p.Name
		if !strings.HasPrefix(name, ".") {
			name = "." + name
		}
		rest, ok := strings.CutPrefix(name, prefix)
		if !ok {
			continue
		}
		parts, ok := indexInts(rest)
		// column.timeMark.localPort.remIndex.addrSubtype.len.addr…
		if !ok || len(parts) < 7 || parts[5] != len(parts)-6 {
			continue
		}
		n := nb[fmt.Sprintf("%d.%d", parts[2], parts[3])]
		if n == nil {
			continue
		}
		var addr string
		if (parts[4] == 1 && parts[5] == 4) || (parts[4] == 2 && parts[5] == 16) {
			addr = ipFromBytes(parts[6:])
		}
		if addr == "" || addr == "0.0.0.0" || addr == "::" {
			continue
		}
		if !contains(n.ManagementAddresses, addr) {
			n.ManagementAddresses = append(n.ManagementAddresses, addr)
		}
	}
	mtIf := column(pdus, oidMtxrNeighborInterfaceID)
	out := make([]LLDPNeighbor, 0, len(nb))
	for key, n := range nb {
		if n.ChassisID == "" && n.PortID == "" && n.SysName == "" {
			continue
		}
		if num, ok := mikrotik[key]; ok {
			if p, ok := mtIf[strconv.Itoa(num)]; ok {
				if v, ok := pduInt(p); ok && v > 0 {
					n.LocalIfIndex = int(v)
					if i, ok := byIndex[int(v)]; ok {
						n.LocalPort = i.Name
					}
				}
			}
		} else {
			n.LocalPort, n.LocalIfIndex = resolveLocal(n.LocalPortNum)
		}
		out = append(out, *n)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].LocalPortNum != out[j].LocalPortNum {
			return out[i].LocalPortNum < out[j].LocalPortNum
		}
		if out[i].LocalIfIndex != out[j].LocalIfIndex {
			return out[i].LocalIfIndex < out[j].LocalIfIndex
		}
		if out[i].ChassisID != out[j].ChassisID {
			return out[i].ChassisID < out[j].ChassisID
		}
		return out[i].PortID < out[j].PortID
	})
	return out
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
