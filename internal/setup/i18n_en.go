package setup

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		"Die Einrichtung ist bereits abgeschlossen":           "The setup is already finished",
		"Einrichtungscode-Datei konnte nicht gelöscht werden": "The setup code file could not be deleted",
	})
}
