package cron

import (
	"fmt"
	"strings"
	"time"

	"netscope/internal/i18n"
)

// phrases are the words of one description language. The descriptions are generated
// grammar, not catalog texts: every language has a table of its own (the German texts
// carry the ignore marker of the catalog coverage test).
type phrases struct {
	weekdays     []string // a weekday as a recurrence: "montags", "Mondays"
	weekdayShort []string
	months       []string // index 1–12
	and          string   // last separator of a list
	or           string   // day of month or weekday
	ordinal      func(int) string
	duration     func(time.Duration) string

	every            string // @every: "Alle %s"
	everyMinute      string
	everyNMinutes    string // "alle %d Minuten"
	betweenHours     string // hour range: " zwischen %02d:00 und %02d:59"
	inHours          string // hour list: " in den Stunden %s"
	inEveryNthHour   string // hour step: " in jeder %s Stunde" (ordinal)
	onTheHour        string
	atMinute         string // "zur Minute %d"
	atMinutes        string // "zu den Minuten %s"
	hourly           string // "stündlich %s" (minute text)
	everyNHours      string // "alle %d Stunden %s" (minute text)
	atTimes          string // "um %s"
	atMinutesInHours string // "zu den Minuten %s in den Stunden %s"
	workdays         string
	weekend          string
	onWeekdays       string // list of weekdays: "%s"
	everyNthDay      string // day-of-month step: "jeden %s Tag des Monats" (ordinal)
	onDays           string // days of month: "am %s jedes Monats" (ordinals)
	daily            string
	inMonths         string // "im %s"
}

var german = phrases{
	weekdays:     []string{"sonntags", "montags", "dienstags", "mittwochs", "donnerstags", "freitags", "samstags"},
	weekdayShort: []string{"So", "Mo", "Di", "Mi", "Do", "Fr", "Sa"},
	months: []string{"", "Januar", "Februar", "März", "April", "Mai", "Juni", "Juli", "August", // i18n:ignore
		"September", "Oktober", "November", "Dezember"},
	and:      " und ",  // i18n:ignore
	or:       " oder ", // i18n:ignore
	ordinal:  func(n int) string { return fmt.Sprintf("%d.", n) },
	duration: GermanDuration,

	every:            "Alle %s",                             // i18n:ignore
	everyMinute:      "jede Minute",                         // i18n:ignore
	everyNMinutes:    "alle %d Minuten",                     // i18n:ignore
	betweenHours:     " zwischen %02d:00 und %02d:59",       // i18n:ignore
	inHours:          " in den Stunden %s",                  // i18n:ignore
	inEveryNthHour:   " in jeder %s Stunde",                 // i18n:ignore
	onTheHour:        "zur vollen Stunde",                   // i18n:ignore
	atMinute:         "zur Minute %d",                       // i18n:ignore
	atMinutes:        "zu den Minuten %s",                   // i18n:ignore
	hourly:           "stündlich %s",                        // i18n:ignore
	everyNHours:      "alle %d Stunden %s",                  // i18n:ignore
	atTimes:          "um %s",                               // i18n:ignore
	atMinutesInHours: "zu den Minuten %s in den Stunden %s", // i18n:ignore
	workdays:         "werktags (Mo–Fr)",                    // i18n:ignore
	weekend:          "am Wochenende",                       // i18n:ignore
	onWeekdays:       "%s",
	everyNthDay:      "jeden %s Tag des Monats", // i18n:ignore
	onDays:           "am %s jedes Monats",      // i18n:ignore
	daily:            "täglich",                 // i18n:ignore
	inMonths:         "im %s",                   // i18n:ignore
}

var english = phrases{
	weekdays:     []string{"Sundays", "Mondays", "Tuesdays", "Wednesdays", "Thursdays", "Fridays", "Saturdays"},
	weekdayShort: []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"},
	months: []string{"", "January", "February", "March", "April", "May", "June", "July", "August",
		"September", "October", "November", "December"},
	and:      " and ",
	or:       " or ",
	ordinal:  englishOrdinal,
	duration: EnglishDuration,

	every:            "Every %s",
	everyMinute:      "every minute",
	everyNMinutes:    "every %d minutes",
	betweenHours:     " between %02d:00 and %02d:59",
	inHours:          " during hours %s",
	inEveryNthHour:   " in every %s hour",
	onTheHour:        "on the hour",
	atMinute:         "at minute %d",
	atMinutes:        "at minutes %s",
	hourly:           "every hour %s",
	everyNHours:      "every %d hours %s",
	atTimes:          "at %s",
	atMinutesInHours: "at minutes %s during hours %s",
	workdays:         "weekdays (Mon–Fri)",
	weekend:          "on weekends",
	onWeekdays:       "on %s",
	everyNthDay:      "every %s day of the month",
	onDays:           "on the %s of every month",
	daily:            "daily",
	inMonths:         "in %s",
}

func phrasesFor(loc i18n.Locale) *phrases {
	if loc == i18n.EN {
		return &english
	}
	return &german
}

// Describe renders a German plain-text description of a cron expression,
// e.g. "Alle 5 Minuten" or "Werktags (Mo–Fr) um 08:00".
func Describe(expr string) (string, error) {
	return DescribeIn(expr, i18n.DE)
}

// DescribeIn renders a plain-text description of a cron expression in the language, e.g.
// "Alle 5 Minuten" / "Every 5 minutes" or "Werktags (Mo–Fr) um 08:00" / "Weekdays (Mon–Fri)
// at 08:00". Languages other than English get German. Errors are German (see internal/i18n).
func DescribeIn(expr string, loc i18n.Locale) (string, error) {
	s, err := Parse(expr)
	if err != nil {
		return "", err
	}
	p := phrasesFor(loc)
	switch v := s.(type) {
	case Every:
		return fmt.Sprintf(p.every, p.duration(v.D)), nil
	case *Spec:
		return capitalize(v.describe(p)), nil
	}
	return expr, nil
}

type durationUnit struct {
	d          time.Duration
	one, other string
}

var germanUnits = []durationUnit{
	{24 * time.Hour, "Tag", "Tage"},      // i18n:ignore
	{time.Hour, "Stunde", "Stunden"},     // i18n:ignore
	{time.Minute, "Minute", "Minuten"},   // i18n:ignore
	{time.Second, "Sekunde", "Sekunden"}, // i18n:ignore
}

var englishUnits = []durationUnit{
	{24 * time.Hour, "day", "days"},
	{time.Hour, "hour", "hours"},
	{time.Minute, "minute", "minutes"},
	{time.Second, "second", "seconds"},
}

// GermanDuration formats a duration as German words ("5 Minuten", "1 Stunde 30 Minuten").
func GermanDuration(d time.Duration) string {
	return formatDuration(d, germanUnits, "0 Sekunden") // i18n:ignore
}

// EnglishDuration formats a duration as English words ("5 minutes", "1 hour 30 minutes").
func EnglishDuration(d time.Duration) string {
	return formatDuration(d, englishUnits, "0 seconds")
}

// formatDuration writes a duration in the units of a language.
func formatDuration(d time.Duration, units []durationUnit, zero string) string {
	if d <= 0 {
		return zero
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

// englishOrdinal returns 1st, 2nd, 3rd, 4th … 11th, 12th, 13th … 21st.
func englishOrdinal(n int) string {
	suffix := "th"
	if n%100 < 11 || n%100 > 13 {
		switch n % 10 {
		case 1:
			suffix = "st"
		case 2:
			suffix = "nd"
		case 3:
			suffix = "rd"
		}
	}
	return fmt.Sprintf("%d%s", n, suffix)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	r := []rune(s)
	r[0] = []rune(strings.ToUpper(string(r[0])))[0]
	return string(r)
}

// join lists items as "a, b und c" / "a, b and c".
func (p *phrases) join(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	}
	return strings.Join(items[:len(items)-1], ", ") + p.and + items[len(items)-1]
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

func intsToStrings(vs []int, format func(int) string) []string {
	out := make([]string, len(vs))
	for i, v := range vs {
		out[i] = format(v)
	}
	return out
}

func itoa(v int) string { return fmt.Sprint(v) }

// timePart describes minute+hour. The boolean reports whether the description already
// implies a recurrence within a day (so "täglich"/"daily" can be omitted).
func (s *Spec) timePart(p *phrases) (string, bool) {
	m, h := s.minute, s.hour
	mStep, mIsStep := m.singleStep()
	hStep, hIsStep := h.singleStep()
	hRange, hIsRange := h.singleRange()
	mVals, hVals := m.values(), h.values()

	hourWindow := ""
	switch {
	case hIsRange:
		hourWindow = fmt.Sprintf(p.betweenHours, hRange.lo, hRange.hi)
	case !h.star && !hIsStep:
		hourWindow = fmt.Sprintf(p.inHours, p.join(intsToStrings(hVals, itoa)))
	}
	hourWindowOrStep := func() string {
		if hIsStep {
			return fmt.Sprintf(p.inEveryNthHour, p.ordinal(hStep.step))
		}
		return hourWindow
	}

	switch {
	case m.star && h.star:
		return p.everyMinute, true
	case m.star:
		return p.everyMinute + hourWindowOrStep(), true
	case mIsStep && mStep.lo == 0 && mStep.hi == 59:
		return fmt.Sprintf(p.everyNMinutes, mStep.step) + hourWindowOrStep(), true
	}

	// explicit minutes from here on
	minuteText := func() string {
		if len(mVals) == 1 && mVals[0] == 0 {
			return p.onTheHour
		}
		if len(mVals) == 1 {
			return fmt.Sprintf(p.atMinute, mVals[0])
		}
		return fmt.Sprintf(p.atMinutes, p.join(intsToStrings(mVals, itoa)))
	}
	switch {
	case h.star:
		return fmt.Sprintf(p.hourly, minuteText()), true
	case hIsStep && hStep.lo == 0 && hStep.hi == 23:
		return fmt.Sprintf(p.everyNHours, hStep.step, minuteText()), true
	case hIsRange:
		return fmt.Sprintf(p.hourly, minuteText()) + hourWindow, true
	}
	if len(mVals)*len(hVals) <= 8 {
		var times []string
		for _, hv := range hVals {
			for _, mv := range mVals {
				times = append(times, fmt.Sprintf("%02d:%02d", hv, mv))
			}
		}
		return fmt.Sprintf(p.atTimes, p.join(times)), false
	}
	return fmt.Sprintf(p.atMinutesInHours,
		p.join(intsToStrings(mVals, itoa)), p.join(intsToStrings(hVals, itoa))), false
}

func (s *Spec) dayPart(p *phrases) string {
	dowVals := s.dow.values()
	domVals := s.dom.values()
	dowText := func() string {
		key := fmt.Sprint(dowVals)
		switch key {
		case "[1 2 3 4 5]":
			return p.workdays
		case "[0 6]":
			return p.weekend
		}
		if r, ok := s.dow.singleRange(); ok {
			return fmt.Sprintf("%s–%s", p.weekdayShort[r.lo], p.weekdayShort[r.hi])
		}
		names := make([]string, len(dowVals))
		for i, v := range dowVals {
			names[i] = p.weekdays[v]
		}
		return fmt.Sprintf(p.onWeekdays, p.join(names))
	}
	domText := func() string {
		if st, ok := s.dom.singleStep(); ok && st.lo == 1 {
			return fmt.Sprintf(p.everyNthDay, p.ordinal(st.step))
		}
		return fmt.Sprintf(p.onDays, p.join(intsToStrings(domVals, p.ordinal)))
	}
	switch {
	case s.dom.star && s.dow.star:
		return p.daily
	case s.dom.star:
		return dowText()
	case s.dow.star:
		return domText()
	default:
		return domText() + p.or + dowText()
	}
}

func (s *Spec) monthPart(p *phrases) string {
	if s.month.star {
		return ""
	}
	vals := s.month.values()
	names := make([]string, len(vals))
	for i, v := range vals {
		names[i] = p.months[v]
	}
	return fmt.Sprintf(p.inMonths, p.join(names))
}

func (s *Spec) describe(p *phrases) string {
	tp, recurring := s.timePart(p)
	dp := s.dayPart(p)
	mp := s.monthPart(p)
	var parts []string
	if dp == p.daily && recurring {
		parts = append(parts, tp)
	} else {
		parts = append(parts, dp, tp)
	}
	if mp != "" {
		parts = append(parts, mp)
	}
	return strings.Join(parts, " ")
}
