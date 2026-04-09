//go:build !windows

package main

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
)

// venvBinDir returns the platform-specific venv binary directory.
func venvBinDir(tabulaHome string) string {
	return filepath.Join(tabulaHome, ".venv", "bin")
}

// mainShellCommand creates a platform-appropriate shell command.
func mainShellCommand(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}

// waitForShutdownSignal blocks until SIGINT or SIGTERM is received.
func waitForShutdownSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}
