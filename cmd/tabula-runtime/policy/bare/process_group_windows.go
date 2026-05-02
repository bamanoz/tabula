//go:build windows

package bare

import "os/exec"

func configureWorkerProcessGroup(*exec.Cmd) {}

func terminateWorkerProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
