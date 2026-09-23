package cve

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestParseCPE23(t *testing.T) {
	cases := []struct {
		in   string
		want CPE
	}{
		{"cpe:2.3:a:openbsd:openssh:9.6:p1:*:*:*:*:*:*", CPE{"a", "openbsd", "openssh", "9.6", "p1"}},
		{"cpe:2.3:a:openbsd:openssh:4.4:-:*:*:*:*:*:*", CPE{"a", "openbsd", "openssh", "4.4", "-"}},
		{"cpe:2.3:o:linux:linux_kernel:*:*:*:*:*:*:*:*", CPE{"o", "linux", "linux_kernel", "*", "*"}},
		// escaped colon inside the product (CVE-2024-35779)
		{`cpe:2.3:a:blueastral:page_builder\:_live_composer:*:*:*:*:*:wordpress:*:*`, CPE{"a", "blueastral", "page_builder:_live_composer", "*", "*"}},
		// escaped plus signs (CVE-2024-23807)
		{`cpe:2.3:a:apache:xerces-c\+\+:*:*:*:*:*:*:*:*`, CPE{"a", "apache", "xerces-c++", "*", "*"}},
		// escaped parentheses in version and update (Cisco)
		{`cpe:2.3:a:cisco:finesse:11.6\(1\):es4:*:*:*:*:*:*`, CPE{"a", "cisco", "finesse", "11.6(1)", "es4"}},
		{`cpe:2.3:a:admiror-design-studio:admirorframes:*:*:*:*:*:joomla\!:*:*`, CPE{"a", "admiror-design-studio", "admirorframes", "*", "*"}},
		// escaped backslash
		{`cpe:2.3:a:vendor:pro\\duct:1.0:*:*:*:*:*:*:*`, CPE{"a", "vendor", `pro\duct`, "1.0", "*"}},
		// short form and upper case
		{"CPE:2.3:A:Grafana:Grafana:11.0.0", CPE{"a", "grafana", "grafana", "11.0.0", "*"}},
		{"cpe:2.3:a:grafana:grafana", CPE{"a", "grafana", "grafana", "*", "*"}},
	}
	for _, c := range cases {
		got, err := ParseCPE(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %+v want %+v", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "cpe:2.3:x:v:p:1", "cpe:2.3:a:vendor", `cpe:2.3:a:v:p:1\`, "cpe:2.3:a:*:p:1", "openssh 9.6"} {
		if _, err := ParseCPE(bad); err == nil {
			t.Errorf("%q: expected error", bad)
		}
	}
}

func TestParseCPE22(t *testing.T) {
	cases := []struct {
		in   string
		want CPE
	}{
		{"cpe:/a:openbsd:openssh:9.6p1", CPE{"a", "openbsd", "openssh", "9.6p1", "*"}},
		{"cpe:/o:linux:linux_kernel", CPE{"o", "linux", "linux_kernel", "*", "*"}},
		{"cpe:/a:igor_sysoev:nginx:1.18.0", CPE{"a", "igor_sysoev", "nginx", "1.18.0", "*"}},
		{"cpe:/o:microsoft:windows_7::sp1", CPE{"o", "microsoft", "windows_7", "*", "sp1"}},
		{"cpe:/a:vendor:c%2b%2b:1.0", CPE{"a", "vendor", "c++", "1.0", "*"}},
		{"cpe:/h:mikrotik:routerboard", CPE{"h", "mikrotik", "routerboard", "*", "*"}},
	}
	for _, c := range cases {
		got, err := ParseCPE(c.in)
		if err != nil {
			t.Errorf("%s: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %+v want %+v", c.in, got, c.want)
		}
	}
	if _, err := ParseCPE("cpe:/a:vendor"); err == nil {
		t.Error("expected error for incomplete URI")
	}
}

func TestCPEStringRoundTrip(t *testing.T) {
	for _, c := range []CPE{
		{"a", "apache", "xerces-c++", "3.2.4", "*"},
		{"a", "blueastral", "page_builder:_live_composer", "1.5.42", "*"},
		{"a", "cisco", "finesse", "11.6(1)", "es4"},
		{"a", "openbsd", "openssh", "4.4", "-"},
		{"o", "linux", "linux_kernel", "6.1.69", "*"},
		{"a", "nodejs", "node.js", "20.11.1", "*"},
	} {
		s := c.String()
		back, err := ParseCPE23(s)
		if err != nil || back != c {
			t.Errorf("%+v -> %s -> %+v (%v)", c, s, back, err)
		}
	}
	if got := (CPE{"a", "apache", "xerces-c++", "3.2.4", "*"}).String(); got != `cpe:2.3:a:apache:xerces-c\+\+:3.2.4:*:*:*:*:*:*:*` {
		t.Errorf("escaping: %s", got)
	}
}

func TestAliases(t *testing.T) {
	if v, p := canonical("igor_sysoev", "nginx"); v != "f5" || p != "nginx" {
		t.Errorf("nginx canonical: %s:%s", v, p)
	}
	if v, p := canonical("matt_johnston", "dropbear_ssh_server"); v != "dropbear_ssh_project" || p != "dropbear_ssh" {
		t.Errorf("dropbear canonical: %s:%s", v, p)
	}
	// the http scanner reports Proxmox VE as proxmox:proxmox_ve, the NVD uses virtual_environment
	if v, p := canonical("proxmox", "proxmox_ve"); v != "proxmox" || p != "virtual_environment" {
		t.Errorf("proxmox canonical: %s:%s", v, p)
	}
	if v, p := canonical("openbsd", "openssh"); v != "openbsd" || p != "openssh" {
		t.Errorf("unaliased product changed: %s:%s", v, p)
	}
	found := false
	for _, vp := range aliases("f5", "nginx") {
		if vp == (vendorProduct{"f5", "nginx_open_source"}) {
			found = true
		}
	}
	if !found {
		t.Error("nginx_open_source missing from nginx aliases")
	}
	seen := map[vendorProduct]bool{}
	for _, g := range aliasGroups {
		for _, vp := range g {
			if seen[vp] {
				t.Errorf("%v is in two alias groups", vp)
			}
			seen[vp] = true
		}
	}
}

// Every criteria string of the real fixtures must parse.
func TestFixtureCriteriaParse(t *testing.T) {
	for _, name := range []string{"nvdcve-2.0-2024.json.gz", "nvdcve-2.0-2021.json.gz"} {
		rd, err := openFeed(filepath.Join("testdata", name))
		if err != nil {
			t.Fatal(err)
		}
		crits := 0
		_, err = parseFeed(context.Background(), rd, nil, func(r *cveRecord) error {
			crits += len(r.Matches)
			for _, m := range r.Matches {
				if m.Vendor == "" || m.Product == "" || m.Version == "" {
					t.Errorf("%s: incomplete criteria %+v", r.ID, m)
				}
			}
			return nil
		})
		rd.Close()
		if err != nil {
			t.Fatal(err)
		}
		if crits == 0 {
			t.Fatalf("%s: no criteria", name)
		}
	}
	if _, err := os.Stat(filepath.Join("testdata", "nvdcve-2.0-2024.json.gz")); err != nil {
		t.Fatal(err)
	}
}
