package cron

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n. The
// descriptions of expressions are not catalog texts: DescribeIn writes them per language.
func init() {
	i18n.Register(map[string]string{
		// validation errors of expressions
		"leerer Ausdruck":                   "empty expression",
		"ungültige Dauer %q: %w":            "invalid duration %q: %w",
		"Intervall muss mindestens %s sein": "interval must be at least %s",
		"unbekanntes Makro %q":              "unknown macro %q",
		"erwartet 5 Felder (Minute Stunde Tag Monat Wochentag), gefunden %d": "expected 5 fields (minute hour day month weekday), found %d",
		"Minute: %w":                        "minute: %w",
		"Stunde: %w":                        "hour: %w",
		"Tag: %w":                           "day: %w",
		"Monat: %w":                         "month: %w",
		"Wochentag: %w":                     "weekday: %w",
		"ungültiger Wert %q":                "invalid value %q",
		"leerer Listeneintrag":              "empty list entry",
		"ungültige Schrittweite %q":         "invalid step %q",
		"Bereich %d-%d außerhalb von %d-%d": "range %d-%d outside %d-%d",
	})
}
