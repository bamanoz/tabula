//go:build windows

package main

import (
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"

	"github.com/bamanoz/tabula/internal/shell"
)

// venvBinDir returns the platform-specific venv binary directory.
func venvBinDir(tabulaHome string) string {
	return filepath.Join(tabulaHome, ".venv", "Scripts")
}

// mainShellCommand creates a platform-appropriate shell command.
func mainShellCommand(command string) *exec.Cmd {
	return shell.Command(command)
}

// waitForShutdownSignal blocks until Ctrl+C (os.Interrupt) is received.
func waitForShutdownSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	<-sigCh
}
