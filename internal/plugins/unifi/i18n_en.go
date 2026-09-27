package unifi

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"UniFi Network": "UniFi Network",
		"Liest Clients und Netzwerkgeräte aus UniFi-Controllern (UniFi-OS-Konsolen oder selbst gehosteter Controller): Namen, feste IPs, VLAN, Switch-Port bzw. Access Point – für die Topologie.": "Reads clients and network devices from UniFi controllers (UniFi OS consoles or self-hosted controllers): names, fixed IPs, VLAN, switch port or access point – for the topology.",
		"Controller": "Controllers",
		"URL der UniFi-Konsole (https://udm.lan) oder des Controllers (https://controller:8443), einer pro Zeile.":                                                                                                                                                                                                                                                          "URL of the UniFi console (https://udm.lan) or the controller (https://controller:8443), one per line.",
		"Benutzer/Passwort eines lokalen UniFi-Kontos ohne MFA (volle Daten: Ports, VLAN, bekannte Offline-Geräte) oder ein API-Token mit dem API-Schlüssel aus Einstellungen → Control Plane → Integrations (nur verbundene Clients, wenige Details).":                                                                                                                     "Username/password of a local UniFi account without MFA (full data: ports, VLAN, known offline devices) or an API token with the API key from Settings → Control Plane → Integrations (connected clients only, few details).",
		"Benutzer/Passwort eines lokalen UniFi-Kontos ohne MFA (volle Daten: Ports, VLAN, bekannte Offline-Geräte) oder ein API-Token mit dem API-Schlüssel aus Einstellungen → Control Plane → Integrations (nur verbundene Clients, wenige Details). Leer = automatisch die passenden je Gerät nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen.": "Username/password of a local UniFi account without MFA (full data: ports, VLAN, known offline devices) or an API token with the API key from Settings → Control Plane → Integrations (connected clients only, few details). Empty = automatically the matching ones per device according to the credential's scope; rejected ones are skipped.",
		"Sites": "Sites",
		"Kurzname der Sites (in der URL …/site/<name>/); * liest alle.": "Short name of the sites (in the URL …/site/<name>/); * reads all.",
		"Bekannte Clients ohne Verbindung":                              "Known disconnected clients",
		"Auch Clients übernehmen, die der Controller kennt, die aber gerade nicht verbunden sind (Namen, feste IPs). Nur mit Benutzer/Passwort.": "Also import clients the controller knows but that are not currently connected (names, fixed IPs). Only with username/password.",
		"UniFi-Geräte übernehmen": "Import UniFi devices",
		"Access Points, Switches und Gateways selbst (Modell, Name, Typ); nötig, damit Clients ihrem Access Point bzw. Switch zugeordnet werden.": "The access points, switches and gateways themselves (model, name, type); needed so that clients are assigned to their access point or switch.",

		// run log, run errors
		"das UniFi-Konto verlangt MFA – ein lokales Konto ohne MFA verwenden": "the UniFi account requires MFA – use a local account without MFA",
		"Controller nicht erreichbar: %w":                                     "Controller not reachable: %w",
		"keine der Sites %s gefunden (vorhanden: %s)":                         "none of the sites %s found (available: %s)",
		"keine der Sites %s gefunden":                                         "none of the sites %s found",
		"Geräte: %w":                                                          "Devices: %w",
		"Netzwerke nicht lesbar – VLAN-Namen fehlen":                          "Cannot read networks – VLAN names missing",
		"Bekannte Clients nicht lesbar":                                       "Cannot read known clients",
		"die Integration-API gibt es nur auf UniFi-OS-Konsolen (Network 9 oder neuer) – für selbst gehostete Controller Benutzer/Passwort verwenden": "the Integration API is only available on UniFi OS consoles (Network 9 or later) – use username/password for self-hosted controllers",
	})
}
