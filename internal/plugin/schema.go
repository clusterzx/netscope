package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net"
	"net/mail"
	"net/netip"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"netscope/internal/cron"
)

// FieldType is the type of a settings field.
type FieldType string

const (
	FieldString        FieldType = "string"
	FieldSecret        FieldType = "secret"
	FieldInt           FieldType = "int"
	FieldBool          FieldType = "bool"
	FieldCron          FieldType = "cron"
	FieldEnum          FieldType = "enum"
	FieldStringList    FieldType = "string-list"
	FieldSubnetList    FieldType = "subnet-list"
	FieldCredentialRef FieldType = "credential-ref"
	FieldDuration      FieldType = "duration"
)

// SecretMask is returned by the API instead of stored secret values. Sending it back
// unchanged keeps the stored secret.
const SecretMask = "********"

// Option is a selectable value of an enum field.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// Validation constrains a field value.
//
//   - int:      Min/Max bound the value
//   - duration: Min/Max in seconds
//   - string, secret: Min/Max bound the length; Pattern/Format check the content
//   - lists:    Min/Max bound the number of items; Pattern/Format apply to each item
type Validation struct {
	Min     *int64 `json:"min,omitempty"`
	Max     *int64 `json:"max,omitempty"`
	Pattern string `json:"pattern,omitempty"`
	// Format: url | host | hostport | ip | email | path | mac | header
	Format string `json:"format,omitempty"`
}

// Condition shows a field only if another field has one of the given values.
type Condition struct {
	Field  string `json:"field"`
	Equals []any  `json:"equals"`
}

// Field is one settings field. The UI renders forms from these definitions.
type Field struct {
	Key             string      `json:"key"`
	Type            FieldType   `json:"type"`
	Label           string      `json:"label"`
	Description     string      `json:"description,omitempty"`
	Default         any         `json:"default,omitempty"`
	Required        bool        `json:"required,omitempty"`
	Secret          bool        `json:"secret,omitempty"`
	Options         []Option    `json:"options,omitempty"`
	Multi           bool        `json:"multi,omitempty"` // enum, credential-ref: several values
	CredentialTypes []string    `json:"credentialTypes,omitempty"`
	Validation      *Validation `json:"validation,omitempty"`
	Placeholder     string      `json:"placeholder,omitempty"`
	Group           string      `json:"group,omitempty"`
	Advanced        bool        `json:"advanced,omitempty"`
	Multiline       bool        `json:"multiline,omitempty"`
	VisibleIf       *Condition  `json:"visibleIf,omitempty"`
	// Widget is a rendering hint: "file" = string field holding the path of an uploaded file.
	Widget string `json:"widget,omitempty"`
}

// IsSecret reports whether the value must be stored encrypted and masked in the API.
func (f Field) IsSecret() bool { return f.Type == FieldSecret || f.Secret }

// Schema is the list of settings fields of a plugin.
type Schema struct {
	Fields []Field `json:"fields"`
}

// Int64 is a helper for Validation bounds.
func Int64(v int64) *int64 { return &v }

var keyRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

// Check verifies the schema definition itself (called at registration).
func (s Schema) Check() error {
	seen := map[string]bool{}
	for _, f := range s.Fields {
		if !keyRe.MatchString(f.Key) {
			return fmt.Errorf("invalid field key %q", f.Key)
		}
		if seen[f.Key] {
			return fmt.Errorf("duplicate field key %q", f.Key)
		}
		seen[f.Key] = true
		if f.Label == "" {
			return fmt.Errorf("field %s: missing label", f.Key)
		}
		switch f.Type {
		case FieldString, FieldSecret, FieldInt, FieldBool, FieldCron, FieldStringList,
			FieldSubnetList, FieldCredentialRef, FieldDuration:
		case FieldEnum:
			if len(f.Options) == 0 {
				return fmt.Errorf("field %s: enum without options", f.Key)
			}
		default:
			return fmt.Errorf("field %s: unknown type %q", f.Key, f.Type)
		}
		if f.Validation != nil && f.Validation.Pattern != "" {
			if _, err := regexp.Compile(f.Validation.Pattern); err != nil {
				return fmt.Errorf("field %s: bad pattern: %w", f.Key, err)
			}
		}
		if f.Default != nil {
			if _, err := normalizeValue(f, f.Default); err != nil {
				return fmt.Errorf("field %s: invalid default: %w", f.Key, err)
			}
		}
	}
	for _, f := range s.Fields {
		if f.VisibleIf != nil && !seen[f.VisibleIf.Field] {
			return fmt.Errorf("field %s: visibleIf references unknown field %q", f.Key, f.VisibleIf.Field)
		}
	}
	return nil
}

// Field returns the field with the given key.
func (s Schema) Field(key string) (Field, bool) {
	for _, f := range s.Fields {
		if f.Key == key {
			return f, true
		}
	}
	return Field{}, false
}

// Defaults returns the normalized default values of all fields.
func (s Schema) Defaults() map[string]any {
	out := map[string]any{}
	for _, f := range s.Fields {
		out[f.Key] = zeroValue(f)
		if f.Default != nil {
			if v, err := normalizeValue(f, f.Default); err == nil {
				out[f.Key] = v
			}
		}
	}
	return out
}

// Normalize converts stored values to their canonical types, dropping unknown keys and
// replacing invalid values by defaults. It is used when loading settings from the DB
// (JSON numbers arrive as float64, lists as []any).
func (s Schema) Normalize(stored map[string]any) map[string]any {
	out := s.Defaults()
	for _, f := range s.Fields {
		if raw, ok := stored[f.Key]; ok && raw != nil {
			if v, err := normalizeValue(f, raw); err == nil {
				out[f.Key] = v
			}
		}
	}
	return out
}

func zeroValue(f Field) any {
	switch f.Type {
	case FieldInt:
		return int64(0)
	case FieldBool:
		return false
	case FieldStringList, FieldSubnetList:
		return []string{}
	case FieldCredentialRef:
		if f.Multi {
			return []int64{}
		}
		return int64(0)
	case FieldEnum:
		if f.Multi {
			return []string{}
		}
		return ""
	default:
		return ""
	}
}

// FieldError is a validation error for one field.
type FieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// ValidationError aggregates field errors.
type ValidationError struct {
	Errors []FieldError `json:"errors"`
}

// FieldErr returns a validation error for a single field (name as in the JSON body).
func FieldErr(field, message string) error {
	return &ValidationError{Errors: []FieldError{{Field: field, Message: message}}}
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, fe := range e.Errors {
		parts[i] = fe.Field + ": " + fe.Message
	}
	return "ungültige Einstellungen: " + strings.Join(parts, "; ")
}

// CredentialChecker verifies that a credential exists and has one of the allowed types.
type CredentialChecker func(id int64, allowedTypes []string) error

// Validate normalizes input against the schema. Unknown keys are rejected, missing keys
// get their defaults. Secret fields equal to SecretMask are taken from previous (the
// stored values), so callers can round-trip masked settings.
func (s Schema) Validate(input, previous map[string]any, checkCred CredentialChecker) (map[string]any, error) {
	out := s.Defaults()
	var errs []FieldError
	known := map[string]bool{}
	for _, f := range s.Fields {
		known[f.Key] = true
	}
	keys := make([]string, 0, len(input))
	for k := range input {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if !known[k] {
			errs = append(errs, FieldError{Field: k, Message: "unbekanntes Feld"})
		}
	}
	for _, f := range s.Fields {
		raw, present := input[f.Key]
		if f.IsSecret() {
			if str, ok := raw.(string); ok && str == SecretMask {
				raw, present = previous[f.Key], previous[f.Key] != nil
			}
		}
		if !present || raw == nil {
			if f.Required && isEmpty(out[f.Key]) && fieldVisible(s, f, input) {
				errs = append(errs, FieldError{Field: f.Key, Message: "Pflichtfeld"})
			}
			continue
		}
		v, err := normalizeValue(f, raw)
		if err != nil {
			errs = append(errs, FieldError{Field: f.Key, Message: err.Error()})
			continue
		}
		if f.Required && isEmpty(v) && fieldVisible(s, f, input) {
			errs = append(errs, FieldError{Field: f.Key, Message: "Pflichtfeld"})
			continue
		}
		if f.Type == FieldCredentialRef && checkCred != nil {
			for _, id := range credIDs(v) {
				if err := checkCred(id, f.CredentialTypes); err != nil {
					errs = append(errs, FieldError{Field: f.Key, Message: err.Error()})
				}
			}
		}
		out[f.Key] = v
	}
	if len(errs) > 0 {
		return nil, &ValidationError{Errors: errs}
	}
	return out, nil
}

func fieldVisible(s Schema, f Field, input map[string]any) bool {
	if f.VisibleIf == nil {
		return true
	}
	dep, ok := s.Field(f.VisibleIf.Field)
	if !ok {
		return true
	}
	cur, present := input[dep.Key]
	if !present {
		cur = dep.Default
	}
	nv, err := normalizeValue(dep, cur)
	if err != nil {
		return true
	}
	for _, want := range f.VisibleIf.Equals {
		wv, err := normalizeValue(dep, want)
		if err == nil && fmt.Sprint(wv) == fmt.Sprint(nv) {
			return true
		}
	}
	return false
}

func credIDs(v any) []int64 {
	switch x := v.(type) {
	case int64:
		if x > 0 {
			return []int64{x}
		}
	case []int64:
		return x
	}
	return nil
}

func isEmpty(v any) bool {
	switch x := v.(type) {
	case nil:
		return true
	case string:
		return strings.TrimSpace(x) == ""
	case []string:
		return len(x) == 0
	case []int64:
		return len(x) == 0
	case int64:
		return false
	}
	return false
}

func toString(v any) (string, error) {
	switch x := v.(type) {
	case string:
		return x, nil
	case json.Number:
		return x.String(), nil
	case float64, int, int64, bool:
		return fmt.Sprint(x), nil
	}
	return "", fmt.Errorf("Text erwartet")
}

func toInt(v any) (int64, error) {
	switch x := v.(type) {
	case int:
		return int64(x), nil
	case int64:
		return x, nil
	case int32:
		return int64(x), nil
	case float64:
		if x != math.Trunc(x) || math.IsInf(x, 0) || math.IsNaN(x) {
			return 0, fmt.Errorf("Ganzzahl erwartet")
		}
		return int64(x), nil
	case json.Number:
		return x.Int64()
	case string:
		s := strings.TrimSpace(x)
		if s == "" {
			return 0, nil
		}
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("Ganzzahl erwartet")
		}
		return n, nil
	}
	return 0, fmt.Errorf("Ganzzahl erwartet")
}

func toStringList(v any) ([]string, error) {
	var items []string
	switch x := v.(type) {
	case []string:
		items = x
	case []any:
		for _, it := range x {
			s, err := toString(it)
			if err != nil {
				return nil, fmt.Errorf("Liste von Texten erwartet")
			}
			items = append(items, s)
		}
	case string:
		items = strings.FieldsFunc(x, func(r rune) bool { return r == '\n' || r == '\r' })
	default:
		return nil, fmt.Errorf("Liste erwartet")
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s := strings.TrimSpace(it); s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func checkFormat(format, s string) error {
	switch format {
	case "":
		return nil
	case "url":
		u, err := url.Parse(s)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return fmt.Errorf("gültige URL erwartet (z. B. https://host/pfad)")
		}
		if u.Scheme != "http" && u.Scheme != "https" {
			return fmt.Errorf("nur http:// oder https:// erlaubt")
		}
	case "host":
		if strings.ContainsAny(s, " /:") && net.ParseIP(s) == nil {
			return fmt.Errorf("Hostname oder IP erwartet")
		}
	case "hostport":
		host, port, err := net.SplitHostPort(s)
		if err != nil || host == "" {
			return fmt.Errorf("host:port erwartet")
		}
		if p, err := strconv.Atoi(port); err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("ungültiger Port")
		}
	case "ip":
		if _, err := netip.ParseAddr(s); err != nil {
			return fmt.Errorf("IP-Adresse erwartet")
		}
	case "email":
		if _, err := mail.ParseAddress(s); err != nil {
			return fmt.Errorf("E-Mail-Adresse erwartet")
		}
	case "path":
		if !strings.HasPrefix(s, "/") && !(len(s) > 2 && s[1] == ':') {
			return fmt.Errorf("absoluter Pfad erwartet")
		}
	case "mac":
		if _, err := net.ParseMAC(s); err != nil {
			return fmt.Errorf("MAC-Adresse erwartet")
		}
	case "header":
		name, _, ok := strings.Cut(s, ":")
		if !ok || strings.TrimSpace(name) == "" || strings.ContainsAny(strings.TrimSpace(name), " \t") {
			return fmt.Errorf("Format \"Name: Wert\" erwartet")
		}
	default:
		return fmt.Errorf("unbekanntes Format %q", format)
	}
	return nil
}

func checkText(f Field, s string) error {
	if f.Validation == nil {
		return nil
	}
	if s == "" {
		return nil
	}
	if f.Validation.Pattern != "" {
		re, err := regexp.Compile(f.Validation.Pattern)
		if err != nil {
			return err
		}
		if !re.MatchString(s) {
			return fmt.Errorf("Wert %q entspricht nicht dem erwarteten Muster", s)
		}
	}
	return checkFormat(f.Validation.Format, s)
}

func checkBounds(f Field, n int64, what string) error {
	if f.Validation == nil {
		return nil
	}
	if f.Validation.Min != nil && n < *f.Validation.Min {
		return fmt.Errorf("%s muss mindestens %d sein", what, *f.Validation.Min)
	}
	if f.Validation.Max != nil && n > *f.Validation.Max {
		return fmt.Errorf("%s darf höchstens %d sein", what, *f.Validation.Max)
	}
	return nil
}

func normalizeValue(f Field, raw any) (any, error) {
	switch f.Type {
	case FieldString, FieldSecret:
		s, err := toString(raw)
		if err != nil {
			return nil, err
		}
		if !f.Multiline {
			s = strings.TrimSpace(s)
		}
		if f.Validation != nil && (f.Validation.Min != nil || f.Validation.Max != nil) {
			if err := checkBounds(f, int64(len([]rune(s))), "Länge"); err != nil {
				return nil, err
			}
		}
		if err := checkText(f, s); err != nil {
			return nil, err
		}
		return s, nil
	case FieldInt:
		n, err := toInt(raw)
		if err != nil {
			return nil, err
		}
		if err := checkBounds(f, n, "Wert"); err != nil {
			return nil, err
		}
		return n, nil
	case FieldBool:
		switch x := raw.(type) {
		case bool:
			return x, nil
		case string:
			b, err := strconv.ParseBool(strings.TrimSpace(x))
			if err != nil {
				return nil, fmt.Errorf("Ja/Nein erwartet")
			}
			return b, nil
		}
		return nil, fmt.Errorf("Ja/Nein erwartet")
	case FieldCron:
		s, err := toString(raw)
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return "", nil
		}
		if err := cron.Validate(s); err != nil {
			return nil, err
		}
		return s, nil
	case FieldEnum:
		allowed := map[string]bool{}
		for _, o := range f.Options {
			allowed[o.Value] = true
		}
		if f.Multi {
			list, err := toStringList(raw)
			if err != nil {
				return nil, err
			}
			for _, v := range list {
				if !allowed[v] {
					return nil, fmt.Errorf("ungültige Auswahl %q", v)
				}
			}
			return list, nil
		}
		s, err := toString(raw)
		if err != nil {
			return nil, err
		}
		if s != "" && !allowed[s] {
			return nil, fmt.Errorf("ungültige Auswahl %q", s)
		}
		return s, nil
	case FieldStringList:
		list, err := toStringList(raw)
		if err != nil {
			return nil, err
		}
		for _, it := range list {
			if err := checkText(f, it); err != nil {
				return nil, err
			}
		}
		if err := checkBounds(f, int64(len(list)), "Anzahl der Einträge"); err != nil {
			return nil, err
		}
		return list, nil
	case FieldSubnetList:
		list, err := toStringList(raw)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, len(list))
		for _, it := range list {
			p, err := ParsePrefix(it)
			if err != nil {
				return nil, err
			}
			out = append(out, p.String())
		}
		if err := checkBounds(f, int64(len(out)), "Anzahl der Einträge"); err != nil {
			return nil, err
		}
		return out, nil
	case FieldCredentialRef:
		if f.Multi {
			var ids []int64
			switch x := raw.(type) {
			case []int64:
				ids = x
			case []any:
				for _, it := range x {
					n, err := toInt(it)
					if err != nil {
						return nil, fmt.Errorf("Credential-ID erwartet")
					}
					if n > 0 {
						ids = append(ids, n)
					}
				}
			default:
				return nil, fmt.Errorf("Liste von Credential-IDs erwartet")
			}
			if ids == nil {
				ids = []int64{}
			}
			return ids, nil
		}
		n, err := toInt(raw)
		if err != nil {
			return nil, fmt.Errorf("Credential-ID erwartet")
		}
		if n < 0 {
			return nil, fmt.Errorf("Credential-ID erwartet")
		}
		return n, nil
	case FieldDuration:
		s, err := toString(raw)
		if err != nil {
			return nil, err
		}
		s = strings.TrimSpace(s)
		if s == "" {
			return "", nil
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return nil, fmt.Errorf("Dauer erwartet (z. B. 30s, 5m, 2h)")
		}
		if d < 0 {
			return nil, fmt.Errorf("Dauer darf nicht negativ sein")
		}
		if err := checkBounds(f, int64(d/time.Second), "Dauer in Sekunden"); err != nil {
			return nil, err
		}
		return d.String(), nil
	}
	return nil, fmt.Errorf("unbekannter Typ %q", f.Type)
}

// ParsePrefix parses a CIDR or single address (as /32 or /128) and masks host bits.
func ParsePrefix(s string) (netip.Prefix, error) {
	s = strings.TrimSpace(s)
	if !strings.Contains(s, "/") {
		a, err := netip.ParseAddr(s)
		if err != nil {
			return netip.Prefix{}, fmt.Errorf("ungültiges Subnetz %q", s)
		}
		return netip.PrefixFrom(a.Unmap(), a.Unmap().BitLen()), nil
	}
	p, err := netip.ParsePrefix(s)
	if err != nil {
		return netip.Prefix{}, fmt.Errorf("ungültiges Subnetz %q", s)
	}
	return netip.PrefixFrom(p.Addr().Unmap(), p.Bits()).Masked(), nil
}

// MaskSecrets returns a copy of values with set secret fields replaced by SecretMask.
func (s Schema) MaskSecrets(values map[string]any) map[string]any {
	out := make(map[string]any, len(values))
	for k, v := range values {
		out[k] = v
	}
	for _, f := range s.Fields {
		if f.IsSecret() {
			if str, _ := out[f.Key].(string); str != "" {
				out[f.Key] = SecretMask
			} else {
				out[f.Key] = ""
			}
		}
	}
	return out
}

// SplitSecrets separates secret field values from the rest.
func (s Schema) SplitSecrets(values map[string]any) (public, secret map[string]any) {
	public, secret = map[string]any{}, map[string]any{}
	for k, v := range values {
		public[k] = v
	}
	for _, f := range s.Fields {
		if f.IsSecret() {
			if v, ok := public[f.Key]; ok {
				secret[f.Key] = v
				delete(public, f.Key)
			}
		}
	}
	return public, secret
}

// ---------------------------------------------------------------- Settings

// Settings gives typed access to validated plugin settings.
type Settings struct {
	values map[string]any
}

// NewSettings wraps normalized values (output of Schema.Validate / Defaults).
func NewSettings(values map[string]any) Settings {
	if values == nil {
		values = map[string]any{}
	}
	return Settings{values: values}
}

// Map returns the underlying values.
func (s Settings) Map() map[string]any { return s.values }

// String returns a string field ("" if unset).
func (s Settings) String(key string) string {
	v, _ := s.values[key].(string)
	return v
}

// Int returns an int field.
func (s Settings) Int(key string) int {
	switch x := s.values[key].(type) {
	case int64:
		return int(x)
	case int:
		return x
	case float64:
		return int(x)
	}
	return 0
}

// Bool returns a bool field.
func (s Settings) Bool(key string) bool {
	b, _ := s.values[key].(bool)
	return b
}

// Duration returns a duration field (0 if unset).
func (s Settings) Duration(key string) time.Duration {
	d, _ := time.ParseDuration(s.String(key))
	return d
}

// StringList returns a string-list, subnet-list or multi-enum field.
func (s Settings) StringList(key string) []string {
	switch x := s.values[key].(type) {
	case []string:
		return append([]string(nil), x...)
	case []any:
		out := make([]string, 0, len(x))
		for _, it := range x {
			if str, ok := it.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// Prefixes returns a subnet-list field as parsed prefixes.
func (s Settings) Prefixes(key string) []netip.Prefix {
	var out []netip.Prefix
	for _, it := range s.StringList(key) {
		if p, err := ParsePrefix(it); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// CredentialID returns a single credential-ref field (0 = none).
func (s Settings) CredentialID(key string) int64 {
	switch x := s.values[key].(type) {
	case int64:
		return x
	case int:
		return int64(x)
	case float64:
		return int64(x)
	}
	return 0
}

// CredentialIDs returns a multi credential-ref field (or the single value as a list).
func (s Settings) CredentialIDs(key string) []int64 {
	switch x := s.values[key].(type) {
	case []int64:
		return append([]int64(nil), x...)
	case []any:
		var out []int64
		for _, it := range x {
			if n, err := toInt(it); err == nil && n > 0 {
				out = append(out, n)
			}
		}
		return out
	}
	if id := s.CredentialID(key); id > 0 {
		return []int64{id}
	}
	return nil
}

// ErrNoCredential is returned when a required credential is not configured.
var ErrNoCredential = errors.New("kein Credential konfiguriert")
