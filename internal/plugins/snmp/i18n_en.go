package snmp

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin snmp, settings
		"SNMP": "SNMP",
		"Fragt Geräte per SNMP v2c/v3 ab: Systemdaten, Interfaces, ARP-Tabelle, Bridge-FDB (MAC → Port) und LLDP-Nachbarn als Grundlage für die Topologie.": "Queries devices over SNMP v2c/v3: system data, interfaces, ARP table, bridge FDB (MAC → port) and LLDP neighbours as the basis for the topology.",
		"Zugangsdaten": "Credentials",
		"Verbindung":   "Connection",
		"Umfang":       "Scope",
		"Community (v2c) oder USM-Benutzer (v3). Leer = automatisch alle, deren Geltungsbereich das Gerät abdeckt. Probiert wird das spezifischste zuerst; das funktionierende wird pro Gerät gemerkt.": "Community (v2c) or USM user (v3). Empty = automatically every credential whose scope covers the device. The most specific one is tried first; the one that works is remembered per device.",
		"UDP-Port":                "UDP port",
		"Zeitlimit pro Anfrage":   "Timeout per request",
		"Wiederholungen":          "Retries",
		"GETBULK max-repetitions": "GETBULK max-repetitions",
		"Wiederholungen je Anfrage, wenn keine Antwort kommt.":                                         "Retries per request when no answer arrives.",
		"Einträge pro GETBULK-Anfrage. Kleinere Werte helfen bei langsamen oder fehlerhaften Agenten.": "Entries per GETBULK request. Smaller values help with slow or buggy agents.",
		"Interfaces abfragen": "Query interfaces",
		"IF-MIB: Name, Beschreibung, Alias, Typ, MAC, Geschwindigkeit, Status.": "IF-MIB: name, description, alias, type, MAC, speed, status.",
		"ARP-Tabelle abfragen": "Query ARP table",
		"Bridge-FDB abfragen":  "Query bridge FDB",
		"MAC-Adresstabelle der Switches (BRIDGE-MIB / Q-BRIDGE-MIB) – Grundlage für „Gerät → Switch-Port“.": "MAC address table of the switches (BRIDGE-MIB / Q-BRIDGE-MIB) – the basis for “device → switch port”.",
		"LLDP-Nachbarn abfragen":           "Query LLDP neighbours",
		"MACs aus ARP-Tabellen übernehmen": "Import MACs from ARP tables",
		"Bekannte Geräte, die nur per IP bekannt sind, lernen ihre MAC aus der ARP-Tabelle (nur Adressen in konfigurierten Subnetzen; es werden keine Geräte angelegt).": "Devices known only by their IP learn their MAC from the ARP table (only addresses in configured subnets; no devices are created).",

		// plugin snmp_traffic, settings
		"SNMP-Traffic": "SNMP traffic",
		"Misst per SNMP den Datenverkehr, die Auslastung, Fehler und Verwürfe je Interface (IF-MIB) und meldet Port-Ausfälle und überlastete Ports.": "Measures traffic, utilisation, errors and discards per interface over SNMP (IF-MIB) and reports port outages and saturated ports.",
		"Interfaces": "Interfaces",
		"Physische Ports, WLAN und Link-Aggregationen":                                    "Physical ports, Wi-Fi and link aggregations",
		"Alle, die gerade verbunden sind":                                                 "All that are currently connected",
		"Alle (auch VLAN- und virtuelle Interfaces)":                                      "All (including VLAN and virtual interfaces)",
		"Welche Interfaces gemessen werden; abgeschaltete (admin down) nie.":              "Which interfaces are measured; disabled ones (admin down) never are.",
		"Interfaces auslassen":                                                            "Exclude interfaces",
		"Namen mit Platzhaltern, z. B. vlan* oder Loopback*; Groß-/Kleinschreibung egal.": "Names with wildcards, e.g. vlan* or Loopback*; case-insensitive.",
		"Überlastet ab (%)":                                                               "Saturated from (%)",
		"Events":                                                                          "Events",
		"Event, wenn ein Port in eine Richtung mindestens so stark ausgelastet ist (0 = aus).": "Event when a port reaches at least this utilisation in one direction (0 = off).",
		"Port-Ausfälle melden": "Report port outages",
		"Event, wenn ein eingeschalteter Port die Verbindung verliert oder wiederbekommt.": "Event when an enabled port loses or regains its link.",
		"Nur Ports mit Beschreibung": "Only ports with a description",
		"Nur Ports mit Beschreibung (ifAlias) melden – typischerweise Uplinks und Server; Arbeitsplätze, die abends ausgehen, bleiben still.": "Only report ports with a description (ifAlias) – typically uplinks and servers; workstations that are switched off in the evening stay quiet.",

		// credentials
		"Credential %q: Community fehlt":                           "credential %q: community missing",
		"Credential %q: Benutzername fehlt":                        "credential %q: username missing",
		"Credential %q: unbekanntes Auth-Protokoll %q":             "credential %q: unknown auth protocol %q",
		"Credential %q: Auth-Passwort fehlt":                       "credential %q: auth password missing",
		"Credential %q: unbekanntes Privacy-Protokoll %q":          "credential %q: unknown privacy protocol %q",
		"Credential %q: Privacy-Passwort fehlt":                    "credential %q: privacy password missing",
		"Credential %q: unbekannte Sicherheitsstufe %q":            "credential %q: unknown security level %q",
		"Credential %q hat Typ %s, erwartet snmp_v2c oder snmp_v3": "credential %q has type %s, expected snmp_v2c or snmp_v3",
		"SNMP-Fehler %s": "SNMP error %s",

		// run log, run errors
		"Subnetze nicht lesbar – ARP-Nachbarn werden nicht übernommen": "Cannot read subnets – ARP neighbours are not imported",
		"gemerkte Credentials nicht lesbar – beginne neu":              "cannot read remembered credentials – starting over",
		"gemerkte Credentials nicht speicherbar":                       "cannot save remembered credentials",
		"Zählerstände nicht speicherbar":                               "Cannot save counter readings",
		"keine SNMP-Antwort":                                           "no SNMP response",
		"kein passendes Credential":                                    "no matching credential",
		"SNMP-Abfrage abgeschlossen":                                   "SNMP query finished",
		"SNMP-Tabelle nicht lesbar":                                    "Cannot read SNMP table",
		"ARP-Nachbar nicht übernommen":                                 "ARP neighbour not imported",
		"Event konnte nicht gespeichert werden":                        "Could not save event",
		"kein Gerät hat per SNMP geantwortet (%d ohne Antwort)":        "no device answered over SNMP (%d without response)",
		"für keines der %d Geräte gibt es passende Zugangsdaten: %w":   "none of the %d devices has matching credentials: %w",
		"Beobachtung speichern: %w":                                    "save observation: %w",
		"Messwerte speichern: %w":                                      "save metrics: %w",
		"Interface-Tabelle: %s":                                        "interface table: %s",

		// events
		"%s auf %s zu %.0f %% ausgelastet":                                 "%s on %s at %.0f%% utilisation",
		"%s %s bei %s Mbit/s Portgeschwindigkeit.":                         "%s %s at %s Mbit/s port speed.",
		"%s eingehend bei %s Mbit/s Portgeschwindigkeit.":                  "%s inbound at %s Mbit/s port speed.",
		"%s ausgehend bei %s Mbit/s Portgeschwindigkeit.":                  "%s outbound at %s Mbit/s port speed.",
		"%s auf %s hat keine Verbindung mehr":                              "%s on %s is no longer connected",
		"Port-Status: %s (vorher up).":                                     "Port status: %s (previously up).",
		"%s auf %s ist wieder verbunden":                                   "%s on %s is connected again",
		"Port-Status: up (vorher %s).":                                     "Port status: up (previously %s).",
		"Antwort von %s (SNMP %s, %s)":                                     "Answer from %s (SNMP %s, %s)",
		"keine SNMP-Antwort – Adresse, Community bzw. Benutzer prüfen: %w": "no SNMP answer – check address, community or user: %w",
	})
}
