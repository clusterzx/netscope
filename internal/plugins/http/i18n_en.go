package http

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"HTTP-Fingerprinting": "HTTP fingerprinting",
		"Untersucht HTTP(S)-Ports: Titel, Server-Header, Redirects, Favicon-Hash und erkennt Web-Anwendungen.": "Examines HTTP(S) ports: title, server header, redirects, favicon hash, and detects web applications.",
		"Bekannte Ports verwenden": "Use known ports",
		"HTTP(S)-Ports aus den bereits gescannten Diensten des Geräts übernehmen.": "Take HTTP(S) ports from the device's already scanned services.",
		"Zusätzliche Ports": "Additional ports",
		"Diese Ports werden immer geprüft, auch ohne Portdaten.": "These ports are always checked, even without port data.",
		"Timeout je Anfrage":       "Timeout per request",
		"Maximale Weiterleitungen": "Maximum redirects",
		"User-Agent":               "User-Agent",
		"Favicon-Hash berechnen":   "Compute favicon hash",
		"Lädt das Favicon und bildet den Shodan-kompatiblen mmh3-Hash.": "Downloads the favicon and computes the Shodan-compatible mmh3 hash.",
		"Maximale Body-Größe (KB)":                                      "Maximum body size (KB)",
		"Eigene Signaturen":                                             "Custom signatures",
		"Je Zeile \"Name|Feld|Regex\". Feld: title, server, body, header:<Name> oder favicon (mmh3-Zahl).": "One \"Name|Field|Regex\" per line. Field: title, server, body, header:<Name> or favicon (mmh3 number).",

		// validation, custom signatures
		"ungültiger Port %q":                       "invalid port %q",
		"Format \"Name|Feld|Regex\" erwartet":      "Format \"Name|Field|Regex\" expected",
		"Name fehlt":                               "Name missing",
		"favicon erwartet einen mmh3-Ganzzahlwert": "favicon expects an mmh3 integer value",
		"ungültiger regulärer Ausdruck: %w":        "invalid regular expression: %w",
		"Header-Name fehlt":                        "Header name missing",
		"unbekanntes Feld %q (title, server, body, header:<Name>, favicon)": "unknown field %q (title, server, body, header:<Name>, favicon)",

		// run log
		"keine Geräte für HTTP-Scan": "no devices for HTTP scan",
		"Beobachtung fehlgeschlagen": "Observation failed",
		"Redirect-Limit erreicht":    "Redirect limit reached",
		"nicht-http Weiterleitung":   "non-HTTP redirect",
	})
}
