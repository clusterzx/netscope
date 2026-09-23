package cve

import (
	"regexp"
	"strings"
)

// Version comparison is generic and deliberately tolerant: versions are split into
// numeric and alphabetic tokens (every other character separates tokens), so "9.6p1",
// "9.6_p1" and "9.6.p1" compare equal. Alphabetic tokens are either pre-release markers
// (dev < alpha < beta < pre < rc), which sort before the plain release ("1.0rc1" <
// "1.0"), or post-release suffixes such as OpenSSH's "p1" or OpenSSL's letter releases,
// which sort after it ("9.8" < "9.8p1", "1.1.1" < "1.1.1w" < "1.1.1x" < "1.1.1za").
// A numeric token beats an alphabetic one at the same position ("1.0a" < "1.0.1") and
// trailing zero components are insignificant ("1.0" == "1.0.0").

type vtok struct {
	num bool
	s   string // digits without leading zeros, or lowercase letters
	pre int    // alphabetic tokens: pre-release rank (0..4), -1 = post-release suffix
}

var preRanks = map[string]int{
	"dev": 0, "snapshot": 0, "nightly": 0,
	"alpha": 1,
	"beta":  2, "milestone": 2,
	"pre": 3, "preview": 3, "ea": 3,
	"rc": 4, "cr": 4,
}

// weak markers are pre-release markers only when a number follows ("2.0a1" is an
// alpha, "1.0.2a" is OpenSSL's first letter release).
var weakPreRanks = map[string]int{"a": 1, "b": 2, "m": 2, "c": 4}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isAlpha(c byte) bool { return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') }

// tokenize splits a version string into tokens.
func tokenize(v string) []vtok {
	var out []vtok
	for i := 0; i < len(v); {
		c := v[i]
		switch {
		case isDigit(c):
			j := i
			for j < len(v) && isDigit(v[j]) {
				j++
			}
			n := strings.TrimLeft(v[i:j], "0")
			if n == "" {
				n = "0"
			}
			out = append(out, vtok{num: true, s: n})
			i = j
		case isAlpha(c):
			j := i
			for j < len(v) && isAlpha(v[j]) {
				j++
			}
			out = append(out, vtok{s: strings.ToLower(v[i:j]), pre: -1})
			i = j
		default:
			i++
		}
	}
	for k := range out {
		if out[k].num {
			continue
		}
		if r, ok := preRanks[out[k].s]; ok {
			out[k].pre = r
		} else if r, ok := weakPreRanks[out[k].s]; ok && k+1 < len(out) && out[k+1].num {
			out[k].pre = r
		}
	}
	return out
}

func cmpNum(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

func cmpAlpha(a, b vtok) int {
	switch {
	case a.pre >= 0 && b.pre >= 0:
		return sign(a.pre - b.pre)
	case a.pre >= 0:
		return -1
	case b.pre >= 0:
		return 1
	}
	return strings.Compare(a.s, b.s)
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}

// tail decides a comparison when one side has run out of tokens: rest are the remaining
// tokens of the longer side. It returns the sign of (longer - shorter) and whether the
// shorter side is only a less precise form of the longer one (e.g. "4.15" vs "4.15.18").
func tail(rest []vtok) (int, bool) {
	for _, t := range rest {
		if t.num && t.s == "0" {
			continue
		}
		if t.num {
			return 1, true
		}
		if t.pre >= 0 {
			return -1, false
		}
		return 1, false
	}
	return 0, false
}

// compareTokens compares a with b. prefix reports that a ran out of tokens while b still
// had significant numeric components (a is a truncated form of b).
func compareTokens(a, b []vtok) (c int, prefix bool) {
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		ta, tb := a[i], b[j]
		switch {
		case ta.num && tb.num:
			if c := cmpNum(ta.s, tb.s); c != 0 {
				return c, false
			}
			i, j = i+1, j+1
		case !ta.num && !tb.num:
			if c := cmpAlpha(ta, tb); c != 0 {
				return c, false
			}
			i, j = i+1, j+1
		case ta.num: // numeric vs alphabetic
			if ta.s == "0" {
				i++ // "1.0.0rc1" == "1.0rc1"
				continue
			}
			return 1, false
		default:
			if tb.s == "0" {
				j++
				continue
			}
			return -1, false
		}
	}
	switch {
	case i == len(a) && j == len(b):
		return 0, false
	case i == len(a):
		c, p := tail(b[j:])
		return -c, p
	default:
		c, _ := tail(a[i:])
		return c, false
	}
}

// CompareVersions compares two version strings (-1, 0, 1).
func CompareVersions(a, b string) int {
	c, _ := compareTokens(tokenize(a), tokenize(b))
	return c
}

// updateMarkers are alphabetic tokens that denote an update of a base version when they
// directly follow it ("9.6p1" is version 9.6 update p1 in the NVD's OpenSSH records).
var updateMarkers = map[string]bool{
	"p": true, "patch": true, "pl": true, "sp": true, "u": true, "update": true, "r": true,
	"rc": true, "beta": true, "alpha": true, "a": true, "b": true, "pre": true, "build": true,
}

// ---------------------------------------------------------------- upstream versions

var (
	epochRe     = regexp.MustCompile(`^\d+:`)
	apkRelRe    = regexp.MustCompile(`-r\d+$`)
	repackRe    = regexp.MustCompile(`(?i)[.+~](dfsg|ds|repack|debian|nmu)\d*.*$`)
	tildePreRe  = regexp.MustCompile(`(?i)^(rc|beta|alpha|pre|a\d|b\d)`)
	distroTagRe = regexp.MustCompile(`(?i)(ubuntu|debian|raspbian|deb\d+u\d+|[+~]deb\d+|\brhel\b|red ?hat|centos|\.el[5-9]|\bel[5-9]\b|\.fc\d\d|fedora|suse|alpine|amzn|rocky|almalinux|freebsd|gentoo|synology)`)
	wildcardRe  = regexp.MustCompile(`(?i)([.\-_]x\b|\*).*$`)
)

// UpstreamVersion derives the upstream version from a distribution package version:
// the epoch ("1:"), the Debian/RPM revision (after the last "-"), apk release ("-r0"),
// repack suffixes ("+dfsg", ".ds") and distro "~" suffixes are removed, while upstream
// suffixes like OpenSSH's "p1" are kept. "+really" versions resolve to the real version.
func UpstreamVersion(manager, v string) string {
	v = strings.TrimSpace(v)
	v = epochRe.ReplaceAllString(v, "")
	switch manager {
	case "apk":
		v = apkRelRe.ReplaceAllString(v, "")
	default:
		if i := strings.LastIndex(v, "-"); i > 0 {
			v = v[:i]
		}
	}
	if i := strings.Index(strings.ToLower(v), "+really"); i >= 0 {
		v = v[i+len("+really"):]
	}
	v = repackRe.ReplaceAllString(v, "")
	if i := strings.Index(v, "~"); i >= 0 && !tildePreRe.MatchString(v[i+1:]) {
		v = v[:i]
	}
	if i := strings.Index(v, "+"); i > 0 {
		v = v[:i]
	}
	return strings.Trim(v, ".-_~")
}

// hasDistroHint reports whether a service version banner indicates a distribution
// package (e.g. "9.6p1 Ubuntu 3ubuntu13.5", "2.4.62 (Debian)"). Such builds usually
// carry backported fixes, so version matches are only heuristic.
func hasDistroHint(s string) bool { return distroTagRe.MatchString(s) }

// serviceVersion extracts the leading version token of a banner version such as
// "9.6p1 Ubuntu 3ubuntu13.5" -> "9.6p1". Wildcard components ("2.4.X") are cut off, so
// the version is treated as imprecise instead of being compared letter by letter.
func serviceVersion(v string) string {
	v = strings.TrimSpace(v)
	if i := strings.IndexAny(v, " \t("); i > 0 {
		v = v[:i]
	}
	v = wildcardRe.ReplaceAllString(v, "")
	return strings.TrimRight(v, ",;.-_")
}

// hasDigit reports whether s contains a digit (a usable version).
func hasDigit(s string) bool {
	for i := 0; i < len(s); i++ {
		if isDigit(s[i]) {
			return true
		}
	}
	return false
}
