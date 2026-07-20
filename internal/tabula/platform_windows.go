//go:build windows

package tabula

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"

	"github.com/bamanoz/tabula/internal/shell"
)

func platformCreateReference(src, dst string, directory bool) error {
	rel, err := filepath.Rel(filepath.Dir(dst), src)
	if err != nil {
		return err
	}
	if err := os.Symlink(rel, dst); err == nil {
		return nil
	}
	if !directory {
		if err := os.Link(src, dst); err == nil {
			return nil
		}
		data, err := os.ReadFile(src)
		if err != nil {
			return err
		}
		info, err := os.Stat(src)
		if err != nil {
			return err
		}
		return os.WriteFile(dst, data, info.Mode().Perm())
	}
	absSrc, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	output, err := exec.Command("cmd.exe", "/d", "/c", "mklink", "/J", dst, absSrc).CombinedOutput()
	if err != nil {
		return fmt.Errorf("create junction %s -> %s: %w: %s", dst, absSrc, err, strings.TrimSpace(string(output)))
	}
	return nil
}

const (
	processQueryLimitedInformation = 0x1000
	stillActive                    = 259
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	openProcess        = kernel32.NewProc("OpenProcess")
	getExitCodeProcess = kernel32.NewProc("GetExitCodeProcess")
	closeHandle        = kernel32.NewProc("CloseHandle")
)

// mainShellCommand creates a platform-appropriate shell command.
func mainShellCommand(command string) *exec.Cmd {
	return shell.Command(command)
}

func shutdownSignalChan() <-chan os.Signal {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	return sigCh
}

// waitForShutdownSignal blocks until Ctrl+C (os.Interrupt) is received.
func waitForShutdownSignal() {
	<-shutdownSignalChan()
}

func platformProcessRunning(pid int) bool {
	handle, _, _ := openProcess.Call(processQueryLimitedInformation, 0, uintptr(uint32(pid)))
	if handle == 0 {
		return false
	}
	defer closeHandle.Call(handle) //nolint:errcheck // Best-effort cleanup of an OS handle.

	var exitCode uint32
	ok, _, _ := getExitCodeProcess.Call(handle, uintptr(unsafe.Pointer(&exitCode)))
	return ok != 0 && exitCode == stillActive
}
