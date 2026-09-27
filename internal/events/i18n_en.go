package events

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		"unbekannter Event-Typ %q":  "unknown event type %q",
		"ungültiger Schweregrad %q": "invalid severity %q",
	})
}
