package report

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin info and settings
		"Geplanter Bericht": "Scheduled report",
		"Verschickt regelmäßig (Standard: montags 08:00) einen Änderungsbericht über die gewählten Publisher.": "Regularly sends a change report (default: Mondays 08:00) through the selected publishers.",
		"Publisher": "Publishers",
		"Plugin-IDs der Publisher, über die der Bericht verschickt wird (z. B. telegram, email).": "Plugin IDs of the publishers that send the report (e.g. telegram, email).",
		"Zeitraum (Tage)": "Period (days)",
		"Priorität":       "Priority",
		"Niedrig":         "Low",
		"Normal":          "Normal",
		"Hoch":            "High",
		"Titel":           "Title",
		"Kalenderwoche und Zeitraum werden angehängt.": "Calendar week and period are appended.",

		// validation and run
		"Publisher %q existiert nicht": "Publisher %q does not exist",
		"%q ist kein Publisher":        "%q is not a publisher",
		"keine Publisher konfiguriert": "no publishers configured",
		"Bericht als PDF: %w":          "report as PDF: %w",
		"Bericht eingereiht":           "Report queued",

		// report title (the default title, then calendar week and period)
		"NetScope Wochenbericht": "NetScope weekly report",
		"NetScope Bericht":       "NetScope report",
		"%s KW %d (%s–%s)":       "%s, week %d (%s–%s)",
	})
}
