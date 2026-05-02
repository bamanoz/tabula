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
}

// Conn is a concrete RuntimeConn backed by one codec connection.
type Conn struct {
	c       *codec.Conn
	writeMu sync.Mutex

	done      chan struct{}
	closeOnce sync.Once

	mu      sync.Mutex
	pending map[string]chan runtimeapi.InvokeResp
	cancels map[string]chan error
	health  chan runtimeapi.HealthResp
	caps    chan runtimeapi.ListCapabilitiesResp
	reloads chan runtimeapi.ReloadResp
}

var _ runtimeapi.RuntimeConn = (*Conn)(nil)

// New creates a RuntimeConn and starts its response router.
func New(c *codec.Conn) *Conn {
	rc := &Conn{
		c:       c,
		done:    make(chan struct{}),
		pending: make(map[string]chan runtimeapi.InvokeResp),
		cancels: make(map[string]chan error),
		health:  make(chan runtimeapi.HealthResp, 1),
		caps:    make(chan runtimeapi.ListCapabilitiesResp, 1),
		reloads: make(chan runtimeapi.ReloadResp, 1),
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
	ch, err := c.registerPending(req.CallID)
	if err != nil {
		return runtimeapi.InvokeResp{}, err
	}
	frame := wire.Invoke{Op: wire.OpInvoke, CallID: req.CallID, TenantID: req.TenantID, Target: req.Target, Tool: req.Tool, Args: req.Args, TimeoutMS: req.TimeoutMS}
	if err := c.write(ctx, frame); err != nil {
		c.unregisterPending(req.CallID)
		return runtimeapi.InvokeResp{}, err
	}
	select {
	case resp := <-ch:
		return resp, nil
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
	if err := c.write(ctx, wire.Reload{Op: wire.OpReload, Target: req.Target}); err != nil {
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
		_, frame, err := c.c.Read(context.Background())
		if err != nil {
			c.closeWithUnavailable()
			return
		}
		switch f := frame.(type) {
		case *wire.InvokeResult:
			c.deliverInvoke(f.CallID, runtimeapi.InvokeResp{CallID: f.CallID, OK: f.OK, Data: f.Data, Error: f.Error})
		case *wire.CancelAck:
			c.deliverCancel(f.CallID, nil)
		case *wire.HealthResp:
			replaceLatest(c.health, *f)
		case *wire.ListCapabilitiesResp:
			replaceLatest(c.caps, *f)
		case *wire.ReloadAck:
			replaceLatest(c.reloads, *f)
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

func (c *Conn) registerPending(callID string) (chan runtimeapi.InvokeResp, error) {
	if callID == "" {
		return nil, wire.ProtocolErrorf("call_id is required")
	}
	ch := make(chan runtimeapi.InvokeResp, 1)
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
	c.pending[callID] = ch
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
	ch := c.pending[callID]
	delete(c.pending, callID)
	c.mu.Unlock()
	if ch != nil {
		ch <- resp
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
		c.pending = make(map[string]chan runtimeapi.InvokeResp)
		cancels := c.cancels
		c.cancels = make(map[string]chan error)
		c.mu.Unlock()
		for callID, ch := range pending {
			ch <- unavailableResp(callID)
		}
		for _, ch := range cancels {
			ch <- runtimeUnavailableError()
		}
		close(c.done)
	})
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
	defer c.CloseNow()
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
	defer c.CloseNow()
	return ServeAfterHandshake(ctx, c, handler)
}

// ServeAfterHandshake handles post-Hello Runtime API frames until the
// connection closes. It treats an unexpected second Hello as protocol-invalid.
func ServeAfterHandshake(ctx context.Context, c *codec.Conn, handler Handler) error {
	var writeMu sync.Mutex
	write := func(frame any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return c.Write(ctx, frame)
	}
	for {
		raw, err := c.ReadRaw(ctx)
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
				if err := write(resp); err != nil && !errors.Is(err, io.EOF) {
					// The read loop returns connection errors. This goroutine must not
					// log frame content because Invoke args may contain user data.
				}
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
		default:
			return fmt.Errorf("unsupported runtime frame %T", frame)
		}
	}
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

// RawJSON returns a copy suitable for tests and handlers that need stable data.
func RawJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}
