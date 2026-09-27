package agent

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n. The
// install script is left German like the agent itself (it runs on the target host).
func init() {
	i18n.Register(map[string]string{
		// errors of the agent API
		"Installations-Token ungültig, abgelaufen, widerrufen oder aufgebraucht":       "enrollment token invalid, expired, revoked or used up",
		"Agent unbekannt – in NetScope entfernt?":                                      "unknown agent – removed in NetScope?",
		"NetScope-Agents sind in dieser Instanz deaktiviert (Plugin „NetScope-Agent“)": "NetScope agents are disabled in this instance (plugin “NetScope agent”)",
		"noch kein Inventar – Messwerte folgen danach":                                 "no inventory yet – measurements follow afterwards",

		// enrollment tokens
		"1–100 Zeichen":                    "1–100 characters",
		"mindestens 1 (leer = unbegrenzt)": "at least 1 (empty = unlimited)",
		"liegt in der Vergangenheit":       "is in the past",

		// log, agent status
		"Agents prüfen":                            "Check agents",
		"Agent angemeldet":                         "Agent enrolled",
		"Agent-Event":                              "Agent event",
		"Erfassung: %s":                            "Collection: %s",
		"Erfassung lieferte keine Daten":           "Collection returned no data",
		"DHCP-Leases nicht vollständig übernommen": "DHCP leases not completely imported",
		"fehler":                               "errors",
		"Tags des Installations-Tokens setzen": "Set the tags of the enrollment token",
		"Gerät offline setzen":                 "Set device offline",

		// events
		"Agent auf %s meldet sich wieder":            "Agent on %s reporting again",
		"Agent auf %s meldet sich nicht":             "Agent on %s not reporting",
		"%s: %s zu %.0f %% belegt":                   "%s: %s is %.0f%% full",
		"%s: %s wieder unter der Schwelle (%.0f %%)": "%s: %s back below the threshold (%.0f%%)",
	})
}
