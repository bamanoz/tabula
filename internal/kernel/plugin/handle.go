package plugin

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// Handle is the kernel-side representation of a single registered plugin
// process. It tracks the plugin's identity, its negotiated tool catalog
// and hook subscriptions, the lifecycle status, and the in-flight
// request bookkeeping used by tool_call / event correlation.
//
// Phase 2 D2.1 scaffolding ships the static surface only: a Handle has no
// live subprocess attached yet. The runtime/supervisor wiring (spawn,
// stdin/stdout pipes, restart loop) lands in subsequent BUILD passes.
// Until then a Handle is constructed via NewHandle in tests and by the
// future Runtime.Spawn(...) call.
//
// Handle deliberately does NOT implement kernel.HookSubscriber here — that
// adapter lives in the parent kernel package (handle_hooksub.go) to avoid
// an import cycle. The methods exposed here are the primitives the
// adapter wraps: ID, Subscriptions, Tools, IsAlive, Done, SendEvent,
// CallTool, Shutdown.
type Handle struct {
	id  string
	cfg map[string]any

	// writer is the NDJSON sink the kernel uses to push messages into
	// the plugin (kernel → plugin direction). Phase 2 D2.1 leaves this
	// nil; it will be wired by Runtime.Spawn to the plugin's stdin.
	writer *Writer

	mu            sync.RWMutex
	tools         []ToolSpec
	subscriptions []SubscriptionSpec

	// alive flips false on process exit / shutdown / register-fail.
	// IsAlive reads it atomically so HookSubscriber.IsConnected() in
	// handle_hooksub.go remains lock-free on the hot dispatch path.
	alive atomic.Bool
	pid   atomic.Int64
	// startedAt records when the runtime marked this handle alive. Supervisor
	// restart policy uses it to reset crash counters after a clean run.
	startedAt time.Time

	// registered flips true once register-reply has been processed.
	// Hook dispatch must skip a Handle that is alive but not yet
	// registered (creative §2 / §2.6 register-fail policy).
	registered atomic.Bool

	// done is closed on process exit, mirroring (*Client).Done().
	done     chan struct{}
	doneOnce sync.Once

	// pending tracks in-flight tool_call callIds awaiting tool_result.
	// EventReply correlation uses the same map keyed by hook callId
	// since callId namespaces are disjoint (kernel mints them).
	pendingMu sync.Mutex
	pending   map[string]chan *Message
}

// NewHandle constructs a Handle that is alive=false, registered=false,
// with no writer attached. Useful for unit tests of the Handle surface
// and as the construction primitive for Runtime.Spawn.
func NewHandle(id string, cfg map[string]any) *Handle {
	return &Handle{
		id:      id,
		cfg:     cfg,
		done:    make(chan struct{}),
		pending: make(map[string]chan *Message),
	}
}

// ID returns the plugin id from the manifest.
func (h *Handle) ID() string { return h.id }

// Config returns the merged config delivered in register_request.
func (h *Handle) Config() map[string]any { return h.cfg }

// Tools returns the plugin's currently-active tool catalog. The slice is
// a copy; callers may mutate it freely.
func (h *Handle) Tools() []ToolSpec {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]ToolSpec, len(h.tools))
	copy(out, h.tools)
	return out
}

// Subscriptions returns the plugin's hook subscription list (copy).
func (h *Handle) Subscriptions() []SubscriptionSpec {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]SubscriptionSpec, len(h.subscriptions))
	copy(out, h.subscriptions)
	return out
}

// IsAlive reports whether the plugin process is currently running and
// has not been marked closed by Close. Lock-free for hot-path use.
func (h *Handle) IsAlive() bool { return h.alive.Load() }

// IsRegistered reports whether the register handshake has completed.
// HookSubscriber adapters must AND this with IsAlive to decide whether
// to dispatch events (creative §2.6 — pre-register events drop).
func (h *Handle) IsRegistered() bool { return h.registered.Load() }

// SetPID records the operating-system process id for diagnostics/snapshots.
func (h *Handle) SetPID(pid int) {
	h.pid.Store(int64(pid))
}

// PID returns the operating-system process id, or 0 if unknown.
func (h *Handle) PID() int {
	return int(h.pid.Load())
}

// Done returns a channel that is closed when the plugin process exits
// (or Close is called). Mirrors (*Client).Done() so the HookEngine can
// cancel pending interactive hooks the same way for plugins.
func (h *Handle) Done() <-chan struct{} { return h.done }

// SetWriter attaches the NDJSON writer for kernel → plugin messages.
// Called by Runtime.Spawn after starting the subprocess. After this
// call Handle is ready to send register_request etc.
func (h *Handle) SetWriter(w *Writer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.writer = w
}

// MarkAlive flips the alive flag to true. Called by Runtime.Spawn after
// the subprocess starts. Idempotent.
func (h *Handle) MarkAlive() {
	h.mu.Lock()
	h.startedAt = time.Now()
	h.mu.Unlock()
	h.alive.Store(true)
}

// StartedAt returns the timestamp recorded by MarkAlive. A zero value means
// the handle was never marked alive (or predates runtime supervision tests).
func (h *Handle) StartedAt() time.Time {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.startedAt
}

// SendRegisterRequest writes the initial kernel → plugin register_request
// preamble. Runtime.Spawn calls this immediately after attaching stdin/stdout
// and marking the subprocess alive, before waiting for the plugin's register
// reply.
func (h *Handle) SendRegisterRequest(p *RegisterRequestParams) error {
	return h.writeRaw(MethodRegisterRequest, p)
}

// MarkRegistered records the plugin's register-reply payload (tools and
// subscriptions become authoritative) and flips the registered flag.
// Returns an error if the plugin id mismatches or a writer is missing.
func (h *Handle) MarkRegistered(reg *RegisterParams) error {
	if reg == nil {
		return errors.New("plugin: nil register params")
	}
	if reg.PluginID != "" && reg.PluginID != h.id {
		return fmt.Errorf("plugin: register plugin_id mismatch: manifest=%s register=%s", h.id, reg.PluginID)
	}
	h.mu.Lock()
	h.tools = append([]ToolSpec(nil), reg.Tools...)
	h.subscriptions = append([]SubscriptionSpec(nil), reg.Subscriptions...)
	h.mu.Unlock()
	h.registered.Store(true)
	return nil
}

// ApplyUpdateTools atomically replaces the tool catalog after an
// update_tools message (creative §2.5 / D2.10). The Removed list is
// advisory only; the new Tools slice is authoritative.
func (h *Handle) ApplyUpdateTools(p *UpdateToolsParams) {
	if p == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.tools = append([]ToolSpec(nil), p.Tools...)
}

// SendEvent writes an `event` JSON-RPC message to the plugin's stdin.
// Used by the HookSubscriber adapter to deliver bus events.
func (h *Handle) SendEvent(p *EventParams) error {
	if p != nil && p.CallID != "" {
		h.registerPending(p.CallID)
	}
	if err := h.writeRaw(MethodEvent, p); err != nil {
		if p != nil && p.CallID != "" {
			h.cancelPending(p.CallID)
		}
		return err
	}
	return nil
}

// SendToolCall writes a `tool_call` message and registers the callId
// in the pending map so a later tool_result can be routed to the
// returned channel. The caller is responsible for closing/draining the
// channel when its deadline elapses (creative §2.6 — kernel synthesises
// a timeout error and drops the callId from the pending map).
func (h *Handle) SendToolCall(p *ToolCallParams) (<-chan *Message, error) {
	if p == nil || p.CallID == "" {
		return nil, errors.New("plugin: tool_call requires callId")
	}
	ch := h.registerPending(p.CallID)
	if err := h.writeRaw(MethodToolCall, p); err != nil {
		h.cancelPending(p.CallID)
		return nil, err
	}
	return ch, nil
}

// SendShutdown writes a graceful shutdown request. Caller is responsible
// for the subsequent SIGTERM/SIGKILL escalation per creative §2.9.
func (h *Handle) SendShutdown() error {
	return h.writeRaw(MethodShutdown, ShutdownParams{})
}

// DeliverResult routes an inbound tool_result / event_reply to the
// channel registered for its callId. Returns false if no pending entry
// exists (caller should log+drop per creative §2.6 unsolicited rule).
func (h *Handle) DeliverResult(callID string, msg *Message) bool {
	if callID == "" {
		return false
	}
	h.pendingMu.Lock()
	ch, ok := h.pending[callID]
	if ok {
		delete(h.pending, callID)
	}
	h.pendingMu.Unlock()
	if !ok {
		return false
	}
	// Non-blocking send: the channel is buffered=1 by registerPending
	// so this never blocks; if it would, the caller already gave up.
	select {
	case ch <- msg:
	default:
	}
	close(ch)
	return true
}

// CancelPending releases a pending callId without delivering a message.
// Used by deadline timers to fail-out a tool_call.
func (h *Handle) CancelPending(callID string) {
	h.cancelPending(callID)
}

// PendingCount returns the number of in-flight callIds (test helper).
func (h *Handle) PendingCount() int {
	h.pendingMu.Lock()
	defer h.pendingMu.Unlock()
	return len(h.pending)
}

// Close marks the plugin as no longer alive and closes Done.
// All pending callIds are released; their channels remain open with no
// further sends (the ranger will see a closed Done and bail).
func (h *Handle) Close() {
	h.alive.Store(false)
	h.registered.Store(false)
	h.doneOnce.Do(func() { close(h.done) })
	h.pendingMu.Lock()
	for id := range h.pending {
		ch := h.pending[id]
		delete(h.pending, id)
		close(ch)
	}
	h.pendingMu.Unlock()
}

func (h *Handle) writeRaw(method string, params any) error {
	h.mu.RLock()
	w := h.writer
	h.mu.RUnlock()
	if w == nil {
		return errors.New("plugin: handle has no writer attached")
	}
	if !h.alive.Load() {
		return errors.New("plugin: handle is not alive")
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return fmt.Errorf("plugin: marshal %s params: %w", method, err)
	}
	return w.WriteMessage(&Message{Method: method, Params: raw})
}

func (h *Handle) registerPending(callID string) chan *Message {
	ch := make(chan *Message, 1)
	h.pendingMu.Lock()
	h.pending[callID] = ch
	h.pendingMu.Unlock()
	return ch
}

func (h *Handle) cancelPending(callID string) {
	h.pendingMu.Lock()
	ch, ok := h.pending[callID]
	if ok {
		delete(h.pending, callID)
	}
	h.pendingMu.Unlock()
	if ok {
		close(ch)
	}
}
