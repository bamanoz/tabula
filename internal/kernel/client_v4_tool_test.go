package kernel

import (
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/agent"
	"github.com/bamanoz/tabula/internal/tenant"
)

func TestHandleClientToolCallStreamsAndCompletesRequest(t *testing.T) {
	hub := NewHub(nil, nil)
	attachTestRuntime(t, hub, runtimePluginCapability("echo", "echo_tool"))
	client := newV4ToolTestClient(hub, tenant.DefaultID, "session-1", nil)
	envelope := &ClientEnvelope{
		V: ClientProtocolVersion, Kind: "command", Op: TopicToolCall, ID: "call-1",
		TenantID: tenant.DefaultID, SessionID: "session-1",
		Data: json.RawMessage(`{"name":"echo_tool","input":{"value":"hello"},"meta":{"source":"test"}}`),
	}

	if err := hub.HandleClientToolCall(client, envelope); err != nil {
		t.Fatalf("HandleClientToolCall: %v", err)
	}

	seen := make(map[string]*ClientEnvelope)
	deadline := time.After(2 * time.Second)
	for seen[TopicToolResult] == nil {
		select {
		case raw := <-client.sendCh:
			var got ClientEnvelope
			if err := json.Unmarshal(raw, &got); err != nil {
				t.Fatalf("decode response: %v", err)
			}
			if got.ID != envelope.ID || got.TenantID != envelope.TenantID || got.SessionID != envelope.SessionID {
				t.Fatalf("correlation mismatch: %+v", got)
			}
			seen[got.Op] = &got
		case <-deadline:
			t.Fatalf("timed out waiting for tool result; seen=%v", seen)
		}
	}
	for _, op := range []string{TopicToolResultStart, TopicToolResultDelta, TopicToolResultEnd, TopicToolResult} {
		if seen[op] == nil {
			t.Fatalf("missing %s envelope", op)
		}
	}
	if seen[TopicToolResult].Kind != "result" {
		t.Fatalf("terminal kind = %q", seen[TopicToolResult].Kind)
	}
	if seen[TopicToolResultDelta].Kind != "event" {
		t.Fatalf("stream kind = %q", seen[TopicToolResultDelta].Kind)
	}
	if got := hub.tools.pendingV4ToolCallCount(); got != 0 {
		t.Fatalf("pending calls after terminal result = %d", got)
	}
}

func TestHandleClientToolCallRequiresDriverAttemptFence(t *testing.T) {
	hub := NewHub(nil, nil)
	client := newV4ToolTestClient(hub, "tenant-1", "session-1", json.RawMessage(`{"tabula.client_role":"driver"}`))
	envelope := &ClientEnvelope{
		V: ClientProtocolVersion, Kind: "command", Op: TopicToolCall, ID: "call-1",
		TenantID: "tenant-1", SessionID: "session-1",
		Data: json.RawMessage(`{"name":"exec_run","input":{}}`),
	}

	err := hub.HandleClientToolCall(client, envelope)
	if !errors.Is(err, agent.ErrInvalidArgument) {
		t.Fatalf("expected invalid argument, got %v", err)
	}
	if got := hub.tools.pendingV4ToolCallCount(); got != 0 {
		t.Fatalf("pending calls after rejection = %d", got)
	}
}

func TestV4ToolCallPendingStateClearsOnDisconnect(t *testing.T) {
	hub := NewHub(nil, nil)
	client := newV4ToolTestClient(hub, "tenant-1", "session-1", nil)
	if !hub.addClient(client) {
		t.Fatal("add client")
	}
	key := v4ToolCallKey{tenantID: "tenant-1", session: "session-1", callID: "call-1"}
	if err := hub.tools.registerV4ToolCall(key, client, "exec_run", toolAttemptContext{}); err != nil {
		t.Fatalf("register pending call: %v", err)
	}

	client.MarkClosed()
	hub.Unregister(client)
	if got := hub.tools.pendingV4ToolCallCount(); got != 0 {
		t.Fatalf("pending calls after disconnect = %d", got)
	}
}

func newV4ToolTestClient(hub *Hub, tenantID, session string, meta json.RawMessage) *Client {
	return &Client{
		hub: hub, name: "v4-client", tenantID: tenantID, session: session, meta: meta,
		sendCh: make(chan []byte, 16), sends: map[string]bool{}, receives: map[string]bool{},
		state: ClientJoined, done: make(chan struct{}),
	}
}
