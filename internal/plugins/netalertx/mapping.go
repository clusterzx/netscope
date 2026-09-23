package netalertx

import (
	"net/netip"
	"strings"
	"time"

	"netscope/internal/netutil"
	"netscope/internal/plugin"
)

// fakeMACPrefix marks synthetic MACs NetAlertX derives from IPs (ICMP_FAKE_MAC etc.).
const fakeMACPrefix = "fa:ce:"

// typeMap maps NetAlertX device types (dropdown values of newdev_template and the types
// of device_heuristics_rules.json) to NetScope types. Keys are normalized with typeKey.
var typeMap = map[string]string{
	"smartphone":             "phone",
	"phone":                  "phone",
	"mobile":                 "phone",
	"tablet":                 "tablet",
	"laptop":                 "laptop",
	"notebook":               "laptop",
	"pc":                     "desktop",
	"desktop":                "desktop",
	"minipc":                 "desktop",
	"computer":               "desktop",
	"workstation":            "desktop",
	"server":                 "server",
	"singleboardcomputersbc": "server",
	"sbc":                    "server",
	"nas":                    "nas",
	"printer":                "printer",
	"domotic":                "smart-home",
	"smarthome":              "smart-home",
	"smartswitch":            "smart-home",
	"smartplug":              "smart-home",
	"smartlight":             "smart-home",
	"smartappliance":         "smart-home",
	"houseappliance":         "smart-home",
	"securitydevice":         "smart-home",
	"ipcamera":               "camera",
	"camera":                 "camera",
	"gameconsole":            "game-console",
	"gamingconsole":          "game-console",
	"smarttv":                "tv",
	"tv":                     "tv",
	"tvdecoder":              "media-player",
	"radio":                  "media-player",
	"mediaplayer":            "media-player",
	"virtualassistance":      "speaker",
	"smartspeaker":           "speaker",
	"speaker":                "speaker",
	"iot":                    "iot",
	"clock":                  "iot",
	"ap":                     "access-point",
	"accesspoint":            "access-point",
	"wlan":                   "access-point",
	"router":                 "router",
	"gateway":                "router",
	"firewall":               "firewall",
	"hypervisor":             "hypervisor",
	"switch":                 "switch",
	"vm":                     "vm",
	"virtualmachine":         "vm",
	"container":              "container",
	"ups":                    "ups",
	"smartwatch":             "other",
	"powerline":              "other",
	"plc":                    "other",
	"usblanadapter":          "other",
	"usbwifiadapter":         "other",
	"other":                  "other",
}

// typeKey normalizes a type name ("Singleboard Computer (SBC)" -> "singleboardcomputersbc").
func typeKey(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// mapType returns the NetScope type of a NetAlertX type ("" if unknown or empty).
func mapType(t string) string {
	return typeMap[typeKey(t)]
}

// cleanText drops NetAlertX placeholders for unknown values.
func cleanText(s string) string {
	s = strings.TrimSpace(s)
	switch strings.ToLower(s) {
	case "", "(unknown)", "unknown", "(name not found)", "none", "(none)", "null":
		return ""
	}
	return s
}

// cleanName removes the " (IP match)" marker NetAlertX appends to names resolved by IP.
func cleanName(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimSpace(strings.TrimSuffix(s, "(IP match)"))
	return cleanText(s)
}

// identity describes how a NetAlertX device is identified in NetScope.
type identity struct {
	Key string // lower-case devMac, used as external reference
	MAC string // normalized real MAC ("" for synthetic or invalid MACs)
}

// identify classifies devMac. ok is false for the Internet pseudo device and empty MACs.
func identify(raw string) (identity, bool) {
	key := strings.ToLower(strings.TrimSpace(raw))
	if key == "" || key == "internet" {
		return identity{}, false
	}
	id := identity{Key: key}
	if strings.HasPrefix(key, fakeMACPrefix) {
		return id, true // synthetic: identified by IP and reference only
	}
	if mac, ok := netutil.NormalizeMAC(key); ok {
		id.MAC = mac
		id.Key = mac
	}
	return id, true
}

// deviceIP returns the last known address (devLastIP, else devPrimaryIPv4/IPv6).
func deviceIP(d device) string {
	for _, f := range []string{"last_ip", "ipv4", "ipv6"} {
		if a, err := netip.ParseAddr(d.get(f)); err == nil && !a.IsUnspecified() {
			return a.Unmap().String()
		}
	}
	return ""
}

// timeLayouts are the timestamp formats of NetAlertX (SQLite DATETIME text).
var timeLayouts = []string{
	"2006-01-02 15:04:05",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02 15:04",
	"2006-01-02",
}

// zonedLayouts carry an explicit offset.
var zonedLayouts = []string{
	time.RFC3339Nano,
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05 -0700",
	"2006-01-02 15:04:05 -0700 MST",
}

// parseTime parses a NetAlertX timestamp; values without offset are in loc.
func parseTime(s string, loc *time.Location) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "None" {
		return time.Time{}, false
	}
	for _, l := range zonedLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t, true
		}
	}
	for _, l := range timeLayouts {
		if t, err := time.ParseInLocation(l, s, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// relationKind derives the NetScope relation kind of a parent link from the parent's
// NetAlertX type, the relationship type (devParentRelType) and the port.
func relationKind(parentType, relType, port string) string {
	if strings.EqualFold(relType, "virtual") {
		return plugin.RelRunsOn
	}
	switch typeKey(parentType) {
	case "hypervisor":
		return plugin.RelRunsOn
	case "ap", "accesspoint", "wlan", "usbwifiadapter":
		return plugin.RelWireless
	case "switch", "powerline", "plc", "usblanadapter":
		return plugin.RelSwitchPort
	}
	if port != "" {
		return plugin.RelSwitchPort
	}
	return plugin.RelL3
}

// cleanPort drops NetAlertX defaults for "no port" (0, None, empty).
func cleanPort(s string) string {
	s = strings.TrimSpace(s)
	switch s {
	case "", "0", "None", "null", "-":
		return ""
	}
	return s
}
