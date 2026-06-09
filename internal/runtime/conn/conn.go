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
	HookEvent(context.Context, wire.HookEvent) (wire.HookEventReply, error)
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

// Conn is a concrete RuntimeConn backed by one codec connection.
type Conn struct {
	c         *codec.Conn
	writeMu   sync.Mutex
	runtimeID string
	sink      runtimeapi.AsyncSink

	done      chan struct{}
	closeOnce sync.Once

	mu      sync.Mutex
	pending map[string]*pendingInvoke
	cancels map[string]chan error
	health  chan runtimeapi.HealthResp
	caps    chan runtimeapi.ListCapabilitiesResp
	reloads chan runtimeapi.ReloadResp
}

var _ runtimeapi.RuntimeConn = (*Conn)(nil)

// New creates a RuntimeConn and starts its response router.
func New(c *codec.Conn) *Conn {
	return NewWithSink(c, "", runtimeapi.NopAsyncSink())
}

// NewWithSink creates a RuntimeConn with a runtime-id-scoped async sink.
func NewWithSink(c *codec.Conn, runtimeID string, sink runtimeapi.AsyncSink) *Conn {
	if sink == nil {
		sink = runtimeapi.NopAsyncSink()
	}
	rc := &Conn{
		c:         c,
		runtimeID: runtimeID,
		sink:      sink,
		done:      make(chan struct{}),
		pending:   make(map[string]*pendingInvoke),
		cancels:   make(map[string]chan error),
		health:    make(chan runtimeapi.HealthResp, 1),
		caps:      make(chan runtimeapi.ListCapabilitiesResp, 1),
		reloads:   make(chan runtimeapi.ReloadResp, 1),
	}
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
	frame := wire.Invoke{Op: wire.OpInvoke, CallID: req.CallID, TenantID: req.TenantID, SessionID: req.SessionID, Target: req.Target, Tool: req.Tool, Args: req.Args, TimeoutMS: req.TimeoutMS}
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
	frame := wire.Invoke{Op: wire.OpInvoke, CallID: req.CallID, TenantID: req.TenantID, SessionID: req.SessionID, Target: req.Target, Tool: req.Tool, Args: req.Args, TimeoutMS: req.TimeoutMS}
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

func (c *Conn) SendHookEvent(ctx context.Context, req runtimeapi.HookEventReq) error {
	replyMode := req.ReplyMode
	if replyMode == "" {
		replyMode = defaultHookReplyMode(req.Event)
	}
	frame := wire.HookEvent{
		Op:        wire.OpHookEvent,
		TenantID:  req.TenantID,
		CallID:    req.CallID,
		Target:    req.Target,
		Event:     req.Event,
		ReplyMode: replyMode,
		Data:      req.Data,
		SessionID: req.SessionID,
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
		case *wire.CatalogUpdate:
			if err := c.sink.CatalogUpdated(c.runtimeID, *f); err != nil {
				c.protocolFailure(err)
				return
			}
		case *wire.HookEventReply:
			if err := c.sink.HookEventReplied(c.runtimeID, *f); err != nil {
				c.protocolFailure(err)
				return
			}
		case *wire.PluginSend:
			if err := c.sink.PluginSent(c.runtimeID, *f); err != nil {
				c.protocolFailure(err)
				return
			}
		case *wire.PluginLog:
			c.sink.PluginLogged(c.runtimeID, *f)
		case *wire.LifecycleNotice:
			if err := c.sink.LifecycleNoticed(c.runtimeID, *f); err != nil {
				c.protocolFailure(err)
				return
			}
		case *wire.HookEvent:
			c.protocolFailure(wire.ProtocolErrorf("unexpected hook_event from runtime"))
			return
		case *wire.Hello, *wire.HelloAck, *wire.Invoke, *wire.Cancel, *wire.Health, *wire.ListCapabilities, *wire.Reload:
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
		c.mu.Unlock()
		for callID, invoke := range pending {
			invoke.ch <- invokeOutcome{resp: unavailableResp(callID)}
		}
		for _, ch := range cancels {
			ch <- runtimeUnavailableError()
		}
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
						if err := write(frame); err != nil && !errors.Is(err, io.EOF) {
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
		case *wire.HookEvent:
			resp, err := handler.HookEvent(ctx, *f)
			if err != nil {
				return err
			}
			if resp.Op == "" {
				continue
			}
			if err := write(resp); err != nil {
				return err
			}
		case *wire.CatalogUpdate, *wire.HookEventReply, *wire.PluginSend, *wire.PluginLog, *wire.LifecycleNotice:
			return fmt.Errorf("unexpected runtime-originated async frame %T on server connection", frame)
		default:
			return fmt.Errorf("unsupported runtime frame %T", frame)
		}
	}
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
	case "after_message", "after_tool_call", "session_join", "session_end", "cancel":
		return wire.HookReplyModeNone
	case "before_message", "before_tool_call", "before_tool_result", "session_start", "before_prompt_build":
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
