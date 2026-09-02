package kernel

import (
	"encoding/json"
	"fmt"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
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
	hooks    []khooks.Subscription
	sent     atomic.Int32
	done     chan struct{}
}

func newTenantHookProbe(name, tenantID string, hooks []khooks.Subscription) *tenantHookProbe {
	return &tenantHookProbe{name: name, tenantID: tenantID, hooks: hooks, done: make(chan struct{})}
}

func (p *tenantHookProbe) Name() string    { return p.name }
func (p *tenantHookProbe) Session() string { return "" }
func (p *tenantHookProbe) ServesTenant(tenantID string) bool {
	return p.tenantID == "" || p.tenantID == tenantID
}
func (p *tenantHookProbe) IsConnected() bool            { return true }
func (p *tenantHookProbe) IsBusy() bool                 { return false }
func (p *tenantHookProbe) Hooks() []khooks.Subscription { return p.hooks }
func (p *tenantHookProbe) SendHook(*khooks.Message)     { p.sent.Add(1) }
func (p *tenantHookProbe) Done() <-chan struct{}        { return p.done }

// connectHook dials + sends connect with hook subscriptions (no join — global subscriber).
func (e *testEnv) connectHook(name string, hooks []khooks.Subscription) *websocket.Conn {
	e.t.Helper()
	conn, _ := e.connectOpen(name, []string{"hook_reply"}, []string{"hook"}, nil, hooks, map[string]any{})
	return conn
}

// --- Void hook tests ---

func TestHookDispatchFiltersGlobalSubscribersByTenant(t *testing.T) {
	engine := khooks.NewEngine(nil)
	alpha := newTenantHookProbe("alpha-approval", "alpha", []khooks.Subscription{{Event: "after_tool_call", Priority: 10}})
	beta := newTenantHookProbe("beta-approval", "beta", []khooks.Subscription{{Event: "after_tool_call", Priority: 10}})
	engine.RebuildIndex([]khooks.Subscriber{alpha, beta})

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
	hook := env.connectHook("logger", []khooks.Subscription{
		{Event: "session_start", Priority: 0},
	})

	// Client joins session — hook receives session_start synchronously
	conn := env.connect("cli", []string{testExtensionTopic}, []string{})

	go func() {
		writeJSON(t, conn, BusMessage{Type: "join", Session: "s1"})
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
	writeJSON(t, hook, BusMessage{
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

	hook := env.connectHook("logger", []khooks.Subscription{{Event: "session_join", Priority: 0}})

	first := env.connect("cli-1", []string{testExtensionTopic}, []string{})
	go func() {
		writeJSON(t, first, BusMessage{Type: "join", Session: "s1"})
	}()

	joinOne := readMsg(t, first)
	if joinOne.Type != "joined" {
		t.Fatalf("expected joined, got %+v", joinOne)
	}
	joinHookOne := readMsg(t, hook)
	if joinHookOne.Type != "hook" || joinHookOne.Name != "session_join" {
		t.Fatalf("expected hook/session_join, got %s/%s", joinHookOne.Type, joinHookOne.Name)
	}

	second := env.connect("cli-2", []string{testExtensionTopic}, []string{})
	go func() {
		writeJSON(t, second, BusMessage{Type: "join", Session: "s1"})
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

	hook := env.connectHook("guard", []khooks.Subscription{
		{Event: "session_start", Priority: 100},
	})

	conn := env.connect("cli", []string{testExtensionTopic}, []string{TopicSessionInit, "error"})

	go func() {
		writeJSON(t, conn, BusMessage{Type: "join", Session: "s1"})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "session_start" {
		t.Fatalf("expected hook/session_start, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	writeJSON(t, hook, BusMessage{
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

	hook := env.connectHook("guard", []khooks.Subscription{
		{Event: "session_start", Priority: 100},
	})
	attacker := env.connect("attacker", []string{"hook_reply"}, []string{})
	conn := env.connect("cli", []string{testExtensionTopic}, []string{TopicSessionInit})

	go func() {
		writeJSON(t, conn, BusMessage{Type: "join", Session: "s1"})
	}()
	joinedCh := make(chan BusMessage, 1)
	go func() {
		joinedCh <- readMsg(t, conn)
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "session_start" {
		t.Fatalf("expected hook/session_start, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	writeJSON(t, attacker, BusMessage{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})
	select {
	case msg := <-joinedCh:
		t.Fatalf("wrong-client hook_reply completed join with %+v", msg)
	case <-time.After(200 * time.Millisecond):
	}

	writeJSON(t, hook, BusMessage{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})
	var joined BusMessage
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

	hook := env.connectHook("ctx", []khooks.Subscription{
		{Event: "session_start", Priority: 100},
	})

	conn := env.connect("cli", []string{testExtensionTopic}, []string{TopicSessionInit})

	go func() {
		writeJSON(t, conn, BusMessage{Type: "join", Session: "s1"})
	}()

	hookMsg := readMsg(t, hook)
	if hookMsg.Type != "hook" || hookMsg.Name != "session_start" {
		t.Fatalf("expected hook/session_start, got %s/%s", hookMsg.Type, hookMsg.Name)
	}

	writeJSON(t, hook, BusMessage{
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

	second := env.connect("cli-2", []string{testExtensionTopic}, []string{TopicSessionInit})
	go func() {
		writeJSON(t, second, BusMessage{Type: "join", Session: "s1"})
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
	hookHigh := env.connectHook("hi", []khooks.Subscription{
		{Event: "session_start", Priority: 20},
	})
	hookLow := env.connectHook("lo", []khooks.Subscription{
		{Event: "session_start", Priority: 10},
	})

	conn := env.connect("cli", []string{testExtensionTopic}, []string{TopicSessionInit})

	go func() {
		writeJSON(t, conn, BusMessage{Type: "join", Session: "s1"})
	}()

	h1 := readMsg(t, hookHigh)
	writeJSON(t, hookHigh, BusMessage{
		Type:    "hook_reply",
		ID:      h1.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"FIRST"}`),
	})

	h2 := readMsg(t, hookLow)
	writeJSON(t, hookLow, BusMessage{
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

	startHook := env.connectHook("start", []khooks.Subscription{{Event: "session_start", Priority: 100}})
	promptHook := env.connectHook("prompt", []khooks.Subscription{{Event: "before_prompt_build", Priority: 100}})

	conn := env.connect("cli", []string{testExtensionTopic}, []string{TopicSessionInit})
	go func() {
		writeJSON(t, conn, BusMessage{Type: "join", Session: "s1"})
	}()

	startMsg := readMsg(t, startHook)
	writeJSON(t, startHook, BusMessage{
		Type:    "hook_reply",
		ID:      startMsg.ID,
		Action:  "modify",
		Payload: json.RawMessage(`{"context":"BASE"}`),
	})
	promptMsg := readMsg(t, promptHook)
	writeJSON(t, promptHook, BusMessage{
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

	second := env.connect("cli-2", []string{testExtensionTopic}, []string{TopicSessionInit})
	go func() {
		writeJSON(t, second, BusMessage{Type: "join", Session: "s1"})
	}()

	secondPromptMsg := readMsg(t, promptHook)
	writeJSON(t, promptHook, BusMessage{
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
	promptHook := env.connectHook("prompt", []khooks.Subscription{{Event: "before_prompt_build", Priority: 100}})

	conn := env.connect("cli", []string{testExtensionTopic}, []string{TopicSessionInit})
	go func() {
		writeJSON(t, conn, BusMessage{Type: "join", Session: "s1"})
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
	writeJSON(t, promptHook, BusMessage{
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

func TestHookBeforeCompaction_FiresBeforeForwardingCompactionStart(t *testing.T) {
	env := newTestEnv(t)
	hook := env.connectHook("memory", []khooks.Subscription{{Event: "before_compaction", Priority: 100}})

	ui := env.connectAndJoin("user", "s1", []string{}, []string{TopicCompactionStart})
	drv := env.connectAndJoin("drv", "s1", []string{TopicCompactionStart}, []string{})

	go func() {
		writeJSON(t, drv, BusMessage{Type: string(MsgEvent), Topic: TopicCompactionStart, Text: "Compacting", Meta: withMetaString(nil, turnCorrelationMetaKey, "tc-compact")})
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
	forwardedCh := make(chan BusMessage, 1)
	forwardedErrCh := make(chan error, 1)
	go func() {
		msg := readMsgTimeout(t, ui, 5*time.Second)
		if msg == nil {
			forwardedErrCh <- fmt.Errorf("compaction read timed out")
			return
		}
		forwardedCh <- *msg
	}()
	select {
	case forwarded := <-forwardedCh:
		t.Fatalf("compaction.start forwarded before hook reply: %+v", forwarded)
	case err := <-forwardedErrCh:
		t.Fatalf("unexpected ui read error before hook reply: %v", err)
	case <-time.After(300 * time.Millisecond):
	}

	writeJSON(t, hook, BusMessage{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})
	var msg BusMessage
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

// --- Priority ordering ---

// --- No hooks = no change ---

// --- Universal before_tool_call tests ---

// newTestEnvWithPluginTool creates a test env with one runtime-hosted plugin tool.
func newTestEnvWithPluginTool(t *testing.T) *testEnv {
	t.Helper()
	return newTestEnvWithPluginToolHome(t, t.TempDir())
}

func newTestEnvWithPluginToolHome(t *testing.T, home string) *testEnv {
	t.Helper()
	toolsJSON := json.RawMessage(`[{"name":"echo_tool","description":"echo","params":{"text":{"type":"string","description":"text"},"command":{"type":"string","description":"command text"}},"required":[]}]`)
	hub := NewHub(toolsJSON, nil)
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
		// Wait for WebSocket pumps to finish unregistering before TempDir cleanup.
		time.Sleep(50 * time.Millisecond)
	})
	return &testEnv{Hub: hub, Server: server, t: t, Token: "test-kernel-token"}
}

func TestBeforeToolCallHookFiresForDynamicPluginTool(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("skipping on windows")
	}
	env := newTestEnvWithPluginTool(t)

	hook := env.connectHook("perm", []khooks.Subscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	// Send dynamic plugin tool_use
	go func() {
		writeJSON(t, drv, BusMessage{
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
	writeJSON(t, hook, BusMessage{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})

	// Should get tool_result
	result := readMsg(t, drv)
	if !isToolResult(result) {
		t.Fatalf("expected tool_result, got %+v", result)
	}
}

func TestBeforeToolCallHookFiresForPluginTool(t *testing.T) {
	env := newTestEnvWithPluginTool(t)

	hook := env.connectHook("perm", []khooks.Subscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	// Send plugin tool_use
	go func() {
		writeJSON(t, drv, BusMessage{
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

	writeJSON(t, hook, BusMessage{Type: "hook_reply", ID: hookMsg.ID, Action: "pass"})

	// Should get tool_result
	result := readMsg(t, drv)
	if !isToolResult(result) {
		t.Fatalf("expected tool_result, got %+v", result)
	}
}

func TestBeforeToolCallHookCanBlockDynamicPluginTool(t *testing.T) {
	env := newTestEnvWithPluginTool(t)

	hook := env.connectHook("perm", []khooks.Subscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	go func() {
		writeJSON(t, drv, BusMessage{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t3",
			Input: json.RawMessage(`{"text":"should-not-run"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	writeJSON(t, hook, BusMessage{
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

	hook := env.connectHook("perm", []khooks.Subscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	go func() {
		writeJSON(t, drv, BusMessage{
			Type:  string(MsgRequest),
			Topic: TopicToolCall,
			Name:  "echo_tool",
			ID:    "t4",
			Input: json.RawMessage(`{"text":"hello"}`),
		})
	}()

	hookMsg := readMsg(t, hook)
	writeJSON(t, hook, BusMessage{
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
	_ = env.connectHook("slow-perm", []khooks.Subscription{
		{Event: "before_tool_call", Priority: 100},
	})

	drv := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicToolResult},
	)

	go func() {
		writeJSON(t, drv, BusMessage{
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
	_ = env.connectHook("slow-domain", []khooks.Subscription{
		{Event: "session_start", Priority: 100},
	})

	// Join should still complete after timeout (fail-open for domain hooks)
	conn := env.connect("cli", []string{testExtensionTopic}, []string{TopicSessionInit})
	go func() {
		writeJSON(t, conn, BusMessage{Type: "join", Session: "s1"})
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
	hook := env.connectHook("observer", []khooks.Subscription{
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
