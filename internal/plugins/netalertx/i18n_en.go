package netalertx

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"NetAlertX-Import": "NetAlertX import",
		"Übernimmt einmalig den Bestand aus NetAlertX oder Pi.Alert (app.db oder CSV-Export): Namen, Typen, Besitzer, Standorte, Gruppen, Erstsichtung und Netzwerk-Eltern.": "Takes over the inventory from NetAlertX or Pi.Alert once (app.db or CSV export): names, types, owners, locations, groups, first seen and network parents.",
		"Datei": "File",
		"NetAlertX-Datenbank (app.db, auch ältere Pi.Alert-Datenbanken) oder CSV-Export (devices.csv aus Wartung → Backup/Wiederherstellung → CSV-Export).": "NetAlertX database (app.db, also older Pi.Alert databases) or CSV export (devices.csv from Maintenance → Backup/Restore → CSV Export).",
		"Format":                           "Format",
		"Automatisch erkennen":             "Detect automatically",
		"SQLite-Datenbank (app.db)":        "SQLite database (app.db)",
		"CSV-Export":                       "CSV export",
		"Vorhandene Angaben überschreiben": "Overwrite existing values",
		"Aus: nur leere Felder (Anzeigename, Typ, Besitzer, Standort, Notizen, Zustand) werden gefüllt.": "Off: only empty fields (display name, type, owner, location, notes, state) are filled in.",
		"Erstsichtung übernehmen": "Import first seen",
		"Die frühere Erstsichtung aus NetAlertX ersetzt eine spätere in NetScope.":        "An earlier first-seen time from NetAlertX replaces a later one in NetScope.",
		"Bestätigte Geräte als bekannt markieren":                                         "Mark confirmed devices as known",
		"Geräte, die NetAlertX nicht mehr als neu führt, erhalten den Zustand „bekannt“.": "Devices that NetAlertX no longer lists as new get the state “known”.",
		"Archivierte Geräte ignorieren":                                                   "Ignore archived devices",
		"In NetAlertX archivierte Geräte erhalten den Zustand „ignoriert“.":               "Devices archived in NetAlertX get the state “ignored”.",

		// run log, run errors
		"keine Datei angegeben":                                                       "no file specified",
		"Datei nicht lesbar: %w":                                                      "cannot read file: %w",
		"NetAlertX-Export gelesen":                                                    "Read NetAlertX export",
		"NetAlertX-Import abgeschlossen":                                              "NetAlertX import finished",
		"kein Gerät konnte importiert werden":                                         "no device could be imported",
		"Gerät ohne gültige MAC- und IP-Adresse übersprungen":                         "Skipped device without a valid MAC and IP address",
		"Gerät konnte nicht importiert werden":                                        "Could not import device",
		"NIC-Verknüpfung nicht übernommen (Gerät ist Netzwerkkarte des Elterngeräts)": "NIC link not imported (device is a network card of the parent device)",
		"Elterngerät unbekannt, Beziehung übersprungen":                               "Parent device unknown, relation skipped",
		"Beziehung konnte nicht gespeichert werden":                                   "Could not save relation",

		// reading the export
		"Datenbank nicht lesbar: %w":                                          "cannot read database: %w",
		"keine NetAlertX-Datenbank: Tabelle Devices fehlt":                    "not a NetAlertX database: table Devices missing",
		"unbekanntes Schema: Tabelle Devices hat keine Spalte devMac/dev_MAC": "unknown schema: table Devices has no column devMac/dev_MAC",
		"Geräte nicht lesbar: %w":                                             "cannot read devices: %w",
		"CSV-Datei ist nicht UTF-8-kodiert":                                   "CSV file is not UTF-8 encoded",
		"CSV-Kopfzeile nicht lesbar: %w":                                      "cannot read CSV header row: %w",
		"keine NetAlertX-CSV: Spalte devMac bzw. dev_MAC fehlt":               "not a NetAlertX CSV: column devMac or dev_MAC missing",
		"CSV Zeile %d: %w":                                                    "CSV line %d: %w",
	})
}
