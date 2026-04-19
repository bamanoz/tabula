package kernel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
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

	// Hook subscriber: listens to session_start (modifying strategy)
	hook := env.connectHook("logger", []HookSubscription{
		{Event: "session_start", Priority: 0},
	})

	// Client joins session — hook receives session_start synchronously
	conn := env.connect("cli", []string{"message"}, []string{})

	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "session_start" {
		t.Fatalf("expected hook/session_start, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	// Payload should contain session and client name
	var payload map[string]string
	json.Unmarshal(hookMsg.Payload, &payload)
	if payload["session"] != "s1" {
		t.Fatalf("expected session s1, got %s", payload["session"])
	}

	// Respond with pass so join completes
	writeJSON(t, hook, Message{
		Type:   "hook_result",
		ID:     hookMsg.ID,
		Action: "pass",
	})

	joined := readMsg(t, conn)
	if joined.Type != "joined" {
		t.Fatalf("expected joined, got %s", joined.Type)
	}
}

func TestHookSessionStartCanBlockJoin(t *testing.T) {
	env := newTestEnv(t)

	hook := env.connectHook("guard", []HookSubscription{
		{Event: "session_start", Priority: 100},
	})

	conn := env.connect("cli", []string{"message"}, []string{"init", "error"})

	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "session_start" {
		t.Fatalf("expected hook/session_start, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	writeJSON(t, hook, Message{
		Type:   "hook_result",
		ID:     hookMsg.ID,
		Action: "block",
		Reason: "denied",
	})

	// Blocked join sends only error, not joined+error.
	errMsg := readMsg(t, conn)
	if errMsg.Type != "error" {
		t.Fatalf("expected error, got %s", errMsg.Type)
	}
	if !strings.Contains(errMsg.Text, "session blocked by hook") {
		t.Fatalf("unexpected error text: %q", errMsg.Text)
	}

	noInit := readMsgTimeout(t, conn, 300*time.Millisecond)
	if noInit != nil {
		t.Fatalf("expected no init after blocked session start, got %s", noInit.Type)
	}
}

func TestHookSessionStartCanInjectInitContext(t *testing.T) {
	env := newTestEnv(t)

	hook := env.connectHook("ctx", []HookSubscription{
		{Event: "session_start", Priority: 100},
	})

	conn := env.connect("cli", []string{"message"}, []string{"init"})

	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "session_start" {
		t.Fatalf("expected hook/session_start, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	writeJSON(t, hook, Message{
		Type:    "hook_result",
		ID:      hookMsg.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"extra join context"}`),
	})

	joined := readMsg(t, conn)
	if joined.Type != "joined" {
		t.Fatalf("expected joined, got %s", joined.Type)
	}

	init := readMsg(t, conn)
	if init.Type != "init" {
		t.Fatalf("expected init, got %s", init.Type)
	}
	if init.Context != "extra join context" {
		t.Fatalf("expected init context, got %q", init.Context)
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

// --- Universal before_tool_call tests ---

// newTestEnvWithSkillTool creates a test env with a skill tool "echo_tool" that just echoes input.
func newTestEnvWithSkillTool(t *testing.T) *testEnv {
	t.Helper()
	toolsJSON := json.RawMessage(`[{"name":"shell_exec","description":"run cmd","params":{"command":{"type":"string","description":"cmd"}},"required":["command"]},{"name":"echo_tool","description":"echo","params":{"text":{"type":"string","description":"text"}},"required":["text"]}]`)
	skillExec := map[string]string{"echo_tool": "echo"}
	hub := NewHub(toolsJSON, skillExec, 3, 5, nil)

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := testUpgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Logf("upgrade error: %v", err)
			return
		}
		NewClient(hub, conn)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(func() {
		hub.Shutdown()
		server.Close()
	})
	return &testEnv{Hub: hub, Server: server, t: t}
}

func TestBeforeToolCallHookFiresForShellExec(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	env := newTestEnv(t)

	hook := env.connectHook("perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"tool_result"},
	)

	// Send shell_exec tool_use
	go func() {
		writeJSON(t, drv, Message{
			Type:  "tool_use",
			Name:  "shell_exec",
			ID:    "t1",
			Input: json.RawMessage(`{"command":"echo hi"}`),
		})
	}()

	// Hook should receive before_tool_call
	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_tool_call" {
		t.Fatalf("expected hook/before_tool_call, got %s/%s", hookMsg.Type, hookMsg.Name)
	}
	var payload map[string]json.RawMessage
	json.Unmarshal(hookMsg.Payload, &payload)
	var tool string
	json.Unmarshal(payload["tool"], &tool)
	if tool != "shell_exec" {
		t.Fatalf("expected tool shell_exec, got %s", tool)
	}

	// Pass through
	writeJSON(t, hook, Message{Type: "hook_result", ID: hookMsg.ID, Action: "pass"})

	// Should get tool_result
	result := readMsg(t, drv)
	if result.Type != "tool_result" {
		t.Fatalf("expected tool_result, got %s", result.Type)
	}
}

func TestBeforeToolCallHookFiresForSkillTool(t *testing.T) {
	env := newTestEnvWithSkillTool(t)

	hook := env.connectHook("perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"tool_result"},
	)

	// Send skill tool_use
	go func() {
		writeJSON(t, drv, Message{
			Type:  "tool_use",
			Name:  "echo_tool",
			ID:    "t2",
			Input: json.RawMessage(`{"text":"hello"}`),
		})
	}()

	// Hook should receive before_tool_call for skill tool
	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_tool_call" {
		t.Fatalf("expected hook/before_tool_call, got %s/%s", hookMsg.Type, hookMsg.Name)
	}
	var payload map[string]json.RawMessage
	json.Unmarshal(hookMsg.Payload, &payload)
	var tool string
	json.Unmarshal(payload["tool"], &tool)
	if tool != "echo_tool" {
		t.Fatalf("expected tool echo_tool, got %s", tool)
	}

	writeJSON(t, hook, Message{Type: "hook_result", ID: hookMsg.ID, Action: "pass"})

	// Should get tool_result
	result := readMsg(t, drv)
	if result.Type != "tool_result" {
		t.Fatalf("expected tool_result, got %s", result.Type)
	}
}

func TestBeforeToolCallHookCanBlockShellExec(t *testing.T) {
	env := newTestEnv(t)

	hook := env.connectHook("perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"tool_result"},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  "tool_use",
			Name:  "shell_exec",
			ID:    "t3",
			Input: json.RawMessage(`{"command":"rm -rf /"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	writeJSON(t, hook, Message{
		Type:   "hook_result",
		ID:     hookMsg.ID,
		Action: "block",
		Reason: "denied",
	})

	// Driver should get error in tool_result
	result := readMsg(t, drv)
	if result.Type != "tool_result" {
		t.Fatalf("expected tool_result, got %s", result.Type)
	}
	if !strings.Contains(result.Output, "blocked by hook") {
		t.Fatalf("expected 'blocked by hook' in output, got %s", result.Output)
	}
}

func TestBeforeToolCallHookCanBlockSkillTool(t *testing.T) {
	env := newTestEnvWithSkillTool(t)

	hook := env.connectHook("perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"tool_result"},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  "tool_use",
			Name:  "echo_tool",
			ID:    "t4",
			Input: json.RawMessage(`{"text":"hello"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	writeJSON(t, hook, Message{
		Type:   "hook_result",
		ID:     hookMsg.ID,
		Action: "block",
		Reason: "denied",
	})

	result := readMsg(t, drv)
	if result.Type != "tool_result" {
		t.Fatalf("expected tool_result, got %s", result.Type)
	}
	if !strings.Contains(result.Output, "blocked by hook") {
		t.Fatalf("expected 'blocked by hook' in output, got %s", result.Output)
	}
}

// --- Hook classification tests ---

func TestSecurityHookTimeoutBlocksToolCall(t *testing.T) {
	env := newTestEnv(t)

	// Hook that never responds on before_tool_call (security event)
	_ = env.connectHook("slow-perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"tool_result"},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  "tool_use",
			Name:  "shell_exec",
			ID:    "t1",
			Input: json.RawMessage(`{"command":"echo hello"}`),
		})
	}()

	// Security hook timeout should block the tool call
	result := readMsgTimeout(t, drv, 10*time.Second)
	if result == nil {
		t.Fatal("expected a result after hook timeout")
	}
	if result.Type != "tool_result" {
		t.Fatalf("expected tool_result, got %s", result.Type)
	}
	if !strings.Contains(result.Output, "blocked by hook") {
		t.Fatalf("expected 'blocked by hook' for timed-out security hook, got %s", result.Output)
	}
}

func TestSecurityHookTimeoutBlocksSpawn(t *testing.T) {
	env := newTestEnv(t)

	_ = env.connectHook("slow-spawn", []HookSubscription{
		{Event: "before_spawn", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"tool_result"},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  "tool_use",
			Name:  "process_spawn",
			ID:    "s1",
			Input: json.RawMessage(`{"command":"sleep 60"}`),
		})
	}()

	result := readMsgTimeout(t, drv, 10*time.Second)
	if result == nil {
		t.Fatal("expected a result after hook timeout")
	}
	if result.Type != "tool_result" {
		t.Fatalf("expected tool_result, got %s", result.Type)
	}
	if !strings.Contains(result.Output, "blocked by hook") {
		t.Fatalf("expected 'blocked by hook' for timed-out security hook, got %s", result.Output)
	}
}

func TestDomainHookTimeoutPassesThrough(t *testing.T) {
	env := newTestEnv(t)

	// Hook that never responds on session_start (domain event)
	_ = env.connectHook("slow-domain", []HookSubscription{
		{Event: "session_start", Priority: 100},
	})

	// Join should still complete after timeout (fail-open for domain hooks)
	conn := env.connect("cli", []string{"message"}, []string{"init"})
	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()

	joined := readMsgTimeout(t, conn, 10*time.Second)
	if joined == nil {
		t.Fatal("expected joined after hook timeout")
	}
	if joined.Type != "joined" {
		t.Fatalf("expected joined, got %s", joined.Type)
	}
}
