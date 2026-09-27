package arpscan

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"ARP-Scan": "ARP scan",
		"Findet aktive Geräte per ARP (arp-scan) in direkt angeschlossenen Netzen und bestimmt ihre Anwesenheit.": "Finds active devices via ARP (arp-scan) in directly attached networks and determines their presence.",
		"Versuche pro Adresse": "Attempts per address",
		"Wie oft eine Adresse ohne Antwort erneut angefragt wird (arp-scan --retry, Gesamtzahl der Versuche).": "How often an address without an answer is queried again (arp-scan --retry, total number of attempts).",
		"Timeout pro Adresse (ms)": "Timeout per address (ms)",
		"Wartezeit auf die erste Antwort je Adresse in Millisekunden (arp-scan --timeout).": "Time to wait for the first answer per address, in milliseconds (arp-scan --timeout).",
		"Sendebandbreite": "Send bandwidth",
		"Maximale Sendebandbreite in Bit/s, z. B. 256K oder 1M (arp-scan --bandwidth). Leer = Standard von arp-scan.": "Maximum send bandwidth in bit/s, e.g. 256K or 1M (arp-scan --bandwidth). Empty = arp-scan's default.",
		"Ignorierte MAC-Adressen": "Ignored MAC addresses",
		"Antworten dieser MAC-Adressen werden verworfen (eine pro Zeile).": "Answers from these MAC addresses are discarded (one per line).",

		// targets
		"kein lokales Interface für %s gefunden – arp-scan erreicht nur direkt angeschlossene Netze": "no local interface found for %s – arp-scan only reaches directly attached networks",
		"%d Geräte über %s":                                 "%d devices via %s",
		"Subnetz %s: arp-scan unterstützt nur IPv4":         "subnet %s: arp-scan only supports IPv4",
		"Subnetz %s ist für arp-scan zu groß (maximal /16)": "subnet %s is too large for arp-scan (at most /16)",
		"Subnetz %s: kein lokales Interface gefunden – arp-scan erreicht nur direkt angeschlossene Netze (Interface eintragen oder das Subnetz als „über Router“ erreichbar markieren)": "subnet %s: no local interface found – arp-scan only reaches directly attached networks (enter an interface or mark the subnet as reachable “via router”)",

		// run log, run errors
		"Keine Subnetze im Scope – scanne die lokal angeschlossenen Netze":            "No subnets in scope – scanning the locally attached networks",
		"Keine Geräte mit IP-Adresse im Scope":                                        "No devices with an IP address in scope",
		"geroutete Subnetze übersprungen":                                             "skipped routed subnets",
		"Nur geroutete Subnetze im Scope – ARP-Scans sind dort nicht möglich":         "Only routed subnets in scope – ARP scans are not possible there",
		"keine scanbaren Netze im Scope":                                              "no scannable networks in scope",
		"arp-scan fehlgeschlagen":                                                     "arp-scan failed",
		"arp-scan abgeschlossen":                                                      "arp-scan finished",
		"arp-scan-Zeile nicht erkannt":                                                "arp-scan line not recognised",
		"Möglicher IP-Adresskonflikt: mehrere MAC-Adressen antworten auf dieselbe IP": "Possible IP address conflict: several MAC addresses answer for the same IP",
		"Doppelte ARP-Antwort ignoriert":                                              "Ignored duplicate ARP answer",
		"MAC-Adresse wird ignoriert":                                                  "Ignoring MAC address",
		"Lokaler Host nicht ermittelt":                                                "Could not determine the local host",
		"Ergebnis konnte nicht gespeichert werden":                                    "Could not save result",
		"lokaler Host auf %s: %s":                                                     "local host on %s: %s",
		"arp-scan darf auf %s keine Raw-Sockets öffnen – NetScope muss als root bzw. mit den Capabilities NET_RAW und NET_ADMIN laufen (Docker: cap_add: [NET_RAW, NET_ADMIN]): %w": "arp-scan may not open raw sockets on %s – NetScope must run as root or with the capabilities NET_RAW and NET_ADMIN (Docker: cap_add: [NET_RAW, NET_ADMIN]): %w",
		"Interface %s existiert nicht: %w": "interface %s does not exist: %w",
	})
}
