package csv

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

// Canonical column fields.
const (
	colMAC         = "mac"
	colMACs        = "macs"
	colIP          = "ip"
	colIPs         = "ips"
	colName        = "name"
	colHostname    = "hostname"
	colType        = "type"
	colVendor      = "vendor"
	colModel       = "model"
	colOS          = "os"
	colLocation    = "location"
	colOwner       = "owner"
	colTags        = "tags"
	colState       = "state"
	colCriticality = "criticality"
	colNotes       = "notes"
	colIgnored     = "-"
)

// columnAliases maps normalized header names (see headerKey) to fields. English names
// match the inventory export of the core (reports.CSVColumns).
var columnAliases = map[string]string{
	"mac":            colMAC,
	"macadresse":     colMAC,
	"macaddress":     colMAC,
	"macs":           colMACs,
	"macadressen":    colMACs,
	"macaddresses":   colMACs,
	"ip":             colIP,
	"ipadresse":      colIP,
	"ipaddress":      colIP,
	"ipv4":           colIP,
	"primaryip":      colIP,
	"ips":            colIPs,
	"ipadressen":     colIPs,
	"ipaddresses":    colIPs,
	"name":           colName,
	"anzeigename":    colName,
	"displayname":    colName,
	"bezeichnung":    colName,
	"hostname":       colHostname,
	"host":           colHostname,
	"rechnername":    colHostname,
	"type":           colType,
	"typ":            colType,
	"gerätetyp":      colType,
	"geraetetyp":     colType,
	"devicetype":     colType,
	"vendor":         colVendor,
	"hersteller":     colVendor,
	"manufacturer":   colVendor,
	"model":          colModel,
	"modell":         colModel,
	"os":             colOS,
	"betriebssystem": colOS,
	"location":       colLocation,
	"standort":       colLocation,
	"ort":            colLocation,
	"raum":           colLocation,
	"owner":          colOwner,
	"besitzer":       colOwner,
	"eigentümer":     colOwner,
	"eigentuemer":    colOwner,
	"verantwortlich": colOwner,
	"tags":           colTags,
	"tag":            colTags,
	"schlagwörter":   colTags,
	"schlagworte":    colTags,
	"state":          colState,
	"zustand":        colState,
	"criticality":    colCriticality,
	"kritikalität":   colCriticality,
	"kritikalitaet":  colCriticality,
	"notes":          colNotes,
	"notizen":        colNotes,
	"notiz":          colNotes,
	"bemerkung":      colNotes,
	"bemerkungen":    colNotes,
	"kommentar":      colNotes,
	"comments":       colNotes,
	// columns of the inventory export that describe the observed state, not the inventory
	"id":            colIgnored,
	"online":        colIgnored,
	"firstseen":     colIgnored,
	"lastseen":      colIgnored,
	"erstsichtung":  colIgnored,
	"letztsichtung": colIgnored,
}

// column is a mapped header cell.
type column struct {
	Header string
	Field  string // canonical field, colIgnored or "" (unknown)
	Custom string // custom field key for cf.<key> / custom:<key>
}

// customKeyRe matches keys of custom fields (inventory.CustomField).
var customKeyRe = regexp.MustCompile(`^[a-z][a-z0-9_]{0,39}$`)

// headerKey normalizes a header cell: lower case without spaces, dashes and underscores.
func headerKey(h string) string {
	h = strings.ToLower(strings.TrimSpace(strings.Trim(strings.TrimSpace(h), `"`)))
	return strings.NewReplacer(" ", "", "-", "", "_", "", ".", "").Replace(h)
}

// mapHeader maps header cells to columns. Invalid custom field keys are returned in bad.
func mapHeader(header []string) (cols []column, bad []string) {
	cols = make([]column, len(header))
	for i, h := range header {
		h = strings.TrimSpace(h) // a leading BOM was removed by decodeText
		cols[i].Header = h
		lower := strings.ToLower(h)
		for _, prefix := range []string{"cf.", "custom:"} {
			if key, ok := strings.CutPrefix(lower, prefix); ok {
				key = strings.TrimSpace(key)
				if customKeyRe.MatchString(key) {
					cols[i].Custom = key
				} else {
					bad = append(bad, h)
					cols[i].Field = colIgnored
				}
			}
		}
		if cols[i].Custom == "" && cols[i].Field == "" {
			cols[i].Field = columnAliases[headerKey(h)]
		}
	}
	return cols, bad
}

// usable reports whether the header identifies devices (MAC or IP column).
func usable(cols []column) bool {
	for _, c := range cols {
		switch c.Field {
		case colMAC, colMACs, colIP, colIPs:
			return true
		}
	}
	return false
}

// splitList splits list cells ("a;b", "a|b", "a, b", "a b").
func splitList(s string, spaces bool) []string {
	return strings.FieldsFunc(s, func(r rune) bool {
		return r == ';' || r == '|' || r == ',' || (spaces && (r == ' ' || r == '\t' || r == '\n' || r == '\r'))
	})
}

// stateValues maps English and German states.
var stateValues = map[string]string{
	"known":      "known",
	"bekannt":    "known",
	"unknown":    "unknown",
	"unbekannt":  "unknown",
	"ignored":    "ignored",
	"ignoriert":  "ignored",
	"ignore":     "ignored",
	"ignorieren": "ignored",
}

// criticalityValues maps English and German criticalities.
var criticalityValues = map[string]string{
	"low":      "low",
	"niedrig":  "low",
	"gering":   "low",
	"normal":   "normal",
	"mittel":   "normal",
	"high":     "high",
	"hoch":     "high",
	"critical": "critical",
	"kritisch": "critical",
}

// typeAliases maps common English and German device type names to NetScope types.
var typeAliases = map[string]string{
	"router":            "router",
	"gateway":           "router",
	"switch":            "switch",
	"accesspoint":       "access-point",
	"ap":                "access-point",
	"wlanap":            "access-point",
	"zugangspunkt":      "access-point",
	"firewall":          "firewall",
	"server":            "server",
	"hypervisor":        "hypervisor",
	"vm":                "vm",
	"virtualmachine":    "vm",
	"virtuellemaschine": "vm",
	"container":         "container",
	"lxc":               "container",
	"nas":               "nas",
	"desktop":           "desktop",
	"pc":                "desktop",
	"rechner":           "desktop",
	"computer":          "desktop",
	"laptop":            "laptop",
	"notebook":          "laptop",
	"phone":             "phone",
	"smartphone":        "phone",
	"handy":             "phone",
	"telefon":           "phone",
	"tablet":            "tablet",
	"tv":                "tv",
	"fernseher":         "tv",
	"smarttv":           "tv",
	"mediaplayer":       "media-player",
	"streaming":         "media-player",
	"speaker":           "speaker",
	"lautsprecher":      "speaker",
	"printer":           "printer",
	"drucker":           "printer",
	"camera":            "camera",
	"kamera":            "camera",
	"ipcamera":          "camera",
	"smarthome":         "smart-home",
	"iot":               "iot",
	"gameconsole":       "game-console",
	"spielkonsole":      "game-console",
	"konsole":           "game-console",
	"ups":               "ups",
	"usv":               "ups",
	"other":             "other",
	"sonstiges":         "other",
	"andere":            "other",
}

// normalizeType maps a type cell to a NetScope type; unknown values are kept as given
// (the selectable types are configurable).
func normalizeType(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if t, ok := typeAliases[headerKey(s)]; ok {
		return t
	}
	return s
}

// cp1252 maps the bytes 0x80–0x9F of Windows-1252 (the rest equals ISO 8859-1).
var cp1252 = [32]rune{
	0x20AC, 0xFFFD, 0x201A, 0x0192, 0x201E, 0x2026, 0x2020, 0x2021, 0x02C6, 0x2030, 0x0160, 0x2039, 0x0152, 0xFFFD, 0x017D, 0xFFFD,
	0xFFFD, 0x2018, 0x2019, 0x201C, 0x201D, 0x2022, 0x2013, 0x2014, 0x02DC, 0x2122, 0x0161, 0x203A, 0x0153, 0xFFFD, 0x017E, 0x0178,
}

// decodeText returns the file content as UTF-8: a UTF-8 BOM is removed, files that are
// not valid UTF-8 are read as Windows-1252 (Excel "CSV (Trennzeichen-getrennt)").
func decodeText(data []byte) (string, bool) {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		data = data[3:]
	}
	if utf8.Valid(data) {
		return string(data), false
	}
	var b strings.Builder
	b.Grow(len(data) + len(data)/8)
	for _, c := range data {
		switch {
		case c < 0x80:
			b.WriteByte(c)
		case c < 0xA0:
			b.WriteRune(cp1252[c-0x80])
		default:
			b.WriteRune(rune(c))
		}
	}
	return b.String(), true
}

// sniffDelimiter picks comma, semicolon or tab by counting them (outside quotes) in the
// first line.
func sniffDelimiter(text string) rune {
	counts := map[rune]int{}
	inQuote := false
	for _, r := range text {
		if r == '"' {
			inQuote = !inQuote
			continue
		}
		if !inQuote && (r == '\n' || r == '\r') {
			break
		}
		if !inQuote {
			counts[r]++
		}
	}
	best := ','
	for _, d := range []rune{';', '\t'} {
		if counts[d] > counts[best] {
			best = d
		}
	}
	return best
}
