package kernel

import (
	"encoding/json"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/tenant"
)

// Hub manages all connected clients, sessions, and spawned processes.
type Hub struct {
	clients      *ClientRegistry
	sessions     *SessionRegistry
	processes    *ProcessSupervisor
	hooks        *HookEngine
	policy       *PolicyEngine
	tools        *ToolService
	runtimes     *RuntimeRegistry
	tenants      tenant.Store
	sessionStore SessionStore
	// toolExec is the unified tool dispatch table per creative
	// `creative-plugin-runtime.md` §4 (D1.7). Keyed by tool name, value
	// carries the runtime routing payload. Entries are synchronized from
	// attached Runtime API targets.
	toolExec        map[string]toolDispatch
	toolExecMu      sync.RWMutex
	runtimeBusy     map[string]int
	runtimeBusyMu   sync.RWMutex
	toolsJSON       json.RawMessage
	initMeta        json.RawMessage
	tenantInitMeta  map[string]json.RawMessage
	Logger          *slog.Logger
	MaxClients      int           // max concurrent clients (default 100)
	ShutdownTimeout time.Duration // grace period before SIGKILL (default 3s)
}

func (h *Hub) SetInitMeta(meta json.RawMessage) {
	h.initMeta = meta
}

func (h *Hub) SetTenantInitMeta(tenantID string, meta json.RawMessage) {
	if h == nil || tenantID == "" || len(meta) == 0 {
		return
	}
	if h.tenantInitMeta == nil {
		h.tenantInitMeta = map[string]json.RawMessage{}
	}
	h.tenantInitMeta[tenantID] = append(json.RawMessage(nil), meta...)
}

// NewHub creates a new Hub.
func NewHub(toolsJSON json.RawMessage, _ int, _ int, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	hub := &Hub{
		clients:         NewClientRegistry(),
		sessions:        NewSessionRegistry(),
		processes:       NewProcessSupervisor(logger, 3*time.Second),
		hooks:           NewHookEngine(logger),
		runtimes:        NewRuntimeRegistry(),
		tenants:         tenant.NewMemoryStore(tenant.Tenant{ID: tenant.DefaultID, CreatedAt: time.Now().UTC()}),
		toolExec:        make(map[string]toolDispatch),
		runtimeBusy:     make(map[string]int),
		tenantInitMeta:  map[string]json.RawMessage{},
		toolsJSON:       toolsJSON,
		Logger:          logger,
		MaxClients:      100,
		ShutdownTimeout: 3 * time.Second,
	}
	hub.hooks.SetSessionTenantResolver(hub.sessionTenantID)
	hub.policy = NewPolicyEngine(hub)
	hub.tools = NewToolService(hub)
	return hub
}

func (h *Hub) ConfigureRuntimeRegistry(tabulaHome string) error {
	if h == nil {
		return nil
	}
	definitions, err := LoadRuntimeDefinitions(tabulaHome)
	if err != nil {
		return err
	}
	runtimeIDs := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		runtimeIDs[definition.ID] = struct{}{}
	}
	bindings := map[string]TenantRuntimeBinding{}
	if h.tenants != nil {
		items, err := h.tenants.List()
		if err != nil {
			return err
		}
		for _, item := range items {
			binding, err := LoadTenantRuntimeBinding(tabulaHome, item.ID, runtimeIDs)
			if err != nil {
				return err
			}
			bindings[item.ID] = binding
		}
	}
	if h.runtimes == nil {
		h.runtimes = NewRuntimeRegistry()
	}
	return h.runtimes.Configure(definitions, bindings)
}

// SetTenantStore replaces the tenant read model used for join validation.
func (h *Hub) SetTenantStore(store tenant.Store) {
	if store == nil {
		return
	}
	h.tenants = store
}

func (h *Hub) sessionTenantID(session string) string {
	if h == nil || h.sessions == nil {
		return tenant.DefaultID
	}
	return h.sessions.TenantID(session)
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
		h.deleteSessionState(c.session, sess.TenantID)
		h.emitSessionEnd(c.session)
		h.sessions.Remove(c.session)
		return
	}
	h.persistSessionState(c.session)
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
