package mdns

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"mDNS / Bonjour": "mDNS / Bonjour",
		"Findet Geräte und Dienste per Multicast-DNS (Bonjour/Avahi): Hostnamen, Dienste, Modell-Hinweise und Gerätetyp.": "Finds devices and services via multicast DNS (Bonjour/Avahi): host names, services, model hints and device type.",
		"Lauschdauer": "Listen duration",
		"Wie lange pro Netz auf Antworten gewartet wird. Nach 40 % der Zeit werden die gefundenen Dienste im Detail abgefragt.": "How long to wait for responses per network. After 40% of the time, the services found are queried in detail.",

		// run log, run errors
		"kein lokales Interface für %s – mDNS erreicht nur direkt angeschlossene Netze":      "no local interface for %s – mDNS only reaches directly attached networks",
		"Subnetz %s: kein lokales Interface – mDNS erreicht nur direkt angeschlossene Netze": "Subnet %s: no local interface – mDNS only reaches directly attached networks",
		"%d Geräte über %s": "%d devices via %s",
		"%s über %s":        "%s via %s",
		"Keine Subnetze im Scope – nutze die lokal angeschlossenen Netze": "No subnets in scope – using the locally attached networks",
		"anzahl": "count",
		"ziel":   "target",
		"Nur geroutete Ziele – mDNS erreicht Geräte hinter Routern und Tunneln nicht": "Only routed targets – mDNS does not reach devices behind routers and tunnels",
		"Nichts zu tun: keine Netze oder Geräte im Scope":                             "Nothing to do: no networks or devices in scope",
		"mDNS-Abfrage fehlgeschlagen":                                                 "mDNS query failed",
		"mDNS-Abfrage abgeschlossen":                                                  "mDNS query finished",
		"Multicast-Interface nicht gesetzt":                                           "Multicast interface not set",
		"Empfang auf 224.0.0.251:5353 nicht möglich, nur Unicast-Antworten":           "Cannot receive on 224.0.0.251:5353, unicast responses only",
		"mDNS-Anfrage nicht gesendet":                                                 "mDNS request not sent",
		"Ergebnis konnte nicht gespeichert werden":                                    "Could not save result",
	})
}
