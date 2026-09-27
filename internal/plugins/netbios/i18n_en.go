package netbios

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"NetBIOS": "NetBIOS",
		"Fragt die NetBIOS-Namenstabelle (NBSTAT) ab: Rechnername, Arbeitsgruppe/Domäne und MAC-Adresse von Windows- und Samba-Geräten.": "Queries the NetBIOS name table (NBSTAT): computer name, workgroup/domain and MAC address of Windows and Samba devices.",
		"Timeout pro Gerät": "Timeout per device",
		"Wartezeit auf die NBSTAT-Antwort; nach der Hälfte wird die Anfrage einmal wiederholt.": "Time to wait for the NBSTAT response; after half of it the request is repeated once.",

		// run log, response errors
		"Keine NetBIOS-Antwort":                    "No NetBIOS response",
		"Ergebnis konnte nicht gespeichert werden": "Could not save result",
		"NBSTAT-Antwort zu kurz":                   "NBSTAT response too short",
		"ungültiges Namensformat":                  "invalid name format",
		"keine Antwort":                            "no response",
		"Fehlercode %d":                            "Error code %d",
		"Antwort ohne Namenstabelle":               "Response without name table",
		"unerwarteter Record-Typ 0x%04x":           "unexpected record type 0x%04x",
	})
}
