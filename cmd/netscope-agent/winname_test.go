package main

import "testing"

func TestWindowsProductName(t *testing.T) {
	for _, c := range []struct{ product, display, build, want string }{
		{"Windows 10 Pro", "23H2", "22631", "Windows 11 Pro 23H2"},
		{"Windows 10 Enterprise", "22H2", "19045", "Windows 10 Enterprise 22H2"},
		{"Windows Server 2022 Standard", "21H2", "20348", "Windows Server 2022 Standard 21H2"},
		{"Windows Server 2025 Datacenter", "24H2", "26100", "Windows Server 2025 Datacenter 24H2"},
		{"Windows Server 2016 Standard", "", "14393", "Windows Server 2016 Standard"},
		{"", "", "", ""},
	} {
		if got := windowsProductName(c.product, c.display, c.build); got != c.want {
			t.Errorf("%q %q %q: %q", c.product, c.display, c.build, got)
		}
	}
}
