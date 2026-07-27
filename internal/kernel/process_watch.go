package kernel

import (
	"os/exec"

	"github.com/bamanoz/tabula/internal/kernel/process"
)

// afterSpawn starts a goroutine that waits for the process to exit.
func (h *Hub) afterSpawn(pid int, proc *process.Spawned) {
	go func() {
		err := proc.Cmd.Wait()

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
