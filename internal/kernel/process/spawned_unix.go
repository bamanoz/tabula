//go:build !windows

package process

// Signal sends SIGINT to the process group rooted at this process.
func (p *Spawned) Signal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Cmd != nil && p.Cmd.Process != nil {
		_ = signalProcess(p.Cmd.Process, interruptSignal)
	}
}
