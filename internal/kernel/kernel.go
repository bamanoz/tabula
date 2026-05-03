package kernel

import (
	"encoding/json"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

// Hub manages all connected clients, sessions, and spawned processes.
type Hub struct {
	clients   *ClientRegistry
	sessions  *SessionRegistry
	processes *ProcessSupervisor
	hooks     *HookEngine
	policy    *PolicyEngine
	tools     *ToolService
	plugins   *plugin.Registry
	runtimes  *RuntimeRegistry
	// pluginRuns tracks active supervisor lifecycles by plugin id. It is kept
	// outside plugin.Registry so replacement/cancellation state does not leak
	// into the public plugin handle snapshot/order semantics.
	pluginRuns   map[string]*pluginLifecycle
	pluginRunsMu sync.Mutex
	// pluginStates tracks durable lifecycle diagnostics for SnapshotPlugins.
	// Unlike pluginRuns, failed terminal states are retained after the handle is
	// removed from the live registry so operators can see why a plugin vanished.
	pluginStates   map[string]*pluginLifecycleState
	pluginStatesMu sync.RWMutex
	// pluginRuntime owns live plugin process startup/stdio mechanics. It is
	// injected as a field (rather than through NewHub's signature) to keep the
	// builder-style Plugin API additive per creative-plugin-runtime.md §3.
	pluginRuntime plugin.Runtime
	// pluginSupervisorPolicy defaults to the frozen protocol values. Tests may
	// override it to avoid waiting for real backoff intervals.
	pluginSupervisorPolicy plugin.SupervisorPolicy
	// toolExec is the unified tool dispatch table per creative
	// `creative-plugin-runtime.md` §4 (D1.7). Keyed by tool name, value
	// carries Source (skill | plugin) plus the source-specific routing
	// payload. Skill tools are populated at NewHub time; plugin tools are
	// inserted by Hub.RegisterPlugin (D2.14, future) on register-reply
	// and atomically replaced on update_tools.
	toolExec        map[string]toolDispatch
	toolExecMu      sync.RWMutex
	toolsJSON       json.RawMessage
	initMeta        json.RawMessage
	Logger          *slog.Logger
	MaxClients      int           // max concurrent clients (default 100)
	ShutdownTimeout time.Duration // grace period before SIGKILL (default 3s)
	// ProjectRoot is the absolute workspace/project root exposed to skills via
	// the `meta.project_root` field on `init`. Skills use it to scope file
	// reads/writes, shell commands, and approval policies. Empty if unset.
	ProjectRoot string
}

func (h *Hub) SetInitMeta(meta json.RawMessage) {
	h.initMeta = meta
}

// NewHub creates a new Hub.
//
// skillExec is the legacy skill-tool dispatch map (`name → exec command`),
// constructed in main.go from `bootConfig.Skills` (or the deprecated
// `bootConfig.Tools` legacy alias). It is converted internally to the
// unified Hub.toolExec dispatch table per creative §4.
func NewHub(toolsJSON json.RawMessage, skillExec map[string]string, _ int, _ int, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	dispatch := make(map[string]toolDispatch, len(skillExec))
	for name, cmd := range skillExec {
		if cmd == "" {
			continue
		}
		dispatch[name] = skillDispatch(cmd)
	}
	hub := &Hub{
		clients:         NewClientRegistry(),
		sessions:        NewSessionRegistry(),
		processes:       NewProcessSupervisor(logger, 3*time.Second),
		hooks:           NewHookEngine(logger),
		plugins:         plugin.NewRegistry(),
		runtimes:        NewRuntimeRegistry(),
		pluginRuns:      make(map[string]*pluginLifecycle),
		pluginStates:    make(map[string]*pluginLifecycleState),
		toolExec:        dispatch,
		toolsJSON:       toolsJSON,
		Logger:          logger,
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
	if sess.IsBusy() && c.canSend(string(MsgDone)) {
		sess.EndTurn()
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

// GetSession returns a session by ID, or nil if not found.
func (h *Hub) GetSession(id string) (*Session, bool) {
	return h.sessions.Get(id)
}
