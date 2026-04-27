//go:build windows

package plugin

import "os/exec"

func configurePluginProcessGroup(cmd *exec.Cmd) {}

func terminatePluginProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
