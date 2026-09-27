package wininv

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		"Ausgabe über der Größengrenze abgeschnitten": "output truncated at the size limit",
		"Ausgabe ist kein gültiges JSON: %w":          "output is not valid JSON: %w",
		// domain role
		"Domänencontroller": "Domain controller",
		"Server":            "Server",
		"Arbeitsplatz":      "Workstation",
		// errors of the script sections, attributes of DHCP reservations
		"Zeitüberschreitung": "Timeout",
		"Beschreibung":       "Description",
	})
}
