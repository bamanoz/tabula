package kernel

import (
	"encoding/json"
	"log/slog"
	"os/exec"
	"time"
)

// Hub manages all connected clients, sessions, and spawned processes.
type Hub struct {
	clients         *ClientRegistry
	sessions        *SessionRegistry
	processes       *ProcessSupervisor
	tokens          *SpawnTokenStore
	hooks           *HookEngine
	policy          *PolicyEngine
	tools           *ToolService
	toolExec        map[string]string // tool name → exec command for skill tools
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
	hub := &Hub{
		clients:         NewClientRegistry(),
		sessions:        NewSessionRegistry(),
		processes:       NewProcessSupervisor(logger, 3*time.Second),
		tokens:          NewSpawnTokenStore(),
		hooks:           NewHookEngine(logger),
		toolExec:        skillExec,
		systemPrompt:    systemPrompt,
		toolsJSON:       toolsJSON,
		Logger:          logger,
		MaxSpawnDepth:   maxSpawnDepth,
		MaxChildren:     maxChildren,
		MaxClients:      100,
		ShutdownTimeout: 3 * time.Second,
	}
	hub.policy = NewPolicyEngine(hub)
	hub.tools = NewToolService(hub)
	return hub
}

// Register adds a client to the hub.
// Returns false if the hub is at capacity (MaxClients).
func (h *Hub) Register(c *Client) bool {
	if !h.addClient(c) {
		h.Logger.Error("rejecting client: max clients reached", "max", h.MaxClients)
		return false
	}
	return true
}

// Unregister removes a client from the hub.
func (h *Hub) Unregister(c *Client) {
	h.removeClient(c)
	if len(c.hooks) > 0 {
		h.rebuildHookIndex()
	}
	h.onClientDisconnect(c)
	h.Logger.Info("client disconnected", "name", c.name, "id", c.id)
}

// onClientDisconnect handles session lifecycle when a client leaves.
func (h *Hub) onClientDisconnect(c *Client) {
	if h.sessions == nil || c.session == "" {
		return
	}
	sess, ok := h.sessions.Get(c.session)
	if !ok {
		return
	}
	sess.RemoveClient(c.name)
	if sess.ClientCount() == 0 {
		h.emitSessionEnd(c.session)
		h.sessions.Remove(c.session)
	}
}

// HandleMessage processes an incoming message from a client.
// Called from the client's readPump goroutine.
func (h *Hub) HandleMessage(sender *Client, msg *Message) {
	switch MsgType(msg.Type) {
	case MsgConnect:
		h.handleConnect(sender, msg)
	case MsgJoin:
		h.handleJoin(sender, msg)
	default:
		h.handleSessionMessage(sender, msg)
	}
}

// RegisterSpawn registers an externally started process in the spawned map.
func (h *Hub) RegisterSpawn(cmd *exec.Cmd, command, session string) {
	pid := cmd.Process.Pid
	proc := h.registerSpawnedCommand(cmd, command, session)
	h.Logger.Info("registered spawned process", "pid", pid, "command", command)
	h.afterSpawn(pid, proc)
}

// Shutdown gracefully stops all spawned processes.
// Sends interrupt signal first, waits up to ShutdownTimeout, then force-kills remaining.
func (h *Hub) Shutdown() {
	h.processes.timeout = h.ShutdownTimeout
	h.processes.Shutdown()
}

// Stop shuts down all spawned processes without grace period.
// Used by one-shot mode to clean up after a single exchange.
func (h *Hub) Stop() {
	h.processes.Shutdown()
}
