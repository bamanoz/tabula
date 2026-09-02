package kernel

import (
	"context"
	"encoding/json"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
)

func waitForMessage(t *testing.T, ch <-chan *BusMessage) *BusMessage {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
		return nil
	}
}

func readCaptureMessageTimeout(ch <-chan *BusMessage, d time.Duration) *BusMessage {
	select {
	case msg := <-ch:
		return msg
	case <-time.After(d):
		return nil
	}
}

func addCaptureClient(t *testing.T, hub *Hub, name, session string, receives, receivesGlobal []string) *Client {
	return addTenantCaptureClient(t, hub, tenant.DefaultID, name, session, receives, receivesGlobal)
}

func addTenantCaptureClient(t *testing.T, hub *Hub, tenantID, name, session string, receives, receivesGlobal []string) *Client {
	t.Helper()
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	client := &Client{
		hub:            hub,
		name:           name,
		tenantID:       tenantID,
		session:        session,
		recvCh:         make(chan *BusMessage, 8),
		receives:       map[string]bool{},
		receivesGlobal: map[string]bool{},
		sends:          map[string]bool{},
		state:          ClientJoined,
		done:           make(chan struct{}),
	}
	for _, msgType := range receives {
		client.receives[msgType] = true
	}
	for _, msgType := range receivesGlobal {
		client.receivesGlobal[msgType] = true
	}
	if !hub.addClient(client) {
		t.Fatal("addClient failed")
	}
	if session != "" {
		hub.sessions.GetOrCreate(session, tenantID).AddClient(name)
	}
	return client
}

func toolDispatchEntry(hub *Hub, name string) (toolDispatch, bool) {
	if hub == nil {
		return toolDispatch{}, false
	}
	hub.toolExecMu.RLock()
	defer hub.toolExecMu.RUnlock()
	entry, ok := hub.toolExec[name]
	return entry, ok
}

func waitForToolDispatch(t *testing.T, hub *Hub, name string, okFn func(toolDispatch) bool) toolDispatch {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		entry, ok := toolDispatchEntry(hub, name)
		if ok && okFn(entry) {
			return entry
		}
		time.Sleep(10 * time.Millisecond)
	}
	entry, _ := toolDispatchEntry(hub, name)
	t.Fatalf("timed out waiting for tool dispatch %q, last=%+v", name, entry)
	return toolDispatch{}
}

type testRuntimeConn struct{}

func (testRuntimeConn) Invoke(_ context.Context, req runtimeapi.InvokeReq) (runtimeapi.InvokeResp, error) {
	switch req.Tool {
	case "echo_tool":
		return runtimeStringResult(req.CallID, strings.TrimSpace(string(req.Args))), nil
	case testShellToolName:
		return runTestShellInvoke(req)
	default:
		return runtimeapi.InvokeResp{CallID: req.CallID, OK: false, Error: &wire.Error{Code: wire.ErrorToolNotFound, Message: "tool not found"}}, nil
	}
}

func (testRuntimeConn) InvokeStream(ctx context.Context, req runtimeapi.InvokeReq, sink runtimeapi.InvokeStreamSink) (runtimeapi.InvokeResp, error) {
	resp, err := (testRuntimeConn{}).Invoke(ctx, req)
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
	resp.Data = nil
	resp.Streamed = true
	return resp, nil
}

func (testRuntimeConn) Cancel(context.Context, string) error { return nil }

func (testRuntimeConn) Health(context.Context) (runtimeapi.HealthResp, error) {
	return runtimeapi.HealthResp{Op: wire.OpHealthResp, OK: true}, nil
}

func (testRuntimeConn) ListCapabilities(context.Context) (runtimeapi.ListCapabilitiesResp, error) {
	return runtimeapi.ListCapabilitiesResp{Op: wire.OpListCapabilitiesResp}, nil
}

func (testRuntimeConn) Reload(context.Context, runtimeapi.ReloadReq) (runtimeapi.ReloadResp, error) {
	return runtimeapi.ReloadResp{Op: wire.OpReloadAck}, nil
}

func (testRuntimeConn) SendHookEvent(context.Context, runtimeapi.HookEventReq) error { return nil }

func (testRuntimeConn) Close() error { return nil }

func attachTestRuntime(t *testing.T, hub *Hub, capabilities ...wire.Capability) {
	t.Helper()
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	conn := testRuntimeConn{}
	if err := hub.runtimes.RegisterHello("local", conn, capabilities, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	for _, capability := range capabilities {
		hub.syncRuntimeCapability("local", capability)
	}
	hub.rebuildHookIndex()
}

func ensureRuntimeDefinitionsForTest(t *testing.T, hub *Hub, defs ...runtimeconfig.Definition) {
	t.Helper()
	if hub == nil {
		t.Fatal("hub is nil")
	}
	if hub.runtimes == nil {
		hub.runtimes = NewRuntimeRegistry()
	}
	hub.runtimes.mu.RLock()
	defined := make(map[string]runtimeconfig.Definition, len(hub.runtimes.defined)+len(defs))
	for id, def := range hub.runtimes.defined {
		defined[id] = def
	}
	bindings := make(map[string]runtimeconfig.Binding, len(hub.runtimes.tenantBindings))
	for tenantID, binding := range hub.runtimes.tenantBindings {
		bindings[tenantID] = runtimeconfig.Binding{
			AllowedRuntimes: append([]string(nil), binding.AllowedRuntimes...),
			DefaultRuntime:  binding.DefaultRuntime,
		}
	}
	hub.runtimes.mu.RUnlock()
	for _, def := range defs {
		defined[def.ID] = def
	}
	merged := make([]runtimeconfig.Definition, 0, len(defined))
	for _, def := range defined {
		merged = append(merged, def)
	}
	sort.Slice(merged, func(i, j int) bool { return merged[i].ID < merged[j].ID })
	if err := hub.runtimes.Configure(merged, bindings); err != nil {
		t.Fatalf("Configure runtimes: %v", err)
	}
}

func runtimePluginCapability(targetID, toolName string) wire.Capability {
	return wire.Capability{
		Target:      wire.Target{Kind: wire.TargetKindPlugin, ID: targetID},
		Tools:       []wire.ToolSpec{{Name: toolName}},
		State:       wire.CapabilityStateReady,
		Source:      wire.CapabilitySourceWorker,
		WorkerMode:  wire.WorkerModeWarm,
		HarnessKind: wire.HarnessKindPython,
	}
}

func runtimeStringResult(callID, value string) runtimeapi.InvokeResp {
	data, _ := json.Marshal(value)
	return runtimeapi.InvokeResp{CallID: callID, OK: true, Data: data}
}

func runTestShellInvoke(req runtimeapi.InvokeReq) (runtimeapi.InvokeResp, error) {
	var input struct {
		Command string `json:"command"`
	}
	if err := json.Unmarshal(req.Args, &input); err != nil || strings.TrimSpace(input.Command) == "" {
		return runtimeStringResult(req.CallID, "ERROR: missing command"), nil
	}
	out, err := testShellCommand(input.Command + " 2>&1").Output()
	return runtimeStringResult(req.CallID, formatCommandResult(out, err)), nil
}

func testShellCommand(command string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		return exec.Command("cmd", "/c", command)
	}
	return exec.Command("sh", "-c", command)
}
