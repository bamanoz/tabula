//go:build windows

package shell

import "os/exec"

// Command creates a platform-appropriate shell command.
func Command(command string) *exec.Cmd {
	return exec.Command("cmd", "/c", command)
}

// SpawnCommand mirrors the unix version but on Windows we don't have process
// groups in the same shape; the caller falls back to terminating the root
// process directly.
func SpawnCommand(command string) *exec.Cmd {
	return Command(command)
}
