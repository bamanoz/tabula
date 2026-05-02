//go:build !windows

package bare

import (
	"os/exec"
	"syscall"
)

func configureWorkerProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func terminateWorkerProcessGroup(cmd *exec.Cmd) error {
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
