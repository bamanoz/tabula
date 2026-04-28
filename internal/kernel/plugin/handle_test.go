package plugin

import (
	"bytes"
	"encoding/json"
	"testing"
)

func TestHandleLifecycleAndRegister(t *testing.T) {
	h := NewHandle("hello", map[string]any{"k": "v"})
	if h.IsAlive() {
		t.Fatal("expected !IsAlive at construction")
	}
	if h.IsRegistered() {
		t.Fatal("expected !IsRegistered at construction")
	}
	if got := h.ID(); got != "hello" {
		t.Fatalf("ID: %q", got)
	}

	// Done channel must not be closed yet.
	select {
	case <-h.Done():
		t.Fatal("Done closed prematurely")
	default:
	}

	h.MarkAlive()
	if !h.IsAlive() {
		t.Fatal("expected IsAlive after MarkAlive")
	}

	reg := &RegisterParams{
		ProtocolVersion: 1,
		PluginID:        "hello",
		Tools: []ToolSpec{
			{Name: "ping", DeadlineMs: 1000},
		},
		Subscriptions: []SubscriptionSpec{
			{Event: "before_tool_call", Priority: 50},
		},
	}
	if err := h.MarkRegistered(reg); err != nil {
		t.Fatalf("MarkRegistered: %v", err)
	}
	if !h.IsRegistered() {
		t.Fatal("expected IsRegistered after MarkRegistered")
	}
	if tools := h.Tools(); len(tools) != 1 || tools[0].Name != "ping" {
		t.Fatalf("Tools after register: %+v", tools)
	}
	if subs := h.Subscriptions(); len(subs) != 1 || subs[0].Event != "before_tool_call" {
		t.Fatalf("Subscriptions after register: %+v", subs)
	}

	h.Close()
	if h.IsAlive() {
		t.Fatal("expected !IsAlive after Close")
	}
	if h.IsRegistered() {
		t.Fatal("expected !IsRegistered after Close")
	}
	select {
	case <-h.Done():
	default:
		t.Fatal("Done should be closed after Close")
	}
	// Idempotent close.
	h.Close()
}

func TestHandleRegisterIDMismatchRejected(t *testing.T) {
	h := NewHandle("hello", nil)
	h.MarkAlive()
	err := h.MarkRegistered(&RegisterParams{PluginID: "other"})
	if err == nil {
		t.Fatal("expected error on plugin_id mismatch")
	}
	if h.IsRegistered() {
		t.Fatal("must not flip registered on mismatch")
	}
}

func TestHandleApplyUpdateToolsReplacesCatalog(t *testing.T) {
	h := NewHandle("mcp", nil)
	h.MarkAlive()
	if err := h.MarkRegistered(&RegisterParams{
		PluginID: "mcp",
		Tools:    []ToolSpec{{Name: "old"}},
	}); err != nil {
		t.Fatalf("MarkRegistered: %v", err)
	}

	if err := h.ApplyUpdateTools(&UpdateToolsParams{
		Tools:   []ToolSpec{{Name: "new1"}, {Name: "new2"}},
		Removed: []string{"old"},
	}); err != nil {
		t.Fatalf("ApplyUpdateTools: %v", err)
	}

	tools := h.Tools()
	if len(tools) != 2 || tools[0].Name != "new1" || tools[1].Name != "new2" {
		t.Fatalf("Tools after update: %+v", tools)
	}
}

func TestHandleRegisterRejectsInvalidCatalogWithoutRegistering(t *testing.T) {
	h := NewHandle("hello", nil)
	h.MarkAlive()
	err := h.MarkRegistered(&RegisterParams{PluginID: "hello", Tools: []ToolSpec{{Name: " "}}})
	if err == nil {
		t.Fatal("expected invalid catalog error")
	}
	if h.IsRegistered() {
		t.Fatal("invalid catalog must not flip registered")
	}
	if len(h.Tools()) != 0 {
		t.Fatalf("invalid catalog mutated tools: %+v", h.Tools())
	}
}

func TestHandleApplyUpdateToolsRejectsInvalidCatalogAtomically(t *testing.T) {
	h := NewHandle("mcp", nil)
	h.MarkAlive()
	if err := h.MarkRegistered(&RegisterParams{PluginID: "mcp", Tools: []ToolSpec{{Name: "old"}}}); err != nil {
		t.Fatalf("MarkRegistered: %v", err)
	}

	err := h.ApplyUpdateTools(&UpdateToolsParams{Tools: []ToolSpec{{Name: "old"}, {Name: "old"}}})
	if err == nil {
		t.Fatal("expected duplicate-name update error")
	}
	tools := h.Tools()
	if len(tools) != 1 || tools[0].Name != "old" {
		t.Fatalf("invalid update mutated tools: %+v", tools)
	}
}

func TestHandleSendEventRequiresAliveAndWriter(t *testing.T) {
	h := NewHandle("x", nil)
	// alive=false → no writer attached either; should error
	if err := h.SendEvent(&EventParams{Event: "before_tool_call"}); err == nil {
		t.Fatal("expected error when no writer attached")
	}

	var buf bytes.Buffer
	h.SetWriter(NewWriter(&buf))
	if err := h.SendEvent(&EventParams{Event: "before_tool_call"}); err == nil {
		t.Fatal("expected error when handle not alive")
	}

	h.MarkAlive()
	if err := h.SendEvent(&EventParams{Event: "before_tool_call"}); err != nil {
		t.Fatalf("SendEvent after alive: %v", err)
	}

	r := NewReader(&buf)
	msg, err := r.ReadMessage()
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if msg.Method != MethodEvent {
		t.Fatalf("method: %q", msg.Method)
	}
	var p EventParams
	if err := msg.DecodeParams(&p); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if p.Event != "before_tool_call" {
		t.Fatalf("event: %q", p.Event)
	}
}

func TestHandleSendToolCallRoundTripAndDeliver(t *testing.T) {
	var buf bytes.Buffer
	h := NewHandle("x", nil)
	h.SetWriter(NewWriter(&buf))
	h.MarkAlive()

	ch, err := h.SendToolCall(&ToolCallParams{
		CallID:     "tc-1",
		Name:       "ping",
		Args:       json.RawMessage(`{}`),
		DeadlineMs: 5000,
	})
	if err != nil {
		t.Fatalf("SendToolCall: %v", err)
	}
	if h.PendingCount() != 1 {
		t.Fatalf("PendingCount: %d", h.PendingCount())
	}

	// Synthesize a tool_result reply and deliver it.
	resultMsg, err := NewMessage(MethodToolResult, ToolResultParams{
		CallID: "tc-1",
		Result: json.RawMessage(`"ok"`),
	})
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	if !h.DeliverResult("tc-1", resultMsg) {
		t.Fatal("DeliverResult returned false")
	}
	if h.PendingCount() != 0 {
		t.Fatalf("PendingCount after deliver: %d", h.PendingCount())
	}

	got := <-ch
	if got.Method != MethodToolResult {
		t.Fatalf("method: %q", got.Method)
	}
}

func TestHandleSendToolCallRequiresCallID(t *testing.T) {
	var buf bytes.Buffer
	h := NewHandle("x", nil)
	h.SetWriter(NewWriter(&buf))
	h.MarkAlive()

	if _, err := h.SendToolCall(&ToolCallParams{}); err == nil {
		t.Fatal("expected error for empty callId")
	}
	if h.PendingCount() != 0 {
		t.Fatalf("PendingCount must remain 0, got %d", h.PendingCount())
	}
}

func TestHandleDeliverUnknownCallIDDrops(t *testing.T) {
	h := NewHandle("x", nil)
	if h.DeliverResult("missing", &Message{Method: MethodToolResult}) {
		t.Fatal("DeliverResult must return false for unknown callId")
	}
	if h.DeliverResult("", &Message{Method: MethodToolResult}) {
		t.Fatal("DeliverResult must return false for empty callId")
	}
}

func TestHandleCancelPendingFreesSlot(t *testing.T) {
	var buf bytes.Buffer
	h := NewHandle("x", nil)
	h.SetWriter(NewWriter(&buf))
	h.MarkAlive()

	if _, err := h.SendToolCall(&ToolCallParams{CallID: "tc-1"}); err != nil {
		t.Fatalf("SendToolCall: %v", err)
	}
	if h.PendingCount() != 1 {
		t.Fatalf("PendingCount: %d", h.PendingCount())
	}
	h.CancelPending("tc-1")
	if h.PendingCount() != 0 {
		t.Fatalf("PendingCount after cancel: %d", h.PendingCount())
	}
}
