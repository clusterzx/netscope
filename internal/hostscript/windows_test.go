package hostscript

import (
	"strings"
	"testing"
	"time"
)

func TestWindowsScript(t *testing.T) {
	s := Windows(Options{Packages: false, CommandTimeout: 45 * time.Second})
	if strings.Contains(s, "@@") || !strings.Contains(s, "$collectPackages = $false") || !strings.Contains(s, "$timeoutSec = 45") {
		t.Fatal("placeholders not replaced")
	}
	for i, r := range s {
		if r > 0x7e || (r < 0x20 && r != '\n' && r != '\r' && r != '\t') {
			t.Fatalf("non-ASCII character %q at %d: the script reaches PowerShell through stdin", r, i)
		}
	}
	if !strings.Contains(Windows(Options{Packages: true}), "$collectPackages = $true") {
		t.Fatal("packages switch")
	}
}
