package cleanup

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin info and settings
		"Aufräumen & Downsampling": "Cleanup & downsampling",
		"Verdichtet Zeitreihen (roh → 5-Minuten → Stunden) und löscht Rohdaten nach ihrer Aufbewahrungsfrist (Beobachtungen, Laufprotokolle, Historie, Zeitreihen, alte Uploads/Backups). Events bleiben immer erhalten.": "Condenses time series (raw → 5 minutes → hours) and deletes raw data after its retention period (observations, run logs, history, time series, old uploads/backups). Events are always kept.",
		"Aufbewahrung (Tage)":                  "Retention (days)",
		"Rohdaten (Beobachtungen)":             "Raw data (observations)",
		"Rohdaten pro Plugin und Lauf.":        "Raw data per plugin and run.",
		"Rohdaten häufiger Anwesenheits-Scans": "Raw data of frequent presence scans",
		"Rohdaten der Anwesenheits-Scanner (ARP, ICMP, nmap), die sehr oft laufen. Begrenzt auch den Lauf-Vergleich dieser Plugins.": "Raw data of the presence scanners (ARP, ICMP, nmap), which run very often. Also limits the run comparison of these plugins.",
		"Laufprotokolle":                        "Run logs",
		"Laufhistorie":                          "Run history",
		"Abgeschlossene Läufe inkl. Protokoll.": "Finished runs incl. log.",
		"Benachrichtigungsverlauf":              "Notification history",
		"Zustandshistorie":                      "State history",
		"Geschlossene historische Einträge (Ports, Zertifikate, Pakete, Container, IPs, Hostnamen, CVEs). Begrenzt auch den Diff-Zeitraum.": "Closed historical entries (ports, certificates, packages, containers, IPs, hostnames, CVEs). Also limits the diff period.",
		"Zeitreihen roh":                     "Time series raw",
		"Zeitreihen 5-Minuten-Aggregate":     "Time series 5-minute aggregates",
		"Zeitreihen Stunden-Aggregate":       "Time series hourly aggregates",
		"Audit-Log":                          "Audit log",
		"0 = unbegrenzt aufbewahren.":        "0 = keep indefinitely.",
		"Hochgeladene Dateien":               "Uploaded files",
		"Importdateien unter /data/uploads.": "Import files under /data/uploads.",
		"Anzahl aufbewahrter Backups":        "Number of backups kept",
		"Löschlauf höchstens alle":           "Delete old data at most every",
		"Downsampling läuft bei jedem Lauf, das Löschen alter Daten seltener.": "Downsampling runs on every run, deleting old data less often.",

		// log messages
		"Zeitreihen verdichtet":   "Time series condensed",
		"WAL-Checkpoint":          "WAL checkpoint",
		"Aufräumen abgeschlossen": "Cleanup finished",
	})
}
