package agents

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"NetScope-Agent": "NetScope agent",
		"Linux- und Windows-Systeme mit installiertem NetScope-Agent liefern Inventar (wie per SSH) und Auslastung (CPU, RAM, Platten, Netz) von sich aus – ohne SSH-Zugang, auch hinter NAT; Windows-DHCP-Server zusätzlich ihre Leases. Installation unter Agents.": "Linux and Windows systems with the NetScope agent installed deliver inventory (as over SSH) and utilisation (CPU, RAM, disks, network) on their own – without SSH access, even behind NAT; Windows DHCP servers also deliver their leases. Installation under Agents.",
		"Erfassung":           "Collection",
		"Warnungen":           "Alerts",
		"Windows-DHCP-Server": "Windows DHCP server",
		"Inventar alle":       "Inventory every",
		"Wie oft jeder Agent das volle Inventar liefert (Pakete, Dienste, Ports, Docker). „Jetzt ausführen“ fordert es sofort an.": "How often each agent delivers the full inventory (packages, services, ports, Docker). “Run now” requests it immediately.",
		"Messung alle": "Measure every",
		"Abstand der Auslastungsmessungen (CPU, RAM, Platten, Netz).": "Interval between utilisation measurements (CPU, RAM, disks, network).",
		"Messwerte senden alle": "Send measurements every",
		"Die Messungen werden gesammelt übertragen; so oft meldet sich der Agent mindestens.": "Measurements are sent in batches; the agent reports at least this often.",
		"Installierte Pakete erfassen":    "Collect installed packages",
		"Grundlage für den CVE-Abgleich.": "Basis for CVE matching.",
		"Docker-Container erfassen":       "Collect Docker containers",
		"Nur wenn der Agent mit --docker installiert wurde (Zugriff auf den Docker-Socket).": "Only if the agent was installed with --docker (access to the Docker socket).",
		"Dateisystem voll ab (%)": "File system full at (%)",
		"Belegung, ab der ein Event „Dateisystem fast voll“ entsteht (0 = aus).": "Usage at which a “File system almost full” event is raised (0 = off).",
		"Agent gilt als weg nach": "Agent counts as gone after",
		"Ohne Meldung so lange entsteht „Agent meldet sich nicht“; scannt kein anderer Scanner das Gerät, geht es offline.": "After this long without a report, “Agent not reporting” is raised; if no other scanner scans the device, it goes offline.",
		"Leases übernehmen": "Import leases",
		"Agents auf Windows-DHCP-Servern liefern Leases und Reservierungen: Namen und Adressen der Geräte im Netz.": "Agents on Windows DHCP servers deliver leases and reservations: names and addresses of the devices on the network.",
		"Fehlende Geräte anlegen": "Create missing devices",
		"Geräte aus Leases anlegen, die noch kein Scan gefunden hat – nützlich für Netze, die NetScope nicht selbst scannt. Aus: nur bekannte Geräte ergänzen.": "Create devices from leases that no scan has found yet – useful for networks that NetScope does not scan itself. Off: only add to known devices.",

		// settings validation
		"mindestens 5 Minuten":             "at least 5 minutes",
		"mindestens 10 Sekunden":           "at least 10 seconds",
		"mindestens 1 Minute":              "at least 1 minute",
		"nicht kürzer als der Messabstand": "not shorter than the measurement interval",

		// run
		"Agent-Dienst läuft nicht":            "agent service is not running",
		"Inventar bei den Agents angefordert": "Requested inventory from the agents",
	})
}
