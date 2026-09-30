package federation

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// sites (central instance)
		"Name erforderlich":    "Name required",
		"höchstens 64 Zeichen": "at most 64 characters",
		"Name braucht mindestens einen Buchstaben oder eine Ziffer":                            "Name needs at least one letter or digit",
		"gültige http(s)-URL erwartet":                                                         "valid http(s) URL expected",
		"Es gibt bereits einen Standort mit diesem Namen":                                      "A site with this name already exists",
		"diese NetScope-Instanz ist nicht als Zentrale eingerichtet":                           "this NetScope instance is not set up as a central instance",
		"Der Standort spricht Protokoll %d, diese Zentrale nur %d–%d: Zentrale aktualisieren":  "The site speaks protocol %d, this central instance only %d–%d: update the central instance",
		"Der Standort spricht Protokoll %d, diese Zentrale erst ab %d: Standort aktualisieren": "The site speaks protocol %d, this central instance requires %d or later: update the site",

		// deliveries of the sites (central instance log)
		"Verbund: Lücke in der Lieferung – vollständiger Abgleich angefordert": "Federation: gap in the delivery – full synchronisation requested",
		"Verbund: Verarbeitung unterbrochen":                                   "Federation: processing interrupted",
		"Verbund: Eintrag des Standorts übersprungen":                          "Federation: skipped an entry of the site",
		"Verbund: Event-Typ des Standorts unbekannt (Zentrale älter?)":         "Federation: unknown event type from the site (central instance older?)",
		"Verbund: vollständiger Abgleich beginnt":                              "Federation: full synchronisation starting",
		"Verbund: vollständiger Abgleich abgeschlossen":                        "Federation: full synchronisation finished",
		"Verbund: unbekannte Eintragsart übersprungen":                         "Federation: skipped an unknown kind of entry",
		"Verbund: Standorte prüfen":                                            "Federation: checking sites",

		// events
		"Standort %s meldet sich wieder":                                            "Site %s reporting again",
		"Standort %s meldet sich nicht":                                             "Site %s not reporting",
		"Letzte Meldung %s. Der Standort puffert seine Daten und liefert sie nach.": "Last report %s. The site buffers its data and delivers it later.",

		// settings
		"Zentrale":                             "Central instance",
		"Verbund-Einstellungen: %w":            "federation settings: %w",
		"Token der Zentrale entschlüsseln: %w": "decrypt the token of the central instance: %w",
		"Die Anbindung an die Zentrale ist über Umgebungsvariablen festgelegt (NETSCOPE_CENTRAL_URL)":  "The connection to the central instance is set by environment variables (NETSCOPE_CENTRAL_URL)",
		"http(s)-URL der Zentrale erwartet, z. B. https://netscope.example.org":                        "http(s) URL of the central instance expected, e.g. https://netscope.example.org",
		"Token des Standorts erforderlich (wird in der Zentrale beim Anlegen des Standorts angezeigt)": "Site token required (shown in the central instance when the site is created)",
		"Standort-Tokens beginnen mit %s":                                  "Site tokens start with %s",
		"SHA-256-Fingerprint als 64 Hex-Zeichen erwartet":                  "SHA-256 fingerprint expected as 64 hex characters",
		"unbekannte Rolle %q":                                              "unknown role %q",
		"Event für die Zentrale vormerken":                                 "Queue event for the central instance",
		"diese Instanz ist nicht als Standort an eine Zentrale angebunden": "this instance is not connected to a central instance as a site",

		// delivery (site)
		"Verbund: Abgleich vorbereiten":                                                                "Federation: preparing synchronisation",
		"Verbund: Puffer begrenzen":                                                                    "Federation: limiting the buffer",
		"Verbund: Zustellung an die Zentrale fehlgeschlagen":                                           "Federation: delivery to the central instance failed",
		"Verbund: Die Zentrale verlangt einen vollständigen Abgleich":                                  "Federation: the central instance requests a full synchronisation",
		"Verbund: Puffer voll – Beobachtungen verworfen, vollständiger Abgleich folgt":                 "Federation: buffer full – observations discarded, full synchronisation follows",
		"Verbund: vollständiger Abgleich vorbereitet":                                                  "Federation: full synchronisation prepared",
		"die Zentrale lehnt das Token ab – Token in der Zentrale neu erzeugen und hier eintragen":      "the central instance rejects the token – generate a new token in the central instance and enter it here",
		"Zentrale nicht erreichbar: %w":                                                                "central instance not reachable: %w",
		"Antwort der Zentrale unlesbar: %w":                                                            "unreadable response from the central instance: %w",
		"kein NetScope unter dieser Adresse oder Version ohne Verbund":                                 "no NetScope at this address, or a version without federation",
		"Zentrale antwortet mit HTTP %d: %s":                                                           "central instance responds with HTTP %d: %s",
		"Zentrale antwortet mit HTTP %d: kein NetScope unter dieser Adresse oder Version ohne Verbund": "central instance responds with HTTP %d: no NetScope at this address, or a version without federation",
		"kein Zertifikat": "no certificate",
		"Zertifikat der Zentrale passt nicht zum hinterlegten Fingerprint (erhalten %s)": "certificate of the central instance does not match the stored fingerprint (received %s)",
		"Nur ein Standort verbindet sich mit einer Zentrale":                             "Only a site connects to a central instance",
	})
}
