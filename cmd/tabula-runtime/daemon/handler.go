// Package daemon contains the M2 runtime daemon frame handler.
package daemon

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/bamanoz/tabula/cmd/tabula-runtime/manifest"
	"github.com/bamanoz/tabula/cmd/tabula-runtime/pool"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

// Options configures the M2 daemon handler.
type Options struct {
	Store *manifest.Store
	Pool  *pool.Pool
}

// Handler answers Runtime API requests for the runtime daemon.
type Handler struct {
	startedAt time.Time
	store     *manifest.Store
	pool      *pool.Pool

	mu      sync.Mutex
	cancels map[string]context.CancelFunc
}

// AsyncFrames exposes optional runtime-originated async plugin-control frames
// synthesized by the worker pool.
func (h *Handler) AsyncFrames() <-chan any {
	if h == nil || h.pool == nil {
		return nil
	}
	go h.pool.PrimeTargets(context.Background(), nil)
	return h.pool.AsyncFrames()
}

// NewHandler creates an M2 daemon handler.
func NewHandler(opts ...Options) *Handler {
	h := &Handler{startedAt: time.Now(), cancels: map[string]context.CancelFunc{}}
	if len(opts) > 0 {
		h.store = opts[0].Store
		h.pool = opts[0].Pool
	}
	return h
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
	callCtx := ctx
	var cancel context.CancelFunc
	if timeout := in.TimeoutDuration(); timeout > 0 {
		callCtx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		callCtx, cancel = context.WithCancel(ctx)
	}
	h.registerCancel(in.CallID, cancel)
	defer h.unregisterCancel(in.CallID)
	defer cancel()
	return h.pool.Invoke(callCtx, in)
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
		if err := h.store.Reload(); err != nil {
			return wire.ReloadAck{}, err
		}
	}
	var evicted []wire.Target
	if h.pool != nil {
		evicted = h.pool.Reload(in.Target)
		go h.pool.PrimeTargets(context.Background(), in.Target)
	}
	return wire.ReloadAck{Op: wire.OpReloadAck, EvictedTargets: evicted}, nil
}

func (h *Handler) HookEvent(ctx context.Context, in wire.HookEvent) (wire.HookEventReply, error) {
	if h.pool == nil {
		return wire.HookEventReply{Op: wire.OpHookEventReply, CallID: in.CallID, Action: wire.HookActionDeny, Reason: "worker pool not configured"}, nil
	}
	reply, err := h.pool.HookEvent(ctx, in)
	if err != nil {
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
