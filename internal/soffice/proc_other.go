//go:build !unix

package soffice

import "os/exec"

// setProcessGroup does nothing on non-Unix systems.
func setProcessGroup(cmd *exec.Cmd) {}

// killGroup kills the command's process if it has started.
func killGroup(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
}
