package wgconf

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// parse errors
		"Zeile %d: %s": "line %d: %s",
		"mehrere [Peer]-Abschnitte – für NetScope genügt genau einer (der WireGuard-Server)": "several [Peer] sections – NetScope needs exactly one (the WireGuard server)",
		"unbekannter Abschnitt [%s]":                                           "unknown section [%s]",
		"Zeile ohne „Schlüssel = Wert“":                                        "line without “key = value”",
		"Wert vor dem ersten Abschnitt ([Interface] oder [Peer])":              "value before the first section ([Interface] or [Peer])",
		"%s mehrfach angegeben":                                                "%s given more than once",
		"PrivateKey im Abschnitt [Interface] fehlt":                            "PrivateKey missing in section [Interface]",
		"Address im Abschnitt [Interface] fehlt (Tunnel-Adresse von NetScope)": "Address missing in section [Interface] (tunnel address of NetScope)",
		"Abschnitt [Peer] (WireGuard-Server) fehlt":                            "section [Peer] (WireGuard server) missing",
		"PublicKey im Abschnitt [Peer] fehlt":                                  "PublicKey missing in section [Peer]",
		"Endpoint im Abschnitt [Peer] fehlt – NetScope baut den Tunnel zum Server auf und braucht dessen Adresse": "Endpoint missing in section [Peer] – NetScope establishes the tunnel to the server and needs its address",
		"AllowedIPs im Abschnitt [Peer] fehlt": "AllowedIPs missing in section [Peer]",
		"PublicKey des Peers ist der eigene Schlüssel – dort gehört der Schlüssel des Servers hin": "PublicKey of the peer is this configuration's own key – the server's key belongs there",
		"PrivateKey ist kein gültiger WireGuard-Schlüssel":                                         "PrivateKey is not a valid WireGuard key",
		"ListenPort muss zwischen 1 und 65535 liegen":                                              "ListenPort must be between 1 and 65535",
		"MTU muss zwischen 1280 und 9000 liegen":                                                   "MTU must be between 1280 and 9000",
		"PublicKey ist kein gültiger WireGuard-Schlüssel":                                          "PublicKey is not a valid WireGuard key",
		"PresharedKey ist kein gültiger WireGuard-Schlüssel":                                       "PresharedKey is not a valid WireGuard key",
		"Endpoint erwartet host:port, z. B. vpn.example.org:51820":                                 "Endpoint expects host:port, e.g. vpn.example.org:51820",
		"Endpoint hat einen ungültigen Port":                                                       "Endpoint has an invalid port",
		"PersistentKeepalive muss eine Zahl in Sekunden sein":                                      "PersistentKeepalive must be a number of seconds",
		"unbekannte Option %q":                                                                     "unknown option %q",
		"ungültige Adresse %q":                                                                     "invalid address %q",
		"ungültiges Netz %q":                                                                       "invalid network %q",
		"leer":                                                                                     "empty",

		// "Zeile %d: %s" translates its message only through patterns: the plain errors
		// that come with a line number, with it
		"Zeile %d: mehrere [Peer]-Abschnitte – für NetScope genügt genau einer (der WireGuard-Server)": "line %d: several [Peer] sections – NetScope needs exactly one (the WireGuard server)",
		"Zeile %d: unbekannter Abschnitt [%s]":                               "line %d: unknown section [%s]",
		"Zeile %d: Zeile ohne „Schlüssel = Wert“":                            "line %d: line without “key = value”",
		"Zeile %d: Wert vor dem ersten Abschnitt ([Interface] oder [Peer])":  "line %d: value before the first section ([Interface] or [Peer])",
		"Zeile %d: %s mehrfach angegeben":                                    "line %d: %s given more than once",
		"Zeile %d: PrivateKey ist kein gültiger WireGuard-Schlüssel":         "line %d: PrivateKey is not a valid WireGuard key",
		"Zeile %d: Address: %w":                                              "line %d: Address: %w",
		"Zeile %d: ListenPort muss zwischen 1 und 65535 liegen":              "line %d: ListenPort must be between 1 and 65535",
		"Zeile %d: MTU muss zwischen 1280 und 9000 liegen":                   "line %d: MTU must be between 1280 and 9000",
		"Zeile %d: PublicKey ist kein gültiger WireGuard-Schlüssel":          "line %d: PublicKey is not a valid WireGuard key",
		"Zeile %d: PresharedKey ist kein gültiger WireGuard-Schlüssel":       "line %d: PresharedKey is not a valid WireGuard key",
		"Zeile %d: Endpoint erwartet host:port, z. B. vpn.example.org:51820": "line %d: Endpoint expects host:port, e.g. vpn.example.org:51820",
		"Zeile %d: Endpoint hat einen ungültigen Port":                       "line %d: Endpoint has an invalid port",
		"Zeile %d: AllowedIPs: %w":                                           "line %d: AllowedIPs: %w",
		"Zeile %d: PersistentKeepalive muss eine Zahl in Sekunden sein":      "line %d: PersistentKeepalive must be a number of seconds",
		"Zeile %d: unbekannte Option %q":                                     "line %d: unknown option %q",

		// ignored options and warnings
		"%s wird ignoriert – %s":                                  "%s is ignored – %s",
		"NetScope führt keine Befehle aus":                        "NetScope does not run commands",
		"die Namensauflösung des Servers bleibt unverändert":      "the server's name resolution stays unchanged",
		"NetScope setzt nur Routen für die zugeordneten Subnetze": "NetScope only sets routes for the assigned subnets",
		"nicht nötig": "not needed",
		// "%s wird ignoriert – %s" with each reason (the reason is no wrapped error)
		"%s wird ignoriert – NetScope führt keine Befehle aus":                        "%s is ignored – NetScope does not run commands",
		"%s wird ignoriert – die Namensauflösung des Servers bleibt unverändert":      "%s is ignored – the server's name resolution stays unchanged",
		"%s wird ignoriert – NetScope setzt nur Routen für die zugeordneten Subnetze": "%s is ignored – NetScope only sets routes for the assigned subnets",
		"%s wird ignoriert – nicht nötig":                                             "%s is ignored – not needed",
		"AllowedIPs leitet alles durch den Tunnel (0.0.0.0/0) – NetScope schickt nur die zugeordneten Subnetze hindurch, der übrige Verkehr des Servers bleibt unverändert.": "AllowedIPs routes everything through the tunnel (0.0.0.0/0) – NetScope only sends the assigned subnets through it, the rest of the server's traffic stays unchanged.",
		"%s ist nicht in AllowedIPs enthalten – die Gegenstelle leitet dieses Netz eventuell nicht weiter.":                                                                  "%s is not included in AllowedIPs – the peer may not forward this network.",
		"Kein PersistentKeepalive angegeben – NetScope verwendet %d s, damit der Tunnel offen bleibt.":                                                                       "No PersistentKeepalive given – NetScope uses %d s to keep the tunnel open.",
	})
}
