package opnsense

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"OPNsense": "OPNsense",
		"Liest DHCP-Leases (Kea, Dnsmasq, ISC), Reservierungen und die ARP-Tabelle von OPNsense-Firewalls über die REST-API und füllt damit Namen, Adressen und Hersteller – auch für Netze hinter der Firewall.": "Reads DHCP leases (Kea, Dnsmasq, ISC), reservations and the ARP table from OPNsense firewalls via the REST API and fills in names, addresses and vendors – also for networks behind the firewall.",
		"Firewalls": "Firewalls",
		"Hostname, IP oder URL der Weboberfläche (z. B. https://fw.example.lan:8443), eine pro Zeile.": "Host name, IP or URL of the web interface (e.g. https://fw.example.lan:8443), one per line.",
		"API-Schlüssel aus System → Zugang → Benutzer → API-Schlüssel: Credential „API-Token“ mit Key als Token-ID und Secret als Token (oder Benutzer/Passwort mit Key und Secret). Nötige Rechte: DHCP-Leases (Kea, Dnsmasq bzw. ISC) und Diagnose: ARP-Tabelle.":                                                                                                                     "API key from System → Access → Users → API keys: credential “API token” with the key as token ID and the secret as token (or username/password with key and secret). Required privileges: DHCP leases (Kea, Dnsmasq or ISC) and Diagnostics: ARP Table.",
		"API-Schlüssel aus System → Zugang → Benutzer → API-Schlüssel: Credential „API-Token“ mit Key als Token-ID und Secret als Token (oder Benutzer/Passwort mit Key und Secret). Nötige Rechte: DHCP-Leases (Kea, Dnsmasq bzw. ISC) und Diagnose: ARP-Tabelle. Leer = automatisch die passenden je Gerät nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen.": "API key from System → Access → Users → API keys: credential “API token” with the key as token ID and the secret as token (or username/password with key and secret). Required privileges: DHCP leases (Kea, Dnsmasq or ISC) and Diagnostics: ARP Table. Empty = automatically the matching ones per device according to the credential's scope; rejected ones are skipped.",

		// run log, run errors
		"keine Daten: Weder DHCP-Leases noch ARP-Tabelle erreichbar – Rechte des API-Schlüssels prüfen (DHCP-Leases, Diagnose: ARP-Tabelle)": "no data: neither DHCP leases nor ARP table reachable – check the API key's privileges (DHCP leases, Diagnostics: ARP Table)",
		"OPNsense-Quellen": "OPNsense sources",
		"gelesen":          "read",
	})
}
