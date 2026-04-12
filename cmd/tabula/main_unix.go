//go:build !windows

package main

import (
	"os"
	"os/exec"
	"os/signal"
	"syscall"

	"github.com/bamanoz/tabula/internal/shell"
)

// mainShellCommand creates a platform-appropriate shell command.
func mainShellCommand(command string) *exec.Cmd {
	return shell.Command(command)
}

// waitForShutdownSignal blocks until SIGINT or SIGTERM is received.
func waitForShutdownSignal() {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh
}
