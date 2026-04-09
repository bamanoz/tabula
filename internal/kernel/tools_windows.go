//go:build windows

package kernel

import (
	"os"
	"os/exec"
)

// shellCommand creates a platform-appropriate shell command.
func shellCommand(command string) *exec.Cmd {
	return exec.Command("cmd", "/c", command)
}

// signalProcess on Windows cannot send SIGINT to child processes.
// Falls back to killing the process.
func (p *SpawnedProcess) Signal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Cmd.Process != nil {
		p.Cmd.Process.Signal(os.Interrupt)
	}
}

// afterSpawn starts a goroutine that waits for the process to exit.
// On Windows we can't use Wait4/WNOHANG, so each process gets its own watcher.
func (h *Hub) afterSpawn(pid int, proc *SpawnedProcess) {
	go func() {
		err := proc.Cmd.Wait()

		h.mu.Lock()
		defer h.mu.Unlock()

		proc.Alive = false

		if err != nil {
			exitCode := 1
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			}
			h.broadcastProcessError(proc.Session, pid, proc.Command, exitCode)
		} else {
			h.log("process %d exited OK: %s", pid, proc.Command)
		}
	}()
}

// StartReaper is a no-op on Windows — afterSpawn handles process lifecycle.
func (h *Hub) StartReaper() {}
