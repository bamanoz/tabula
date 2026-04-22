package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/bamanoz/tabula/internal/shell"
)

// ProcessLauncher abstracts OS process creation.
// This allows swapping local processes for remote workers in the future.
type ProcessLauncher interface {
	// Start launches a command and returns the process handle.
	Start(command string, env []string) (ProcessHandle, error)
	// Run executes a command synchronously and returns output.
	Run(command string) ([]byte, error)
	// CombinedOutput executes a command and returns combined stdout+stderr.
	CombinedOutput(command string, stdin string, extraEnv []string) ([]byte, error)
}

// ProcessHandle represents a running process.
type ProcessHandle interface {
	PID() int
	Signal() error
	Kill() error
	Wait() error
}

// LocalProcessLauncher is the default implementation using os/exec.
type LocalProcessLauncher struct{}

func (l *LocalProcessLauncher) Start(command string, env []string) (ProcessHandle, error) {
	cmd := shell.SpawnCommand(command)
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	cmd.Stdout = devNull
	cmd.Stderr = devNull
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		devNull.Close()
		return nil, err
	}
	devNull.Close()

	return &localProcessHandle{cmd: cmd, devNull: devNull}, nil
}

func (l *LocalProcessLauncher) Run(command string) ([]byte, error) {
	cmd := shell.Command(command + " 2>&1")
	return cmd.Output()
}

func (l *LocalProcessLauncher) CombinedOutput(command string, stdin string, extraEnv []string) ([]byte, error) {
	cmd := shell.Command(command)
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = append(os.Environ(), extraEnv...)
	return cmd.CombinedOutput()
}

type localProcessHandle struct {
	cmd     *exec.Cmd
	devNull *os.File
}

func (h *localProcessHandle) PID() int { return h.cmd.Process.Pid }

// Signal sends SIGINT to the whole process group so children spawned by the
// wrapping shell (e.g. “sh -c "sleep 60"“) are interrupted too. On Windows
// (where we can't create a process group) we fall back to signaling the root
// process.
func (h *localProcessHandle) Signal() error { return signalProcess(h.cmd.Process, interruptSignal) }

// Kill sends SIGKILL to the whole process group (falling back to the root
// process). Must be used only after Signal() followed by a grace period.
func (h *localProcessHandle) Kill() error { return signalProcess(h.cmd.Process, killSignal) }
func (h *localProcessHandle) Wait() error {
	err := h.cmd.Wait()
	h.devNull.Close()
	return err
}

// ProcessManager handles SPAWN/KILL/LIST tool execution.
// It bridges the message layer (ToolService) with the process layer (ProcessSupervisor + ProcessLauncher).
type ProcessManager struct {
	hub      *Hub
	launcher ProcessLauncher
}

func NewProcessManager(hub *Hub, launcher ProcessLauncher) *ProcessManager {
	if launcher == nil {
		launcher = &LocalProcessLauncher{}
	}
	return &ProcessManager{hub: hub, launcher: launcher}
}

// SpawnResult holds the outcome of a spawn operation.
type SpawnResult struct {
	PID   int
	Error string
}

// Spawn starts a new process and registers it with the supervisor.
// Validation (depth, MaxChildren, hooks) is done by PolicyEngine.CanSpawn before calling this.
func (pm *ProcessManager) Spawn(command, session string, childDepth int) SpawnResult {
	token, err := pm.hub.generateSpawnToken(childDepth)
	if err != nil {
		return SpawnResult{Error: err.Error()}
	}

	env := append(os.Environ(), "TABULA_SPAWN_TOKEN="+token)
	handle, err := pm.launcher.Start(command, env)
	if err != nil {
		return SpawnResult{Error: err.Error()}
	}

	pid := handle.PID()

	// Create a minimal exec.Cmd for ProcessSupervisor's bookkeeping.
	// The actual process operations go through the Handle.
	cmd := shell.Command(command)
	cmd.Env = env

	proc := pm.hub.processes.RegisterWithPID(pid, cmd, command, session)
	proc.Handle = handle

	pm.hub.Logger.Info("spawned process", "pid", pid, "command", command, "session", session)
	pm.hub.afterSpawn(pid, proc)

	return SpawnResult{PID: pid}
}

// Kill terminates a process by PID.
func (pm *ProcessManager) Kill(pid int, session string) error {
	proc, ok := pm.hub.processByPID(pid)
	if !ok {
		return fmt.Errorf("unknown PID")
	}
	if proc.Session != session {
		return fmt.Errorf("not your process")
	}

	if proc.Handle != nil {
		proc.Handle.Kill()
	} else {
		proc.Kill()
	}
	pm.hub.updateProcess(pid, func(p *SpawnedProcess) {
		p.Alive = false
	})
	return nil
}

// List returns a summary of processes in a session.
func (pm *ProcessManager) List(session string) string {
	var parts []string
	pm.hub.forEachProcess(func(pid int, proc *SpawnedProcess) {
		if proc.Session != session {
			return
		}
		parts = append(parts, fmt.Sprintf("PID %d %s alive=%v", pid, proc.Command, proc.Alive))
	})

	if len(parts) == 0 {
		return "(empty)"
	}
	return strings.Join(parts, ", ")
}

// execTool runs a tool asynchronously: execute → format → send result → emit hook.
func (pm *ProcessManager) execTool(session, toolID, toolName, logMsg string, exec func() ([]byte, error)) {
	go func() {
		out, err := exec()
		result := formatCommandResult(out, err)

		pm.hub.Logger.Debug(logMsg, "tool", toolName, "session", session, "bytes", len(result))
		pm.hub.sendToolResult(session, toolID, result)
		pm.hub.emitAfterToolCall(session, toolID, map[string]string{
			"tool": toolName, "id": toolID, "output": result,
		})
	}()
}

// RunCommand executes a shell command asynchronously and sends the result.
func (pm *ProcessManager) RunCommand(session, toolID, command string) {
	pm.execTool(session, toolID, string(ToolShellExec), "exec completed", func() ([]byte, error) {
		return pm.launcher.Run(command)
	})
}

// RunSkillTool executes a skill tool asynchronously.
func (pm *ProcessManager) RunSkillTool(session, toolID, toolName, execCmd string, input json.RawMessage) {
	pm.execTool(session, toolID, toolName, "skill tool completed", func() ([]byte, error) {
		return pm.launcher.CombinedOutput(execCmd, string(input), []string{"TABULA_SESSION=" + session})
	})
}
