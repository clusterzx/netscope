package oui

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings, action
		"OUI-Hersteller": "OUI vendor",
		"Ermittelt den Hersteller eines Geräts aus seiner MAC-Adresse anhand einer lokalen OUI-Datei (IEEE, arp-scan, nmap).": "Determines a device's vendor from its MAC address using a local OUI file (IEEE, arp-scan, nmap).",
		"Aktualisierung": "Update",
		"Download-URLs":  "Download URLs",
		"IEEE-Registerdateien im CSV-Format (MA-L, MA-M, MA-S), die „OUI-Datei aktualisieren“ lädt. Der Dateiname der URL bestimmt die lokale Datei.": "IEEE registry files in CSV format (MA-L, MA-M, MA-S) that “Update OUI file” downloads. The file name in the URL determines the local file.",
		"Download-Timeout":                          "Download timeout",
		"Maximale Dauer eines einzelnen Downloads.": "Maximum duration of a single download.",
		"OUI-Datei aktualisieren":                   "Update OUI file",
		"Lädt die aktuellen Herstellerlisten der IEEE (MA-L, MA-M, MA-S) herunter und ersetzt die lokale OUI-Datei.": "Downloads the current IEEE vendor lists (MA-L, MA-M, MA-S) and replaces the local OUI file.",

		// run log, errors, action results
		"keine OUI-Datei vorhanden – bitte auf der Plugin-Seite „OUI-Datei aktualisieren“ ausführen": "no OUI file available – run “Update OUI file” on the plugin page",
		"Einige OUI-Dateien konnten nicht gelesen werden":                                            "Some OUI files could not be read",
		"Ergebnis konnte nicht gespeichert werden":                                                   "Could not save result",
		"OUI-Nachschlagen übersprungen":                                                              "OUI lookup skipped",
		"Hersteller konnte nicht gespeichert werden":                                                 "Could not save vendor",
		"unbekannte Aktion %q":                                                                       "unknown action %q",
		"OUI-Aktualisierung fehlgeschlagen: %w":                                                      "OUI update failed: %w",
		"OUI-Dateien aktualisiert":                                                                   "OUI files updated",
		"OUI-Datei aktualisiert: %s Einträge (%s)":                                                   "OUI file updated: %s entries (%s)",
		"CSV-Kopfzeile fehlt: %w":                                                                    "CSV header missing: %w",
		"keine IEEE-Registerdatei (Kopfzeile „Registry,Assignment,…“ erwartet)":                      "not an IEEE registry file (header “Registry,Assignment,…” expected)",
		"CSV-Fehler: %w":                                            "CSV error: %w",
		"ungültige URL %q":                                          "invalid URL %q",
		"URL %q verweist auf keine CSV-Datei":                       "URL %q does not point to a CSV file",
		"keine Download-URLs konfiguriert":                          "no download URLs configured",
		"kein Datenverzeichnis für das Plugin konfiguriert":         "no data directory configured for the plugin",
		"Dateiname %s mehrfach konfiguriert":                        "file name %s configured more than once",
		"HTTP-Status %d":                                            "HTTP status %d",
		"Download abgebrochen: %w":                                  "Download aborted: %w",
		"Datei größer als %d MB":                                    "file larger than %d MB",
		"ungültige Datei: %w":                                       "invalid file: %w",
		"ungültige Datei: nur %d Einträge (mindestens %d erwartet)": "invalid file: only %d entries (at least %d expected)",
	})
}
