//go:build !windows

package plugin

import (
	"os/exec"
	"syscall"
)

func configurePluginProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminatePluginProcessGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err == nil && pgid > 0 {
		if killErr := syscall.Kill(-pgid, syscall.SIGTERM); killErr == nil {
			return nil
		}
	}
	return cmd.Process.Kill()
}
