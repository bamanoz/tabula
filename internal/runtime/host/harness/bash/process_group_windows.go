//go:build windows

package bash

import "os/exec"

func configureProcessGroup(*exec.Cmd) {}

func terminateProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
