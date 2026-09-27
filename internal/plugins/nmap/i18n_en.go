package nmap

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"Nmap (TCP)": "Nmap (TCP)",
		"Scannt TCP-Ports, erkennt Dienste, Versionen und Betriebssystem und liest CPE-Kennungen aus.": "Scans TCP ports, detects services, versions and the operating system, and reads CPE identifiers.",
		"Ports": "Ports",
		"\"top-N\" (häufigste Ports), \"all\" (alle 65535) oder eine Portliste wie 22,80,443,8000-8100.": "\"top-N\" (most common ports), \"all\" (all 65535) or a port list such as 22,80,443,8000-8100.",
		"Timing":                                         "Timing",
		"T0 – paranoid (sehr langsam)":                   "T0 – paranoid (very slow)",
		"T1 – heimlich":                                  "T1 – sneaky",
		"T2 – höflich":                                   "T2 – polite",
		"T3 – normal":                                    "T3 – normal",
		"T4 – aggressiv (empfohlen im LAN)":              "T4 – aggressive (recommended on a LAN)",
		"T5 – wahnsinnig (unzuverlässig)":                "T5 – insane (unreliable)",
		"Diensterkennung (-sV)":                          "Service detection (-sV)",
		"Ermittelt Produkt und Version je offenem Port.": "Determines product and version for each open port.",
		"Erkennungsintensität":                           "Detection intensity",
		"0 (schnell) bis 9 (gründlich). Nur bei aktiver Diensterkennung.": "0 (fast) to 9 (thorough). Only with service detection enabled.",
		"OS-Erkennung (-O)": "OS detection (-O)",
		"Rät das Betriebssystem (--osscan-guess). Benötigt Root-Rechte im Container.": "Guesses the operating system (--osscan-guess). Requires root privileges in the container.",
		"Mindestgenauigkeit OS": "Minimum OS accuracy",
		"OS-Treffer unter dieser Genauigkeit (0–100) werden verworfen.": "OS matches below this accuracy (0–100) are discarded.",
		"Host-Timeout": "Host timeout",
		"Bricht den Scan eines einzelnen Hosts nach dieser Dauer ab.": "Aborts the scan of a single host after this duration.",
		"Host-Erkennung":                       "Host discovery",
		"Ping-Scan (nmap -sn) vorab":           "Ping scan (nmap -sn) first",
		"Nur bekannte Geräte des Scopes (-Pn)": "Only known devices in scope (-Pn)",
		"Wie die zu scannenden Hosts bestimmt werden. Bei Geräte-Scope immer nur die Geräte.": "How the hosts to scan are determined. With a device scope always just the devices.",
		"Ausschlüsse": "Exclusions",
		"Adressen oder Subnetze (CIDR), die nicht gescannt werden.": "Addresses or subnets (CIDR) that are not scanned.",

		// port specification
		"Portangabe fehlt": "Port specification missing",
		"ungültige Portangabe %q: top-N erwartet mit 1–65535": "invalid port specification %q: top-N expected with 1–65535",
		"ungültige Portangabe %q: leerer Eintrag":             "invalid port specification %q: empty entry",
		"ungültiger Portbereich %q":                           "invalid port range %q",
		"ungültiger Port %q":                                  "invalid port %q",
		"Port außerhalb 1–65535":                              "Port outside 1–65535",

		// run log, run errors
		"nmap läuft ohne Root-Rechte – Fallback auf TCP-Connect-Scan (-sT), keine OS-Erkennung": "nmap runs without root privileges – falling back to TCP connect scan (-sT), no OS detection",
		"keine Ziele für den Scan":             "no targets to scan",
		"Host-Scan fehlgeschlagen":             "Host scan failed",
		"Beobachtung fehlgeschlagen":           "Observation failed",
		"Discovery-Beobachtung fehlgeschlagen": "Discovery observation failed",
		"Host-Erkennung: %w":                   "Host discovery: %w",
		"Host-Erkennung abgeschlossen":         "Host discovery finished",
		"Host-Scan im Timeout":                 "Host scan timed out",
	})
}
