package hostscript

import (
	_ "embed"
	"strconv"
	"strings"
	"time"
)

//go:embed windows.ps1
var windowsScript string

// Windows returns the PowerShell inventory script of a Windows host (NetScope agent). It
// prints one JSON document; Docker is not collected on Windows.
func Windows(o Options) string {
	t := max(int(o.CommandTimeout/time.Second), 1)
	pk := "$false"
	if o.Packages {
		pk = "$true"
	}
	return strings.NewReplacer("@@PACKAGES@@", pk, "@@TIMEOUT@@", strconv.Itoa(t)).Replace(windowsScript)
}

// WindowsMaxDuration bounds one run of the Windows script: the queries themselves take a few
// seconds, the Windows Update search is limited to commandTimeout.
func WindowsMaxDuration(commandTimeout time.Duration) time.Duration {
	return 2*commandTimeout + 2*time.Minute
}
