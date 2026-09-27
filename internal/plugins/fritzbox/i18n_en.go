package fritzbox

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"FRITZ!Box": "FRITZ!Box",
		"Liest die Geräteliste von FRITZ!Box-Routern über TR-064: Namen, Adressen, LAN oder WLAN und ob ein Gerät gerade verbunden ist.":                                                                                                                                                                                 "Reads the device list of FRITZ!Box routers via TR-064: names, addresses, LAN or Wi-Fi and whether a device is currently connected.",
		"Hostname oder IP; https://… nutzt den verschlüsselten Port 49443. Eine pro Zeile.":                                                                                                                                                                                                                              "Host name or IP; https://… uses the encrypted port 49443. One per line.",
		"Benutzer/Passwort eines FRITZ!Box-Benutzers mit dem Recht „FRITZ!Box Einstellungen“. Unter Heimnetz → Netzwerk → Netzwerkeinstellungen muss „Zugriff für Anwendungen zulassen“ aktiv sein.":                                                                                                                     "Username/password of a FRITZ!Box user with the “FRITZ!Box Settings” permission. “Allow access for applications” must be enabled under Home Network → Network → Network Settings.",
		"Benutzer/Passwort eines FRITZ!Box-Benutzers mit dem Recht „FRITZ!Box Einstellungen“. Unter Heimnetz → Netzwerk → Netzwerkeinstellungen muss „Zugriff für Anwendungen zulassen“ aktiv sein. Leer = automatisch die passenden je Gerät nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen.": "Username/password of a FRITZ!Box user with the “FRITZ!Box Settings” permission. “Allow access for applications” must be enabled under Home Network → Network → Network Settings. Empty = automatically the matching ones per device according to the credential's scope; rejected ones are skipped.",
		"Auch nicht verbundene Geräte": "Include disconnected devices",
		"Die FRITZ!Box merkt sich Geräte, die gerade nicht verbunden sind; auch ihre Namen übernehmen.": "The FRITZ!Box remembers devices that are currently not connected; import their names too.",

		// run log, run errors
		"FRITZ!Box sperrt die Anmeldung vorübergehend nach Fehlversuchen (HTTP 503)":   "FRITZ!Box temporarily blocks login after failed attempts (HTTP 503)",
		"%s: dem Benutzer fehlt das Recht „FRITZ!Box Einstellungen“ (UPnP-Fehler 606)": "%s: the user lacks the “FRITZ!Box Settings” permission (UPnP error 606)",
		"Hostliste der FRITZ!Box nicht lesbar, Einzelabfrage":                          "Cannot read the FRITZ!Box host list, querying hosts one by one",
		"Hostliste der FRITZ!Box nicht verfügbar, Einzelabfrage":                       "FRITZ!Box host list not available, querying hosts one by one",
		"Hostliste: HTTP %d": "Host list: HTTP %d",
		"Hostliste: %w":      "Host list: %w",
	})
}
