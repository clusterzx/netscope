package fortigate

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"Fortinet FortiGate": "Fortinet FortiGate",
		"Liest DHCP-Leases, die ARP-Tabelle und die von der FortiGate erkannten Geräte (Name, Betriebssystem, FortiSwitch-Port, FortiAP) über die FortiOS-REST-API.": "Reads DHCP leases, the ARP table and the devices detected by the FortiGate (name, operating system, FortiSwitch port, FortiAP) via the FortiOS REST API.",
		"FortiGates": "FortiGates",
		"Hostname, IP oder URL der Verwaltungsoberfläche (z. B. https://fw.lan:8443), eine pro Zeile.":                                                                                                                                                                                                   "Host name, IP or URL of the management interface (e.g. https://fw.lan:8443), one per line.",
		"API-Token eines REST-API-Administrators (System → Administratoren → REST-API-Admin; Profil z. B. super_admin_readonly, vertrauenswürdige Hosts: die Adresse von NetScope).":                                                                                                                     "API token of a REST API administrator (System → Administrators → REST API Admin; profile e.g. super_admin_readonly, trusted hosts: the address of NetScope).",
		"API-Token eines REST-API-Administrators (System → Administratoren → REST-API-Admin; Profil z. B. super_admin_readonly, vertrauenswürdige Hosts: die Adresse von NetScope). Leer = automatisch die passenden je Gerät nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen.": "API token of a REST API administrator (System → Administrators → REST API Admin; profile e.g. super_admin_readonly, trusted hosts: the address of NetScope). Empty = automatically the matching ones per device according to the credential's scope; rejected ones are skipped.",
		"VDOMs": "VDOMs",
		"Nur bei mehreren VDOMs: deren Namen. Leer = die VDOM des Tokens.": "Only with multiple VDOMs: their names. Empty = the token's VDOM.",
		"Erkannte Geräte": "Detected devices",
		"Geräte aus der Geräteerkennung der FortiGate (Name, Betriebssystem, FortiSwitch-Port, FortiAP); braucht „Device detection“ auf den Schnittstellen.": "Devices from the FortiGate's device detection (name, operating system, FortiSwitch port, FortiAP); requires “Device detection” on the interfaces.",

		// run log
		"ARP-Tabelle nicht lesbar (Recht sysgrp/netgrp lesen?)":       "Cannot read ARP table (read permission for sysgrp/netgrp?)",
		"Erkannte Geräte nicht lesbar (Recht „User & Device“ lesen?)": "Cannot read detected devices (read permission for “User & Device”?)",
	})
}
