package sophos

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"Sophos Firewall": "Sophos Firewall",
		"Liest die DHCP-Reservierungen (Name, MAC, IP, Schnittstelle) von Sophos Firewalls über die XML-API. Aktuelle Leases und die ARP-Tabelle stellt SFOS über die API nicht bereit.": "Reads the DHCP reservations (name, MAC, IP, interface) from Sophos firewalls via the XML API. SFOS does not provide current leases or the ARP table via the API.",
		"Firewalls": "Firewalls",
		"Hostname, IP oder URL der Verwaltung (Standard-Port 4444), eine pro Zeile.":                                                                                                   "Host name, IP or URL of the management interface (default port 4444), one per line.",
		"Benutzer/Passwort eines Administrators. Die API muss aktiv und die Adresse von NetScope zugelassen sein (v20/21: Sicherung & Firmware → API; v22: Verwaltung → API-Zugriff).": "Username/password of an administrator. The API must be enabled and the address of NetScope allowed (v20/21: Backup & firmware → API; v22: Administration → API access).",
		"Benutzer/Passwort eines Administrators. Die API muss aktiv und die Adresse von NetScope zugelassen sein (v20/21: Sicherung & Firmware → API; v22: Verwaltung → API-Zugriff). Leer = automatisch die passenden je Gerät nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen.": "Username/password of an administrator. The API must be enabled and the address of NetScope allowed (v20/21: Backup & firmware → API; v22: Administration → API access). Empty = automatically the matching ones per device according to the credential's scope; rejected ones are skipped.",

		// run errors
		"die API ist nicht aktiviert":                                       "the API is not enabled",
		"die Adresse von NetScope ist für die API nicht zugelassen":         "the address of NetScope is not allowed for the API",
		"XML-API: HTTP %d":                                                  "XML API: HTTP %d",
		"XML-API: Antwort nicht lesbar: %w":                                 "XML API: cannot read response: %w",
		"Sophos-API: %s (Code %s)":                                          "Sophos API: %s (code %s)",
		"XML-API: unerwartete Antwort (keine Anmeldung, keine DHCP-Server)": "XML API: unexpected response (no login, no DHCP servers)",
	})
}
