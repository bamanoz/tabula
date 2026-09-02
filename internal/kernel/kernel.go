package kernel

import (
	"encoding/json"
	"log/slog"
	"os/exec"
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/agent"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
	"github.com/bamanoz/tabula/internal/kernel/process"
	"github.com/bamanoz/tabula/internal/kernel/toolstate"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
	"github.com/bamanoz/tabula/internal/sessionrecord"
	"github.com/bamanoz/tabula/internal/tenant"
)

// Hub manages all connected clients, sessions, and spawned processes.
type Hub struct {
	clients           *ClientRegistry
	sessions          *SessionRegistry
	processes         *process.Supervisor
	hooks             *khooks.Engine
	policy            *PolicyEngine
	tools             *ToolService
	runtimes          *RuntimeRegistry
	agentSessions     agent.SessionRepository
	sessionRecords    sessionrecord.Store
	inputProcessor    *agent.InputProcessor
	driverLeases      *agent.DriverLeaseService
	turnExecutions    *agent.TurnExecutionService
	outputs           *agent.OutputService
	recovery          *agent.RecoveryService
	execution         *ExecutionCoordinator
	driverSupervisor  *agent.DriverSupervisor
	driverLeaseExpiry *driverLeaseExpiryScheduler
	tenants           tenant.Store
	sessionStore      SessionStore
	// toolExec is the unified tool dispatch table per creative
	// `creative-plugin-runtime.md` §4 (D1.7). Keyed by tool name, value
	// carries the runtime routing payload. Entries are synchronized from
	// attached Runtime API targets.
	toolExec             map[string]toolDispatch
	toolExecMu           sync.RWMutex
	exchanges            map[string]pendingExchange
	exchangesMu          sync.Mutex
	suspendedExchanges   map[string]pendingSuspendedExchange
	suspendedExchangeMu  sync.Mutex
	toolLifecycleMu      sync.Mutex
	runtimeBusy          map[string]*runtimeBusyState
	runtimeBusyMu        sync.RWMutex
	runtimeLifecycleMu   sync.Mutex
	runtimeLifecycleLock map[string]*sync.Mutex
	runtimeReconcileMu   sync.Mutex
	runtimeReconcile     map[string]*runtimeReconciliation
	catalogRefreshMu     sync.Mutex
	catalogRefresh       catalogRefreshState
	runID                string
	toolsJSON            json.RawMessage
	initMeta             json.RawMessage
	tenantInitMeta       map[string]json.RawMessage
	clientAuthToken      string
	Logger               *slog.Logger
	MaxClients           int           // max concurrent clients (default 100)
	ShutdownTimeout      time.Duration // grace period before SIGKILL (default 3s)
}

func (h *Hub) SetInitMeta(meta json.RawMessage) {
	h.initMeta = meta
}

// SetClientAuthToken configures the bearer token required by kernel WebSocket clients.
func (h *Hub) SetClientAuthToken(token string) {
	if h == nil {
		return
	}
	h.clientAuthToken = token
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
func NewHub(toolsJSON json.RawMessage, logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	hub := &Hub{
		clients:              NewClientRegistry(),
		sessions:             NewSessionRegistry(),
		processes:            process.NewSupervisor(logger, 3*time.Second),
		hooks:                khooks.NewEngine(logger),
		runtimes:             NewRuntimeRegistry(),
		tenants:              tenant.NewMemoryStore(tenant.Tenant{ID: tenant.DefaultID, CreatedAt: time.Now().UTC()}),
		toolExec:             make(map[string]toolDispatch),
		exchanges:            make(map[string]pendingExchange),
		suspendedExchanges:   make(map[string]pendingSuspendedExchange),
		runtimeBusy:          make(map[string]*runtimeBusyState),
		runtimeLifecycleLock: make(map[string]*sync.Mutex),
		runtimeReconcile:     make(map[string]*runtimeReconciliation),
		tenantInitMeta:       map[string]json.RawMessage{},
		runID:                toolstate.NewRunID(),
		toolsJSON:            toolsJSON,
		Logger:               logger,
		MaxClients:           100,
		ShutdownTimeout:      3 * time.Second,
	}
	hub.hooks.SetSessionTenantResolver(hub.sessionTenantID)
	hub.hooks.SetAuditRecorder(hub.recordHookDispatchAudit)
	hub.policy = NewPolicyEngine(hub)
	hub.tools = NewToolService(hub)
	return hub
}

func (h *Hub) SetSessionRecordStore(store sessionrecord.Store) {
	if h == nil {
		return
	}
	h.sessionRecords = store
}

// SetAgentSessionRepository enables durable driver reconciliation and execution for v4 sessions.
func (h *Hub) SetAgentSessionRepository(repository agent.SessionRepository) {
	if h == nil {
		return
	}
	h.StopAgentLifecycle()
	h.agentSessions = repository
	h.inputProcessor = nil
	h.driverLeases = nil
	h.turnExecutions = nil
	h.outputs = nil
	h.recovery = nil
	h.execution = nil
	h.driverSupervisor = nil
	h.driverLeaseExpiry = nil
	if repository == nil {
		return
	}

	inputProcessor, err := agent.NewInputProcessor(repository)
	if err != nil {
		h.Logger.Error("configure input processor", "err", err)
		return
	}
	driverLeases, err := agent.NewDriverLeaseService(repository, agent.LeaseOptions{
		TTL:               30 * time.Second,
		HeartbeatInterval: 10 * time.Second,
	})
	if err != nil {
		h.Logger.Error("configure driver lease service", "err", err)
		return
	}
	turnExecutions, err := agent.NewTurnExecutionService(repository)
	if err != nil {
		h.Logger.Error("configure turn execution service", "err", err)
		return
	}
	outputs, err := agent.NewOutputService(repository)
	if err != nil {
		h.Logger.Error("configure output service", "err", err)
		return
	}
	recovery, err := agent.NewRecoveryService(repository)
	if err != nil {
		h.Logger.Error("configure recovery service", "err", err)
		return
	}
	execution, err := newExecutionCoordinator(repository, turnExecutions, recovery, h.runtimes)
	if err != nil {
		h.Logger.Error("configure execution coordinator", "err", err)
		return
	}
	execution.prepareTurn = h.prepareTurnContext

	h.inputProcessor = inputProcessor
	h.driverLeases = driverLeases
	h.turnExecutions = turnExecutions
	h.outputs = outputs
	h.recovery = recovery
	h.execution = execution
	h.driverSupervisor = agent.NewDriverSupervisor(repository, h.runtimes)
	h.driverLeaseExpiry = newDriverLeaseExpiryScheduler(repository, driverLeases, h.driverSupervisor, h.agentTenantIDs, h.Logger, driverLeaseExpiryInterval)
	h.driverLeaseExpiry.afterTurn = h.dispatchAfterTurn
}

func (h *Hub) ConfigureRuntimeRegistry(definitions []runtimeconfig.Definition, bindings map[string]runtimeconfig.Binding) error {
	if h == nil {
		return nil
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

func (h *Hub) sessionTenantID(tenantID, session string) string {
	if tenantID != "" {
		return tenantID
	}
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
	h.cancelExchangesForClient(c)
	if h.tools != nil {
		h.tools.cancelV4ToolCallsForClient(c)
	}
	h.onClientDisconnect(c)
	h.Logger.Info("client disconnected", "name", c.name, "id", c.id)
}

// onClientDisconnect handles session lifecycle when a client leaves.
func (h *Hub) onClientDisconnect(c *Client) {
	if h.sessions == nil || c.session == "" {
		return
	}
	sess, ok := h.sessions.Get(c.session, c.tenantID)
	if !ok {
		return
	}
	sess.RemoveClient(c.name)
	if sess.ClientCount() == 0 {
		h.deleteSessionState(c.session, sess.TenantID)
		h.emitSessionEnd(sess.TenantID, c.session)
		h.sessions.Remove(c.session, sess.TenantID)
		return
	}
	h.persistSessionState(sess.TenantID, c.session)
}

// HandleBusMessage routes one internal extension message.
func (h *Hub) HandleBusMessage(sender *Client, msg *BusMessage) {
	if BusMessageType(msg.Type) == MsgJoin {
		h.handleJoin(sender, msg)
		return
	}
	h.handleSessionMessage(sender, msg)
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
	h.StopAgentLifecycle()
	h.processes.SetTimeout(h.ShutdownTimeout)
	h.processes.Shutdown()
}

// GetSession returns a session by ID, or nil if not found.
func (h *Hub) GetSession(id, tenantID string) (*Session, bool) {
	return h.sessions.Get(id, tenantID)
}
