package wininv

import (
	"strings"
)

// NVD product names of the Windows releases by build number. The NVD lists Windows CVEs per
// release with the fixed build as upper bound (versionEndExcluding 10.0.22631.4460), so the
// full version including the update build revision (UBR) gives the missing patches.
var (
	clientProducts = map[string]string{
		"10240": "windows_10_1507", "10586": "windows_10_1511", "14393": "windows_10_1607", "15063": "windows_10_1703",
		"16299": "windows_10_1709", "17134": "windows_10_1803", "17763": "windows_10_1809", "18362": "windows_10_1903",
		"18363": "windows_10_1909", "19041": "windows_10_2004", "19042": "windows_10_20h2", "19043": "windows_10_21h1",
		"19044": "windows_10_21h2", "19045": "windows_10_22h2",
		"22000": "windows_11_21h2", "22621": "windows_11_22h2", "22631": "windows_11_23h2", "26100": "windows_11_24h2",
		"26200": "windows_11_25h2",
	}
	serverProducts = map[string]string{
		"14393": "windows_server_2016", "17763": "windows_server_2019", "20348": "windows_server_2022",
		"25398": "windows_server_2022_23h2", "26100": "windows_server_2025",
	}
)

// osCPE returns the CPE of a Windows release, or "" when the release is unknown or the
// version lacks the update build revision (without it every CVE of the release would match).
func osCPE(productType int, build, version, architecture string) string {
	products := clientProducts
	if productType == 2 || productType == 3 {
		products = serverProducts
	}
	product, ok := products[strings.TrimSpace(build)]
	if !ok || strings.Count(version, ".") != 3 || !strings.Contains(version, "."+build+".") {
		return ""
	}
	return "cpe:2.3:o:microsoft:" + product + ":" + version + ":*:*:*:*:*:" + targetHW(architecture) + ":*"
}

// targetHW maps Win32_OperatingSystem.OSArchitecture ("64-Bit", "ARM 64-bit Processor",
// localised) to the CPE target hardware.
func targetHW(arch string) string {
	a := strings.ToLower(arch)
	switch {
	case strings.Contains(a, "arm"):
		return "arm64"
	case strings.Contains(a, "64"):
		return "x64"
	case strings.Contains(a, "32"):
		return "x86"
	}
	return "*"
}
