package kernel

import (
	"context"
	"fmt"
	"log/slog"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

// RuntimeAttachOptions configures the authenticated Runtime API listener path.
type RuntimeAttachOptions struct {
	Auth           runtimeauth.Authenticator
	Logger         *slog.Logger
	RuntimePID     int
	RuntimePIDFunc func(runtimeID string) int
}

// ServeAuthenticatedRuntime validates the first Hello frame, writes HelloAck,
// registers the accepted runtime in the Hub read model, and then watches the
// post-handshake stream until it disconnects. M2-07 will promote the attached
// connection from read-model evidence to plugin dispatch routing.
func (h *Hub) ServeAuthenticatedRuntime(ctx context.Context, c *codec.Conn, opts RuntimeAttachOptions) error {
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
	runtimePID := opts.RuntimePID
	if opts.RuntimePIDFunc != nil {
		runtimePID = opts.RuntimePIDFunc(hello.RuntimeID)
	}
	if err := h.runtimes.RegisterHello(hello.RuntimeID, attachedConn, hello.Capabilities, runtimePID, hello.TenantsServed); err != nil {
		return err
	}
	for _, capability := range hello.Capabilities {
		h.syncRuntimeCapability(hello.RuntimeID, capability)
	}
	h.rebuildHookIndex()
	if runtimeCapabilitiesHaveHook(hello.Capabilities, "before_prompt_build") {
		h.scheduleRuntimeCatalogRefreshForTenants(hello.TenantsServed, true)
	}
	if opts.Logger != nil {
		opts.Logger.Info("runtime attached", "runtime_id", hello.RuntimeID)
	}
	select {
	case <-attachedConn.Done():
		err = nil
	case <-ctx.Done():
		err = ctx.Err()
	}
	tenants := h.runtimes.TenantsServed(hello.RuntimeID)
	rebuildContext := h.runtimeHasHook(hello.RuntimeID, "before_prompt_build")
	h.runtimes.MarkDetached(hello.RuntimeID, err)
	removedTools := h.removeRuntimeTools(hello.RuntimeID)
	h.rebuildHookIndex()
	h.scheduleRuntimeCatalogRefreshForTenants(tenants, rebuildContext)
	if opts.Logger != nil {
		opts.Logger.Warn("runtime detached", "runtime_id", hello.RuntimeID, "tenants", tenants, "removed_tools", removedTools, "err", err)
	}
	return err
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
	conn := h.runtimes.RuntimeConn(runtimeID)
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

// ConfigureRuntimeRegistryForTest replaces runtime definitions in tests.
func (h *Hub) ConfigureRuntimeRegistryForTest(definitions []RuntimeDefinition) {
	if h.runtimes == nil {
		h.runtimes = NewRuntimeRegistry()
	}
	_ = h.runtimes.Configure(definitions, nil)
}

// RegisterRuntimeForTest attaches a runtime connection in tests.
func (h *Hub) RegisterRuntimeForTest(runtimeID string, conn runtimeapi.RuntimeConn) error {
	if h.runtimes == nil {
		h.runtimes = NewRuntimeRegistry()
	}
	return h.runtimes.RegisterHello(runtimeID, conn, nil, 0)
}
