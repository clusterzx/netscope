package mikrotik

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"MikroTik RouterOS": "MikroTik RouterOS",
		"Liest DHCP-Leases, die ARP-Tabelle und die Bridge-Hosttabelle (welches Gerät an welchem Port) von MikroTik-Routern und -Switches über die REST-API von RouterOS 7.": "Reads DHCP leases, the ARP table and the bridge host table (which device is on which port) from MikroTik routers and switches via the RouterOS 7 REST API.",
		"Router und Switches": "Routers and switches",
		"Hostname, IP oder URL (https://router oder http://router:8080), eins pro Zeile. Der Dienst www-ssl muss aktiv sein.":                                                                                            "Host name, IP or URL (https://router or http://router:8080), one per line. The www-ssl service must be enabled.",
		"Benutzer/Passwort eines RouterOS-Benutzers einer Gruppe mit den Rechten read und rest-api.":                                                                                                                     "Username/password of a RouterOS user in a group with the read and rest-api policies.",
		"Benutzer/Passwort eines RouterOS-Benutzers einer Gruppe mit den Rechten read und rest-api. Leer = automatisch die passenden je Gerät nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen.": "Username/password of a RouterOS user in a group with the read and rest-api policies. Empty = automatically the matching ones per device according to the credential's scope; rejected ones are skipped.",
		"Bridge-Ports übernehmen": "Import bridge ports",
		"Aus der Bridge-Hosttabelle: an welchem Port ein Gerät hängt (für die Topologie).": "From the bridge host table: which port a device is connected to (for the topology).",

		// run log
		"Bridge-Hosttabelle nicht lesbar": "Cannot read bridge host table",
	})
}
