// Package mock provides an in-memory RuntimeConn for kernel-side unit tests.
package mock

import (
	"context"
	"sync"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

// RuntimeConn is a programmable in-memory implementation of runtime.RuntimeConn.
type RuntimeConn struct {
	mu             sync.Mutex
	closed         bool
	programs       map[invokeKey]invokeProgram
	capabilities   []wire.Capability
	recorded       []runtimeapi.InvokeReq
	recordedNotify chan struct{}
	hookEvents     []runtimeapi.HookEventReq
	hookNotify     chan struct{}
	done           chan struct{}
	cancels        []string
	reloads        []runtimeapi.ReloadReq
	pending        map[string]chan wire.Error
}

// WithCapabilities configures the ListCapabilities response directly.
func (m *RuntimeConn) WithCapabilities(capabilities ...wire.Capability) *RuntimeConn {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.capabilities = append([]wire.Capability(nil), capabilities...)
	return m
}

var _ runtimeapi.RuntimeConn = (*RuntimeConn)(nil)

type invokeKey struct {
	tenantID string
	target   string
	tool     string
}

type invokeProgram struct {
	delay time.Duration
	resp  runtimeapi.InvokeResp
	set   bool
}

// New returns a new mock runtime connection.
func New() *RuntimeConn {
	return &RuntimeConn{programs: make(map[invokeKey]invokeProgram), recordedNotify: make(chan struct{}, 1), hookNotify: make(chan struct{}, 1), done: make(chan struct{}), pending: make(map[string]chan wire.Error)}
}

// OnInvoke starts configuring a response for a tenant/target/tool tuple.
func (m *RuntimeConn) OnInvoke(tenantID string, target wire.Target, tool string) *InvokeBuilder {
	return &InvokeBuilder{mock: m, key: invokeKey{tenantID: tenantID, target: mockTargetKey(target), tool: tool}}
}

// InvokeBuilder configures one programmed Invoke response.
type InvokeBuilder struct {
	mock *RuntimeConn
	key  invokeKey
	prog invokeProgram
}

// Delay configures a delay before returning the response.
func (b *InvokeBuilder) Delay(d time.Duration) *InvokeBuilder {
	b.prog.delay = d
	return b
}

// Return configures a successful JSON result.
func (b *InvokeBuilder) Return(data []byte) *RuntimeConn {
	b.prog.resp = runtimeapi.InvokeResp{OK: true, Data: data}
	b.prog.set = true
	return b.save()
}

// ReturnError configures a structured Runtime API wire error result.
func (b *InvokeBuilder) ReturnError(err wire.Error) *RuntimeConn {
	b.prog.resp = runtimeapi.InvokeResp{OK: false, Error: &err}
	b.prog.set = true
	return b.save()
}

func (b *InvokeBuilder) save() *RuntimeConn {
	b.mock.mu.Lock()
	defer b.mock.mu.Unlock()
	b.mock.programs[b.key] = b.prog
	return b.mock
}

// Invoke records and executes a programmed call.
func (m *RuntimeConn) Invoke(ctx context.Context, req runtimeapi.InvokeReq) (runtimeapi.InvokeResp, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return unavailable(req.CallID), nil
	}
	terminalCh := make(chan wire.Error, 1)
	m.pending[req.CallID] = terminalCh
	m.recorded = append(m.recorded, req)
	select {
	case m.recordedNotify <- struct{}{}:
	default:
	}
	prog := m.programs[invokeKey{tenantID: req.TenantID, target: mockTargetKey(req.Target), tool: req.Tool}]
	m.mu.Unlock()

	defer func() {
		m.mu.Lock()
		delete(m.pending, req.CallID)
		m.mu.Unlock()
	}()

	if !prog.set {
		return runtimeapi.InvokeResp{CallID: req.CallID, OK: false, Error: &wire.Error{Code: wire.ErrorToolNotFound, Retryable: false}}, nil
	}
	if prog.delay > 0 {
		timer := time.NewTimer(prog.delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return runtimeapi.InvokeResp{}, ctx.Err()
		case terminal := <-terminalCh:
			return runtimeapi.InvokeResp{CallID: req.CallID, OK: false, Error: &terminal}, nil
		case <-timer.C:
		}
	}
	resp := prog.resp
	resp.CallID = req.CallID
	return resp, nil
}

func (m *RuntimeConn) InvokeStream(ctx context.Context, req runtimeapi.InvokeReq, sink runtimeapi.InvokeStreamSink) (runtimeapi.InvokeResp, error) {
	resp, err := m.Invoke(ctx, req)
	if err != nil || sink == nil || !resp.OK {
		return resp, err
	}
	if err := sink.Start(req.CallID); err != nil {
		return runtimeapi.InvokeResp{}, err
	}
	if len(resp.Data) > 0 {
		if err := sink.Delta(req.CallID, resp.Data); err != nil {
			return runtimeapi.InvokeResp{}, err
		}
	}
	if err := sink.End(req.CallID, int64(len(resp.Data))); err != nil {
		return runtimeapi.InvokeResp{}, err
	}
	bytes := int64(len(resp.Data))
	resp.Data = nil
	resp.Streamed = true
	resp.Bytes = bytes
	return resp, nil
}

// Cancel records cancellation and unblocks a pending Invoke if present.
func (m *RuntimeConn) Cancel(ctx context.Context, callID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.cancels = append(m.cancels, callID)
	if ch, ok := m.pending[callID]; ok {
		ch <- wire.Error{Code: wire.ErrorCancelled, Retryable: false}
		delete(m.pending, callID)
	}
	return nil
}

// Health returns a default healthy response.
func (m *RuntimeConn) Health(context.Context) (runtimeapi.HealthResp, error) {
	return runtimeapi.HealthResp{Op: wire.OpHealthResp, OK: true}, nil
}

// ListCapabilities returns capabilities for configured targets.
func (m *RuntimeConn) ListCapabilities(context.Context) (runtimeapi.ListCapabilitiesResp, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.capabilities != nil {
		return runtimeapi.ListCapabilitiesResp{Op: wire.OpListCapabilitiesResp, Targets: append([]wire.Capability(nil), m.capabilities...)}, nil
	}
	seen := map[string]struct{}{}
	var targets []wire.Capability
	for key := range m.programs {
		if _, ok := seen[key.target]; ok {
			continue
		}
		seen[key.target] = struct{}{}
		targets = append(targets, wire.Capability{Target: parseMockTargetKey(key.target), State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})
	}
	return runtimeapi.ListCapabilitiesResp{Op: wire.OpListCapabilitiesResp, Targets: targets}, nil
}

func mockTargetKey(target wire.Target) string {
	return string(target.Kind) + "\x00" + target.ID
}

func parseMockTargetKey(key string) wire.Target {
	for i := range key {
		if key[i] == '\x00' {
			return wire.Target{Kind: wire.TargetKind(key[:i]), ID: key[i+1:]}
		}
	}
	return wire.Target{ID: key}
}

// Reload returns an acknowledgement for the requested target.
func (m *RuntimeConn) Reload(_ context.Context, req runtimeapi.ReloadReq) (runtimeapi.ReloadResp, error) {
	m.mu.Lock()
	m.reloads = append(m.reloads, req)
	m.mu.Unlock()
	resp := runtimeapi.ReloadResp{Op: wire.OpReloadAck}
	if req.Target != nil {
		resp.EvictedTargets = []wire.Target{*req.Target}
	}
	return resp, nil
}

// SendHookEvent records hook delivery for kernel-side tests.
func (m *RuntimeConn) SendHookEvent(_ context.Context, req runtimeapi.HookEventReq) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.hookEvents = append(m.hookEvents, req)
	select {
	case m.hookNotify <- struct{}{}:
	default:
	}
	return nil
}

// Close drains pending invokes with runtime_unavailable.
func (m *RuntimeConn) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil
	}
	m.closed = true
	close(m.done)
	for _, ch := range m.pending {
		ch <- wire.Error{Code: wire.ErrorRuntimeUnavailable, Retryable: true}
	}
	for callID := range m.pending {
		delete(m.pending, callID)
	}
	return nil
}

// Done closes when the mock runtime is closed.
func (m *RuntimeConn) Done() <-chan struct{} { return m.done }

// RecordedInvokes returns invokes in chronological order.
func (m *RuntimeConn) RecordedInvokes() []runtimeapi.InvokeReq {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]runtimeapi.InvokeReq, len(m.recorded))
	copy(out, m.recorded)
	return out
}

// WaitHookEvent waits for a hook event to be delivered to the mock runtime.
func (m *RuntimeConn) WaitHookEvent(ctx context.Context) (runtimeapi.HookEventReq, bool) {
	for {
		m.mu.Lock()
		if len(m.hookEvents) > 0 {
			req := m.hookEvents[0]
			m.hookEvents = append([]runtimeapi.HookEventReq(nil), m.hookEvents[1:]...)
			m.mu.Unlock()
			return req, true
		}
		m.mu.Unlock()
		select {
		case <-ctx.Done():
			return runtimeapi.HookEventReq{}, false
		case <-m.hookNotify:
		}
	}
}

// RecordedCancels returns cancelled call IDs in chronological order.
func (m *RuntimeConn) RecordedCancels() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.cancels))
	copy(out, m.cancels)
	return out
}

// RecordedReloads returns reload requests in chronological order.
func (m *RuntimeConn) RecordedReloads() []runtimeapi.ReloadReq {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]runtimeapi.ReloadReq, len(m.reloads))
	copy(out, m.reloads)
	return out
}

// WaitForRecordedInvokes blocks until at least n invokes have been recorded or
// ctx is cancelled. It lets tests synchronize with in-flight invokes without
// polling or sleeping.
func (m *RuntimeConn) WaitForRecordedInvokes(ctx context.Context, n int) error {
	for {
		m.mu.Lock()
		count := len(m.recorded)
		m.mu.Unlock()
		if count >= n {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-m.recordedNotify:
		}
	}
}

func unavailable(callID string) runtimeapi.InvokeResp {
	return runtimeapi.InvokeResp{CallID: callID, OK: false, Error: &wire.Error{Code: wire.ErrorRuntimeUnavailable, Retryable: true}}
}
