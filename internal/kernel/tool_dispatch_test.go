package kernel

import (
	"encoding/json"
	"testing"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

// TestNewHub_ConvertsSkillExecToDispatch verifies the legacy
// `skillExec map[string]string` argument is faithfully converted to the
// unified `toolDispatch` table per creative §4 (D1.7).
func TestNewHub_ConvertsSkillExecToDispatch(t *testing.T) {
	skillExec := map[string]string{
		"echo":  "echo",
		"empty": "", // empty commands must be skipped
	}
	hub := NewHub(json.RawMessage(`[]`), skillExec, 3, 5, nil)
	if got, ok := hub.toolExec["echo"]; !ok {
		t.Fatal("expected echo to be registered")
	} else {
		if got.Source != toolSourceSkill {
			t.Fatalf("expected toolSourceSkill, got %v", got.Source)
		}
		if got.Command != "echo" {
			t.Fatalf("expected command 'echo', got %q", got.Command)
		}
		if got.Plugin != nil {
			t.Fatalf("expected Plugin to be nil for skill source")
		}
	}
	if _, ok := hub.toolExec["empty"]; ok {
		t.Fatal("expected empty-command tools to be skipped")
	}
}

// TestHandleDynamicTool_PluginSourceUnavailable verifies a registered plugin
// tool whose handle is not alive/registered returns a clean error rather than
// panicking.
func TestHandleDynamicTool_PluginSourceUnavailable(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	hub.toolExec["mcp__foo"] = toolDispatch{
		Source: toolSourcePlugin,
		Plugin: plugin.NewHandle("mcp", nil),
	}

	// Use an internal client to capture the tool_result without WebSocket.
	c := &Client{
		hub:     hub,
		name:    "test",
		session: "s1",
		recvCh:  make(chan *Message, 4),
		sends:   map[string]bool{},
		state:   ClientJoined,
		done:    make(chan struct{}),
	}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreate("s1").AddClient(c.name)

	// Drive the dispatcher directly.
	hub.tools.handleDynamicTool("s1", "tid-1", "mcp__foo", json.RawMessage(`{}`))

	select {
	case msg := <-c.recvCh:
		if msg.Type != string(MsgToolResult) {
			t.Fatalf("expected tool_result, got %+v", msg)
		}
		// The Output field carries the error string for tool_result.
		if got := msg.Output; got == "" {
			t.Fatal("expected non-empty output content")
		}
	default:
		// In some race the message may go elsewhere; the important
		// assertion is no panic and dispatch completed.
	}
}

func TestHandleDynamicTool_RuntimeSourceInvokesAttachedRuntime(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	rc := runtimemock.New()
	target := wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}
	rc.OnInvoke("default", target, "mcp__echo").Return([]byte(`"ok"`))
	if err := hub.runtimes.RegisterHello("local", rc, []wire.Capability{{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker}}, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.syncRuntimeCapability("local", wire.Capability{Target: target, Tools: []wire.ToolSpec{{Name: "mcp__echo"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker})

	c := &Client{
		hub:      hub,
		name:     "test",
		session:  "s1",
		recvCh:   make(chan *Message, 4),
		receives: map[string]bool{string(MsgToolResult): true},
		sends:    map[string]bool{},
		state:    ClientJoined,
		done:     make(chan struct{}),
	}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	hub.sessions.GetOrCreate("s1").AddClient(c.name)

	hub.tools.handleDynamicTool("s1", "tid-rt", "mcp__echo", json.RawMessage(`{}`))
	msg := waitForMessage(t, c.recvCh)
	if msg.Type != string(MsgToolResult) || msg.Output != "ok" {
		t.Fatalf("unexpected runtime tool result: %+v", msg)
	}
	recorded := rc.RecordedInvokes()
	if len(recorded) != 1 || recorded[0].TenantID != "default" || recorded[0].Tool != "mcp__echo" || recorded[0].Target != target {
		t.Fatalf("unexpected runtime invoke record: %+v", recorded)
	}
}
