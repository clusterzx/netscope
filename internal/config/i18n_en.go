package config

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		"listen darf nicht leer sein":                                          "listen must not be empty",
		"data_dir darf nicht leer sein":                                        "data_dir must not be empty",
		"NETSCOPE_CENTRAL_URL und NETSCOPE_CENTRAL_TOKEN nur gemeinsam setzen": "set NETSCOPE_CENTRAL_URL and NETSCOPE_CENTRAL_TOKEN only together",
		"log_format %q: erlaubt sind json, text":                               "log_format %q: allowed are json, text",
		"trusted_proxies: %q ist weder IP noch CIDR":                           "trusted_proxies: %q is neither an IP address nor a CIDR",
		"log_level %q: erlaubt sind debug, info, warn, error":                  "log_level %q: allowed are debug, info, warn, error",
	})
}
