package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
)

const maxExecOutput = 16 * 1024 // 16KB

const (
	maxSpawnDepth         = 3 // 0=main, 1=subagent, 2=sub-subagent, 3=max
	maxChildrenPerSession = 10 // max alive spawned processes per session
)

// SpawnedProcess tracks a background process started via SPAWN.
type SpawnedProcess struct {
	Cmd     *exec.Cmd
	Command string
	Alive   bool
	Session string
	mu      sync.Mutex
}

func (p *SpawnedProcess) Signal() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Cmd.Process != nil {
		p.Cmd.Process.Signal(syscall.SIGINT)
	}
}

func (p *SpawnedProcess) Kill() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Cmd.Process != nil {
		p.Cmd.Process.Kill()
		p.Cmd.Wait()
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
		if sender.depth >= maxSpawnDepth {
			h.sendToolResult(session, toolID, fmt.Sprintf("ERROR: max spawn depth reached (%d)", maxSpawnDepth))
			return
		}
		// Enforce max children per session
		alive := 0
		for _, p := range h.spawned {
			if p.Alive && p.Session == session {
				alive++
			}
		}
		if alive >= maxChildrenPerSession {
			h.sendToolResult(session, toolID, fmt.Sprintf("ERROR: too many active subagents (%d)", maxChildrenPerSession))
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
// Caller must hold h.mu (or be called from a goroutine that acquires it).
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
	cmd := exec.Command("sh", "-c", command+" 2>&1")
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
	cmd := exec.Command("sh", "-c", command)
	// Spawned processes communicate via WebSocket, not stdout/stderr.
	// Discard their output to prevent leaking into the kernel's terminal.
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return 0, fmt.Errorf("open /dev/null: %w", err)
	}
	cmd.Stdout = devNull
	cmd.Stderr = devNull

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
	return pid, nil
}

// StartReaper starts a goroutine that reaps zombie processes.
func (h *Hub) StartReaper() {
	go func() {
		for {
			time.Sleep(500 * time.Millisecond)
			h.reapZombies()
		}
	}()
}

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
			h.log("process %d crashed (exit %d): %s", pid, exitCode, proc.Command)
			errMsg := &Message{
				Type: "error",
				Text: fmt.Sprintf("process %d crashed (exit %d)", pid, exitCode),
			}
			data, err := json.Marshal(errMsg)
			if err != nil {
				continue
			}
			if proc.Session != "" {
				h.broadcastToSessionRaw(proc.Session, "error", data)
			} else {
				// Broadcast to all clients that receive errors
				for c := range h.clients {
					if c.connected && c.canReceive("error") {
						c.SendRaw(data)
					}
				}
			}
		}
	}
}
