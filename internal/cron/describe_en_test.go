package cron

import (
	"testing"
	"time"

	"netscope/internal/i18n"
)

func TestDescribeEnglish(t *testing.T) {
	cases := map[string]string{
		"*/5 * * * *":        "Every 5 minutes",
		"* * * * *":          "Every minute",
		"* */2 * * *":        "Every minute in every 2nd hour",
		"0 3 * * *":          "Daily at 03:00",
		"0 * * * *":          "Every hour on the hour",
		"15 * * * *":         "Every hour at minute 15",
		"0,30 * * * *":       "Every hour at minutes 0 and 30",
		"0 */6 * * *":        "Every 6 hours on the hour",
		"15 9-17 * * *":      "Every hour at minute 15 between 09:00 and 17:59",
		"0 8 * * 1-5":        "Weekdays (Mon–Fri) at 08:00",
		"0 8 * * 1":          "On Mondays at 08:00",
		"0 8 * * 1,3,5":      "On Mondays, Wednesdays and Fridays at 08:00",
		"0 8 * * 2-4":        "Tue–Thu at 08:00",
		"0 0 1 * *":          "On the 1st of every month at 00:00",
		"0 0 1,15 * *":       "On the 1st and 15th of every month at 00:00",
		"0 0 */2 * *":        "Every 2nd day of the month at 00:00",
		"0 0 22 * *":         "On the 22nd of every month at 00:00",
		"0 0 13 * 1":         "On the 13th of every month or on Mondays at 00:00",
		"0 8,20 * * *":       "Daily at 08:00 and 20:00",
		"*/15 8-18 * * *":    "Every 15 minutes between 08:00 and 18:59",
		"*/10 8,12 * * *":    "Every 10 minutes during hours 8 and 12",
		"0,30 1-5,7 * * *":   "Daily at minutes 0 and 30 during hours 1, 2, 3, 4, 5 and 7",
		"0 0 * * 0,6":        "On weekends at 00:00",
		"0 4 * 1 *":          "Daily at 04:00 in January",
		"0 4 * 3,12 *":       "Daily at 04:00 in March and December",
		"*/5 * * * 1-5":      "Weekdays (Mon–Fri) every 5 minutes",
		"@every 90s":         "Every 1 minute 30 seconds",
		"@every 2h":          "Every 2 hours",
		"@every 1h30m":       "Every 1 hour 30 minutes",
		"@every 25h":         "Every 1 day 1 hour",
		"@daily":             "Daily at 00:00",
		"@weekly":            "On Sundays at 00:00",
		"@monthly":           "On the 1st of every month at 00:00",
		"@yearly":            "On the 1st of every month at 00:00 in January",
		"@hourly":            "Every hour on the hour",
		"30 2 * * sun":       "On Sundays at 02:30",
		"0 12 * jan-mar mon": "On Mondays at 12:00 in January, February and March",
	}
	for expr, want := range cases {
		got, err := DescribeIn(expr, i18n.EN)
		if err != nil {
			t.Errorf("DescribeIn(%q, en): %v", expr, err)
			continue
		}
		if got != want {
			t.Errorf("DescribeIn(%q, en) = %q, want %q", expr, got, want)
		}
	}
}

// German stays exactly what Describe returned before English existed.
func TestDescribeInGerman(t *testing.T) {
	cases := map[string]string{
		"*/5 * * * *":   "Alle 5 Minuten",
		"0 8 * * 1-5":   "Werktags (Mo–Fr) um 08:00",
		"0 0 */2 * *":   "Jeden 2. Tag des Monats um 00:00",
		"0 0 1,15 * *":  "Am 1. und 15. jedes Monats um 00:00",
		"* */2 * * *":   "Jede Minute in jeder 2. Stunde",
		"0 8 * * 1,3,5": "Montags, mittwochs und freitags um 08:00",
		"0 0 13 * 1":    "Am 13. jedes Monats oder montags um 00:00",
		"@every 1h30m":  "Alle 1 Stunde 30 Minuten",
	}
	for expr, want := range cases {
		for _, loc := range []i18n.Locale{i18n.DE, ""} {
			got, err := DescribeIn(expr, loc)
			if err != nil || got != want {
				t.Errorf("DescribeIn(%q, %q) = %q, %v, want %q", expr, loc, got, err, want)
			}
		}
		if got, _ := Describe(expr); got != want {
			t.Errorf("Describe(%q) = %q, want %q", expr, got, want)
		}
	}
}

func TestEnglishDuration(t *testing.T) {
	cases := map[time.Duration]string{
		0:                                     "0 seconds",
		time.Second:                           "1 second",
		10 * time.Second:                      "10 seconds",
		time.Minute:                           "1 minute",
		90 * time.Minute:                      "1 hour 30 minutes",
		48*time.Hour + 2*time.Minute:          "2 days 2 minutes",
		500 * time.Millisecond:                "500ms",
		time.Hour + time.Minute + time.Second: "1 hour 1 minute 1 second",
	}
	for d, want := range cases {
		if got := EnglishDuration(d); got != want {
			t.Errorf("EnglishDuration(%v) = %q, want %q", d, got, want)
		}
	}
	if got := GermanDuration(90 * time.Minute); got != "1 Stunde 30 Minuten" {
		t.Errorf("GermanDuration = %q", got)
	}
	if got := GermanDuration(0); got != "0 Sekunden" {
		t.Errorf("GermanDuration(0) = %q", got)
	}
}

func TestEnglishOrdinal(t *testing.T) {
	for n, want := range map[int]string{1: "1st", 2: "2nd", 3: "3rd", 4: "4th", 11: "11th", 12: "12th",
		13: "13th", 21: "21st", 22: "22nd", 23: "23rd", 31: "31st", 111: "111th"} {
		if got := englishOrdinal(n); got != want {
			t.Errorf("englishOrdinal(%d) = %q, want %q", n, got, want)
		}
	}
}
