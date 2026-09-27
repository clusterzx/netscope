package db

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		"NVD-Spiegel %s anhängen: %w": "attach NVD mirror %s: %w",
		"NVD-Spiegel: %w":             "NVD mirror: %w",
		"NVD-Spiegel anlegen: %w":     "create NVD mirror: %w",
		"NVD-Spiegel verschieben: %w": "move NVD mirror: %w",
		"NVD-Indizes: %w":             "NVD indexes: %w",
		"NVD-Spiegel wird einmalig aus der Datenbank in eine eigene Datei verschoben": "Moving the NVD mirror out of the database into a file of its own (once)",
		"NVD-Spiegel liegt in eigener Datei":                                          "NVD mirror is in a file of its own",
	})
}
