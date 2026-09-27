package pfsense

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"pfSense": "pfSense",
		"Liest DHCP-Leases (ISC oder Kea), statische Zuordnungen und die ARP-Tabelle von pfSense-Firewalls per SSH (nur feste Lesebefehle).": "Reads DHCP leases (ISC or Kea), static mappings and the ARP table from pfSense firewalls over SSH (fixed read-only commands only).",
		"Firewalls":                         "Firewalls",
		"Hostname oder IP, eine pro Zeile.": "Host name or IP, one per line.",
		"SSH-Zugang (admin oder ein Benutzer mit dem Recht „User - System: Shell account access“; SSH unter System → Advanced aktivieren).":                                                                                                                     "SSH access (admin or a user with the “User - System: Shell account access” privilege; enable SSH under System → Advanced).",
		"SSH-Zugang (admin oder ein Benutzer mit dem Recht „User - System: Shell account access“; SSH unter System → Advanced aktivieren). Leer = automatisch die passenden je Gerät nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen.": "SSH access (admin or a user with the “User - System: Shell account access” privilege; enable SSH under System → Advanced). Empty = automatically the matching ones per device according to the credential's scope; rejected ones are skipped.",
		"SSH-Port":          "SSH port",
		"SSH-Hostschlüssel": "SSH host key",
		"Beim ersten Kontakt merken, Änderungen ablehnen": "Remember on first contact, reject changes",
		"Nicht prüfen (unsicher)":                         "Do not verify (insecure)",

		// validation, run log, run errors
		"%q: Hostname oder IP erwartet":           "%q: host name or IP expected",
		"SSH-Verbindung zu %s fehlgeschlagen: %w": "SSH connection to %s failed: %w",
		"keine Befehlsausgabe – die Anmeldung landet vermutlich im Konsolenmenü von pfSense. Einen Benutzer mit Shell-Zugriff verwenden („User - System: Shell account access“)": "no command output – the login probably ends up in the pfSense console menu. Use a user with shell access (“User - System: Shell account access”)",
		"config.xml nicht lesbar – statische Zuordnungen und Schnittstellennamen fehlen (Benutzer ohne Leserecht?)":                                                              "config.xml not readable – static mappings and interface names are missing (user without read permission?)",
	})
}
