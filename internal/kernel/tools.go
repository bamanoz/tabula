package kernel

import (
	"os/exec"
	"sync"
)

// SpawnedProcess tracks a background process started via SPAWN.
type SpawnedProcess struct {
	PID     int
	Cmd     *exec.Cmd
	Command string
	Alive   bool
	Session string
	done    chan struct{} // closed when process exits
	mu      sync.Mutex
	Handle  ProcessHandle // abstract process handle for non-local launchers
}

// Kill forcefully terminates the process (and its process group when
// available). The watcher goroutine will set Alive=false and close the done
// channel asynchronously.
func (p *SpawnedProcess) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Handle != nil {
		_ = p.Handle.Kill()
	} else if p.Cmd != nil && p.Cmd.Process != nil && p.Alive {
		_ = signalProcess(p.Cmd.Process, killSignal)
	}
}

func (h *Hub) handleToolUse(sender *Client, msg *Message) {
	h.tools.HandleToolUse(sender, msg)
}
