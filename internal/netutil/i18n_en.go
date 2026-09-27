package netutil

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		"nur IPv4-Subnetze werden aufgezählt: %s": "only IPv4 subnets are enumerated: %s",
		"Subnetz %s ist zu groß (maximal /16)":    "Subnet %s is too large (maximum /16)",
		"Hostname %s nicht auflösbar: %w":         "cannot resolve host name %s: %w",
		"Hostname %s nicht auflösbar":             "cannot resolve host name %s",
	})
}
