package main

import (
	"strconv"
	"strings"
)

// windowsProductName builds the OS name from the registry values ProductName, DisplayVersion
// and CurrentBuildNumber. Windows 11 still reports "Windows 10" as its ProductName; builds
// from 22000 on are Windows 11.
func windowsProductName(product, display, build string) string {
	name := strings.TrimSpace(product)
	if b, err := strconv.Atoi(strings.TrimSpace(build)); err == nil && b >= 22000 && strings.HasPrefix(name, "Windows 10") {
		name = "Windows 11" + strings.TrimPrefix(name, "Windows 10")
	}
	if d := strings.TrimSpace(display); d != "" && name != "" {
		name += " " + d
	}
	return name
}
