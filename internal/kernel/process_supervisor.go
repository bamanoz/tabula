package kernel

import (
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

type ProcessSupervisor struct {
	mu           sync.RWMutex
	spawned      map[int]*SpawnedProcess
	logger       *slog.Logger
	timeout      time.Duration
	shuttingDown bool // set before Shutdown() to suppress crash logs
}

func NewProcessSupervisor(logger *slog.Logger, timeout time.Duration) *ProcessSupervisor {
	if logger == nil {
		logger = slog.Default()
	}
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	return &ProcessSupervisor{
		spawned: make(map[int]*SpawnedProcess),
		logger:  logger,
		timeout: timeout,
	}
}

func (ps *ProcessSupervisor) Register(cmd *exec.Cmd, command, session string) *SpawnedProcess {
	return ps.RegisterWithPID(cmd.Process.Pid, cmd, command, session)
}

// RegisterWithPID registers a process with an explicit PID.
func (ps *ProcessSupervisor) RegisterWithPID(pid int, cmd *exec.Cmd, command, session string) *SpawnedProcess {
	proc := &SpawnedProcess{
		PID:     pid,
		Cmd:     cmd,
		Command: command,
		Alive:   true,
		Session: session,
		done:    make(chan struct{}),
	}
	ps.mu.Lock()
	ps.spawned[pid] = proc
	ps.mu.Unlock()
	return proc
}

func (ps *ProcessSupervisor) ByPID(pid int) (*SpawnedProcess, bool) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	proc, ok := ps.spawned[pid]
	return proc, ok
}

func (ps *ProcessSupervisor) Update(pid int, fn func(*SpawnedProcess)) bool {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	proc, ok := ps.spawned[pid]
	if !ok {
		return false
	}
	fn(proc)
	return true
}

func (ps *ProcessSupervisor) ForEach(fn func(pid int, proc *SpawnedProcess)) {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	for pid, proc := range ps.spawned {
		fn(pid, proc)
	}
}

// IsShuttingDown returns true if Shutdown() has been called.
func (ps *ProcessSupervisor) IsShuttingDown() bool {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.shuttingDown
}

func (ps *ProcessSupervisor) MarkExited(pid int) (*SpawnedProcess, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	proc, ok := ps.spawned[pid]
	if !ok {
		return nil, false
	}
	proc.Alive = false
	close(proc.done)
	return proc, true
}

func (ps *ProcessSupervisor) Shutdown() {
	ps.shuttingDown = true
	var alive []*SpawnedProcess
	ps.ForEach(func(pid int, proc *SpawnedProcess) {
		if proc.Alive {
			ps.logger.Info("sending interrupt on shutdown", "pid", pid)
			proc.Signal()
			alive = append(alive, proc)
		}
	})

	if len(alive) == 0 {
		return
	}

	timer := time.NewTimer(ps.timeout)
	defer timer.Stop()

	done := make(chan struct{})
	go func() {
		for _, proc := range alive {
			<-proc.done
		}
		close(done)
	}()

	select {
	case <-done:
		ps.logger.Info("all processes exited gracefully")
		return
	case <-timer.C:
		ps.logger.Warn("shutdown timeout, force-killing remaining processes")
	}

	var stillAlive []*SpawnedProcess
	ps.ForEach(func(pid int, proc *SpawnedProcess) {
		if proc.Alive {
			ps.logger.Warn("force-killing process on shutdown", "pid", pid)
			proc.Kill()
			stillAlive = append(stillAlive, proc)
		}
	})

	// Wait for the SIGKILLed processes to actually disappear, but cap the
	// wait so a stuck child can't block kernel shutdown forever. Anything
	// still alive after this is a leak that the OS will reap when we exit.
	if len(stillAlive) == 0 {
		return
	}
	finalTimer := time.NewTimer(ps.timeout)
	defer finalTimer.Stop()
	finalDone := make(chan struct{})
	go func() {
		for _, proc := range stillAlive {
			<-proc.done
		}
		close(finalDone)
	}()
	select {
	case <-finalDone:
	case <-finalTimer.C:
		ps.logger.Error("processes still alive after SIGKILL grace; abandoning",
			"count", len(stillAlive))
	}
}
