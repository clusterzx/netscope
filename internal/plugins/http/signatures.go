package http

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"netscope/internal/plugin"
)

// matchField identifies which part of a response a matcher inspects.
type matchField int

const (
	fieldTitle matchField = iota
	fieldServer
	fieldBody
	fieldHeader
	fieldFavicon
)

// matcher is one condition of a signature.
type matcher struct {
	field  matchField
	header string         // for fieldHeader
	re     *regexp.Regexp // for text fields
	hash   int32          // for fieldFavicon
}

// signature fingerprints one web application. A signature matches when any of its matchers
// matches (evidence records which one).
type signature struct {
	name         string
	cpeVendor    string
	cpeProduct   string
	deviceType   string // strong device-type hint, "" if none
	confidence   string // high | medium
	matchers     []matcher
	version      *regexp.Regexp // optional; capture group 1 is the version
	versionField matchField
}

// fingerprint is the extracted data a set of signatures is matched against.
type fingerprint struct {
	title   string
	server  string
	body    []byte
	headers http.Header
	favicon *int32
}

func ci(pat string) *regexp.Regexp { return regexp.MustCompile("(?i)" + pat) }

func title(pat string) matcher  { return matcher{field: fieldTitle, re: ci(pat)} }
func server(pat string) matcher { return matcher{field: fieldServer, re: ci(pat)} }
func body(pat string) matcher   { return matcher{field: fieldBody, re: ci(pat)} }
func header(name, pat string) matcher {
	return matcher{field: fieldHeader, header: name, re: ci(pat)}
}

// value returns the text a matcher inspects and whether that field is present.
func (fp fingerprint) value(field matchField, header string) (string, bool) {
	switch field {
	case fieldTitle:
		return fp.title, fp.title != ""
	case fieldServer:
		return fp.server, fp.server != ""
	case fieldBody:
		return string(fp.body), len(fp.body) > 0
	case fieldHeader:
		vals := fp.headers.Values(header)
		if len(vals) == 0 {
			return "", false
		}
		return strings.Join(vals, "\n"), true
	}
	return "", false
}

func (m matcher) match(fp fingerprint) bool {
	if m.field == fieldFavicon {
		return fp.favicon != nil && *fp.favicon == m.hash
	}
	v, ok := fp.value(m.field, m.header)
	if !ok {
		return false
	}
	return m.re.MatchString(v)
}

func (m matcher) describe() string {
	switch m.field {
	case fieldTitle:
		return "title"
	case fieldServer:
		return "server"
	case fieldBody:
		return "body"
	case fieldHeader:
		return "header:" + m.header
	case fieldFavicon:
		return "favicon"
	}
	return "?"
}

// detect matches fp against the given signatures and returns the recognised apps. It also
// returns a device-type hint from the first high-confidence signature that carries one.
func detect(fp fingerprint, sigs []signature) ([]plugin.DetectedApp, string) {
	var apps []plugin.DetectedApp
	deviceType := ""
	seen := map[string]bool{}
	for _, s := range sigs {
		hit := ""
		for _, m := range s.matchers {
			if m.match(fp) {
				hit = m.describe()
				break
			}
		}
		if hit == "" || seen[s.name] {
			continue
		}
		seen[s.name] = true
		app := plugin.DetectedApp{Name: s.name, Confidence: s.confidence, Evidence: hit}
		if s.version != nil {
			if v, ok := fp.value(s.versionField, ""); ok {
				if m := s.version.FindStringSubmatch(v); len(m) > 1 {
					app.Version = strings.TrimSpace(m[1])
				}
			}
		}
		app.CPE = buildCPE(s.cpeVendor, s.cpeProduct, app.Version)
		apps = append(apps, app)
		if deviceType == "" && s.deviceType != "" && s.confidence == "high" {
			deviceType = s.deviceType
		}
	}
	return apps, deviceType
}

// buildCPE builds a 2.3 application CPE, filling the version when known.
func buildCPE(vendor, product, version string) string {
	if vendor == "" || product == "" {
		return ""
	}
	v := cpeEscape(version)
	if v == "" {
		v = "*"
	}
	return fmt.Sprintf("cpe:2.3:a:%s:%s:%s:*:*:*:*:*:*:*", cpeEscape(vendor), cpeEscape(product), v)
}

var cpeSpecial = regexp.MustCompile(`[:\s]+`)

func cpeEscape(s string) string {
	return cpeSpecial.ReplaceAllString(strings.ToLower(strings.TrimSpace(s)), "_")
}

// parseCustomSignature parses a "Name|feld|regex" entry. feld is one of title, server,
// body, header:<Name>, favicon (an mmh3 integer).
func parseCustomSignature(line string) (signature, error) {
	parts := strings.SplitN(line, "|", 3)
	if len(parts) != 3 {
		return signature{}, fmt.Errorf("Format \"Name|Feld|Regex\" erwartet")
	}
	name := strings.TrimSpace(parts[0])
	field := strings.ToLower(strings.TrimSpace(parts[1]))
	pat := parts[2]
	if name == "" {
		return signature{}, fmt.Errorf("Name fehlt")
	}
	s := signature{name: name, confidence: "medium"}
	if field == "favicon" {
		h, err := strconv.ParseInt(strings.TrimSpace(pat), 10, 32)
		if err != nil {
			return signature{}, fmt.Errorf("favicon erwartet einen mmh3-Ganzzahlwert")
		}
		s.matchers = []matcher{{field: fieldFavicon, hash: int32(h)}}
		return s, nil
	}
	re, err := regexp.Compile("(?i)" + pat)
	if err != nil {
		return signature{}, fmt.Errorf("ungültiger regulärer Ausdruck: %w", err)
	}
	switch {
	case field == "title":
		s.matchers = []matcher{{field: fieldTitle, re: re}}
	case field == "server":
		s.matchers = []matcher{{field: fieldServer, re: re}}
	case field == "body":
		s.matchers = []matcher{{field: fieldBody, re: re}}
	case strings.HasPrefix(field, "header:"):
		hn := strings.TrimSpace(field[len("header:"):])
		if hn == "" {
			return signature{}, fmt.Errorf("Header-Name fehlt")
		}
		s.matchers = []matcher{{field: fieldHeader, header: hn, re: re}}
	default:
		return signature{}, fmt.Errorf("unbekanntes Feld %q (title, server, body, header:<Name>, favicon)", field)
	}
	return s, nil
}
