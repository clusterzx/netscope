package app

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		"NetScope startet": "NetScope starting",
		"Wiederherstellung angefordert – Dienste werden neu gestartet": "Restore requested – restarting the services",
		"NetScope beendet":                 "NetScope stopped",
		"Wiederherstellung fehlgeschlagen": "Restore failed",
		"Neuer Vault-Master-Key erzeugt – bitte sichern, ohne ihn sind gespeicherte Zugangsdaten verloren": "New vault master key generated – please back it up, without it stored credentials are lost",
		"Admin-Benutzer angelegt – das Startpasswort steht in der Datei (nach dem ersten Login ändern)":    "Admin user created – the initial password is in the file (change it after the first login)",
		"Subnetze erkennen":          "Detect subnets",
		"Lokale Subnetze übernommen": "Local subnets added",
		"Gateways ermitteln":         "Determine gateways",
		"Gateway der Subnetze aus der Routing-Tabelle übernommen":                     "Subnet gateways taken from the routing table",
		"Weboberfläche abgeschaltet (NETSCOPE_UI=false) – nur die API ist erreichbar": "Web UI switched off (NETSCOPE_UI=false) – only the API is reachable",
		"Verbund aktiv":       "Federation active",
		"HTTP-Server: %w":     "HTTP server: %w",
		"NetScope bereit":     "NetScope ready",
		"Datenbank schließen": "Close database",
		"Wiederhergestellte Datenbank nicht nutzbar – vorherige Datenbank wird verwendet": "Restored database not usable – using the previous database",
		"Datenbank wiederhergestellt": "Database restored",
		"NetScope läuft ohne Weboberfläche (NETSCOPE_UI=false). API-Dokumentation: /api/docs\n":                                                "NetScope runs without a web UI (NETSCOPE_UI=false). API documentation: /api/docs\n",
		"NETSCOPE_ADMIN_PASSWORD gesetzt – Einrichtungsassistent übersprungen, ab Werk aktive Plugins laufen":                                  "NETSCOPE_ADMIN_PASSWORD set – setup wizard skipped, plugins enabled by default are running",
		"Ohne Weboberfläche kein Einrichtungsassistent – Benutzer admin mit Zufallspasswort angelegt, bei Bedarf mit „netscope passwd“ setzen": "No setup wizard without web interface – user admin created with a random password, set one with \"netscope passwd\" if needed",
		"Einrichtung ausstehend – NetScope im Browser öffnen und den Einrichtungscode eingeben; bis dahin läuft kein Plugin":                   "Setup pending – open NetScope in the browser and enter the setup code; until then no plugin runs",
		"Einrichtungscode: %w": "Setup code: %w",
	})
}
