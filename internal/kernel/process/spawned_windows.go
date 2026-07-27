//go:build windows

package process

import "os"

// Signal sends os.Interrupt to the process.
func (p *Spawned) Signal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Cmd != nil && p.Cmd.Process != nil {
		_ = p.Cmd.Process.Signal(os.Interrupt)
	}
}
