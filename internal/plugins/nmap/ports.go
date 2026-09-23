package nmap

import (
	"fmt"
	"strconv"
	"strings"
)

// portsToArgs turns the "ports" setting into nmap arguments. Accepted forms:
//
//	"top-N"                 → --top-ports N        (1 ≤ N ≤ 65535)
//	"all"                   → -p-                  (all 65535 ports)
//	"22,80,443,8000-8100"   → -p 22,80,443,8000-8100
//
// Any other input is rejected so no arbitrary nmap flags can be smuggled in.
func portsToArgs(spec string) ([]string, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("Portangabe fehlt")
	}
	if strings.EqualFold(spec, "all") {
		return []string{"-p-"}, nil
	}
	if rest, ok := cutFoldPrefix(spec, "top-"); ok {
		n, err := strconv.Atoi(strings.TrimSpace(rest))
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("ungültige Portangabe %q: top-N erwartet mit 1–65535", spec)
		}
		return []string{"--top-ports", strconv.Itoa(n)}, nil
	}
	if err := validatePortList(spec); err != nil {
		return nil, err
	}
	return []string{"-p", spec}, nil
}

// validatePortList checks a bare nmap port list ("22,80,8000-8100"): only digits, commas
// and hyphenated ranges, every port in 1–65535 and ranges non-decreasing.
func validatePortList(spec string) error {
	parts := strings.Split(spec, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return fmt.Errorf("ungültige Portangabe %q: leerer Eintrag", spec)
		}
		if from, to, ok := strings.Cut(part, "-"); ok {
			f, err1 := parsePort(from)
			t, err2 := parsePort(to)
			if err1 != nil || err2 != nil || t < f {
				return fmt.Errorf("ungültiger Portbereich %q", part)
			}
			continue
		}
		if _, err := parsePort(part); err != nil {
			return fmt.Errorf("ungültiger Port %q", part)
		}
	}
	return nil
}

func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n < 1 || n > 65535 {
		return 0, fmt.Errorf("Port außerhalb 1–65535")
	}
	return n, nil
}

func cutFoldPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && strings.EqualFold(s[:len(prefix)], prefix) {
		return s[len(prefix):], true
	}
	return "", false
}
