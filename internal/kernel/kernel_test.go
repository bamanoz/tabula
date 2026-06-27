package kernel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
	"github.com/gorilla/websocket"
)

// --- Test helper ---

var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const testShellToolName = "test_shell"

// testEnv bundles a Hub + httptest server + WS client factory for tests.
type testEnv struct {
	Hub    *Hub
	Server *httptest.Server
	t      *testing.T
	Token  string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	toolsJSON := json.RawMessage(`[{"name":"echo_tool","description":"echo stdin","params":{"text":{"type":"string","description":"text to echo"}},"required":[]},{"name":"test_shell","description":"test-only shell-style skill","params":{"command":{"type":"string","description":"command to run"}},"required":["command"]}]`)
	hub := NewHub(toolsJSON, 3, 5, nil)
	hub.SetClientAuthToken("test-kernel-token")
	attachTestRuntime(t, hub,
		runtimePluginCapability("echo", "echo_tool"),
		runtimePluginCapability("test-shell", testShellToolName),
	)

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
		// Give readPump goroutines time to finish Unregister after server.Close()
		time.Sleep(50 * time.Millisecond)
	})
	return &testEnv{Hub: hub, Server: server, t: t, Token: "test-kernel-token"}
}

func TestBroadcastToSessionStampsSessionAndTenantForAllReceivers(t *testing.T) {
	env := newTestEnv(t)
	member := addCaptureClient(t, env.Hub, "member", "main", []string{TopicStreamDelta}, nil)
	global := addCaptureClient(t, env.Hub, "global", "", nil, []string{TopicStreamDelta})

	env.Hub.broadcastToSession("default", "main", TopicStreamDelta, &Message{Type: string(MsgEvent), Topic: TopicStreamDelta, Data: mustMarshalRaw(map[string]any{"text": "hi"})}, nil)

	memberMsg := waitForMessage(t, member.recvCh)
	if memberMsg.Session != "main" {
		t.Fatalf("expected session member message Session=main, got %q", memberMsg.Session)
	}
	if memberMsg.TenantID != tenant.DefaultID {
		t.Fatalf("expected session member message TenantID=%q, got %q", tenant.DefaultID, memberMsg.TenantID)
	}
	globalMsg := waitForMessage(t, global.recvCh)
	if globalMsg.Session != "main" {
		t.Fatalf("expected global receiver session=main, got %q", globalMsg.Session)
	}
	if globalMsg.TenantID != tenant.DefaultID {
		t.Fatalf("expected global receiver tenant=%q, got %q", tenant.DefaultID, globalMsg.TenantID)
	}
}

// wsURL returns the WebSocket URL for the test server.
func (e *testEnv) wsURL() string {
	return "ws" + strings.TrimPrefix(e.Server.URL, "http") + "/ws"
}

// dial opens a raw WS connection.
func (e *testEnv) dial() *websocket.Conn {
	e.t.Helper()
	conn, _, err := websocket.DefaultDialer.Dial(e.wsURL(), nil)
	if err != nil {
		e.t.Fatalf("dial failed: %v", err)
	}
	e.t.Cleanup(func() { conn.Close() })
	return conn
}

// connect dials + sends hello + reads hello_ack response.
func (e *testEnv) connect(name string, sends, receives []string) *websocket.Conn {
	conn, _ := e.connectAck(name, sends, receives)
	return conn
}

func (e *testEnv) connectAck(name string, sends, receives []string) (*websocket.Conn, Message) {
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
		}),
	})
	msg := readMsg(e.t, conn)
	if msg.Type != string(MsgHelloAck) {
		e.t.Fatalf("expected hello_ack, got %s", msg.Type)
	}
	return conn, msg
}

// connectAndJoin dials + connect + join.
func (e *testEnv) connectAndJoin(name, session string, sends, receives []string) *websocket.Conn {
	return e.connectAndJoinTenant(name, tenant.DefaultID, session, sends, receives)
}

func (e *testEnv) connectAndJoinTenant(name, tenantID, session string, sends, receives []string) *websocket.Conn {
	e.t.Helper()
	conn := e.connect(name, sends, receives)
	writeJSON(e.t, conn, Message{Type: "join", TenantID: tenantID, Session: session})
	msg := readMsg(e.t, conn)
	if msg.Type != "joined" {
		e.t.Fatalf("expected joined, got %s", msg.Type)
	}
	return conn
}

// disconnectClient closes the WebSocket connection and waits for cleanup.
func (e *testEnv) disconnectClient(name string) {
	e.t.Helper()
	for _, c := range e.Hub.clients.All() {
		if c.name == name {
			c.conn.Close()
			// Give the readPump goroutine time to clean up
			time.Sleep(50 * time.Millisecond)
			return
		}
	}
}

// writeJSON sends a JSON message on a WS connection.
func writeJSON(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	if msg, ok := v.(Message); ok && msg.V == 0 {
		msg.V = ProtocolVersion
		v = msg
	}
	if err := conn.WriteJSON(v); err != nil {
		t.Fatalf("writeJSON failed: %v", err)
	}
}

// readMsg reads one JSON message with a timeout.
func readMsg(t *testing.T, conn *websocket.Conn) Message {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("readMsg failed: %v", err)
	}
	return msg
}

// readMsgTimeout reads one JSON message with a custom timeout. Returns nil on timeout.
func readMsgTimeout(t *testing.T, conn *websocket.Conn, d time.Duration) *Message {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(d))
	var msg Message
	if err := conn.ReadJSON(&msg); err != nil {
		return nil
	}
	return &msg
}

func helloClientID(t *testing.T, msg Message) string {
	t.Helper()
	var data struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(msg.Data, &data); err != nil {
		t.Fatalf("invalid hello_ack data: %v", err)
	}
	if data.ClientID == "" {
		t.Fatalf("hello_ack missing client_id: %+v", msg)
	}
	return data.ClientID
}

func isUserMessage(msg *Message) bool {
	return msg != nil && msg.Type == string(MsgEvent) && msg.Topic == TopicMessageUser
}

func isSessionInit(msg *Message) bool {
	return msg != nil && msg.Type == string(MsgEvent) && msg.Topic == TopicSessionInit
}

func preferredRuntimeFromMeta(t *testing.T, raw json.RawMessage) string {
	t.Helper()
	var meta map[string]any
	if err := json.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("invalid meta: %v", err)
	}
	kernel, ok := meta["kernel"].(map[string]any)
	if !ok {
		t.Fatalf("missing kernel meta: %+v", meta)
	}
	runtimeID, _ := kernel[preferredRuntimeKernelMetaKey].(string)
	return runtimeID
}

func userMessage(text string) Message {
	return Message{Type: string(MsgEvent), Topic: TopicMessageUser, Data: mustMarshalRaw(map[string]any{"text": text})}
}

func toolCall(id, name string, input json.RawMessage) Message {
	return Message{Type: string(MsgRequest), Topic: TopicToolCall, ID: id, Name: name, Input: input}
}

func turnDone() Message {
	return Message{Type: string(MsgEvent), Topic: TopicTurnDone}
}

func turnCancel() Message {
	return Message{Type: string(MsgEvent), Topic: TopicTurnCancel}
}

func turnSteer(text string) Message {
	return Message{Type: string(MsgEvent), Topic: TopicTurnSteer, Data: mustMarshalRaw(map[string]any{"text": text})}
}

func isToolResult(msg any) bool {
	switch m := msg.(type) {
	case Message:
		return m.Type == string(MsgReply) && m.Topic == TopicToolResult
	case *Message:
		return m != nil && m.Type == string(MsgReply) && m.Topic == TopicToolResult
	case **Message:
		return m != nil && *m != nil && (*m).Type == string(MsgReply) && (*m).Topic == TopicToolResult
	default:
		return false
	}
}

func isToolCall(msg any) bool {
	switch m := msg.(type) {
	case Message:
		return m.Type == string(MsgRequest) && m.Topic == TopicToolCall
	case *Message:
		return m != nil && m.Type == string(MsgRequest) && m.Topic == TopicToolCall
	case **Message:
		return m != nil && *m != nil && (*m).Type == string(MsgRequest) && (*m).Topic == TopicToolCall
	default:
		return false
	}
}

// --- Tests ---

func TestConnectJoinHandshake(t *testing.T) {
	env := newTestEnv(t)

	// First client → c1
	conn1, ack1 := env.connectAck("client-a", []string{TopicMessageUser}, []string{TopicMessageUser})
	if clientID := helloClientID(t, ack1); clientID != "c1" {
		t.Errorf("expected c1, got %s", clientID)
	}
	writeJSON(t, conn1, Message{Type: "join", Session: "main"})
	msg1 := readMsg(t, conn1)
	if msg1.Type != "joined" || msg1.Session != "main" {
		t.Errorf("expected joined main, got type=%s session=%s", msg1.Type, msg1.Session)
	}

	_, ack2 := env.connectAck("client-b", []string{TopicMessageUser}, []string{TopicMessageUser})
	clientID := helloClientID(t, ack2)
	if clientID != "c2" {
		t.Errorf("expected c2, got %s", clientID)
	}

	_, ack3 := env.connectAck("client-c", []string{TopicMessageUser}, []string{TopicMessageUser})
	clientID = helloClientID(t, ack3)
	if clientID != "c3" {
		t.Errorf("expected c3, got %s", clientID)
	}
}

func TestConnectRequiresKernelClientToken(t *testing.T) {
	env := newTestEnv(t)

	missing := env.dial()
	writeJSON(t, missing, Message{
		V:    ProtocolVersion,
		Type: string(MsgHello),
		Data: mustMarshalRaw(map[string]any{
			"name":        "missing-token",
			"send_topics": []string{TopicMessageUser},
		}),
	})
	msg := readMsg(t, missing)
	if msg.Type != "error" {
		t.Fatalf("expected error for missing token, got %s", msg.Type)
	}
	if !strings.Contains(msg.Text, "invalid kernel client token") {
		t.Fatalf("unexpected missing-token error: %q", msg.Text)
	}

	wrong := env.dial()
	writeJSON(t, wrong, Message{
		V:    ProtocolVersion,
		Type: string(MsgHello),
		Data: mustMarshalRaw(map[string]any{
			"name":        "wrong-token",
			"send_topics": []string{TopicMessageUser},
			"auth_token":  "bad-token",
		}),
	})
	msg = readMsg(t, wrong)
	if msg.Type != "error" {
		t.Fatalf("expected error for wrong token, got %s", msg.Type)
	}
	if !strings.Contains(msg.Text, "invalid kernel client token") {
		t.Fatalf("unexpected wrong-token error: %q", msg.Text)
	}
}

func TestInitOnJoin(t *testing.T) {
	env := newTestEnv(t)

	// Client that receives init
	conn := env.connectAndJoin("driver", "main", []string{TopicMessageUser}, []string{TopicSessionInit, TopicMessageUser})
	init := readMsg(t, conn)
	if init.Type != string(MsgEvent) || init.Topic != TopicSessionInit {
		t.Fatalf("expected session.init, got %+v", init)
	}
	if init.Context != "" {
		t.Errorf("expected empty init context, got %q", init.Context)
	}
	if len(init.Tools) == 0 {
		t.Error("expected tools, got empty")
	}
}

func TestJoinPersistsSessionStateUnderTenantDir(t *testing.T) {
	env := newTestEnv(t)
	home := t.TempDir()
	env.Hub.SetTenantStore(tenant.NewMemoryStore(
		tenant.Tenant{ID: tenant.DefaultID, CreatedAt: time.Now()},
		tenant.Tenant{ID: "alpha", CreatedAt: time.Now()},
	))
	env.Hub.SetSessionStore(NewDiskSessionStore(home))

	conn := env.connect("driver", []string{TopicMessageUser}, []string{TopicSessionInit, TopicMessageUser})
	writeJSON(t, conn, Message{Type: "join", Session: "tenant-s1", TenantID: "alpha"})
	if msg := readMsg(t, conn); msg.Type != "joined" || msg.TenantID != "alpha" {
		t.Fatalf("expected joined alpha, got %+v", msg)
	}
	path := filepath.Join(home, "tenants", "alpha", "state", "sessions", "tenant-s1.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("session state file missing: %v", err)
	}
	env.disconnectClient("driver")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("session state file should be removed, err=%v", err)
	}
}

func TestRemovedKernelBuiltinsNotExposedOrExecutable(t *testing.T) {
	toolsJSON := json.RawMessage(`[{"name":"echo_tool","description":"echo stdin","params":{"text":{"type":"string","description":"text to echo"}},"required":[]}]`)
	hub := NewHub(toolsJSON, 3, 5, nil)
	hub.SetClientAuthToken("test-kernel-token")
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
	defer func() {
		hub.Shutdown()
		server.Close()
	}()
	env := &testEnv{Hub: hub, Server: server, t: t, Token: "test-kernel-token"}

	conn := env.connectAndJoin("driver", "main", []string{TopicToolCall}, []string{TopicSessionInit, TopicToolResult})
	init := readMsg(t, conn)
	if init.Type != string(MsgEvent) || init.Topic != TopicSessionInit {
		t.Fatalf("expected session.init, got %+v", init)
	}
	for _, removed := range []string{"shell_exec", "process_spawn", "process_kill", "process_list"} {
		if strings.Contains(string(init.Tools), `"`+removed+`"`) {
			t.Fatalf("unexpected removed kernel builtin %s leaked in init tools: %s", removed, string(init.Tools))
		}
	}

	for i, removed := range []string{"shell_exec", "process_spawn", "process_kill", "process_list"} {
		writeJSON(t, conn, toolCall(fmt.Sprintf("removed-%d", i), removed, json.RawMessage(`{}`)))
		result := readMsg(t, conn)
		if !isToolResult(&result) {
			t.Fatalf("%s: expected tool_result, got %s", removed, result.Type)
		}
		expected := "unknown tool " + removed
		if !strings.Contains(result.Output, expected) {
			t.Fatalf("%s: expected removed builtin to be rejected with %q, got %q", removed, expected, result.Output)
		}
	}
}

func TestInitIncludesRuntimeManifestLoadedTools(t *testing.T) {
	hub := NewHub(nil, 3, 5, nil)
	hub.syncRuntimeCapability("local", wire.Capability{
		Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "dynamic"},
		Tools:  []wire.ToolSpec{{Name: "testbed_dynamic_ping"}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	})
	hub.syncRuntimeCapability("local", wire.Capability{
		Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "recorder"},
		Tools:  []wire.ToolSpec{{Name: "testbed_hook_recorder_clear"}, {Name: "testbed_hook_recorder_events"}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	})
	raw := hub.initToolsJSON()
	if !strings.Contains(string(raw), `"testbed_dynamic_ping"`) || !strings.Contains(string(raw), `"testbed_hook_recorder_clear"`) || !strings.Contains(string(raw), `"testbed_hook_recorder_events"`) {
		t.Fatalf("runtime manifest-loaded tool missing from init tools: %s", string(raw))
	}
}

func TestInitSyncsAttachedRuntimeCapabilities(t *testing.T) {
	hub := NewHub(nil, 3, 5, nil)
	ensureRuntimeDefinitionsForTest(t, hub, RuntimeDefinition{ID: "local", Backend: "local"})
	conn := runtimemock.New().WithCapabilities(wire.Capability{
		Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "cron"},
		Tools:  []wire.ToolSpec{{Name: "cron_add"}, {Name: "cron_list"}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	})
	if err := hub.runtimes.RegisterHello("local", conn, nil, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}

	raw := hub.initToolsJSON()
	if !strings.Contains(string(raw), `"cron_add"`) || !strings.Contains(string(raw), `"cron_list"`) {
		t.Fatalf("init did not sync attached runtime capabilities: %s", string(raw))
	}
}

func TestRuntimeCatalogUpdateRefreshesJoinedClients(t *testing.T) {
	env := newTestEnv(t)
	env.Hub.runtimes = NewRuntimeRegistry()
	ensureRuntimeDefinitionsForTest(t, env.Hub, RuntimeDefinition{ID: "local", Backend: "local"})
	conn := runtimemock.New().WithCapabilities(wire.Capability{
		Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "mcp"},
		Tools:  []wire.ToolSpec{{Name: "mcp_call"}},
		State:  wire.CapabilityStateManifestLoaded,
		Source: wire.CapabilitySourceManifest,
	})
	if err := env.Hub.runtimes.RegisterHello("local", conn, nil, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}

	driver := env.connectAndJoin("driver", "main", []string{TopicMessageUser}, []string{TopicSessionInit})
	initial := readMsg(t, driver)
	if !isSessionInit(&initial) || !strings.Contains(string(initial.Tools), `"mcp_call"`) {
		t.Fatalf("expected initial mcp_call init tools, got %+v", initial)
	}

	if err := env.Hub.runtimeAsyncSink().CatalogUpdated("local", wire.CatalogUpdate{
		Op:       wire.OpCatalogUpdate,
		Target:   wire.Target{Kind: wire.TargetKindPlugin, ID: "mcp"},
		Tools:    []wire.ToolSpec{{Name: "mcp_call"}, {Name: "mcp__duckduckgo__search"}},
		Revision: 2,
		State:    wire.CapabilityStateReady,
		Source:   wire.CapabilitySourceWorker,
	}); err != nil {
		t.Fatalf("CatalogUpdated: %v", err)
	}

	updated := readMsg(t, driver)
	if !isSessionInit(&updated) || !strings.Contains(string(updated.Tools), `"mcp__duckduckgo__search"`) {
		t.Fatalf("expected refreshed init tools with dynamic MCP tool, got %+v", updated)
	}
}

func TestInitNotSentWithoutReceive(t *testing.T) {
	env := newTestEnv(t)

	// Client without init in receives
	conn := env.connectAndJoin("gateway", "main", []string{TopicMessageUser}, []string{TopicMessageUser})

	// Should NOT receive init — expect timeout
	msg := readMsgTimeout(t, conn, 200*time.Millisecond)
	if msg != nil {
		t.Errorf("expected no message, got %s", msg.Type)
	}
}

func TestMessageRouting(t *testing.T) {
	env := newTestEnv(t)

	// Client A: sender
	connA := env.connectAndJoin("sender", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	// Client B: receiver in same session
	connB := env.connectAndJoin("receiver", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	// Client C: different session
	connC := env.connectAndJoin("other-session", "sub-1", []string{TopicMessageUser}, []string{TopicMessageUser})
	// Client D: same session, does NOT receive TopicMessageUser
	connD := env.connectAndJoin("no-msg", "main", []string{TopicMessageUser}, []string{"error"})

	// A sends message
	writeJSON(t, connA, userMessage("hello"))
	time.Sleep(50 * time.Millisecond)

	// B should receive
	msgB := readMsgTimeout(t, connB, time.Second)
	if !isUserMessage(msgB) || messageText(msgB) != "hello" {
		t.Errorf("B: expected message 'hello', got %+v", msgB)
	}

	// A (sender) should NOT receive own message
	msgA := readMsgTimeout(t, connA, 200*time.Millisecond)
	if msgA != nil {
		t.Errorf("A: should not receive own message, got %s", msgA.Type)
	}

	// C (different session) should NOT receive
	msgC := readMsgTimeout(t, connC, 200*time.Millisecond)
	if msgC != nil {
		t.Errorf("C: should not receive cross-session, got %s", msgC.Type)
	}

	// D (no message in receives) should NOT receive
	msgD := readMsgTimeout(t, connD, 200*time.Millisecond)
	if msgD != nil {
		t.Errorf("D: should not receive without 'message' in receives, got %s", msgD.Type)
	}
}

func TestCrossSessionRouting(t *testing.T) {
	env := newTestEnv(t)

	// Client in "main" session
	connMain := env.connectAndJoin("main-client", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	// Client in "sub-1" session
	connSub := env.connectAndJoin("sub-client", "sub-1", []string{TopicMessageUser}, []string{TopicMessageUser})

	// Sub sends message with explicit session="main"
	crossSessionMessage := userMessage("result from sub")
	crossSessionMessage.Session = "main"
	writeJSON(t, connSub, crossSessionMessage)
	time.Sleep(50 * time.Millisecond)

	// Main should receive it
	msg := readMsgTimeout(t, connMain, time.Second)
	if !isUserMessage(msg) || messageText(msg) != "result from sub" {
		t.Errorf("main: expected 'result from sub', got %+v", msg)
	}

	// Sub should NOT receive it (it was routed to "main", not "sub-1")
	msgSub := readMsgTimeout(t, connSub, 200*time.Millisecond)
	if msgSub != nil {
		t.Errorf("sub: should not receive its own cross-session message, got %+v", msgSub)
	}
}

func TestSendsValidation(t *testing.T) {
	env := newTestEnv(t)

	// Client that can only send TopicMessageUser
	connSender := env.connectAndJoin("limited", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	// Receiver in same session
	connRecv := env.connectAndJoin("recv", "main", []string{TopicMessageUser}, []string{TopicStreamDelta, TopicMessageUser})

	// Try to send stream.delta (not in sends list) — should be ignored
	writeJSON(t, connSender, Message{Type: string(MsgEvent), Topic: TopicStreamDelta, Data: mustMarshalRaw(map[string]any{"text": "hacked"})})
	time.Sleep(50 * time.Millisecond)

	msg := readMsgTimeout(t, connRecv, 200*time.Millisecond)
	if msg != nil {
		t.Errorf("should not receive unauthorized message, got %s: %s", msg.Type, msg.Text)
	}
}

func TestReceivesGlobal(t *testing.T) {
	env := newTestEnv(t)

	// Client A: joined to "main" session, sends messages
	connA := env.connectAndJoin("sender", "main", []string{TopicMessageUser}, []string{TopicMessageUser})

	// Client B: NOT joined to any session, but has receives_global: [TopicMessageUser]
	connB := env.dial()
	writeJSON(t, connB, Message{
		V:    ProtocolVersion,
		Type: string(MsgHello),
		Data: mustMarshalRaw(map[string]any{
			"name":           "global-listener",
			"send_topics":    []string{},
			"receive_topics": []string{},
			"global_topics":  []string{TopicMessageUser},
			"auth_token":     env.Token,
		}),
	})
	msgConnected := readMsg(t, connB)
	if msgConnected.Type != string(MsgHelloAck) {
		t.Fatalf("expected hello_ack, got %s", msgConnected.Type)
	}

	// Client C: NOT joined, no receives_global — should NOT receive
	connC := env.connect("no-global", []string{}, []string{TopicMessageUser})

	// A sends message to its session ("main")
	writeJSON(t, connA, userMessage("hello global"))
	time.Sleep(50 * time.Millisecond)

	// B (receives_global) should receive it even though not in session
	msgB := readMsgTimeout(t, connB, time.Second)
	if !isUserMessage(msgB) || messageText(msgB) != "hello global" {
		t.Errorf("B: expected message 'hello global', got %+v", msgB)
	}

	// C (no receives_global, not joined) should NOT receive
	msgC := readMsgTimeout(t, connC, 200*time.Millisecond)
	if msgC != nil {
		t.Errorf("C: should not receive without receives_global, got %+v", msgC)
	}
}

func TestRoutedMessagesOverwriteKernelMeta(t *testing.T) {
	env := newTestEnv(t)
	sender := env.connectAndJoin("sender", "main", []string{TopicMessageUser}, []string{})
	receiver := env.connectAndJoin("receiver", "main", []string{}, []string{TopicMessageUser})

	msg := userMessage("hello")
	msg.Meta = mustMarshalRaw(map[string]any{
		"kernel": map[string]any{"sender": map[string]any{"id": "spoof"}},
		"user":   map[string]any{"source": "test"},
	})
	writeJSON(t, sender, msg)

	routed := readMsg(t, receiver)
	if !isUserMessage(&routed) || messageText(&routed) != "hello" {
		t.Fatalf("expected routed message, got %+v", routed)
	}
	var meta map[string]any
	if err := json.Unmarshal(routed.Meta, &meta); err != nil {
		t.Fatalf("invalid routed meta: %v", err)
	}
	user, ok := meta["user"].(map[string]any)
	if !ok || user["source"] != "test" {
		t.Fatalf("expected user meta to be preserved, got %+v", meta)
	}
	kernelMeta, ok := meta["kernel"].(map[string]any)
	if !ok {
		t.Fatalf("expected kernel meta, got %+v", meta)
	}
	senderMeta, ok := kernelMeta["sender"].(map[string]any)
	if !ok || senderMeta["name"] != "sender" || senderMeta["id"] == "spoof" {
		t.Fatalf("expected authenticated sender meta, got %+v", kernelMeta["sender"])
	}
	route, ok := kernelMeta["route"].(map[string]any)
	if !ok || route["scope"] != "session" || route["session"] != "main" || route["tenant_id"] != tenant.DefaultID {
		t.Fatalf("expected session route meta, got %+v", kernelMeta["route"])
	}
}

func TestUserMessagesGetTurnCorrelationID(t *testing.T) {
	env := newTestEnv(t)
	sender := env.connectAndJoin("sender", "main", []string{TopicMessageUser}, []string{})
	receiver := env.connectAndJoin("receiver", "main", []string{}, []string{TopicMessageUser})

	writeJSON(t, sender, userMessage("hello"))
	routed := readMsg(t, receiver)
	var meta map[string]any
	if err := json.Unmarshal(routed.Meta, &meta); err != nil {
		t.Fatalf("invalid routed meta: %v", err)
	}
	turnCorrelationID, _ := meta[turnCorrelationMetaKey].(string)
	if turnCorrelationID == "" {
		t.Fatalf("expected %s in routed meta, got %+v", turnCorrelationMetaKey, meta)
	}
	if !strings.HasPrefix(turnCorrelationID, "tc-") {
		t.Fatalf("expected generated turn correlation id, got %q", turnCorrelationID)
	}
}

func TestUserMessagesStampPreferredRuntimeAndQueuedTurnsUpdateOnDispatch(t *testing.T) {
	hub := NewHub(nil, 3, 5, nil)
	gatewayA := addCaptureClient(t, hub, "gateway-a", "main", nil, nil)
	gatewayA.meta = json.RawMessage(`{"tabula.runtime_id":"rt-aaaaaaaaaaaaaaaaaaaa"}`)
	gatewayB := addCaptureClient(t, hub, "gateway-b", "main", nil, nil)
	gatewayB.meta = json.RawMessage(`{"tabula.runtime_id":"rt-bbbbbbbbbbbbbbbbbbbb"}`)
	driver := addCaptureClient(t, hub, "driver", "main", []string{TopicMessageUser}, nil)
	driver.sends[TopicTurnDone] = true

	first := userMessage("first")
	hub.handleUserMessage(gatewayA, &first)
	routedFirst := waitForMessage(t, driver.recvCh)
	if got := preferredRuntimeFromMeta(t, routedFirst.Meta); got != "rt-aaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("first routed preferred runtime = %q", got)
	}
	if got := hub.sessionPreferredRuntime("default", "main"); got != "rt-aaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("session preferred runtime after first turn = %q", got)
	}

	second := userMessage("second")
	hub.handleUserMessage(gatewayB, &second)
	if got := hub.sessionPreferredRuntime("default", "main"); got != "rt-aaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("queued turn should not change active preferred runtime yet, got %q", got)
	}
	if msg := readCaptureMessageTimeout(driver.recvCh, 100*time.Millisecond); msg != nil {
		t.Fatalf("queued turn should not dispatch before turn.done, got %+v", msg)
	}

	hub.forwardSessionMessage(driver, &Message{Type: string(MsgEvent), Topic: TopicTurnDone, Session: "main", TenantID: tenant.DefaultID})
	routedSecond := waitForMessage(t, driver.recvCh)
	if got := preferredRuntimeFromMeta(t, routedSecond.Meta); got != "rt-bbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("queued routed preferred runtime = %q", got)
	}
	if got := hub.sessionPreferredRuntime("default", "main"); got != "rt-bbbbbbbbbbbbbbbbbbbb" {
		t.Fatalf("session preferred runtime after queued dispatch = %q", got)
	}
}

func TestTurnSteerStampsPreferredRuntimeMeta(t *testing.T) {
	hub := NewHub(nil, 3, 5, nil)
	gateway := addCaptureClient(t, hub, "gateway", "main", nil, nil)
	gateway.meta = json.RawMessage(`{"tabula.runtime_id":"rt-cccccccccccccccccccc"}`)
	driver := addCaptureClient(t, hub, "driver", "main", []string{TopicTurnSteer}, nil)

	steer := turnSteer("nudge")
	hub.handleTurnSteer(gateway, &steer)
	routed := waitForMessage(t, driver.recvCh)
	if routed.Topic != TopicTurnSteer {
		t.Fatalf("expected routed steer, got %+v", routed)
	}
	if got := preferredRuntimeFromMeta(t, routed.Meta); got != "rt-cccccccccccccccccccc" {
		t.Fatalf("steer preferred runtime = %q", got)
	}
	if got := hub.sessionPreferredRuntime("default", "main"); got != "rt-cccccccccccccccccccc" {
		t.Fatalf("session preferred runtime after steer = %q", got)
	}
}

func TestExchangeReplyRequiresChosenResponder(t *testing.T) {
	testExchangeReplyRequiresChosenResponder(t, TopicExchangeChoose)
	testExchangeReplyRequiresChosenResponder(t, TopicExchangeApprove)
}

func TestExchangeApproveHelperReachesTenantSessionResponder(t *testing.T) {
	hub := NewHub(nil, 3, 5, nil)
	hub.sessions.GetOrCreate("s1", "tenant-a")
	responder := addTenantCaptureClient(t, hub, "tenant-a", "responder", "s1", []string{TopicExchangeApprove}, nil)
	help := addTenantCaptureClient(t, hub, "tenant-a", "helper", "s1", []string{TopicExchangeApprove}, nil)
	responder.sends[TopicExchangeApprove] = true
	help.sends[TopicExchangeApprove] = true

	hub.handleExchangeRequest(help, &Message{Type: string(MsgRequest), Topic: TopicExchangeApprove, ID: "approve-1", Session: "s1", TenantID: "tenant-a"})

	request := waitForMessage(t, responder.recvCh)
	if request.Type != string(MsgRequest) || request.Topic != TopicExchangeApprove || request.ID != "approve-1" {
		t.Fatalf("expected approval request to responder, got %+v", request)
	}
	hub.handleExchangeReply(responder, &Message{Type: string(MsgReply), Topic: TopicExchangeApprove, ID: "approve-1", Data: mustMarshalRaw(map[string]any{"choice": "allow once", "index": 0})})
	reply := waitForMessage(t, help.recvCh)
	if reply.Type != string(MsgReply) || reply.Topic != TopicExchangeApprove || reply.ID != "approve-1" {
		t.Fatalf("expected approval reply to helper, got %+v", reply)
	}
}

func TestTenantSessionsWithSameIDAreIsolated(t *testing.T) {
	env := newTestEnv(t)
	env.Hub.SetTenantStore(tenant.NewMemoryStore(
		tenant.Tenant{ID: "alpha", CreatedAt: time.Now()},
		tenant.Tenant{ID: "beta", CreatedAt: time.Now()},
	))

	alphaSender := env.connectAndJoinTenant("alpha-sender", "alpha", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	alphaReceiver := env.connectAndJoinTenant("alpha-receiver", "alpha", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	betaReceiver := env.connectAndJoinTenant("beta-receiver", "beta", "main", []string{TopicMessageUser}, []string{TopicMessageUser})

	alphaSession, ok := env.Hub.sessions.Get("main", "alpha")
	if !ok {
		t.Fatal("expected alpha/main session")
	}
	betaSession, ok := env.Hub.sessions.Get("main", "beta")
	if !ok {
		t.Fatal("expected beta/main session")
	}
	if alphaSession == betaSession {
		t.Fatal("expected distinct live session objects per tenant")
	}

	writeJSON(t, alphaSender, Message{Type: string(MsgEvent), Topic: TopicMessageUser, Data: mustMarshalRaw(map[string]any{"text": "hello alpha"})})
	received := readMsg(t, alphaReceiver)
	if eventText(&received) != "hello alpha" {
		t.Fatalf("expected alpha receiver to get tenant message, got %+v", received)
	}
	if msg := readMsgTimeout(t, betaReceiver, 200*time.Millisecond); msg != nil {
		t.Fatalf("beta tenant received alpha message: %+v", msg)
	}

	var snapshot map[string]snapshotSessionInfo
	if err := json.Unmarshal(env.Hub.SnapshotSessions(), &snapshot); err != nil {
		t.Fatalf("invalid snapshot: %v", err)
	}
	if _, ok := snapshot["alpha/main"]; !ok {
		t.Fatalf("snapshot missing alpha/main: %#v", snapshot)
	}
	if _, ok := snapshot["beta/main"]; !ok {
		t.Fatalf("snapshot missing beta/main: %#v", snapshot)
	}
}

func testExchangeReplyRequiresChosenResponder(t *testing.T, topic string) {
	hub := NewHub(nil, 3, 5, nil)
	requester := addCaptureClient(t, hub, "requester", "main", []string{topic}, []string{topic, string(MsgError)})
	responder := addCaptureClient(t, hub, "responder", "main", []string{topic}, []string{topic, string(MsgError)})
	attacker := addCaptureClient(t, hub, "attacker", "main", []string{string(MsgError)}, []string{string(MsgError)})
	requester.sends[topic] = true
	responder.sends[topic] = true
	attacker.sends[topic] = true

	hub.HandleMessage(requester, &Message{
		V:     ProtocolVersion,
		Type:  string(MsgRequest),
		Topic: topic,
		ID:    "ex-1",
		Data:  mustMarshalRaw(map[string]any{"question": "Allow?", "options": []string{"yes", "no"}}),
	})

	req := waitForMessage(t, responder.recvCh)
	if req.Type != string(MsgRequest) || req.Topic != topic || req.ID != "ex-1" {
		t.Fatalf("expected exchange request for responder, got %+v", req)
	}
	if msg := readCaptureMessageTimeout(attacker.recvCh, 100*time.Millisecond); msg != nil {
		t.Fatalf("attacker should not receive exchange request, got %+v", msg)
	}

	hub.HandleMessage(attacker, &Message{V: ProtocolVersion, Type: string(MsgReply), Topic: topic, ID: "ex-1", Data: mustMarshalRaw(map[string]any{"choice": "yes"})})
	_ = readCaptureMessageTimeout(attacker.recvCh, 100*time.Millisecond) // spoof rejection is allowed but not required by callers.
	if msg := readCaptureMessageTimeout(requester.recvCh, 100*time.Millisecond); msg != nil {
		t.Fatalf("requester should not receive spoofed reply, got %+v", msg)
	}

	hub.HandleMessage(responder, &Message{V: ProtocolVersion, Type: string(MsgReply), Topic: topic, ID: "ex-1", Data: mustMarshalRaw(map[string]any{"choice": "yes", "index": 0})})
	reply := waitForMessage(t, requester.recvCh)
	if reply.Type != string(MsgReply) || reply.Topic != topic || reply.ID != "ex-1" {
		t.Fatalf("expected exchange reply for requester, got %+v", reply)
	}
}

func TestLateExchangeApprovalReplyAfterRequesterDisconnectIsRejected(t *testing.T) {
	hub := NewHub(nil, 3, 5, nil)
	approval := addCaptureClient(t, hub, "hook-approvals", "main", []string{TopicExchangeApprove, string(MsgError)}, []string{TopicExchangeApprove, string(MsgError)})
	ui := addCaptureClient(t, hub, "gateway-web", "main", []string{TopicExchangeApprove, string(MsgError)}, []string{TopicExchangeApprove, string(MsgError)})
	approval.sends[TopicExchangeApprove] = true
	ui.sends[TopicExchangeApprove] = true
	ui.meta = mustMarshalRaw(map[string]any{"tabula.client_role": "ui", "tabula.managed": true})

	hub.HandleMessage(approval, &Message{
		V:     ProtocolVersion,
		Type:  string(MsgRequest),
		Topic: TopicExchangeApprove,
		ID:    "approval-late-1",
		Data:  mustMarshalRaw(map[string]any{"question": "Allow exec_run?", "options": []string{"allow once", "deny once"}}),
	})
	req := waitForMessage(t, ui.recvCh)
	if req.Type != string(MsgRequest) || req.Topic != TopicExchangeApprove || req.ID != "approval-late-1" {
		t.Fatalf("expected approval exchange request for ui, got %+v", req)
	}

	// The approval hook/plugin gives up or disconnects while the UI still shows
	// the stale prompt. This deletes the pending exchange.
	hub.Unregister(approval)

	// A late browser reply for the old exchange id must be rejected instead of
	// being delivered to a new or missing requester.
	hub.HandleMessage(ui, &Message{
		V:     ProtocolVersion,
		Type:  string(MsgReply),
		Topic: TopicExchangeApprove,
		ID:    "approval-late-1",
		Data:  mustMarshalRaw(map[string]any{"choice": "allow once", "index": 0}),
	})
	errorMsg := waitForMessage(t, ui.recvCh)
	if errorMsg.Type != string(MsgError) || errorMsg.Text != "client not allowed to answer exchange" {
		t.Fatalf("expected stale approval reply rejection, got %+v", errorMsg)
	}
}

func TestPickExchangeResponderPrefersManagedUIClient(t *testing.T) {
	hub := NewHub(nil, 3, 5, nil)
	requester := addCaptureClient(t, hub, "requester", "main", []string{TopicExchangeChoose}, []string{TopicExchangeChoose, string(MsgError)})
	generic := addCaptureClient(t, hub, "generic", "main", []string{TopicExchangeChoose}, nil)
	ui := addCaptureClient(t, hub, "gateway-web", "main", []string{TopicExchangeChoose}, nil)
	requester.sends[TopicExchangeChoose] = true
	generic.sends[TopicExchangeChoose] = true
	ui.sends[TopicExchangeChoose] = true
	generic.id = 1
	ui.id = 2
	ui.meta = mustMarshalRaw(map[string]any{"tabula.client_role": "ui", "tabula.managed": true})

	hub.HandleMessage(requester, &Message{
		V:     ProtocolVersion,
		Type:  string(MsgRequest),
		Topic: TopicExchangeChoose,
		ID:    "ex-ui",
		Data:  mustMarshalRaw(map[string]any{"question": "Pick one", "options": []string{"yes", "no"}}),
	})

	req := waitForMessage(t, ui.recvCh)
	if req.ID != "ex-ui" || req.Topic != TopicExchangeChoose {
		t.Fatalf("expected managed UI responder to receive exchange, got %+v", req)
	}
	if msg := readCaptureMessageTimeout(generic.recvCh, 100*time.Millisecond); msg != nil {
		t.Fatalf("generic responder should not receive exchange when UI responder exists, got %+v", msg)
	}
}

func TestPickExchangeResponderPrefersNewestManagedUIClient(t *testing.T) {
	hub := NewHub(nil, 3, 5, nil)
	requester := addCaptureClient(t, hub, "requester", "main", []string{TopicExchangeChoose}, []string{TopicExchangeChoose, string(MsgError)})
	older := addCaptureClient(t, hub, "gateway-old", "main", []string{TopicExchangeChoose}, nil)
	newer := addCaptureClient(t, hub, "gateway-new", "main", []string{TopicExchangeChoose}, nil)
	requester.sends[TopicExchangeChoose] = true
	older.sends[TopicExchangeChoose] = true
	newer.sends[TopicExchangeChoose] = true
	older.id = 10
	newer.id = 11
	older.meta = mustMarshalRaw(map[string]any{"tabula.client_role": "ui", "tabula.managed": true})
	newer.meta = mustMarshalRaw(map[string]any{"tabula.client_role": "ui", "tabula.managed": true})

	hub.HandleMessage(requester, &Message{
		V:     ProtocolVersion,
		Type:  string(MsgRequest),
		Topic: TopicExchangeChoose,
		ID:    "ex-newest",
		Data:  mustMarshalRaw(map[string]any{"question": "Pick one", "options": []string{"yes", "no"}}),
	})

	req := waitForMessage(t, newer.recvCh)
	if req.ID != "ex-newest" || req.Topic != TopicExchangeChoose {
		t.Fatalf("expected newest managed UI responder to receive exchange, got %+v", req)
	}
	if msg := readCaptureMessageTimeout(older.recvCh, 100*time.Millisecond); msg != nil {
		t.Fatalf("older UI responder should not receive exchange when a newer one exists, got %+v", msg)
	}
}

func TestNotConnected(t *testing.T) {
	env := newTestEnv(t)

	// Dial raw WS, don't send connect — send message directly
	conn := env.dial()
	connRecv := env.connectAndJoin("recv", "main", []string{TopicMessageUser}, []string{TopicMessageUser})

	writeJSON(t, conn, userMessage("sneaky"))
	time.Sleep(50 * time.Millisecond)

	msg := readMsgTimeout(t, connRecv, 200*time.Millisecond)
	if msg != nil {
		t.Errorf("should not receive message from unconnected client, got %+v", msg)
	}
}

func TestNotJoined(t *testing.T) {
	env := newTestEnv(t)

	// Connect but don't join
	connSender := env.connect("no-join", []string{TopicMessageUser}, []string{TopicMessageUser})
	connRecv := env.connectAndJoin("recv", "main", []string{TopicMessageUser}, []string{TopicMessageUser})

	writeJSON(t, connSender, userMessage("no session"))
	time.Sleep(50 * time.Millisecond)

	msg := readMsgTimeout(t, connRecv, 200*time.Millisecond)
	if msg != nil {
		t.Errorf("should not receive message from unjoined client, got %+v", msg)
	}
}

func TestExecBasic(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, conn) // init

	writeJSON(t, conn, toolCall("t1", testShellToolName, json.RawMessage(`{"command":"echo hello"}`)))

	msg := readMsg(t, conn)
	if !isToolResult(&msg) || msg.ID != "t1" {
		t.Fatalf("expected tool_result t1, got type=%s id=%s", msg.Type, msg.ID)
	}
	if msg.Output != "hello" {
		t.Errorf("expected 'hello', got %q", msg.Output)
	}
}

func TestExecErrorExit(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, conn) // init

	writeJSON(t, conn, toolCall("t2", testShellToolName, json.RawMessage(`{"command":"exit 1"}`)))

	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "exit code 1") {
		t.Errorf("expected 'exit code 1' in output, got %q", msg.Output)
	}
}

func TestExecLargeOutputRequiresBeforeToolResultRewrite(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, conn) // init

	// Generate >16KB of output
	writeJSON(t, conn, toolCall("t3", testShellToolName, json.RawMessage(`{"command":"python3 -c \"print('A'*32768)\""}`)))

	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "before_tool_result") {
		t.Errorf("expected explicit before_tool_result rewrite error, got %q", msg.Output)
	}
}

func TestExecStderrMerged(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, conn) // init

	// The kernel wraps as: sh -c "COMMAND 2>&1"
	// Use a program that writes to stderr natively
	writeJSON(t, conn, toolCall("t4", testShellToolName, json.RawMessage(`{"command":"python3 -c \"import sys; sys.stderr.write('stderr_text\\n')\""}`)))

	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "stderr_text") {
		t.Errorf("expected stderr in output, got %q", msg.Output)
	}
}

func TestExecAsyncNonBlocking(t *testing.T) {
	env := newTestEnv(t)

	// Two clients in different sessions, each running EXEC simultaneously
	conn1 := env.connectAndJoin("driver-1", "sess-1",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, conn1) // init

	conn2 := env.connectAndJoin("driver-2", "sess-2",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, conn2) // init

	start := time.Now()

	// Fire both EXEC at the same time (sleep 0.5s each)
	writeJSON(t, conn1, toolCall("e1", testShellToolName, json.RawMessage(`{"command":"sleep 0.5 && echo done1"}`)))
	writeJSON(t, conn2, toolCall("e2", testShellToolName, json.RawMessage(`{"command":"sleep 0.5 && echo done2"}`)))

	msg1 := readMsg(t, conn1)
	msg2 := readMsg(t, conn2)
	elapsed := time.Since(start)

	if msg1.Output != "done1" {
		t.Errorf("conn1: expected 'done1', got %q", msg1.Output)
	}
	if msg2.Output != "done2" {
		t.Errorf("conn2: expected 'done2', got %q", msg2.Output)
	}
	// Both ran in parallel — total time should be ~0.5s, not ~1s
	if elapsed > 1500*time.Millisecond {
		t.Errorf("EXEC should be async: took %v (expected <1.5s)", elapsed)
	}
}

func TestUnknownTool(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, conn) // init

	writeJSON(t, conn, toolCall("u1", "UNKNOWN", json.RawMessage(`{}`)))
	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "unknown tool UNKNOWN") {
		t.Errorf("expected unknown tool error, got %q", msg.Output)
	}
}

func TestInvalidExecCommand(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, conn) // init

	// Missing command field
	writeJSON(t, conn, toolCall("x1", testShellToolName, json.RawMessage(`{}`)))
	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "ERROR") {
		t.Errorf("expected ERROR, got %q", msg.Output)
	}
}

func TestMultipleClientsInSession(t *testing.T) {
	env := newTestEnv(t)

	sender := env.connectAndJoin("sender", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	recv1 := env.connectAndJoin("recv1", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	recv2 := env.connectAndJoin("recv2", "main", []string{TopicMessageUser}, []string{TopicMessageUser})

	writeJSON(t, sender, userMessage("broadcast"))
	time.Sleep(50 * time.Millisecond)

	// Both receivers get the message
	m1 := readMsgTimeout(t, recv1, time.Second)
	m2 := readMsgTimeout(t, recv2, time.Second)
	if !isUserMessage(m1) || messageText(m1) != "broadcast" {
		t.Errorf("recv1: expected 'broadcast', got %+v", m1)
	}
	if !isUserMessage(m2) || messageText(m2) != "broadcast" {
		t.Errorf("recv2: expected 'broadcast', got %+v", m2)
	}

	// Sender does NOT receive
	ms := readMsgTimeout(t, sender, 200*time.Millisecond)
	if ms != nil {
		t.Errorf("sender should not receive own message, got %+v", ms)
	}
}

func TestClientDisconnect(t *testing.T) {
	env := newTestEnv(t)

	conn1 := env.connectAndJoin("will-disconnect", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	conn2 := env.connectAndJoin("stays", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	conn3 := env.connectAndJoin("sender", "main", []string{TopicMessageUser}, []string{TopicMessageUser})

	// Disconnect conn1
	conn1.Close()
	time.Sleep(100 * time.Millisecond)

	// Sending message after disconnect should not panic
	writeJSON(t, conn3, userMessage("after disconnect"))
	time.Sleep(50 * time.Millisecond)

	// conn2 should still receive
	msg := readMsgTimeout(t, conn2, time.Second)
	if !isUserMessage(msg) || messageText(msg) != "after disconnect" {
		t.Errorf("conn2: expected 'after disconnect', got %+v", msg)
	}
}

func TestSlowClientDropsMessages(t *testing.T) {
	env := newTestEnv(t)

	// Create a "slow" client that never reads
	slowConn := env.connectAndJoin("slow", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	sender := env.connectAndJoin("sender", "main", []string{TopicMessageUser}, []string{TopicMessageUser})
	fast := env.connectAndJoin("fast", "main", []string{TopicMessageUser}, []string{TopicMessageUser})

	// Stop reading from slow client (simulate slow consumer)
	// Just flood messages — sendCh is 64 deep, so 100 should overflow
	_ = slowConn // prevent GC

	for i := 0; i < 100; i++ {
		writeJSON(t, sender, userMessage(fmt.Sprintf("msg-%d", i)))
	}
	time.Sleep(100 * time.Millisecond)

	// Fast client should still receive messages (not blocked by slow client)
	received := 0
	for {
		msg := readMsgTimeout(t, fast, 500*time.Millisecond)
		if msg == nil {
			break
		}
		received++
	}
	if received == 0 {
		t.Error("fast client should have received messages")
	}
	if received < 50 {
		t.Errorf("fast client received only %d messages, expected most of 100", received)
	}
}

func TestConcurrentMessages(t *testing.T) {
	env := newTestEnv(t)

	// Create 10 clients in same session
	var clients []*websocket.Conn
	for i := 0; i < 10; i++ {
		c := env.connectAndJoin(
			fmt.Sprintf("client-%d", i), "main",
			[]string{TopicMessageUser}, []string{TopicMessageUser},
		)
		clients = append(clients, c)
	}

	// All send messages concurrently
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func(idx int, conn *websocket.Conn) {
			defer wg.Done()
			writeJSON(t, conn, userMessage(fmt.Sprintf("hello from %d", idx)))
		}(i, c)
	}
	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	// Each client should receive 9 messages (from the other 9 clients)
	for i, c := range clients {
		received := 0
		for {
			msg := readMsgTimeout(t, c, 500*time.Millisecond)
			if msg == nil {
				break
			}
			received++
		}
		if received != 9 {
			t.Errorf("client-%d: expected 9 messages, got %d", i, received)
		}
	}
}

func TestSessionBusyQueuesConcurrentRootMessagesAndDispatchesAfterDone(t *testing.T) {
	env := newTestEnv(t)
	gateway := env.connectAndJoin("gateway", "main",
		[]string{TopicMessageUser, TopicTurnCancel},
		[]string{"error"})
	driver := env.connectAndJoin("driver", "main",
		[]string{TopicTurnDone},
		[]string{TopicMessageUser, TopicTurnCancel})

	writeJSON(t, gateway, userMessage("first"))
	first := readMsg(t, driver)
	if !isUserMessage(&first) || messageText(&first) != "first" {
		t.Fatalf("expected first turn message, got %+v", first)
	}

	sess, ok := env.Hub.sessions.Get("main", "default")
	if !ok {
		t.Fatal("session main should exist")
	}
	if !sess.IsBusy() {
		t.Fatal("session should be busy after first root message")
	}

	writeJSON(t, gateway, turnCancel())
	cancel := readMsg(t, driver)
	if cancel.Type != string(MsgEvent) || cancel.Topic != TopicTurnCancel {
		t.Fatalf("expected cancel to reach driver, got %+v", cancel)
	}
	if !sess.CancelRequested() {
		t.Fatal("session should remember cancel request while turn is inflight")
	}

	writeJSON(t, gateway, userMessage("second"))
	if errMsg := readMsgTimeout(t, gateway, 100*time.Millisecond); errMsg != nil {
		t.Fatalf("expected queued message without busy error, got %+v", errMsg)
	}
	if got := sess.PendingInputCount(); got != 1 {
		t.Fatalf("pending input count = %d, want 1", got)
	}

	writeJSON(t, driver, turnDone())
	second := readMsg(t, driver)
	if !isUserMessage(&second) || messageText(&second) != "second" {
		t.Fatalf("expected queued second message after done, got %+v", second)
	}
	if !sess.IsBusy() {
		t.Fatal("session should stay busy while queued turn is dispatched")
	}
	if sess.CancelRequested() {
		t.Fatal("cancel state should reset before queued turn")
	}
	if got := sess.PendingInputCount(); got != 0 {
		t.Fatalf("pending input count = %d, want 0", got)
	}

	writeJSON(t, driver, turnDone())
	time.Sleep(50 * time.Millisecond)
	if sess.IsBusy() {
		t.Fatal("session should stop being busy after queued turn done")
	}

	writeJSON(t, gateway, userMessage("third"))
	third := readMsg(t, driver)
	if !isUserMessage(&third) || messageText(&third) != "third" {
		t.Fatalf("expected turn to resume after done, got %+v", third)
	}
}

func TestQueuedFollowUpBroadcastsBackToOriginalSenderWhenDispatched(t *testing.T) {
	hub := NewHub(nil, 3, 5, nil)
	gateway := addCaptureClient(t, hub, "gateway", "main", []string{TopicMessageUser}, nil)
	driver := addCaptureClient(t, hub, "driver", "main", []string{TopicMessageUser}, nil)
	queued := userMessage("queued")
	queued.Session = "main"
	queued.TenantID = "default"

	hub.dispatchQueuedInput("default", "main", queuedInput{message: &queued, exclude: gateway})

	queuedEcho := waitForMessage(t, gateway.recvCh)
	if !isUserMessage(queuedEcho) || messageText(queuedEcho) != "queued" {
		t.Fatalf("expected queued message echo for original sender, got %+v", queuedEcho)
	}
	queuedForDriver := waitForMessage(t, driver.recvCh)
	if !isUserMessage(queuedForDriver) || messageText(queuedForDriver) != "queued" {
		t.Fatalf("expected queued message for driver, got %+v", queuedForDriver)
	}
}

func TestTurnCancelClearsQueuedFollowUpMessages(t *testing.T) {
	env := newTestEnv(t)
	gateway := env.connectAndJoin("gateway", "main",
		[]string{TopicMessageUser, TopicTurnCancel},
		[]string{"error"})
	driver := env.connectAndJoin("driver", "main",
		[]string{TopicTurnDone},
		[]string{TopicMessageUser, TopicTurnCancel})

	writeJSON(t, gateway, userMessage("first"))
	_ = readMsg(t, driver)
	writeJSON(t, gateway, userMessage("queued"))

	sess, ok := env.Hub.sessions.Get("main", "default")
	if !ok {
		t.Fatal("session main should exist")
	}
	deadline := time.Now().Add(500 * time.Millisecond)
	for sess.PendingInputCount() != 1 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if got := sess.PendingInputCount(); got != 1 {
		t.Fatalf("pending input count before cancel = %d, want 1", got)
	}

	writeJSON(t, gateway, turnCancel())
	_ = readMsg(t, driver)
	if got := sess.PendingInputCount(); got != 0 {
		t.Fatalf("pending input count after cancel = %d, want 0", got)
	}

	writeJSON(t, driver, turnDone())
	if msg := readMsgTimeout(t, driver, 100*time.Millisecond); msg != nil {
		t.Fatalf("queued follow-up should not dispatch after cancel, got %+v", *msg)
	}
}

func TestTurnCancelUsesExplicitTargetSession(t *testing.T) {
	env := newTestEnv(t)
	gateway := env.connectAndJoin("gateway", "main", []string{TopicTurnCancel}, []string{"error"})
	driver := env.connectAndJoin("driver", "other", []string{TopicTurnDone}, []string{TopicTurnCancel})

	cancel := turnCancel()
	cancel.Session = "other"
	writeJSON(t, gateway, cancel)

	routed := readMsg(t, driver)
	if routed.Type != string(MsgEvent) || routed.Topic != TopicTurnCancel {
		t.Fatalf("expected explicit-session cancel to reach other driver, got %+v", routed)
	}
}

func TestQueuedMessagesPreserveEnvelope(t *testing.T) {
	env := newTestEnv(t)
	gateway := env.connectAndJoin("gateway", "main",
		[]string{TopicMessageUser},
		[]string{"error"})
	driver := env.connectAndJoin("driver", "main",
		[]string{TopicTurnDone},
		[]string{TopicMessageUser})

	writeJSON(t, gateway, userMessage("first"))
	_ = readMsg(t, driver)

	meta := json.RawMessage(`{"source":"timer","timer_id":"timer-1"}`)
	queuedInput := userMessage("queued")
	queuedInput.ID = "timer-1"
	queuedInput.Meta = meta
	writeJSON(t, gateway, queuedInput)
	writeJSON(t, driver, turnDone())

	queued := readMsg(t, driver)
	if queued.ID != "timer-1" || !isUserMessage(&queued) || messageText(&queued) != "queued" {
		t.Fatalf("queued message envelope not preserved: %+v meta=%s", queued, string(queued.Meta))
	}
	var gotMeta map[string]any
	if err := json.Unmarshal(queued.Meta, &gotMeta); err != nil {
		t.Fatalf("queued message meta is invalid: %v", err)
	}
	if gotMeta["source"] != "timer" || gotMeta["timer_id"] != "timer-1" {
		t.Fatalf("queued message user meta not preserved: %+v", gotMeta)
	}
	if _, ok := gotMeta["kernel"].(map[string]any); !ok {
		t.Fatalf("queued message missing kernel meta: %+v", gotMeta)
	}
}

func TestTurnSteerBypassesSessionQueueDuringActiveTurn(t *testing.T) {
	env := newTestEnv(t)
	gateway := env.connectAndJoin("gateway", "main",
		[]string{TopicMessageUser, TopicTurnSteer},
		[]string{"error"})
	driver := env.connectAndJoin("driver", "main",
		[]string{TopicTurnDone},
		[]string{TopicMessageUser, TopicTurnSteer})

	writeJSON(t, gateway, userMessage("first"))
	_ = readMsg(t, driver)

	steer := turnSteer("steer now")
	steer.ID = "steer-1"
	writeJSON(t, gateway, steer)

	routed := readMsg(t, driver)
	if routed.Type != string(MsgEvent) || routed.Topic != TopicTurnSteer || routed.ID != "steer-1" || messageText(&routed) != "steer now" {
		t.Fatalf("expected steer to bypass queue, got %+v", routed)
	}

	writeJSON(t, driver, turnDone())
	if msg := readMsgTimeout(t, driver, 100*time.Millisecond); msg != nil {
		t.Fatalf("steer should not be redispatched after turn.done, got %+v", *msg)
	}
}

func TestTurnSteerWaitsForActiveToolCallToFinish(t *testing.T) {
	hub := NewHub(nil, 3, 5, nil)
	gateway := addTenantCaptureClient(t, hub, "default", "gateway", "main", nil, nil)
	driver := addTenantCaptureClient(t, hub, "default", "driver", "main", []string{TopicTurnSteer}, nil)

	hub.recordToolStarted("default", "main", "tool-1", "subagent_spawn")
	steer := turnSteer("after tool")
	steer.ID = "steer-1"
	hub.handleTurnSteer(gateway, &steer)

	if msg := readCaptureMessageTimeout(driver.recvCh, 100*time.Millisecond); msg != nil {
		t.Fatalf("steer should wait while tool call is active, got %+v", msg)
	}

	hub.recordToolTerminal("default", "main", "tool-1", "subagent_spawn", "completed")
	msg := waitForMessage(t, driver.recvCh)
	if msg.Topic != TopicTurnSteer || msg.ID != "steer-1" || messageText(msg) != "after tool" {
		t.Fatalf("expected deferred steer after tool terminal, got %+v", msg)
	}
}

func TestExecWithSpecialCharacters(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, conn) // init

	writeJSON(t, conn, toolCall("t5", testShellToolName, json.RawMessage(`{"command":"echo 'hello world'"}`)))

	msg := readMsg(t, conn)
	if msg.Output != "hello world" {
		t.Errorf("expected 'hello world', got %q", msg.Output)
	}
}

func TestToolResultRoutedToCorrectSession(t *testing.T) {
	env := newTestEnv(t)

	// Session A
	connA := env.connectAndJoin("driver-a", "sess-a",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, connA) // init

	// Session B
	connB := env.connectAndJoin("driver-b", "sess-b",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, connB) // init

	// EXEC from session A
	writeJSON(t, connA, toolCall("ta1", testShellToolName, json.RawMessage(`{"command":"echo from_a"}`)))

	// A should receive result
	msgA := readMsg(t, connA)
	if msgA.Output != "from_a" {
		t.Errorf("A: expected 'from_a', got %q", msgA.Output)
	}

	// B should NOT receive A's result
	msgB := readMsgTimeout(t, connB, 500*time.Millisecond)
	if msgB != nil {
		t.Errorf("B should not receive A's tool_result, got %+v", msgB)
	}
}

func TestGoroutineLeaks(t *testing.T) {
	before := runtime.NumGoroutine()

	env := newTestEnv(t)
	for i := 0; i < 5; i++ {
		c := env.connectAndJoin(fmt.Sprintf("c%d", i), "main",
			[]string{TopicMessageUser}, []string{TopicMessageUser})
		c.Close()
	}
	time.Sleep(500 * time.Millisecond)

	env.Hub.Shutdown()
	env.Server.Close()
	time.Sleep(500 * time.Millisecond)

	after := runtime.NumGoroutine()
	leaked := after - before
	if leaked > 5 {
		t.Errorf("possible goroutine leak: %d before, %d after (leaked %d)", before, after, leaked)
	}
}

// TestParallelToolUseSameSession simulates the LLM sending multiple tool.call
// requests from a single session in quick succession (batch tool calls).
// All results must come back with correct IDs and none should be lost.
func TestParallelToolUseSameSession(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, conn) // init

	n := 5
	// Fire N EXEC tool.call requests as fast as possible (simulates batch from LLM)
	for i := range n {
		writeJSON(t, conn, toolCall(fmt.Sprintf("batch-%d", i), testShellToolName, json.RawMessage(fmt.Sprintf(`{"command":"sleep 0.3 && echo result-%d"}`, i))))
	}

	// Collect all N results
	results := make(map[string]string) // id → output
	for range n {
		msg := readMsg(t, conn)
		if !isToolResult(&msg) {
			t.Fatalf("expected tool_result, got %s", msg.Type)
		}
		results[msg.ID] = msg.Output
	}

	// Verify all IDs present and outputs correct
	for i := range n {
		id := fmt.Sprintf("batch-%d", i)
		expected := fmt.Sprintf("result-%d", i)
		output, ok := results[id]
		if !ok {
			t.Errorf("missing result for %s", id)
		} else if output != expected {
			t.Errorf("%s: expected %q, got %q", id, expected, output)
		}
	}
}

// TestLongRunningExecBatch simulates multiple slow EXEC calls (like subagent work)
// running in parallel from the same session. Verifies all complete concurrently.
func TestLongRunningExecBatch(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{TopicToolCall},
		[]string{TopicSessionInit, TopicToolResult})
	readMsg(t, conn) // init

	n := 4
	start := time.Now()

	// Fire N slow EXEC commands (each sleeps 0.5s)
	for i := range n {
		writeJSON(t, conn, toolCall(fmt.Sprintf("slow-%d", i), testShellToolName, json.RawMessage(fmt.Sprintf(`{"command":"sleep 0.5 && echo done-%d"}`, i))))
	}

	// Collect all results
	results := make(map[string]string)
	for range n {
		msg := readMsg(t, conn)
		results[msg.ID] = msg.Output
	}
	elapsed := time.Since(start)

	// All should have completed
	for i := range n {
		id := fmt.Sprintf("slow-%d", i)
		expected := fmt.Sprintf("done-%d", i)
		if results[id] != expected {
			t.Errorf("%s: expected %q, got %q", id, expected, results[id])
		}
	}

	// If truly parallel: ~0.5s. If serial: ~2s. Allow up to 1.5s.
	if elapsed > 1500*time.Millisecond {
		t.Errorf("batch EXEC should run in parallel: %d commands took %v (expected <1.5s)", n, elapsed)
	}
	t.Logf("%d parallel EXEC completed in %v", n, elapsed)
}

// TestToolResultsNotLeakedAcrossSessions verifies that when two sessions run
// EXEC simultaneously, results go only to the correct session.
func TestToolResultsNotLeakedAcrossSessions(t *testing.T) {
	env := newTestEnv(t)

	connA := env.connectAndJoin("driver-a", "sess-a",
		[]string{TopicToolCall}, []string{TopicSessionInit, TopicToolResult})
	readMsg(t, connA) // init

	connB := env.connectAndJoin("driver-b", "sess-b",
		[]string{TopicToolCall}, []string{TopicSessionInit, TopicToolResult})
	readMsg(t, connB) // init

	// Both sessions fire EXEC at the same time
	writeJSON(t, connA, toolCall("a1", testShellToolName, json.RawMessage(`{"command":"sleep 0.2 && echo from-a"}`)))
	writeJSON(t, connB, toolCall("b1", testShellToolName, json.RawMessage(`{"command":"sleep 0.2 && echo from-b"}`)))

	// Each session should get exactly one result, with the correct content
	msgA := readMsg(t, connA)
	if msgA.ID != "a1" || msgA.Output != "from-a" {
		t.Errorf("sess-a: expected a1/from-a, got id=%s output=%q", msgA.ID, msgA.Output)
	}
	msgB := readMsg(t, connB)
	if msgB.ID != "b1" || msgB.Output != "from-b" {
		t.Errorf("sess-b: expected b1/from-b, got id=%s output=%q", msgB.ID, msgB.Output)
	}

	// Verify no cross-leak
	leakA := readMsgTimeout(t, connA, 500*time.Millisecond)
	if leakA != nil {
		t.Errorf("sess-a got extra message: %+v", leakA)
	}
	leakB := readMsgTimeout(t, connB, 500*time.Millisecond)
	if leakB != nil {
		t.Errorf("sess-b got extra message: %+v", leakB)
	}
}

func TestMaxClients(t *testing.T) {
	env := newTestEnv(t)
	env.Hub.MaxClients = 2

	// First two clients should succeed
	conn1 := env.dial()
	NewClient(env.Hub, conn1)
	conn2 := env.dial()
	NewClient(env.Hub, conn2)

	// Third should be rejected (connection closed by server)
	conn3 := env.dial()
	c := NewClient(env.Hub, conn3)
	if c != nil {
		t.Error("expected nil client when at capacity")
	}
}

// --- Protocol version and message validation tests ---

func TestProtocolVersionInConnectedResponse(t *testing.T) {
	env := newTestEnv(t)
	_, msg := env.connectAck("versioned-client", []string{TopicMessageUser}, []string{TopicSessionInit})
	if msg.Type != string(MsgHelloAck) {
		t.Fatalf("expected hello_ack, got %s", msg.Type)
	}
	if msg.V != ProtocolVersion {
		t.Errorf("expected version %d, got %d", ProtocolVersion, msg.V)
	}
}

func TestLegacyClientRejected(t *testing.T) {
	env := newTestEnv(t)
	conn := env.dial()
	// Version 0 (omitted) — must be rejected; clients must declare PROTOCOL_VERSION.
	writeRawJSON(t, conn, map[string]any{
		"type": string(MsgHello),
		"data": map[string]any{
			"name":        "legacy-client",
			"send_topics": []string{TopicMessageUser},
		},
	})
	msg := readMsg(t, conn)
	if msg.Type != "error" {
		t.Fatalf("expected error for missing protocol version, got %s", msg.Type)
	}
	if !strings.Contains(msg.Text, "unsupported protocol version") {
		t.Errorf("unexpected error text: %q", msg.Text)
	}
}

func writeRawJSON(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	if err := conn.WriteJSON(v); err != nil {
		t.Fatalf("writeJSON failed: %v", err)
	}
}

func TestUnsupportedProtocolVersionRejected(t *testing.T) {
	env := newTestEnv(t)
	conn := env.dial()
	writeJSON(t, conn, Message{
		V:    999, // future incompatible version
		Type: string(MsgHello),
		Data: mustMarshalRaw(map[string]any{
			"name":        "future-client",
			"send_topics": []string{TopicMessageUser},
			"auth_token":  env.Token,
		}),
	})
	msg := readMsg(t, conn)
	if msg.Type != "error" {
		t.Fatalf("expected error for unsupported version, got %s", msg.Type)
	}
	if !strings.Contains(msg.Text, "unsupported protocol version") {
		t.Errorf("unexpected error text: %q", msg.Text)
	}
}

func TestValidateMessageMissingType(t *testing.T) {
	err := validateMessage(&Message{})
	if err == nil {
		t.Fatal("expected error for missing type")
	}
	if !strings.Contains(err.Error(), "missing message type") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateHelloMissingName(t *testing.T) {
	err := validateMessage(&Message{V: ProtocolVersion, Type: string(MsgHello), Data: mustMarshalRaw(map[string]any{})})
	if err == nil {
		t.Fatal("expected error for hello without name")
	}
	if !strings.Contains(err.Error(), "missing name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateJoinMissingSession(t *testing.T) {
	err := validateMessage(&Message{V: ProtocolVersion, Type: "join"})
	if err == nil {
		t.Fatal("expected error for join without session")
	}
	if !strings.Contains(err.Error(), "missing session") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateToolCallMissingFields(t *testing.T) {
	err := validateMessage(&Message{V: ProtocolVersion, Type: string(MsgRequest), Topic: TopicToolCall})
	if err == nil {
		t.Fatal("expected error for tool.call without id/name")
	}
	if !strings.Contains(err.Error(), "missing id") {
		t.Errorf("unexpected error: %v", err)
	}

	err = validateMessage(&Message{V: ProtocolVersion, Type: string(MsgRequest), Topic: TopicToolCall, ID: "t1"})
	if err == nil {
		t.Fatal("expected error for tool.call without name")
	}
	if !strings.Contains(err.Error(), "missing name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateHookReplyMissingFields(t *testing.T) {
	err := validateMessage(&Message{V: ProtocolVersion, Type: "hook_reply"})
	if err == nil {
		t.Fatal("expected error for hook_reply without id/action")
	}
	if !strings.Contains(err.Error(), "missing id") {
		t.Errorf("unexpected error: %v", err)
	}

	err = validateMessage(&Message{V: ProtocolVersion, Type: "hook_reply", ID: "h1"})
	if err == nil {
		t.Fatal("expected error for hook_reply without action")
	}
	if !strings.Contains(err.Error(), "missing action") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateValidMessages(t *testing.T) {
	valid := []*Message{
		{V: ProtocolVersion, Type: string(MsgHello), Data: mustMarshalRaw(map[string]any{"name": "test", "send_topics": []string{TopicMessageUser}})},
		{V: ProtocolVersion, Type: "join", Session: "main"},
		{V: ProtocolVersion, Type: string(MsgEvent), Topic: TopicMessageUser, Data: mustMarshalRaw(map[string]any{"text": "hello"})},
		{V: ProtocolVersion, Type: string(MsgRequest), Topic: TopicToolCall, ID: "t1", Name: "shell_exec"},
		{V: ProtocolVersion, Type: "hook_reply", ID: "h1", Action: "pass"},
		{V: ProtocolVersion, Type: string(MsgEvent), Topic: TopicTurnDone},
		{V: ProtocolVersion, Type: string(MsgEvent), Topic: TopicTurnCancel},
	}
	for _, msg := range valid {
		if err := validateMessage(msg); err != nil {
			t.Errorf("expected valid message %+v, got error: %v", msg, err)
		}
	}
}
