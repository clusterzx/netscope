package docker

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"Docker": "Docker",
		"Liest Container, Images, veröffentlichte Ports und Compose-Projekte von Docker-Hosts (lokaler Socket, TCP oder SSH-Tunnel) und ordnet sie dem Host als Kind-Objekte zu.": "Reads containers, images, published ports and Compose projects from Docker hosts (local socket, TCP or SSH tunnel) and assigns them to the host as child objects.",
		"Endpunkte": "Endpoints",
		"Ein Endpunkt pro Zeile: unix:///var/run/docker.sock (in den NetScope-Container gemounteter Socket des eigenen Hosts), tcp://host:2375 (unverschlüsselte Docker-API) oder ssh://[benutzer@]host[:port] (Socket /var/run/docker.sock des Hosts per SSH-Tunnel).": "One endpoint per line: unix:///var/run/docker.sock (socket of NetScope's own host mounted into the NetScope container), tcp://host:2375 (unencrypted Docker API) or ssh://[user@]host[:port] (the host's socket /var/run/docker.sock through an SSH tunnel).",
		"SSH-Zugangsdaten": "SSH credentials",
		"Für ssh://-Endpunkte. Leer = automatisch die passenden je Host nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen. Ein Benutzer in der URL hat Vorrang; er braucht Zugriff auf den Docker-Socket (root oder Gruppe docker).": "For ssh:// endpoints. Empty = automatically the matching ones per host according to the credential's scope; rejected ones are skipped. A user in the URL takes precedence; it needs access to the Docker socket (root or group docker).",
		"SSH-Hostschlüssel": "SSH host key",
		"Beim ersten Kontakt merken, Änderungen ablehnen": "Remember on first contact, reject changes",
		"Nicht prüfen (unsicher)":                         "Do not verify (insecure)",
		"Timeout pro Anfrage":                             "Timeout per request",

		// endpoints
		"Endpunkt %q: Schema fehlt (unix://, tcp:// oder ssh://)":              "endpoint %q: scheme missing (unix://, tcp:// or ssh://)",
		"Endpunkt %q: Socket-Pfad fehlt":                                       "endpoint %q: socket path missing",
		"Endpunkt %q: Host fehlt oder ungültig":                                "endpoint %q: host missing or invalid",
		"Endpunkt %q: Passwörter gehören ins SSH-Credential, nicht in die URL": "endpoint %q: passwords belong in the SSH credential, not in the URL",
		"Endpunkt %q: tcp:// erwartet keinen Pfad":                             "endpoint %q: tcp:// expects no path",
		"Endpunkt %q: ungültiger Port":                                         "endpoint %q: invalid port",
		"Endpunkt %q: nicht unterstütztes Schema %q (erlaubt: unix, tcp, ssh)": "endpoint %q: unsupported scheme %q (allowed: unix, tcp, ssh)",
		"nicht unterstütztes Schema %q":                                        "unsupported scheme %q",
		"%s: ungültige JSON-Antwort: %w":                                       "%s: invalid JSON response: %w",
		"/version: ungültige JSON-Antwort: %w":                                 "/version: invalid JSON response: %w",
		"keine lokale IPv4-Adresse gefunden":                                   "no local IPv4 address found",
		"IP-Adresse des lokalen Docker-Hosts unbekannt: %w":                    "IP address of the local Docker host unknown: %w",
		"SSH-Verbindung: %w":                                                   "SSH connection: %w",
		"Docker-Socket %s über SSH nicht erreichbar: %w":                       "Docker socket %s not reachable over SSH: %w",
		"Docker-API nicht erreichbar: %w":                                      "Docker API not reachable: %w",

		// run log, run errors
		"keine Docker-Endpunkte konfiguriert": "no Docker endpoints configured",
		"Docker-Endpunkt fehlgeschlagen":      "Docker endpoint failed",
		"kein Docker-Endpunkt erreichbar: %w": "no Docker endpoint reachable: %w",
		"keine passenden SSH-Zugangsdaten für %s (Auswahl oder Geltungsbereich der Credentials prüfen): %w": "no matching SSH credentials for %s (check the selection or the scope of the credentials): %w",
		"Image-Liste nicht lesbar":     "Cannot read image list",
		"Speichern fehlgeschlagen: %w": "saving failed: %w",
		"Docker-Host ist nicht im Inventar – die Container werden zugeordnet, sobald ein Scan den Host gefunden hat": "Docker host is not in the inventory – the containers will be assigned once a scan has found the host",
		"Docker-Host importiert": "Imported Docker host",
	})
}
