package kernel

import (
	"encoding/json"
	"log/slog"
	"os/exec"
	"sync"
	"time"
)

// spawnTokenEntry tracks a one-time spawn token with its creation time.
type spawnTokenEntry struct {
	depth     int
	createdAt time.Time
}

const spawnTokenTTL = 60 * time.Second

// Hub manages all connected clients, sessions, and spawned processes.
type Hub struct {
	mu              sync.Mutex
	clients         map[*Client]bool
	spawned         map[int]*SpawnedProcess
	spawnTokens     map[string]spawnTokenEntry
	hookIndex       map[string][]hookEntry
	pendingHooks    map[string]chan *HookResult
	toolExec        map[string]string // tool name → exec command for skill tools
	nextClientID    int
	systemPrompt    string
	toolsJSON       json.RawMessage
	Logger          *slog.Logger
	MaxSpawnDepth   int
	MaxChildren     int
	MaxClients      int           // max concurrent clients (default 100)
	ShutdownTimeout time.Duration // grace period before SIGKILL (default 3s)
}

// NewHub creates a new Hub.
func NewHub(systemPrompt string, toolsJSON json.RawMessage, skillExec map[string]string, maxSpawnDepth int, maxChildren int, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	if skillExec == nil {
		skillExec = make(map[string]string)
	}
	return &Hub{
		clients:         make(map[*Client]bool),
		spawned:         make(map[int]*SpawnedProcess),
		spawnTokens:     make(map[string]spawnTokenEntry),
		hookIndex:       make(map[string][]hookEntry),
		pendingHooks:    make(map[string]chan *HookResult),
		toolExec:        skillExec,
		nextClientID:    1,
		systemPrompt:    systemPrompt,
		toolsJSON:       toolsJSON,
		Logger:          logger,
		MaxSpawnDepth:   maxSpawnDepth,
		MaxChildren:     maxChildren,
		MaxClients:      100,
		ShutdownTimeout: 3 * time.Second,
	}
}

// Register adds a client to the hub.
// Returns false if the hub is at capacity (MaxClients).
func (h *Hub) Register(c *Client) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.MaxClients > 0 && len(h.clients) >= h.MaxClients {
		h.Logger.Error("rejecting client: max clients reached", "max", h.MaxClients)
		return false
	}
	h.clients[c] = true
	return true
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	delete(h.clients, c)
	if len(c.hooks) > 0 {
		h.rebuildHookIndex()
	}
	h.Logger.Info("client disconnected", "name", c.name, "id", c.id)
}

// HandleMessage processes an incoming message from a client.
// Called from the client's readPump goroutine.
func (h *Hub) HandleMessage(sender *Client, msg *Message) {
	h.mu.Lock()
	defer h.mu.Unlock()

	switch msg.Type {
	case "connect":
		h.handleConnect(sender, msg)
	case "join":
		h.handleJoin(sender, msg)
	default:
		if !sender.connected {
			return
		}
		// hook_result can come from clients without a session (global hook subscribers).
		if msg.Type == "hook_result" {
			h.handleHookResult(msg)
			return
		}
		if !sender.canSend(msg.Type) {
			h.Logger.Warn("client not allowed to send", "name", sender.name, "type", msg.Type)
			return
		}
		if sender.session == "" {
			h.Logger.Warn("client not in a session", "name", sender.name)
			return
		}

		switch msg.Type {
		case "tool_use":
			h.handleToolUse(sender, msg)
		case "cancel":
			h.handleCancel(sender.session)
		case "message":
			// before_message hook (modifying): can alter or block.
			payload, _ := json.Marshal(map[string]string{"text": msg.Text, "sender": sender.name})
			result, ok := h.dispatchHook("before_message", payload, sender.session)
			if !ok {
				sender.SendMsg(&Message{Type: "error", Text: "message blocked by hook"})
				return
			}
			// Apply modifications from hook.
			var modified struct{ Text string }
			if json.Unmarshal(result, &modified) == nil && modified.Text != "" {
				msg.Text = modified.Text
			}
			target := sender.session
			if msg.Session != "" {
				target = msg.Session
			}
			h.broadcastToSession(target, msg.Type, msg, sender)
		default:
			target := sender.session
			if msg.Session != "" {
				target = msg.Session
			}
			h.broadcastToSession(target, msg.Type, msg, sender)
			// Fire after_message hook on turn completion.
			if msg.Type == "done" {
				payload, _ := json.Marshal(map[string]string{
					"session": target,
					"sender":  sender.name,
					"type":    "done",
				})
				h.dispatchHook("after_message", payload, target)
			}
		}
	}
}

// RegisterSpawn registers an externally started process in the spawned map.
func (h *Hub) RegisterSpawn(cmd *exec.Cmd, command, session string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	pid := cmd.Process.Pid
	proc := &SpawnedProcess{
		Cmd:     cmd,
		Command: command,
		Alive:   true,
		Session: session,
		done:    make(chan struct{}),
	}
	h.spawned[pid] = proc
	h.Logger.Info("registered spawned process", "pid", pid, "command", command)
	h.afterSpawn(pid, proc)
}

// Shutdown gracefully stops all spawned processes.
// Sends interrupt signal first, waits up to ShutdownTimeout, then force-kills remaining.
func (h *Hub) Shutdown() {
	h.mu.Lock()
	var alive []*SpawnedProcess
	for pid, proc := range h.spawned {
		if proc.Alive {
			h.Logger.Info("sending interrupt on shutdown", "pid", pid)
			proc.Signal()
			alive = append(alive, proc)
		}
	}
	h.mu.Unlock()

	if len(alive) == 0 {
		return
	}

	timeout := h.ShutdownTimeout
	if timeout <= 0 {
		timeout = 3 * time.Second
	}
	timer := time.NewTimer(timeout)
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
		h.Logger.Info("all processes exited gracefully")
		return
	case <-timer.C:
		h.Logger.Warn("shutdown timeout, force-killing remaining processes")
	}

	h.mu.Lock()
	var stillAlive []*SpawnedProcess
	for pid, proc := range h.spawned {
		if proc.Alive {
			h.Logger.Warn("force-killing process on shutdown", "pid", pid)
			proc.Kill()
			stillAlive = append(stillAlive, proc)
		}
	}
	h.mu.Unlock()

	for _, proc := range stillAlive {
		<-proc.done
	}
}

// SnapshotSessions returns a JSON snapshot of all sessions grouped by session name.
func (h *Hub) SnapshotSessions() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()

	type processInfo struct {
		PID     int    `json:"pid"`
		Command string `json:"command"`
		Alive   bool   `json:"alive"`
	}
	type sessionInfo struct {
		Clients   []string      `json:"clients"`
		Processes []processInfo `json:"processes"`
	}

	sessions := make(map[string]*sessionInfo)

	ensure := func(name string) *sessionInfo {
		if s, ok := sessions[name]; ok {
			return s
		}
		s := &sessionInfo{
			Clients:   []string{},
			Processes: []processInfo{},
		}
		sessions[name] = s
		return s
	}

	for c := range h.clients {
		if c.connected && c.session != "" {
			s := ensure(c.session)
			s.Clients = append(s.Clients, c.name)
		}
	}
	for pid, proc := range h.spawned {
		if proc.Session != "" {
			s := ensure(proc.Session)
			s.Processes = append(s.Processes, processInfo{
				PID:     pid,
				Command: proc.Command,
				Alive:   proc.Alive,
			})
		}
	}

	data, _ := json.Marshal(sessions)
	return data
}
