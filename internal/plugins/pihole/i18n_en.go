package pihole

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"Pi-hole": "Pi-hole",
		"Liest DHCP-Leases und Reservierungen sowie die Netzwerk-Tabelle (Geräte mit Namen aus DNS-Anfragen) von Pi-hole v6 über die REST-API.":                                                                                         "Reads DHCP leases and reservations as well as the network table (devices with names from DNS queries) from Pi-hole v6 via the REST API.",
		"Hostname, IP oder URL (z. B. http://pi.hole oder https://pihole.lan), einer pro Zeile.":                                                                                                                                        "Host name, IP or URL (e.g. http://pi.hole or https://pihole.lan), one per line.",
		"Benutzer/Passwort mit dem Web-Passwort oder besser einem App-Passwort von Pi-hole (Benutzer bleibt leer).":                                                                                                                     "Username/password with the Pi-hole web password or, better, an app password (leave the username empty).",
		"Benutzer/Passwort mit dem Web-Passwort oder besser einem App-Passwort von Pi-hole (Benutzer bleibt leer). Leer = automatisch die passenden je Gerät nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen.": "Username/password with the Pi-hole web password or, better, an app password (leave the username empty). Empty = automatically the matching ones per device according to the credential's scope; rejected ones are skipped.",
		"Netzwerk-Tabelle einbeziehen": "Include network table",
		"Auch Geräte aus der Netzwerk-Tabelle von Pi-hole (alle, die DNS-Anfragen stellen – mit oder ohne DHCP).": "Also devices from Pi-hole's network table (everything that sends DNS queries – with or without DHCP).",

		// run log, run errors
		"Pi-hole lehnt die Anmeldung ab: zu viele Sitzungen oder Versuche (HTTP 429)": "Pi-hole rejects the login: too many sessions or attempts (HTTP 429)",
		"ohne Passwort":                    "without password",
		"DHCP-Reservierungen nicht lesbar": "Cannot read DHCP reservations",
		"Netzwerk-Tabelle: %w":             "Network table: %w",
	})
}
