package rules

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// validation
		"Uhrzeit %q: HH:MM erwartet":                       "Time %q: HH:MM expected",
		"Name erforderlich":                                "Name required",
		"unbekannter Event-Typ %q":                         "unknown event type %q",
		"ungültiger Schweregrad %q":                        "invalid severity %q",
		"ungültiger Standort %d":                           "invalid site %d",
		"ungültiger Gerätezustand %q":                      "invalid device state %q",
		"Payload-Bedingung %d: Feld fehlt":                 "Payload condition %d: field missing",
		"Payload-Bedingung %d: Operator %q unbekannt":      "Payload condition %d: unknown operator %q",
		"Wochentag %d ungültig (0–6)":                      "Weekday %d invalid (0–6)",
		"mindestens eine Aktion erforderlich":              "at least one action required",
		"Aktion %d: unbekannter Publisher %q":              "Action %d: unknown publisher %q",
		"Aktion %d: ungültige Priorität %q":                "Action %d: invalid priority %q",
		"Aktion %d: Sammelzeitraum 1–1440 Minuten":         "Action %d: batch period 1–1440 minutes",
		"Aktion %d: Modus immediate oder batch":            "Action %d: mode immediate or batch",
		"Aktion %d: Drosselung als Dauer ≥ 1m (z. B. 24h)": "Action %d: throttle as a duration ≥ 1m (e.g. 24h)",
		"Aktion %d: Ruhezeit-Verhalten delay oder drop":    "Action %d: quiet hours behaviour delay or drop",
		"Aktion %d: Eskalation 0–10080 Minuten":            "Action %d: escalation 0–10080 minutes",
		"Aktion %d: unbekannter Eskalations-Publisher %q":  "Action %d: unknown escalation publisher %q",
		"Aktion %d: ungültige Eskalations-Priorität":       "Action %d: invalid escalation priority",
		"Regel %d: %w":         "Rule %d: %w",
		"Regel %d doppelt":     "Rule %d listed twice",
		"Standardregel %q: %w": "Default rule %q: %w",
		"Gerät %d: %w":         "Device %d: %w",

		// default rules
		"Neues unbekanntes Gerät → Telegram sofort":                                                                  "New unknown device → Telegram immediately",
		"Meldet jedes neu entdeckte Gerät, das nicht als bekannt markiert ist, sofort.":                              "Immediately reports every newly discovered device that is not marked as known.",
		"Neuer Port auf bekanntem Gerät → Telegram gesammelt":                                                        "New port on known device → Telegram batched",
		"Sammelt neu geöffnete Ports auf bekannten Geräten und schickt sie stündlich gebündelt.":                     "Collects newly opened ports on known devices and sends them bundled once an hour.",
		"CVE ≥ 9 → Telegram sofort":                                                                                  "CVE ≥ 9 → Telegram immediately",
		"Kritische Schwachstellen (CVSS ab 9.0) werden sofort gemeldet.":                                             "Critical vulnerabilities (CVSS 9.0 and above) are reported immediately.",
		"Zertifikat < 14 Tage → täglich einmal":                                                                      "Certificate < 14 days → once a day",
		"Zertifikate, die in weniger als 14 Tagen ablaufen (oder abgelaufen sind), höchstens einmal täglich melden.": "Report certificates that expire in less than 14 days (or have expired) at most once a day.",

		// condition explanations (simulation)
		"Event-Typ":                      "Event type",
		"Schweregrad":                    "Severity",
		"Standort":                       "Site",
		"Gerät":                          "Device",
		"Event hat kein Gerät":           "Event has no device",
		"Geräte-Tags [%s], gesucht [%s]": "Device tags [%s], wanted [%s]",
		"Gruppe":                         "Group",
		"Mitglied einer der Gruppen %v":  "Member of one of the groups %v",
		"Subnetz":                        "Subnet",
		" (Fehler: %s)":                  " (error: %s)",
		"Geräte-Filter":                  "Device filter",
		"Nur nicht bekannte Geräte":      "Only unknown devices",
		"Zustand %s":                     "State %s",
		"Gerätezustand":                  "Device state",
		"Zeitfenster":                    "Time window",
		"diese Instanz":                  "this instance",
		"Standort %d":                    "Site %d",
		"Das Gerät ist ignoriert – für ignorierte Geräte werden nie Benachrichtigungen verschickt.":                                                                   "The device is ignored – no notifications are ever sent for ignored devices.",
		"%s Die Regel ist deaktiviert und würde im Betrieb nicht ausgewertet.":                                                                                        "%s The rule is disabled and would not be evaluated in operation.",
		"Die Regel ist deaktiviert und würde im Betrieb nicht ausgewertet.":                                                                                           "The rule is disabled and would not be evaluated in operation.",
		"Das Gerät ist ignoriert – für ignorierte Geräte werden nie Benachrichtigungen verschickt. Die Regel ist deaktiviert und würde im Betrieb nicht ausgewertet.": "The device is ignored – no notifications are ever sent for ignored devices. The rule is disabled and would not be evaluated in operation.",

		// notifications (skip reasons, titles)
		"Publisher existiert nicht":                  "Publisher does not exist",
		"Publisher ist deaktiviert":                  "Publisher is disabled",
		"Ruhezeit":                                   "Quiet hours",
		"inzwischen quittiert":                       "acknowledged in the meantime",
		"Ereignisse nicht mehr vorhanden":            "events no longer exist",
		"Eskalation: %s":                             "Escalation: %s",
		"Eskalation: %d nicht quittierte Ereignisse": "Escalation: %d unacknowledged events",
		"%d Ereignisse":                              "%d events",
		"%d Ereignisse – %s":                         "%d events – %s",

		// log messages
		"Regelauswertung fehlgeschlagen":                         "Rule evaluation failed",
		"Benachrichtigung planen":                                "Scheduling notification",
		"Eskalationen prüfen":                                    "Checking escalations",
		"Benachrichtigungen zustellen":                           "Delivering notifications",
		"Zusatzinhalt der Benachrichtigung unlesbar":             "Additional notification content unreadable",
		"Benachrichtigung endgültig fehlgeschlagen":              "Notification failed permanently",
		"Benachrichtigung fehlgeschlagen, neuer Versuch geplant": "Notification failed, retry scheduled",
	})
}
