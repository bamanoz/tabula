package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/bamanoz/tabula/internal/shell"
)

const maxExecOutput = 16 * 1024 // 16KB

// SpawnedProcess tracks a background process started via SPAWN.
type SpawnedProcess struct {
	Cmd     *exec.Cmd
	Command string
	Alive   bool
	Session string
	done    chan struct{} // closed when process exits
	mu      sync.Mutex
}

// Kill forcefully terminates the process. The watcher goroutine will
// set Alive=false and close the done channel asynchronously.
func (p *SpawnedProcess) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Cmd.Process != nil && p.Alive {
		_ = p.Cmd.Process.Kill()
	}
}

// handleToolUse dispatches a tool_use message.
// Caller must hold h.mu.
func (h *Hub) handleToolUse(sender *Client, msg *Message) {
	toolName := msg.Name
	toolID := msg.ID
	session := sender.session

	h.Logger.Debug("tool_use", "tool", toolName, "session", session, "id", toolID)

	// Universal before_tool_call hook — fires for ALL tools.
	hookPayload, _ := json.Marshal(map[string]any{
		"tool": toolName, "id": toolID, "input": msg.Input,
	})
	if _, ok := h.dispatchHook("before_tool_call", hookPayload, session); !ok {
		h.sendToolResult(session, toolID, "ERROR: blocked by hook")
		return
	}

	switch toolName {
	case "EXEC":
		var input struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(msg.Input, &input); err != nil || input.Command == "" {
			h.sendToolResult(session, toolID, "ERROR: missing or invalid command")
			return
		}
		// Run async — release lock, execute in goroutine
		go h.execAsync(session, toolID, input.Command)

	case "SPAWN":
		var input struct {
			Command string `json:"command"`
		}
		if err := json.Unmarshal(msg.Input, &input); err != nil || input.Command == "" {
			h.sendToolResult(session, toolID, "ERROR: missing or invalid command")
			return
		}
		// before_spawn hook
		hookPayload, _ := json.Marshal(map[string]string{
			"tool": "SPAWN", "id": toolID, "command": input.Command,
		})
		if _, ok := h.dispatchHook("before_spawn", hookPayload, session); !ok {
			h.sendToolResult(session, toolID, "ERROR: blocked by hook")
			return
		}
		// Enforce spawn depth limit
		if sender.depth >= h.MaxSpawnDepth {
			h.sendToolResult(session, toolID, fmt.Sprintf("ERROR: max spawn depth reached (%d)", h.MaxSpawnDepth))
			return
		}
		// Enforce max children per session
		alive := 0
		for _, p := range h.spawned {
			if p.Alive && p.Session == session {
				alive++
			}
		}
		if alive >= h.MaxChildren {
			h.sendToolResult(session, toolID, fmt.Sprintf("ERROR: too many active subagents (%d)", h.MaxChildren))
			return
		}
		pid, err := h.spawnProcess(input.Command, session, sender.depth+1)
		if err != nil {
			h.sendToolResult(session, toolID, fmt.Sprintf("ERROR: %v", err))
			return
		}
		h.sendToolResult(session, toolID, fmt.Sprintf("PID %d", pid))
		// after_spawn hook (void)
		spawnHookPayload, _ := json.Marshal(map[string]any{
			"tool": "SPAWN", "id": toolID, "command": input.Command, "pid": pid,
		})
		h.dispatchHook("after_spawn", spawnHookPayload, session)

	case "KILL":
		var input struct {
			PID int `json:"pid"`
		}
		if err := json.Unmarshal(msg.Input, &input); err != nil || input.PID == 0 {
			h.sendToolResult(session, toolID, "ERROR: missing or invalid pid")
			return
		}
		proc, ok := h.spawned[input.PID]
		if !ok {
			h.sendToolResult(session, toolID, "ERROR: unknown PID")
			return
		}
		if proc.Session != session {
			h.sendToolResult(session, toolID, "ERROR: not your process")
			return
		}
		proc.Kill()
		proc.Alive = false // immediate update under h.mu so SPAWN count is accurate
		h.sendToolResult(session, toolID, "OK")

	case "LIST":
		var parts []string
		for pid, proc := range h.spawned {
			if proc.Session != session {
				continue
			}
			parts = append(parts, fmt.Sprintf("PID %d %s alive=%v", pid, proc.Command, proc.Alive))
		}
		result := "(empty)"
		if len(parts) > 0 {
			result = strings.Join(parts, ", ")
		}
		h.sendToolResult(session, toolID, result)

	default:
		if execCmd, ok := h.toolExec[toolName]; ok {
			go h.execSkillTool(session, toolID, toolName, execCmd, msg.Input)
		} else {
			h.sendToolResult(session, toolID, fmt.Sprintf("ERROR: unknown tool %s", toolName))
		}
	}
}

// execAsync runs a command in a goroutine and sends the result to the session.
func (h *Hub) execAsync(session, toolID, command string) {
	cmd := shell.Command(command + " 2>&1")
	out, err := cmd.Output()

	// Truncate output
	if len(out) > maxExecOutput {
		out = append(out[:maxExecOutput], []byte("\n[truncated]")...)
	}

	result := strings.TrimSpace(string(out))
	if err != nil {
		if result == "" {
			if exitErr, ok := err.(*exec.ExitError); ok {
				result = fmt.Sprintf("ERROR: exit code %d", exitErr.ExitCode())
			} else {
				result = fmt.Sprintf("ERROR: %v", err)
			}
		}
	}
	if result == "" {
		result = "OK"
	}

	h.Logger.Debug("exec completed", "command", command, "session", session, "bytes", len(result))

	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendToolResult(session, toolID, result)
	// after_tool_call hook (void)
	hookPayload, _ := json.Marshal(map[string]string{
		"tool": "EXEC", "id": toolID, "command": command, "output": result,
	})
	h.dispatchHook("after_tool_call", hookPayload, session)
}

// spawnProcess starts a background process.
// Caller must hold h.mu.
func (h *Hub) spawnProcess(command, session string, childDepth int) (int, error) {
	cmd := shell.Command(command)
	// Spawned processes communicate via WebSocket, not stdout/stderr.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	cmd.Stdout = devNull
	cmd.Stderr = devNull

	// Generate one-time spawn token for child to authenticate and receive depth
	token, err := h.generateSpawnToken(childDepth)
	if err != nil {
		devNull.Close()
		return 0, err
	}

	env := os.Environ()
	env = append(env, "TABULA_SPAWN_TOKEN="+token)
	cmd.Env = env

	if err := cmd.Start(); err != nil {
		devNull.Close()
		return 0, err
	}
	devNull.Close() // child inherited the fd, parent can close

	pid := cmd.Process.Pid
	proc := &SpawnedProcess{
		Cmd:     cmd,
		Command: command,
		Alive:   true,
		Session: session,
		done:    make(chan struct{}),
	}
	h.spawned[pid] = proc
	h.Logger.Info("spawned process", "pid", pid, "command", command, "session", session)

	// Start per-process watcher goroutine
	h.afterSpawn(pid, proc)

	return pid, nil
}

// execSkillTool runs a skill tool's exec command with JSON input on stdin.
func (h *Hub) execSkillTool(session, toolID, toolName, execCmd string, input json.RawMessage) {
	cmd := shell.Command(execCmd)
	cmd.Stdin = strings.NewReader(string(input))
	cmd.Env = append(os.Environ(), "TABULA_SESSION="+session)
	out, err := cmd.CombinedOutput()

	if len(out) > maxExecOutput {
		out = append(out[:maxExecOutput], []byte("\n[truncated]")...)
	}

	result := strings.TrimSpace(string(out))
	if err != nil {
		if result == "" {
			if exitErr, ok := err.(*exec.ExitError); ok {
				result = fmt.Sprintf("ERROR: exit code %d", exitErr.ExitCode())
			} else {
				result = fmt.Sprintf("ERROR: %v", err)
			}
		}
	}
	if result == "" {
		result = "OK"
	}

	h.Logger.Debug("skill tool completed", "tool", toolName, "session", session, "bytes", len(result))

	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendToolResult(session, toolID, result)
	// after_tool_call hook (void)
	hookPayload, _ := json.Marshal(map[string]string{
		"tool": toolName, "id": toolID, "output": result,
	})
	h.dispatchHook("after_tool_call", hookPayload, session)
}
