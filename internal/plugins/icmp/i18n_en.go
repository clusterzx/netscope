package icmp

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"ICMP-Ping": "ICMP ping",
		"Misst Latenz und Paketverlust aller Geräte per Ping und speichert sie als Zeitreihe.": "Measures latency and packet loss of all devices via ping and stores them as a time series.",
		"Pakete pro Gerät":                                                          "Packets per device",
		"Anzahl der Echo-Requests je Adresse und Lauf.":                             "Number of echo requests per address and run.",
		"Abstand zwischen Paketen":                                                  "Interval between packets",
		"Pause zwischen zwei Echo-Requests an dieselbe Adresse.":                    "Pause between two echo requests to the same address.",
		"Wartezeit auf Antworten":                                                   "Wait time for replies",
		"Wie lange nach dem letzten Echo-Request noch auf Antworten gewartet wird.": "How long to keep waiting for replies after the last echo request.",
		"Nutzdatengröße (Byte)":                                                     "Payload size (bytes)",
		"Größe der ICMP-Nutzdaten (mindestens 24 Byte).":                            "Size of the ICMP payload (at least 24 bytes).",
		"Alle IP-Adressen pingen":                                                   "Ping all IP addresses",
		"Aus: nur die primäre IP eines Geräts. An: jede bekannte IP-Adresse (eigene Zeitreihe je IP).": "Off: only a device's primary IP. On: every known IP address (separate time series per IP).",
		"Raw-Sockets verwenden": "Use raw sockets",
		"Privilegierter ICMP-Modus (root bzw. NET_RAW). Aus: unprivilegierte ICMP-Datagramm-Sockets, erfordert passendes net.ipv4.ping_group_range.": "Privileged ICMP mode (root or NET_RAW). Off: unprivileged ICMP datagram sockets, requires a suitable net.ipv4.ping_group_range.",

		// run log, run errors
		"Keine Geräte mit IP-Adresse im Scope":     "No devices with an IP address in scope",
		"Ping fehlgeschlagen":                      "Ping failed",
		"Keine Echo-Requests gesendet":             "No echo requests sent",
		"Ergebnis konnte nicht gespeichert werden": "Could not save result",
		"kein Ping möglich – für ICMP braucht NetScope root bzw. die Capability NET_RAW (oder „Raw-Sockets verwenden“ aus und passendes net.ipv4.ping_group_range): %w": "ping not possible – for ICMP NetScope needs root or the NET_RAW capability (or “Use raw sockets” off and a suitable net.ipv4.ping_group_range): %w",
	})
}
