package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/shell"
	"github.com/bamanoz/tabula/internal/tenant"
)

func waitForMessage(t *testing.T, ch <-chan *Message) *Message {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for message")
		return nil
	}
}

func readCaptureMessageTimeout(ch <-chan *Message, d time.Duration) *Message {
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
		recvCh:         make(chan *Message, 8),
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
	conn := testRuntimeConn{}
	if err := hub.runtimes.RegisterHello("local", conn, capabilities, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	for _, capability := range capabilities {
		hub.syncRuntimeCapability("local", capability)
	}
	hub.rebuildHookIndex()
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
	out, err := shell.Command(input.Command + " 2>&1").Output()
	return runtimeStringResult(req.CallID, formatCommandResult(out, err)), nil
}
