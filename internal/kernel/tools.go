package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
)

const maxExecOutput = 16 * 1024 // 16KB

// SpawnedProcess tracks a background process started via SPAWN.
type SpawnedProcess struct {
	Cmd     *exec.Cmd
	Command string
	Alive   bool
	Session string
	mu      sync.Mutex
}

// Kill forcefully terminates the process.
func (p *SpawnedProcess) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Cmd.Process != nil {
		_ = p.Cmd.Process.Kill()
		_ = p.Cmd.Wait()
		p.Alive = false
	}
}

// handleToolUse dispatches a tool_use message.
// Caller must hold h.mu.
func (h *Hub) handleToolUse(sender *Client, msg *Message) {
	toolName := msg.Name
	toolID := msg.ID
	session := sender.session

	h.log("handleToolUse: %s from session %s, id=%s", toolName, session, toolID)

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
		h.sendToolResult(session, toolID, fmt.Sprintf("ERROR: unknown tool %s", toolName))
	}
}

// sendToolResult sends a tool_result to a session.
func (h *Hub) sendToolResult(session, toolID, output string) {
	msg := &Message{
		Type:   "tool_result",
		ID:     toolID,
		Output: output,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.broadcastToSessionRaw(session, "tool_result", data)
}

// execAsync runs a command in a goroutine and sends the result to the session.
func (h *Hub) execAsync(session, toolID, command string) {
	cmd := shellCommand(command + " 2>&1")
	out, err := cmd.Output()

	// Truncate output
	if len(out) > maxExecOutput {
		out = out[:maxExecOutput]
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

	h.log("exec completed: %s (session %s, %d bytes)", command, session, len(result))

	h.mu.Lock()
	defer h.mu.Unlock()
	h.sendToolResult(session, toolID, result)
}

// spawnProcess starts a background process.
// Caller must hold h.mu.
func (h *Hub) spawnProcess(command, session string, childDepth int) (int, error) {
	cmd := shellCommand(command)
	// Spawned processes communicate via WebSocket, not stdout/stderr.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return 0, fmt.Errorf("open %s: %w", os.DevNull, err)
	}
	cmd.Stdout = devNull
	if h.Verbose && h.LogFile != nil {
		cmd.Stderr = h.LogFile
	} else {
		cmd.Stderr = devNull
	}

	// Generate one-time spawn token for child to authenticate and receive depth
	token := h.generateSpawnToken(childDepth)

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
	}
	h.spawned[pid] = proc
	h.log("spawned PID %d: %s (session %s)", pid, command, session)

	// Platform-specific: start process watcher (goroutine on Windows, reaper on Unix)
	h.afterSpawn(pid, proc)

	return pid, nil
}

// broadcastProcessError sends an error message about a crashed process.
func (h *Hub) broadcastProcessError(session string, pid int, command string, exitCode int) {
	h.log("process %d crashed (exit %d): %s", pid, exitCode, command)
	errMsg := &Message{
		Type: "error",
		Text: fmt.Sprintf("process %d crashed (exit %d)", pid, exitCode),
	}
	data, err := json.Marshal(errMsg)
	if err != nil {
		return
	}
	if session != "" {
		h.broadcastToSessionRaw(session, "error", data)
	} else {
		for c := range h.clients {
			if c.connected && c.canReceive("error") {
				c.SendRaw(data)
			}
		}
	}
}
