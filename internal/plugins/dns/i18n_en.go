package dns

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"Reverse-DNS": "Reverse DNS",
		"Ermittelt Hostnamen per Reverse-DNS-Abfrage (PTR) gegen einen konfigurierbaren DNS-Server.": "Determines host names via reverse DNS lookup (PTR) against a configurable DNS server.",
		"DNS-Server": "DNS server",
		"System-DNS": "System DNS",
		"Hostname oder IP des DNS-Servers, optional mit Port (Standard 53). Meist der Router, der die DHCP-Namen kennt. Leer: die DNS-Server des Systems (/etc/resolv.conf).": "Host name or IP of the DNS server, optionally with port (default 53). Usually the router that knows the DHCP names. Empty: the system's DNS servers (/etc/resolv.conf).",
		"Timeout pro Abfrage":                             "Timeout per query",
		"Maximale Wartezeit auf eine Antwort je Adresse.": "Maximum time to wait for an answer per address.",
		"Alle IP-Adressen abfragen":                       "Query all IP addresses",
		"Aus: nur die primäre IP eines Geräts. An: alle bekannten Adressen, der erste gefundene Name gewinnt.": "Off: only a device's primary IP. On: all known addresses, the first name found wins.",

		// validation, run log, run errors
		"DNS-Server fehlt": "DNS server missing",
		"ungültiger DNS-Server %q (host oder host:port erwartet)": "invalid DNS server %q (host or host:port expected)",
		"Reverse-DNS ohne Ergebnis":                               "Reverse DNS without result",
		"Ergebnis konnte nicht gespeichert werden":                "Could not save result",
		"DNS-Server %s hat auf keine der %d Abfragen geantwortet": "DNS server %s did not answer any of the %d queries",
		"System-DNS hat auf keine der %d Abfragen geantwortet":    "System DNS did not answer any of the %d queries",
	})
}
