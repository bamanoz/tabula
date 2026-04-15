//go:build windows

package kernel

import (
	"os"
	"os/exec"
)

// Signal sends os.Interrupt to the process.
func (p *SpawnedProcess) Signal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Handle != nil {
		_ = p.Handle.Signal()
	} else if p.Cmd.Process != nil {
		_ = p.Cmd.Process.Signal(os.Interrupt)
	}
}

// afterSpawn starts a goroutine that waits for the process to exit.
func (h *Hub) afterSpawn(pid int, proc *SpawnedProcess) {
	go func() {
		var err error
		if proc.Handle != nil {
			err = proc.Handle.Wait()
		} else {
			err = proc.Cmd.Wait()
		}

		proc, ok := h.processes.MarkExited(pid)
		if !ok {
			return
		}

		if err != nil {
			exitCode := 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			if exitCode < 0 {
				h.Logger.Info("process killed by signal", "pid", pid, "signal", -exitCode, "command", proc.Command)
			} else if h.processes.IsShuttingDown() {
				h.Logger.Info("process exited on shutdown", "pid", pid, "exit_code", exitCode, "command", proc.Command)
			} else {
				h.broadcastProcessError(proc.Session, pid, proc.Command, exitCode)
			}
		} else {
			h.Logger.Info("process exited", "pid", pid, "command", proc.Command)
		}
	}()
}

// StartReaper is a no-op — afterSpawn handles process lifecycle via per-process goroutines.
func (h *Hub) StartReaper() {}
