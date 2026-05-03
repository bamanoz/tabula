package runtime

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

func TestRuntimeContractNetPipeEndToEnd(t *testing.T) {
	t.Run("handshake", func(t *testing.T) {
		client, server := newRuntimePipeFixture(t)
		defer client.Close()
		defer server.Close()

		ack, err := client.Handshake(context.Background(), wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
		if err != nil {
			t.Fatalf("handshake failed: %v", err)
		}
		if !ack.Accepted || ack.KernelID != "main" {
			t.Fatalf("unexpected hello ack: %#v", ack)
		}
	})

	t.Run("nominal invoke", func(t *testing.T) {
		client, server := newRuntimePipeFixture(t)
		defer client.Close()
		defer server.Close()
		mustHandshake(t, client)

		resp, err := client.Invoke(context.Background(), InvokeReq{CallID: "call-ok", TenantID: "default", Target: pluginTarget("fs"), Tool: "echo", Args: json.RawMessage(`{"path":"README.md"}`)})
		if err != nil {
			t.Fatalf("Invoke returned error: %v", err)
		}
		if !resp.OK || string(resp.Data) != `{"echo":{"path":"README.md"}}` {
			t.Fatalf("unexpected invoke response: %#v", resp)
		}
	})

	t.Run("error invoke", func(t *testing.T) {
		client, server := newRuntimePipeFixture(t)
		defer client.Close()
		defer server.Close()
		mustHandshake(t, client)

		resp, err := client.Invoke(context.Background(), InvokeReq{CallID: "call-missing", TenantID: "default", Target: pluginTarget("fs"), Tool: "missing"})
		if err != nil {
			t.Fatalf("Invoke returned error: %v", err)
		}
		assertWireError(t, resp, wire.ErrorToolNotFound, false)
	})

	t.Run("cancel propagation", func(t *testing.T) {
		client, server := newRuntimePipeFixture(t)
		defer client.Close()
		defer server.Close()
		mustHandshake(t, client)

		started := server.ObserveInvoke("call-long")
		done := make(chan InvokeResp, 1)
		go func() {
			resp, err := client.Invoke(context.Background(), InvokeReq{CallID: "call-long", TenantID: "default", Target: pluginTarget("fs"), Tool: "long"})
			if err != nil {
				t.Errorf("Invoke returned error: %v", err)
			}
			done <- resp
		}()

		waitFor(t, started, "server to observe long invoke")
		if err := client.Cancel(context.Background(), "call-long"); err != nil {
			t.Fatalf("Cancel returned error: %v", err)
		}
		select {
		case resp := <-done:
			assertWireError(t, resp, wire.ErrorCancelled, false)
		case <-time.After(time.Second):
			t.Fatal("timed out waiting for cancelled invoke")
		}
	})

	t.Run("health ping under load", func(t *testing.T) {
		client, server := newRuntimePipeFixture(t)
		defer client.Close()
		defer server.Close()
		mustHandshake(t, client)

		started := server.ObserveInvoke("call-load")
		done := make(chan InvokeResp, 1)
		go func() {
			resp, err := client.Invoke(context.Background(), InvokeReq{CallID: "call-load", TenantID: "default", Target: pluginTarget("fs"), Tool: "long"})
			if err != nil {
				t.Errorf("Invoke returned error: %v", err)
			}
			done <- resp
		}()

		waitFor(t, started, "server to observe load invoke")
		for i := 0; i < 10; i++ {
			health, err := client.Health(context.Background())
			if err != nil {
				t.Fatalf("Health #%d returned error: %v", i, err)
			}
			if !health.OK || health.WorkerCount != 1 {
				t.Fatalf("unexpected health #%d: %#v", i, health)
			}
		}
		if err := client.Cancel(context.Background(), "call-load"); err != nil {
			t.Fatalf("Cancel returned error: %v", err)
		}
		<-done
	})

	t.Run("list capabilities", func(t *testing.T) {
		client, server := newRuntimePipeFixture(t)
		defer client.Close()
		defer server.Close()
		mustHandshake(t, client)

		caps, err := client.ListCapabilities(context.Background())
		if err != nil {
			t.Fatalf("ListCapabilities returned error: %v", err)
		}
		if len(caps.Targets) == 0 || caps.Targets[0].Target != pluginTarget("fs") {
			t.Fatalf("unexpected capabilities: %#v", caps)
		}
	})

	t.Run("reload", func(t *testing.T) {
		client, server := newRuntimePipeFixture(t)
		defer client.Close()
		defer server.Close()
		mustHandshake(t, client)

		target := pluginTarget("fs")
		ack, err := client.Reload(context.Background(), ReloadReq{Target: &target})
		if err != nil {
			t.Fatalf("Reload returned error: %v", err)
		}
		if len(ack.EvictedTargets) != 1 || ack.EvictedTargets[0] != target {
			t.Fatalf("unexpected reload ack: %#v", ack)
		}
	})

	t.Run("disconnect", func(t *testing.T) {
		client, server := newRuntimePipeFixture(t)
		defer client.Close()
		defer server.Close()
		mustHandshake(t, client)

		resp, err := client.Invoke(context.Background(), InvokeReq{CallID: "call-disconnect", TenantID: "default", Target: pluginTarget("fs"), Tool: "disconnect"})
		if err != nil {
			t.Fatalf("Invoke returned error: %v", err)
		}
		assertWireError(t, resp, wire.ErrorRuntimeUnavailable, true)
	})

	t.Run("protocol error missing tenant id", func(t *testing.T) {
		client, server := newRuntimePipeFixture(t)
		defer client.Close()
		defer server.Close()
		mustHandshake(t, client)

		resp, err := client.RawInvoke(context.Background(), []byte(`{"op":"invoke","call_id":"call-bad","target":{"kind":"plugin","id":"fs"},"tool":"echo"}`), "call-bad")
		if err != nil {
			t.Fatalf("RawInvoke returned error: %v", err)
		}
		assertWireError(t, resp, wire.ErrorProtocolError, false)
	})

	t.Run("concurrent invokes", func(t *testing.T) {
		client, server := newRuntimePipeFixture(t)
		defer client.Close()
		defer server.Close()
		mustHandshake(t, client)

		const count = 50
		var wg sync.WaitGroup
		for i := 0; i < count; i++ {
			i := i
			wg.Add(1)
			go func() {
				defer wg.Done()
				callID := fmt.Sprintf("call-parallel-%02d", i)
				resp, err := client.Invoke(context.Background(), InvokeReq{CallID: callID, TenantID: "default", Target: pluginTarget("fs"), Tool: "parallel"})
				if err != nil {
					t.Errorf("Invoke %s returned error: %v", callID, err)
					return
				}
				if !resp.OK || resp.CallID != callID || string(resp.Data) != fmt.Sprintf(`{"call_id":"%s"}`, callID) {
					t.Errorf("unexpected invoke response for %s: %#v", callID, resp)
				}
			}()
		}
		wg.Wait()
	})

	t.Run("worker frame symmetry", func(t *testing.T) {
		var left, right bytes.Buffer
		init := workerwire.WorkerInit{Op: workerwire.OpInit, KernelID: "main", TenantID: "default", TargetID: "fs", Env: map[string]string{"TABULA_TENANT_ID": "default"}}
		if err := workerwire.WriteFrame(&left, &init); err != nil {
			t.Fatalf("WriteFrame init: %v", err)
		}
		_, frame, err := workerwire.ReadFrame(bufio.NewReader(&left))
		if err != nil {
			t.Fatalf("ReadFrame init: %v", err)
		}
		gotInit, ok := frame.(*workerwire.WorkerInit)
		if !ok {
			t.Fatalf("expected WorkerInit, got %T", frame)
		}
		if gotInit.TenantID != init.TenantID || gotInit.TargetID != init.TargetID {
			t.Fatalf("unexpected worker init: %#v", gotInit)
		}

		result := workerwire.WorkerResult{Op: workerwire.OpResult, CallID: "call-worker", OK: false, Error: &workerwire.WorkerErrorBody{Code: "plugin_local", Message: "worker-local errors are not wire errors"}}
		if err := workerwire.WriteFrame(&right, &result); err != nil {
			t.Fatalf("WriteFrame result: %v", err)
		}
		_, frame, err = workerwire.ReadFrame(bufio.NewReader(&right))
		if err != nil {
			t.Fatalf("ReadFrame result: %v", err)
		}
		gotResult, ok := frame.(*workerwire.WorkerResult)
		if !ok {
			t.Fatalf("expected WorkerResult, got %T", frame)
		}
		if gotResult.Error == nil || wire.IsErrorCode(wire.ErrorCode(gotResult.Error.Code)) {
			t.Fatalf("worker-local error leaked into Runtime API wire namespace: %#v", gotResult)
		}
	})
}

type runtimePipeClient struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder

	writeMu sync.Mutex
	mu      sync.Mutex
	pending map[string]chan InvokeResp
	cancels map[string]chan error
	health  chan HealthResp
	caps    chan ListCapabilitiesResp
	reloads chan ReloadResp
	closed  chan struct{}
}

var _ RuntimeConn = (*runtimePipeClient)(nil)

func newRuntimePipeFixture(t *testing.T) (*runtimePipeClient, *fakeRuntimeServer) {
	t.Helper()
	t.Cleanup(func() {
		if t.Failed() {
			t.Log("runtime contract fixture failed; inspect the named subtest and decoded frame assertions above")
		}
	})
	clientConn, serverConn := net.Pipe()
	client := &runtimePipeClient{
		conn:    clientConn,
		enc:     json.NewEncoder(clientConn),
		dec:     json.NewDecoder(clientConn),
		pending: make(map[string]chan InvokeResp),
		cancels: make(map[string]chan error),
		health:  make(chan HealthResp, 16),
		caps:    make(chan ListCapabilitiesResp, 1),
		reloads: make(chan ReloadResp, 1),
		closed:  make(chan struct{}),
	}
	server := newFakeRuntimeServer(serverConn)
	go server.Serve()
	return client, server
}

func (c *runtimePipeClient) Handshake(ctx context.Context, hello wire.Hello) (wire.HelloAck, error) {
	if err := c.writeJSON(hello); err != nil {
		return wire.HelloAck{}, err
	}
	ackCh := make(chan wire.HelloAck, 1)
	errCh := make(chan error, 1)
	go func() {
		var raw json.RawMessage
		if err := c.dec.Decode(&raw); err != nil {
			errCh <- err
			return
		}
		_, frame, err := wire.Decode(raw)
		if err != nil {
			errCh <- err
			return
		}
		ack, ok := frame.(*wire.HelloAck)
		if !ok {
			errCh <- fmt.Errorf("expected hello_ack, got %T", frame)
			return
		}
		ackCh <- *ack
	}()
	select {
	case ack := <-ackCh:
		go c.readLoop()
		return ack, nil
	case err := <-errCh:
		return wire.HelloAck{}, err
	case <-ctx.Done():
		return wire.HelloAck{}, ctx.Err()
	}
}

func (c *runtimePipeClient) Invoke(ctx context.Context, req InvokeReq) (InvokeResp, error) {
	ch := c.registerPending(req.CallID)
	frame := wire.Invoke{Op: wire.OpInvoke, CallID: req.CallID, TenantID: req.TenantID, Target: req.Target, Tool: req.Tool, Args: req.Args, TimeoutMS: req.TimeoutMS}
	if err := c.writeFrame(frame); err != nil {
		c.unregisterPending(req.CallID)
		return InvokeResp{}, err
	}
	return c.waitInvoke(ctx, req.CallID, ch)
}

func (c *runtimePipeClient) RawInvoke(ctx context.Context, raw []byte, callID string) (InvokeResp, error) {
	ch := c.registerPending(callID)
	if err := c.writeRaw(raw); err != nil {
		c.unregisterPending(callID)
		return InvokeResp{}, err
	}
	return c.waitInvoke(ctx, callID, ch)
}

func (c *runtimePipeClient) Cancel(ctx context.Context, callID string) error {
	ack := make(chan error, 1)
	c.mu.Lock()
	c.cancels[callID] = ack
	c.mu.Unlock()
	if err := c.writeFrame(wire.Cancel{Op: wire.OpCancel, CallID: callID}); err != nil {
		return err
	}
	select {
	case err := <-ack:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (c *runtimePipeClient) Health(ctx context.Context) (HealthResp, error) {
	if err := c.writeFrame(wire.Health{Op: wire.OpHealth}); err != nil {
		return HealthResp{}, err
	}
	select {
	case resp := <-c.health:
		return resp, nil
	case <-ctx.Done():
		return HealthResp{}, ctx.Err()
	}
}

func (c *runtimePipeClient) ListCapabilities(ctx context.Context) (ListCapabilitiesResp, error) {
	if err := c.writeFrame(wire.ListCapabilities{Op: wire.OpListCapabilities}); err != nil {
		return ListCapabilitiesResp{}, err
	}
	select {
	case resp := <-c.caps:
		return resp, nil
	case <-ctx.Done():
		return ListCapabilitiesResp{}, ctx.Err()
	}
}

func (c *runtimePipeClient) Reload(ctx context.Context, req ReloadReq) (ReloadResp, error) {
	if err := c.writeFrame(wire.Reload{Op: wire.OpReload, Target: req.Target}); err != nil {
		return ReloadResp{}, err
	}
	select {
	case resp := <-c.reloads:
		return resp, nil
	case <-ctx.Done():
		return ReloadResp{}, ctx.Err()
	}
}

func (c *runtimePipeClient) SendHookEvent(ctx context.Context, req HookEventReq) error {
	return c.writeFrame(wire.HookEvent{
		Op:        wire.OpHookEvent,
		CallID:    req.CallID,
		Target:    req.Target,
		Event:     req.Event,
		ReplyMode: req.ReplyMode,
		Data:      req.Data,
		SessionID: req.SessionID,
	})
}

func (c *runtimePipeClient) Close() error {
	return c.conn.Close()
}

func (c *runtimePipeClient) readLoop() {
	for {
		var raw json.RawMessage
		if err := c.dec.Decode(&raw); err != nil {
			c.failPending(unavailableResp)
			close(c.closed)
			return
		}
		_, frame, err := wire.Decode(raw)
		if err != nil {
			continue
		}
		switch f := frame.(type) {
		case *wire.InvokeResult:
			resp := InvokeResp{CallID: f.CallID, OK: f.OK, Data: f.Data, Error: f.Error}
			c.deliverInvoke(f.CallID, resp)
		case *wire.CancelAck:
			c.deliverCancel(f.CallID, nil)
		case *wire.HealthResp:
			c.health <- *f
		case *wire.ListCapabilitiesResp:
			c.caps <- *f
		case *wire.ReloadAck:
			c.reloads <- *f
		}
	}
}

func (c *runtimePipeClient) registerPending(callID string) chan InvokeResp {
	ch := make(chan InvokeResp, 1)
	c.mu.Lock()
	c.pending[callID] = ch
	c.mu.Unlock()
	return ch
}

func (c *runtimePipeClient) unregisterPending(callID string) {
	c.mu.Lock()
	delete(c.pending, callID)
	c.mu.Unlock()
}

func (c *runtimePipeClient) waitInvoke(ctx context.Context, callID string, ch chan InvokeResp) (InvokeResp, error) {
	select {
	case resp := <-ch:
		return resp, nil
	case <-ctx.Done():
		c.unregisterPending(callID)
		return InvokeResp{}, ctx.Err()
	}
}

func (c *runtimePipeClient) deliverInvoke(callID string, resp InvokeResp) {
	c.mu.Lock()
	ch := c.pending[callID]
	delete(c.pending, callID)
	c.mu.Unlock()
	if ch != nil {
		ch <- resp
	}
}

func (c *runtimePipeClient) deliverCancel(callID string, err error) {
	c.mu.Lock()
	ch := c.cancels[callID]
	delete(c.cancels, callID)
	c.mu.Unlock()
	if ch != nil {
		ch <- err
	}
}

func (c *runtimePipeClient) failPending(resp func(string) InvokeResp) {
	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[string]chan InvokeResp)
	c.mu.Unlock()
	for callID, ch := range pending {
		ch <- resp(callID)
	}
}

func (c *runtimePipeClient) writeFrame(frame any) error {
	data, err := wire.Encode(frame)
	if err != nil {
		return err
	}
	return c.writeRaw(data)
}

func (c *runtimePipeClient) writeJSON(frame any) error {
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	return c.writeRaw(data)
}

func (c *runtimePipeClient) writeRaw(data []byte) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	return c.enc.Encode(json.RawMessage(data))
}

type fakeRuntimeServer struct {
	conn net.Conn
	enc  *json.Encoder
	dec  *json.Decoder

	writeMu  sync.Mutex
	observe  sync.Mutex
	observed map[string]chan struct{}
}

func newFakeRuntimeServer(conn net.Conn) *fakeRuntimeServer {
	return &fakeRuntimeServer{conn: conn, enc: json.NewEncoder(conn), dec: json.NewDecoder(conn), observed: make(map[string]chan struct{})}
}

func (s *fakeRuntimeServer) Serve() {
	for {
		var raw json.RawMessage
		if err := s.dec.Decode(&raw); err != nil {
			return
		}
		env, frame, err := wire.Decode(raw)
		if err != nil {
			callID := callIDFromRaw(raw)
			if callID != "" {
				_ = s.writeFrame(wire.InvokeResult{Op: wire.OpInvokeResult, CallID: callID, OK: false, Error: &wire.Error{Code: wire.ErrorProtocolError, Retryable: false, Message: err.Error()}})
			}
			continue
		}
		switch f := frame.(type) {
		case *wire.Hello:
			_ = s.writeFrame(wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"})
		case *wire.Invoke:
			s.markObserved(f.CallID)
			s.handleInvoke(f)
		case *wire.Cancel:
			_ = s.writeFrame(wire.CancelAck{Op: wire.OpCancelAck, CallID: f.CallID})
			_ = s.writeFrame(wire.InvokeResult{Op: wire.OpInvokeResult, CallID: f.CallID, OK: false, Error: &wire.Error{Code: wire.ErrorCancelled, Retryable: false}})
		case *wire.Health:
			_ = s.writeFrame(wire.HealthResp{Op: wire.OpHealthResp, OK: true, UptimeMS: 1, WorkerCount: 1})
		case *wire.ListCapabilities:
			_ = s.writeFrame(wire.ListCapabilitiesResp{Op: wire.OpListCapabilitiesResp, Targets: []wire.Capability{{Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "echo"}, {Name: "missing"}, {Name: "long"}, {Name: "parallel"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}})
		case *wire.Reload:
			var evicted []wire.Target
			if f.Target != nil {
				evicted = []wire.Target{*f.Target}
			}
			_ = s.writeFrame(wire.ReloadAck{Op: wire.OpReloadAck, EvictedTargets: evicted})
		default:
			_ = env
		}
	}
}

func (s *fakeRuntimeServer) handleInvoke(invoke *wire.Invoke) {
	switch invoke.Tool {
	case "echo":
		_ = s.writeFrame(wire.InvokeResult{Op: wire.OpInvokeResult, CallID: invoke.CallID, OK: true, Data: json.RawMessage(fmt.Sprintf(`{"echo":%s}`, jsonOrNull(invoke.Args)))})
	case "missing":
		_ = s.writeFrame(wire.InvokeResult{Op: wire.OpInvokeResult, CallID: invoke.CallID, OK: false, Error: &wire.Error{Code: wire.ErrorToolNotFound, Retryable: false}})
	case "long":
		// Leave the call in flight. A later Cancel frame will produce CancelAck and
		// the terminal cancelled InvokeResult while the read loop continues to serve
		// health pings and other ops.
	case "disconnect":
		_ = s.conn.Close()
	case "parallel":
		_ = s.writeFrame(wire.InvokeResult{Op: wire.OpInvokeResult, CallID: invoke.CallID, OK: true, Data: json.RawMessage(fmt.Sprintf(`{"call_id":"%s"}`, invoke.CallID))})
	default:
		_ = s.writeFrame(wire.InvokeResult{Op: wire.OpInvokeResult, CallID: invoke.CallID, OK: false, Error: &wire.Error{Code: wire.ErrorToolNotFound, Retryable: false}})
	}
}

func (s *fakeRuntimeServer) ObserveInvoke(callID string) <-chan struct{} {
	s.observe.Lock()
	defer s.observe.Unlock()
	if ch, ok := s.observed[callID]; ok {
		return ch
	}
	ch := make(chan struct{})
	s.observed[callID] = ch
	return ch
}

func (s *fakeRuntimeServer) markObserved(callID string) {
	s.observe.Lock()
	defer s.observe.Unlock()
	ch, ok := s.observed[callID]
	if !ok {
		ch = make(chan struct{})
		s.observed[callID] = ch
	}
	select {
	case <-ch:
	default:
		close(ch)
	}
}

func (s *fakeRuntimeServer) Close() error { return s.conn.Close() }

func (s *fakeRuntimeServer) writeFrame(frame any) error {
	data, err := wire.Encode(frame)
	if err != nil {
		return err
	}
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.enc.Encode(json.RawMessage(data))
}

func mustHandshake(t *testing.T, client *runtimePipeClient) {
	t.Helper()
	ack, err := client.Handshake(context.Background(), wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("handshake failed: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("handshake rejected: %#v", ack)
	}
}

func assertWireError(t *testing.T, resp InvokeResp, code wire.ErrorCode, retryable bool) {
	t.Helper()
	if resp.OK || resp.Error == nil || resp.Error.Code != code || resp.Error.Retryable != retryable {
		t.Fatalf("expected error %s retryable=%t, got %#v", code, retryable, resp)
	}
}

func waitFor(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func pluginTarget(id string) wire.Target {
	return wire.Target{Kind: wire.TargetKindPlugin, ID: id}
}

func unavailableResp(callID string) InvokeResp {
	return InvokeResp{CallID: callID, OK: false, Error: &wire.Error{Code: wire.ErrorRuntimeUnavailable, Retryable: true}}
}

func callIDFromRaw(raw json.RawMessage) string {
	var head struct {
		CallID string `json:"call_id"`
	}
	_ = json.Unmarshal(raw, &head)
	return head.CallID
}

func jsonOrNull(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("null")
	}
	return raw
}

func TestRuntimeContractRejectsStaleWireErrorAliases(t *testing.T) {
	for _, code := range []wire.ErrorCode{"tenant_denied", "target_not_authorized", "fs_outside_root", "exec_denied"} {
		_, _, err := wire.Decode([]byte(fmt.Sprintf(`{"op":"invoke_result","call_id":"call-1","ok":false,"error":{"code":%q,"retryable":false}}`, code)))
		if err == nil {
			t.Fatalf("expected stale/plugin-local wire error %q to be rejected", code)
		}
		var protocolErr wire.ProtocolError
		if !errors.As(err, &protocolErr) {
			t.Fatalf("expected ProtocolError for %q, got %T %v", code, err, err)
		}
	}
}
