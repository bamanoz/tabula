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

// ProcessManager handles command-backed skill tool execution.
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
		pm.hub.sendToolResultForTool(session, toolID, toolName, result)
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

// SkillExec encapsulates per-call execution of skill tools registered via
// the boot config (`SKILL.md` `tools[].exec`). It is the kernel-internal
// counterpart to skill subprocesses described in
// `docs/plans/SKILL_PLUGIN_ARCHITECTURE.md` §4.2.
//
// Until Phase 2 introduces the unified `toolDispatch` (creative §4), this
// type simply delegates to ProcessManager.execTool — preserving async
// semantics, hook integration and result formatting.
type SkillExec struct {
	pm *ProcessManager
}

// NewSkillExec constructs a SkillExec bound to the given ProcessManager.
func NewSkillExec(pm *ProcessManager) *SkillExec {
	return &SkillExec{pm: pm}
}

// Run executes a skill tool asynchronously and posts the result back through
// the kernel bus, emitting `after_tool_call` on completion.
func (s *SkillExec) Run(session, toolID, toolName, execCmd string, input json.RawMessage) {
	s.pm.execTool(session, toolID, toolName, "skill tool completed", func() ([]byte, error) {
		return s.pm.launcher.CombinedOutput(execCmd, string(input), []string{"TABULA_SESSION=" + session})
	})
}
