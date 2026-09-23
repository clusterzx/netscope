package ssh

import (
	"bytes"
	"strconv"
	"strings"
	"time"
)

// The inventory is collected with ONE remote shell session: a POSIX sh script built from
// the fixed, read-only command list below. Every command writes into its own section,
// delimited by marker lines, and is parsed by its own parser. Nothing in the list is
// configurable – settings only switch whole sections on or off.

// marker starts every control line of the script output (stdout and stderr).
const marker = "@@NETSCOPE@@"

// Section names.
const (
	secDate         = "date"
	secOSRelease    = "os_release"
	secUname        = "uname"
	secHostname     = "hostname"
	secCPUInfo      = "cpuinfo"
	secNproc        = "nproc"
	secMeminfo      = "meminfo"
	secUptime       = "uptime"
	secDF           = "df"
	secLsblk        = "lsblk"
	secIPAddr       = "ip_addr"
	secPkgDpkg      = "pkg_dpkg"
	secPkgRPM       = "pkg_rpm"
	secPkgApk       = "pkg_apk"
	secLastDpkg     = "last_dpkg"
	secLastApt      = "last_apt"
	secLastRPM      = "last_rpm"
	secLastApk      = "last_apk"
	secServices     = "services"
	secSockets      = "sockets"
	secDockerPS     = "docker_ps"
	secDockerImages = "docker_images"
)

// variant is one way to produce a section. The first variant whose required program
// exists (command -v) is executed; if none exists the section is reported as missing.
type variant struct {
	name string // reported with the section, tells the parser which format to expect
	need string // program that must exist ("" = always available)
	cmd  string // shell command; $TO expands to the timeout(1) prefix when available
}

type section struct {
	name     string
	variants []variant
	packages bool // only with collect_packages
	docker   bool // only with collect_docker
}

// commands is the fixed list of read-only commands.
var commands = []section{
	{name: secDate, variants: []variant{{cmd: `date +'%s %z'`}}},
	{name: secOSRelease, variants: []variant{{cmd: `cat /etc/os-release 2>/dev/null || cat /usr/lib/os-release`}}},
	{name: secUname, variants: []variant{{cmd: `uname -srmo 2>/dev/null || uname -srm`}}},
	{name: secHostname, variants: []variant{{cmd: `hostname 2>/dev/null || cat /proc/sys/kernel/hostname`}}},
	{name: secCPUInfo, variants: []variant{{cmd: `cat /proc/cpuinfo`}}},
	{name: secNproc, variants: []variant{{cmd: `nproc 2>/dev/null || getconf _NPROCESSORS_ONLN`}}},
	{name: secMeminfo, variants: []variant{{cmd: `cat /proc/meminfo`}}},
	{name: secUptime, variants: []variant{{cmd: `cat /proc/uptime`}}},
	{name: secDF, variants: []variant{{need: "df", cmd: `$TO df -P -T`}}},
	{name: secLsblk, variants: []variant{{need: "lsblk",
		cmd: `$TO lsblk -J -b -o NAME,TYPE,SIZE,MODEL,SERIAL,ROTA,TRAN,FSTYPE,MOUNTPOINT 2>/dev/null || $TO lsblk -J -b`}}},
	{name: secIPAddr, variants: []variant{
		{name: "ip", need: "ip", cmd: `ip -j addr 2>/dev/null || ip addr`},
	}},
	{name: secPkgDpkg, packages: true, variants: []variant{{need: "dpkg-query",
		cmd: `$TO dpkg-query -W -f='${Package}\t${Version}\t${Architecture}\t${db:Status-Abbrev}\n'`}}},
	{name: secPkgRPM, packages: true, variants: []variant{{need: "rpm",
		cmd: `$TO rpm -qa --qf '%{NAME}\t%{EPOCH}\t%{VERSION}-%{RELEASE}\t%{ARCH}\n'`}}},
	{name: secPkgApk, packages: true, variants: []variant{{need: "apk",
		cmd: `$TO apk list -I 2>/dev/null || $TO apk info -v`}}},
	{name: secLastDpkg, variants: []variant{{need: "dpkg-query", cmd: `stat -c %Y /var/lib/dpkg/status`}}},
	{name: secLastApt, variants: []variant{{need: "dpkg-query",
		cmd: `grep '^Start-Date:' /var/log/apt/history.log 2>/dev/null | tail -n 1`}}},
	{name: secLastRPM, variants: []variant{{need: "rpm", cmd: `$TO rpm -qa --last | head -n 1`}}},
	{name: secLastApk, variants: []variant{{need: "apk", cmd: `stat -c %Y /lib/apk/db/installed`}}},
	{name: secServices, variants: []variant{{need: "systemctl",
		cmd: `$TO systemctl list-units --type=service --state=running --no-legend --plain --no-pager`}}},
	{name: secSockets, variants: []variant{
		{name: "ss", need: "ss", cmd: `$TO ss -tulpnH 2>/dev/null || $TO ss -tulpn`},
		{name: "netstat", need: "netstat", cmd: `$TO netstat -tulpn`},
	}},
	{name: secDockerPS, docker: true, variants: []variant{{need: "docker",
		cmd: `$TO docker ps -a --no-trunc --format '{{json .}}'`}}},
	{name: secDockerImages, docker: true, variants: []variant{{need: "docker",
		cmd: `$TO docker images --no-trunc --format '{{json .}}'`}}},
}

// scriptOptions switch optional sections.
type scriptOptions struct {
	Packages       bool
	Docker         bool
	CommandTimeout time.Duration
}

// buildScript returns the POSIX sh script for the given options.
func buildScript(o scriptOptions) string {
	t := int(o.CommandTimeout / time.Second)
	if t < 1 {
		t = 1
	}
	ts := strconv.Itoa(t)
	var b strings.Builder
	b.WriteString(`export LC_ALL=C LANG=C
PATH="$PATH:/usr/local/sbin:/usr/sbin:/sbin:/usr/local/bin:/usr/bin:/bin"; export PATH
M='` + marker + `'
b() { printf '\n%s %s %s\n' "$M" "$1" "$2"; printf '%s %s %s\n' "$M" "$1" "$2" >&2; }
r() { printf '\n%s = %s\n' "$M" "$1"; }
has() { command -v "$1" >/dev/null 2>&1; }
TO=
if has timeout; then
  if timeout 5 true >/dev/null 2>&1; then TO="timeout ` + ts + `"
  elif timeout -t 5 true >/dev/null 2>&1; then TO="timeout -t ` + ts + `"; fi
fi
`)
	for _, s := range commands {
		if (s.packages && !o.Packages) || (s.docker && !o.Docker) {
			continue
		}
		for i, v := range s.variants {
			name := v.name
			if name == "" {
				name = "-"
			}
			run := "b " + s.name + " " + name + "; " + v.cmd + "; r $?"
			if v.need == "" {
				if i > 0 {
					b.WriteString("else " + run + "; fi\n")
				} else {
					b.WriteString(run + "\n")
				}
				break
			}
			kw := "if"
			if i > 0 {
				kw = "elif"
			}
			b.WriteString(kw + " has " + v.need + "; then " + run + "\n")
			if i == len(s.variants)-1 {
				b.WriteString("else b " + s.name + " -; r -; fi\n")
			}
		}
	}
	return b.String()
}

// sectionOutput is the captured output of one section.
type sectionOutput struct {
	Name    string
	Variant string
	Out     string
	Err     string
	RC      int
	Missing bool // no variant's program exists on the host
	Done    bool // end marker seen (not cut off by the output limit or a timeout)
}

// OK reports whether the section ran to completion with exit status 0.
func (s *sectionOutput) OK() bool { return s != nil && s.Done && !s.Missing && s.RC == 0 }

// splitOutput splits the script output into sections. Text outside of sections (login
// banners) is ignored.
func splitOutput(stdout, stderr []byte) map[string]*sectionOutput {
	out := map[string]*sectionOutput{}
	var (
		cur   *sectionOutput
		lines []string
	)
	finish := func() {
		if cur == nil || cur.Done {
			return
		}
		// the end marker is preceded by an extra newline
		if n := len(lines); n > 0 && lines[n-1] == "" {
			lines = lines[:n-1]
		}
		cur.Out = strings.Join(lines, "\n")
		lines = nil
	}
	for _, line := range splitLines(stdout) {
		rest, ok := strings.CutPrefix(line, marker+" ")
		if !ok {
			if cur != nil && !cur.Done {
				lines = append(lines, line)
			}
			continue
		}
		f := strings.Fields(rest)
		if len(f) == 0 {
			continue
		}
		if f[0] == "=" {
			if cur == nil || cur.Done {
				continue
			}
			code := "-"
			if len(f) > 1 {
				code = f[1]
			}
			finish()
			cur.Done = true
			if code == "-" {
				cur.Missing = true
			} else if n, err := strconv.Atoi(code); err == nil {
				cur.RC = n
			} else {
				cur.RC = -1
			}
			continue
		}
		finish()
		cur = &sectionOutput{Name: f[0], RC: -1}
		if len(f) > 1 && f[1] != "-" {
			cur.Variant = f[1]
		}
		out[cur.Name] = cur
	}
	if cur != nil && !cur.Done {
		finish()
	}
	// stderr carries the same begin markers
	var (
		errName  string
		errLines = map[string][]string{}
	)
	for _, line := range splitLines(stderr) {
		if rest, ok := strings.CutPrefix(line, marker+" "); ok {
			if f := strings.Fields(rest); len(f) > 0 {
				errName = f[0]
			}
			continue
		}
		if errName != "" && strings.TrimSpace(line) != "" {
			errLines[errName] = append(errLines[errName], strings.TrimSpace(line))
		}
	}
	for name, ls := range errLines {
		if s, ok := out[name]; ok {
			s.Err = strings.Join(ls, "\n")
		}
	}
	return out
}

// splitLines splits text into lines without line terminators (handles \r\n).
func splitLines(b []byte) []string {
	if len(b) == 0 {
		return nil
	}
	s := string(bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")))
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n")
}
