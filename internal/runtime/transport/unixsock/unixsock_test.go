package unixsock

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestUnixSocketTransportPermissions(t *testing.T) {
	sock := shortSocketPath(t)
	listener, err := Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	if runtime.GOOS == "windows" {
		return
	}
	if got := fileMode(t, filepath.Dir(sock)); got != 0o700 {
		t.Fatalf("socket dir mode = %#o, want 0700", got)
	}
	if got := fileMode(t, sock); got != 0o600 {
		t.Fatalf("socket mode = %#o, want 0600", got)
	}
}

func TestUnixSocketRuntimeConnRoundTripAndClose(t *testing.T) {
	sock := shortSocketPath(t)
	listener, err := Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	accepted := make(chan struct{})
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- listener.Serve(func(ctx context.Context, c *codec.Conn) {
			close(accepted)
			_ = runtimeconn.ServeAuthenticated(ctx, c, socketHandler{})
		})
	}()

	c, err := Dial(context.Background(), "unix://"+sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()
	waitFor(t, accepted, "server accept")

	ack, err := runtimeconn.Handshake(context.Background(), c, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"})
	if err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	if !ack.Accepted || ack.KernelID != "main" {
		t.Fatalf("unexpected ack: %#v", ack)
	}
	rc := runtimeconn.New(c)

	resp, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-echo", TenantID: "default", Target: pluginTarget("fs"), Tool: "echo", Args: json.RawMessage(`{"path":"README.md"}`)})
	if err != nil {
		t.Fatalf("Invoke: %v", err)
	}
	if !resp.OK || string(resp.Data) != `{"echo":{"path":"README.md"}}` {
		t.Fatalf("unexpected invoke response: %#v", resp)
	}

	health, err := rc.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if !health.OK || health.WorkerCount != 1 {
		t.Fatalf("unexpected health: %#v", health)
	}

	caps, err := rc.ListCapabilities(context.Background())
	if err != nil {
		t.Fatalf("ListCapabilities: %v", err)
	}
	if len(caps.Targets) != 1 || caps.Targets[0].Target != pluginTarget("fs") {
		t.Fatalf("unexpected capabilities: %#v", caps)
	}

	target := pluginTarget("fs")
	reload, err := rc.Reload(context.Background(), runtimeapi.ReloadReq{Target: &target})
	if err != nil {
		t.Fatalf("Reload: %v", err)
	}
	if len(reload.EvictedTargets) != 1 || reload.EvictedTargets[0] != target {
		t.Fatalf("unexpected reload: %#v", reload)
	}

	started := make(chan struct{})
	done := make(chan runtimeapi.InvokeResp, 1)
	go func() {
		close(started)
		resp, err := rc.Invoke(context.Background(), runtimeapi.InvokeReq{CallID: "call-drain", TenantID: "default", Target: pluginTarget("fs"), Tool: "long"})
		if err != nil {
			t.Errorf("Invoke long returned error: %v", err)
		}
		done <- resp
	}()
	<-started
	time.Sleep(10 * time.Millisecond)
	if err := rc.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	select {
	case resp := <-done:
		if resp.OK || resp.Error == nil || resp.Error.Code != wire.ErrorRuntimeUnavailable || !resp.Error.Retryable {
			t.Fatalf("expected drained runtime_unavailable, got %#v", resp)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for Close to drain pending invoke")
	}
	listener.Close()
	<-serverDone
}

func TestUnixSocketConcurrentInvokes(t *testing.T) {
	sock := shortSocketPath(t)
	listener, err := Listen(sock)
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	defer listener.Close()

	accepted := make(chan struct{})
	go func() {
		_ = listener.Serve(func(ctx context.Context, c *codec.Conn) {
			close(accepted)
			_ = runtimeconn.ServeAuthenticated(ctx, c, socketHandler{})
		})
	}()
	c, err := Dial(context.Background(), sock)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer func() { _ = c.CloseNow() }()
	waitFor(t, accepted, "server accept")
	if _, err := runtimeconn.Handshake(context.Background(), c, wire.Hello{Op: wire.OpHello, RuntimeID: "local", Token: "redacted", ProtocolVersion: "1"}); err != nil {
		t.Fatalf("Handshake: %v", err)
	}
	rc := runtimeconn.New(c)

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
}

func TestSocketPathParsesUnixURL(t *testing.T) {
	got, err := socketPath("unix:///tmp/tabula.sock")
	if err != nil {
		t.Fatalf("socketPath unix url: %v", err)
	}
	want := "/tmp/tabula.sock"
	if got != want {
		t.Fatalf("socketPath = %q, want %q", got, want)
	}
}

func TestSocketPathPreservesWindowsDrivePath(t *testing.T) {
	const drivePath = "C:\\Users\\Valera\\tabula\\runtime.sock"
	got, err := socketPath("unix://" + drivePath)
	if err != nil {
		t.Fatalf("socketPath windows drive path: %v", err)
	}
	want := drivePath
	if runtime.GOOS != "windows" {
		want = "/" + drivePath
	}
	if got != want {
		t.Fatalf("socketPath = %q, want %q", got, want)
	}
}

func TestSocketPathStripsWindowsURLLeadingSlash(t *testing.T) {
	got, err := socketPath("unix:///C:/Users/Valera/tabula/runtime.sock")
	if err != nil {
		t.Fatalf("socketPath windows slash path: %v", err)
	}
	want := "/C:/Users/Valera/tabula/runtime.sock"
	if runtime.GOOS == "windows" {
		want = "C:/Users/Valera/tabula/runtime.sock"
	}
	if got != want {
		t.Fatalf("socketPath = %q, want %q", got, want)
	}
}

type socketHandler struct{}

func (socketHandler) Hello(context.Context, wire.Hello) (wire.HelloAck, error) {
	return wire.HelloAck{Op: wire.OpHelloAck, Accepted: true, KernelID: "main"}, nil
}

func (socketHandler) Invoke(_ context.Context, in wire.Invoke) (wire.InvokeResult, error) {
	switch in.Tool {
	case "echo":
		return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: in.CallID, OK: true, Data: json.RawMessage(fmt.Sprintf(`{"echo":%s}`, jsonOrNull(in.Args)))}, nil
	case "long":
		select {}
	case "parallel":
		return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: in.CallID, OK: true, Data: json.RawMessage(fmt.Sprintf(`{"call_id":"%s"}`, in.CallID))}, nil
	default:
		return wire.InvokeResult{Op: wire.OpInvokeResult, CallID: in.CallID, OK: false, Error: &wire.Error{Code: wire.ErrorToolNotFound}}, nil
	}
}

func (socketHandler) Cancel(context.Context, wire.Cancel) (wire.CancelAck, error) {
	return wire.CancelAck{Op: wire.OpCancelAck, CallID: "unused"}, nil
}

func (socketHandler) Health(context.Context, wire.Health) (wire.HealthResp, error) {
	return wire.HealthResp{Op: wire.OpHealthResp, OK: true, WorkerCount: 1}, nil
}

func (socketHandler) ListCapabilities(context.Context, wire.ListCapabilities) (wire.ListCapabilitiesResp, error) {
	return wire.ListCapabilitiesResp{Op: wire.OpListCapabilitiesResp, Targets: []wire.Capability{{Target: pluginTarget("fs"), Tools: []wire.ToolSpec{{Name: "echo"}, {Name: "parallel"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}}, nil
}

func (socketHandler) Reload(_ context.Context, in wire.Reload) (wire.ReloadAck, error) {
	if in.Target == nil {
		return wire.ReloadAck{Op: wire.OpReloadAck}, nil
	}
	return wire.ReloadAck{Op: wire.OpReloadAck, EvictedTargets: []wire.Target{*in.Target}}, nil
}

func (socketHandler) HookEvent(_ context.Context, in wire.HookEvent) (wire.HookEventReply, error) {
	return wire.HookEventReply{Op: wire.OpHookEventReply, CallID: in.CallID, Action: wire.HookActionOK}, nil
}

func fileMode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	return info.Mode().Perm()
}

func shortSocketPath(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "tabula-rt-*")
	if err != nil {
		t.Fatalf("temp dir: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return filepath.Join(dir, "run", "rt.sock")
}

func waitFor(t *testing.T, ch <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func pluginTarget(id string) wire.Target { return wire.Target{Kind: wire.TargetKindPlugin, ID: id} }

func jsonOrNull(raw json.RawMessage) []byte {
	if len(raw) == 0 {
		return []byte("null")
	}
	return raw
}
