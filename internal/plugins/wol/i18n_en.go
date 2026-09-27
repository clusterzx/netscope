package wol

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin, settings, action
		"Wake-on-LAN": "Wake-on-LAN",
		"Weckt Geräte per Wake-on-LAN (Magic Packet an die Broadcast-Adressen ihres Netzes).": "Wakes devices via Wake-on-LAN (magic packet to the broadcast addresses of their network).",
		"UDP-Port": "UDP port",
		"Zielport des Magic Packets (üblich: 9 oder 7).": "Destination port of the magic packet (usually 9 or 7).",
		"Wiederholungen": "Retries",
		"Wie oft das Paket an jede Zieladresse gesendet wird.": "How often the packet is sent to each destination address.",
		"Zusätzliche Broadcast-Adressen":                       "Additional broadcast addresses",
		"Weitere Zieladressen (eine pro Zeile), z. B. die Broadcast-Adresse eines gerouteten Netzes mit Directed-Broadcast-Weiterleitung.": "Further destination addresses (one per line), e.g. the broadcast address of a routed network with directed broadcast forwarding.",
		"SecureOn-Passwort": "SecureOn password",
		"Optionales 6-Byte-Passwort (12 Hex-Zeichen, z. B. 01:23:45:67:89:ab), das manche Netzwerkkarten zusätzlich verlangen.": "Optional 6-byte password (12 hex characters, e.g. 01:23:45:67:89:ab) that some network cards additionally require.",
		"Wake-on-LAN senden": "Send Wake-on-LAN",
		"Sendet ein Magic Packet an alle MAC-Adressen der ausgewählten Geräte.": "Sends a magic packet to all MAC addresses of the selected devices.",

		// validation, action errors, run log
		"SecureOn-Passwort muss aus 6 Byte (12 Hex-Zeichen) bestehen": "SecureOn password must be 6 bytes (12 hex characters)",
		"ungültige MAC-Adresse %s":                                    "invalid MAC address %s",
		"unbekannte Aktion %q":                                        "unknown action %q",
		"kein Gerät ausgewählt":                                       "no device selected",
		"Subnetze konnten nicht gelesen werden":                       "Could not read subnets",
		"Gerät hat keine MAC-Adresse – Wake-on-LAN nicht möglich":     "Device has no MAC address – Wake-on-LAN not possible",
		"Magic Packet konnte nicht gesendet werden":                   "Could not send magic packet",
		"Magic Packet gesendet":                                       "Magic packet sent",
		"ziel":                                                        "target",
		"Gerät":                                                       "Device",
		"Geräte":                                                      "Devices",
		"Gerät %s":                                                    "Device %s",

		// action result: the parts and the finished texts they are joined to
		"Kein Magic Packet gesendet":                                                             "No magic packet sent",
		" – ohne MAC-Adresse: %s":                                                                " – without MAC address: %s",
		"Kein Magic Packet gesendet – ohne MAC-Adresse: %s":                                      "No magic packet sent – without MAC address: %s",
		"Kein Magic Packet gesendet – %s":                                                        "No magic packet sent – %s",
		"Wake-on-LAN an %d %s gesendet (%d Pakete an %s)":                                        "Wake-on-LAN sent to %d %s (%d packets to %s)",
		"Wake-on-LAN an 1 Gerät gesendet (%d Pakete an %s)":                                      "Wake-on-LAN sent to 1 device (%d packets to %s)",
		"Wake-on-LAN an %d Geräte gesendet (%d Pakete an %s)":                                    "Wake-on-LAN sent to %d devices (%d packets to %s)",
		"; ohne MAC-Adresse übersprungen: %s":                                                    "; skipped without MAC address: %s",
		"Wake-on-LAN an 1 Gerät gesendet (%d Pakete an %s); ohne MAC-Adresse übersprungen: %s":   "Wake-on-LAN sent to 1 device (%d packets to %s); skipped without MAC address: %s",
		"Wake-on-LAN an %d Geräte gesendet (%d Pakete an %s); ohne MAC-Adresse übersprungen: %s": "Wake-on-LAN sent to %d devices (%d packets to %s); skipped without MAC address: %s",
	})
}
