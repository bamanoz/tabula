// Package conn implements RuntimeConn over the Runtime API codec.
package conn

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/coder/websocket"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

// Handler services Runtime API frames accepted by a server-side transport.
type Handler interface {
	Hello(context.Context, wire.Hello) (wire.HelloAck, error)
	Invoke(context.Context, wire.Invoke) (wire.InvokeResult, error)
	Cancel(context.Context, wire.Cancel) (wire.CancelAck, error)
	Health(context.Context, wire.Health) (wire.HealthResp, error)
	ListCapabilities(context.Context, wire.ListCapabilities) (wire.ListCapabilitiesResp, error)
	Reload(context.Context, wire.Reload) (wire.ReloadAck, error)
	PrepareTenant(context.Context, wire.PrepareTenant) (wire.PrepareTenantAck, error)
	HookEvent(context.Context, wire.HookEvent) (wire.HookEventReply, error)
}

// DriverHandler optionally handles session-scoped driver process control.
type DriverHandler interface {
	DriverEnsure(context.Context, wire.DriverEnsure) (wire.DriverEnsureAck, error)
	DriverStop(context.Context, wire.DriverStop) (wire.DriverStopAck, error)
}

// DriverExecutionHandler optionally handles kernel-originated driver execution
// v4 turn control.
type DriverExecutionHandler interface {
	TurnAssign(context.Context, wire.TurnAssign) (wire.DriverResult, error)
	TurnPermit(context.Context, wire.TurnPermit) (wire.DriverResult, error)
	TurnToolResult(context.Context, wire.TurnToolResult) (wire.DriverResult, error)
	TurnCancel(context.Context, wire.TurnCancel) (wire.DriverResult, error)
}

// DriverExecutionResponseHandler optionally receives correlated kernel replies
// to runtime-originated driver execution v4 requests.
type DriverExecutionResponseHandler interface {
	DriverLeaseGranted(context.Context, wire.DriverLeaseGranted) error
	DriverResult(context.Context, wire.DriverResult) error
}

// AsyncFrameSource optionally supplies runtime-originated async plugin-control
// frames that should be written to the peer on the same connection.
type AsyncFrameSource interface {
	AsyncFrames() <-chan any
}

const invokeResultStreamChunkBytes = 64 << 10

type invokeResultStreamState struct {
	started bool
	data    []byte
	bytes   int64
	nextSeq int64
}

type pendingInvoke struct {
	ch    chan invokeOutcome
	sink  runtimeapi.InvokeStreamSink
	state invokeResultStreamState
}

type invokeOutcome struct {
	resp runtimeapi.InvokeResp
	err  error
}

type driverExecutionOutcome struct {
	resp wire.DriverResult
	err  error
}

type prepareTenantOutcome struct {
	resp wire.PrepareTenantAck
	err  error
}

// Conn is a concrete RuntimeConn backed by one codec connection.
type Conn struct {
	c         *codec.Conn
	writeMu   sync.Mutex
	runtimeID string
	sink      runtimeapi.AsyncSink

	done        chan struct{}
	closeOnce   sync.Once
	asyncCtx    context.Context
	asyncCancel context.CancelFunc

	mu               sync.Mutex
	pending          map[string]*pendingInvoke
	cancels          map[string]chan error
	health           chan runtimeapi.HealthResp
	caps             chan runtimeapi.ListCapabilitiesResp
	reloads          chan runtimeapi.ReloadResp
	tenantPrepares   map[string]chan prepareTenantOutcome
	driverEnsures    map[string]chan error
	driverStops      map[string]chan error
	driverExecution  map[string]chan driverExecutionOutcome
	driverAsyncQueue chan any
}

var _ runtimeapi.RuntimeConn = (*Conn)(nil)
var _ runtimeapi.DriverExecutionConn = (*Conn)(nil)

// New creates a RuntimeConn and starts its response router.
func New(c *codec.Conn) *Conn {
	return NewWithSink(c, "", runtimeapi.NopAsyncSink())
}

// NewWithSink creates a RuntimeConn with a runtime-id-scoped async sink.
func NewWithSink(c *codec.Conn, runtimeID string, sink runtimeapi.AsyncSink) *Conn {
	if sink == nil {
		sink = runtimeapi.NopAsyncSink()
	}
	asyncCtx, asyncCancel := context.WithCancel(context.Background())
	rc := &Conn{
		c:                c,
		runtimeID:        runtimeID,
		sink:             sink,
		done:             make(chan struct{}),
		asyncCtx:         asyncCtx,
		asyncCancel:      asyncCancel,
		pending:          make(map[string]*pendingInvoke),
		cancels:          make(map[string]chan error),
		health:           make(chan runtimeapi.HealthResp, 1),
		caps:             make(chan runtimeapi.ListCapabilitiesResp, 1),
		reloads:          make(chan runtimeapi.ReloadResp, 1),
		tenantPrepares:   make(map[string]chan prepareTenantOutcome),
		driverEnsures:    make(map[string]chan error),
		driverStops:      make(map[string]chan error),
		driverExecution:  make(map[string]chan driverExecutionOutcome),
		driverAsyncQueue: make(chan any, 128),
	}
	go rc.driverExecutionLoop()
	go rc.readLoop()
	return rc
}

// Handshake writes Hello and waits for HelloAck before starting normal use.
func Handshake(ctx context.Context, c *codec.Conn, hello wire.Hello) (wire.HelloAck, error) {
	if err := c.Write(ctx, hello); err != nil {
		return wire.HelloAck{}, err
	}
	_, frame, err := c.Read(ctx)
	if err != nil {
		return wire.HelloAck{}, err
	}
	ack, ok := frame.(*wire.HelloAck)
	if !ok {
		return wire.HelloAck{}, fmt.Errorf("runtime handshake expected hello_ack, got %T", frame)
	}
	return *ack, nil
}

func (c *Conn) Invoke(ctx context.Context, req runtimeapi.InvokeReq) (runtimeapi.InvokeResp, error) {
	ch, err := c.registerPending(req.CallID, nil)
	if err != nil {
		return runtimeapi.InvokeResp{}, err
	}
	frame := wire.Invoke{Op: wire.OpInvoke, CallID: req.CallID, TenantID: req.TenantID, SessionID: req.SessionID, TurnCorrelationID: req.TurnCorrelationID, Meta: req.Meta, Target: req.Target, Tool: req.Tool, Args: req.Args, TimeoutMS: req.TimeoutMS}
	if err := c.write(ctx, frame); err != nil {
		c.unregisterPending(req.CallID)
		return runtimeapi.InvokeResp{}, err
	}
	select {
	case outcome := <-ch:
		return outcome.resp, outcome.err
	case <-ctx.Done():
		c.unregisterPending(req.CallID)
		return runtimeapi.InvokeResp{}, ctx.Err()
	case <-c.done:
		return unavailableResp(req.CallID), nil
	}
}

func (c *Conn) InvokeStream(ctx context.Context, req runtimeapi.InvokeReq, sink runtimeapi.InvokeStreamSink) (runtimeapi.InvokeResp, error) {
	if sink == nil {
		return c.Invoke(ctx, req)
	}
	ch, err := c.registerPending(req.CallID, sink)
	if err != nil {
		return runtimeapi.InvokeResp{}, err
	}
	frame := wire.Invoke{Op: wire.OpInvoke, CallID: req.CallID, TenantID: req.TenantID, SessionID: req.SessionID, TurnCorrelationID: req.TurnCorrelationID, Meta: req.Meta, Target: req.Target, Tool: req.Tool, Args: req.Args, TimeoutMS: req.TimeoutMS}
	if err := c.write(ctx, frame); err != nil {
		c.unregisterPending(req.CallID)
		return runtimeapi.InvokeResp{}, err
	}
	select {
	case outcome := <-ch:
		return outcome.resp, outcome.err
	case <-ctx.Done():
		c.unregisterPending(req.CallID)
		return runtimeapi.InvokeResp{}, ctx.Err()
	case <-c.done:
		return unavailableResp(req.CallID), nil
	}
}

func (c *Conn) Cancel(ctx context.Context, callID string) error {
	ack, err := c.registerCancel(callID)
	if err != nil {
		return err
	}
	if err := c.write(ctx, wire.Cancel{Op: wire.OpCancel, CallID: callID}); err != nil {
		c.unregisterCancel(callID)
		return err
	}
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		c.unregisterCancel(callID)
		return ctx.Err()
	case <-c.done:
		return runtimeUnavailableError()
	}
}

func (c *Conn) Health(ctx context.Context) (runtimeapi.HealthResp, error) {
	if err := c.write(ctx, wire.Health{Op: wire.OpHealth}); err != nil {
		return runtimeapi.HealthResp{}, err
	}
	select {
	case resp := <-c.health:
		return resp, nil
	case <-ctx.Done():
		return runtimeapi.HealthResp{}, ctx.Err()
	case <-c.done:
		return runtimeapi.HealthResp{}, runtimeUnavailableError()
	}
}

func (c *Conn) ListCapabilities(ctx context.Context) (runtimeapi.ListCapabilitiesResp, error) {
	if err := c.write(ctx, wire.ListCapabilities{Op: wire.OpListCapabilities}); err != nil {
		return runtimeapi.ListCapabilitiesResp{}, err
	}
	select {
	case resp := <-c.caps:
		return resp, nil
	case <-ctx.Done():
		return runtimeapi.ListCapabilitiesResp{}, ctx.Err()
	case <-c.done:
		return runtimeapi.ListCapabilitiesResp{}, runtimeUnavailableError()
	}
}

func (c *Conn) Reload(ctx context.Context, req runtimeapi.ReloadReq) (runtimeapi.ReloadResp, error) {
	if err := c.write(ctx, wire.Reload{Op: wire.OpReload, Target: req.Target, Tenants: req.Tenants}); err != nil {
		return runtimeapi.ReloadResp{}, err
	}
	select {
	case resp := <-c.reloads:
		return resp, nil
	case <-ctx.Done():
		return runtimeapi.ReloadResp{}, ctx.Err()
	case <-c.done:
		return runtimeapi.ReloadResp{}, runtimeUnavailableError()
	}
}

func (c *Conn) PrepareTenant(ctx context.Context, req runtimeapi.PrepareTenantReq) error {
	ack, err := c.registerTenantPrepare(req.RequestID)
	if err != nil {
		return err
	}
	if err := c.write(ctx, wire.PrepareTenant{Op: wire.OpPrepareTenant, RequestID: req.RequestID, TenantID: req.TenantID}); err != nil {
		c.unregisterTenantPrepare(req.RequestID)
		return err
	}
	var outcome prepareTenantOutcome
	select {
	case outcome = <-ack:
	case <-ctx.Done():
		c.unregisterTenantPrepare(req.RequestID)
		return ctx.Err()
	case <-c.done:
		return runtimeUnavailableError()
	}
	if outcome.err != nil {
		return outcome.err
	}
	for _, capability := range outcome.resp.Capabilities {
		if err := c.sink.CatalogUpdated(c.runtimeID, wire.CatalogUpdate{
			Op:       wire.OpCatalogUpdate,
			Target:   capability.Target,
			Tenants:  append([]string(nil), capability.Tenants...),
			Tools:    append([]wire.ToolSpec(nil), capability.Tools...),
			Hooks:    append([]wire.HookSpec(nil), capability.Hooks...),
			Revision: capability.Revision,
			State:    capability.State,
			Source:   capability.Source,
		}); err != nil {
			return fmt.Errorf("apply prepared tenant capability %s: %w", capability.Target.ID, err)
		}
	}
	return nil
}

func (c *Conn) EnsureDriver(ctx context.Context, req runtimeapi.DriverEnsureReq) error {
	ack, err := c.registerDriverRequest(req.RequestID, c.driverEnsures)
	if err != nil {
		return err
	}
	frame := wire.DriverEnsure{
		Op:                wire.OpDriverEnsure,
		RequestID:         req.RequestID,
		TenantID:          req.TenantID,
		SessionID:         req.SessionID,
		ComponentID:       req.ComponentID,
		AgentSpecRevision: req.AgentSpecRevision,
		DesiredGeneration: req.DesiredGeneration,
	}
	if err := c.write(ctx, frame); err != nil {
		c.unregisterDriverRequest(req.RequestID, c.driverEnsures)
		return err
	}
	return c.waitDriverAck(ctx, req.RequestID, ack, c.driverEnsures)
}

func (c *Conn) StopDriver(ctx context.Context, req runtimeapi.DriverStopReq) error {
	ack, err := c.registerDriverRequest(req.RequestID, c.driverStops)
	if err != nil {
		return err
	}
	frame := wire.DriverStop{Op: wire.OpDriverStop, RequestID: req.RequestID, TenantID: req.TenantID, SessionID: req.SessionID}
	if err := c.write(ctx, frame); err != nil {
		c.unregisterDriverRequest(req.RequestID, c.driverStops)
		return err
	}
	return c.waitDriverAck(ctx, req.RequestID, ack, c.driverStops)
}

func (c *Conn) TurnAssign(ctx context.Context, req wire.TurnAssign) (wire.DriverResult, error) {
	return c.driverExecutionRequest(ctx, req.RequestID, req)
}

func (c *Conn) TurnPermit(ctx context.Context, req wire.TurnPermit) (wire.DriverResult, error) {
	return c.driverExecutionRequest(ctx, req.RequestID, req)
}

func (c *Conn) TurnToolResult(ctx context.Context, req wire.TurnToolResult) (wire.DriverResult, error) {
	return c.driverExecutionRequest(ctx, req.RequestID, req)
}

func (c *Conn) TurnCancel(ctx context.Context, req wire.TurnCancel) (wire.DriverResult, error) {
	return c.driverExecutionRequest(ctx, req.RequestID, req)
}

func (c *Conn) driverExecutionRequest(ctx context.Context, requestID string, frame any) (wire.DriverResult, error) {
	result, err := c.registerDriverExecution(requestID)
	if err != nil {
		return wire.DriverResult{}, err
	}
	if err := c.write(ctx, frame); err != nil {
		c.unregisterDriverExecution(requestID)
		return wire.DriverResult{}, err
	}
	select {
	case outcome := <-result:
		return outcome.resp, outcome.err
	case <-ctx.Done():
		c.unregisterDriverExecution(requestID)
		return wire.DriverResult{}, ctx.Err()
	case <-c.done:
		return wire.DriverResult{}, runtimeUnavailableError()
	}
}

func (c *Conn) SendHookEvent(ctx context.Context, req runtimeapi.HookEventReq) error {
	replyMode := req.ReplyMode
	if replyMode == "" {
		replyMode = defaultHookReplyMode(req.Event)
	}
	frame := wire.HookEvent{
		Op:                wire.OpHookEvent,
		TenantID:          req.TenantID,
		CallID:            req.CallID,
		Target:            req.Target,
		Event:             req.Event,
		ReplyMode:         replyMode,
		Data:              req.Data,
		SessionID:         req.SessionID,
		TurnCorrelationID: req.TurnCorrelationID,
	}
	if frame.TenantID == "" && frame.SessionID != "" {
		var payload struct {
			TenantID string `json:"tenant_id"`
		}
		_ = json.Unmarshal(frame.Data, &payload)
		frame.TenantID = payload.TenantID
	}
	return c.write(ctx, frame)
}

func (c *Conn) Close() error {
	c.closeWithUnavailable()
	return c.c.Close(websocket.StatusNormalClosure, "")
}

// Done is closed when the underlying Runtime API connection becomes unusable.
// It is intentionally not part of the public RuntimeConn interface; connection
// owners use it to update attachment read models without exposing lifecycle
// details to dispatch callers.
func (c *Conn) Done() <-chan struct{} { return c.done }

func (c *Conn) readLoop() {
	for {
		_, frame, err := c.c.Read(codec.NoIdleTimeoutContext())
		if err != nil {
			c.closeWithUnavailable()
			return
		}
		switch f := frame.(type) {
		case *wire.InvokeResult:
			if err := c.handleInvokeResult(*f); err != nil {
				c.protocolFailure(err)
				return
			}
		case *wire.InvokeResultStart:
			if err := c.startInvokeResultStream(*f); err != nil {
				c.protocolFailure(err)
				return
			}
		case *wire.InvokeResultDelta:
			if err := c.appendInvokeResultStream(*f); err != nil {
				c.protocolFailure(err)
				return
			}
		case *wire.InvokeResultEnd:
			if err := c.endInvokeResultStream(*f); err != nil {
				c.protocolFailure(err)
				return
			}
		case *wire.CancelAck:
			c.deliverCancel(f.CallID, nil)
		case *wire.HealthResp:
			replaceLatest(c.health, *f)
		case *wire.ListCapabilitiesResp:
			replaceLatest(c.caps, *f)
		case *wire.ReloadAck:
			replaceLatest(c.reloads, *f)
		case *wire.PrepareTenantAck:
			c.deliverTenantPrepare(*f)
		case *wire.DriverEnsureAck:
			c.deliverDriverAck(f.RequestID, c.driverEnsures)
		case *wire.DriverStopAck:
			c.deliverDriverAck(f.RequestID, c.driverStops)
		case *wire.DriverResult:
			if err := c.deliverDriverExecution(*f); err != nil {
				c.protocolFailure(err)
				return
			}
		case *wire.DriverRegister:
			c.queueDriverExecution(*f)
		case *wire.DriverReady:
			c.queueDriverExecution(*f)
		case *wire.DriverHeartbeat:
			c.queueDriverExecution(*f)
		case *wire.TurnPrepared:
			c.queueDriverExecution(*f)
		case *wire.TurnPrepareFailed:
			c.queueDriverExecution(*f)
		case *wire.TurnOutput:
			c.queueDriverExecution(*f)
		case *wire.TurnToolCall:
			c.queueDriverExecution(*f)
		case *wire.TurnCompleted:
			c.queueDriverExecution(*f)
		case *wire.TurnFailed:
			c.queueDriverExecution(*f)
		case *wire.TurnCancelled:
			c.queueDriverExecution(*f)
		case *wire.TurnUncertain:
			c.queueDriverExecution(*f)
		case *wire.CatalogUpdate:
			if err := c.sink.CatalogUpdated(c.runtimeID, *f); err != nil {
				c.sink.RuntimeProtocolError(c.runtimeID, err)
			}
		case *wire.HookEventReply:
			if err := c.sink.HookEventReplied(c.runtimeID, *f); err != nil {
				c.protocolFailure(err)
				return
			}
		case *wire.PluginSend:
			if err := c.sink.PluginSent(c.runtimeID, *f); err != nil {
				c.sink.RuntimeProtocolError(c.runtimeID, err)
			}
		case *wire.PluginLog:
			c.sink.PluginLogged(c.runtimeID, *f)
		case *wire.LifecycleNotice:
			if err := c.sink.LifecycleNoticed(c.runtimeID, *f); err != nil {
				c.sink.RuntimeProtocolError(c.runtimeID, err)
			}
		case *wire.DriverLifecycle:
			if sink, ok := c.sink.(runtimeapi.DriverAsyncSink); ok {
				if err := sink.DriverLifecycleNoticed(c.runtimeID, *f); err != nil {
					c.sink.RuntimeProtocolError(c.runtimeID, err)
				}
			}
		case *wire.HookEvent:
			c.protocolFailure(wire.ProtocolErrorf("unexpected hook_event from runtime"))
			return
		case *wire.Hello, *wire.HelloAck, *wire.Invoke, *wire.Cancel, *wire.Health, *wire.ListCapabilities, *wire.Reload, *wire.DriverLeaseGranted, *wire.TurnAssign, *wire.TurnPermit, *wire.TurnToolResult, *wire.TurnCancel:
			c.protocolFailure(wire.ProtocolErrorf("unexpected request frame %T from runtime", frame))
			return
		}
	}
}

func (c *Conn) write(ctx context.Context, frame any) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	select {
	case <-c.done:
		return runtimeUnavailableError()
	default:
	}
	return c.c.Write(ctx, frame)
}

func (c *Conn) registerPending(callID string, sink runtimeapi.InvokeStreamSink) (chan invokeOutcome, error) {
	if callID == "" {
		return nil, wire.ProtocolErrorf("call_id is required")
	}
	ch := make(chan invokeOutcome, 1)
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return nil, runtimeUnavailableError()
	default:
	}
	if _, exists := c.pending[callID]; exists {
		return nil, wire.ProtocolErrorf("duplicate call_id %q", callID)
	}
	c.pending[callID] = &pendingInvoke{ch: ch, sink: sink, state: invokeResultStreamState{nextSeq: 1}}
	return ch, nil
}

func (c *Conn) unregisterPending(callID string) {
	c.mu.Lock()
	delete(c.pending, callID)
	c.mu.Unlock()
}

func (c *Conn) registerCancel(callID string) (chan error, error) {
	if callID == "" {
		return nil, wire.ProtocolErrorf("call_id is required")
	}
	ch := make(chan error, 1)
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return nil, runtimeUnavailableError()
	default:
	}
	c.cancels[callID] = ch
	return ch, nil
}

func (c *Conn) unregisterCancel(callID string) {
	c.mu.Lock()
	delete(c.cancels, callID)
	c.mu.Unlock()
}

func (c *Conn) registerTenantPrepare(requestID string) (chan prepareTenantOutcome, error) {
	if requestID == "" {
		return nil, wire.ProtocolErrorf("request_id is required")
	}
	ch := make(chan prepareTenantOutcome, 1)
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return nil, runtimeUnavailableError()
	default:
	}
	if _, exists := c.tenantPrepares[requestID]; exists {
		return nil, wire.ProtocolErrorf("duplicate request_id %q", requestID)
	}
	c.tenantPrepares[requestID] = ch
	return ch, nil
}

func (c *Conn) unregisterTenantPrepare(requestID string) {
	c.mu.Lock()
	delete(c.tenantPrepares, requestID)
	c.mu.Unlock()
}

func (c *Conn) deliverTenantPrepare(resp wire.PrepareTenantAck) {
	c.mu.Lock()
	ch := c.tenantPrepares[resp.RequestID]
	delete(c.tenantPrepares, resp.RequestID)
	c.mu.Unlock()
	if ch != nil {
		ch <- prepareTenantOutcome{resp: resp}
	}
}

func (c *Conn) registerDriverRequest(requestID string, pending map[string]chan error) (chan error, error) {
	if requestID == "" {
		return nil, wire.ProtocolErrorf("request_id is required")
	}
	ch := make(chan error, 1)
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return nil, runtimeUnavailableError()
	default:
	}
	if _, exists := pending[requestID]; exists {
		return nil, wire.ProtocolErrorf("duplicate request_id %q", requestID)
	}
	pending[requestID] = ch
	return ch, nil
}

func (c *Conn) unregisterDriverRequest(requestID string, pending map[string]chan error) {
	c.mu.Lock()
	delete(pending, requestID)
	c.mu.Unlock()
}

func (c *Conn) waitDriverAck(ctx context.Context, requestID string, ack chan error, pending map[string]chan error) error {
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		c.unregisterDriverRequest(requestID, pending)
		return ctx.Err()
	case <-c.done:
		return runtimeUnavailableError()
	}
}

func (c *Conn) deliverDriverAck(requestID string, pending map[string]chan error) {
	c.mu.Lock()
	ch := pending[requestID]
	delete(pending, requestID)
	c.mu.Unlock()
	if ch != nil {
		ch <- nil
	}
}

func (c *Conn) registerDriverExecution(requestID string) (chan driverExecutionOutcome, error) {
	if requestID == "" {
		return nil, wire.ProtocolErrorf("request_id is required")
	}
	ch := make(chan driverExecutionOutcome, 1)
	c.mu.Lock()
	defer c.mu.Unlock()
	select {
	case <-c.done:
		return nil, runtimeUnavailableError()
	default:
	}
	if _, exists := c.driverExecution[requestID]; exists {
		return nil, wire.ProtocolErrorf("duplicate request_id %q", requestID)
	}
	c.driverExecution[requestID] = ch
	return ch, nil
}

func (c *Conn) unregisterDriverExecution(requestID string) {
	c.mu.Lock()
	delete(c.driverExecution, requestID)
	c.mu.Unlock()
}

func (c *Conn) deliverDriverExecution(result wire.DriverResult) error {
	if result.RequestID == "" {
		return wire.ProtocolErrorf("driver.result request_id is required")
	}
	c.mu.Lock()
	ch := c.driverExecution[result.RequestID]
	delete(c.driverExecution, result.RequestID)
	c.mu.Unlock()
	if ch == nil {
		return wire.ProtocolErrorf("driver.result has unknown request_id %q", result.RequestID)
	}
	ch <- driverExecutionOutcome{resp: result}
	return nil
}

func (c *Conn) queueDriverExecution(frame any) {
	select {
	case c.driverAsyncQueue <- frame:
	case <-c.done:
	}
}

func (c *Conn) driverExecutionLoop() {
	for {
		select {
		case <-c.done:
			return
		case frame := <-c.driverAsyncQueue:
			sink, ok := c.sink.(runtimeapi.DriverExecutionAsyncSink)
			if !ok {
				c.protocolFailure(wire.ProtocolErrorf("driver execution sink is not configured for %T", frame))
				return
			}
			response, err := dispatchDriverExecutionAsync(c.asyncCtx, sink, c.runtimeID, frame)
			if err != nil {
				c.protocolFailure(err)
				return
			}
			if err := c.write(context.Background(), response); err != nil {
				if !errors.Is(err, io.EOF) {
					c.protocolFailure(err)
				}
				return
			}
			ready, ok := frame.(wire.DriverReady)
			if !ok {
				continue
			}
			result, ok := response.(wire.DriverResult)
			if !ok || !result.Accepted {
				continue
			}
			acknowledged, ok := c.sink.(runtimeapi.DriverReadyAcknowledgedSink)
			if !ok {
				continue
			}
			if err := acknowledged.DriverReadyAcknowledged(c.asyncCtx, c.runtimeID, ready); err != nil {
				c.sink.RuntimeProtocolError(c.runtimeID, err)
			}
		}
	}
}

func dispatchDriverExecutionAsync(ctx context.Context, sink runtimeapi.DriverExecutionAsyncSink, runtimeID string, frame any) (any, error) {
	var requestID string
	var response any
	var err error
	switch f := frame.(type) {
	case wire.DriverRegister:
		requestID = f.RequestID
		response, err = sink.DriverRegister(ctx, runtimeID, f)
	case wire.DriverReady:
		requestID = f.RequestID
		response, err = sink.DriverReady(ctx, runtimeID, f)
	case wire.DriverHeartbeat:
		requestID = f.RequestID
		response, err = sink.DriverHeartbeat(ctx, runtimeID, f)
	case wire.TurnPrepared:
		requestID = f.RequestID
		response, err = sink.TurnPrepared(ctx, runtimeID, f)
	case wire.TurnPrepareFailed:
		requestID = f.RequestID
		response, err = sink.TurnPrepareFailed(ctx, runtimeID, f)
	case wire.TurnOutput:
		requestID = f.RequestID
		response, err = sink.TurnOutput(ctx, runtimeID, f)
	case wire.TurnToolCall:
		requestID = f.RequestID
		response, err = sink.TurnToolCall(ctx, runtimeID, f)
	case wire.TurnCompleted:
		requestID = f.RequestID
		response, err = sink.TurnCompleted(ctx, runtimeID, f)
	case wire.TurnFailed:
		requestID = f.RequestID
		response, err = sink.TurnFailed(ctx, runtimeID, f)
	case wire.TurnCancelled:
		requestID = f.RequestID
		response, err = sink.TurnCancelled(ctx, runtimeID, f)
	case wire.TurnUncertain:
		requestID = f.RequestID
		response, err = sink.TurnUncertain(ctx, runtimeID, f)
	default:
		return nil, wire.ProtocolErrorf("unsupported driver execution frame %T", frame)
	}
	if err != nil {
		return nil, err
	}
	if responseRequestID(response) != requestID {
		return nil, wire.ProtocolErrorf("driver execution response request_id %q does not match request_id %q", responseRequestID(response), requestID)
	}
	return response, nil
}

func responseRequestID(frame any) string {
	switch f := frame.(type) {
	case wire.DriverLeaseGranted:
		return f.RequestID
	case wire.DriverResult:
		return f.RequestID
	default:
		return ""
	}
}

func (c *Conn) deliverInvoke(callID string, resp runtimeapi.InvokeResp) {
	c.mu.Lock()
	pending := c.pending[callID]
	delete(c.pending, callID)
	c.mu.Unlock()
	if pending != nil {
		pending.ch <- invokeOutcome{resp: resp}
	}
}

func (c *Conn) failInvoke(callID string, err error) {
	c.mu.Lock()
	pending := c.pending[callID]
	delete(c.pending, callID)
	c.mu.Unlock()
	if pending != nil {
		pending.ch <- invokeOutcome{err: err}
	}
}

func (c *Conn) deliverCancel(callID string, err error) {
	c.mu.Lock()
	ch := c.cancels[callID]
	delete(c.cancels, callID)
	c.mu.Unlock()
	if ch != nil {
		ch <- err
	}
}

func (c *Conn) closeWithUnavailable() {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		pending := c.pending
		c.pending = make(map[string]*pendingInvoke)
		cancels := c.cancels
		c.cancels = make(map[string]chan error)
		tenantPrepares := c.tenantPrepares
		c.tenantPrepares = make(map[string]chan prepareTenantOutcome)
		driverEnsures := c.driverEnsures
		c.driverEnsures = make(map[string]chan error)
		driverStops := c.driverStops
		c.driverStops = make(map[string]chan error)
		driverExecution := c.driverExecution
		c.driverExecution = make(map[string]chan driverExecutionOutcome)
		c.mu.Unlock()
		for callID, invoke := range pending {
			invoke.ch <- invokeOutcome{resp: unavailableResp(callID)}
		}
		for _, ch := range cancels {
			ch <- runtimeUnavailableError()
		}
		for _, ch := range tenantPrepares {
			ch <- prepareTenantOutcome{err: runtimeUnavailableError()}
		}
		for _, requests := range []map[string]chan error{driverEnsures, driverStops} {
			for _, ch := range requests {
				ch <- runtimeUnavailableError()
			}
		}
		for _, ch := range driverExecution {
			ch <- driverExecutionOutcome{err: runtimeUnavailableError()}
		}
		c.asyncCancel()
		close(c.done)
	})
}

func (c *Conn) handleInvokeResult(result wire.InvokeResult) error {
	c.mu.Lock()
	pending := c.pending[result.CallID]
	c.mu.Unlock()
	if pending == nil {
		return nil
	}
	if pending.sink != nil && result.OK {
		if err := emitInvokeResultToSink(result.CallID, result.Data, pending.sink); err != nil {
			c.failInvoke(result.CallID, err)
			return nil
		}
		c.deliverInvoke(result.CallID, runtimeapi.InvokeResp{CallID: result.CallID, OK: true, Streamed: true, Bytes: int64(len(result.Data))})
		return nil
	}
	c.deliverInvoke(result.CallID, runtimeapi.InvokeResp{CallID: result.CallID, OK: result.OK, Data: result.Data, Error: result.Error})
	return nil
}

func (c *Conn) startInvokeResultStream(start wire.InvokeResultStart) error {
	c.mu.Lock()
	pending, exists := c.pending[start.CallID]
	if !exists {
		c.mu.Unlock()
		return nil
	}
	if pending.state.nextSeq != 1 || len(pending.state.data) > 0 {
		c.mu.Unlock()
		return wire.ProtocolErrorf("duplicate invoke_result_start for call_id %q", start.CallID)
	}
	pending.state.started = true
	sink := pending.sink
	c.mu.Unlock()
	if sink != nil {
		if err := sink.Start(start.CallID); err != nil {
			c.failInvoke(start.CallID, err)
		}
	}
	return nil
}

func (c *Conn) appendInvokeResultStream(delta wire.InvokeResultDelta) error {
	c.mu.Lock()
	pending, exists := c.pending[delta.CallID]
	if !exists {
		c.mu.Unlock()
		return nil
	}
	if !pending.state.started {
		c.mu.Unlock()
		return wire.ProtocolErrorf("invoke_result_delta for call_id %q arrived before invoke_result_start", delta.CallID)
	}
	if delta.Seq != pending.state.nextSeq {
		c.mu.Unlock()
		return wire.ProtocolErrorf("invoke_result_delta for call_id %q expected seq %d, got %d", delta.CallID, pending.state.nextSeq, delta.Seq)
	}
	pending.state.nextSeq++
	pending.state.bytes += int64(len(delta.Data))
	sink := pending.sink
	if sink == nil {
		pending.state.data = append(pending.state.data, delta.Data...)
		c.mu.Unlock()
		return nil
	}
	c.mu.Unlock()
	if err := sink.Delta(delta.CallID, []byte(delta.Data)); err != nil {
		c.failInvoke(delta.CallID, err)
	}
	return nil
}

func (c *Conn) endInvokeResultStream(end wire.InvokeResultEnd) error {
	c.mu.Lock()
	pending, exists := c.pending[end.CallID]
	if !exists {
		c.mu.Unlock()
		return nil
	}
	if !pending.state.started {
		c.mu.Unlock()
		return wire.ProtocolErrorf("invoke_result_end for call_id %q arrived before invoke_result_start", end.CallID)
	}
	sink := pending.sink
	data := append([]byte(nil), pending.state.data...)
	reconstructedBytes := pending.state.bytes
	if sink == nil {
		reconstructedBytes = int64(len(data))
	}
	if end.Bytes != 0 && reconstructedBytes != end.Bytes {
		c.mu.Unlock()
		return wire.ProtocolErrorf("invoke_result_end for call_id %q declared %d bytes, reconstructed %d", end.CallID, end.Bytes, reconstructedBytes)
	}
	ch := pending.ch
	delete(c.pending, end.CallID)
	c.mu.Unlock()
	if sink != nil {
		if err := sink.End(end.CallID, end.Bytes); err != nil {
			if ch != nil {
				ch <- invokeOutcome{err: err}
			}
			return nil
		}
		if ch != nil {
			ch <- invokeOutcome{resp: runtimeapi.InvokeResp{CallID: end.CallID, OK: true, Streamed: true, Bytes: end.Bytes}}
		}
		return nil
	}
	if ch != nil {
		ch <- invokeOutcome{resp: runtimeapi.InvokeResp{CallID: end.CallID, OK: true, Data: json.RawMessage(data), Bytes: end.Bytes}}
	}
	return nil
}

func emitInvokeResultToSink(callID string, data []byte, sink runtimeapi.InvokeStreamSink) error {
	if err := sink.Start(callID); err != nil {
		return err
	}
	remaining := data
	for len(remaining) > 0 {
		chunk, rest := nextBytesChunk(remaining, invokeResultStreamChunkBytes)
		if err := sink.Delta(callID, chunk); err != nil {
			return err
		}
		remaining = rest
	}
	return sink.End(callID, int64(len(data)))
}

func (c *Conn) protocolFailure(err error) {
	c.sink.RuntimeProtocolError(c.runtimeID, err)
	c.closeWithUnavailable()
	_ = c.c.Close(websocket.StatusProtocolError, "runtime protocol error")
}

func replaceLatest[T any](ch chan T, value T) {
	select {
	case ch <- value:
	default:
		<-ch
		ch <- value
	}
}

func unavailableResp(callID string) runtimeapi.InvokeResp {
	return runtimeapi.InvokeResp{CallID: callID, OK: false, Error: &wire.Error{Code: wire.ErrorRuntimeUnavailable, Retryable: true}}
}

func runtimeUnavailableError() error {
	return errors.New("runtime unavailable")
}

// ServeAuthenticated handles one accepted runtime connection, requires the
// first frame to be Hello, and closes the connection after rejected handshakes.
func ServeAuthenticated(ctx context.Context, c *codec.Conn, handler Handler) error {
	defer func() { _ = c.CloseNow() }()
	_, frame, err := c.Read(ctx)
	if err != nil {
		return err
	}
	hello, ok := frame.(*wire.Hello)
	if !ok {
		return fmt.Errorf("runtime handshake expected hello, got %T", frame)
	}
	ack, err := handler.Hello(ctx, *hello)
	if err != nil {
		return err
	}
	if err := c.Write(ctx, ack); err != nil {
		return err
	}
	if !ack.Accepted {
		return nil
	}
	return ServeAfterHandshake(ctx, c, handler)
}

// Serve handles one accepted codec connection until it is closed.
func Serve(ctx context.Context, c *codec.Conn, handler Handler) error {
	defer func() { _ = c.CloseNow() }()
	return ServeAfterHandshake(ctx, c, handler)
}

// ServeAfterHandshake handles post-Hello Runtime API frames until the
// connection closes. It treats an unexpected second Hello as protocol-invalid.
func ServeAfterHandshake(ctx context.Context, c *codec.Conn, handler Handler) error {
	var writeMu sync.Mutex
	var driverPendingMu sync.Mutex
	driverPending := make(map[string]wire.Operation)
	write := func(frame any) error {
		frames, err := expandFramesForWrite(frame)
		if err != nil {
			return err
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		for _, item := range frames {
			if err := c.Write(ctx, item); err != nil {
				return err
			}
		}
		return nil
	}
	writeAsync := func(frame any) error {
		frames, err := expandFramesForWrite(frame)
		if err != nil {
			return nil
		}
		writeMu.Lock()
		defer writeMu.Unlock()
		for _, item := range frames {
			requestID, responseOp, correlated := driverExecutionAsyncCorrelation(item)
			if correlated {
				driverPendingMu.Lock()
				if _, exists := driverPending[requestID]; exists {
					driverPendingMu.Unlock()
					return wire.ProtocolErrorf("duplicate request_id %q", requestID)
				}
				driverPending[requestID] = responseOp
				driverPendingMu.Unlock()
			}
			if err := c.Write(ctx, item); err != nil {
				if correlated {
					driverPendingMu.Lock()
					delete(driverPending, requestID)
					driverPendingMu.Unlock()
				}
				return err
			}
		}
		return nil
	}
	if source, ok := handler.(AsyncFrameSource); ok {
		if frames := source.AsyncFrames(); frames != nil {
			go func() {
				for {
					select {
					case <-ctx.Done():
						return
					case frame, ok := <-frames:
						if !ok {
							return
						}
						if frame == nil {
							continue
						}
						if err := writeAsync(frame); err != nil {
							if errors.Is(err, io.EOF) {
								return
							}
							_ = c.CloseNow()
							return
						}
					}
				}
			}()
		}
	}
	for {
		raw, err := c.ReadRaw(codec.NoIdleTimeoutContext())
		if err != nil {
			return err
		}
		_, frame, err := wire.Decode(raw)
		if err != nil {
			if resp, ok := protocolErrorResponse(raw, err); ok {
				if writeErr := write(resp); writeErr != nil {
					return writeErr
				}
				continue
			}
			return err
		}
		switch f := frame.(type) {
		case *wire.Hello:
			return fmt.Errorf("unexpected hello after runtime handshake")
		case *wire.Invoke:
			go func(in wire.Invoke) {
				resp, err := handler.Invoke(ctx, in)
				if err != nil {
					resp = wire.InvokeResult{Op: wire.OpInvokeResult, CallID: in.CallID, OK: false, Error: &wire.Error{Code: wire.ErrorInternal, Retryable: false, Message: err.Error()}}
				}
				// The read loop returns connection errors. This goroutine must not
				// log frame content because Invoke args may contain user data.
				_ = write(resp)
			}(*f)
		case *wire.Cancel:
			ack, err := handler.Cancel(ctx, *f)
			if err != nil {
				return err
			}
			if err := write(ack); err != nil {
				return err
			}
		case *wire.Health:
			resp, err := handler.Health(ctx, *f)
			if err != nil {
				return err
			}
			if err := write(resp); err != nil {
				return err
			}
		case *wire.ListCapabilities:
			resp, err := handler.ListCapabilities(ctx, *f)
			if err != nil {
				return err
			}
			if err := write(resp); err != nil {
				return err
			}
		case *wire.Reload:
			resp, err := handler.Reload(ctx, *f)
			if err != nil {
				return err
			}
			if err := write(resp); err != nil {
				return err
			}
		case *wire.PrepareTenant:
			resp, err := handler.PrepareTenant(ctx, *f)
			if err != nil {
				return err
			}
			if err := write(resp); err != nil {
				return err
			}
		case *wire.HookEvent:
			go func(in wire.HookEvent) {
				resp, err := handler.HookEvent(ctx, in)
				if err != nil || resp.Op == "" {
					return
				}
				_ = write(resp)
			}(*f)
		case *wire.DriverEnsure:
			driverHandler, ok := handler.(DriverHandler)
			if !ok {
				return fmt.Errorf("runtime handler does not support driver control")
			}
			resp, err := driverHandler.DriverEnsure(ctx, *f)
			if err != nil {
				return err
			}
			if err := write(resp); err != nil {
				return err
			}
		case *wire.DriverStop:
			driverHandler, ok := handler.(DriverHandler)
			if !ok {
				return fmt.Errorf("runtime handler does not support driver control")
			}
			resp, err := driverHandler.DriverStop(ctx, *f)
			if err != nil {
				return err
			}
			if err := write(resp); err != nil {
				return err
			}
		case *wire.TurnAssign, *wire.TurnPermit, *wire.TurnToolResult, *wire.TurnCancel:
			driverHandler, ok := handler.(DriverExecutionHandler)
			if !ok {
				return fmt.Errorf("runtime handler does not support driver execution")
			}
			go func(frame any) {
				resp, err := dispatchDriverExecutionRequest(ctx, driverHandler, frame)
				if err != nil {
					_ = c.CloseNow()
					return
				}
				_ = write(resp)
			}(frame)
		case *wire.DriverLeaseGranted, *wire.DriverResult:
			responseHandler, ok := handler.(DriverExecutionResponseHandler)
			if !ok {
				return fmt.Errorf("runtime handler does not support driver execution responses")
			}
			requestID, op := driverExecutionResponseCorrelation(frame)
			driverPendingMu.Lock()
			expectedOp, exists := driverPending[requestID]
			if exists && expectedOp == op {
				delete(driverPending, requestID)
			}
			driverPendingMu.Unlock()
			if !exists {
				return wire.ProtocolErrorf("driver execution response has unknown request_id %q", requestID)
			}
			if expectedOp != op {
				return wire.ProtocolErrorf("driver execution response for request_id %q has op %q, expected %q", requestID, op, expectedOp)
			}
			if err := dispatchDriverExecutionResponse(ctx, responseHandler, frame); err != nil {
				return err
			}
		case *wire.CatalogUpdate, *wire.HookEventReply, *wire.PluginSend, *wire.PluginLog, *wire.LifecycleNotice, *wire.DriverLifecycle, *wire.DriverRegister, *wire.DriverReady, *wire.DriverHeartbeat, *wire.TurnPrepared, *wire.TurnPrepareFailed, *wire.TurnOutput, *wire.TurnToolCall, *wire.TurnCompleted, *wire.TurnFailed, *wire.TurnCancelled, *wire.TurnUncertain:
			return fmt.Errorf("unexpected runtime-originated async frame %T on server connection", frame)
		default:
			return fmt.Errorf("unsupported runtime frame %T", frame)
		}
	}
}

func driverExecutionAsyncCorrelation(frame any) (string, wire.Operation, bool) {
	switch f := frame.(type) {
	case wire.DriverRegister:
		return f.RequestID, wire.OpDriverLeaseGranted, true
	case *wire.DriverRegister:
		return f.RequestID, wire.OpDriverLeaseGranted, true
	case wire.DriverReady:
		return f.RequestID, wire.OpDriverResult, true
	case *wire.DriverReady:
		return f.RequestID, wire.OpDriverResult, true
	case wire.DriverHeartbeat:
		return f.RequestID, wire.OpDriverResult, true
	case *wire.DriverHeartbeat:
		return f.RequestID, wire.OpDriverResult, true
	case wire.TurnPrepared:
		return f.RequestID, wire.OpDriverResult, true
	case *wire.TurnPrepared:
		return f.RequestID, wire.OpDriverResult, true
	case wire.TurnPrepareFailed:
		return f.RequestID, wire.OpDriverResult, true
	case *wire.TurnPrepareFailed:
		return f.RequestID, wire.OpDriverResult, true
	case wire.TurnOutput:
		return f.RequestID, wire.OpDriverResult, true
	case *wire.TurnOutput:
		return f.RequestID, wire.OpDriverResult, true
	case wire.TurnToolCall:
		return f.RequestID, wire.OpDriverResult, true
	case *wire.TurnToolCall:
		return f.RequestID, wire.OpDriverResult, true
	case wire.TurnCompleted:
		return f.RequestID, wire.OpDriverResult, true
	case *wire.TurnCompleted:
		return f.RequestID, wire.OpDriverResult, true
	case wire.TurnFailed:
		return f.RequestID, wire.OpDriverResult, true
	case *wire.TurnFailed:
		return f.RequestID, wire.OpDriverResult, true
	case wire.TurnCancelled:
		return f.RequestID, wire.OpDriverResult, true
	case *wire.TurnCancelled:
		return f.RequestID, wire.OpDriverResult, true
	case wire.TurnUncertain:
		return f.RequestID, wire.OpDriverResult, true
	case *wire.TurnUncertain:
		return f.RequestID, wire.OpDriverResult, true
	default:
		return "", "", false
	}
}

func driverExecutionResponseCorrelation(frame any) (string, wire.Operation) {
	switch f := frame.(type) {
	case *wire.DriverLeaseGranted:
		return f.RequestID, wire.OpDriverLeaseGranted
	case *wire.DriverResult:
		return f.RequestID, wire.OpDriverResult
	default:
		return "", ""
	}
}

func dispatchDriverExecutionResponse(ctx context.Context, handler DriverExecutionResponseHandler, frame any) error {
	switch f := frame.(type) {
	case *wire.DriverLeaseGranted:
		return handler.DriverLeaseGranted(ctx, *f)
	case *wire.DriverResult:
		return handler.DriverResult(ctx, *f)
	default:
		return wire.ProtocolErrorf("unsupported driver execution response %T", frame)
	}
}

func dispatchDriverExecutionRequest(ctx context.Context, handler DriverExecutionHandler, frame any) (wire.DriverResult, error) {
	var requestID string
	var resp wire.DriverResult
	var err error
	switch f := frame.(type) {
	case *wire.TurnAssign:
		requestID = f.RequestID
		resp, err = handler.TurnAssign(ctx, *f)
	case *wire.TurnPermit:
		requestID = f.RequestID
		resp, err = handler.TurnPermit(ctx, *f)
	case *wire.TurnToolResult:
		requestID = f.RequestID
		resp, err = handler.TurnToolResult(ctx, *f)
	case *wire.TurnCancel:
		requestID = f.RequestID
		resp, err = handler.TurnCancel(ctx, *f)
	default:
		return wire.DriverResult{}, wire.ProtocolErrorf("unsupported driver execution request %T", frame)
	}
	if err != nil {
		return wire.DriverResult{}, err
	}
	if resp.RequestID != requestID {
		return wire.DriverResult{}, wire.ProtocolErrorf("driver execution response request_id %q does not match request_id %q", resp.RequestID, requestID)
	}
	return resp, nil
}

func expandFramesForWrite(frame any) ([]any, error) {
	encoded, err := wire.Encode(frame)
	if err != nil || int64(len(encoded)) <= codec.MaxFrameBytes {
		return []any{frame}, err
	}
	switch f := frame.(type) {
	case wire.InvokeResult:
		return streamInvokeResultForWrite(f)
	case *wire.InvokeResult:
		if f == nil {
			return []any{frame}, nil
		}
		return streamInvokeResultForWrite(*f)
	default:
		return []any{frame}, nil
	}
}

func streamInvokeResultForWrite(result wire.InvokeResult) ([]any, error) {
	if !result.OK || len(result.Data) == 0 {
		return []any{result}, nil
	}
	frames := make([]any, 0, 4)
	frames = append(frames, wire.InvokeResultStart{Op: wire.OpInvokeResultStart, CallID: result.CallID})
	remaining := result.Data
	seq := int64(1)
	for len(remaining) > 0 {
		chunk, rest := nextUTF8Chunk(remaining, invokeResultStreamChunkBytes)
		frames = append(frames, wire.InvokeResultDelta{Op: wire.OpInvokeResultDelta, CallID: result.CallID, Seq: seq, Data: chunk})
		remaining = rest
		seq++
	}
	frames = append(frames, wire.InvokeResultEnd{Op: wire.OpInvokeResultEnd, CallID: result.CallID, Bytes: int64(len(result.Data))})
	return frames, nil
}

func nextUTF8Chunk(data []byte, limit int) (string, []byte) {
	if limit <= 0 || len(data) <= limit {
		return string(data), nil
	}
	cut := limit
	for cut > 0 && !utf8.Valid(data[:cut]) {
		cut--
	}
	if cut <= 0 {
		cut = limit
	}
	return string(data[:cut]), data[cut:]
}

func nextBytesChunk(data []byte, limit int) ([]byte, []byte) {
	if limit <= 0 || len(data) <= limit {
		return append([]byte(nil), data...), nil
	}
	return append([]byte(nil), data[:limit]...), data[limit:]
}

func protocolErrorResponse(raw json.RawMessage, err error) (wire.InvokeResult, bool) {
	var head struct {
		Op     wire.Operation `json:"op"`
		CallID string         `json:"call_id"`
	}
	if decodeErr := json.Unmarshal(raw, &head); decodeErr != nil {
		return wire.InvokeResult{}, false
	}
	if head.Op != wire.OpInvoke || head.CallID == "" {
		return wire.InvokeResult{}, false
	}
	return wire.InvokeResult{
		Op:     wire.OpInvokeResult,
		CallID: head.CallID,
		OK:     false,
		Error: &wire.Error{
			Code:      wire.ErrorProtocolError,
			Retryable: false,
			Message:   sanitizeProtocolError(err),
		},
	}, true
}

func sanitizeProtocolError(err error) string {
	if err == nil {
		return "protocol error"
	}
	msg := err.Error()
	if strings.Contains(msg, "token") {
		return "protocol error"
	}
	return msg
}

func defaultHookReplyMode(event string) wire.HookReplyMode {
	switch event {
	case "after_message", "after_tool_call", "after_turn", "session_join", "session_end", "cancel":
		return wire.HookReplyModeNone
	case "before_message", "before_tool_call", "before_tool_result", "session_start", "before_prompt_build", "before_turn", "before_compaction":
		return wire.HookReplyModeModifying
	default:
		return wire.HookReplyModeModifying
	}
}

// RawJSON returns a copy suitable for tests and handlers that need stable data.
func RawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
