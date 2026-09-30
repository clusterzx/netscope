package rack

import "netscope/internal/i18n"

// English texts of this package (German source text → English), see internal/i18n.
func init() {
	i18n.Register(map[string]string{
		"Name fehlt":                                         "Name missing",
		"19 oder 10 Zoll erwartet":                           "19 or 10 inch expected",
		"1 bis %d Höheneinheiten erwartet":                   "1 to %d rack units expected",
		"bottom oder top erwartet":                           "bottom or top expected",
		"Das Rack ist bis HE %d belegt":                      "The rack is occupied up to U%d",
		"Das Rack hat nur %d Höheneinheiten":                 "The rack has only %d rack units",
		"Der Platz ist belegt (%s)":                          "The space is taken (%s)",
		"Gerät fehlt":                                        "Device missing",
		"Unbekannte Art":                                     "Unknown kind",
		"Höheneinheit ab 1 erwartet":                         "Rack unit from 1 expected",
		"front oder rear erwartet":                           "front or rear expected",
		"Volle, halbe oder drittel Breite erwartet":          "Full, half or third width expected",
		"Ungültige Spalte für diese Breite":                  "Invalid column for this width",
		"0 bis %d Ports erwartet":                            "0 to %d ports expected",
		"Gerät existiert nicht":                              "Device does not exist",
		"Das Gerät ist bereits in Rack %s eingebaut":         "The device is already mounted in rack %s",
		"Unbekannter Port":                                   "Unknown port",
		"Unbekannter Port %s":                                "Unknown port %s",
		"Ein Gerät kann nicht an einem eigenen Port stecken": "A device cannot be plugged into one of its own ports",
		"An diesem Port steckt ein Patchkabel – erst das Kabel entfernen": "A patch cable is plugged into this port – remove the cable first",
		"An Port %s steckt direkt ein Gerät – erst die Zuordnung lösen":   "A device is plugged directly into port %s – remove the assignment first",
		"Port %s ist bereits belegt":                                      "Port %s is already taken",
		"Ein Kabel braucht zwei verschiedene Ports":                       "A cable needs two different ports",
		"Element existiert nicht":                                         "Element does not exist",
		"Unbekannte Farbe":                                                "Unknown color",
		"Nur Geräte haben erkannte Verbindungen":                          "Only devices have detected connections",
		"Patchfeld":        "Patch panel",
		"Fachboden":        "Shelf",
		"Blende":           "Blank panel",
		"Kabelführung":     "Cable manager",
		"Steckdosenleiste": "PDU",
		"Gerät":            "Device",
		"Element":          "Element",
		"Gerät %d":         "Device %d",
	})
}
