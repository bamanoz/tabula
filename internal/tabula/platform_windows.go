//go:build windows

package tabula

import (
	"os"
	"os/exec"
	"os/signal"

	"github.com/bamanoz/tabula/internal/shell"
)

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
