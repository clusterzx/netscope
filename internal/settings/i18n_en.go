package settings

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		"0–16384 erwartet":             "0–16384 expected",
		"1–100 erwartet":               "1–100 expected",
		"1–64 erwartet":                "1–64 expected",
		"de oder en erwartet":          "de or en expected",
		"gültige http(s)-URL erwartet": "valid http(s) URL expected",
	})
}
