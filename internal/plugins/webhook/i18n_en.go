package webhook

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin info and settings
		"Webhook": "Webhook",
		"Sendet Benachrichtigungen als JSON (NetScope-Payload v1) per HTTP an eine beliebige URL, optional mit HMAC-Signatur.": "Sends notifications as JSON (NetScope payload v1) over HTTP to any URL, optionally with an HMAC signature.",
		"URL": "URL",
		"Empfänger der Benachrichtigungen. Weiterleitungen (3xx) werden nicht verfolgt.": "Receiver of the notifications. Redirects (3xx) are not followed.",
		"HTTP-Methode":       "HTTP method",
		"POST":               "POST",
		"PUT":                "PUT",
		"Zusätzliche Header": "Additional headers",
		"Ein Header pro Zeile im Format „Name: Wert“, z. B. „Authorization: Bearer …“. Die Werte werden nicht verschlüsselt gespeichert und in der UI angezeigt – für ein gemeinsames Geheimnis besser das HMAC-Secret verwenden. Content-Type, User-Agent, Host und X-NetScope-* setzt NetScope selbst.": "One header per line in the format “Name: value”, e.g. “Authorization: Bearer …”. The values are stored unencrypted and shown in the UI – for a shared secret, better use the HMAC secret. NetScope sets Content-Type, User-Agent, Host and X-NetScope-* itself.",
		"HMAC-Secret": "HMAC secret",
		"Signatur":    "Signature",
		"Optional. Wenn gesetzt, trägt jede Anfrage die Header X-NetScope-Timestamp (Unix-Sekunden) und X-NetScope-Signature = „sha256=“ + HEX(HMAC-SHA256(Secret, Timestamp + „.“ + Body)). Empfohlen: mindestens 32 zufällige Zeichen.": "Optional. If set, every request carries the headers X-NetScope-Timestamp (Unix seconds) and X-NetScope-Signature = “sha256=” + HEX(HMAC-SHA256(secret, timestamp + “.” + body)). Recommended: at least 32 random characters.",
		"Timeout":                          "Timeout",
		"Maximale Dauer einer Zustellung.": "Maximum duration of a delivery.",
		"TLS-Zertifikat prüfen":            "Verify TLS certificate",
		"Nur für Empfänger mit selbstsigniertem Zertifikat im eigenen Netz abschalten.": "Turn off only for receivers with a self-signed certificate in your own network.",

		// validation and delivery errors
		"Header-Zeile ohne „Name: Wert“":                                         "header line without “Name: value”",
		"ungültiger Header-Name %q":                                              "invalid header name %q",
		"Header %s: Steuerzeichen im Wert":                                       "header %s: control characters in the value",
		"Header %s wird von NetScope gesetzt und kann nicht konfiguriert werden": "header %s is set by NetScope and cannot be configured",
		"Webhook: %w": "Webhook: %w",
		"Webhook: Payload konnte nicht erzeugt werden: %w": "Webhook: could not build the payload: %w",
		"Webhook zugestellt": "Webhook delivered",
		"Empfänger leitet weiter (HTTP %s) nach %s – bitte die endgültige URL eintragen": "receiver redirects (HTTP %s) to %s – please enter the final URL",
		"Empfänger leitet weiter (HTTP %s) – bitte die endgültige URL eintragen":         "receiver redirects (HTTP %s) – please enter the final URL",
		"Empfänger antwortete mit HTTP %s: %s":                                           "receiver responded with HTTP %s: %s",
		"Empfänger antwortete mit HTTP %s":                                               "receiver responded with HTTP %s",
		"ungültige Ziel-URL (http:// oder https:// erwartet)":                            "invalid target URL (http:// or https:// expected)",
		"Anfrage konnte nicht erstellt werden (URL/Methode prüfen)":                      "could not create the request (check URL/method)",
		"%s: Zeitüberschreitung nach %s":                                                 "%s: timed out after %s",
		"%s nicht erreichbar: %w":                                                        "%s not reachable: %w",
	})
}
