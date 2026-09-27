package csv

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"CSV-Import": "CSV import",
		"Importiert Geräte aus einer CSV-Datei mit Kopfzeile (deutsche oder englische Spaltennamen). Der Inventar-Export unter Berichte liefert dasselbe Format und lässt sich so wieder einlesen.": "Imports devices from a CSV file with a header row (German or English column names). The inventory export under Reports produces the same format, so it can be read back in.",
		"CSV-Datei": "CSV file",
		"Erste Zeile = Spaltennamen, jede Zeile braucht eine MAC- oder IP-Adresse. Spalten: mac, macs, ip, ips, name (Anzeigename), hostname, " +
			"type/typ, vendor/hersteller, model, os, location/aufstellort/standort, owner/besitzer, tags (getrennt durch ; oder |), state/zustand " +
			"(bekannt, unbekannt, ignoriert), criticality/kritikalität (niedrig, normal, hoch, kritisch), notes/notizen sowie cf.<schlüssel> " +
			"oder custom:<schlüssel> für eigene Felder. Unbekannte Spalten (z. B. id, online, first_seen) werden ignoriert.": "First row = column names, every row needs a MAC or IP address. Columns: mac, macs, ip, ips, name (display name), hostname, " +
			"type, vendor, model, os, location, owner, tags (separated by ; or |), state (known, unknown, ignored), " +
			"criticality (low, normal, high, critical), notes and cf.<key> or custom:<key> for custom fields. " +
			"Unknown columns (e.g. id, online, first_seen) are ignored.",
		"Trennzeichen":                     "Delimiter",
		"Automatisch erkennen":             "Detect automatically",
		"Komma (,)":                        "Comma (,)",
		"Semikolon (;)":                    "Semicolon (;)",
		"Tabulator":                        "Tab",
		"Vorhandene Angaben überschreiben": "Overwrite existing values",
		"Aus: Anzeigename, Typ, Aufstellort, Besitzer, Notizen, Zustand, Kritikalität und eigene Felder werden nur gefüllt, wenn sie leer sind.": "Off: display name, type, location, owner, notes, state, criticality and custom fields are only filled in when they are empty.",
		"Fehlende Geräte anlegen": "Create missing devices",
		"Aus: Zeilen ohne passendes Gerät im Inventar werden übersprungen.": "Off: rows without a matching device in the inventory are skipped.",

		// run log, run errors
		"keine Datei angegeben":  "no file specified",
		"Datei nicht lesbar: %w": "cannot read file: %w",
		"Datei größer als %d MB": "file larger than %d MB",
		"Datei ist nicht UTF-8-kodiert, wird als Windows-1252 gelesen": "File is not UTF-8 encoded, reading it as Windows-1252",
		"die Datei ist leer":         "the file is empty",
		"Kopfzeile nicht lesbar: %w": "cannot read header row: %w",
		"keine verwendbare Kopfzeile: Spalte mac/macs oder ip/ips fehlt (gelesen: %s, Trennzeichen %q)": "no usable header row: column mac/macs or ip/ips missing (read: %s, delimiter %q)",
		"Eigene Felder mit ungültigem Schlüssel werden ignoriert (erlaubt: a-z, 0-9, _)":                "Custom fields with an invalid key are ignored (allowed: a-z, 0-9, _)",
		"Unbekannte Spalten werden ignoriert":                                                           "Unknown columns are ignored",
		"Zeile nicht lesbar":                                                                            "Cannot read line",
		"CSV nicht lesbar: %w":                                                                          "cannot read CSV: %w",
		"CSV-Import abgeschlossen":                                                                      "CSV import finished",
		"Zeile %d":                                                                                      "Line %d",
		"Zeile %d: %s":                                                                                  "line %d: %s", // same as internal/wgconf
		"Zeile %d: kein passendes Gerät im Inventar, übersprungen":                                      "Line %d: no matching device in the inventory, skipped",

		// row problems, alone and as "Zeile %d: <problem>"
		"ungültige MAC-Adresse wird ignoriert":                                               "invalid MAC address ignored",
		"ungültige IP-Adresse wird ignoriert":                                                "invalid IP address ignored",
		"weder gültige MAC- noch IP-Adresse, Zeile übersprungen":                             "neither a valid MAC nor a valid IP address, line skipped",
		"unbekannter Zustand wird ignoriert (bekannt, unbekannt, ignoriert)":                 "unknown state ignored (known, unknown, ignored)",
		"unbekannte Kritikalität wird ignoriert (niedrig, normal, hoch, kritisch)":           "unknown criticality ignored (low, normal, high, critical)",
		"Gerät konnte nicht gespeichert werden":                                              "Could not save device",
		"Zeile %d: ungültige MAC-Adresse wird ignoriert":                                     "Line %d: invalid MAC address ignored",
		"Zeile %d: ungültige IP-Adresse wird ignoriert":                                      "Line %d: invalid IP address ignored",
		"Zeile %d: weder gültige MAC- noch IP-Adresse, Zeile übersprungen":                   "Line %d: neither a valid MAC nor a valid IP address, line skipped",
		"Zeile %d: unbekannter Zustand wird ignoriert (bekannt, unbekannt, ignoriert)":       "Line %d: unknown state ignored (known, unknown, ignored)",
		"Zeile %d: unbekannte Kritikalität wird ignoriert (niedrig, normal, hoch, kritisch)": "Line %d: unknown criticality ignored (low, normal, high, critical)",
		"Zeile %d: Gerät konnte nicht gespeichert werden":                                    "Line %d: could not save device",
	})
}
