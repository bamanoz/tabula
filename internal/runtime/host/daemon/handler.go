// Package daemon contains the M2 runtime daemon frame handler.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/host/driver"
	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/pool"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

// Options configures the M2 daemon handler.
type Options struct {
	Store  *manifest.Store
	Pool   *pool.Pool
	Driver *driver.Supervisor
}

// Handler answers Runtime API requests for the runtime daemon.
type Handler struct {
	startedAt time.Time
	store     *manifest.Store
	pool      *pool.Pool
	driver    *driver.Supervisor

	mu      sync.Mutex
	cancels map[string]context.CancelFunc

	asyncOnce   sync.Once
	asyncFrames <-chan any
}

// AsyncFrames exposes optional runtime-originated async plugin-control frames
// synthesized by the worker pool.
func (h *Handler) AsyncFrames() <-chan any {
	if h == nil || (h.pool == nil && h.driver == nil) {
		return nil
	}
	h.asyncOnce.Do(func() {
		frames := make(chan any, 128)
		h.asyncFrames = frames
		var forwarders sync.WaitGroup
		if h.pool != nil {
			go func() {
				ctx := context.Background()
				h.pool.PrimeRuntimeTargets(ctx, nil)
				h.pool.PrimeDynamicTargets(ctx, nil)
			}()
			forwarders.Add(1)
			go forwardFrames(&forwarders, frames, h.pool.AsyncFrames())
		}
		if h.driver != nil {
			forwarders.Add(2)
			go forwardDriverFrames(&forwarders, frames, h.driver.Events())
			go forwardFrames(&forwarders, frames, h.driver.ExecutionEvents())
		}
		go func() {
			forwarders.Wait()
			close(frames)
		}()
	})
	return h.asyncFrames
}

// NewHandler creates an M2 daemon handler.
func NewHandler(opts ...Options) *Handler {
	h := &Handler{startedAt: time.Now(), cancels: map[string]context.CancelFunc{}}
	if len(opts) > 0 {
		h.store = opts[0].Store
		h.pool = opts[0].Pool
		h.driver = opts[0].Driver
	}
	return h
}

func forwardFrames(forwarders *sync.WaitGroup, dst chan<- any, src <-chan any) {
	defer forwarders.Done()
	for frame := range src {
		dst <- frame
	}
}

func forwardDriverFrames(forwarders *sync.WaitGroup, dst chan<- any, src <-chan wire.DriverLifecycle) {
	defer forwarders.Done()
	for frame := range src {
		dst <- frame
	}
}

func (h *Handler) Hello(context.Context, wire.Hello) (wire.HelloAck, error) {
	return wire.HelloAck{
		Op:       wire.OpHelloAck,
		Accepted: false,
		Error:    &wire.Error{Code: wire.ErrorProtocolError, Retryable: false, Message: "runtime daemon does not accept inbound hello"},
	}, nil
}

func (h *Handler) Invoke(ctx context.Context, in wire.Invoke) (wire.InvokeResult, error) {
	if h.pool == nil {
		return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: in.CallID, OK: false, Error: &wire.Error{Code: wire.ErrorInternal, Retryable: false, Message: "worker pool not configured"}}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var cancel context.CancelFunc
	if timeout := in.TimeoutDuration(); timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	h.registerCancel(in.CallID, cancel)
	defer h.unregisterCancel(in.CallID)
	defer cancel()
	return h.pool.Invoke(ctx, in)
}

func (h *Handler) Cancel(_ context.Context, in wire.Cancel) (wire.CancelAck, error) {
	h.mu.Lock()
	cancel := h.cancels[in.CallID]
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return wire.CancelAck{Op: wire.OpCancelAck, CallID: in.CallID}, nil
}

func (h *Handler) Health(context.Context, wire.Health) (wire.HealthResp, error) {
	workerCount := 0
	if h.pool != nil {
		workerCount = h.pool.WorkerCount()
	}
	return wire.HealthResp{Op: wire.OpHealthResp, OK: true, UptimeMS: time.Since(h.startedAt).Milliseconds(), WorkerCount: workerCount}, nil
}

func (h *Handler) ListCapabilities(context.Context, wire.ListCapabilities) (wire.ListCapabilitiesResp, error) {
	var targets []wire.Capability
	if h.pool != nil {
		targets = h.pool.Capabilities()
	} else if h.store != nil {
		targets = h.store.Capabilities()
	}
	return wire.ListCapabilitiesResp{Op: wire.OpListCapabilitiesResp, Targets: targets}, nil
}

func (h *Handler) Reload(_ context.Context, in wire.Reload) (wire.ReloadAck, error) {
	if h.store != nil {
		if len(in.Tenants) == 0 {
			if err := h.store.Reload(); err != nil {
				return wire.ReloadAck{}, err
			}
		} else {
			for _, tenantID := range in.Tenants {
				if err := h.store.ReloadTenant(tenantID); err != nil {
					return wire.ReloadAck{}, err
				}
			}
		}
	}
	var evicted []wire.Target
	if h.pool != nil {
		evicted = h.pool.Reload(in.Target, in.Tenants...)
		go func() {
			ctx := context.Background()
			h.pool.PrimeTargets(ctx, in.Target)
			h.pool.PrimeRuntimeTargets(ctx, in.Target)
			h.pool.PrimeDynamicTargets(ctx, in.Target)
		}()
	}
	return wire.ReloadAck{Op: wire.OpReloadAck, EvictedTargets: evicted}, nil
}

func (h *Handler) PrepareTenant(ctx context.Context, in wire.PrepareTenant) (wire.PrepareTenantAck, error) {
	if h.pool == nil {
		return wire.PrepareTenantAck{}, errors.New("worker pool not configured")
	}
	capabilities, err := h.pool.PrepareTenant(ctx, in.TenantID)
	if err != nil {
		return wire.PrepareTenantAck{}, err
	}
	return wire.PrepareTenantAck{Op: wire.OpPrepareTenantAck, RequestID: in.RequestID, Capabilities: capabilities}, nil
}

func (h *Handler) DriverEnsure(ctx context.Context, in wire.DriverEnsure) (wire.DriverEnsureAck, error) {
	if h.driver == nil {
		return wire.DriverEnsureAck{}, errors.New("driver supervisor not configured")
	}
	if err := h.driver.Ensure(ctx, in); err != nil {
		return wire.DriverEnsureAck{}, err
	}
	return wire.DriverEnsureAck{Op: wire.OpDriverEnsureAck, RequestID: in.RequestID}, nil
}

func (h *Handler) DriverStop(ctx context.Context, in wire.DriverStop) (wire.DriverStopAck, error) {
	if h.driver == nil {
		return wire.DriverStopAck{}, errors.New("driver supervisor not configured")
	}
	if err := h.driver.Stop(ctx, in.TenantID, in.SessionID); err != nil {
		return wire.DriverStopAck{}, err
	}
	return wire.DriverStopAck{Op: wire.OpDriverStopAck, RequestID: in.RequestID}, nil
}

func (h *Handler) TurnAssign(ctx context.Context, in wire.TurnAssign) (wire.DriverResult, error) {
	if h.driver == nil {
		return rejectedDriverExecution(in.RequestID, errors.New("driver supervisor not configured")), nil
	}
	result, err := h.driver.TurnAssign(ctx, in)
	if err != nil {
		return rejectedDriverExecution(in.RequestID, err), nil
	}
	return result, nil
}

func (h *Handler) TurnPermit(ctx context.Context, in wire.TurnPermit) (wire.DriverResult, error) {
	if h.driver == nil {
		return rejectedDriverExecution(in.RequestID, errors.New("driver supervisor not configured")), nil
	}
	result, err := h.driver.TurnPermit(ctx, in)
	if err != nil {
		return rejectedDriverExecution(in.RequestID, err), nil
	}
	return result, nil
}

func (h *Handler) TurnToolResult(ctx context.Context, in wire.TurnToolResult) (wire.DriverResult, error) {
	if h.driver == nil {
		return rejectedDriverExecution(in.RequestID, errors.New("driver supervisor not configured")), nil
	}
	if err := h.driver.TurnToolResult(ctx, in); err != nil {
		return rejectedDriverExecution(in.RequestID, err), nil
	}
	return wire.DriverResult{Op: wire.OpDriverResult, RequestID: in.RequestID, Accepted: true}, nil
}

func (h *Handler) TurnCancel(ctx context.Context, in wire.TurnCancel) (wire.DriverResult, error) {
	if h.driver == nil {
		return rejectedDriverExecution(in.RequestID, errors.New("driver supervisor not configured")), nil
	}
	result, err := h.driver.TurnCancel(ctx, in)
	if err != nil {
		return rejectedDriverExecution(in.RequestID, err), nil
	}
	return result, nil
}

func rejectedDriverExecution(requestID string, err error) wire.DriverResult {
	result := wire.DriverResult{Op: wire.OpDriverResult, RequestID: requestID, Accepted: false}
	switch {
	case errors.Is(err, context.DeadlineExceeded):
		result.Error = &wire.Error{Code: wire.ErrorTimeout, Retryable: true}
	case errors.Is(err, context.Canceled):
		result.Error = &wire.Error{Code: wire.ErrorCancelled, Retryable: true}
	case errors.Is(err, driver.ErrWorkerNotFound):
		result.Error = &wire.Error{Code: wire.ErrorRuntimeUnavailable, Retryable: true}
	case errors.Is(err, driver.ErrStaleGeneration), errors.Is(err, driver.ErrStaleInstance):
		result.Error = &wire.Error{Code: wire.ErrorUnauthorized, Retryable: false}
	default:
		result.Error = &wire.Error{Code: wire.ErrorInternal, Retryable: true}
	}
	return result
}

func (h *Handler) DriverLeaseGranted(ctx context.Context, in wire.DriverLeaseGranted) error {
	if h.driver == nil {
		return errors.New("driver supervisor not configured")
	}
	return h.driver.DriverLeaseGranted(ctx, in)
}

func (h *Handler) DriverResult(ctx context.Context, in wire.DriverResult) error {
	if h.driver == nil {
		return errors.New("driver supervisor not configured")
	}
	return h.driver.DriverResult(ctx, in)
}

func (h *Handler) HookEvent(ctx context.Context, in wire.HookEvent) (wire.HookEventReply, error) {
	if h.pool == nil {
		return wire.HookEventReply{Op: wire.OpHookEventReply, CallID: in.CallID, Action: wire.HookActionDeny, Reason: "worker pool not configured"}, nil
	}
	reply, err := h.pool.HookEvent(ctx, in)
	if err != nil {
		if errors.Is(err, pool.ErrTargetNotFound) {
			return wire.HookEventReply{Op: wire.OpHookEventReply, CallID: in.CallID, Action: wire.HookActionOK, Reason: "target not found"}, nil
		}
		return wire.HookEventReply{}, err
	}
	if reply == nil {
		return wire.HookEventReply{}, nil
	}
	return *reply, nil
}

func (h *Handler) registerCancel(callID string, cancel context.CancelFunc) {
	h.mu.Lock()
	h.cancels[callID] = cancel
	h.mu.Unlock()
}

func (h *Handler) unregisterCancel(callID string) {
	h.mu.Lock()
	delete(h.cancels, callID)
	h.mu.Unlock()
}

// RawNotImplementedPayload is reserved for later worker policy tests that need
// a stable JSON result shape without invoking a real worker.
func RawNotImplementedPayload() json.RawMessage {
	return json.RawMessage(`{"status":"not_implemented"}`)
}
