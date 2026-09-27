package main

import "netscope/internal/i18n"

// English texts of the CLI (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		usageText:               usageEN,
		"unbekannter Befehl %q": "unknown command %q",
		"Fehler:":               "Error:",
		"verwende: netscope token create --name NAME --scope read|write [--ttl 24h]": "usage: netscope token create --name NAME --scope read|write [--ttl 24h]",
		"Name des Tokens":             "name of the token",
		"read oder write":             "read or write",
		"Gültigkeit (0 = unbegrenzt)": "validity (0 = unlimited)",
		"kein Benutzer vorhanden – Server einmal starten: %w": "no user exists – start the server once: %w",
		"neues Passwort (sonst über stdin)":                   "new password (otherwise via stdin)",
		"Neues Passwort: ":                                    "New password: ",
		"Passwort geändert, alle Sitzungen beendet.":          "Password changed, all sessions ended.",
		"Zweiter Faktor entfernt, alle Sitzungen beendet. Verlangt die Rolle 2FA, wird sie beim nächsten Login neu eingerichtet.": "Second factor removed, all sessions ended. If the role requires 2FA, it is set up again at the next login.",
		"Name des Benutzers": "user name",
		"Status %d":          "status %d",
	})
}

// usageEN is the English version of usageText (main.go).
const usageEN = `NetScope – network inventory and asset monitoring

Commands:
  serve                                   start the server (default)
  token create --name NAME --scope read|write [--ttl 24h]
                                          create an API token (printed only once)
  passwd [--user NAME] [--password PASSWORD]
                                          reset a password (default admin; otherwise read from stdin)
  2fa-reset [--user NAME]                 remove the second factor (TOTP, passkeys, codes) of a user
  healthcheck                             check /api/v1/health of the local server
  openapi                                 print the OpenAPI specification
  version                                 print the version

Configuration: /data/config.yaml or NETSCOPE_CONFIG, overrides via NETSCOPE_*.
`
