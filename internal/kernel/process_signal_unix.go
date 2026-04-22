//go:build !windows

package kernel

import (
	"os"
	"syscall"
)

var (
	interruptSignal os.Signal = syscall.SIGINT
	killSignal      os.Signal = syscall.SIGKILL
)

// signalProcess sends “sig“ to the whole process group rooted at “proc“.
// Falls back to signaling just the root process if the group lookup fails
// (e.g. the process exited between PID lookup and the kill call).
func signalProcess(proc *os.Process, sig os.Signal) error {
	if proc == nil {
		return nil
	}
	pgid, err := syscall.Getpgid(proc.Pid)
	if err == nil && pgid > 0 {
		// Negative PID == "send to process group".
		if sysSig, ok := sig.(syscall.Signal); ok {
			if err := syscall.Kill(-pgid, sysSig); err == nil {
				return nil
			}
		}
	}
	return proc.Signal(sig)
}
