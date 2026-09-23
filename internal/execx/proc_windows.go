//go:build windows

package execx

import "os/exec"

func setProcAttrs(cmd *exec.Cmd) {}

func killTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
