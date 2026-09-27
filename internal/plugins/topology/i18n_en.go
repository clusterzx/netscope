package topology

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		// plugin info and settings
		"Topologie": "Topology",
		"Leitet aus SNMP-FDB, LLDP-Nachbarn und Subnetz-Gateways die Verbindungen zwischen Geräten ab (Gerät → Switch-Port, LLDP-Nachbarn, Layer 3 zum Gateway).": "Derives the connections between devices from the SNMP FDB, LLDP neighbours and subnet gateways (device → switch port, LLDP neighbours, layer 3 to the gateway).",
		"Uplink ab MAC-Anzahl": "Uplink from MAC count",
		"Ein Switch-Port mit mehr MAC-Adressen gilt als Uplink (dahinter hängt ein weiterer Switch oder AP); Geräte auf Uplinks werden nicht diesem Port zugeordnet. Ports mit LLDP-Nachbar sind immer Uplinks.": "A switch port with more MAC addresses counts as an uplink (another switch or AP is connected behind it); devices on uplinks are not assigned to this port. Ports with an LLDP neighbour are always uplinks.",
		"Geräte ohne Switch-Port ans Gateway hängen":                                                                    "Attach devices without a switch port to the gateway",
		"Geräte ohne ermittelte Layer-2-Verbindung werden als Layer-3-Kante unter das Gateway ihres Subnetzes gehängt.": "Devices without a detected layer 2 connection are attached below the gateway of their subnet as a layer 3 edge.",
		"Geräte mit diesen Tags ausschließen":                                                                           "Exclude devices with these tags",
		"Ein Tag pro Zeile. Solche Geräte erhalten keine automatisch ermittelten Verbindungen.":                         "One tag per line. Such devices get no automatically detected connections.",
		"Haltezeit verschwundener Verbindungen":                                                                         "Retention of vanished connections",
		"Switch-Port- und LLDP-Verbindungen bleiben so lange bestehen, wenn ein Gerät (z. B. im Standby) nicht mehr in der MAC-Tabelle steht und nirgends sonst gesehen wird. 0 = sofort entfernen.": "Switch port and LLDP connections are kept this long when a device (e.g. in standby) is no longer in the MAC table and is not seen anywhere else. 0 = remove immediately.",

		// errors
		"keine Datenbank verfügbar": "no database available",
		"Topologie-Daten lesen: %w": "Reading topology data: %w",
		"Topologie speichern: %w":   "Saving topology: %w",
		"Geräte: %w":                "Devices: %w",
		"IP-Adressen: %w":           "IP addresses: %w",
		"Subnetze: %w":              "Subnets: %w",
		"Standorte: %w":             "Sites: %w",
		"SNMP-Inventar: %w":         "SNMP inventory: %w",
		"Beziehungen: %w":           "Relations: %w",

		// log message and attributes
		"Topologie aktualisiert": "Topology updated",
		"neu":                    "new",
		"entfernt":               "removed",
		"gehalten":               "kept",
		"dauer_ms":               "duration_ms",
	})
}
