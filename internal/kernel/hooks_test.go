package kernel

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/bamanoz/tabula/internal/tenant"
)

type tenantHookProbe struct {
	name     string
	tenantID string
	hooks    []HookSubscription
	sent     atomic.Int32
	done     chan struct{}
}

func newTenantHookProbe(name, tenantID string, hooks []HookSubscription) *tenantHookProbe {
	return &tenantHookProbe{name: name, tenantID: tenantID, hooks: hooks, done: make(chan struct{})}
}

func (p *tenantHookProbe) Name() string    { return p.name }
func (p *tenantHookProbe) Session() string { return "" }
func (p *tenantHookProbe) ServesTenant(tenantID string) bool {
	return p.tenantID == "" || p.tenantID == tenantID
}
func (p *tenantHookProbe) IsConnected() bool         { return true }
func (p *tenantHookProbe) IsBusy() bool              { return false }
func (p *tenantHookProbe) Hooks() []HookSubscription { return p.hooks }
func (p *tenantHookProbe) SendMsg(*Message)          { p.sent.Add(1) }
func (p *tenantHookProbe) Done() <-chan struct{}     { return p.done }

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

func (e *testEnv) connectManagedUIAndJoin(name, session string, sends, receives []string) *websocket.Conn {
	return e.connectManagedUserInputAndJoin(name, "ui", session, sends, receives)
}

func (e *testEnv) connectManagedUserInputAndJoin(name, role, session string, sends, receives []string) *websocket.Conn {
	e.t.Helper()
	conn := e.dial()
	writeJSON(e.t, conn, Message{
		V:    ProtocolVersion,
		Type: string(MsgHello),
		Data: mustMarshalRaw(map[string]any{
			"name":           name,
			"send_topics":    sends,
			"receive_topics": receives,
			"auth_token":     e.Token,
			"meta": map[string]any{
				"tabula.client_role": role,
				"tabula.managed":     true,
			},
		}),
	})
	msg := readMsg(e.t, conn)
	if msg.Type != string(MsgHelloAck) {
		e.t.Fatalf("expected hello_ack, got %+v", msg)
	}
	writeJSON(e.t, conn, Message{Type: "join", Session: session})
	msg = readMsg(e.t, conn)
	if msg.Type != "joined" {
		e.t.Fatalf("expected joined, got %s", msg.Type)
	}
	return conn
}

func waitForNoTurnReceiver(t *testing.T, hub *Hub, tenantID, session, senderName string) {
	t.Helper()
	var senderClient *Client
	for _, client := range hub.clients.All() {
		if client.name == senderName {
			senderClient = client
			break
		}
	}
	if senderClient == nil {
		t.Fatal("sender client not found")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if !hub.hasTurnReceiver(senderClient, tenantID, session) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting for turn receiver disconnect")
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

func TestHookDispatchFiltersGlobalSubscribersByTenant(t *testing.T) {
	engine := NewHookEngine(nil)
	alpha := newTenantHookProbe("alpha-approval", "alpha", []HookSubscription{{Event: "after_tool_call", Priority: 10}})
	beta := newTenantHookProbe("beta-approval", "beta", []HookSubscription{{Event: "after_tool_call", Priority: 10}})
	engine.RebuildIndex([]HookSubscriber{alpha, beta})

	_, ok, _ := engine.DispatchDetailedExcept("after_tool_call", json.RawMessage(`{"tool":"exec_run"}`), "alpha", "main", nil)
	if !ok {
		t.Fatal("after_tool_call should continue")
	}
	if got := alpha.sent.Load(); got != 1 {
		t.Fatalf("alpha hook sends = %d, want 1", got)
	}
	if got := beta.sent.Load(); got != 0 {
		t.Fatalf("beta hook sends = %d, want 0", got)
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
		Payload: mustMarshalRaw(map[string]any{
			"kind":      "approval_denied",
			"retryable": false,
		}),
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

func TestHookBeforePromptBuild_CanRewriteInitTools(t *testing.T) {
	env := newTestEnv(t)
	promptHook := env.connectHook("prompt", []HookSubscription{{Event: "before_prompt_build", Priority: 100}})

	conn := env.connect("cli", []string{TopicMessageUser}, []string{TopicSessionInit})
	go func() {
		writeJSON(t, conn, Message{Type: "join", Session: "s1"})
	}()

	promptMsg := readMsg(t, promptHook)
	var payload struct {
		Tools []map[string]any `json:"tools"`
	}
	if err := json.Unmarshal(promptMsg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal before_prompt_build payload: %v", err)
	}
	if len(payload.Tools) == 0 {
		t.Fatal("expected before_prompt_build payload to include tools")
	}
	writeJSON(t, promptHook, Message{
		Type:    "hook_reply",
		ID:      promptMsg.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"tools":[{"name":"echo_tool","description":"filtered","params":{},"required":[]}]}`),
	})

	_ = readMsg(t, conn) // joined
	init := readMsg(t, conn)
	if !isSessionInit(&init) {
		t.Fatalf("expected session.init, got %+v", init)
	}
	if strings.Contains(string(init.Tools), testShellToolName) {
		t.Fatalf("expected rewritten tools to omit %s, got %s", testShellToolName, string(init.Tools))
	}
	if !strings.Contains(string(init.Tools), "echo_tool") {
		t.Fatalf("expected rewritten tools to include echo_tool, got %s", string(init.Tools))
	}
}

func TestHookBeforeTurn_InjectsTransientTurnContext(t *testing.T) {
	env := newTestEnv(t)
	hook := env.connectHook("memory", []HookSubscription{{Event: "before_turn", Priority: 100}})

	gw := env.connectAndJoin("gw", "s1", []string{TopicMessageUser}, []string{})
	drv := env.connectAndJoin("drv", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})

	go func() {
		writeJSON(t, gw, userMessage("hello"))
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_turn" {
		t.Fatalf("expected hook/before_turn, got %s/%s", hookMsg.Type, hookMsg.Name)
	}
	var payload map[string]any
	if err := json.Unmarshal(hookMsg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal before_turn payload: %v", err)
	}
	if payload["text"] != "hello" {
		t.Fatalf("expected before_turn text hello, got %#v", payload["text"])
	}
	if payload["turn_correlation_id"] == "" {
		t.Fatal("expected before_turn payload to include turn_correlation_id")
	}
	writeJSON(t, hook, Message{
		Type:    "hook_reply",
		ID:      hookMsg.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"remember this user preference"}`),
	})

	msg := readMsg(t, drv)
	if !isUserMessage(&msg) || messageText(&msg) != "hello" {
		t.Fatalf("driver expected message/hello, got %+v", msg)
	}
	var meta map[string]any
	if err := json.Unmarshal(msg.Meta, &meta); err != nil {
		t.Fatalf("unmarshal message meta: %v", err)
	}
	kernel, _ := meta["kernel"].(map[string]any)
	if kernel[turnContextKernelMetaKey] != "remember this user preference" {
		t.Fatalf("expected kernel turn context, got %#v", kernel[turnContextKernelMetaKey])
	}
	if meta[turnCorrelationMetaKey] == "" {
		t.Fatalf("expected routed message to keep turn correlation id, got %#v", meta)
	}
}

func TestHookBeforeTurn_QueuedInputRunsAtDispatchTime(t *testing.T) {
	env := newTestEnv(t)
	hook := env.connectHook("memory", []HookSubscription{{Event: "before_turn", Priority: 100}})

	gw := env.connectAndJoin("gw", "s1", []string{TopicMessageUser}, []string{TopicTurnDone})
	drv := env.connectAndJoin("drv", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})

	writeJSON(t, gw, userMessage("first"))
	firstHook := readMsg(t, hook)
	writeJSON(t, hook, Message{
		Type:    "hook_reply",
		ID:      firstHook.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"FIRST"}`),
	})
	firstMsg := readMsg(t, drv)
	if !isUserMessage(&firstMsg) || messageText(&firstMsg) != "first" {
		t.Fatalf("driver expected first message, got %+v", firstMsg)
	}

	writeJSON(t, gw, userMessage("second"))
	secondHookCh := make(chan Message, 1)
	secondHookErrCh := make(chan error, 1)
	go func() {
		_ = hook.SetReadDeadline(time.Now().Add(5 * time.Second))
		var msg Message
		if err := hook.ReadJSON(&msg); err != nil {
			secondHookErrCh <- err
			return
		}
		secondHookCh <- msg
	}()
	select {
	case queuedHook := <-secondHookCh:
		t.Fatalf("expected queued input to wait for dispatch, got hook %s/%s", queuedHook.Type, queuedHook.Name)
	case err := <-secondHookErrCh:
		t.Fatalf("unexpected hook read error before turn done: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	writeJSON(t, drv, Message{Type: string(MsgEvent), Topic: TopicTurnDone, Meta: firstMsg.Meta})
	done := readMsg(t, gw)
	if done.Type != string(MsgEvent) || done.Topic != TopicTurnDone {
		t.Fatalf("gateway expected turn.done, got %+v", done)
	}

	var secondHook Message
	select {
	case secondHook = <-secondHookCh:
	case err := <-secondHookErrCh:
		t.Fatalf("read second before_turn hook: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for second before_turn hook")
	}
	if secondHook.Type != "hook" || secondHook.Name != "before_turn" {
		t.Fatalf("expected second hook/before_turn, got %s/%s", secondHook.Type, secondHook.Name)
	}
	var secondPayload map[string]any
	if err := json.Unmarshal(secondHook.Payload, &secondPayload); err != nil {
		t.Fatalf("unmarshal second before_turn payload: %v", err)
	}
	if secondPayload["text"] != "second" {
		t.Fatalf("expected queued before_turn text second, got %#v", secondPayload["text"])
	}
	writeJSON(t, hook, Message{
		Type:    "hook_reply",
		ID:      secondHook.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"SECOND"}`),
	})

	secondMsg := readMsg(t, drv)
	if !isUserMessage(&secondMsg) || messageText(&secondMsg) != "second" {
		t.Fatalf("driver expected second message, got %+v", secondMsg)
	}
	var secondMeta map[string]any
	if err := json.Unmarshal(secondMsg.Meta, &secondMeta); err != nil {
		t.Fatalf("unmarshal queued message meta: %v", err)
	}
	kernel, _ := secondMeta["kernel"].(map[string]any)
	if kernel[turnContextKernelMetaKey] != "SECOND" {
		t.Fatalf("expected queued turn context SECOND, got %#v", kernel[turnContextKernelMetaKey])
	}
}

func TestHookBeforeTurn_ManagedInputQueuesWhenReceiverDisconnectsDuringHook(t *testing.T) {
	for _, role := range []string{"ui", "api"} {
		t.Run(role, func(t *testing.T) {
			env := newTestEnv(t)
			hook := env.connectHook("memory", []HookSubscription{{Event: "before_turn", Priority: 100}})

			gw := env.connectManagedUserInputAndJoin("gw", role, "s1", []string{TopicMessageUser}, []string{TopicTurnDone})
			drv := env.connectAndJoin("drv", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})

			go func() {
				writeJSON(t, gw, userMessage("hello"))
			}()

			hookMsg := readMsg(t, hook)
			if hookMsg.Type != "hook" || hookMsg.Name != "before_turn" {
				t.Fatalf("expected hook/before_turn, got %s/%s", hookMsg.Type, hookMsg.Name)
			}
			if err := drv.Close(); err != nil {
				t.Fatalf("close driver: %v", err)
			}
			waitForNoTurnReceiver(t, env.Hub, tenant.DefaultID, "s1", "gw")

			writeJSON(t, hook, Message{
				Type:    "hook_reply",
				ID:      hookMsg.ID,
				Action:  "modify",
				Payload: json.RawMessage(`{"context":"remembered"}`),
			})
			status := readMsg(t, gw)
			if status.Type != string(MsgEvent) || status.Topic != TopicSessionStatus {
				t.Fatalf("gateway expected waiting status, got %+v", status)
			}
			var statusData map[string]any
			if err := json.Unmarshal(status.Data, &statusData); err != nil {
				t.Fatalf("unmarshal waiting status data: %v", err)
			}
			if statusData["state"] != "waiting_for_driver" || statusData["reason"] != "turn_receiver_unavailable" {
				t.Fatalf("unexpected waiting status data: %#v", statusData)
			}

			replacement := env.connectAndJoin("drv2", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})
			queuedHook := readMsg(t, hook)
			if queuedHook.Type != "hook" || queuedHook.Name != "before_turn" {
				t.Fatalf("expected queued hook/before_turn, got %s/%s", queuedHook.Type, queuedHook.Name)
			}
			writeJSON(t, hook, Message{
				Type:    "hook_reply",
				ID:      queuedHook.ID,
				Action:  "modify",
				Payload: json.RawMessage(`{"context":"remembered"}`),
			})
			queued := readMsg(t, replacement)
			if !isUserMessage(&queued) || messageText(&queued) != "hello" {
				t.Fatalf("replacement driver expected queued message/hello, got %+v", queued)
			}
			var meta map[string]any
			if err := json.Unmarshal(queued.Meta, &meta); err != nil {
				t.Fatalf("unmarshal queued message meta: %v", err)
			}
			kernel, _ := meta["kernel"].(map[string]any)
			if kernel[turnContextKernelMetaKey] != "remembered" {
				t.Fatalf("expected queued turn context remembered, got %#v", kernel[turnContextKernelMetaKey])
			}
			writeJSON(t, replacement, Message{Type: string(MsgEvent), Topic: TopicTurnDone, Meta: queued.Meta})
			done := readMsg(t, gw)
			if done.Type != string(MsgEvent) || done.Topic != TopicTurnDone {
				t.Fatalf("gateway expected turn.done, got %+v", done)
			}
		})
	}
}

func TestHookBeforeTurn_ManagedInputQueuesWhenBroadcastDeliversZero(t *testing.T) {
	env := newTestEnv(t)
	hook := env.connectHook("memory", []HookSubscription{{Event: "before_turn", Priority: 100}})

	gw := env.connectManagedUserInputAndJoin("gw", "api", "s1", []string{TopicMessageUser}, []string{TopicTurnDone, TopicSessionStatus})
	drv := env.connectAndJoin("drv", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})

	go func() {
		writeJSON(t, gw, userMessage("hello"))
	}()

	hookMsg := readMsg(t, hook)
	if err := drv.Close(); err != nil {
		t.Fatalf("close driver: %v", err)
	}
	writeJSON(t, hook, Message{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})

	status := readMsg(t, gw)
	if status.Type != string(MsgEvent) || status.Topic != TopicSessionStatus {
		t.Fatalf("gateway expected waiting status, got %+v", status)
	}

	replacement := env.connectAndJoin("drv2", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})
	queuedHook := readMsg(t, hook)
	writeJSON(t, hook, Message{Type: "hook_reply", ID: queuedHook.ID, Action: "pass"})
	queued := readMsg(t, replacement)
	if !isUserMessage(&queued) || messageText(&queued) != "hello" {
		t.Fatalf("replacement driver expected queued message/hello, got %+v", queued)
	}
}

func TestHookBeforeTurn_QueuedManagedInputRequeuesWhenBroadcastDeliversZero(t *testing.T) {
	env := newTestEnv(t)
	hook := env.connectHook("memory", []HookSubscription{{Event: "before_turn", Priority: 100}})

	gw := env.connectManagedUserInputAndJoin("gw", "api", "s1", []string{TopicMessageUser}, []string{TopicTurnDone, TopicSessionStatus})
	drv := env.connectAndJoin("drv", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})

	writeJSON(t, gw, userMessage("first"))
	firstHook := readMsg(t, hook)
	writeJSON(t, hook, Message{Type: "hook_reply", ID: firstHook.ID, Action: "pass"})
	first := readMsg(t, drv)
	if !isUserMessage(&first) || messageText(&first) != "first" {
		t.Fatalf("driver expected first message, got %+v", first)
	}

	writeJSON(t, gw, userMessage("second"))
	writeJSON(t, drv, Message{Type: string(MsgEvent), Topic: TopicTurnDone, Meta: first.Meta})
	_ = readMsg(t, gw)

	queuedHook := readMsg(t, hook)
	if queuedHook.Type != "hook" || queuedHook.Name != "before_turn" {
		t.Fatalf("expected queued hook/before_turn, got %s/%s", queuedHook.Type, queuedHook.Name)
	}
	if err := drv.Close(); err != nil {
		t.Fatalf("close driver: %v", err)
	}
	writeJSON(t, hook, Message{Type: "hook_reply", ID: queuedHook.ID, Action: "pass"})

	status := readMsg(t, gw)
	if status.Type != string(MsgEvent) || status.Topic != TopicSessionStatus {
		t.Fatalf("gateway expected waiting status, got %+v", status)
	}

	replacement := env.connectAndJoin("drv2", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})
	requeuedHook := readMsg(t, hook)
	writeJSON(t, hook, Message{Type: "hook_reply", ID: requeuedHook.ID, Action: "pass"})
	requeued := readMsg(t, replacement)
	if !isUserMessage(&requeued) || messageText(&requeued) != "second" {
		t.Fatalf("replacement driver expected requeued second message, got %+v", requeued)
	}
}

func TestHookBeforeTurn_QueuedManagedInputMirrorsObserversWithoutCountingAsTurnDelivery(t *testing.T) {
	env := newTestEnv(t)
	hook := env.connectHook("memory", []HookSubscription{{Event: "before_turn", Priority: 100}})

	gw := env.connectManagedUserInputAndJoin("gw", "api", "s1", []string{TopicMessageUser}, []string{TopicTurnDone, TopicSessionStatus})
	observer := env.connectAndJoin("observer", "s1", nil, []string{TopicMessageUser})
	drv := env.connectAndJoin("drv", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})

	writeJSON(t, gw, userMessage("first"))
	firstHook := readMsg(t, hook)
	writeJSON(t, hook, Message{Type: "hook_reply", ID: firstHook.ID, Action: "pass"})
	observedFirst := readMsg(t, observer)
	if !isUserMessage(&observedFirst) || messageText(&observedFirst) != "first" {
		t.Fatalf("observer expected mirrored first message, got %+v", observedFirst)
	}
	first := readMsg(t, drv)
	if !isUserMessage(&first) || messageText(&first) != "first" {
		t.Fatalf("driver expected first message, got %+v", first)
	}

	writeJSON(t, gw, userMessage("second"))
	writeJSON(t, drv, Message{Type: string(MsgEvent), Topic: TopicTurnDone, Meta: first.Meta})
	_ = readMsg(t, gw)

	queuedHook := readMsg(t, hook)
	if err := drv.Close(); err != nil {
		t.Fatalf("close driver: %v", err)
	}
	writeJSON(t, hook, Message{Type: "hook_reply", ID: queuedHook.ID, Action: "pass"})

	if observed := readMsgTimeout(t, observer, 50*time.Millisecond); observed == nil || !isUserMessage(observed) || messageText(observed) != "second" {
		t.Fatalf("observer expected mirrored queued second message, got %+v", observed)
	}
	status := readMsg(t, gw)
	if status.Type != string(MsgEvent) || status.Topic != TopicSessionStatus {
		t.Fatalf("gateway expected waiting status, got %+v", status)
	}

	replacement := env.connectAndJoin("drv2", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})
	requeuedHook := readMsg(t, hook)
	writeJSON(t, hook, Message{Type: "hook_reply", ID: requeuedHook.ID, Action: "pass"})
	requeued := readMsg(t, replacement)
	if !isUserMessage(&requeued) || messageText(&requeued) != "second" {
		t.Fatalf("replacement driver expected requeued second message, got %+v", requeued)
	}
}

func TestHookAfterTurn_FiresOnTurnDone(t *testing.T) {
	env := newTestEnv(t)
	hook := env.connectHook("memory", []HookSubscription{{Event: "after_turn", Priority: 100}})

	gw := env.connectAndJoin("gw", "s1", []string{TopicMessageUser}, []string{TopicTurnDone})
	drv := env.connectAndJoin("drv", "s1", []string{TopicTurnDone}, []string{TopicMessageUser})

	writeJSON(t, gw, userMessage("hello"))
	msg := readMsg(t, drv)
	if !isUserMessage(&msg) {
		t.Fatalf("driver expected user message, got %+v", msg)
	}
	writeJSON(t, drv, Message{Type: string(MsgEvent), Topic: TopicTurnDone, Meta: msg.Meta})
	_ = readMsg(t, gw)

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "after_turn" {
		t.Fatalf("expected hook/after_turn, got %s/%s", hookMsg.Type, hookMsg.Name)
	}
	var payload map[string]any
	if err := json.Unmarshal(hookMsg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal after_turn payload: %v", err)
	}
	if payload["status"] != "completed" {
		t.Fatalf("expected after_turn status completed, got %#v", payload["status"])
	}
	if payload["turn_correlation_id"] == "" {
		t.Fatalf("expected after_turn payload to include turn correlation id, got %#v", payload)
	}
	if payload["topic"] != TopicTurnDone {
		t.Fatalf("expected after_turn topic %s, got %#v", TopicTurnDone, payload["topic"])
	}
}

func TestHookBeforeCompaction_FiresBeforeForwardingCompactionStart(t *testing.T) {
	env := newTestEnv(t)
	hook := env.connectHook("memory", []HookSubscription{{Event: "before_compaction", Priority: 100}})

	ui := env.connectAndJoin("ui", "s1", []string{}, []string{TopicCompactionStart})
	drv := env.connectAndJoin("drv", "s1", []string{TopicCompactionStart}, []string{})

	go func() {
		writeJSON(t, drv, Message{Type: string(MsgEvent), Topic: TopicCompactionStart, Text: "Compacting", Meta: withMetaString(nil, turnCorrelationMetaKey, "tc-compact")})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "before_compaction" {
		t.Fatalf("expected hook/before_compaction, got %s/%s", hookMsg.Type, hookMsg.Name)
	}
	var payload map[string]any
	if err := json.Unmarshal(hookMsg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal before_compaction payload: %v", err)
	}
	if payload["topic"] != TopicCompactionStart {
		t.Fatalf("expected before_compaction topic %s, got %#v", TopicCompactionStart, payload["topic"])
	}
	if payload["turn_correlation_id"] != "tc-compact" {
		t.Fatalf("expected before_compaction turn correlation id, got %#v", payload["turn_correlation_id"])
	}
	forwardedCh := make(chan Message, 1)
	forwardedErrCh := make(chan error, 1)
	go func() {
		_ = ui.SetReadDeadline(time.Now().Add(5 * time.Second))
		var msg Message
		if err := ui.ReadJSON(&msg); err != nil {
			forwardedErrCh <- err
			return
		}
		forwardedCh <- msg
	}()
	select {
	case forwarded := <-forwardedCh:
		t.Fatalf("compaction.start forwarded before hook reply: %+v", forwarded)
	case err := <-forwardedErrCh:
		t.Fatalf("unexpected ui read error before hook reply: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	writeJSON(t, hook, Message{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})
	var msg Message
	select {
	case msg = <-forwardedCh:
	case err := <-forwardedErrCh:
		t.Fatalf("read compaction.start after hook reply: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for compaction.start after hook reply")
	}
	if msg.Type != string(MsgEvent) || msg.Topic != TopicCompactionStart {
		t.Fatalf("expected compaction.start after hook reply, got %+v", msg)
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

// newTestEnvWithPluginTool creates a test env with one runtime-hosted plugin tool.
func newTestEnvWithPluginTool(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithPluginToolHome(t, t.TempDir())
}

func newTestEnvWithPluginToolHome(t *testing.T, home string) *testEnv {
	t.Helper()
	toolsJSON := json.RawMessage(`[{"name":"echo_tool","description":"echo","params":{"text":{"type":"string","description":"text"},"command":{"type":"string","description":"command text"}},"required":[]}]`)
	hub := NewHub(toolsJSON, 3, 5, nil)
	hub.SetClientAuthToken("test-kernel-token")
	hub.SetSessionStore(NewDiskSessionStore(home))
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

func TestBeforeToolCallHookFiresForDynamicPluginTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	env := newTestEnvWithPluginTool(t)

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

func TestBeforeToolCallHookFiresForPluginTool(t *testing.T) {
	env := newTestEnvWithPluginTool(t)

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

func TestBeforeToolCallHookCanBlockDynamicPluginTool(t *testing.T) {
	env := newTestEnvWithPluginTool(t)

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
		Type:    "hook_reply",
		ID:      hookMsg.ID,
		Action:  "block",
		Reason:  "denied",
		Payload: mustMarshalRaw(map[string]any{"kind": "approval_denied", "retryable": false}),
	})

	// Driver should get error in tool_result
	result := readMsg(t, drv)
	if !isToolResult(result) {
		t.Fatalf("expected tool_result, got %+v", result)
	}
	var blocked struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Hook  struct {
			ReplyAction string `json:"reply_action"`
			Reason      string `json:"reason"`
		} `json:"hook"`
	}
	if err := json.Unmarshal([]byte(result.Output), &blocked); err != nil {
		t.Fatalf("blocked output should be structured JSON: %v (%s)", err, result.Output)
	}
	if blocked.OK || blocked.Error != "not_invoked" || blocked.Hook.ReplyAction != "block" || blocked.Hook.Reason != "denied" {
		t.Fatalf("unexpected blocked output: %+v", blocked)
	}
	if !strings.Contains(result.Output, `"kind":"approval_denied"`) {
		t.Fatalf("expected structured hook details in blocked output, got %s", result.Output)
	}
}

func TestBeforeToolCallHookCanBlockPluginTool(t *testing.T) {
	env := newTestEnvWithPluginTool(t)

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
	var blocked struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		Hook  struct {
			ReplyAction string `json:"reply_action"`
			Reason      string `json:"reason"`
		} `json:"hook"`
	}
	if err := json.Unmarshal([]byte(result.Output), &blocked); err != nil {
		t.Fatalf("blocked output should be structured JSON: %v (%s)", err, result.Output)
	}
	if blocked.OK || blocked.Error != "not_invoked" || blocked.Hook.ReplyAction != "block" || blocked.Hook.Reason != "denied" {
		t.Fatalf("unexpected blocked output: %+v", blocked)
	}
}

// --- Hook classification tests ---

func TestSecurityHookTimeoutBlocksToolCall(t *testing.T) {
	env := newTestEnvWithPluginTool(t)

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
	var blocked struct {
		Error string `json:"error"`
		Hook  struct {
			Status string `json:"status"`
		} `json:"hook"`
	}
	if err := json.Unmarshal([]byte(result.Output), &blocked); err != nil {
		t.Fatalf("blocked output should be structured JSON: %v (%s)", err, result.Output)
	}
	if blocked.Error != "not_invoked" || blocked.Hook.Status == "" {
		t.Fatalf("unexpected blocked timeout output: %+v", blocked)
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

func TestAfterToolCallHookTruncatesLargeOutput(t *testing.T) {
	env := newTestEnv(t)
	hook := env.connectHook("observer", []HookSubscription{
		{Event: "after_tool_call", Priority: 0},
	})

	env.Hub.emitAfterToolCall(tenant.DefaultID, "main", "tool-1", map[string]string{
		"tool":   "fs_grep",
		"id":     "tool-1",
		"output": strings.Repeat("x", afterToolCallOutputPreviewBytes+1024),
	})

	msg := readMsg(t, hook)
	if msg.Type != string(MsgHook) || msg.Name != "after_tool_call" {
		t.Fatalf("expected after_tool_call hook, got %+v", msg)
	}
	var payload map[string]string
	if err := json.Unmarshal(msg.Payload, &payload); err != nil {
		t.Fatalf("unmarshal hook payload: %v", err)
	}
	if payload["output_truncated"] != "true" {
		t.Fatalf("expected output_truncated=true, got %+v", payload)
	}
	if len(payload["output"]) != afterToolCallOutputPreviewBytes {
		t.Fatalf("preview length = %d, want %d", len(payload["output"]), afterToolCallOutputPreviewBytes)
	}
	if payload["output_bytes"] == "" {
		t.Fatalf("expected original output size, got %+v", payload)
	}
}
