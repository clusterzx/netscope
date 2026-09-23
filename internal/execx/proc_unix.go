//go:build !windows

package execx

import (
	"os/exec"
	"syscall"
)

// setProcAttrs puts the child in its own process group so the whole tree can be killed.
func setProcAttrs(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	// Negative pid = process group.
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil {
		return cmd.Process.Kill()
	}
	return nil
}
