package nmapudp

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"Nmap (UDP)": "Nmap (UDP)",
		"Seltener UDP-Scan der häufigsten Ports bekannter Geräte (langsam, daher standardmäßig wöchentlich).": "Occasional UDP scan of the most common ports of known devices (slow, hence weekly by default).",
		"Anzahl Top-Ports": "Number of top ports",
		"Wie viele der häufigsten UDP-Ports gescannt werden (1–1000).": "How many of the most common UDP ports are scanned (1–1000).",
		"Timing":                "Timing",
		"T0 – paranoid":         "T0 – paranoid",
		"T1 – heimlich":         "T1 – sneaky",
		"T2 – höflich":          "T2 – polite",
		"T3 – normal":           "T3 – normal",
		"T4 – aggressiv":        "T4 – aggressive",
		"T5 – wahnsinnig":       "T5 – insane",
		"Diensterkennung (-sV)": "Service detection (-sV)",
		"UDP-Diensterkennung ist langsam und wird selten benötigt.":           "UDP service detection is slow and rarely needed.",
		"open|filtered aufnehmen":                                             "Include open|filtered",
		"Auch Ports melden, deren Zustand nicht sicher ist (bei UDP häufig).": "Also report ports whose state is uncertain (common with UDP).",
		"Host-Timeout": "Host timeout",

		// run log, run errors
		"keine Geräte für den UDP-Scan": "no devices for the UDP scan",
		"UDP-Scan fehlgeschlagen":       "UDP scan failed",
		"Beobachtung fehlgeschlagen":    "Observation failed",
		"UDP-Scan im Timeout":           "UDP scan timed out",
	})
}
