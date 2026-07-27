package process

import (
	"os/exec"
	"sync"
)

// Spawned tracks a background process started via SPAWN.
type Spawned struct {
	PID     int
	Cmd     *exec.Cmd
	Command string
	Alive   bool
	Session string
	done    chan struct{}
	mu      sync.Mutex
}

// Kill forcefully terminates the process and its process group when available.
func (p *Spawned) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Cmd != nil && p.Cmd.Process != nil && p.Alive {
		_ = signalProcess(p.Cmd.Process, killSignal)
	}
}
