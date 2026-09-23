package snmp

// vendor is an entry of the IANA private enterprise number registry
// (https://www.iana.org/assignments/enterprise-numbers) as found in sysObjectID.
type vendor struct {
	Name string
	// Agent marks enterprise numbers of SNMP agent software (net-snmp, Windows SNMP
	// service …): they say nothing about the hardware manufacturer and are therefore not
	// reported as the device vendor (that would override the OUI vendor).
	Agent bool
}

var vendors = map[int]vendor{
	2:     {Name: "IBM"},
	9:     {Name: "Cisco"},
	11:    {Name: "HPE"},
	43:    {Name: "3Com"},
	171:   {Name: "D-Link"},
	207:   {Name: "Allied Telesis"},
	232:   {Name: "HPE"}, // Compaq (iLO, ProLiant)
	311:   {Name: "Microsoft", Agent: true},
	318:   {Name: "APC"},
	534:   {Name: "Eaton"},
	674:   {Name: "Dell"},
	789:   {Name: "NetApp"},
	890:   {Name: "Zyxel"},
	1588:  {Name: "Brocade"},
	1916:  {Name: "Extreme Networks"},
	1991:  {Name: "Brocade"}, // Foundry Networks
	2011:  {Name: "Huawei"},
	2021:  {Name: "net-snmp", Agent: true}, // UC Davis (ucd-snmp)
	2604:  {Name: "Sophos"},
	2620:  {Name: "Check Point"},
	2636:  {Name: "Juniper"},
	3375:  {Name: "F5"},
	3808:  {Name: "CyberPower"},
	3955:  {Name: "Linksys"},
	4413:  {Name: "Broadcom"},
	4526:  {Name: "NETGEAR"},
	5624:  {Name: "Enterasys"},
	6027:  {Name: "Dell"}, // Force10
	6486:  {Name: "Alcatel-Lucent"},
	6574:  {Name: "Synology"},
	6876:  {Name: "VMware"},
	8072:  {Name: "net-snmp", Agent: true},
	8691:  {Name: "Moxa"},
	8741:  {Name: "SonicWall"},
	10876: {Name: "Supermicro"},
	11863: {Name: "TP-Link"},
	12356: {Name: "Fortinet"},
	14823: {Name: "Aruba"},
	14988: {Name: "MikroTik"},
	17713: {Name: "Cambium Networks"},
	22610: {Name: "A10 Networks"},
	24681: {Name: "QNAP"},
	25053: {Name: "Ruckus"},
	25461: {Name: "Palo Alto Networks"},
	25506: {Name: "H3C"},
	26928: {Name: "Aerohive"},
	30065: {Name: "Arista"},
	41112: {Name: "Ubiquiti"},
	47196: {Name: "Aruba"}, // Aruba, a Hewlett Packard Enterprise company (AOS-CX)
}

func lookupVendor(enterprise int) (vendor, bool) {
	v, ok := vendors[enterprise]
	return v, ok
}
