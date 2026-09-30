package openwrt

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings
		"OpenWrt DHCP": "OpenWrt DHCP",
		"Liest DHCP-Leases und statische Leases von OpenWrt-/GL.iNet-Routern (per SSH oder LuCI) und füllt damit Hostnamen und Adressen.": "Reads DHCP leases and static leases from OpenWrt/GL.iNet routers (over SSH or LuCI) and uses them to fill in host names and addresses.",
		"Router": "Routers",
		"Hostname oder IP-Adresse, ein Router pro Zeile. Bei LuCI auch als URL, z. B. http://192.168.8.1:8080 (GL.iNet-Firmware 4.x); ohne URL wird http://<Router> verwendet.": "Host name or IP address, one router per line. With LuCI also as a URL, e.g. http://192.168.8.1:8080 (GL.iNet firmware 4.x); without a URL http://<router> is used.",
		"Zugriff":                             "Access",
		"SSH (dhcp.leases und uci show dhcp)": "SSH (dhcp.leases and uci show dhcp)",
		"LuCI (ubus JSON-RPC)":                "LuCI (ubus JSON-RPC)",
		"Zugangsdaten":                        "Credentials",
		"SSH: Credentials vom Typ SSH oder Benutzer/Passwort. LuCI: Benutzer/Passwort (leerer Benutzer = root). Leer = automatisch die passenden je Router nach dem Geltungsbereich des Credentials; abgelehnte werden übersprungen.": "SSH: credentials of type SSH or username/password. LuCI: username/password (empty user = root). Empty = automatically the matching ones per router according to the credential's scope; rejected ones are skipped.",
		"SSH-Port":          "SSH port",
		"SSH-Hostschlüssel": "SSH host key",
		"Beim ersten Kontakt merken, Änderungen ablehnen": "Remember on first contact, reject changes",
		"Nicht prüfen (unsicher)":                         "Do not verify (insecure)",
		"TLS-Zertifikat prüfen":                           "Verify TLS certificate",
		"Nur bei HTTPS mit gültigem Zertifikat einschalten; OpenWrt nutzt standardmäßig ein selbstsigniertes.": "Only enable for HTTPS with a valid certificate; OpenWrt uses a self-signed one by default.",
		"Statische Leases importieren": "Import static leases",
		"Feste Zuordnungen (uci dhcp host) auch ohne aktive Lease übernehmen; ihr Name hat Vorrang vor dem vom Client gemeldeten.": "Import fixed assignments (uci dhcp host) even without an active lease; their name takes precedence over the one the client reports.",
		"Fehlende Geräte anlegen": "Create missing devices",
		"Geräte aus Leases anlegen, die noch kein Scan gefunden hat. Aus: nur bekannte Geräte ergänzen.": "Create devices from leases that no scan has found yet. Off: only add to known devices.",

		// settings validation
		"%q: http(s)-URL oder Hostname erwartet":                                           "%q: http(s) URL or host name expected",
		"%q: URLs gibt es nur für den Zugriff per LuCI – für SSH Hostname oder IP angeben": "%q: URLs are only supported for access through LuCI – enter a host name or IP for SSH",
		"%q: Hostname oder IP erwartet":                                                    "%q: host name or IP expected",

		// run log, run errors
		"kein Router konfiguriert": "no router configured",
		"Router nicht gelesen":     "Could not read router",
		"kein Router gelesen: %w":  "no router read: %w",
		"keine passenden Zugangsdaten für %s (Auswahl oder Geltungsbereich der Credentials prüfen): %w": "no matching credentials for %s (check the selection or the scope of the credentials): %w",
		"LuCI-Anmeldung abgelehnt, nächste Zugangsdaten werden probiert":                                "LuCI login rejected, trying the next credentials",
		"DHCP-Daten gelesen":                            "Read DHCP data",
		"Lease konnte nicht gespeichert werden":         "Could not save lease",
		"keine der %d Leases konnte gespeichert werden": "none of the %d leases could be saved",

		// SSH
		"SSH-Verbindung zu %s fehlgeschlagen: %w":                                             "SSH connection to %s failed: %w",
		"uci show dhcp fehlgeschlagen, statische Leases fehlen":                               "uci show dhcp failed, static leases are missing",
		"Lease-Datei nicht lesbar":                                                            "Cannot read lease file",
		"weder Lease-Datei noch DHCP-Konfiguration lesbar – ist das Ziel ein OpenWrt-Router?": "neither lease file nor DHCP configuration readable – is the target an OpenWrt router?",

		// LuCI / ubus
		"ungültige LuCI-URL %q (erwartet z. B. http://192.168.8.1)":               "invalid LuCI URL %q (expected e.g. http://192.168.8.1)",
		"ubus %s: ungültige Antwort: %w":                                          "ubus %s: invalid response: %w",
		"ubus %s: ungültiger Status: %w":                                          "ubus %s: invalid status: %w",
		"ubus session.login: keine Session erhalten":                              "ubus session.login: no session received",
		"LuCI-Anmeldung abgelehnt":                                                "LuCI login rejected",
		"LuCI-Anmeldung als %q fehlgeschlagen – Benutzer und Passwort prüfen":     "LuCI login as %q failed – check username and password",
		"LuCI %s nicht erreichbar: %w":                                            "LuCI %s not reachable: %w",
		"keine Berechtigung für luci-rpc getDHCPLeases (%w)":                      "no permission for luci-rpc getDHCPLeases (%w)",
		"DHCP-Konfiguration (uci get dhcp) nicht lesbar, statische Leases fehlen": "Cannot read DHCP configuration (uci get dhcp), static leases are missing",
		// ubus status codes, alone and inside "ubus <call>: <status> (<code>)"
		"ungültiger Befehl":                                     "invalid command",
		"ungültiges Argument":                                   "invalid argument",
		"Methode nicht gefunden":                                "method not found",
		"nicht gefunden":                                        "not found",
		"keine Daten":                                           "no data",
		"Zugriff verweigert":                                    "access denied",
		"Zeitüberschreitung":                                    "Timeout",
		"nicht unterstützt":                                     "not supported",
		"unbekannter Fehler":                                    "unknown error",
		"Verbindung fehlgeschlagen":                             "connection failed",
		"ubus %s: ungültiger Befehl (%d)":                       "ubus %s: invalid command (%d)",
		"ubus %s: ungültiges Argument (%d)":                     "ubus %s: invalid argument (%d)",
		"ubus %s: Methode nicht gefunden (%d)":                  "ubus %s: method not found (%d)",
		"ubus %s: nicht gefunden (%d)":                          "ubus %s: not found (%d)",
		"ubus %s: keine Daten (%d)":                             "ubus %s: no data (%d)",
		"ubus %s: Zugriff verweigert (%d)":                      "ubus %s: access denied (%d)",
		"ubus %s: Zeitüberschreitung (%d)":                      "ubus %s: timeout (%d)",
		"ubus %s: nicht unterstützt (%d)":                       "ubus %s: not supported (%d)",
		"ubus %s: unbekannter Fehler (%d)":                      "ubus %s: unknown error (%d)",
		"ubus %s: Verbindung fehlgeschlagen (%d)":               "ubus %s: connection failed (%d)",
		"Verbunden – %d Leases und %d statische Leases gelesen": "Connected – read %d leases and %d static leases",
	})
}
