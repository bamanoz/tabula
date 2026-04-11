//go:build !windows

package kernel

import (
	"os/exec"
	"syscall"
)

// signalProcess sends an interrupt signal to a process.
func (p *SpawnedProcess) Signal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Cmd.Process != nil {
		_ = p.Cmd.Process.Signal(syscall.SIGINT)
	}
}

// afterSpawn starts a goroutine that waits for the process to exit.
func (h *Hub) afterSpawn(pid int, proc *SpawnedProcess) {
	go func() {
		err := proc.Cmd.Wait()

		h.mu.Lock()
		proc.Alive = false
		close(proc.done)

		if err != nil {
			exitCode := 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			if exitCode < 0 {
				// Killed by signal (e.g. SIGINT, SIGKILL during shutdown) — not a crash
				h.Logger.Info("process killed by signal", "pid", pid, "signal", -exitCode, "command", proc.Command)
			} else {
				h.broadcastProcessError(proc.Session, pid, proc.Command, exitCode)
			}
		} else {
			h.Logger.Info("process exited", "pid", pid, "command", proc.Command)
		}
		h.mu.Unlock()
	}()
}

// StartReaper is a no-op — afterSpawn handles process lifecycle via per-process goroutines.
func (h *Hub) StartReaper() {}
