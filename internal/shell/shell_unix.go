//go:build !windows

package shell

import "os/exec"

// Command creates a platform-appropriate shell command.
func Command(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}
