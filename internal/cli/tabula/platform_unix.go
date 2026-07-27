//go:build !windows

package tabula

import (
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
)

func platformCreateReference(src, dst string, _ bool) error {
	rel, err := filepath.Rel(filepath.Dir(dst), src)
	if err != nil {
		return err
	}
	return os.Symlink(rel, dst)
}

func shutdownSignalChan() <-chan os.Signal {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	return sigCh
}

// waitForShutdownSignal blocks until SIGINT or SIGTERM is received.
func waitForShutdownSignal() {
	<-shutdownSignalChan()
}

func platformProcessRunning(pid int) bool {
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}
