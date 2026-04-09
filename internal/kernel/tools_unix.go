//go:build !windows

package kernel

import (
	"os/exec"
	"syscall"
	"time"
)

// shellCommand creates a platform-appropriate shell command.
func shellCommand(command string) *exec.Cmd {
	return exec.Command("sh", "-c", command)
}

// signalProcess sends an interrupt signal to a process.
func (p *SpawnedProcess) Signal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Cmd.Process != nil {
		_ = p.Cmd.Process.Signal(syscall.SIGINT)
	}
}

// afterSpawn is a no-op on Unix — reapZombies handles process reaping.
func (h *Hub) afterSpawn(pid int, proc *SpawnedProcess) {}

// reapZombies uses Wait4 to detect exited child processes.
func (h *Hub) reapZombies() {
	h.mu.Lock()
	defer h.mu.Unlock()

	for pid, proc := range h.spawned {
		if !proc.Alive {
			continue
		}

		var status syscall.WaitStatus
		wpid, err := syscall.Wait4(pid, &status, syscall.WNOHANG, nil)
		if err != nil || wpid == 0 {
			continue
		}

		proc.Alive = false

		exitCode := 0
		if status.Exited() {
			exitCode = status.ExitStatus()
		} else {
			exitCode = 1
		}

		if exitCode == 0 {
			h.log("process %d exited OK: %s", pid, proc.Command)
		} else {
			h.broadcastProcessError(proc.Session, pid, proc.Command, exitCode)
		}
	}
}

// StartReaper starts a goroutine that periodically reaps zombie processes.
func (h *Hub) StartReaper() {
	go func() {
		for {
			time.Sleep(500 * time.Millisecond)
			h.reapZombies()
		}
	}()
}
