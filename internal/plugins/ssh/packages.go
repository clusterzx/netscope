package ssh

import (
	"sort"
	"strings"

	"netscope/internal/plugin"
)

// parseDpkg parses dpkg-query -W -f='${Package}\t${Version}\t${Architecture}\t${db:Status-Abbrev}\n'.
// Only installed packages are returned (status "ii", "hi" …; also accepts the long
// "${Status}" form "install ok installed").
func parseDpkg(s string) []plugin.Package {
	var out []plugin.Package
	for _, line := range strings.Split(s, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) < 3 || strings.TrimSpace(f[0]) == "" {
			continue
		}
		if len(f) >= 4 && !dpkgInstalled(strings.TrimSpace(f[3])) {
			continue
		}
		out = append(out, plugin.Package{Name: strings.TrimSpace(f[0]), Version: strings.TrimSpace(f[1]), Arch: strings.TrimSpace(f[2])})
	}
	return sortPackages(out)
}

func dpkgInstalled(status string) bool {
	if w := strings.Fields(status); len(w) == 3 {
		switch w[2] {
		case "installed", "triggers-awaiting", "triggers-pending":
			return true
		}
		return false
	}
	// db:Status-Abbrev: desired, current, error flag – current i/W/t means installed
	if len(status) < 2 {
		return false
	}
	switch status[1] {
	case 'i', 'W', 't':
		return true
	}
	return false
}

// parseRPM parses rpm -qa --qf '%{NAME}\t%{EPOCH}\t%{VERSION}-%{RELEASE}\t%{ARCH}\n'.
// The version carries the epoch when set ("2:4.9-8.el9"); gpg-pubkey pseudo packages
// are skipped.
func parseRPM(s string) []plugin.Package {
	var out []plugin.Package
	for _, line := range strings.Split(s, "\n") {
		f := strings.Split(strings.TrimRight(line, "\r"), "\t")
		if len(f) < 4 {
			continue
		}
		name, epoch, version, arch := strings.TrimSpace(f[0]), strings.TrimSpace(f[1]), strings.TrimSpace(f[2]), strings.TrimSpace(f[3])
		if name == "" || name == "gpg-pubkey" || version == "" {
			continue
		}
		if epoch != "" && epoch != "(none)" && epoch != "0" {
			version = epoch + ":" + version
		}
		if arch == "(none)" {
			arch = ""
		}
		out = append(out, plugin.Package{Name: name, Version: version, Arch: arch})
	}
	return sortPackages(out)
}

// parseApk parses `apk list -I` ("musl-1.2.5-r12 x86_64 {musl} (MIT) [installed]") or
// `apk info -v` ("musl-1.2.5-r12"). Warning lines are skipped.
func parseApk(s string) []plugin.Package {
	var out []plugin.Package
	for _, line := range strings.Split(s, "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasSuffix(f[0], ":") || f[0] == "WARNING" || f[0] == "ERROR" {
			continue
		}
		if len(f) > 1 && !strings.Contains(line, "[installed]") && !strings.Contains(line, "[upgradable") {
			continue
		}
		name, version, ok := splitApkName(f[0])
		if !ok {
			continue
		}
		p := plugin.Package{Name: name, Version: version}
		if len(f) > 1 && !strings.HasPrefix(f[1], "{") {
			p.Arch = f[1]
		}
		out = append(out, p)
	}
	return sortPackages(out)
}

// splitApkName splits "ca-certificates-bundle-20260909-r0" into name and "20260909-r0".
func splitApkName(s string) (string, string, bool) {
	rel := strings.LastIndex(s, "-")
	if rel <= 0 || rel+2 > len(s) || s[rel+1] != 'r' {
		return "", "", false
	}
	ver := strings.LastIndex(s[:rel], "-")
	if ver <= 0 || ver+1 >= rel || s[ver+1] < '0' || s[ver+1] > '9' {
		return "", "", false
	}
	return s[:ver], s[ver+1:], true
}

func sortPackages(list []plugin.Package) []plugin.Package {
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Name != list[j].Name {
			return list[i].Name < list[j].Name
		}
		return list[i].Arch < list[j].Arch
	})
	return list
}
