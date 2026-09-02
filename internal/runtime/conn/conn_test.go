package conn

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestCodecNetPipeRoundTripEveryOp(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()
	defer func() { _ = server.CloseNow() }()

	frames := []any{
		wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1", Capabilities: []wire.Capability{{Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}},
		wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"},
		wire.Invoke{Op: wire.OpInvoke, CallID: "call-1", TenantID: "default", Target: pluginTarget("fs"), Tool: "echo", Args: json.RawMessage(`{"x":1}`)},
		wire.InvokeResult{Op: wire.OpInvokeResult, CallID: "call-1", OK: true, Data: json.RawMessage(`{"ok":true}`)},
		wire.InvokeResultStart{Op: wire.OpInvokeResultStart, CallID: "call-2"},
		wire.InvokeResultDelta{Op: wire.OpInvokeResultDelta, CallID: "call-2", Seq: 1, Data: `{"ok":true}`},
		wire.InvokeResultEnd{Op: wire.OpInvokeResultEnd, CallID: "call-2", Bytes: 11},
		wire.Cancel{Op: wire.OpCancel, CallID: "call-1"},
		wire.CancelAck{Op: wire.OpCancelAck, CallID: "call-1"},
		wire.Health{Op: wire.OpHealth},
		wire.HealthResp{Op: wire.OpHealthResp, OK: true, WorkerCount: 1},
		wire.ListCapabilities{Op: wire.OpListCapabilities},
		wire.ListCapabilitiesResp{Op: wire.OpListCapabilitiesResp, Targets: []wire.Capability{{Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}},
		wire.Reload{Op: wire.OpReload, Target: ptr(pluginTarget("fs"))},
		wire.ReloadAck{Op: wire.OpReloadAck, EvictedTargets: []wire.Target{pluginTarget("fs")}},
		wire.PrepareTenant{Op: wire.OpPrepareTenant, RequestID: "prepare-1", TenantID: "default"},
		wire.PrepareTenantAck{Op: wire.OpPrepareTenantAck, RequestID: "prepare-1", Capabilities: []wire.Capability{{Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}},
		wire.HookEvent{Op: wire.OpHookEvent, CallID: "hook-1", Target: pluginTarget("fs"), Event: "before_tool_call", ReplyMode: wire.HookReplyModeModifying, Data: json.RawMessage(`{"tool":"echo"}`)},
		wire.HookEventReply{Op: wire.OpHookEventReply, CallID: "hook-1", Action: wire.HookActionOK},
		wire.CatalogUpdate{Op: wire.OpCatalogUpdate, Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "read"}}, Revision: 2, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker},
		wire.PluginSend{Op: wire.OpPluginSend, Target: pluginTarget("fs"), Channel: "bus", Type: "event", Payload: json.RawMessage(`{"ok":true}`)},
		wire.PluginLog{Op: wire.OpPluginLog, Target: pluginTarget("fs"), Level: "info", Message: "started"},
		wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: pluginTarget("fs"), State: wire.LifecycleStateReady, PID: 123},
	}

	for _, frame := range frames {
		writeErr := make(chan error, 1)
		go func(frame any) {
			writeErr <- client.Write(context.Background(), frame)
		}(frame)
		_, got, err := server.Read(context.Background())
		if err != nil {
			t.Fatalf("Read(%T): %v", frame, err)
		}
		if err := <-writeErr; err != nil {
			t.Fatalf("Write(%T): %v", frame, err)
		}
		if !reflect.DeepEqual(frame, deref(got)) {
			t.Fatalf("round trip mismatch for %T\nwant: %#v\n got: %#v", frame, frame, got)
		}
	}
}

func TestRuntimeConnConcurrentInvokesAndDisconnect(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	go func() {
		_ = ServeAuthenticated(context.Background(), server, testHandler{})
	}()

	ack, err := Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted || ack.KernelID != "main" {
		t.Fatalf("unexpected ack: %#v", ack)
	}
	rc := New(client)

	const count = 50
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			callID := fmt.Sprintf("call-%02d", i)
			resp, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: callID, TenantID: "default", Target: pluginTarget("fs"), Tool: "parallel"})
			if err != nil {
				t.Errorf("Invoke %s: %v", callID, err)
				return
			}
			if !resp.OK || resp.CallID != callID || string(resp.Data) != fmt.Sprintf(`{"call_id":"%s"}`, callID) {
				t.Errorf("unexpected response for %s: %#v", callID, resp)
			}
		}()
	}
	wg.Wait()

	started := make(chan struct{})
	done := make(chan runtimeapi.InvokeResp, 1)
	go func() {
		close(started)
		resp, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-disconnect", TenantID: "default", Target: pluginTarget("fs"), Tool: "long"})
		if err != nil {
			t.Errorf("disconnect invoke returned error: %v", err)
		}
		done <- resp
	}()
	<-started
	time.Sleep(10 * time.Millisecond)
	_ = server.CloseNow()
	select {
	case resp := <-done:
		if resp.OK || resp.Error == nil || resp.Error.Code != wire.ErrorRuntimeUnavailable || !resp.Error.Retryable {
			t.Fatalf("expected runtime_unavailable retryable, got %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for disconnect fail-fast")
	}
}

func TestRuntimeConnContinuesAfterAsyncSinkError(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	go func() {
		_ = ServeAuthenticated(context.Background(), server, testHandler{})
	}()

	ack, err := Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("handshake rejected: %#v", ack)
	}
	rc := NewWithSink(client, "local", &failingAsyncSink{})

	if err := server.Write(context.Background(), wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: pluginTarget("bad"), State: wire.LifecycleStateCrashed}); err != nil {
		t.Fatalf("write lifecycle notice: %v", err)
	}
	time.Sleep(10 * time.Millisecond)

	got, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-after-async-error", TenantID: "default", Target: pluginTarget("fs"), Tool: "parallel"})
	if err != nil {
		t.Fatalf("Invoke after async sink error: %v", err)
	}
	if !got.OK || got.CallID != "call-after-async-error" {
		t.Fatalf("connection did not remain usable after async sink error: %#v", got)
	}
}

func TestServeMapsMalformedInvokeToProtocolError(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	go func() {
		_ = ServeAuthenticated(context.Background(), server, testHandler{})
	}()

	ack, err := Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("handshake rejected: %#v", ack)
	}

	writeRawFrame(t, client.WebSocket(), `{"op":"invoke","call_id":"call-bad","target":{"kind":"plugin","id":"fs"},"tool":"echo"}`)
	_, frame, err := client.Read(context.Background())
	if err != nil {
		t.Fatalf("Read protocol error response: %v", err)
	}
	resp, ok := frame.(*wire.InvokeResult)
	if !ok {
		t.Fatalf("expected InvokeResult, got %T", frame)
	}
	if resp.CallID != "call-bad" || resp.OK || resp.Error == nil || resp.Error.Code != wire.ErrorProtocolError || resp.Error.Retryable {
		t.Fatalf("unexpected protocol error response: %#v", resp)
	}

	rc := New(client)
	got, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-after-bad", TenantID: "default", Target: pluginTarget("fs"), Tool: "parallel"})
	if err != nil {
		t.Fatalf("Invoke after malformed frame: %v", err)
	}
	if !got.OK || got.CallID != "call-after-bad" {
		t.Fatalf("connection did not remain usable after protocol error: %#v", got)
	}
}

func TestServeRejectsMalformedNonInvokeFrame(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	serveDone := make(chan error, 1)
	go func() {
		serveDone <- Serve(context.Background(), server, testHandler{})
	}()

	writeRawFrame(t, client.WebSocket(), `{"op":"healthz","call_id":"call-health"}`)
	select {
	case err := <-serveDone:
		if err == nil {
			t.Fatal("expected malformed non-invoke frame to stop server connection")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for server to reject malformed non-invoke frame")
	}
}

func TestRuntimeConnRoutesAsyncPluginControlFramesToSink(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	go func() {
		_ = ServeAuthenticated(context.Background(), server, testHandler{})
	}()

	ack, err := Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("handshake rejected: %#v", ack)
	}

	sink := &recordingAsyncSink{}
	rc := NewWithSink(client, "local", sink)
	defer rc.Close()

	for _, frame := range []any{
		wire.CatalogUpdate{Op: wire.OpCatalogUpdate, Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "echo"}}, Revision: 3, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker},
		wire.HookEventReply{Op: wire.OpHookEventReply, CallID: "hook-1", Action: wire.HookActionRewrite, Data: json.RawMessage(`{"tool":"echo"}`)},
		wire.PluginSend{Op: wire.OpPluginSend, Target: pluginTarget("fs"), Channel: "bus", Type: "notify", Payload: json.RawMessage(`{"x":1}`)},
		wire.PluginLog{Op: wire.OpPluginLog, Target: pluginTarget("fs"), Level: "info", Message: "ready"},
		wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: pluginTarget("fs"), State: wire.LifecycleStateReady, PID: 77},
	} {
		if err := server.Write(context.Background(), frame); err != nil {
			t.Fatalf("Write(%T): %v", frame, err)
		}
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		ready := sink.catalogRevision == 3 && sink.hookReply.CallID == "hook-1" && sink.send.Type == "notify" && sink.log.Message == "ready" && sink.notice.PID == 77
		sink.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("async sink did not receive all frames: %+v", sink)
}

func TestServeStreamsOversizedInvokeResultAndKeepsConnectionUsable(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	go func() {
		_ = ServeAuthenticated(context.Background(), server, testHandler{})
	}()

	ack, err := Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("handshake rejected: %#v", ack)
	}
	rc := New(client)

	got, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-huge", TenantID: "default", Target: pluginTarget("fs"), Tool: "huge"})
	if err != nil {
		t.Fatalf("Invoke huge: %v", err)
	}
	if !got.OK {
		t.Fatalf("expected OK for huge invoke, got %#v", got)
	}
	var text string
	if err := json.Unmarshal(got.Data, &text); err != nil {
		t.Fatalf("expected streamed string payload, got %s: %v", string(got.Data), err)
	}
	want := "prefix\n" + strings.Repeat("x", int(codec.MaxFrameBytes)+4096)
	if text != want {
		t.Fatalf("unexpected streamed payload length/content: got=%d want=%d", len(text), len(want))
	}
	if len(got.Data) <= int(codec.MaxFrameBytes) {
		t.Fatalf("expected reconstructed payload to exceed frame limit after streaming, got %d", len(got.Data))
	}

	after, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-after-huge", TenantID: "default", Target: pluginTarget("fs"), Tool: "parallel"})
	if err != nil {
		t.Fatalf("Invoke after huge: %v", err)
	}
	if !after.OK || after.CallID != "call-after-huge" {
		t.Fatalf("connection unusable after huge invoke: %#v", after)
	}
}

func TestRuntimeConnProtocolFailureOnInvokeResultDeltaBeforeStart(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	done := make(chan struct{})
	resultCh := make(chan runtimeapi.InvokeResp, 1)
	go func() {
		_, frame, err := server.Read(context.Background())
		if err != nil {
			return
		}
		hello, ok := frame.(*wire.Hello)
		if !ok {
			return
		}
		_ = server.Write(context.Background(), wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: hello.RuntimeID})
		_, frame, err = server.Read(context.Background())
		if err != nil {
			return
		}
		invoke, ok := frame.(*wire.Invoke)
		if !ok {
			return
		}
		_ = server.Write(context.Background(), wire.InvokeResultDelta{Op: wire.OpInvokeResultDelta, CallID: invoke.CallID, Seq: 1, Data: "{}"})
		close(done)
	}()

	ack, err := Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("handshake rejected: %#v", ack)
	}
	rc := New(client)
	go func() {
		resp, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "bad-call", TenantID: "default", Target: pluginTarget("fs"), Tool: "parallel"})
		if err == nil {
			resultCh <- resp
		}
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("server did not send malformed invoke result delta")
	}

	select {
	case <-rc.Done():
	case <-time.After(time.Second):
		t.Fatal("expected protocol failure to close runtime connection")
	}

	select {
	case resp := <-resultCh:
		if resp.Error == nil || resp.Error.Code != wire.ErrorRuntimeUnavailable {
			t.Fatalf("expected bad invoke to fail with runtime_unavailable, got %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("invoke did not finish after protocol failure")
	}

	after, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-after-bad-stream", TenantID: "default", Target: pluginTarget("fs"), Tool: "parallel"})
	if err == nil {
		if after.Error == nil || after.Error.Code != wire.ErrorRuntimeUnavailable {
			t.Fatalf("expected runtime_unavailable after protocol failure, got %#v", after)
		}
		return
	}
	if !strings.Contains(err.Error(), "runtime unavailable") {
		t.Fatalf("expected runtime unavailable error after protocol failure, got %v", err)
	}
}

func TestRuntimeConnInvokeStreamStartErrorRemovesPending(t *testing.T) {
	wantErr := errors.New("sink start failed")
	rc := &Conn{pending: make(map[string]*pendingInvoke), cancels: make(map[string]chan error), done: make(chan struct{})}
	ch, err := rc.registerPending("call-start-fail", failingStartSink{err: wantErr})
	if err != nil {
		t.Fatalf("registerPending: %v", err)
	}
	if err := rc.startInvokeResultStream(wire.InvokeResultStart{Op: wire.OpInvokeResultStart, CallID: "call-start-fail"}); err != nil {
		t.Fatalf("startInvokeResultStream: %v", err)
	}
	select {
	case outcome := <-ch:
		if !errors.Is(outcome.err, wantErr) {
			t.Fatalf("expected sink error, got %+v", outcome)
		}
	case <-time.After(time.Second):
		t.Fatal("start sink failure did not finish invoke")
	}
	rc.mu.Lock()
	_, stillPending := rc.pending["call-start-fail"]
	rc.mu.Unlock()
	if stillPending {
		t.Fatal("pending invoke survived sink start failure")
	}
	if err := rc.appendInvokeResultStream(wire.InvokeResultDelta{Op: wire.OpInvokeResultDelta, CallID: "call-start-fail", Seq: 1, Data: `"late"`}); err != nil {
		t.Fatalf("late delta for failed pending should be ignored: %v", err)
	}
}

func TestRuntimeConnInvokeStreamValidatesEndBytesWithSink(t *testing.T) {
	rc := &Conn{pending: make(map[string]*pendingInvoke), cancels: make(map[string]chan error), done: make(chan struct{})}
	if _, err := rc.registerPending("call-byte-mismatch", countingStreamSink{}); err != nil {
		t.Fatalf("registerPending: %v", err)
	}
	if err := rc.startInvokeResultStream(wire.InvokeResultStart{Op: wire.OpInvokeResultStart, CallID: "call-byte-mismatch"}); err != nil {
		t.Fatalf("startInvokeResultStream: %v", err)
	}
	if err := rc.appendInvokeResultStream(wire.InvokeResultDelta{Op: wire.OpInvokeResultDelta, CallID: "call-byte-mismatch", Seq: 1, Data: `"abc"`}); err != nil {
		t.Fatalf("appendInvokeResultStream: %v", err)
	}
	if err := rc.endInvokeResultStream(wire.InvokeResultEnd{Op: wire.OpInvokeResultEnd, CallID: "call-byte-mismatch", Bytes: 99}); err == nil || !strings.Contains(err.Error(), "declared 99 bytes") {
		t.Fatalf("expected byte mismatch protocol error, got %v", err)
	}
}

func TestServeWritesHandlerAsyncFramesToClientSink(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	h := asyncHandler{frames: make(chan any, 8)}
	go func() {
		_ = ServeAuthenticated(context.Background(), server, h)
	}()

	ack, err := Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("handshake rejected: %#v", ack)
	}

	sink := &recordingAsyncSink{}
	rc := NewWithSink(client, "local", sink)
	defer rc.Close()

	h.frames <- wire.CatalogUpdate{Op: wire.OpCatalogUpdate, Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "echo"}}, Revision: 4, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}
	h.frames <- wire.PluginLog{Op: wire.OpPluginLog, Target: pluginTarget("fs"), Level: "info", Message: "served async"}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		ready := sink.catalogRevision == 4 && sink.log.Message == "served async"
		sink.mu.Unlock()
		if ready {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("handler async frames did not reach sink: %+v", sink)
}

func TestServeDropsMalformedHandlerAsyncFrameAndContinues(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	h := asyncHandler{frames: make(chan any, 8)}
	go func() {
		_ = ServeAuthenticated(context.Background(), server, h)
	}()

	ack, err := Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("handshake rejected: %#v", ack)
	}

	sink := &recordingAsyncSink{}
	rc := NewWithSink(client, "local", sink)
	defer rc.Close()

	h.frames <- wire.LifecycleNotice{Op: wire.OpLifecycleNotice, Target: pluginTarget("bad"), State: wire.LifecycleState("failed")}
	h.frames <- wire.CatalogUpdate{Op: wire.OpCatalogUpdate, Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "echo"}}, Revision: 5, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		sink.mu.Lock()
		ready := sink.catalogRevision == 5
		sink.mu.Unlock()
		if ready {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	sink.mu.Lock()
	revision := sink.catalogRevision
	sink.mu.Unlock()
	if revision != 5 {
		t.Fatalf("valid async frame after malformed frame was not delivered: revision=%d", revision)
	}

	got, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-after-malformed-async", TenantID: "default", Target: pluginTarget("fs"), Tool: "parallel"})
	if err != nil {
		t.Fatalf("Invoke after malformed async frame: %v", err)
	}
	if !got.OK || got.CallID != "call-after-malformed-async" {
		t.Fatalf("connection did not remain usable after malformed async frame: %#v", got)
	}
}

func TestServeHandlesHookEventFrames(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	go func() {
		_ = ServeAuthenticated(context.Background(), server, testHandler{})
	}()

	ack, err := Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("handshake rejected: %#v", ack)
	}

	rc := New(client)
	if err := rc.SendHookEvent(context.Background(), runtimeapi.HookEventReq{CallID: "hook-1", Target: pluginTarget("fs"), Event: "before_tool_call", ReplyMode: wire.HookReplyModeModifying, Data: json.RawMessage(`{"tool":"parallel"}`)}); err != nil {
		t.Fatalf("SendHookEvent: %v", err)
	}

	got, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-after-hook", TenantID: "default", Target: pluginTarget("fs"), Tool: "parallel"})
	if err != nil {
		t.Fatalf("Invoke after hook event: %v", err)
	}
	if !got.OK || got.CallID != "call-after-hook" {
		t.Fatalf("connection did not remain usable after hook event: %#v", got)
	}
}

func TestServeDoesNotBlockReadLoopBehindSlowHookEvent(t *testing.T) {
	clientWS, serverWS := websocketNetPipe(t)
	client := codec.New(clientWS)
	server := codec.New(serverWS)
	defer func() { _ = client.CloseNow() }()

	go func() {
		_ = ServeAuthenticated(context.Background(), server, slowHookHandler{})
	}()

	ack, err := Handshake(context.Background(), client, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted {
		t.Fatalf("handshake rejected: %#v", ack)
	}

	rc := New(client)
	if err := rc.SendHookEvent(context.Background(), runtimeapi.HookEventReq{CallID: "slow-hook", Target: pluginTarget("fs"), Event: "before_tool_call", ReplyMode: wire.HookReplyModeModifying, Data: json.RawMessage(`{"tool":"slow"}`)}); err != nil {
		t.Fatalf("SendHookEvent: %v", err)
	}

	invokeCtx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	got, err := rc.Invoke(invokeCtx, runtimeapi.InvokeReq{CallID: "call-behind-slow-hook", TenantID: "default", Target: pluginTarget("fs"), Tool: "parallel"})
	if err != nil {
		t.Fatalf("Invoke behind slow hook: %v", err)
	}
	if !got.OK || got.CallID != "call-behind-slow-hook" {
		t.Fatalf("connection read loop blocked behind slow hook event: %#v", got)
	}
}

type testHandler struct{}

type slowHookHandler struct {
	testHandler
}

type asyncHandler struct {
	testHandler
	frames chan any
}

func (h asyncHandler) AsyncFrames() <-chan any { return h.frames }

func (testHandler) Hello(context.Context, wire.Hello) (wire.HelloAck, error) {
	return wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}, nil
}

func (testHandler) Invoke(_ context.Context, in wire.Invoke) (wire.InvokeResult, error) {
	if in.Tool == "long" {
		select {}
	}
	if in.Tool == "huge" {
		data, _ := json.Marshal("prefix\n" + strings.Repeat("x", int(codec.MaxFrameBytes)+4096))
		return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: in.CallID, OK: true, Data: data}, nil
	}
	return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: in.CallID, OK: true, Data: json.RawMessage(fmt.Sprintf(`{"call_id":"%s"}`, in.CallID))}, nil
}

func (testHandler) Cancel(context.Context, wire.Cancel) (wire.CancelAck, error) {
	return wire.CancelAck{Op: wire.OpCancelAck, CallID: "unused"}, nil
}

func (testHandler) Health(context.Context, wire.Health) (wire.HealthResp, error) {
	return wire.HealthResp{Op: wire.OpHealthResp, OK: true}, nil
}

func (testHandler) ListCapabilities(context.Context, wire.ListCapabilities) (wire.ListCapabilitiesResp, error) {
	return wire.ListCapabilitiesResp{Op: wire.OpListCapabilitiesResp, Targets: []wire.Capability{{Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "parallel"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}}, nil
}

func (testHandler) Reload(context.Context, wire.Reload) (wire.ReloadAck, error) {
	return wire.ReloadAck{Op: wire.OpReloadAck}, nil
}

func (testHandler) PrepareTenant(_ context.Context, in wire.PrepareTenant) (wire.PrepareTenantAck, error) {
	return wire.PrepareTenantAck{
		Op:        wire.OpPrepareTenantAck,
		RequestID: in.RequestID,
		Capabilities: []wire.Capability{{
			Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "parallel"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker,
		}},
	}, nil
}

func (testHandler) HookEvent(_ context.Context, in wire.HookEvent) (wire.HookEventReply, error) {
	return wire.HookEventReply{Op: wire.OpHookEventReply, CallID: in.CallID, Action: wire.HookActionOK}, nil
}

func (slowHookHandler) HookEvent(_ context.Context, in wire.HookEvent) (wire.HookEventReply, error) {
	if in.CallID == "slow-hook" {
		time.Sleep(time.Second)
	}
	return wire.HookEventReply{Op: wire.OpHookEventReply, CallID: in.CallID, Action: wire.HookActionOK}, nil
}

type recordingAsyncSink struct {
	mu              sync.Mutex
	catalogRevision int64
	hookReply       wire.HookEventReply
	send            wire.PluginSend
	log             wire.PluginLog
	notice          wire.LifecycleNotice
}

type failingAsyncSink struct {
	recordingAsyncSink
}

func (s *failingAsyncSink) CatalogUpdated(string, wire.CatalogUpdate) error {
	return errors.New("catalog sink failed")
}

func (s *failingAsyncSink) PluginSent(string, wire.PluginSend) error {
	return errors.New("plugin send sink failed")
}

func (s *failingAsyncSink) LifecycleNoticed(string, wire.LifecycleNotice) error {
	return errors.New("lifecycle sink failed")
}

func (s *recordingAsyncSink) CatalogUpdated(_ string, update wire.CatalogUpdate) error {
	s.mu.Lock()
	s.catalogRevision = update.Revision
	s.mu.Unlock()
	return nil
}

func (s *recordingAsyncSink) HookEventReplied(_ string, reply wire.HookEventReply) error {
	s.mu.Lock()
	s.hookReply = reply
	s.mu.Unlock()
	return nil
}

func (s *recordingAsyncSink) PluginSent(_ string, send wire.PluginSend) error {
	s.mu.Lock()
	s.send = send
	s.mu.Unlock()
	return nil
}

func (s *recordingAsyncSink) PluginLogged(_ string, log wire.PluginLog) {
	s.mu.Lock()
	s.log = log
	s.mu.Unlock()
}

func (s *recordingAsyncSink) LifecycleNoticed(_ string, notice wire.LifecycleNotice) error {
	s.mu.Lock()
	s.notice = notice
	s.mu.Unlock()
	return nil
}

func (s *recordingAsyncSink) RuntimeProtocolError(string, error) {}

type failingStartSink struct {
	err error
}

func (s failingStartSink) Start(string) error { return s.err }
func (s failingStartSink) Delta(string, []byte) error {
	return errors.New("delta should not be called after start failure")
}
func (s failingStartSink) End(string, int64) error {
	return errors.New("end should not be called after start failure")
}

type countingStreamSink struct{}

func (countingStreamSink) Start(string) error         { return nil }
func (countingStreamSink) Delta(string, []byte) error { return nil }
func (countingStreamSink) End(string, int64) error    { return nil }

func websocketNetPipe(t *testing.T) (*websocket.Conn, *websocket.Conn) {
	t.Helper()
	var serverConn *websocket.Conn
	transport := fakeTransport{handler: func(w http.ResponseWriter, r *http.Request) {
		var err error
		serverConn, err = websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{codec.Subprotocol}, InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("Accept: %v", err)
		}
	}}
	clientConn, _, err := websocket.Dial(context.Background(), "ws://runtime.local/runtime", &websocket.DialOptions{HTTPClient: &http.Client{Transport: transport}, Subprotocols: []string{codec.Subprotocol}})
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	if serverConn == nil {
		t.Fatal("server websocket was not accepted")
	}
	return clientConn, serverConn
}

func writeRawFrame(t *testing.T, ws *websocket.Conn, raw string) {
	t.Helper()
	writer, err := ws.Writer(context.Background(), websocket.MessageText)
	if err != nil {
		t.Fatalf("raw frame writer: %v", err)
	}
	if _, err := writer.Write([]byte(raw)); err != nil {
		_ = writer.Close()
		t.Fatalf("raw frame write: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("raw frame close: %v", err)
	}
}

type fakeTransport struct {
	handler http.HandlerFunc
}

func (t fakeTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	clientConn, serverConn := net.Pipe()
	hj := testHijacker{ResponseRecorder: httptest.NewRecorder(), serverConn: serverConn}
	t.handler.ServeHTTP(hj, r)
	resp := hj.ResponseRecorder.Result()
	if resp.StatusCode == http.StatusSwitchingProtocols {
		resp.Body = clientConn
	}
	return resp, nil
}

type testHijacker struct {
	*httptest.ResponseRecorder
	serverConn net.Conn
}

var _ http.Hijacker = testHijacker{}

func (h testHijacker) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return h.serverConn, bufio.NewReadWriter(bufio.NewReader(h.serverConn), bufio.NewWriter(h.serverConn)), nil
}

func pluginTarget(id string) wire.Target { return wire.Target{Kind: wire.TargetKindPlugin, ID: id} }

func ptr[T any](v T) *T { return &v }

func deref(v any) any {
	switch f := v.(type) {
	case *wire.Hello:
		return *f
	case *wire.HelloAck:
		return *f
	case *wire.Invoke:
		return *f
	case *wire.InvokeResult:
		return *f
	case *wire.InvokeResultStart:
		return *f
	case *wire.InvokeResultDelta:
		return *f
	case *wire.InvokeResultEnd:
		return *f
	case *wire.Cancel:
		return *f
	case *wire.CancelAck:
		return *f
	case *wire.Health:
		return *f
	case *wire.HealthResp:
		return *f
	case *wire.ListCapabilities:
		return *f
	case *wire.ListCapabilitiesResp:
		return *f
	case *wire.Reload:
		return *f
	case *wire.ReloadAck:
		return *f
	case *wire.PrepareTenant:
		return *f
	case *wire.PrepareTenantAck:
		return *f
	case *wire.HookEvent:
		return *f
	case *wire.HookEventReply:
		return *f
	case *wire.CatalogUpdate:
		return *f
	case *wire.PluginSend:
		return *f
	case *wire.PluginLog:
		return *f
	case *wire.LifecycleNotice:
		return *f
	default:
		return v
	}
}
