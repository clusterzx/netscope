package plugin

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func testSchema() Schema {
	return Schema{Fields: []Field{
		{Key: "name", Type: FieldString, Label: "Name", Required: true, Validation: &Validation{Min: Int64(2), Max: Int64(10)}},
		{Key: "url", Type: FieldString, Label: "URL", Validation: &Validation{Format: "url"}},
		{Key: "token", Type: FieldSecret, Label: "Token"},
		{Key: "count", Type: FieldInt, Label: "Anzahl", Default: 3, Validation: &Validation{Min: Int64(1), Max: Int64(10)}},
		{Key: "enabled", Type: FieldBool, Label: "Aktiv", Default: true},
		{Key: "schedule", Type: FieldCron, Label: "Zeitplan"},
		{Key: "mode", Type: FieldEnum, Label: "Modus", Default: "a", Options: []Option{{"a", "A"}, {"b", "B"}}},
		{Key: "kinds", Type: FieldEnum, Label: "Arten", Multi: true, Options: []Option{{"x", "X"}, {"y", "Y"}}},
		{Key: "hosts", Type: FieldStringList, Label: "Hosts", Validation: &Validation{Format: "host", Max: Int64(3)}},
		{Key: "nets", Type: FieldSubnetList, Label: "Netze"},
		{Key: "cred", Type: FieldCredentialRef, Label: "Zugang", CredentialTypes: []string{CredSSH}},
		{Key: "creds", Type: FieldCredentialRef, Label: "Zugänge", Multi: true},
		{Key: "timeout", Type: FieldDuration, Label: "Timeout", Default: "5s", Validation: &Validation{Min: Int64(1), Max: Int64(60)}},
		{Key: "b_only", Type: FieldString, Label: "Nur B", Required: true, VisibleIf: &Condition{Field: "mode", Equals: []any{"b"}}},
	}}
}

func TestSchemaCheck(t *testing.T) {
	if err := testSchema().Check(); err != nil {
		t.Fatal(err)
	}
	bad := []Schema{
		{Fields: []Field{{Key: "Bad-Key", Type: FieldString, Label: "x"}}},
		{Fields: []Field{{Key: "a", Type: FieldString, Label: "x"}, {Key: "a", Type: FieldInt, Label: "y"}}},
		{Fields: []Field{{Key: "a", Type: FieldString}}},
		{Fields: []Field{{Key: "a", Type: "json", Label: "x"}}},
		{Fields: []Field{{Key: "a", Type: FieldEnum, Label: "x"}}},
		{Fields: []Field{{Key: "a", Type: FieldInt, Label: "x", Default: "abc"}}},
		{Fields: []Field{{Key: "a", Type: FieldString, Label: "x", Validation: &Validation{Pattern: "("}}}},
		{Fields: []Field{{Key: "a", Type: FieldString, Label: "x", VisibleIf: &Condition{Field: "missing"}}}},
	}
	for i, s := range bad {
		if err := s.Check(); err == nil {
			t.Errorf("schema %d: expected error", i)
		}
	}
}

func TestSchemaValidateNormalizes(t *testing.T) {
	s := testSchema()
	in := map[string]any{
		"name":     "  test ",
		"url":      "https://example.org/x",
		"token":    "geheim",
		"count":    float64(7), // JSON numbers arrive as float64
		"enabled":  "false",
		"schedule": "*/5 * * * *",
		"mode":     "a",
		"kinds":    []any{"x", "y"},
		"hosts":    "a.lan\nb.lan\n\n",
		"nets":     []any{"192.168.8.7/24", "10.0.0.1"},
		"cred":     float64(4),
		"creds":    []any{float64(1), float64(2)},
		"timeout":  "1m",
	}
	out, err := s.Validate(in, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"name": "test", "url": "https://example.org/x", "token": "geheim", "count": int64(7), "enabled": false,
		"schedule": "*/5 * * * *", "mode": "a", "kinds": []string{"x", "y"}, "hosts": []string{"a.lan", "b.lan"},
		"nets": []string{"192.168.8.0/24", "10.0.0.1/32"}, "cred": int64(4), "creds": []int64{1, 2}, "timeout": "1m0s", "b_only": "",
	}
	if !reflect.DeepEqual(out, want) {
		t.Fatalf("got  %#v\nwant %#v", out, want)
	}
	st := NewSettings(out)
	if st.Int("count") != 7 || st.Duration("timeout").Seconds() != 60 || len(st.Prefixes("nets")) != 2 || st.CredentialID("cred") != 4 ||
		len(st.CredentialIDs("creds")) != 2 || st.Bool("enabled") {
		t.Fatalf("typed accessors wrong: %+v", st)
	}
}

func TestSchemaValidateErrors(t *testing.T) {
	s := testSchema()
	cases := []struct {
		name  string
		in    map[string]any
		field string
	}{
		{"missing required", map[string]any{}, "name"},
		{"too short", map[string]any{"name": "a"}, "name"},
		{"bad url", map[string]any{"name": "ok", "url": "ftp://x"}, "url"},
		{"int bounds", map[string]any{"name": "ok", "count": 99}, "count"},
		{"fraction", map[string]any{"name": "ok", "count": 1.5}, "count"},
		{"bad bool", map[string]any{"name": "ok", "enabled": "vielleicht"}, "enabled"},
		{"bad cron", map[string]any{"name": "ok", "schedule": "* * *"}, "schedule"},
		{"bad enum", map[string]any{"name": "ok", "mode": "c"}, "mode"},
		{"bad multi enum", map[string]any{"name": "ok", "kinds": []any{"z"}}, "kinds"},
		{"list too long", map[string]any{"name": "ok", "hosts": []any{"a", "b", "c", "d"}}, "hosts"},
		{"bad host", map[string]any{"name": "ok", "hosts": []any{"a b"}}, "hosts"},
		{"bad subnet", map[string]any{"name": "ok", "nets": []any{"999.1.1.1/24"}}, "nets"},
		{"bad duration", map[string]any{"name": "ok", "timeout": "ewig"}, "timeout"},
		{"duration bounds", map[string]any{"name": "ok", "timeout": "2h"}, "timeout"},
		{"unknown key", map[string]any{"name": "ok", "extra": 1}, "extra"},
		{"visibleIf required", map[string]any{"name": "ok", "mode": "b"}, "b_only"},
	}
	for _, c := range cases {
		_, err := s.Validate(c.in, nil, nil)
		var ve *ValidationError
		if !errors.As(err, &ve) {
			t.Errorf("%s: expected ValidationError, got %v", c.name, err)
			continue
		}
		found := false
		for _, fe := range ve.Errors {
			if fe.Field == c.field {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no error for field %s: %v", c.name, c.field, ve)
		}
	}
	// hidden required fields are not required
	if _, err := s.Validate(map[string]any{"name": "ok", "mode": "a"}, nil, nil); err != nil {
		t.Errorf("hidden field required: %v", err)
	}
}

func TestSecretsMaskRoundtrip(t *testing.T) {
	s := testSchema()
	stored, err := s.Validate(map[string]any{"name": "ok", "token": "s3cret"}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	masked := s.MaskSecrets(stored)
	if masked["token"] != SecretMask {
		t.Fatalf("secret not masked: %v", masked["token"])
	}
	if strings.Contains(strings.Join([]string{masked["token"].(string)}, ""), "s3cret") {
		t.Fatal("secret leaked")
	}
	// sending the mask back keeps the secret, empty clears it
	again, err := s.Validate(masked, stored, nil)
	if err != nil || again["token"] != "s3cret" {
		t.Fatalf("mask roundtrip: %v %v", again["token"], err)
	}
	masked["token"] = ""
	cleared, _ := s.Validate(masked, stored, nil)
	if cleared["token"] != "" {
		t.Fatal("secret not cleared")
	}
	pub, sec := s.SplitSecrets(stored)
	if _, ok := pub["token"]; ok || sec["token"] != "s3cret" {
		t.Fatal("split secrets")
	}
}

func TestCredentialChecker(t *testing.T) {
	s := testSchema()
	check := func(id int64, types []string) error {
		if id == 9 {
			return errors.New("Credential 9 existiert nicht")
		}
		return nil
	}
	if _, err := s.Validate(map[string]any{"name": "ok", "cred": 9}, nil, check); err == nil {
		t.Fatal("missing credential accepted")
	}
	if _, err := s.Validate(map[string]any{"name": "ok", "cred": 3}, nil, check); err != nil {
		t.Fatal(err)
	}
}

func TestNormalizeIgnoresUnknownAndInvalid(t *testing.T) {
	s := testSchema()
	out := s.Normalize(map[string]any{"count": "not a number", "removed_field": true, "nets": []any{"10.1.2.3/8"}})
	if out["count"] != int64(3) {
		t.Errorf("invalid value must fall back to default: %v", out["count"])
	}
	if _, ok := out["removed_field"]; ok {
		t.Error("unknown key kept")
	}
	if !reflect.DeepEqual(out["nets"], []string{"10.0.0.0/8"}) {
		t.Errorf("nets %v", out["nets"])
	}
}

func TestCredentialTypeSchemasAreValid(t *testing.T) {
	for _, ct := range CredentialTypes() {
		if err := ct.Schema.Check(); err != nil {
			t.Errorf("%s: %v", ct.Type, err)
		}
	}
}

func TestEventCatalog(t *testing.T) {
	seen := map[string]bool{}
	for _, e := range Catalog() {
		if seen[e.Type] {
			t.Errorf("duplicate %s", e.Type)
		}
		seen[e.Type] = true
		if !e.DefaultSeverity.Valid() || e.Label == "" || e.Category == "" {
			t.Errorf("incomplete spec %+v", e)
		}
	}
	if !MatchEventType("port.*", "port.opened") || MatchEventType("port.*", "device.new") || !MatchEventType("*", "x") {
		t.Error("MatchEventType")
	}
	if SeverityFromCVSS(9.8) != SevCritical || SeverityFromCVSS(7) != SevHigh || SeverityFromCVSS(0) != SevInfo {
		t.Error("SeverityFromCVSS")
	}
}
