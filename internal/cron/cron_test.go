package cron

import (
	"testing"
	"time"
)

func mustLoc(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestParseErrors(t *testing.T) {
	bad := []string{"", "* * * *", "60 * * * *", "* 24 * * *", "* * 0 * *", "* * * 13 *",
		"*/0 * * * *", "5-1 * * * *", "@every 1s", "@every x", "@foo", "a b c d e"}
	for _, e := range bad {
		if _, err := Parse(e); err == nil {
			t.Errorf("Parse(%q): expected error", e)
		}
	}
}

func TestNext(t *testing.T) {
	loc := mustLoc(t)
	base := time.Date(2026, 9, 22, 10, 7, 30, 0, loc) // Tuesday
	cases := []struct {
		expr string
		want time.Time
	}{
		{"*/5 * * * *", time.Date(2026, 9, 22, 10, 10, 0, 0, loc)},
		{"0 3 * * *", time.Date(2026, 9, 23, 3, 0, 0, 0, loc)},
		{"0 8 * * 1", time.Date(2026, 9, 28, 8, 0, 0, 0, loc)},
		{"0 8 * * mon-fri", time.Date(2026, 9, 23, 8, 0, 0, 0, loc)},
		{"30 10 * * *", time.Date(2026, 9, 22, 10, 30, 0, 0, loc)},
		{"0 0 1 * *", time.Date(2026, 10, 1, 0, 0, 0, 0, loc)},
		{"0 0 1 1 *", time.Date(2027, 1, 1, 0, 0, 0, 0, loc)},
		{"@hourly", time.Date(2026, 9, 22, 11, 0, 0, 0, loc)},
		{"0 12 * * 7", time.Date(2026, 9, 27, 12, 0, 0, 0, loc)},
		// dom and dow both restricted: either matches
		{"0 0 25 * 3", time.Date(2026, 9, 23, 0, 0, 0, 0, loc)},
		{"0 0 29 2 *", time.Date(2028, 2, 29, 0, 0, 0, 0, loc)},
	}
	for _, c := range cases {
		s, err := Parse(c.expr)
		if err != nil {
			t.Fatalf("Parse(%q): %v", c.expr, err)
		}
		if got := s.Next(base); !got.Equal(c.want) {
			t.Errorf("%q: Next = %v, want %v", c.expr, got, c.want)
		}
	}
}

func TestNextDST(t *testing.T) {
	loc := mustLoc(t)
	// 2026-03-29 02:00 CET -> 03:00 CEST; a 02:30 job must not run twice or loop.
	s, _ := Parse("30 2 * * *")
	got := s.Next(time.Date(2026, 3, 28, 12, 0, 0, 0, loc))
	if got.IsZero() || got.Before(time.Date(2026, 3, 28, 12, 0, 0, 0, loc)) {
		t.Fatalf("unexpected %v", got)
	}
	s2, _ := Parse("0 3 * * *")
	got = s2.Next(time.Date(2026, 3, 29, 0, 0, 0, 0, loc))
	if want := time.Date(2026, 3, 29, 3, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("DST: got %v want %v", got, want)
	}
}

func TestEvery(t *testing.T) {
	s, err := Parse("@every 5m")
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 22, 10, 7, 30, 0, time.UTC)
	if got, want := s.Next(base), time.Date(2026, 9, 22, 10, 10, 0, 0, time.UTC); !got.Equal(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if n := NextN(s, base, 3); len(n) != 3 || n[2].Sub(n[0]) != 10*time.Minute {
		t.Fatalf("NextN: %v", n)
	}
}

func TestDescribe(t *testing.T) {
	cases := map[string]string{
		"*/5 * * * *":     "Alle 5 Minuten",
		"* * * * *":       "Jede Minute",
		"0 3 * * *":       "Täglich um 03:00",
		"0 * * * *":       "Stündlich zur vollen Stunde",
		"15 * * * *":      "Stündlich zur Minute 15",
		"0 */6 * * *":     "Alle 6 Stunden zur vollen Stunde",
		"0 8 * * 1-5":     "Werktags (Mo–Fr) um 08:00",
		"0 8 * * 1":       "Montags um 08:00",
		"0 0 1 * *":       "Am 1. jedes Monats um 00:00",
		"0 8,20 * * *":    "Täglich um 08:00 und 20:00",
		"*/15 8-18 * * *": "Alle 15 Minuten zwischen 08:00 und 18:59",
		"0 0 * * 0,6":     "Am Wochenende um 00:00",
		"0 4 * 1 *":       "Täglich um 04:00 im Januar",
		"@every 90s":      "Alle 1 Minute 30 Sekunden",
		"@every 2h":       "Alle 2 Stunden",
		"@daily":          "Täglich um 00:00",
		"@weekly":         "Sonntags um 00:00",
	}
	for expr, want := range cases {
		got, err := Describe(expr)
		if err != nil {
			t.Errorf("Describe(%q): %v", expr, err)
			continue
		}
		if got != want {
			t.Errorf("Describe(%q) = %q, want %q", expr, got, want)
		}
	}
}
