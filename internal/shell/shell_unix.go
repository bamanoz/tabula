//go:build !windows

package shell

import (
	"os/exec"
	"syscall"
)

// Command creates a platform-appropriate shell command.
func Command(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}

// SpawnCommand creates a long-running command that is placed into its own
// process group so the kernel can terminate the entire group (including any
// children spawned by the wrapping shell) with a single signal.
func SpawnCommand(command string) *exec.Cmd {
	cmd := Command(command)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}
