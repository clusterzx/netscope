// Package cron parses standard 5-field cron expressions (minute hour day-of-month month
// day-of-week), the usual @macros and "@every <duration>", computes next activation
// times and renders a German plain-text description for the UI.
package cron

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Schedule computes activation times.
type Schedule interface {
	// Next returns the first activation strictly after t, or the zero time if none
	// exists within the next five years.
	Next(t time.Time) time.Time
}

// MinEvery is the smallest interval accepted for "@every".
const MinEvery = 10 * time.Second

// Every fires at fixed intervals.
type Every struct{ D time.Duration }

// Next implements Schedule. Intervals are aligned to the interval boundary so that
// restarts do not shift the rhythm (e.g. @every 5m fires at :00, :05, ...).
func (e Every) Next(t time.Time) time.Time {
	if e.D <= 0 {
		return time.Time{}
	}
	n := t.Truncate(e.D).Add(e.D)
	if !n.After(t) {
		n = n.Add(e.D)
	}
	return n
}

type part struct{ lo, hi, step int }

type field struct {
	bits  uint64
	star  bool // "*" or "?"
	parts []part
	min   int
	max   int
}

func (f field) has(v int) bool { return f.bits&(1<<uint(v)) != 0 }

func (f field) values() []int {
	var out []int
	for v := f.min; v <= f.max; v++ {
		if f.has(v) {
			out = append(out, v)
		}
	}
	return out
}

// Spec is a parsed 5-field expression.
type Spec struct {
	Expr                          string
	minute, hour, dom, month, dow field
}

var macros = map[string]string{
	"@yearly":   "0 0 1 1 *",
	"@annually": "0 0 1 1 *",
	"@monthly":  "0 0 1 * *",
	"@weekly":   "0 0 * * 0",
	"@daily":    "0 0 * * *",
	"@midnight": "0 0 * * *",
	"@hourly":   "0 * * * *",
}

var monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6,
	"jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}
var dowNames = map[string]int{"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

// Parse parses a cron expression.
func Parse(expr string) (Schedule, error) {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return nil, fmt.Errorf("leerer Ausdruck")
	}
	lower := strings.ToLower(expr)
	if strings.HasPrefix(lower, "@every") {
		arg := strings.TrimSpace(expr[len("@every"):])
		d, err := time.ParseDuration(arg)
		if err != nil {
			return nil, fmt.Errorf("ungültige Dauer %q: %w", arg, err)
		}
		if d < MinEvery {
			return nil, fmt.Errorf("Intervall muss mindestens %s sein", MinEvery)
		}
		return Every{D: d}, nil
	}
	if m, ok := macros[lower]; ok {
		s, err := parseSpec(m)
		if err != nil {
			return nil, err
		}
		s.Expr = expr
		return s, nil
	}
	if strings.HasPrefix(lower, "@") {
		return nil, fmt.Errorf("unbekanntes Makro %q", expr)
	}
	return parseSpec(expr)
}

// Validate reports whether expr is a valid cron expression.
func Validate(expr string) error {
	_, err := Parse(expr)
	return err
}

func parseSpec(expr string) (*Spec, error) {
	fs := strings.Fields(expr)
	if len(fs) != 5 {
		return nil, fmt.Errorf("erwartet 5 Felder (Minute Stunde Tag Monat Wochentag), gefunden %d", len(fs))
	}
	s := &Spec{Expr: expr}
	var err error
	if s.minute, err = parseField(fs[0], 0, 59, nil); err != nil {
		return nil, fmt.Errorf("Minute: %w", err)
	}
	if s.hour, err = parseField(fs[1], 0, 23, nil); err != nil {
		return nil, fmt.Errorf("Stunde: %w", err)
	}
	if s.dom, err = parseField(fs[2], 1, 31, nil); err != nil {
		return nil, fmt.Errorf("Tag: %w", err)
	}
	if s.month, err = parseField(fs[3], 1, 12, monthNames); err != nil {
		return nil, fmt.Errorf("Monat: %w", err)
	}
	if s.dow, err = parseField(fs[4], 0, 7, dowNames); err != nil {
		return nil, fmt.Errorf("Wochentag: %w", err)
	}
	// 7 is an alias for Sunday.
	if s.dow.has(7) {
		s.dow.bits |= 1
		s.dow.bits &^= 1 << 7
	}
	s.dow.max = 6
	return s, nil
}

func parseValue(s string, names map[string]int) (int, error) {
	if names != nil {
		if v, ok := names[strings.ToLower(s)]; ok {
			return v, nil
		}
	}
	v, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("ungültiger Wert %q", s)
	}
	return v, nil
}

func parseField(expr string, min, max int, names map[string]int) (field, error) {
	f := field{min: min, max: max}
	for _, item := range strings.Split(expr, ",") {
		if item == "" {
			return f, fmt.Errorf("leerer Listeneintrag")
		}
		rangeExpr, stepExpr, hasStep := strings.Cut(item, "/")
		step := 1
		if hasStep {
			v, err := strconv.Atoi(stepExpr)
			if err != nil || v <= 0 {
				return f, fmt.Errorf("ungültige Schrittweite %q", stepExpr)
			}
			step = v
		}
		var lo, hi int
		switch {
		case rangeExpr == "*" || rangeExpr == "?":
			lo, hi = min, max
			if !hasStep {
				f.star = true
			}
		case strings.Contains(rangeExpr, "-"):
			a, b, _ := strings.Cut(rangeExpr, "-")
			var err error
			if lo, err = parseValue(a, names); err != nil {
				return f, err
			}
			if hi, err = parseValue(b, names); err != nil {
				return f, err
			}
		default:
			v, err := parseValue(rangeExpr, names)
			if err != nil {
				return f, err
			}
			lo, hi = v, v
			if hasStep {
				hi = max
			}
		}
		if lo < min || hi > max || lo > hi {
			return f, fmt.Errorf("Bereich %d-%d außerhalb von %d-%d", lo, hi, min, max)
		}
		for v := lo; v <= hi; v += step {
			f.bits |= 1 << uint(v)
		}
		f.parts = append(f.parts, part{lo, hi, step})
	}
	return f, nil
}

func (s *Spec) dayMatches(t time.Time) bool {
	domMatch := s.dom.has(t.Day())
	dowMatch := s.dow.has(int(t.Weekday()))
	if s.dom.star || s.dow.star {
		return domMatch && dowMatch
	}
	return domMatch || dowMatch
}

// Next implements Schedule.
func (s *Spec) Next(t time.Time) time.Time {
	loc := t.Location()
	t = t.Truncate(time.Minute).Add(time.Minute)
	added := false
	yearLimit := t.Year() + 5

wrap:
	if t.Year() > yearLimit {
		return time.Time{}
	}
	for !s.month.has(int(t.Month())) {
		if !added {
			added = true
			t = time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
		}
		t = t.AddDate(0, 1, 0)
		if t.Month() == time.January {
			goto wrap
		}
	}
	for !s.dayMatches(t) {
		if !added {
			added = true
			t = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
		}
		t = t.AddDate(0, 0, 1)
		// Compensate for DST transitions that moved midnight.
		if h := t.Hour(); h != 0 {
			if h > 12 {
				t = t.Add(time.Duration(24-h) * time.Hour)
			} else {
				t = t.Add(-time.Duration(h) * time.Hour)
			}
		}
		if t.Day() == 1 {
			goto wrap
		}
	}
	for !s.hour.has(t.Hour()) {
		if !added {
			added = true
			t = time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), 0, 0, 0, loc)
		}
		t = t.Add(time.Hour)
		if t.Hour() == 0 {
			goto wrap
		}
	}
	for !s.minute.has(t.Minute()) {
		if !added {
			added = true
		}
		t = t.Add(time.Minute)
		if t.Minute() == 0 {
			goto wrap
		}
	}
	return t
}

// NextN returns the next n activation times after from.
func NextN(s Schedule, from time.Time, n int) []time.Time {
	out := make([]time.Time, 0, n)
	t := from
	for i := 0; i < n; i++ {
		t = s.Next(t)
		if t.IsZero() {
			break
		}
		out = append(out, t)
	}
	return out
}
