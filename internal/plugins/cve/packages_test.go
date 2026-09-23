package cve

import (
	"path"
	"testing"
)

func TestLookupPackage(t *testing.T) {
	cases := []struct {
		name    string
		product string // vendor:product, "" = not mapped
		kernel  bool
	}{
		{"openssh-server", "openbsd:openssh", false},
		{"openssh-client", "openbsd:openssh", false},
		{"openssl", "openssl:openssl", false},
		{"libssl3t64", "openssl:openssl", false},
		{"libssl1.1", "openssl:openssl", false},
		{"nginx-core", "f5:nginx", false},
		{"apache2", "apache:http_server", false},
		{"httpd", "apache:http_server", false},
		{"sudo", "sudo_project:sudo", false},
		{"libcurl4t64", "haxx:curl", false},
		{"libc6", "gnu:glibc", false},
		{"xz-utils", "tukaani:xz", false},
		{"zlib1g", "zlib:zlib", false},
		{"python3.12-minimal", "python:python", false},
		{"libpython3.11-stdlib", "python:python", false},
		{"postgresql-16", "postgresql:postgresql", false},
		{"php8.3-cli", "php:php", false},
		{"linux-image-6.8.0-45-generic", "linux:linux_kernel", true},
		{"linux-image-amd64", "linux:linux_kernel", true},
		{"proxmox-kernel-6.8.12-4-pve-signed", "linux:linux_kernel", true},
		{"kernel-core", "linux:linux_kernel", true},
		{"docker-ce", "mobyproject:moby", false},
		// must not be mapped: different projects that merely share a prefix
		{"python3-requests", "", false},
		{"postgresql-16-postgis-3", "", false},
		{"postgresql-client-16", "", false},
		{"libsslcommon2", "", false},
		{"docker", "", false},
		{"foo", "", false},
	}
	for _, c := range cases {
		r, ok := lookupPackage(c.name)
		got := ""
		if ok {
			got = r.vendor + ":" + r.product
		}
		if got != c.product || (ok && r.kernel != c.kernel) {
			t.Errorf("%s: got %q kernel=%v, want %q kernel=%v", c.name, got, r.kernel, c.product, c.kernel)
		}
	}
}

func TestPackageRulesValid(t *testing.T) {
	for _, r := range packageRules {
		if r.part != "a" && r.part != "o" {
			t.Errorf("%s:%s: bad part %q", r.vendor, r.product, r.part)
		}
		for _, n := range r.names {
			if _, err := path.Match(n, "x"); err != nil {
				t.Errorf("bad pattern %q: %v", n, err)
			}
		}
	}
	where, args := packageFilter()
	if where == "" || len(args) == 0 {
		t.Fatal("empty package filter")
	}
}

func TestKernelVersionUsable(t *testing.T) {
	cases := []struct {
		upstream, full string
		want           bool
	}{
		{"6.8.0", "6.8.0-45.45", false},              // Ubuntu ABI version
		{"5.14.0", "5.14.0-427.13.1.el9_4", false},   // RHEL
		{"6.5.0", "6.5.0-35.35~22.04.1", false},      // Ubuntu HWE
		{"6.1.106", "6.1.106-3", true},               // Debian
		{"6.8.12", "6.8.12-4", true},                 // Proxmox
		{"6.10.11", "6.10.11-200.fc40", true},        // Fedora
		{"6.6.31", "6.6.31-r0", true},                // Alpine
		{"6.8", "6.8-1", false},                      // imprecise
		{"6.8.0", "6.8.0", true},                     // vanilla kernel
		{"4.19.0", "4.19.0-27-amd64", false},         // Debian ABI meta version
		{"6.12.0", "6.12.0-rc1", false},              // pre-release with ".0" patch level
		{"5.15.167", "5.15.167-1-pve", true},         // Proxmox 7
		{"6.1.0", "6.1.0-25-amd64", false},           // Debian ABI package version
		{"3.10.0", "3.10.0-1160.119.1.el7", false},   // CentOS 7
		{"5.10.0", "5.10.0-0.deb10.30-amd64", false}, // backport ABI
	}
	for _, c := range cases {
		if got := kernelVersionUsable(c.upstream, c.full); got != c.want {
			t.Errorf("%s (%s) = %v, want %v", c.upstream, c.full, got, c.want)
		}
	}
}
