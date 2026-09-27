package i18n

import (
	"context"
	"strings"
	"testing"
)

func init() {
	Register(map[string]string{
		"Testgerät neu":                   "Test device new",
		"Testgerät neu: %s":               "New test device: %s",
		"Test-Port %d/%s neu auf %s":      "New test port %d/%s on %s",
		"%s auf %s trifft testweise zu":   "%[1]s applies to %[2]s in the test",
		"Testlauf fehlgeschlagen: %w":     "Test run failed: %w",
		"Testzeitüberschreitung":          "Test timeout",
		"Testwert %q ist ungültig":        "Test value %q is invalid",
		"%s: Test zu %.0f %% belegt":      "%s: test %.0f%% used",
		"Test-Auslastung (%)":             "Test utilisation (%)",
		"Nach 40 % der Testzeit":          "After 40% of the test time",
		"Testabschnitt":                   "Test section",
		"Testumgebung %s wird %s geprüft": "Checking test environment %[2]s: %[1]s",
	})
}

func TestParseAndAccept(t *testing.T) {
	for in, want := range map[string]Locale{"de": DE, "EN": EN, "en-GB": EN, "de_AT": DE} {
		if got, ok := Parse(in); !ok || got != want {
			t.Errorf("Parse(%q) = %q, %v", in, got, ok)
		}
	}
	for _, in := range []string{"", "fr", "english", "e"} {
		if _, ok := Parse(in); ok {
			t.Errorf("Parse(%q) accepted", in)
		}
	}
	cases := map[string]Locale{
		"":                               DE,
		"fr-FR,fr;q=0.9":                 DE,
		"en-US,en;q=0.9,de;q=0.8":        EN,
		"de-DE,de;q=0.9,en-US;q=0.8":     DE,
		"fr-FR,en;q=0.5,de;q=0.7":        DE,
		"fr, en-GB;q=0.8":                EN,
		"de;q=0, en":                     EN,
		" en-us ; q=0.3 , de-ch ; q=0.2": EN,
	}
	for in, want := range cases {
		if got := Accept(in); got != want {
			t.Errorf("Accept(%q) = %q, want %q", in, got, want)
		}
	}
	if got := Pick("en", "de-DE"); got != EN {
		t.Errorf("preference must win, got %q", got)
	}
	if got := Pick("", "en-US"); got != EN {
		t.Errorf("browser language without preference, got %q", got)
	}
	if got := Pick("xx", ""); got != DE {
		t.Errorf("default, got %q", got)
	}
}

func TestContext(t *testing.T) {
	if FromContext(context.Background()) != DE {
		t.Fatal("default locale expected")
	}
	ctx := WithLocale(context.Background(), EN)
	if FromContext(ctx) != EN || Tr(ctx, "Testzeitüberschreitung") != "Test timeout" {
		t.Fatal("locale from context not used")
	}
}

func TestTranslate(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Testgerät neu", "Test device new"},
		{"Testgerät neu: Drucker (10.0.0.5)", "New test device: Drucker (10.0.0.5)"},
		{"Test-Port 443/tcp neu auf nas", "New test port 443/tcp on nas"},
		{"CVE-2024-1 auf nas.lan trifft testweise zu", "CVE-2024-1 applies to nas.lan in the test"},
		{"Testwert \"a b\" ist ungültig", "Test value \"a b\" is invalid"},
		{"srv: Test zu 93 % belegt", "srv: test 93% used"},
		{"Test-Auslastung (%)", "Test utilisation (%)"},
		{"Nach 40 % der Testzeit", "After 40% of the test time"},
		{"Testumgebung lab wird heute geprüft", "Checking test environment heute: lab"},
		// wrapped errors are translated recursively
		{"Testlauf fehlgeschlagen: Testzeitüberschreitung", "Test run failed: Test timeout"},
		{"Testlauf fehlgeschlagen: Testlauf fehlgeschlagen: Testzeitüberschreitung", "Test run failed: Test run failed: Test timeout"},
		// line by line
		{"Testabschnitt\nTestgerät neu: x\nkeinTestwort", "Test section\nNew test device: x\nkeinTestwort"},
		// a %s never swallows the following lines
		{"Testgerät neu: a\nb", "New test device: a\nb"},
		// a generated text inside a %s argument is translated through its (specific) pattern
		{"Testumgebung Testgerät neu: a wird heute geprüft", "Checking test environment heute: New test device: a"},
		// unknown texts stay as they are
		{"völlig keinTestwort", "völlig keinTestwort"},
		{"", ""},
	}
	for _, c := range cases {
		if got := T(EN, c.in); got != c.want {
			t.Errorf("T(%q) = %q, want %q", c.in, got, c.want)
		}
		if got := T(DE, c.in); got != c.in {
			t.Errorf("German must stay unchanged: %q → %q", c.in, got)
		}
	}
	// the cache must not change results
	for i := 0; i < 3; i++ {
		if got := T(EN, "Testgerät neu: a"); got != "New test device: a" {
			t.Fatalf("cached result %q", got)
		}
	}
}

func TestErrChain(t *testing.T) {
	// the context "ssh 10.0.0.1" has no translation, the cause has
	if got := Err(EN, "ssh 10.0.0.1: Testzeitüberschreitung"); got != "ssh 10.0.0.1: Test timeout" {
		t.Errorf("chain: %q", got)
	}
	if got := T(EN, "ssh 10.0.0.1: Testzeitüberschreitung"); got != "ssh 10.0.0.1: Testzeitüberschreitung" {
		t.Errorf("T must not split chains: %q", got)
	}
	if got := Err(EN, "a: b: c"); got != "a: b: c" {
		t.Errorf("unknown chain changed: %q", got)
	}
}

func TestSprintf(t *testing.T) {
	if got := Sprintf(EN, "Test-Port %d/%s neu auf %s", 22, "tcp", "srv"); got != "New test port 22/tcp on srv" {
		t.Errorf("Sprintf EN: %q", got)
	}
	if got := Sprintf(DE, "Test-Port %d/%s neu auf %s", 22, "tcp", "srv"); got != "Test-Port 22/tcp neu auf srv" {
		t.Errorf("Sprintf DE: %q", got)
	}
	if got := Sprintf(EN, "%s auf %s trifft testweise zu", "CVE-1", "srv"); got != "CVE-1 applies to srv in the test" {
		t.Errorf("Sprintf reorder: %q", got)
	}
	if got := Sprintf(EN, "Testgerät neu"); got != "Test device new" {
		t.Errorf("Sprintf plain: %q", got)
	}
}

func TestRegisterChecks(t *testing.T) {
	base := len(Problems())
	mustFail := func(name string, m map[string]string) {
		t.Helper()
		before := len(Problems())
		Register(m)
		if len(Problems()) != before+1 {
			t.Errorf("%s: not reported", name)
		}
		problems = problems[:before]
	}
	mustFail("conflict", map[string]string{"Testgerät neu": "Something else"})
	mustFail("missing argument", map[string]string{"Fehlt %s und %s": "Missing %s"})
	mustFail("unknown argument", map[string]string{"Fehlt %s": "Missing %[2]s"})
	mustFail("empty", map[string]string{"Leer": " "})
	if T(EN, "Testgerät neu") != "Test device new" || Has("Fehlt %s") || Has("Leer") {
		t.Error("invalid registrations must not change the catalog")
	}
	// the same translation twice is fine
	Register(map[string]string{"Testgerät neu": "Test device new"})
	if len(Problems()) != base {
		t.Errorf("problems: %v", Problems()[base:])
	}
	if !Has("Testgerät neu: %s") || Has("gibt es nicht") {
		t.Error("Has")
	}
	if c := Catalog(); c["Testabschnitt"] != "Test section" {
		t.Error("Catalog")
	}
}

func TestVerbs(t *testing.T) {
	if v := verbs("zu %.0f %% belegt, %[2]s und %q"); strings.Join(v, ",") != "%.0f,%[2]s,%q" {
		t.Errorf("verbs: %v", v)
	}
	if v := verbs("Nach 40 % der Zeit"); len(v) != 0 {
		t.Errorf("space is no flag: %v", v)
	}
}
