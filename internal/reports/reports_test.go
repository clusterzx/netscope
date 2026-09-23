package reports

import (
	"testing"
	"time"
)

func TestHumanDuration(t *testing.T) {
	cases := map[time.Duration]string{
		45 * time.Second:               "45 s",
		5*time.Minute + 20*time.Second: "5 min",
		2 * time.Hour:                  "2 h",
		2*time.Hour + 5*time.Minute:    "2 h 5 min",
		3*24*time.Hour + 4*time.Hour:   "3 d 4 h",
		50*time.Hour + 59*time.Second:  "2 d 2 h",
		24*time.Hour + 30*time.Second:  "1 d",
		1500 * time.Millisecond:        "2 s",
	}
	for d, want := range cases {
		if got := humanDuration(d); got != want {
			t.Errorf("%v: got %q want %q", d, got, want)
		}
	}
}
