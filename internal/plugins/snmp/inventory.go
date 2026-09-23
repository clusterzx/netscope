package snmp

// Inventory is the structured SNMP data of one device. The scanner stores it in
// device_inventory (source "snmp"); the topology processor reads exactly this JSON to
// derive switch-port, LLDP and layer-3 edges. All MAC addresses are normalised to
// lower-case colon notation (aa:bb:cc:dd:ee:ff), as in device_macs.
type Inventory struct {
	System     System      `json:"system"`
	Interfaces []Interface `json:"interfaces,omitempty"`
	// ARP is the IP-to-MAC table of the device (IP-MIB ipNetToMediaTable, or
	// ipNetToPhysicalTable when the former is empty).
	ARP []ARPEntry `json:"arp,omitempty"`
	// FDB is the bridge forwarding table (Q-BRIDGE-MIB dot1qTpFdbTable when available,
	// otherwise BRIDGE-MIB dot1dTpFdbTable).
	FDB []FDBEntry `json:"fdb,omitempty"`
	// LLDPLocal identifies the device itself in LLDP (what its neighbours report).
	LLDPLocal *LLDPLocal `json:"lldpLocal,omitempty"`
	// LLDP lists the LLDP neighbours (LLDP-MIB lldpRemTable).
	LLDP []LLDPNeighbor `json:"lldp,omitempty"`
	// Walked lists the tables that were queried successfully ("interfaces", "arp",
	// "fdb", "lldp"); an empty table that was walked means "none", a missing entry means
	// "unknown".
	Walked []string `json:"walked,omitempty"`
	// Errors holds failed queries per table.
	Errors map[string]string `json:"errors,omitempty"`
}

// System is the SNMPv2-MIB system group.
type System struct {
	Descr         string `json:"descr,omitempty"`
	ObjectID      string `json:"objectId,omitempty"`   // sysObjectID, e.g. ".1.3.6.1.4.1.14988.1"
	Enterprise    int    `json:"enterprise,omitempty"` // private enterprise number from sysObjectID
	Vendor        string `json:"vendor,omitempty"`     // name of the enterprise (see vendors.go)
	UptimeSeconds int64  `json:"uptimeSeconds,omitempty"`
	Contact       string `json:"contact,omitempty"`
	Name          string `json:"name,omitempty"`
	Location      string `json:"location,omitempty"`
	Version       string `json:"version,omitempty"` // SNMP version that answered: 2c | 3
}

// Interface is a row of IF-MIB ifTable/ifXTable.
type Interface struct {
	Index       int    `json:"index"`
	Name        string `json:"name,omitempty"` // ifName, falls back to ifDescr
	Descr       string `json:"descr,omitempty"`
	Alias       string `json:"alias,omitempty"`
	Type        int    `json:"type,omitempty"` // IANAifType, e.g. 6 = ethernetCsmacd, 161 = ieee8023adLag
	MAC         string `json:"mac,omitempty"`
	SpeedMbps   int64  `json:"speedMbps,omitempty"`
	AdminStatus string `json:"adminStatus,omitempty"` // up | down | testing
	OperStatus  string `json:"operStatus,omitempty"`  // up | down | testing | unknown | dormant | notPresent | lowerLayerDown
}

// ARPEntry is an IP-to-MAC mapping learned by the device.
type ARPEntry struct {
	IfIndex int    `json:"ifIndex,omitempty"`
	IfName  string `json:"ifName,omitempty"`
	IP      string `json:"ip"`
	MAC     string `json:"mac"`
	Type    string `json:"type,omitempty"` // other | invalid | dynamic | static | local
}

// FDBEntry is a MAC address learned on a bridge port.
type FDBEntry struct {
	MAC        string `json:"mac"`
	VLAN       int    `json:"vlan,omitempty"` // 0 = unknown (BRIDGE-MIB without Q-BRIDGE)
	BridgePort int    `json:"bridgePort"`
	IfIndex    int    `json:"ifIndex,omitempty"` // via dot1dBasePortIfIndex
	IfName     string `json:"ifName,omitempty"`
	Status     string `json:"status,omitempty"` // other | invalid | learned | self | mgmt
}

// LLDPLocal is the device's own LLDP identity (lldpLocalSystemData).
type LLDPLocal struct {
	ChassisIDSubtype string `json:"chassisIdSubtype,omitempty"`
	ChassisID        string `json:"chassisId,omitempty"`
	ChassisMAC       string `json:"chassisMac,omitempty"`
	SysName          string `json:"sysName,omitempty"`
}

// LLDPNeighbor is a remote system seen on a local port.
type LLDPNeighbor struct {
	LocalPortNum int    `json:"localPortNum"`
	LocalPort    string `json:"localPort,omitempty"`    // local port name (ifName, lldpLocPortId/Desc)
	LocalIfIndex int    `json:"localIfIndex,omitempty"` // resolved local interface (0 = unknown)
	// ChassisIDSubtype: chassisComponent | interfaceAlias | portComponent | macAddress |
	// networkAddress | interfaceName | local
	ChassisIDSubtype string `json:"chassisIdSubtype,omitempty"`
	ChassisID        string `json:"chassisId,omitempty"`  // MACs as aa:bb:…, network addresses as IP, else text
	ChassisMAC       string `json:"chassisMac,omitempty"` // set when the chassis id is a MAC
	// PortIDSubtype: interfaceAlias | portComponent | macAddress | networkAddress |
	// interfaceName | agentCircuitId | local
	PortIDSubtype string `json:"portIdSubtype,omitempty"`
	PortID        string `json:"portId,omitempty"`
	PortMAC       string `json:"portMac,omitempty"` // set when the port id is a MAC
	PortDesc      string `json:"portDesc,omitempty"`
	SysName       string `json:"sysName,omitempty"`
	SysDesc       string `json:"sysDesc,omitempty"`
	// Capabilities enabled on the remote system: other, repeater, bridge,
	// wlanAccessPoint, router, telephone, docsisCableDevice, stationOnly.
	Capabilities []string `json:"capabilities,omitempty"`
	// ManagementAddresses from lldpRemManAddrTable.
	ManagementAddresses []string `json:"managementAddresses,omitempty"`
}

// RemotePort returns the most descriptive name of the neighbour's port.
func (n LLDPNeighbor) RemotePort() string {
	switch n.PortIDSubtype {
	case "interfaceName", "interfaceAlias", "local":
		if n.PortID != "" {
			return n.PortID
		}
	}
	if n.PortDesc != "" {
		return n.PortDesc
	}
	return n.PortID
}

// Table names used in Walked and Errors.
const (
	TableSystem     = "system"
	TableInterfaces = "interfaces"
	TableARP        = "arp"
	TableFDB        = "fdb"
	TableLLDP       = "lldp"
)

// HasWalked reports whether a table was queried successfully.
func (inv *Inventory) HasWalked(table string) bool {
	for _, t := range inv.Walked {
		if t == table {
			return true
		}
	}
	return false
}
