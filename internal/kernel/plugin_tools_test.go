package kernel

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

func TestRegisterPluginHandleAddsHookSubscriberAndToolDispatch(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	var buf bytes.Buffer
	h := plugin.NewHandle("hello", nil)
	h.SetWriter(plugin.NewWriter(&buf))
	h.MarkAlive()
	if err := h.MarkRegistered(&plugin.RegisterParams{
		PluginID: "hello",
		Tools: []plugin.ToolSpec{{
			Name:       "hello_ping",
			Schema:     json.RawMessage(`{"type":"object"}`),
			DeadlineMs: 1234,
		}},
		Subscriptions: []plugin.SubscriptionSpec{{Event: "after_message", Priority: 80}},
	}); err != nil {
		t.Fatalf("MarkRegistered: %v", err)
	}

	hub.registerPluginHandle(h)

	entry, ok := hub.toolExec["hello_ping"]
	if !ok {
		t.Fatal("expected plugin tool dispatch entry")
	}
	if entry.Source != toolSourcePlugin || entry.Plugin != h || entry.DeadlineMs != 1234 {
		t.Fatalf("unexpected dispatch entry: %+v", entry)
	}
	if got := len(hub.allHookSubscribers()); got != 1 {
		t.Fatalf("allHookSubscribers count: %d", got)
	}
	if entries := hub.hooks.entries("after_message"); len(entries) != 1 || entries[0].sub.Name() != "hello" {
		t.Fatalf("hook index entries: %+v", entries)
	}
}

func TestHandlePluginProtocolUpdateToolsReplacesDispatchEntries(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	h := plugin.NewHandle("mcp", nil)
	h.MarkAlive()
	if err := h.MarkRegistered(&plugin.RegisterParams{PluginID: "mcp", Tools: []plugin.ToolSpec{{Name: "old"}}}); err != nil {
		t.Fatalf("MarkRegistered: %v", err)
	}
	hub.registerPluginHandle(h)

	msg, err := plugin.NewMessage(plugin.MethodUpdateTools, plugin.UpdateToolsParams{
		Tools: []plugin.ToolSpec{{Name: "new", DeadlineMs: 45000}},
	})
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	hub.handlePluginProtocolMessage(h, msg)

	if _, ok := hub.toolExec["old"]; ok {
		t.Fatal("old plugin tool dispatch entry should be removed")
	}
	entry, ok := hub.toolExec["new"]
	if !ok {
		t.Fatal("expected new plugin dispatch entry")
	}
	if entry.Plugin != h || entry.DeadlineMs != 45000 {
		t.Fatalf("unexpected dispatch entry: %+v", entry)
	}
}

func TestPluginToolDispatchRoundTrip(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	var buf bytes.Buffer
	h := plugin.NewHandle("hello", nil)
	h.SetWriter(plugin.NewWriter(&buf))
	h.MarkAlive()
	if err := h.MarkRegistered(&plugin.RegisterParams{PluginID: "hello", Tools: []plugin.ToolSpec{{Name: "hello_ping", DeadlineMs: 1000}}}); err != nil {
		t.Fatalf("MarkRegistered: %v", err)
	}
	hub.registerPluginHandle(h)
	c := addToolResultCaptureClient(t, hub, "s1")

	hub.tools.handleDynamicTool("s1", "tc-1", "hello_ping", json.RawMessage(`{"x":1}`))

	reader := plugin.NewReader(&buf)
	outbound, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if outbound.Method != plugin.MethodToolCall {
		t.Fatalf("method: %q", outbound.Method)
	}
	var call plugin.ToolCallParams
	if err := outbound.DecodeParams(&call); err != nil {
		t.Fatalf("DecodeParams: %v", err)
	}
	if call.CallID != "tc-1" || call.Name != "hello_ping" || call.Session != "s1" || call.DeadlineMs != 1000 {
		t.Fatalf("tool_call params: %+v", call)
	}

	result, err := plugin.NewMessage(plugin.MethodToolResult, plugin.ToolResultParams{
		CallID: "tc-1",
		Result: json.RawMessage(`"pong"`),
	})
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	hub.handlePluginProtocolMessage(h, result)

	msg := waitForMessage(t, c.recvCh)
	if msg.Type != string(MsgToolResult) || msg.ID != "tc-1" || msg.Output != "pong" {
		t.Fatalf("tool_result message: %+v", msg)
	}
}

func TestPluginHookSubscriberEventReplyRoundTrip(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	pr, pw := io.Pipe()
	defer pr.Close()
	defer pw.Close()
	h := plugin.NewHandle("guard", nil)
	h.SetWriter(plugin.NewWriter(pw))
	h.MarkAlive()
	if err := h.MarkRegistered(&plugin.RegisterParams{
		PluginID:      "guard",
		Subscriptions: []plugin.SubscriptionSpec{{Event: "before_tool_call", Priority: 90}},
	}); err != nil {
		t.Fatalf("MarkRegistered: %v", err)
	}
	hub.registerPluginHandle(h)

	done := make(chan bool, 1)
	go func() {
		_, ok := hub.dispatchHook("before_tool_call", json.RawMessage(`{"tool":"x"}`), "s1")
		done <- ok
	}()

	reader := plugin.NewReader(pr)
	outbound, err := reader.ReadMessage()
	if err != nil {
		t.Fatalf("ReadMessage: %v", err)
	}
	if outbound.Method != plugin.MethodEvent {
		t.Fatalf("method: %q", outbound.Method)
	}
	var event plugin.EventParams
	if err := outbound.DecodeParams(&event); err != nil {
		t.Fatalf("DecodeParams: %v", err)
	}
	if event.CallID == "" || event.Event != "before_tool_call" {
		t.Fatalf("event params: %+v", event)
	}

	reply, err := plugin.NewMessage(plugin.MethodEventReply, plugin.EventReplyParams{
		CallID: event.CallID,
		Action: plugin.ActionDeny,
		Reason: "blocked by plugin",
	})
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	hub.handlePluginProtocolMessage(h, reply)

	select {
	case ok := <-done:
		if ok {
			t.Fatal("expected plugin deny to block hook dispatch")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for hook dispatch")
	}
}

func TestPluginToolDispatchTimeoutCleansPending(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	var buf bytes.Buffer
	h := plugin.NewHandle("slow", nil)
	h.SetWriter(plugin.NewWriter(&buf))
	h.MarkAlive()
	if err := h.MarkRegistered(&plugin.RegisterParams{PluginID: "slow", Tools: []plugin.ToolSpec{{Name: "slow_tool", DeadlineMs: 1}}}); err != nil {
		t.Fatalf("MarkRegistered: %v", err)
	}
	hub.registerPluginHandle(h)
	c := addToolResultCaptureClient(t, hub, "s1")

	hub.tools.handleDynamicTool("s1", "tc-timeout", "slow_tool", json.RawMessage(`{}`))

	msg := waitForMessage(t, c.recvCh)
	if msg.Type != string(MsgToolResult) || msg.ID != "tc-timeout" || !strings.Contains(msg.Output, "plugin timeout") {
		t.Fatalf("timeout tool_result: %+v", msg)
	}
	if got := h.PendingCount(); got != 0 {
		t.Fatalf("pending calls after timeout: %d", got)
	}
}

func TestPluginSendRoutesBusEventToSessionAndGlobalReceivers(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	h := plugin.NewHandle("announcer", nil)
	h.MarkAlive()
	if err := h.MarkRegistered(&plugin.RegisterParams{PluginID: "announcer"}); err != nil {
		t.Fatalf("MarkRegistered: %v", err)
	}

	sessionRecv := addCaptureClient(t, hub, "session-recorder", "s1", []string{"plugin_event"}, nil)
	globalRecv := addCaptureClient(t, hub, "global-recorder", "", nil, []string{"plugin_event"})
	otherSessionRecv := addCaptureClient(t, hub, "other-recorder", "s2", []string{"plugin_event"}, nil)

	msg, err := plugin.NewMessage(plugin.MethodSend, plugin.SendParams{
		Channel: "bus",
		Type:    "plugin_event",
		Session: "s1",
		Payload: json.RawMessage(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	hub.handlePluginProtocolMessage(h, msg)

	for name, ch := range map[string]<-chan *Message{
		"session": sessionRecv.recvCh,
		"global":  globalRecv.recvCh,
	} {
		got := waitForMessage(t, ch)
		if got.Type != "plugin_event" || got.Session != "s1" || string(got.Payload) != `{"ok":true}` {
			t.Fatalf("%s receiver got unexpected message: %+v payload=%s", name, got, string(got.Payload))
		}
	}

	select {
	case got := <-otherSessionRecv.recvCh:
		t.Fatalf("other session should not receive plugin send, got %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestPluginSendDropsUnknownChannel(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	h := plugin.NewHandle("announcer", nil)
	h.MarkAlive()
	if err := h.MarkRegistered(&plugin.RegisterParams{PluginID: "announcer"}); err != nil {
		t.Fatalf("MarkRegistered: %v", err)
	}
	recv := addCaptureClient(t, hub, "recorder", "s1", []string{"plugin_event"}, nil)

	msg, err := plugin.NewMessage(plugin.MethodSend, plugin.SendParams{
		Channel: "files",
		Type:    "plugin_event",
		Session: "s1",
		Payload: json.RawMessage(`{"ok":true}`),
	})
	if err != nil {
		t.Fatalf("NewMessage: %v", err)
	}
	hub.handlePluginProtocolMessage(h, msg)

	select {
	case got := <-recv.recvCh:
		t.Fatalf("unknown channel should be dropped, got %+v", got)
	case <-time.After(100 * time.Millisecond):
	}
}

func addToolResultCaptureClient(t *testing.T, hub *Hub, session string) *Client {
	t.Helper()
	return addCaptureClient(t, hub, "capture", session, []string{string(MsgToolResult)}, nil)
}

func addCaptureClient(t *testing.T, hub *Hub, name, session string, receives, receivesGlobal []string) *Client {
	t.Helper()
	c := &Client{
		hub:            hub,
		name:           name,
		session:        session,
		recvCh:         make(chan *Message, 8),
		receives:       makeCapabilitySet(receives),
		receivesGlobal: makeCapabilitySet(receivesGlobal),
		sends:          map[string]bool{},
		state:          ClientJoined,
		done:           make(chan struct{}),
	}
	if !hub.addClient(c) {
		t.Fatal("addClient failed")
	}
	if session != "" {
		hub.sessions.GetOrCreate(session).AddClient(c.name)
	}
	return c
}

func waitForMessage(t *testing.T, ch <-chan *Message) *Message {
	t.Helper()
	select {
	case msg := <-ch:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for message")
		return nil
	}
}
