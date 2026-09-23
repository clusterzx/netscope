package snmp

// OIDs (numeric, with leading dot as returned by gosnmp).
const (
	// SNMPv2-MIB system group
	oidSysDescr    = ".1.3.6.1.2.1.1.1.0"
	oidSysObjectID = ".1.3.6.1.2.1.1.2.0"
	oidSysUpTime   = ".1.3.6.1.2.1.1.3.0"
	oidSysContact  = ".1.3.6.1.2.1.1.4.0"
	oidSysName     = ".1.3.6.1.2.1.1.5.0"
	oidSysLocation = ".1.3.6.1.2.1.1.6.0"

	// IF-MIB ifTable
	oidIfDescr       = ".1.3.6.1.2.1.2.2.1.2"
	oidIfType        = ".1.3.6.1.2.1.2.2.1.3"
	oidIfSpeed       = ".1.3.6.1.2.1.2.2.1.5"
	oidIfPhysAddress = ".1.3.6.1.2.1.2.2.1.6"
	oidIfAdminStatus = ".1.3.6.1.2.1.2.2.1.7"
	oidIfOperStatus  = ".1.3.6.1.2.1.2.2.1.8"
	// IF-MIB ifXTable
	oidIfName      = ".1.3.6.1.2.1.31.1.1.1.1"
	oidIfHighSpeed = ".1.3.6.1.2.1.31.1.1.1.15"
	oidIfAlias     = ".1.3.6.1.2.1.31.1.1.1.18"

	// IP-MIB ipNetToMediaTable (index ifIndex.a.b.c.d)
	oidIPNetToMediaPhysAddress = ".1.3.6.1.2.1.4.22.1.2"
	oidIPNetToMediaType        = ".1.3.6.1.2.1.4.22.1.4"
	// IP-MIB ipNetToPhysicalTable (index ifIndex.addrType.len.addr…)
	oidIPNetToPhysicalPhysAddress = ".1.3.6.1.2.1.4.35.1.4"
	oidIPNetToPhysicalType        = ".1.3.6.1.2.1.4.35.1.6"

	// BRIDGE-MIB
	oidDot1dBasePortIfIndex = ".1.3.6.1.2.1.17.1.4.1.2"
	oidDot1dTpFdbPort       = ".1.3.6.1.2.1.17.4.3.1.2" // index = MAC (6 octets)
	oidDot1dTpFdbStatus     = ".1.3.6.1.2.1.17.4.3.1.3"
	// Q-BRIDGE-MIB
	oidDot1qTpFdbPort   = ".1.3.6.1.2.1.17.7.1.2.2.1.2" // index = fdbId.MAC
	oidDot1qTpFdbStatus = ".1.3.6.1.2.1.17.7.1.2.2.1.3"
	oidDot1qVlanFdbID   = ".1.3.6.1.2.1.17.7.1.4.2.1.3" // index = timeMark.vlanIndex, value = fdbId

	// LLDP-MIB (IEEE 802.1AB-2005)
	oidLldpLocalSystemData = ".1.0.8802.1.1.2.1.3"
	oidLldpLocChassisIDSub = ".1.0.8802.1.1.2.1.3.1.0"
	oidLldpLocChassisID    = ".1.0.8802.1.1.2.1.3.2.0"
	oidLldpLocSysName      = ".1.0.8802.1.1.2.1.3.3.0"
	oidLldpLocPortIDSub    = ".1.0.8802.1.1.2.1.3.7.1.2" // index = lldpLocPortNum
	oidLldpLocPortID       = ".1.0.8802.1.1.2.1.3.7.1.3"
	oidLldpLocPortDesc     = ".1.0.8802.1.1.2.1.3.7.1.4"
	oidLldpRemTable        = ".1.0.8802.1.1.2.1.4.1"
	oidLldpRemChassisIDSub = ".1.0.8802.1.1.2.1.4.1.1.4" // index = timeMark.localPortNum.remIndex
	oidLldpRemChassisID    = ".1.0.8802.1.1.2.1.4.1.1.5"
	oidLldpRemPortIDSub    = ".1.0.8802.1.1.2.1.4.1.1.6"
	oidLldpRemPortID       = ".1.0.8802.1.1.2.1.4.1.1.7"
	oidLldpRemPortDesc     = ".1.0.8802.1.1.2.1.4.1.1.8"
	oidLldpRemSysName      = ".1.0.8802.1.1.2.1.4.1.1.9"
	oidLldpRemSysDesc      = ".1.0.8802.1.1.2.1.4.1.1.10"
	oidLldpRemSysCapEnable = ".1.0.8802.1.1.2.1.4.1.1.12"
	oidLldpRemManAddrTable = ".1.0.8802.1.1.2.1.4.2"
	oidLldpRemManAddrIfSub = ".1.0.8802.1.1.2.1.4.2.1.3" // index = timeMark.localPortNum.remIndex.addrSubtype.len.addr…

	// MIKROTIK-MIB mtxrNeighborInterfaceID: RouterOS indexes lldpRemTable by a plain neighbour
	// number; this column maps it to the local ifIndex.
	oidMtxrNeighborInterfaceID = ".1.3.6.1.4.1.14988.1.1.11.1.1.8"
)

// systemOIDs are fetched with one GET.
var systemOIDs = []string{oidSysDescr, oidSysObjectID, oidSysUpTime, oidSysContact, oidSysName, oidSysLocation}
