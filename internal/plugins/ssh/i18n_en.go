package ssh

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"SSH-Inventar": "SSH inventory",
		"Liest per SSH Betriebssystem, Hardware, Netzwerk, Pakete, Dienste, lauschende Sockets und Docker-Container von Linux-Hosts aus – ausschließlich mit fest definierten Lesekommandos.": "Reads operating system, hardware, network, packages, services, listening sockets and Docker containers from Linux hosts over SSH – using only a fixed set of read-only commands.",
		"Zugangsdaten": "Credentials",
		"Verbindung":   "Connection",
		"Umfang":       "Scope",
		"Sicherheit":   "Security",
		"Leer = automatisch alle Zugangsdaten, deren Geltungsbereich das Gerät abdeckt. Sonst nur die ausgewählten. Probiert wird das spezifischste zuerst (Gerät vor Gruppe/Tag vor Subnetz vor überall); das funktionierende wird pro Gerät gemerkt und beim nächsten Lauf zuerst verwendet.": "Empty = automatically every credential whose scope covers the device. Otherwise only the selected ones. The most specific one is tried first (device before group/tag before subnet before everywhere); the one that works is remembered per device and tried first on the next run.",
		"SSH-Port": "SSH port",
		"Standard-Port für alle Geräte ohne abweichende Angabe.": "Default port for all devices without an override.",
		"Abweichende SSH-Ports": "SSH port overrides",
		"Ein Eintrag pro Zeile: Adresse oder Netz = Port, z. B. 192.168.1.1=2222 oder 10.0.5.0/24=2200. Die spezifischste Angabe gewinnt; solche Geräte werden immer versucht.": "One entry per line: address or network = port, e.g. 192.168.1.1=2222 or 10.0.5.0/24=2200. The most specific entry wins; such devices are always tried.",
		"SSH-Port aus dem Portscan übernehmen": "Use the SSH port found by the port scan",
		"Ist der Standard-Port zu, aber hat nmap auf dem Gerät SSH auf einem anderen Port erkannt (z. B. 2222), wird dieser verwendet.": "If the default port is closed but nmap detected SSH on another port of the device (e.g. 2222), that port is used.",
		"Nur Geräte mit offenem SSH-Port": "Only devices with an open SSH port",
		"Geräte überspringen, deren bekannte offene Ports weder den SSH-Port noch einen erkannten SSH-Dienst enthalten. Geräte ohne Portdaten werden trotzdem versucht.": "Skip devices whose known open ports include neither the SSH port nor a detected SSH service. Devices without port data are still tried.",
		"Zeitlimit pro Kommando": "Timeout per command",
		"Gilt für den Verbindungsaufbau und jedes einzelne Kommando auf dem Zielsystem.": "Applies to establishing the connection and to every single command on the target system.",
		"Installierte Pakete erfassen":                                                        "Collect installed packages",
		"dpkg, rpm oder apk – je nachdem, was vorhanden ist.":                                 "dpkg, rpm or apk – whichever is available.",
		"Docker-Container erfassen":                                                           "Collect Docker containers",
		"Nur wenn docker vorhanden ist und der Benutzer darauf zugreifen darf.":               "Only if docker is installed and the user is allowed to access it.",
		"Hostschlüssel-Prüfung":                                                               "Host key verification",
		"Beim ersten Kontakt merken, danach prüfen (TOFU)":                                    "Remember on first contact, verify afterwards (TOFU)",
		"Nicht prüfen (unsicher)":                                                             "Do not verify (insecure)",
		"Bei TOFU wird ein geänderter Hostschlüssel abgelehnt (möglicher Man-in-the-Middle).": "With TOFU a changed host key is rejected (possible man-in-the-middle).",
		"Maximale Ausgabe pro Gerät (MB)":                                                     "Maximum output per device (MB)",
		"Längere Ausgaben werden abgeschnitten; unvollständige Paket- oder Containerlisten werden dann nicht übernommen.": "Longer output is truncated; incomplete package or container lists are then not imported.",

		// settings validation
		"%q: Adresse=Port erwartet, z. B. 192.168.1.1=2222": "%q: address=port expected, e.g. 192.168.1.1=2222",
		"%q: Port 1–65535 erwartet":                         "%q: port 1–65535 expected",
		"%q: ungültiges Netz":                               "%q: invalid network",
		"%q: ungültige IP-Adresse":                          "%q: invalid IP address",

		// run log, run errors
		"Subnetze nicht lesbar – es werden keine weiteren IP-Adressen übernommen": "Cannot read subnets – no further IP addresses are imported",
		"gemerkte Credentials nicht lesbar – beginne neu":                         "cannot read remembered credentials – starting over",
		"gemerkte Credentials nicht speicherbar":                                  "cannot save remembered credentials",
		"Geräte ohne offenen SSH-Port übersprungen":                               "Skipped devices without an open SSH port",
		"Geräte ohne passende Zugangsdaten übersprungen":                          "Skipped devices without matching credentials",
		"kein passendes Credential":                                               "no matching credential",
		"SSH-Inventar fehlgeschlagen":                                             "SSH inventory failed",
		"SSH-Inventar abgeschlossen":                                              "SSH inventory finished",
		"kein Gerät per SSH inventarisiert (%d fehlgeschlagen)":                   "no device inventoried over SSH (%d failed)",
		"für keines der %d Geräte gibt es passende Zugangsdaten: %w":              "none of the %d devices has matching credentials: %w",
		"Anmeldung abgelehnt für %s":                                              "login rejected for %s",
		"Inventar-Skript: %w":                                                     "inventory script: %w",
		"Inventar-Skript lieferte keine Daten (Exit-Code %d): %s":                 "inventory script returned no data (exit code %d): %s",
		"Ausgabe gekürzt – Limit erreicht":                                        "Output truncated – limit reached",
		"Abschnitt fehlerhaft":                                                    "Section failed",
		"Beobachtung speichern: %w":                                               "save observation: %w",

		// inventory section errors
		"Ausgabe gekürzt (Limit erreicht)": "output truncated (limit reached)",
		"unvollständige Ausgabe":           "incomplete output",
		"Zeitüberschreitung":               "Timeout",
		"keine Einträge":                   "no entries",
		"ungültige Ausgabe %q":             "invalid output %q",
		"%d Zeile(n) nicht lesbar":         "%d unreadable line(s)",
		"Exit-Code %d":                     "exit code %d",
		"leere Ausgabe":                    "empty output",
		"ungültige Zeit %q":                "invalid time %q",
		"ungültige Uptime %q":              "invalid uptime %q",
		"ungültiger Zeitstempel %q":        "invalid timestamp %q",
		"MemTotal fehlt":                   "MemTotal missing",
		"lsblk-JSON: %w":                   "lsblk JSON: %w",
		"ip-JSON: %w":                      "ip JSON: %w",
		"kein Start-Date":                  "no Start-Date",
		"ungültiges Datum %q":              "invalid date %q",
		"unbekanntes Datumsformat %q":      "unknown date format %q",
		"Angemeldet als %s (%s) – %s":      "Signed in as %s (%s) – %s",
	})
}
