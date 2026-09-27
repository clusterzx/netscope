package healthcheck

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin info and settings
		"Health-Checks": "Health checks",
		"Führt die konfigurierten Checks (TCP, HTTP, TLS, ICMP) im jeweiligen Intervall aus, dämpft Flattern über Schwellwerte, führt Ausfallhistorie und Verfügbarkeit (24 h / 7 d / 30 d) und erzeugt Events bei Zustandswechseln.": "Runs the configured checks (TCP, HTTP, TLS, ICMP) at their intervals, damps flapping with thresholds, keeps outage history and availability (24 h / 7 d / 30 d) and raises events on state changes.",
		"ICMP mit Raw-Sockets":                               "ICMP with raw sockets",
		"Benötigt root bzw. NET_RAW (im Container gegeben).": "Requires root or NET_RAW (granted in the container).",
		"HTTP User-Agent":                                    "HTTP user agent",
		"Jedes Ergebnis protokollieren":                      "Log every result",
		"Sonst werden nur Zustandswechsel und Fehler ins Laufprotokoll geschrieben.": "Otherwise only state changes and errors are written to the run log.",

		// check results (also event messages and outage reasons)
		"Gerät hat keine IP-Adresse":                "Device has no IP address",
		"kein Ziel":                                 "no target",
		"unbekannter Typ %s":                        "unknown type %s",
		"Antwortzeit %.0f ms über %d ms":            "Response time %.0f ms above %d ms",
		"HTTP-Status %d nicht erwartet (%s)":        "HTTP status %d not expected (%s)",
		"Antwort enthält den erwarteten Text nicht": "Response does not contain the expected text",
		"kein Zertifikat erhalten":                  "no certificate received",
		"Zertifikat abgelaufen am %s":               "Certificate expired on %s",
		"Zertifikat läuft in %d Tagen ab":           "Certificate expires in %d days",
		"keine Antwort (%d Pakete gesendet)":        "no response (%d packets sent)",
		"%.0f %% Paketverlust":                      "%.0f%% packet loss",
		"Zeitüberschreitung":                        "Timeout",

		// events
		"Check ausgefallen: %s":     "Check down: %s",
		"Check beeinträchtigt: %s":  "Check degraded: %s",
		"Check wieder OK: %s":       "Check OK again: %s",
		"Nach %s wieder erreichbar": "Reachable again after %s",

		// log messages
		"Ergebnis speichern fehlgeschlagen":     "Saving the result failed",
		"Zustandswechsel":                       "State change",
		"Check ausgeführt":                      "Check executed",
		"Event konnte nicht gespeichert werden": "Could not save event",

		// validation
		"ungültiger Statuscode %q":                    "invalid status code %q",
		"ungültiger Statusbereich %q":                 "invalid status range %q",
		"Statuscode außerhalb 100–599: %q":            "Status code outside 100–599: %q",
		"Name erforderlich":                           "Name required",
		"Port 1–65535 erforderlich":                   "Port 1–65535 required",
		"URL muss mit http:// oder https:// beginnen": "URL must start with http:// or https://",
		"URL oder Port erforderlich":                  "URL or port required",
		"Methode GET oder HEAD":                       "Method GET or HEAD",
		"Body-Regex: %s":                              "Body regex: %s",
		"Anzahl Pings 1–20":                           "Number of pings 1–20",
		"Typ muss tcp, http, tls oder icmp sein":      "Type must be tcp, http, tls or icmp",
		"Ziel (Host/IP) oder Gerät erforderlich":      "Target (host/IP) or device required",
		"Ziel muss ein Hostname oder eine IP sein":    "Target must be a hostname or an IP",
		"Intervall 30 Sekunden bis 24 Stunden":        "Interval 30 seconds to 24 hours",
		"Timeout 1–120 Sekunden":                      "Timeout 1–120 seconds",
		"Schwellwert 1–20":                            "Threshold 1–20",
		"negative Werte sind nicht erlaubt":           "negative values are not allowed",
		"Gerät %d existiert nicht":                    "Device %d does not exist",
	})
}
