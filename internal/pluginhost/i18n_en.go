package pluginhost

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// credential scope reasons
		"überall":          "everywhere",
		"Subnetz %s":       "Subnet %s",
		"Gerät zugewiesen": "Device assigned",
		"Gruppe „%s“":      "Group “%s”",
		"Tag „%s“":         "Tag “%s”",

		// runs
		"Plugin ist bereits eingeplant oder läuft":             "Plugin is already queued or running",
		"Plugin %s kann nicht ausgeführt werden":               "Plugin %s cannot be run",
		"Lauf %d läuft nicht":                                  "Run %d is not running",
		"Scope auflösen: %w":                                   "Resolving scope: %w",
		"Subnetze übersprungen – Tunnel getrennt":              "Subnets skipped – tunnel disconnected",
		"Lauf gestartet":                                       "Run started",
		"Plugin hat keine Läufe":                               "Plugin has no runs",
		"Zeitüberschreitung nach %s":                           "Timeout after %s",
		"Lauf beendet":                                         "Run finished",
		"Lauf nicht erfolgreich":                               "Run not successful",
		"Wiederholung eingeplant":                              "Retry scheduled",
		"%s: Lauf fehlgeschlagen":                              "%s: run failed",
		"Anwesenheitsauswertung ohne fehlgeschlagene Subnetze": "Presence evaluation without the failed subnets",
		"Plugin-Panic":                                         "Plugin panic",
		"Plugin-Panic: %v":                                     "Plugin panic: %v",
		"%d weitere Protokollzeilen verworfen (Limit %d)":      "%d more log lines dropped (limit %d)",
		"Zeitplan": "Schedule",
		"Geplanter Lauf übersprungen – vorheriger Lauf aktiv": "Scheduled run skipped – previous run still active",
		"Plugin nicht vorhanden":                              "Plugin not available",
		"Abgebrochen: NetScope wird beendet":                  "Cancelled: NetScope is shutting down",
		"Abgebrochen durch Benutzer":                          "Cancelled by user",
		"Abgebrochen: NetScope wurde neu gestartet":           "Cancelled: NetScope was restarted",

		// change handlers and actions
		"Live-Update auf nicht erlaubtem Topic verworfen": "Live update on a disallowed topic dropped",
		"Änderungen verworfen – Verarbeitung zu langsam":  "Changes dropped – processing too slow",
		"Verarbeitung fehlgeschlagen":                     "Processing failed",
		"Plugin %q existiert nicht":                       "Plugin %q does not exist",
		"Plugin %q hat keine Aktionen":                    "Plugin %q has no actions",
		"unbekannte Aktion %q":                            "unknown action %q",
		"Plugin %s ist deaktiviert":                       "Plugin %s is disabled",
		"keine Geräte ausgewählt":                         "no devices selected",
		"Plugin hat keine Aktionen":                       "Plugin has no actions",
		"Aktion gestartet":                                "Action started",

		// configuration
		"Secrets entschlüsseln: %w":      "Decrypting secrets: %w",
		"Gruppe %d existiert nicht":      "Group %d does not exist",
		"dieses Plugin hat keine Läufe":  "this plugin has no runs",
		"muss zwischen %s und %s liegen": "must be between %s and %s",
		"0–%d erlaubt":                   "0–%d allowed",
		"1–%d erlaubt":                   "1–%d allowed",

		// publishing
		"Publisher ist deaktiviert":    "Publisher is disabled",
		"Publisher %q existiert nicht": "Publisher %q does not exist",
		"Plugin %q ist kein Publisher": "Plugin %q is not a publisher",
		"NetScope: Testnachricht":      "NetScope: test message",
		"Diese Testnachricht wurde von %s ausgelöst. Ist sie angekommen, ist der Publisher korrekt eingerichtet.": "This test message was triggered by %s. If it arrived, the publisher is set up correctly.",

		// metrics
		"Plugin-Läufe nach Status":               "Plugin runs by status",
		"Summierte Laufzeit der Plugin-Läufe":    "Total duration of plugin runs",
		"Dauer des letzten Laufs":                "Duration of the last run",
		"Zugestellte Benachrichtigungen":         "Delivered notifications",
		"Von Processorn verarbeitete Änderungen": "Changes handled by processors",
		"Aktuell laufende Plugin-Läufe":          "Plugin runs currently running",
		"Ausstehende Änderungen je Processor":    "Pending changes per processor",
	})
}
