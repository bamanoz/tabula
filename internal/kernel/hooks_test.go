package kernel

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// connectHook dials + sends connect with hook subscriptions (no join — global subscriber).
func (e *testEnv) connectHook(name string, hooks []HookSubscription) *websocket.Conn {
	e.t.Helper()
	conn := e.dial()
	writeJSON(e.t, conn, Message{
		Type:     "connect",
		Name:     name,
		Sends:    []string{"hook_result"},
		Receives: []string{"hook"},
		Hooks:    hooks,
	})
	msg := readMsg(e.t, conn)
	if msg.Type != "connected" {
		e.t.Fatalf("expected connected, got %s", msg.Type)
	}
	return conn
}

// --- Void hook tests ---

func TestHookVoid_FireAndForget(t *testing.T) {
	env := newTestEnv(t)

	// Hook subscriber: listens to after_message (void)
	hook := env.connectHook("logger", []HookSubscription{
		{Event: "after_message", Priority: 0},
	})

	// Gateway + driver
	gw := env.connectAndJoin("gw", "s1", []string{"message"}, []string{"done"})
	drv := env.connectAndJoin("drv", "s1", []string{"done"}, []string{"message"})

	// Gateway sends message
	writeJSON(t, gw, Message{Type: "message", Text: "hello"})
	// Driver receives message
	msg := readMsg(t, drv)
	if msg.Type != "message" || msg.Text != "hello" {
		t.Fatalf("driver expected message/hello, got %s/%s", msg.Type, msg.Text)
	}

	// Driver sends done
	writeJSON(t, drv, Message{Type: "done"})
	// Gateway receives done
	msg = readMsg(t, gw)
	if msg.Type != "done" {
		t.Fatalf("gw expected done, got %s", msg.Type)
	}

	// Hook subscriber should receive after_message hook
	hookMsg := readMsgTimeout(t, hook, 2*time.Second)
	if hookMsg == nil {
		t.Fatal("hook subscriber did not receive after_message hook")
	}
	if hookMsg.Type != "hook" {
		t.Fatalf("expected hook, got %s", hookMsg.Type)
	}
	if hookMsg.Name != "after_message" {
		t.Fatalf("expected event after_message, got %s", hookMsg.Name)
	}
}

func TestHookVoid_SessionStart(t *testing.T) {
	env := newTestEnv(t)

	// Hook subscriber: listens to session_start (void)
	hook := env.connectHook("logger", []HookSubscription{
		{Event: "session_start", Priority: 0},
	})

	// Client joins session — should fire session_start
	env.connectAndJoin("cli", "s1", []string{"message"}, []string{})

	hookMsg := readMsgTimeout(t, hook, 2*time.Second)
	if hookMsg == nil {
		t.Fatal("hook subscriber did not receive session_start hook")
	}
	if hookMsg.Type != "hook" || hookMsg.Name != "session_start" {
		t.Fatalf("expected hook/session_start, got %s/%s", hookMsg.Type, hookMsg.Name)
	}
	// Payload should contain session and client name
	var payload map[string]string
	json.Unmarshal(hookMsg.Payload, &payload)
	if payload["session"] != "s1" {
		t.Fatalf("expected session s1, got %s", payload["session"])
	}
}

// --- Modifying hook tests ---

func TestHookModifying_PassThrough(t *testing.T) {
	env := newTestEnv(t)

	// Hook subscriber: before_message (modifying), responds with "pass"
	hook := env.connectHook("filter", []HookSubscription{
		{Event: "before_message", Priority: 10},
	})

	gw := env.connectAndJoin("gw", "s1", []string{"message"}, []string{})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{"message"})

	// Send message — hook will receive before_message
	go func() {
		writeJSON(t, gw, Message{Type: "message", Text: "hello"})
	}()

	// Hook receives before_message
	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_message" {
		t.Fatalf("expected hook/before_message, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	// Respond with pass
	writeJSON(t, hook, Message{
		Type:   "hook_result",
		ID:     hookMsg.ID,
		Action: "pass",
	})

	// Driver should receive original message
	msg := readMsg(t, drv)
	if msg.Type != "message" || msg.Text != "hello" {
		t.Fatalf("driver expected message/hello, got %s/%s", msg.Type, msg.Text)
	}
}

func TestHookModifying_ModifyText(t *testing.T) {
	env := newTestEnv(t)

	hook := env.connectHook("filter", []HookSubscription{
		{Event: "before_message", Priority: 10},
	})

	gw := env.connectAndJoin("gw", "s1", []string{"message"}, []string{})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{"message"})

	go func() {
		writeJSON(t, gw, Message{Type: "message", Text: "rm -rf /"})
	}()

	hookMsg := readMsg(t, hook)

	// Modify the text
	writeJSON(t, hook, Message{
		Type:    "hook_result",
		ID:      hookMsg.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"text":"[filtered]"}`),
	})

	// Driver should receive modified message
	msg := readMsg(t, drv)
	if msg.Type != "message" || msg.Text != "[filtered]" {
		t.Fatalf("driver expected message/[filtered], got %s/%s", msg.Type, msg.Text)
	}
}

func TestHookModifying_Block(t *testing.T) {
	env := newTestEnv(t)

	hook := env.connectHook("filter", []HookSubscription{
		{Event: "before_message", Priority: 10},
	})

	gw := env.connectAndJoin("gw", "s1", []string{"message"}, []string{"error"})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{"message"})

	go func() {
		writeJSON(t, gw, Message{Type: "message", Text: "evil"})
	}()

	hookMsg := readMsg(t, hook)

	// Block the message
	writeJSON(t, hook, Message{
		Type:   "hook_result",
		ID:     hookMsg.ID,
		Action: "block",
		Reason: "dangerous",
	})

	// Gateway should receive error
	msg := readMsg(t, gw)
	if msg.Type != "error" {
		t.Fatalf("gw expected error, got %s", msg.Type)
	}

	// Driver should NOT receive anything
	noMsg := readMsgTimeout(t, drv, 500*time.Millisecond)
	if noMsg != nil {
		t.Fatalf("driver should not receive message, got %s", noMsg.Type)
	}
}

func TestHookModifying_Timeout(t *testing.T) {
	env := newTestEnv(t)

	// Hook subscriber that never responds
	_ = env.connectHook("slow", []HookSubscription{
		{Event: "before_message", Priority: 10},
	})

	gw := env.connectAndJoin("gw", "s1", []string{"message"}, []string{})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{"message"})

	writeJSON(t, gw, Message{Type: "message", Text: "hello"})

	// Should still deliver after timeout (treated as pass)
	msg := readMsgTimeout(t, drv, 10*time.Second)
	if msg == nil {
		t.Fatal("message should have been delivered after hook timeout")
	}
	if msg.Text != "hello" {
		t.Fatalf("expected hello, got %s", msg.Text)
	}
}

// --- Priority ordering ---

func TestHookModifying_PriorityOrder(t *testing.T) {
	env := newTestEnv(t)

	// Two hooks: low priority adds prefix, high priority adds suffix.
	// High runs first (priority 20), then low (priority 10).
	hookHigh := env.connectHook("high", []HookSubscription{
		{Event: "before_message", Priority: 20},
	})
	hookLow := env.connectHook("low", []HookSubscription{
		{Event: "before_message", Priority: 10},
	})

	gw := env.connectAndJoin("gw", "s1", []string{"message"}, []string{})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{"message"})

	go func() {
		writeJSON(t, gw, Message{Type: "message", Text: "msg"})
	}()

	// High priority hook fires first
	h1 := readMsg(t, hookHigh)
	writeJSON(t, hookHigh, Message{
		Type:    "hook_result",
		ID:      h1.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"text":"msg+high"}`),
	})

	// Low priority hook fires second, receives modified text
	h2 := readMsg(t, hookLow)
	var payload map[string]string
	json.Unmarshal(h2.Payload, &payload)
	if payload["text"] != "msg+high" {
		t.Fatalf("low hook expected text 'msg+high', got '%s'", payload["text"])
	}

	writeJSON(t, hookLow, Message{
		Type:    "hook_result",
		ID:      h2.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"text":"msg+high+low"}`),
	})

	// Driver gets final result
	msg := readMsg(t, drv)
	if msg.Text != "msg+high+low" {
		t.Fatalf("expected msg+high+low, got %s", msg.Text)
	}
}

// --- No hooks = no change ---

func TestHookNone_MessagePassesThrough(t *testing.T) {
	env := newTestEnv(t)

	// No hook subscribers — message should pass through immediately
	gw := env.connectAndJoin("gw", "s1", []string{"message"}, []string{})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{"message"})

	writeJSON(t, gw, Message{Type: "message", Text: "hello"})
	msg := readMsg(t, drv)
	if msg.Type != "message" || msg.Text != "hello" {
		t.Fatalf("expected message/hello, got %s/%s", msg.Type, msg.Text)
	}
}
