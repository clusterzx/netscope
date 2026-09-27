package sshx

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		"SSH-Hostschlüssel hat sich geändert (möglicher Man-in-the-Middle) – Eintrag in known_hosts prüfen": "SSH host key has changed (possible man-in-the-middle) – check the entry in known_hosts",
		"Credential %q: Benutzername fehlt":              "credential %q: username missing",
		"Credential %q: privater Schlüssel ungültig: %w": "credential %q: invalid private key: %w",
		"Credential %q: weder Schlüssel noch Passwort":   "credential %q: neither key nor password",
		"Anmeldung abgelehnt für %s":                     "login rejected for %s",
	})
}
