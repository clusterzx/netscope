package ntfy

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin info and settings
		"ntfy": "ntfy",
		"Sendet Push-Benachrichtigungen über ntfy (ntfy.sh oder eigener Server) an ein Topic.": "Sends push notifications through ntfy (ntfy.sh or your own server) to a topic.",
		"Server":                      "Server",
		"Verbindung":                  "Connection",
		"Anmeldung":                   "Authentication",
		"Darstellung":                 "Display",
		"Basis-URL des ntfy-Servers.": "Base URL of the ntfy server.",
		"Topic":                       "Topic",
		"Name des Topics (1–64 Zeichen: Buchstaben, Ziffern, - und _). Auf ntfy.sh ist jedes Topic ohne Zugriffsschutz öffentlich – einen schwer zu erratenden Namen wählen.": "Name of the topic (1–64 characters: letters, digits, - and _). On ntfy.sh every topic without access control is public – choose a name that is hard to guess.",
		"Access-Token": "Access token",
		"Optional: ntfy-Access-Token (tk_…), gesendet als „Authorization: Bearer“. Hat Vorrang vor dem Credential.": "Optional: ntfy access token (tk_…), sent as “Authorization: Bearer”. Takes precedence over the credential.",
		"Benutzer/Passwort": "Username/password",
		"Optional: Zugangsdaten für Basic-Auth, falls kein Token gesetzt ist.": "Optional: credentials for basic auth if no token is set.",
		"Markdown": "Markdown",
		"Nachricht als Markdown senden (Fettschrift, Links). Die ntfy-Web-App stellt Markdown dar; Apps ohne Markdown-Unterstützung zeigen den Text mit Formatierungszeichen.": "Send the message as Markdown (bold, links). The ntfy web app renders Markdown; apps without Markdown support show the text with formatting characters.",
		"Emoji nach Schweregrad": "Emoji by severity",
		"Setzt Tags nach dem höchsten Schweregrad (🚨 kritisch, ⚠️ hoch, 🔶 mittel, 🔷 niedrig, ℹ️ info).": "Sets tags by the highest severity (🚨 critical, ⚠️ high, 🔶 medium, 🔷 low, ℹ️ info).",
		"Timeout":                          "Timeout",
		"Maximale Dauer einer Zustellung.": "Maximum duration of a delivery.",

		// delivery errors
		"ntfy: ungültige Server-URL":                                          "ntfy: invalid server URL",
		"ntfy: kein Topic konfiguriert":                                       "ntfy: no topic configured",
		"ntfy: %w":                                                            "ntfy: %w",
		"ntfy: Anfrage konnte nicht erstellt werden":                          "ntfy: could not create the request",
		"ntfy: %s: Zeitüberschreitung nach %s":                                "ntfy: %s: timed out after %s",
		"ntfy: %s nicht erreichbar: %w":                                       "ntfy: %s not reachable: %w",
		"ntfy-Nachricht gesendet":                                             "ntfy message sent",
		"Server antwortete mit HTTP %s":                                       "server responded with HTTP %s",
		"Server antwortete mit HTTP %s: %s":                                   "server responded with HTTP %s: %s",
		"%w – Token bzw. Zugangsdaten und Schreibrecht auf das Topic prüfen":  "%w – check the token or credentials and the write permission on the topic",
		"%w – Ratenlimit erreicht, später erneut versuchen (Retry-After: %s)": "%w – rate limit reached, try again later (Retry-After: %s)",
		"%w – Ratenlimit erreicht, später erneut versuchen":                   "%w – rate limit reached, try again later",
		"%w – Nachricht zu groß":                                              "%w – message too large",
		"Credential-Zugriff nicht verfügbar":                                  "credential access not available",
		"Credential %q: Benutzername fehlt":                                   "credential %q: username missing",

		// message texts
		"Eskalation – nicht quittiert: %s": "Escalation – not acknowledged: %s",
		"In NetScope öffnen":               "Open in NetScope",
		"Standort %s":                      "Site %s",
		"eskaliert":                        "escalated",
		"quittiert":                        "acknowledged",
		"Öffnen":                           "Open",
		"… und 1 weiteres Ereignis":        "… and 1 more event",
		"… und %d weitere Ereignisse":      "… and %d more events",
		"Alle anzeigen":                    "Show all",
	})
}
