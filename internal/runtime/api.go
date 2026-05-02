// Package runtime defines the kernel-facing RuntimeConn contract shared by all
// execution backends.
package runtime

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

// ErrNotImplemented is returned by scaffolding implementations that exist only
// to let later slices wire non-nil placeholders.
var ErrNotImplemented = errors.New("runtime: not implemented")

// RuntimeConn is a concurrency-safe connection from the kernel to one runtime.
// Invoke may be called concurrently. Close must fail all in-flight and future
// operations promptly with runtime_unavailable once real transports exist.
type RuntimeConn interface {
	// Invoke performs a tenant-scoped tool call and blocks until a terminal result,
	// context cancellation, timeout, disconnect, or Close.
	Invoke(ctx context.Context, req InvokeReq) (InvokeResp, error)
	// Cancel asks the runtime to abort an in-flight call_id. It returns after the
	// cancel frame is accepted or the context/connection fails.
	Cancel(ctx context.Context, callID string) error
	// Health returns tenantless runtime liveness information.
	Health(ctx context.Context) (HealthResp, error)
	// ListCapabilities returns tenantless target capability information.
	ListCapabilities(ctx context.Context) (ListCapabilitiesResp, error)
	// Reload asks the runtime to refresh worker/config state, optionally scoped to
	// a target.
	Reload(ctx context.Context, req ReloadReq) (ReloadResp, error)
	// Close closes the connection and rejects later operations.
	Close() error
}

// Backend produces RuntimeConn instances for a concrete transport/backend.
type Backend interface {
	// Connect establishes or attaches to a runtime connection.
	Connect(ctx context.Context) (RuntimeConn, error)
}

// Target is the ergonomic kernel-facing form of the Runtime API target object.
type Target = wire.Target

// InvokeReq is the kernel-facing request for a tenant-scoped tool call.
type InvokeReq struct {
	CallID    string
	TenantID  string
	Target    Target
	Tool      string
	Args      json.RawMessage
	TimeoutMS int64
}

// InvokeResp is the terminal result of an Invoke.
type InvokeResp struct {
	CallID string
	OK     bool
	Data   json.RawMessage
	Error  *wire.Error
}

// HealthResp is the kernel-facing runtime liveness response.
type HealthResp = wire.HealthResp

// Capability describes one runtime target and its tools.
type Capability = wire.Capability

// ListCapabilitiesResp is the kernel-facing target capability response.
type ListCapabilitiesResp = wire.ListCapabilitiesResp

// ReloadReq optionally scopes runtime reload to a target.
type ReloadReq struct {
	Target *Target
}

// ReloadResp returns targets affected by reload.
type ReloadResp = wire.ReloadAck

type notImplementedConn struct{}

var _ RuntimeConn = (*notImplementedConn)(nil)

// NewNotImplementedConn returns a placeholder RuntimeConn for wiring-only
// milestones. It is not a production fallback.
func NewNotImplementedConn() RuntimeConn { return &notImplementedConn{} }

func (c *notImplementedConn) Invoke(context.Context, InvokeReq) (InvokeResp, error) {
	return InvokeResp{}, ErrNotImplemented
}

func (c *notImplementedConn) Cancel(context.Context, string) error { return ErrNotImplemented }

func (c *notImplementedConn) Health(context.Context) (HealthResp, error) {
	return HealthResp{}, ErrNotImplemented
}

func (c *notImplementedConn) ListCapabilities(context.Context) (ListCapabilitiesResp, error) {
	return ListCapabilitiesResp{}, ErrNotImplemented
}

func (c *notImplementedConn) Reload(context.Context, ReloadReq) (ReloadResp, error) {
	return ReloadResp{}, ErrNotImplemented
}

func (c *notImplementedConn) Close() error { return ErrNotImplemented }
