package upnp

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"UPnP / SSDP": "UPnP / SSDP",
		"Findet UPnP-Geräte per SSDP und liest ihre Gerätebeschreibung: Name, Hersteller, Modell und Gerätetyp.": "Finds UPnP devices via SSDP and reads their device description: name, vendor, model and device type.",
		"Lauschdauer": "Listen duration",
		"Wie lange pro Netz auf SSDP-Antworten gewartet wird (Geräte antworten innerhalb von 2 Sekunden).": "How long to wait for SSDP responses per network (devices respond within 2 seconds).",

		// run log, run errors
		"kein lokales Interface für %s – SSDP erreicht nur direkt angeschlossene Netze":      "no local interface for %s – SSDP only reaches directly attached networks",
		"Subnetz %s: kein lokales Interface – SSDP erreicht nur direkt angeschlossene Netze": "Subnet %s: no local interface – SSDP only reaches directly attached networks",
		"%d Geräte über %s": "%d devices via %s",
		"%s über %s":        "%s via %s",
		"Keine Subnetze im Scope – nutze die lokal angeschlossenen Netze": "No subnets in scope – using the locally attached networks",
		"anzahl": "count",
		"ziel":   "target",
		"Nur geroutete Ziele – SSDP erreicht Geräte hinter Routern und Tunneln nicht": "Only routed targets – SSDP does not reach devices behind routers and tunnels",
		"Nichts zu tun: keine Netze oder Geräte im Scope":                             "Nothing to do: no networks or devices in scope",
		"SSDP-Suche fehlgeschlagen":                                                   "SSDP search failed",
		"SSDP-Suche abgeschlossen":                                                    "SSDP search finished",
		"Multicast-Interface nicht gesetzt":                                           "Multicast interface not set",
		"M-SEARCH nicht gesendet":                                                     "M-SEARCH not sent",
		"Ergebnis konnte nicht gespeichert werden":                                    "Could not save result",
		"Gerätebeschreibung nicht lesbar":                                             "Cannot read device description",

		// device description
		"ungültige Gerätebeschreibung: %w":                  "invalid device description: %w",
		"Gerätebeschreibung ohne <device>":                  "Device description without <device>",
		"LOCATION verweist nicht auf das antwortende Gerät": "LOCATION does not point to the responding device",
		"ungültige LOCATION %q":                             "invalid LOCATION %q",
		"LOCATION %q: nur http und https erlaubt":           "LOCATION %q: only http and https allowed",
		"HTTP-Status %d":                                    "HTTP status %d",
		"Gerätebeschreibung größer als %d KB":               "Device description larger than %d KB",
	})
}
