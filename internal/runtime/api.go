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
	// InvokeStream performs a tenant-scoped tool call and delivers successful
	// result bytes to sink incrementally instead of materializing them into one
	// response payload.
	InvokeStream(ctx context.Context, req InvokeReq, sink InvokeStreamSink) (InvokeResp, error)
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
	// SendHookEvent emits one kernel-originated hook event to the runtime.
	SendHookEvent(ctx context.Context, req HookEventReq) error
	// Close closes the connection and rejects later operations.
	Close() error
}

// AsyncSink receives runtime-originated async plugin-control frames.
type AsyncSink interface {
	CatalogUpdated(runtimeID string, update wire.CatalogUpdate) error
	HookEventReplied(runtimeID string, reply wire.HookEventReply) error
	PluginSent(runtimeID string, send wire.PluginSend) error
	PluginLogged(runtimeID string, log wire.PluginLog)
	LifecycleNoticed(runtimeID string, notice wire.LifecycleNotice) error
	RuntimeProtocolError(runtimeID string, err error)
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
	CallID            string
	TenantID          string
	SessionID         string
	TurnCorrelationID string
	Target            Target
	Tool              string
	Args              json.RawMessage
	TimeoutMS         int64
}

// InvokeResp is the terminal result of an Invoke.
type InvokeResp struct {
	CallID   string
	OK       bool
	Data     json.RawMessage
	Error    *wire.Error
	Streamed bool
	Bytes    int64
}

// InvokeStreamSink receives one successful invoke result as start/delta/end.
// Implementations must treat callbacks as strictly ordered and non-concurrent
// per call.
type InvokeStreamSink interface {
	Start(callID string) error
	Delta(callID string, chunk []byte) error
	End(callID string, totalBytes int64) error
}

// HealthResp is the kernel-facing runtime liveness response.
type HealthResp = wire.HealthResp

// Capability describes one runtime target and its tools.
type Capability = wire.Capability

// ListCapabilitiesResp is the kernel-facing target capability response.
type ListCapabilitiesResp = wire.ListCapabilitiesResp

// HookEventReq is the kernel-facing hook delivery request.
type HookEventReq = wire.HookEvent

// ReloadReq optionally scopes runtime reload to a target.
type ReloadReq struct {
	Target  *Target
	Tenants []string
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

func (c *notImplementedConn) InvokeStream(context.Context, InvokeReq, InvokeStreamSink) (InvokeResp, error) {
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

func (c *notImplementedConn) SendHookEvent(context.Context, HookEventReq) error {
	return ErrNotImplemented
}

func (c *notImplementedConn) Close() error { return ErrNotImplemented }

type nopAsyncSink struct{}

// NopAsyncSink returns an AsyncSink that drops every event.
func NopAsyncSink() AsyncSink { return nopAsyncSink{} }

func (nopAsyncSink) CatalogUpdated(string, wire.CatalogUpdate) error     { return nil }
func (nopAsyncSink) HookEventReplied(string, wire.HookEventReply) error  { return nil }
func (nopAsyncSink) PluginSent(string, wire.PluginSend) error            { return nil }
func (nopAsyncSink) PluginLogged(string, wire.PluginLog)                 {}
func (nopAsyncSink) LifecycleNoticed(string, wire.LifecycleNotice) error { return nil }
func (nopAsyncSink) RuntimeProtocolError(string, error)                  {}
