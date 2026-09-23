package cve

import "testing"

func TestCompareVersions(t *testing.T) {
	// a < b
	less := [][2]string{
		{"9.6p1", "9.8"},
		{"9.8", "9.8p1"}, // OpenSSH portable release after the OpenBSD release
		{"4.4", "4.4p1"},
		{"4.3p2", "4.4"},
		{"9.8p1", "9.8p2"},
		{"1.1.1", "1.1.1w"}, // OpenSSL letter releases
		{"1.1.1w", "1.1.1x"},
		{"1.0.2zi", "1.0.2zj"},
		{"1.0.2z", "1.0.2za"},
		{"1.0.2za", "1.0.3"},
		{"1.0.2", "1.0.2a"},
		{"1.0a", "1.0.1"},
		{"1.0rc1", "1.0"}, // pre-releases
		{"1.0-rc1", "1.0.0"},
		{"2.0a1", "2.0"},
		{"2.0a2", "2.0b1"},
		{"2.0b3", "2.0rc1"},
		{"1.0-alpha", "1.0-beta"},
		{"1.0-beta", "1.0"},
		{"1.0.dev1", "1.0a1"},
		{"1.9", "1.10"},
		{"10.0.2", "10.0.12"},
		{"2023.12", "2024.1"},
		{"20231231", "20240101"},
		{"1.9.5", "1.9.5p2"},
		{"1.9.5p1", "1.9.5p2"},
		{"4.32.1", "4.32.1f"},
		{"7.44.0", "8.5.0"},
		{"8.5.0", "8.7.0"},
		{"1.4.0-beta.2", "1.4.0-beta.11"},
		{"2.4.49", "2.4.50"},
		{"6.1.69", "6.1.76"},
	}
	for _, c := range less {
		if got := CompareVersions(c[0], c[1]); got != -1 {
			t.Errorf("%s vs %s = %d, want -1", c[0], c[1], got)
		}
		if got := CompareVersions(c[1], c[0]); got != 1 {
			t.Errorf("%s vs %s = %d, want 1", c[1], c[0], got)
		}
	}
	equal := [][2]string{
		{"1.0", "1.0.0"},
		{"1.02", "1.2"},
		{"9.8p1", "9.8_p1"},
		{"9.8p1", "9.8.p1"},
		{"1.0.0-rc1", "1.0rc1"},
		{"2.0-alpha1", "2.0a1"}, // "a" followed by a number is an alpha
		{"V1.2", "v1.2"},
		{"", ""},
	}
	for _, c := range equal {
		if got := CompareVersions(c[0], c[1]); got != 0 {
			t.Errorf("%s vs %s = %d, want 0", c[0], c[1], got)
		}
	}
}

func TestComparePrecision(t *testing.T) {
	cases := []struct {
		a, b   string
		cmp    int
		prefix bool
	}{
		{"4.15", "4.15.18", -1, true}, // "4.15" could be any 4.15.x
		{"2.4", "2.4.58", -1, true},
		{"6.1", "6.1.76", -1, true},
		{"9.8", "9.8p1", -1, false}, // a real, distinct release
		{"1.1.1", "1.1.1x", -1, false},
		{"5.4", "5.4.0", 0, false},
		{"4.15.18", "4.15", 1, false},
		{"4.14", "4.15.18", -1, false},
		{"1.0", "1.0rc1", 1, false},
	}
	for _, c := range cases {
		cmp, prefix := compareTokens(tokenize(c.a), tokenize(c.b))
		if cmp != c.cmp || prefix != c.prefix {
			t.Errorf("%s vs %s: got (%d,%v) want (%d,%v)", c.a, c.b, cmp, prefix, c.cmp, c.prefix)
		}
	}
}

func TestUpstreamVersion(t *testing.T) {
	cases := []struct{ manager, in, want string }{
		{"dpkg", "1:9.6p1-3ubuntu13.5", "9.6p1"},
		{"dpkg", "1:8.9p1-3ubuntu0.10", "8.9p1"},
		{"dpkg", "3.0.13-0ubuntu3.4", "3.0.13"},
		{"dpkg", "2.39-0ubuntu8.3", "2.39"},
		{"dpkg", "8.5.0-2ubuntu10.5", "8.5.0"},
		{"dpkg", "1.9.15p5-3ubuntu5.24.04.1", "1.9.15p5"},
		{"dpkg", "1:1.3.dfsg-3.1ubuntu2", "1.3"},
		{"dpkg", "2.2.4+dfsg1-1", "2.2.4"},
		{"dpkg", "1.2.3~rc1-1", "1.2.3~rc1"},
		{"dpkg", "2.4.62-1~deb12u2", "2.4.62"},
		{"dpkg", "7.88.1-10+deb12u8", "7.88.1"},
		{"dpkg", "5:27.3.1-1~ubuntu.24.04~noble", "27.3.1"},
		{"dpkg", "1.2+really1.1-2", "1.1"},
		{"dpkg", "3.0.2~bpo11+1", "3.0.2"},
		{"dpkg", "8:6.9.12.98+dfsg1-5.2build2", "6.9.12.98"},
		{"dpkg", "6.1.106-3", "6.1.106"},
		{"dpkg", "9.6p1", "9.6p1"},
		{"dpkg", "2:8.3+93ubuntu2", "8.3"},
		{"rpm", "3.0.7-27.el9", "3.0.7"},
		{"rpm", "1:1.1.1k-12.el8_9", "1.1.1k"},
		{"rpm", "5.14.0-427.13.1.el9_4", "5.14.0"},
		{"apk", "3.3.2-r0", "3.3.2"},
		{"apk", "9.7_p1-r4", "9.7_p1"},
		{"apk", "1.36.1-r29", "1.36.1"},
	}
	for _, c := range cases {
		if got := UpstreamVersion(c.manager, c.in); got != c.want {
			t.Errorf("%s %s: got %q want %q", c.manager, c.in, got, c.want)
		}
	}
	// the extracted OpenSSH version compares like the portable release
	if CompareVersions(UpstreamVersion("apk", "9.7_p1-r4"), "9.7p1") != 0 {
		t.Error("apk _p1 must equal p1")
	}
}

func TestDistroHintAndServiceVersion(t *testing.T) {
	for _, s := range []string{"9.6p1 Ubuntu 3ubuntu13.5", "Ubuntu Linux; protocol 2.0", "2.4.62 (Debian)", "7.9p1 Debian 10+deb10u4",
		"3.0.7-27.el9", "OpenSSH_8.0 el8", "Raspbian-5+deb11u3", "FreeBSD-20240701"} {
		if !hasDistroHint(s) {
			t.Errorf("%q: distro hint not detected", s)
		}
	}
	for _, s := range []string{"9.6p1", "protocol 2.0", "1.18.0", "nginx", "Grafana 10.2.3", "debugger", ""} {
		if hasDistroHint(s) {
			t.Errorf("%q: false distro hint", s)
		}
	}
	for in, want := range map[string]string{
		"9.6p1 Ubuntu 3ubuntu13.5": "9.6p1",
		"2.4.X":                    "2.4",
		"2.X":                      "2",
		"1.18.0":                   "1.18.0",
		"2.4.58 (Ubuntu)":          "2.4.58",
		"":                         "",
	} {
		if got := serviceVersion(in); got != want {
			t.Errorf("serviceVersion(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestExactMatchUpdates(t *testing.T) {
	mk := func(version string) *entry {
		e, ok := buildEntry(CPE{Part: "a", Vendor: "openbsd", Product: "openssh", Version: version, Update: "*"}, kindPort, "nmap", "", "", false, 0)
		if !ok {
			t.Fatalf("entry %s", version)
		}
		return &e
	}
	crit := func(version, upd string) *criteria {
		c := &criteria{version: version, upd: upd, vtoks: tokenize(version)}
		c.fullToks = c.vtoks
		if specific(upd) {
			c.fullToks = tokenize(version + " " + upd)
		}
		return c
	}
	cases := []struct {
		dev, ver, upd string
		want          bool
	}{
		{"8.5p1", "8.5", "p1", true},
		{"8.5p2", "8.5", "p1", false},
		{"4.4p1", "4.4", "-", false}, // NA update: only the plain release
		{"4.4", "4.4", "-", true},
		{"9.6p1", "9.6", "*", true}, // ANY update covers p1
		{"9.6", "9.6", "*", true},
		{"1.1.1w", "1.1.1", "*", false}, // a letter release is a different version
		{"2.0rc1", "2.0", "*", true},
		{"9.6.1", "9.6", "*", false},
	}
	for _, c := range cases {
		if got := exactMatch(mk(c.dev), crit(c.ver, c.upd)); got != c.want {
			t.Errorf("%s vs %s:%s = %v, want %v", c.dev, c.ver, c.upd, got, c.want)
		}
	}
}
