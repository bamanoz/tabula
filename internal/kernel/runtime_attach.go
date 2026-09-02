package kernel

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"github.com/bamanoz/tabula/internal/agent"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
)

// RuntimeAttachOptions configures the authenticated Runtime API listener path.
type RuntimeAttachOptions struct {
	Auth           runtimeauth.Authenticator
	Logger         *slog.Logger
	RuntimePID     int
	RuntimePIDFunc func(runtimeID string) int
}

type runtimeReconciliation struct {
	attachmentDone <-chan struct{}
	cancel         context.CancelFunc
}

// ServeAuthenticatedRuntime validates the first Hello frame, writes HelloAck,
// registers the accepted runtime in the Hub read model, and then watches the
// post-handshake stream until it disconnects. M2-07 will promote the attached
// connection from read-model evidence to plugin dispatch routing.
func (h *Hub) ServeAuthenticatedRuntime(ctx context.Context, c *codec.Conn, opts RuntimeAttachOptions) (err error) {
	if h == nil {
		return fmt.Errorf("kernel: nil Hub")
	}
	if c == nil {
		return fmt.Errorf("runtime codec connection is nil")
	}
	defer func() { _ = c.CloseNow() }()

	_, frame, err := c.Read(ctx)
	if err != nil {
		return err
	}
	hello, ok := frame.(*wire.Hello)
	if !ok {
		return fmt.Errorf("runtime handshake expected hello, got %T", frame)
	}
	ack := opts.Auth.HelloAckContext(ctx, *hello)
	if err := c.Write(ctx, ack); err != nil {
		return err
	}
	if !ack.Accepted {
		if opts.Logger != nil {
			opts.Logger.Warn("runtime authentication rejected", "runtime_id", hello.RuntimeID)
		}
		return nil
	}

	if h.runtimes == nil {
		h.runtimes = NewRuntimeRegistry()
	}
	attachedConn := runtimeconn.NewWithSink(c, hello.RuntimeID, h.runtimeAsyncSink())
	defer attachedConn.Close()
	registered := false
	setupErr := func() error {
		unlock := h.lockRuntimeLifecycle(hello.RuntimeID)
		defer unlock()

		runtimePID := opts.RuntimePID
		if opts.RuntimePIDFunc != nil {
			runtimePID = opts.RuntimePIDFunc(hello.RuntimeID)
		}
		if registerErr := h.runtimes.RegisterHello(hello.RuntimeID, attachedConn, hello.Capabilities, runtimePID, hello.TenantsServed); registerErr != nil {
			return registerErr
		}
		registered = true
		for _, capability := range hello.Capabilities {
			h.syncRuntimeCapability(hello.RuntimeID, capability)
		}
		h.rebuildHookIndex()
		if runtimeCapabilitiesHaveHook(hello.Capabilities, "before_prompt_build") {
			h.scheduleRuntimeCatalogRefreshForTenants(hello.TenantsServed, true)
		}
		return nil
	}()
	if registered {
		defer func() {
			h.detachRuntimeConnection(context.WithoutCancel(ctx), hello.RuntimeID, attachedConn.Done(), opts.Logger, err)
		}()
	}
	if setupErr != nil {
		return setupErr
	}
	h.startRuntimeReconciliation(ctx, hello.RuntimeID, hello.TenantsServed, attachedConn.Done())
	if opts.Logger != nil {
		opts.Logger.Info("runtime attached", "runtime_id", hello.RuntimeID)
	}
	select {
	case <-attachedConn.Done():
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hub) startRuntimeReconciliation(ctx context.Context, runtimeID string, tenantsServed []string, attachmentDone <-chan struct{}) {
	driverSupervisor := h.driverSupervisor
	execution := h.execution
	tenantStore := h.tenants
	if driverSupervisor == nil && execution == nil {
		return
	}
	reconcileCtx, cancel := context.WithCancel(ctx)
	reconciliation := &runtimeReconciliation{
		attachmentDone: attachmentDone,
		cancel:         cancel,
	}
	h.runtimeReconcileMu.Lock()
	if h.runtimeReconcile == nil {
		h.runtimeReconcile = make(map[string]*runtimeReconciliation)
	}
	previous := h.runtimeReconcile[runtimeID]
	h.runtimeReconcile[runtimeID] = reconciliation
	h.runtimeReconcileMu.Unlock()
	if previous != nil {
		previous.cancel()
	}
	go func() {
		defer h.finishRuntimeReconciliation(runtimeID, reconciliation)
		h.reconcileAttachedRuntime(reconcileCtx, runtimeID, tenantsServed, tenantStore, driverSupervisor, execution)
	}()
}

func (h *Hub) reconcileAttachedRuntime(ctx context.Context, runtimeID string, tenantsServed []string, tenantStore tenant.Store, driverSupervisor *agent.DriverSupervisor, execution *ExecutionCoordinator) {
	if tenantStore == nil {
		h.logRuntimeReconciliationError(ctx, "runtime tenant reconciliation setup failed", runtimeID, fmt.Errorf("tenant store is not configured"))
		return
	}
	tenants, err := tenantStore.List()
	if err != nil {
		h.logRuntimeReconciliationError(ctx, "runtime tenant reconciliation setup failed", runtimeID, err)
		return
	}
	tenantIDs := make([]string, 0, len(tenants))
	for _, tenant := range tenants {
		if runtimeServesTenant(tenantsServed, tenant.ID) {
			tenantIDs = append(tenantIDs, tenant.ID)
		}
	}
	if driverSupervisor != nil {
		if err := driverSupervisor.ReconcileRuntime(ctx, tenantIDs, runtimeID); err != nil {
			h.logRuntimeReconciliationError(ctx, "runtime driver reconciliation failed", runtimeID, err)
		}
	}
	if execution != nil {
		if err := execution.ReconcileRuntime(ctx, tenantIDs, runtimeID); err != nil {
			h.logRuntimeReconciliationError(ctx, "runtime execution reconciliation failed", runtimeID, err)
		}
	}
}

func (h *Hub) logRuntimeReconciliationError(ctx context.Context, message, runtimeID string, err error) {
	if ctx.Err() == nil && h.Logger != nil {
		h.Logger.Warn(message, "runtime_id", runtimeID, "error", err)
	}
}

func (h *Hub) finishRuntimeReconciliation(runtimeID string, reconciliation *runtimeReconciliation) {
	h.runtimeReconcileMu.Lock()
	if h.runtimeReconcile[runtimeID] == reconciliation {
		delete(h.runtimeReconcile, runtimeID)
	}
	h.runtimeReconcileMu.Unlock()
}

func (h *Hub) cancelRuntimeReconciliation(runtimeID string, attachmentDone <-chan struct{}) {
	h.runtimeReconcileMu.Lock()
	reconciliation := h.runtimeReconcile[runtimeID]
	if reconciliation != nil && reconciliation.attachmentDone == attachmentDone {
		delete(h.runtimeReconcile, runtimeID)
	} else {
		reconciliation = nil
	}
	h.runtimeReconcileMu.Unlock()
	if reconciliation != nil {
		reconciliation.cancel()
	}
}

func (h *Hub) lockRuntimeLifecycle(runtimeID string) func() {
	h.runtimeLifecycleMu.Lock()
	lock := h.runtimeLifecycleLock[runtimeID]
	if lock == nil {
		lock = &sync.Mutex{}
		h.runtimeLifecycleLock[runtimeID] = lock
	}
	h.runtimeLifecycleMu.Unlock()
	lock.Lock()
	return lock.Unlock
}

func (h *Hub) detachRuntimeConnection(ctx context.Context, runtimeID string, done <-chan struct{}, logger *slog.Logger, cause error) {
	unlock := h.lockRuntimeLifecycle(runtimeID)
	defer unlock()
	if !h.runtimes.AttachmentCurrent(runtimeID, done) {
		return
	}
	h.cancelRuntimeReconciliation(runtimeID, done)
	tenants := h.runtimes.TenantsServed(runtimeID)
	if h.driverLeases != nil {
		if disconnectErr := h.driverLeases.DisconnectRuntime(ctx, tenants, runtimeID); disconnectErr != nil && logger != nil {
			logger.Warn("mark detached runtime driver leases suspect", "runtime_id", runtimeID, "tenants", tenants, "err", disconnectErr)
		}
	}
	rebuildContext := h.runtimeHasHook(runtimeID, "before_prompt_build")
	if !h.runtimes.MarkDetachedIfCurrent(runtimeID, done, cause) {
		return
	}
	removedTools := h.removeRuntimeTools(runtimeID)
	h.rebuildHookIndex()
	h.scheduleRuntimeCatalogRefreshForTenants(tenants, rebuildContext)
	if logger != nil {
		logger.Warn("runtime detached", "runtime_id", runtimeID, "tenants", tenants, "removed_tools", removedTools, "err", cause)
	}
}

// SnapshotRuntimes returns a JSON snapshot of Runtime API attachments.
func (h *Hub) SnapshotRuntimes() []byte {
	return snapshotRuntimes(h)
}

// RuntimeAttached reports whether one runtime is currently attached.
func (h *Hub) RuntimeAttached(runtimeID string) bool {
	if h == nil || h.runtimes == nil {
		return false
	}
	return h.runtimes.Attached(runtimeID)
}

// RuntimeConfigured reports whether a runtime id exists in the configured registry.
func (h *Hub) RuntimeConfigured(runtimeID string) bool {
	if h == nil || h.runtimes == nil {
		return false
	}
	return h.runtimes.Defined(runtimeID)
}

// SetAttachedRuntimePID updates the read-model pid for one attached runtime.
func (h *Hub) SetAttachedRuntimePID(runtimeID string, pid int) {
	if h == nil || h.runtimes == nil {
		return
	}
	h.runtimes.SetPID(runtimeID, pid)
}

// DetachRuntimeForRevoke removes an active runtime after its token is revoked.
func (h *Hub) DetachRuntimeForRevoke(runtimeID string) {
	if h == nil || h.runtimes == nil {
		return
	}
	unlock := h.lockRuntimeLifecycle(runtimeID)
	defer unlock()
	conn := h.runtimes.RuntimeConn(runtimeID)
	if closer, ok := conn.(interface{ Done() <-chan struct{} }); ok {
		h.cancelRuntimeReconciliation(runtimeID, closer.Done())
	}
	if conn != nil {
		_ = conn.Close()
	}
	tenants := h.runtimes.TenantsServed(runtimeID)
	rebuildContext := h.runtimeHasHook(runtimeID, "before_prompt_build")
	h.runtimes.MarkDetached(runtimeID, fmt.Errorf("runtime token revoked"))
	removedTools := h.removeRuntimeTools(runtimeID)
	h.rebuildHookIndex()
	h.scheduleRuntimeCatalogRefreshForTenants(tenants, rebuildContext)
	h.Logger.Warn("runtime detached for revoke", "runtime_id", runtimeID, "tenants", tenants, "removed_tools", removedTools)
}
