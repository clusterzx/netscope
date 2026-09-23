package mdns

import (
	"strings"
)

// typeCategory maps DNS-SD service names to device types. "airplay" is resolved to
// media-player or speaker in guessType.
var typeCategory = map[string]string{
	"_googlecast":      "media-player",
	"_airplay":         "airplay",
	"_raop":            "airplay",
	"_ipp":             "printer",
	"_ipps":            "printer",
	"_printer":         "printer",
	"_pdl-datastream":  "printer",
	"_scanner":         "printer",
	"_uscan":           "printer",
	"_uscans":          "printer",
	"_hap":             "smart-home",
	"_homekit":         "smart-home",
	"_hue":             "smart-home",
	"_matter":          "smart-home",
	"_matterc":         "smart-home",
	"_home-assistant":  "smart-home",
	"_shelly":          "smart-home",
	"_esphomelib":      "iot",
	"_wled":            "iot",
	"_smb":             "nas",
	"_afpovertcp":      "nas",
	"_adisk":           "nas",
	"_workstation":     "desktop",
	"_companion-link":  "phone",
	"_sonos":           "speaker",
	"_spotify-connect": "speaker",
}

// speakerModels are AirPlay model prefixes of loudspeakers (HomePod, AirPort Express).
var speakerModels = []string{"audioaccessory", "airport", "homepod"}

// guessType derives a device type from the service types; only an unambiguous result
// is returned.
func guessType(types []string, model string) string {
	cats := map[string]bool{}
	for _, t := range types {
		name, _, _ := strings.Cut(t, ".")
		if c, ok := typeCategory[strings.ToLower(name)]; ok {
			cats[c] = true
		}
	}
	if cats["airplay"] {
		delete(cats, "airplay")
		m := strings.ToLower(model)
		speaker := cats["speaker"]
		for _, p := range speakerModels {
			if strings.HasPrefix(m, p) {
				speaker = true
			}
		}
		if speaker {
			cats["speaker"] = true
		} else {
			cats["media-player"] = true
		}
	}
	if len(cats) != 1 {
		return ""
	}
	for c := range cats {
		return c
	}
	return ""
}

// modelKeys are TXT keys that carry a model name, in order of preference.
var modelKeys = []string{"ty", "usb_MDL", "md", "model", "am", "modelid", "product"}

// vendorKeys are TXT keys that carry the manufacturer.
var vendorKeys = []string{"usb_MFG", "manufacturer", "mfg", "vendor"}

// txtValue returns the first non-empty value of the keys (case-insensitive) over all
// services.
func txtValue(services []Service, keys []string) string {
	for _, k := range keys {
		for _, s := range services {
			for tk, v := range s.TXT {
				if strings.EqualFold(tk, k) {
					if v = cleanValue(v); v != "" {
						return v
					}
				}
			}
		}
	}
	return ""
}

// cleanValue trims whitespace and the parentheses printers put around "product".
func cleanValue(v string) string {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, "(") && strings.HasSuffix(v, ")") {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	return v
}
