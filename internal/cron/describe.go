package cron

import (
	"fmt"
	"strings"
	"time"
)

var germanWeekdays = []string{"sonntags", "montags", "dienstags", "mittwochs", "donnerstags", "freitags", "samstags"}
var germanWeekdayShort = []string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"}
var germanMonths = []string{"", "Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August",
	"September", "Oktober", "November", "Dezember"}

// Describe renders a German plain-text description of a cron expression,
// e.g. "Alle 5 Minuten" or "Werktags (Mo–Fr) um 08:00".
func Describe(expr string) (string, error) {
	s, err := Parse(expr)
	if err != nil {
		return "", err
	}
	switch v := s.(type) {
	case Every:
		return "Alle " + GermanDuration(v.D), nil
	case *Spec:
		return capitalize(v.describe()), nil
	}
	return expr, nil
}

// GermanDuration formats a duration as German words ("5 Minuten", "1 Stunde 30 Minuten").
func GermanDuration(d time.Duration) string {
	if d <= 0 {
		return "0 Sekunden"
	}
	type unit struct {
		d          time.Duration
		one, other string
	}
	units := []unit{
		{24 * time.Hour, "Tag", "Tage"},
		{time.Hour, "Stunde", "Stunden"},
		{time.Minute, "Minute", "Minuten"},
		{time.Second, "Sekunde", "Sekunden"},
	}
	var parts []string
	rest := d
	for _, u := range units {
		n := rest / u.d
		if n == 0 {
			continue
		}
		rest -= n * u.d
		name := u.other
		if n == 1 {
			name = u.one
		}
		parts = append(parts, fmt.Sprintf("%d %s", n, name))
	}
	if len(parts) == 0 {
		return d.String()
	}
	return strings.Join(parts, " ")
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

func joinGerman(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + " und " + items[len(items)-1]
}

func (f field) singleStep() (part, bool) {
	if len(f.parts) == 1 && f.parts[0].step > 1 {
		return f.parts[0], true
	}
	return part{}, false
}

func (f field) singleRange() (part, bool) {
	if !f.star && len(f.parts) == 1 && f.parts[0].step == 1 && f.parts[0].lo != f.parts[0].hi {
		return f.parts[0], true
	}
	return part{}, false
}

func intsToStrings(vs []int, format string) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = fmt.Sprintf(format, v)
	}
	return out
}

// timePart describes minute+hour. The boolean reports whether the description already
// implies a recurrence within a day (so "täglich" can be omitted).
func (s *Spec) timePart() (string, bool) {
	m, h := s.minute, s.hour
	mStep, mIsStep := m.singleStep()
	hStep, hIsStep := h.singleStep()
	hRange, hIsRange := h.singleRange()
	mVals, hVals := m.values(), h.values()

	hourWindow := ""
	switch {
	case hIsRange:
		hourWindow = fmt.Sprintf(" zwischen %02d:00 und %02d:59", hRange.lo, hRange.hi)
	case !h.star && !hIsStep:
		hourWindow = " in den Stunden " + joinGerman(intsToStrings(hVals, "%d"))
	}

	switch {
	case m.star && h.star:
		return "jede Minute", true
	case m.star:
		return "jede Minute" + hourWindowOrStep(hourWindow, hIsStep, hStep), true
	case mIsStep && mStep.lo == 0 && mStep.hi == 59:
		txt := fmt.Sprintf("alle %d Minuten", mStep.step)
		return txt + hourWindowOrStep(hourWindow, hIsStep, hStep), true
	}

	// explicit minutes from here on
	minuteText := func() string {
		if len(mVals) == 1 && mVals[0] == 0 {
			return "zur vollen Stunde"
		}
		if len(mVals) == 1 {
			return fmt.Sprintf("zur Minute %d", mVals[0])
		}
		return "zu den Minuten " + joinGerman(intsToStrings(mVals, "%d"))
	}
	switch {
	case h.star:
		return "stündlich " + minuteText(), true
	case hIsStep && hStep.lo == 0 && hStep.hi == 23:
		return fmt.Sprintf("alle %d Stunden %s", hStep.step, minuteText()), true
	case hIsRange:
		return "stündlich " + minuteText() + hourWindow, true
	}
	if len(mVals)*len(hVals) <= 8 {
		var times []string
		for _, hv := range hVals {
			for _, mv := range mVals {
				times = append(times, fmt.Sprintf("%02d:%02d", hv, mv))
			}
		}
		return "um " + joinGerman(times), false
	}
	return fmt.Sprintf("zu den Minuten %s in den Stunden %s",
		joinGerman(intsToStrings(mVals, "%d")), joinGerman(intsToStrings(hVals, "%d"))), false
}

func hourWindowOrStep(window string, isStep bool, step part) string {
	if isStep {
		return fmt.Sprintf(" in jeder %d. Stunde", step.step)
	}
	return window
}

func (s *Spec) dayPart() string {
	dowVals := s.dow.values()
	domVals := s.dom.values()
	dowText := func() string {
		key := fmt.Sprint(dowVals)
		switch key {
		case "[1 2 3 4 5]":
			return "werktags (Mo–Fr)"
		case "[0 6]":
			return "am Wochenende"
		}
		if r, ok := s.dow.singleRange(); ok {
			return fmt.Sprintf("%s–%s", germanWeekdayShort[r.lo], germanWeekdayShort[r.hi])
		}
		names := make([]string, len(dowVals))
		for i, v := range dowVals {
			names[i] = germanWeekdays[v]
		}
		return joinGerman(names)
	}
	domText := func() string {
		if st, ok := s.dom.singleStep(); ok && st.lo == 1 {
			return fmt.Sprintf("jeden %d. Tag des Monats", st.step)
		}
		return "am " + joinGerman(intsToStrings(domVals, "%d.")) + " jedes Monats"
	}
	switch {
	case s.dom.star && s.dow.star:
		return "täglich"
	case s.dom.star:
		return dowText()
	case s.dow.star:
		return domText()
	default:
		return domText() + " oder " + dowText()
	}
}

func (s *Spec) monthPart() string {
	if s.month.star {
		return ""
	}
	vals := s.month.values()
	names := make([]string, len(vals))
	for i, v := range vals {
		names[i] = germanMonths[v]
	}
	return "im " + joinGerman(names)
}

func (s *Spec) describe() string {
	tp, recurring := s.timePart()
	dp := s.dayPart()
	mp := s.monthPart()
	var parts []string
	if dp == "täglich" && recurring {
		parts = append(parts, tp)
	} else {
		parts = append(parts, dp, tp)
	}
	if mp != "" {
		parts = append(parts, mp)
	}
	return strings.Join(parts, " ")
}
