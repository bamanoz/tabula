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
		V:    ProtocolVersion,
		Type: string(MsgHello),
		Data: mustMarshalRaw(map[string]any{
			"name":           name,
			"send_topics":    []string{"hook_reply"},
			"receive_topics": []string{"hook"},
			"hooks":          hooks,
			"auth_token":     e.Token,
		}),
	})
	msg := readMsg(e.t, conn)
	if msg.Type != string(MsgHelloAck) {
		e.t.Fatalf("expected hello_ack, got %+v", msg)
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
	gw := env.connectAndJoin("gw", "s1", []string{TopicMessageUser}, []string{TopicTurnDone})
	drv := env.connectAndJoin("drv", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})

	// Gateway sends message
	writeJSON(t, gw, userMessage("hello"))
	// Driver receives message
	msg := readMsg(t, drv)
	if !isUserMessage(&msg) || messageText(&msg) != "hello" {
		t.Fatalf("driver expected message/hello, got %+v", msg)
	}

	// Driver sends done
	writeJSON(t, drv, turnDone())
	// Gateway receives done
	msg = readMsg(t, gw)
	if msg.Type != string(MsgEvent) || msg.Topic != TopicTurnDone {
		t.Fatalf("gw expected turn.done, got %+v", msg)
	}

	// Hook subscriber should receive after_message hook
	hookMsg := readMsgTimeout(t, hook, 2*time.Second)
	if hookMsg == nil {
		t.Fatal("hook subscriber did not receive after_message hook")
	}
	if hookMsg.Type != "hook" {
		t.Fatalf("expected hook, got %+v", hookMsg)
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
	conn := env.connect("cli", []string{TopicMessageUser}, []string{})

	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "session_start" {
		t.Fatalf("expected hook/session_start, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	// Payload should contain session and client name
	var payload map[string]string
	if err := json.Unmarshal(hookMsg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal hook payload: %v", err)
	}
	if payload["session"] != "s1" {
		t.Fatalf("expected session s1, got %s", payload["session"])
	}

	// Respond with pass so join completes
	writeJSON(t, hook, Message{
		Type:   "hook_reply",
		ID:     hookMsg.ID,
		Action: "pass",
	})

	joined := readMsg(t, conn)
	if joined.Type != "joined" {
		t.Fatalf("expected joined, got %+v", joined)
	}
}

func TestHookVoid_SessionJoinFiresOnEveryJoin(t *testing.T) {
	env := newTestEnv(t)

	hook := env.connectHook("logger", []HookSubscription{{Event: "session_join", Priority: 0}})

	first := env.connect("cli-1", []string{TopicMessageUser}, []string{})
	go func() {
		writeJSON(t, first, Message{Type: "join", Session: "s1"})
	}()

	joinOne := readMsg(t, first)
	if joinOne.Type != "joined" {
		t.Fatalf("expected joined, got %+v", joinOne)
	}
	joinHookOne := readMsg(t, hook)
	if joinHookOne.Type != "hook" || joinHookOne.Name != "session_join" {
		t.Fatalf("expected hook/session_join, got %s/%s", joinHookOne.Type, joinHookOne.Name)
	}

	second := env.connect("cli-2", []string{TopicMessageUser}, []string{})
	go func() {
		writeJSON(t, second, Message{Type: "join", Session: "s1"})
	}()

	joinTwo := readMsg(t, second)
	if joinTwo.Type != "joined" {
		t.Fatalf("expected joined, got %+v", joinTwo)
	}
	joinHookTwo := readMsg(t, hook)
	if joinHookTwo.Type != "hook" || joinHookTwo.Name != "session_join" {
		t.Fatalf("expected hook/session_join, got %s/%s", joinHookTwo.Type, joinHookTwo.Name)
	}
}

func TestHookSessionStartCanBlockJoin(t *testing.T) {
	env := newTestEnv(t)

	hook := env.connectHook("guard", []HookSubscription{
		{Event: "session_start", Priority: 100},
	})

	conn := env.connect("cli", []string{TopicMessageUser}, []string{TopicSessionInit, "error"})

	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "session_start" {
		t.Fatalf("expected hook/session_start, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	writeJSON(t, hook, Message{
		Type:   "hook_reply",
		ID:     hookMsg.ID,
		Action: "block",
		Reason: "denied",
	})

	// Blocked join sends only error, not joined+error.
	errMsg := readMsg(t, conn)
	if errMsg.Type != "error" {
		t.Fatalf("expected error, got %+v", errMsg)
	}
	if !strings.Contains(errMsg.Text, "session blocked") {
		t.Fatalf("unexpected error text: %q", errMsg.Text)
	}

	noInit := readMsgTimeout(t, conn, 300*time.Millisecond)
	if noInit != nil {
		t.Fatalf("expected no init after blocked session start, got %+v", noInit)
	}
}

func TestHookReplyRequiresSubscriberIdentity(t *testing.T) {
	env := newTestEnv(t)

	hook := env.connectHook("guard", []HookSubscription{
		{Event: "session_start", Priority: 100},
	})
	attacker := env.connect("attacker", []string{"hook_reply"}, []string{})
	conn := env.connect("cli", []string{TopicMessageUser}, []string{TopicSessionInit})

	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()
	joinedCh := make(chan Message, 1)
	go func() {
		joinedCh <- readMsg(t, conn)
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "session_start" {
		t.Fatalf("expected hook/session_start, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	writeJSON(t, attacker, Message{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})
	select {
	case msg := <-joinedCh:
		t.Fatalf("wrong-client hook_reply completed join with %+v", msg)
	case <-time.After(200 * time.Millisecond):
	}

	writeJSON(t, hook, Message{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})
	var joined Message
	select {
	case joined = <-joinedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for joined after intended subscriber response")
	}
	if joined.Type != "joined" {
		t.Fatalf("expected joined after intended subscriber response, got %+v", joined)
	}
}

func TestHookSessionStartCanInjectInitContext(t *testing.T) {
	env := newTestEnv(t)

	hook := env.connectHook("ctx", []HookSubscription{
		{Event: "session_start", Priority: 100},
	})

	conn := env.connect("cli", []string{TopicMessageUser}, []string{TopicSessionInit})

	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "session_start" {
		t.Fatalf("expected hook/session_start, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	writeJSON(t, hook, Message{
		Type:    "hook_reply",
		ID:      hookMsg.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"extra join context"}`),
	})

	joined := readMsg(t, conn)
	if joined.Type != "joined" {
		t.Fatalf("expected joined, got %+v", joined)
	}

	init := readMsg(t, conn)
	if !isSessionInit(&init) {
		t.Fatalf("expected session.init, got %+v", init)
	}
	if init.Context != "extra join context" {
		t.Fatalf("expected init context, got %q", init.Context)
	}

	second := env.connect("cli-2", []string{TopicMessageUser}, []string{TopicSessionInit})
	go func() {
		writeJSON(t, second, Message{Type: "join", Session: "s1"})
	}()

	joinedSecond := readMsg(t, second)
	if joinedSecond.Type != "joined" {
		t.Fatalf("expected joined, got %+v", joinedSecond)
	}
	initSecond := readMsg(t, second)
	if !isSessionInit(&initSecond) {
		t.Fatalf("expected session.init, got %+v", initSecond)
	}
	if initSecond.Context != "extra join context" {
		t.Fatalf("expected persisted init context, got %q", initSecond.Context)
	}
	if msg := readMsgTimeout(t, hook, 300*time.Millisecond); msg != nil {
		t.Fatalf("expected no second session_start dispatch, got %s/%s", msg.Type, msg.Name)
	}
}

// --- Modifying hook tests ---

func TestHookSessionStart_ConcatenatesContextAcrossHooks(t *testing.T) {
	env := newTestEnv(t)

	// Two hooks both rewrite session_start with different `context`.
	// Higher priority runs first; lower priority must see the first
	// hook's context as input and the engine must concatenate the two
	// contributions instead of replacing.
	hookHigh := env.connectHook("hi", []HookSubscription{
		{Event: "session_start", Priority: 20},
	})
	hookLow := env.connectHook("lo", []HookSubscription{
		{Event: "session_start", Priority: 10},
	})

	conn := env.connect("cli", []string{TopicMessageUser}, []string{TopicSessionInit})

	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()

	h1 := readMsg(t, hookHigh)
	writeJSON(t, hookHigh, Message{
		Type:    "hook_reply",
		ID:      h1.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"FIRST"}`),
	})

	h2 := readMsg(t, hookLow)
	writeJSON(t, hookLow, Message{
		Type:    "hook_reply",
		ID:      h2.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"SECOND"}`),
	})

	_ = readMsg(t, conn) // joined
	init := readMsg(t, conn)
	if !isSessionInit(&init) {
		t.Fatalf("expected session.init, got %+v", init)
	}
	if !strings.Contains(init.Context, "FIRST") || !strings.Contains(init.Context, "SECOND") {
		t.Fatalf("expected concatenated context with FIRST and SECOND, got %q", init.Context)
	}
}

func TestHookBeforePromptBuild_AppendsContextPerJoin(t *testing.T) {
	env := newTestEnv(t)

	startHook := env.connectHook("start", []HookSubscription{{Event: "session_start", Priority: 100}})
	promptHook := env.connectHook("prompt", []HookSubscription{{Event: "before_prompt_build", Priority: 100}})

	conn := env.connect("cli", []string{TopicMessageUser}, []string{TopicSessionInit})
	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()

	startMsg := readMsg(t, startHook)
	writeJSON(t, startHook, Message{
		Type:    "hook_reply",
		ID:      startMsg.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"BASE"}`),
	})
	promptMsg := readMsg(t, promptHook)
	writeJSON(t, promptHook, Message{
		Type:    "hook_reply",
		ID:      promptMsg.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"PROMPT"}`),
	})

	_ = readMsg(t, conn) // joined
	init := readMsg(t, conn)
	if !isSessionInit(&init) {
		t.Fatalf("expected session.init, got %+v", init)
	}
	if init.Context != "BASE\n\nPROMPT" {
		t.Fatalf("expected base + prompt context, got %q", init.Context)
	}

	second := env.connect("cli-2", []string{TopicMessageUser}, []string{TopicSessionInit})
	go func() {
		writeJSON(t, second, Message{Type: "join", Session: "s1"})
	}()

	secondPromptMsg := readMsg(t, promptHook)
	writeJSON(t, promptHook, Message{
		Type:    "hook_reply",
		ID:      secondPromptMsg.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"PROMPT"}`),
	})

	_ = readMsg(t, second) // joined
	initSecond := readMsg(t, second)
	if !isSessionInit(&initSecond) {
		t.Fatalf("expected session.init, got %+v", initSecond)
	}
	if initSecond.Context != "BASE\n\nPROMPT" {
		t.Fatalf("expected base + prompt context on second join, got %q", initSecond.Context)
	}
	if msg := readMsgTimeout(t, startHook, 300*time.Millisecond); msg != nil {
		t.Fatalf("expected no second session_start dispatch, got %s/%s", msg.Type, msg.Name)
	}
}

func TestHookModifying_PassThrough(t *testing.T) {
	env := newTestEnv(t)

	// Hook subscriber: before_message (modifying), responds with "pass"
	hook := env.connectHook("filter", []HookSubscription{
		{Event: "before_message", Priority: 10},
	})

	gw := env.connectAndJoin("gw", "s1", []string{TopicMessageUser}, []string{})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{TopicMessageUser})

	// Send message — hook will receive before_message
	go func() {
		writeJSON(t, gw, userMessage("hello"))
	}()

	// Hook receives before_message
	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_message" {
		t.Fatalf("expected hook/before_message, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	// Respond with pass
	writeJSON(t, hook, Message{
		Type:   "hook_reply",
		ID:     hookMsg.ID,
		Action: "pass",
	})

	// Driver should receive original message
	msg := readMsg(t, drv)
	if !isUserMessage(&msg) || messageText(&msg) != "hello" {
		t.Fatalf("driver expected message/hello, got %+v", msg)
	}
}

func TestHookModifying_ModifyText(t *testing.T) {
	env := newTestEnv(t)

	hook := env.connectHook("filter", []HookSubscription{
		{Event: "before_message", Priority: 10},
	})

	gw := env.connectAndJoin("gw", "s1", []string{TopicMessageUser}, []string{})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{TopicMessageUser})

	go func() {
		writeJSON(t, gw, userMessage("rm -rf /"))
	}()

	hookMsg := readMsg(t, hook)

	// Modify the text
	writeJSON(t, hook, Message{
		Type:    "hook_reply",
		ID:      hookMsg.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"text":"[filtered]"}`),
	})

	// Driver should receive modified message
	msg := readMsg(t, drv)
	if !isUserMessage(&msg) || messageText(&msg) != "[filtered]" {
		t.Fatalf("driver expected message/[filtered], got %+v", msg)
	}
}

func TestHookModifying_Block(t *testing.T) {
	env := newTestEnv(t)

	hook := env.connectHook("filter", []HookSubscription{
		{Event: "before_message", Priority: 10},
	})

	gw := env.connectAndJoin("gw", "s1", []string{TopicMessageUser}, []string{"error"})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{TopicMessageUser})

	go func() {
		writeJSON(t, gw, userMessage("evil"))
	}()

	hookMsg := readMsg(t, hook)

	// Block the message
	writeJSON(t, hook, Message{
		Type:   "hook_reply",
		ID:     hookMsg.ID,
		Action: "block",
		Reason: "dangerous",
	})

	// Gateway should receive error
	msg := readMsg(t, gw)
	if msg.Type != "error" {
		t.Fatalf("gw expected error, got %+v", msg)
	}

	// Driver should NOT receive anything
	noMsg := readMsgTimeout(t, drv, 500*time.Millisecond)
	if noMsg != nil {
		t.Fatalf("driver should not receive message, got %+v", noMsg)
	}
}

func TestHookModifying_Timeout(t *testing.T) {
	env := newTestEnv(t)

	// Hook subscriber that never responds
	_ = env.connectHook("slow", []HookSubscription{
		{Event: "before_message", Priority: 10},
	})

	gw := env.connectAndJoin("gw", "s1", []string{TopicMessageUser}, []string{})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{TopicMessageUser})

	writeJSON(t, gw, userMessage("hello"))

	// Should still deliver after timeout (treated as pass)
	msg := readMsgTimeout(t, drv, 10*time.Second)
	if msg == nil {
		t.Fatal("message should have been delivered after hook timeout")
	}
	if messageText(msg) != "hello" {
		t.Fatalf("expected hello, got %+v", msg)
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

	gw := env.connectAndJoin("gw", "s1", []string{TopicMessageUser}, []string{})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{TopicMessageUser})

	go func() {
		writeJSON(t, gw, userMessage("msg"))
	}()

	// High priority hook fires first
	h1 := readMsg(t, hookHigh)
	writeJSON(t, hookHigh, Message{
		Type:    "hook_reply",
		ID:      h1.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"text":"msg+high"}`),
	})

	// Low priority hook fires second, receives modified text
	h2 := readMsg(t, hookLow)
	var payload map[string]string
	if err := json.Unmarshal(h2.Payload, &payload); err != nil {
		t.Fatalf("unmarshal hook payload: %v", err)
	}
	if payload["text"] != "msg+high" {
		t.Fatalf("low hook expected text 'msg+high', got '%s'", payload["text"])
	}

	writeJSON(t, hookLow, Message{
		Type:    "hook_reply",
		ID:      h2.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"text":"msg+high+low"}`),
	})

	// Driver gets final result
	msg := readMsg(t, drv)
	if messageText(&msg) != "msg+high+low" {
		t.Fatalf("expected msg+high+low, got %+v", msg)
	}
}

// --- No hooks = no change ---

func TestHookNone_MessagePassesThrough(t *testing.T) {
	env := newTestEnv(t)

	// No hook subscribers — message should pass through immediately
	gw := env.connectAndJoin("gw", "s1", []string{TopicMessageUser}, []string{})
	drv := env.connectAndJoin("drv", "s1", []string{}, []string{TopicMessageUser})

	writeJSON(t, gw, userMessage("hello"))
	msg := readMsg(t, drv)
	if !isUserMessage(&msg) || messageText(&msg) != "hello" {
		t.Fatalf("expected message/hello, got %+v", msg)
	}
}

// --- Universal before_tool_call tests ---

// newTestEnvWithSkillTool creates a test env with one runtime-hosted plugin tool.
func newTestEnvWithSkillTool(t *testing.T) *testEnv {
	t.Helper()
	toolsJSON := json.RawMessage(`[{"name":"echo_tool","description":"echo","params":{"text":{"type":"string","description":"text"},"command":{"type":"string","description":"command text"}},"required":[]}]`)
	hub := NewHub(toolsJSON, 3, 5, nil)
	hub.SetClientAuthToken("test-kernel-token")
	attachTestRuntime(t, hub, runtimePluginCapability("echo", "echo_tool"))

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
	return &testEnv{Hub: hub, Server: server, t: t, Token: "test-kernel-token"}
}

func TestBeforeToolCallHookFiresForDynamicSkillTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	env := newTestEnvWithSkillTool(t)

	hook := env.connectHook("perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	// Send dynamic plugin tool_use
	go func() {
		writeJSON(t, drv, Message{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t1",
			Input: json.RawMessage(`{"text":"hi"}`),
		})
	}()

	// Hook should receive before_tool_call
	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_tool_call" {
		t.Fatalf("expected hook/before_tool_call, got %s/%s", hookMsg.Type, hookMsg.Name)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(hookMsg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal hook payload: %v", err)
	}
	var tool string
	if err := json.Unmarshal(payload["tool"], &tool); err != nil {
		t.Fatalf("unmarshal hook tool: %v", err)
	}
	if tool != "echo_tool" {
		t.Fatalf("expected tool echo_tool, got %+v", tool)
	}

	// Pass through
	writeJSON(t, hook, Message{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})

	// Should get tool_result
	result := readMsg(t, drv)
	if !isToolResult(result) {
		t.Fatalf("expected tool_result, got %+v", result)
	}
}

func TestBeforeToolCallHookFiresForSkillTool(t *testing.T) {
	env := newTestEnvWithSkillTool(t)

	hook := env.connectHook("perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	// Send plugin tool_use
	go func() {
		writeJSON(t, drv, Message{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t2",
			Input: json.RawMessage(`{"text":"hello"}`),
		})
	}()

	// Hook should receive before_tool_call for plugin tool
	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_tool_call" {
		t.Fatalf("expected hook/before_tool_call, got %s/%s", hookMsg.Type, hookMsg.Name)
	}
	var payload map[string]json.RawMessage
	if err := json.Unmarshal(hookMsg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal hook payload: %v", err)
	}
	var tool string
	if err := json.Unmarshal(payload["tool"], &tool); err != nil {
		t.Fatalf("unmarshal hook tool: %v", err)
	}
	if tool != "echo_tool" {
		t.Fatalf("expected tool echo_tool, got %+v", tool)
	}

	writeJSON(t, hook, Message{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})

	// Should get tool_result
	result := readMsg(t, drv)
	if !isToolResult(result) {
		t.Fatalf("expected tool_result, got %+v", result)
	}
}

func TestBeforeToolCallHookCanBlockDynamicSkillTool(t *testing.T) {
	env := newTestEnvWithSkillTool(t)

	hook := env.connectHook("perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t3",
			Input: json.RawMessage(`{"text":"should-not-run"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	writeJSON(t, hook, Message{
		Type:   "hook_reply",
		ID:     hookMsg.ID,
		Action: "block",
		Reason: "denied",
	})

	// Driver should get error in tool_result
	result := readMsg(t, drv)
	if !isToolResult(result) {
		t.Fatalf("expected tool_result, got %+v", result)
	}
	if !strings.Contains(result.Output, "blocked") {
		t.Fatalf("expected 'blocked' in output, got %s", result.Output)
	}
}

func TestBeforeToolCallHookCanBlockSkillTool(t *testing.T) {
	env := newTestEnvWithSkillTool(t)

	hook := env.connectHook("perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t4",
			Input: json.RawMessage(`{"text":"hello"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	writeJSON(t, hook, Message{
		Type:   "hook_reply",
		ID:     hookMsg.ID,
		Action: "block",
		Reason: "denied",
	})

	result := readMsg(t, drv)
	if !isToolResult(&result) {
		t.Fatalf("expected tool_result, got %+v", result)
	}
	if !strings.Contains(result.Output, "blocked") {
		t.Fatalf("expected 'blocked' in output, got %s", result.Output)
	}
}

// --- Hook classification tests ---

func TestSecurityHookTimeoutBlocksToolCall(t *testing.T) {
	env := newTestEnvWithSkillTool(t)

	// Hook that never responds on before_tool_call (security event)
	_ = env.connectHook("slow-perm", []HookSubscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	go func() {
		writeJSON(t, drv, Message{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t1",
			Input: json.RawMessage(`{"text":"hello"}`),
		})
	}()

	// Security hook timeout should block the tool call
	result := readMsgTimeout(t, drv, 10*time.Second)
	if result == nil {
		t.Fatal("expected a result after hook timeout")
	}
	if !isToolResult(&result) {
		t.Fatalf("expected tool_result, got %+v", result)
	}
	if !strings.Contains(result.Output, "blocked") {
		t.Fatalf("expected 'blocked' for timed-out security hook, got %s", result.Output)
	}
}

func TestDomainHookTimeoutPassesThrough(t *testing.T) {
	env := newTestEnv(t)

	// Hook that never responds on session_start (domain event)
	_ = env.connectHook("slow-domain", []HookSubscription{
		{Event: "session_start", Priority: 100},
	})

	// Join should still complete after timeout (fail-open for domain hooks)
	conn := env.connect("cli", []string{TopicMessageUser}, []string{TopicSessionInit})
	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()

	joined := readMsgTimeout(t, conn, 10*time.Second)
	if joined == nil {
		t.Fatal("expected joined after hook timeout")
	}
	if joined.Type != "joined" {
		t.Fatalf("expected joined, got %+v", joined)
	}
}
