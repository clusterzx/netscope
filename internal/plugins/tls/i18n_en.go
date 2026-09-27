package tls

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"TLS-Zertifikate": "TLS certificates",
		"Erfasst Zertifikate (CN/SAN, Aussteller, Gültigkeit, selbstsigniert) und die TLS-Konfiguration je Port. SSLv3 kann nicht geprüft werden.": "Records certificates (CN/SAN, issuer, validity, self-signed) and the TLS configuration per port. SSLv3 cannot be checked.",
		"Bekannte Ports verwenden": "Use known ports",
		"TLS-Ports aus den gescannten Diensten übernehmen (tunnel ssl, https, imaps, …).": "Take TLS ports from the scanned services (tunnel ssl, https, imaps, …).",
		"Zusätzliche Ports":                 "Additional ports",
		"Timeout je Verbindung":             "Timeout per connection",
		"SNI (Servername)":                  "SNI (server name)",
		"Hostname des Geräts senden":        "Send the device's host name",
		"Kein SNI":                          "No SNI",
		"Schwache Protokolle/Cipher prüfen": "Check weak protocols/ciphers",
		"Testet unterstützte TLS-Versionen und unsichere Cipher (zusätzliche Handshakes).": "Tests supported TLS versions and insecure ciphers (additional handshakes).",
		"STARTTLS verwenden": "Use STARTTLS",
		"STARTTLS für SMTP (25/587), IMAP (143), POP3 (110) und FTP (21).": "STARTTLS for SMTP (25/587), IMAP (143), POP3 (110) and FTP (21).",

		// validation, run log, probe errors
		"ungültiger Port %s":                "invalid port %s",
		"keine Geräte für TLS-Scan":         "no devices for TLS scan",
		"kein TLS":                          "no TLS",
		"Beobachtung fehlgeschlagen":        "Observation failed",
		"kein Zertifikat geliefert":         "no certificate presented",
		"unbekanntes STARTTLS-Protokoll %q": "unknown STARTTLS protocol %q",
		"unerwartete Antwort %q":            "unexpected response %q",
		"unbekannt":                         "unknown",
	})
}
