package netsrc

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// settings fields shared by the importers
		"Zugangsdaten": "Credentials",
		"%s Leer = automatisch die passenden je Gerät nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen.": "%s Empty = automatically the matching ones per device according to the credential's scope; rejected ones are skipped.",
		"TLS-Zertifikat prüfen": "Verify TLS certificate",
		"Nur mit gültigem Zertifikat einschalten; die meisten Geräte nutzen ein selbstsigniertes.": "Only switch on with a valid certificate; most devices use a self-signed one.",
		"Fehlende Geräte anlegen": "Create missing devices",
		"Geräte anlegen, die noch kein Scan gefunden hat – nützlich für Netze, die NetScope nicht selbst scannt. Aus: nur bekannte Geräte ergänzen.": "Create devices that no scan has found yet – useful for networks that NetScope does not scan itself. Off: only add to known devices.",
		"ARP-Tabelle einbeziehen": "Include the ARP table",
		"Auch Geräte mit fester IP-Adresse (ohne Lease) aus der ARP-Tabelle übernehmen.": "Also import devices with a fixed IP address (without lease) from the ARP table.",
		"%q: Hostname, IP oder http(s)-URL erwartet":                                     "%q: host name, IP or http(s) URL expected",
		"leerer Eintrag": "empty entry",

		// credentials, HTTP
		"keine passenden Zugangsdaten für %s (Auswahl oder Geltungsbereich der Credentials prüfen): %w": "no matching credentials for %s (check the selection or the scope of the credentials): %w",
		"Anmeldung abgelehnt, nächste Zugangsdaten werden probiert":                                     "Login rejected, trying the next credentials",
		"Anmeldung abgelehnt":                      "login rejected",
		"%s: Antwort ist kein erwartetes JSON: %w": "%s: response is not the expected JSON: %w",

		// run log, run errors
		"kein %s konfiguriert":                            "no %s configured",
		"%s nicht gelesen":                                "Could not read %s",
		"keine Quelle gelesen: %w":                        "no source read: %w",
		"Netzwerkgerät konnte nicht gespeichert werden":   "Could not save network device",
		"Clients gelesen":                                 "Read clients",
		"Client konnte nicht gespeichert werden":          "Could not save client",
		"keiner der %d Clients konnte gespeichert werden": "none of the %d clients could be saved",
		// log attributes
		"quelle":          "source",
		"ziel":            "target",
		"netzwerkgeraete": "network devices",
		"Verbunden – %d Clients und %d Netzwerkgeräte gelesen": "Connected – read %d clients and %d network devices",
		"Verbunden – %d Clients gelesen":                       "Connected – read %d clients",
		"%w (HTTP %d)":                                         "%w (HTTP %d)",
	})
}
