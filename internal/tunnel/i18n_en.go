package tunnel

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// network operations (Linux)
		"WireGuard-Steuerung nicht verfügbar: %w": "WireGuard control not available: %w",
		"keine Berechtigung, Netzwerk-Interfaces anzulegen – der Container braucht NET_ADMIN (cap_add) und Host-Netzwerk (%w)": "no permission to create network interfaces – the container needs NET_ADMIN (cap_add) and host networking (%w)",
		"der Kernel des Hosts unterstützt kein WireGuard (enthalten ab Linux 5.6) (%w)":                                        "the host's kernel does not support WireGuard (included since Linux 5.6) (%w)",
		"Interface %s existiert bereits und ist kein WireGuard-Interface":                                                      "interface %s already exists and is not a WireGuard interface",
		"MTU setzen: %w":                  "set MTU: %w",
		"Endpoint %s nicht auflösbar: %w": "cannot resolve endpoint %s: %w",
		"der Endpoint %s liegt im Subnetz %s, das durch den Tunnel geleitet werden soll": "the endpoint %s lies in the subnet %s, which is to be routed through the tunnel",
		"WireGuard konfigurieren: %w": "configure WireGuard: %w",
		"Interface aktivieren: %w":    "activate interface: %w",
		"Adresse %s entfernen: %w":    "remove address %s: %w",
		"Adresse %s setzen: %w":       "set address %s: %w",
		"für %s gibt es auf dem Host schon eine Route über %s – das Netz ist bereits anders erreichbar": "the host already has a route for %s via %s – the network is already reachable another way",
		"Route %s setzen: %w":                                 "set route %s: %w",
		"Route %s entfernen: %w":                              "remove route %s: %w",
		"WireGuard-Tunnel werden nur unter Linux unterstützt": "WireGuard tunnels are only supported on Linux",

		// tunnel manager (log, state, events)
		"WireGuard-Tunnel nicht verfügbar":                  "WireGuard tunnels not available",
		"Tunnel-Interface entfernen":                        "Remove tunnel interface",
		"Tunnel: Subnetze nicht lesbar":                     "Tunnel: cannot read subnets",
		"Tunnel abgebaut":                                   "Tunnel removed",
		"Tunnel eingerichtet":                               "Tunnel set up",
		"Tunnel verbunden":                                  "Tunnel connected",
		"Tunnel-Event speichern":                            "Save tunnel event",
		"WireGuard ist auf diesem Host nicht verfügbar: %s": "WireGuard is not available on this host: %s",
		"Zugangsdaten nicht lesbar: %s":                     "Cannot read credentials: %s",
		"Credential „%s“ ist keine WireGuard-Konfiguration": "Credential “%s” is not a WireGuard configuration",
		"Konfiguration ungültig: %s":                        "Invalid configuration: %s",
		"das Subnetz %s überschneidet sich mit dem lokal angeschlossenen Netz %s": "the subnet %s overlaps the locally attached network %s",
		"Interface %s fehlt: %s":                "Interface %s missing: %s",
		"Tunnel „%s“ wieder verbunden":          "Tunnel “%s” connected again",
		"Tunnel „%s“ getrennt":                  "Tunnel “%s” disconnected",
		"Tunnel „%s“ lässt sich nicht aufbauen": "Tunnel “%s” cannot be established",
		"Keine Antwort der Gegenstelle %s – die Geräte in %s werden solange nicht gescannt und nicht als offline gewertet.": "No response from the peer %s – until then the devices in %s are not scanned and not counted as offline.",

		// connection test
		"Der Tunnel „%s“ mit dieser Konfiguration steht bereits.":                                                                                                          "The tunnel “%s” with this configuration is already up.",
		"Diese Konfiguration läuft bereits als Tunnel „%s“ (Zustand: %s) %s":                                                                                               "This configuration already runs as tunnel “%s” (state: %s) %s",
		"Handshake mit %s erfolgreich – Schlüssel und Endpoint stimmen.":                                                                                                   "Handshake with %s successful – keys and endpoint are correct.",
		"Keine Antwort von %s innerhalb von %d s. Prüfen: Endpoint und UDP-Port, Firewall der Gegenstelle, ob der öffentliche Schlüssel %s dort als Peer eingetragen ist.": "No response from %s within %d s. Check: endpoint and UDP port, the peer's firewall, whether the public key %s is entered there as a peer.",
	})
}
