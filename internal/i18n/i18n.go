// Package i18n translates the texts NetScope shows to people. German is the source language:
// every text is written in German in the code, and English translations are registered as
// catalogs that map the German text to its English counterpart.
//
// Each package registers the translations of its own texts in init(), usually in a file
// i18n_en.go next to the code:
//
//	func init() {
//		i18n.Register(map[string]string{
//			"Neues Gerät":           "New device",
//			"Neues Gerät: %s":       "New device: %s",
//			"%s auf %s ausgelastet": "%[2]s: %[1]s saturated",
//		})
//	}
//
// Keys containing fmt verbs (%s, %d, %q, %v, %w, %.1f …) are patterns: they translate the
// finished text as fmt produced it, so errors and event titles are translated without the
// code knowing the reader's language. The English side may reorder arguments with %[n]s; %w
// and %v arguments (usually errors) are translated as well, other arguments (%s, %q, %d …)
// are kept verbatim – they carry names and addresses.
//
// A test (catalog_test.go) makes sure every German text in the code has a translation.
package i18n

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// Locale is a supported language.
type Locale string

const (
	// DE is German, the source language and the default.
	DE Locale = "de"
	// EN is English.
	EN Locale = "en"
)

// Default is used when neither a preference nor the browser names a supported language.
const Default = DE

// Supported lists the languages in display order.
var Supported = []Locale{DE, EN}

// Parse accepts "de", "en" and region variants ("en-GB", "de_AT"). ok is false for other or
// empty input.
func Parse(s string) (Locale, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	if len(s) > 2 && (s[2] == '-' || s[2] == '_') {
		s = s[:2]
	}
	switch Locale(s) {
	case DE:
		return DE, true
	case EN:
		return EN, true
	}
	return "", false
}

// Accept returns the first supported language of an Accept-Language header in order of
// preference (q values), or Default.
func Accept(header string) Locale {
	type cand struct {
		loc Locale
		q   float64
		i   int
	}
	var cands []cand
	for i, part := range strings.Split(header, ",") {
		tag, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		loc, ok := Parse(tag)
		if !ok {
			continue
		}
		q := 1.0
		for _, p := range strings.Split(params, ";") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(p), "q="); ok {
				if f, err := strconv.ParseFloat(v, 64); err == nil {
					q = f
				}
			}
		}
		if q > 0 {
			cands = append(cands, cand{loc, q, i})
		}
	}
	if len(cands) == 0 {
		return Default
	}
	sort.SliceStable(cands, func(a, b int) bool { return cands[a].q > cands[b].q })
	return cands[0].loc
}

// Pick returns the language of a request: the user's preference if set, otherwise the
// browser's (Accept-Language), otherwise Default.
func Pick(preference, acceptLanguage string) Locale {
	if loc, ok := Parse(preference); ok {
		return loc
	}
	return Accept(acceptLanguage)
}

type ctxKey struct{}

// WithLocale returns a context carrying the language.
func WithLocale(ctx context.Context, loc Locale) context.Context {
	return context.WithValue(ctx, ctxKey{}, loc)
}

// FromContext returns the language of the context (Default if none was set).
func FromContext(ctx context.Context) Locale {
	if ctx != nil {
		if loc, ok := ctx.Value(ctxKey{}).(Locale); ok && loc != "" {
			return loc
		}
	}
	return Default
}

// ---------------------------------------------------------------- catalog

// pattern is a catalog key with fmt verbs, compiled for matching finished texts.
type pattern struct {
	key     string
	re      *regexp.Regexp
	lit     string // longest literal part, a cheap pre-check
	litLen  int    // total literal length (more literal text = more specific)
	wrapped []bool // per argument: %w or %v, translate the argument too
	en      string
	// multiline keys match texts with line breaks as a whole
	multiline bool
}

var (
	mu sync.RWMutex
	// exact holds every key (plain texts and patterns), plain only the texts without verbs.
	exact    = map[string]string{}
	plain    = map[string]string{}
	patterns []*pattern
	sorted   bool
	cache    = map[string]string{}
	problems []string
)

// Problems returns conflicting or invalid registrations (empty when the catalogs are
// consistent).
func Problems() []string {
	mu.RLock()
	defer mu.RUnlock()
	return append([]string(nil), problems...)
}

const cacheMax = 8192

// verbRe matches a fmt verb with optional argument index, flags, width and precision. The
// space flag is left out on purpose: German writes "40 % der Zeit", which is no verb.
var verbRe = regexp.MustCompile(`%(\[\d+\])?[-+#0]*\d*(\.\d+)?[a-zA-Z%]`)

// Register adds English translations (German source text → English). A text registered
// twice with different translations keeps the first one, a pattern whose English side does
// not use the same arguments is left out; both are reported by Problems (and fail the
// catalog tests) instead of stopping the server.
func Register(en map[string]string) {
	mu.Lock()
	defer mu.Unlock()
	keys := make([]string, 0, len(en))
	for k := range en {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, de := range keys {
		tr := en[de]
		if prev, ok := exact[de]; ok {
			if prev != tr {
				problems = append(problems, fmt.Sprintf("%q registered twice: %q and %q", de, prev, tr))
			}
			continue
		}
		if strings.TrimSpace(tr) == "" {
			problems = append(problems, fmt.Sprintf("%q: empty translation", de))
			continue
		}
		if err := checkArgs(de, tr); err != nil {
			problems = append(problems, fmt.Sprintf("%q: %v", de, err))
			continue
		}
		exact[de] = tr
		if p := compile(de, tr); p != nil {
			patterns = append(patterns, p)
			sorted = false
		} else {
			plain[de] = tr
		}
	}
	clear(cache)
}

// verbs returns the verbs of a format string ("%%" excluded).
func verbs(s string) []string {
	var out []string
	for _, m := range verbRe.FindAllString(s, -1) {
		if m != "%%" {
			out = append(out, m)
		}
	}
	return out
}

// argIndexes returns the argument numbers (1-based) the verbs of a format string use.
func argIndexes(s string) []int {
	var out []int
	next := 1
	for _, v := range verbs(s) {
		n := next
		if strings.HasPrefix(v, "%[") {
			end := strings.IndexByte(v, ']')
			n, _ = strconv.Atoi(v[2:end])
		}
		out = append(out, n)
		next = n + 1
	}
	return out
}

// checkArgs verifies that the English text uses every argument of the German one.
func checkArgs(de, en string) error {
	want := len(argIndexes(de))
	got := map[int]bool{}
	for _, n := range argIndexes(en) {
		if n < 1 || n > want {
			return fmt.Errorf("argument %d does not exist in the German text", n)
		}
		got[n] = true
	}
	if len(got) != want {
		return fmt.Errorf("English text uses %d of %d arguments", len(got), want)
	}
	return nil
}

// compile turns a key with verbs into a pattern (nil for plain texts).
func compile(de, en string) *pattern {
	locs := verbRe.FindAllStringIndex(de, -1)
	hasVerb := false
	for _, l := range locs {
		if de[l[0]:l[1]] != "%%" {
			hasVerb = true
		}
	}
	if !hasVerb {
		return nil
	}
	var (
		b       strings.Builder
		lits    []string
		wrapped []bool
		last    int
	)
	// a %s never spans lines, unless the key itself has several lines: multi-line texts are
	// translated line by line
	multiline := strings.Contains(de, "\n")
	if multiline {
		b.WriteString(`(?s)`)
	}
	b.WriteString(`^`)
	addLit := func(s string) {
		b.WriteString(regexp.QuoteMeta(s))
		lits = append(lits, s)
	}
	pending := ""
	for _, l := range locs {
		pending += de[last:l[0]]
		v := de[l[0]:l[1]]
		last = l[1]
		if v == "%%" {
			pending += "%"
			continue
		}
		addLit(pending)
		pending = ""
		switch v[len(v)-1] {
		case 'd':
			b.WriteString(`(-?\d+)`)
		case 'f', 'g', 'e':
			b.WriteString(`(-?[\d.]+)`)
		case 'q':
			b.WriteString(`("(?:[^"\\]|\\.)*")`)
		default:
			b.WriteString(`(.*?)`)
		}
		// %w and %v usually carry an error: translate them as well; %s, %q and %d stay as
		// they are (names, addresses, user data)
		wrapped = append(wrapped, v[len(v)-1] == 'w' || v[len(v)-1] == 'v')
	}
	addLit(pending + de[last:])
	b.WriteString(`$`)
	re, err := regexp.Compile(b.String())
	if err != nil {
		panic(fmt.Sprintf("i18n: pattern %q: %v", de, err))
	}
	p := &pattern{key: de, re: re, wrapped: wrapped, en: en, multiline: multiline}
	for _, s := range lits {
		p.litLen += len(s)
		if len(s) > len(p.lit) {
			p.lit = s
		}
	}
	return p
}

// Has reports whether a German text (or pattern key) has a translation.
func Has(de string) bool {
	mu.RLock()
	defer mu.RUnlock()
	_, ok := exact[de]
	return ok
}

// Catalog returns a copy of all registered translations.
func Catalog() map[string]string {
	mu.RLock()
	defer mu.RUnlock()
	out := make(map[string]string, len(exact))
	for k, v := range exact {
		out[k] = v
	}
	return out
}

// ---------------------------------------------------------------- translation

// T translates a German text into the language. It tries the text itself, then the
// patterns, then – for texts with several lines – every line on its own. Texts without a
// translation are returned unchanged.
func T(loc Locale, s string) string {
	if loc != EN || s == "" {
		return s
	}
	if out, ok := lookup(s, false); ok {
		return out
	}
	return s
}

// Err translates an error message. Besides T it translates the parts of an error chain
// ("context: cause") on their own, so a translated cause shows up in English even if the
// wrapping context has no translation.
func Err(loc Locale, s string) string {
	if loc != EN || s == "" {
		return s
	}
	if out, ok := lookup(s, true); ok {
		return out
	}
	return s
}

// Sprintf formats with the translated format string (for code that knows the language
// when it writes the text, e.g. reports).
func Sprintf(loc Locale, format string, args ...any) string {
	f := format
	if loc == EN {
		mu.RLock()
		if tr, ok := exact[format]; ok {
			f = tr
		}
		mu.RUnlock()
	}
	if len(args) == 0 && !strings.Contains(f, "%") {
		return f
	}
	return fmt.Sprintf(strings.ReplaceAll(f, "%w", "%v"), args...)
}

// Tr is shorthand for T with a language taken from a context.
func Tr(ctx context.Context, s string) string { return T(FromContext(ctx), s) }

func lookup(s string, chain bool) (string, bool) {
	key := s
	if chain {
		key = "\x00" + s
	}
	mu.RLock()
	if tr, ok := plain[s]; ok {
		mu.RUnlock()
		return tr, true
	}
	if out, ok := cache[key]; ok {
		mu.RUnlock()
		return out, out != s
	}
	mu.RUnlock()

	out, ok := translate(s, chain, 0)
	mu.Lock()
	if len(cache) >= cacheMax {
		clear(cache)
	}
	if ok {
		cache[key] = out
	} else {
		cache[key] = s
	}
	mu.Unlock()
	return out, ok
}

// translate does the actual work; depth bounds recursion through wrapped errors.
func translate(s string, chain bool, depth int) (string, bool) {
	if depth > 8 || s == "" {
		return s, false
	}
	mu.RLock()
	tr, ok := plain[s]
	mu.RUnlock()
	if ok {
		return tr, true
	}
	multi := strings.Contains(s, "\n")
	// a text with several lines only matches keys with several lines as a whole; otherwise
	// every line is translated on its own
	if out, ok := matchPattern(s, multi, depth); ok {
		return out, true
	}
	if multi {
		lines := strings.Split(s, "\n")
		changed := false
		for i, l := range lines {
			if out, ok := translate(l, chain, depth+1); ok {
				lines[i], changed = out, true
			}
		}
		if changed {
			return strings.Join(lines, "\n"), true
		}
	}
	if chain && strings.Contains(s, ": ") {
		parts := strings.Split(s, ": ")
		changed := false
		for i, p := range parts {
			mu.RLock()
			tr, ok := plain[p]
			mu.RUnlock()
			if ok {
				parts[i], changed = tr, true
				continue
			}
			if out, ok := matchPattern(p, false, depth+1); ok {
				parts[i], changed = out, true
			}
		}
		if changed {
			return strings.Join(parts, ": "), true
		}
	}
	return s, false
}

func sortPatterns() {
	mu.Lock()
	defer mu.Unlock()
	if sorted {
		return
	}
	sort.SliceStable(patterns, func(i, j int) bool {
		if patterns[i].litLen != patterns[j].litLen {
			return patterns[i].litLen > patterns[j].litLen
		}
		return patterns[i].key < patterns[j].key
	})
	sorted = true
}

// nestedMinLiteral is the literal text a pattern needs before it is applied to a %s
// argument: "Eskalation: %s" translates the event title inside, "%s (%s)" would change
// names that merely look like a pattern.
const nestedMinLiteral = 6

func matchPattern(s string, multiline bool, depth int) (string, bool) {
	mu.RLock()
	ready := sorted
	mu.RUnlock()
	if !ready {
		sortPatterns()
	}
	mu.RLock()
	list := patterns
	mu.RUnlock()
	for _, p := range list {
		if p.multiline != multiline || (p.lit != "" && !strings.Contains(s, p.lit)) {
			continue
		}
		m := p.re.FindStringSubmatch(s)
		if m == nil {
			continue
		}
		args := m[1:]
		for i, a := range args {
			if a == "" {
				continue
			}
			if p.wrapped[i] {
				// %w, %v: an error, translated like any error
				if out, ok := translate(a, true, depth+1); ok {
					args[i] = out
				}
			} else if !strings.HasPrefix(a, `"`) && depth < 4 {
				// %s: only a generated text (a specific pattern), never a plain name
				if out, ok := matchNested(a, depth+1); ok {
					args[i] = out
				}
			}
		}
		return fill(p.en, args), true
	}
	return s, false
}

// matchNested translates an argument through the specific patterns only.
func matchNested(s string, depth int) (string, bool) {
	mu.RLock()
	list := patterns
	mu.RUnlock()
	for _, p := range list {
		if p.multiline || p.litLen < nestedMinLiteral || (p.lit != "" && !strings.Contains(s, p.lit)) {
			continue
		}
		if p.re.MatchString(s) {
			return matchPattern(s, false, depth)
		}
	}
	return s, false
}

// fill replaces the verbs of an English pattern by the captured argument texts.
func fill(en string, args []string) string {
	var b strings.Builder
	next := 0
	last := 0
	for _, l := range verbRe.FindAllStringIndex(en, -1) {
		b.WriteString(en[last:l[0]])
		last = l[1]
		v := en[l[0]:l[1]]
		if v == "%%" {
			b.WriteByte('%')
			continue
		}
		n := next
		if strings.HasPrefix(v, "%[") {
			end := strings.IndexByte(v, ']')
			k, _ := strconv.Atoi(v[2:end])
			n = k - 1
		}
		if n >= 0 && n < len(args) {
			b.WriteString(args[n])
		}
		next = n + 1
	}
	b.WriteString(en[last:])
	return b.String()
}
