package ssh

import "netscope/internal/hostscript"

// The collection script and its output splitter live in internal/hostscript (shared with
// the NetScope agent); the names below keep the parsers in this package readable.

const (
	secDate         = hostscript.Date
	secOSRelease    = hostscript.OSRelease
	secUname        = hostscript.Uname
	secHostname     = hostscript.Hostname
	secCPUInfo      = hostscript.CPUInfo
	secNproc        = hostscript.Nproc
	secMeminfo      = hostscript.Meminfo
	secUptime       = hostscript.Uptime
	secDF           = hostscript.DF
	secLsblk        = hostscript.Lsblk
	secIPAddr       = hostscript.IPAddr
	secPkgDpkg      = hostscript.PkgDpkg
	secPkgRPM       = hostscript.PkgRPM
	secPkgApk       = hostscript.PkgApk
	secLastDpkg     = hostscript.LastDpkg
	secLastApt      = hostscript.LastApt
	secLastRPM      = hostscript.LastRPM
	secLastApk      = hostscript.LastApk
	secServices     = hostscript.Services
	secSockets      = hostscript.Sockets
	secDockerPS     = hostscript.DockerPS
	secDockerImages = hostscript.DockerImages
)

type (
	scriptOptions = hostscript.Options
	sectionOutput = hostscript.Section
)

func buildScript(o scriptOptions) string { return hostscript.Build(o) }

func splitOutput(stdout, stderr []byte) map[string]*sectionOutput {
	return hostscript.Split(stdout, stderr)
}
