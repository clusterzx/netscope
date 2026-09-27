package execx

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		"%s nicht gefunden: %w": "%s not found: %w",
	})
}
