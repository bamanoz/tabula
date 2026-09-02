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
	"sync/atomic"
	"testing"
	"time"

	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	runtimeconfig "github.com/bamanoz/tabula/internal/runtime/registryconfig"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
	"github.com/gorilla/websocket"
)

// --- Test helper ---

var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const (
	testShellToolName  = "test_shell"
	testExtensionTopic = "test.message"
)

var testExtensionCommandID atomic.Uint64
var testWebSocketScopes sync.Map

type testWebSocketScope struct {
	tenantID string
	session  string
}

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
	hub := NewHub(toolsJSON, nil)
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

	env.Hub.broadcastToSession("default", "main", TopicStreamDelta, &BusMessage{Type: string(MsgEvent), Topic: TopicStreamDelta, Data: mustMarshalRaw(map[string]any{"text": "hi"})}, nil)

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

// connect opens an authenticated protocol v4 connection.
func (e *testEnv) connect(name string, sends, receives []string) *websocket.Conn {
	conn, _ := e.connectWithID(name, sends, receives)
	return conn
}

func (e *testEnv) connectWithID(name string, sends, receives []string) (*websocket.Conn, string) {
	return e.connectOpen(name, sends, receives, nil, nil, map[string]any{})
}

func (e *testEnv) connectOpen(name string, sends, receives, receivesGlobal []string, hooks []khooks.Subscription, meta map[string]any) (*websocket.Conn, string) {
	e.t.Helper()
	conn := e.dial()
	requestID := fmt.Sprintf("test-open-%d", testExtensionCommandID.Add(1))
	if err := conn.WriteJSON(ClientEnvelope{
		V: ClientProtocolVersion, Kind: "command", Op: "connection.open", ID: requestID,
		Data: mustMarshalRaw(map[string]any{
			"name": name, "send_topics": sends, "receive_topics": receives,
			"receive_global_topics": receivesGlobal, "hooks": hooks,
			"auth_token": e.Token, "meta": meta,
		}),
	}); err != nil {
		e.t.Fatalf("connection.open failed: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var envelope ClientEnvelope
	if err := conn.ReadJSON(&envelope); err != nil {
		e.t.Fatalf("connection.open read failed: %v", err)
	}
	if envelope.Kind != "result" || envelope.Op != "connection.open" || envelope.ID != requestID {
		e.t.Fatalf("expected connection.open result, got %+v", envelope)
	}
	var data struct {
		ClientID string `json:"client_id"`
	}
	if err := json.Unmarshal(envelope.Data, &data); err != nil {
		e.t.Fatalf("invalid connection.open result: %v", err)
	}
	return conn, data.ClientID
}

// connectAndJoin dials + connect + join.
func (e *testEnv) connectAndJoin(name, session string, sends, receives []string) *websocket.Conn {
	return e.connectAndJoinTenant(name, tenant.DefaultID, session, sends, receives)
}

func (e *testEnv) connectAndJoinTenant(name, tenantID, session string, sends, receives []string) *websocket.Conn {
	e.t.Helper()
	conn := e.connect(name, sends, receives)
	testWebSocketScopes.Store(conn, testWebSocketScope{tenantID: tenantID, session: session})
	writeJSON(e.t, conn, BusMessage{Type: "join", TenantID: tenantID, Session: session})
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
	if msg, ok := v.(BusMessage); ok {
		scope, _ := testWebSocketScopes.Load(conn)
		bound, _ := scope.(testWebSocketScope)
		if msg.TenantID == "" {
			msg.TenantID = bound.tenantID
			if msg.TenantID == "" {
				msg.TenantID = tenant.DefaultID
			}
		}
		if msg.Session == "" {
			msg.Session = bound.session
		}
		if BusMessageType(msg.Type) == MsgJoin {
			testWebSocketScopes.Store(conn, testWebSocketScope{tenantID: msg.TenantID, session: msg.Session})
		}
		payload := map[string]any{}
		raw, err := json.Marshal(msg)
		if err != nil || json.Unmarshal(raw, &payload) != nil {
			t.Fatalf("encode extension message failed: %v", err)
		}
		delete(payload, "tenant_id")
		delete(payload, "session")
		v = ClientEnvelope{
			V: ClientProtocolVersion, Kind: "command", Op: "extension.send",
			ID:       fmt.Sprintf("test-ext-%d", testExtensionCommandID.Add(1)),
			TenantID: msg.TenantID, SessionID: msg.Session, Data: mustMarshalRaw(payload),
		}
	}
	if err := conn.WriteJSON(v); err != nil {
		t.Fatalf("writeJSON failed: %v", err)
	}
}

// readMsg reads one extension message with a timeout.
func readMsg(t *testing.T, conn *websocket.Conn) BusMessage {
	t.Helper()
	msg := readMsgTimeout(t, conn, 5*time.Second)
	if msg == nil {
		t.Fatal("readMsg timed out")
	}
	return *msg
}

// readMsgTimeout reads one extension message with a custom timeout. Returns nil on timeout.
func readMsgTimeout(t *testing.T, conn *websocket.Conn, d time.Duration) *BusMessage {
	t.Helper()
	deadline := time.Now().Add(d)
	for {
		_ = conn.SetReadDeadline(deadline)
		var envelope ClientEnvelope
		if err := conn.ReadJSON(&envelope); err != nil {
			return nil
		}
		if envelope.Kind == "result" && envelope.Op == "extension.send" {
			continue
		}
		if envelope.Kind != "event" || envelope.Op != "extension.event" {
			msg := &BusMessage{Type: envelope.Kind, Topic: envelope.Op, ID: envelope.ID, TenantID: envelope.TenantID, Session: envelope.SessionID, Data: envelope.Data}
			if envelope.Kind == "error" {
				var data struct {
					Message string `json:"message"`
				}
				_ = json.Unmarshal(envelope.Data, &data)
				msg.Text = data.Message
			}
			return msg
		}
		var msg BusMessage
		if err := json.Unmarshal(envelope.Data, &msg); err != nil {
			t.Fatalf("decode extension event failed: %v", err)
		}
		msg.TenantID = envelope.TenantID
		msg.Session = envelope.SessionID
		if msg.ID == "" {
			msg.ID = envelope.ID
		}
		return &msg
	}
}

func isUserMessage(msg *BusMessage) bool {
	return msg != nil && msg.Type == string(MsgEvent) && msg.Topic == testExtensionTopic
}

func isSessionInit(msg *BusMessage) bool {
	return msg != nil && msg.Type == string(MsgEvent) && msg.Topic == TopicSessionInit
}

func userMessage(text string) BusMessage {
	return BusMessage{Type: string(MsgEvent), Topic: testExtensionTopic, Data: mustMarshalRaw(map[string]any{"text": text})}
}

func toolCall(id, name string, input json.RawMessage) BusMessage {
	return BusMessage{Type: string(MsgRequest), Topic: TopicToolCall, ID: id, Name: name, Input: input}
}

func isToolResult(msg any) bool {
	switch m := msg.(type) {
	case BusMessage:
		return m.Type == string(MsgReply) && m.Topic == TopicToolResult
	case *BusMessage:
		return m != nil && m.Type == string(MsgReply) && m.Topic == TopicToolResult
	case **BusMessage:
		return m != nil && *m != nil && (*m).Type == string(MsgReply) && (*m).Topic == TopicToolResult
	default:
		return false
	}
}

// --- Tests ---

func TestConnectionOpenAndExtensionJoin(t *testing.T) {
	env := newTestEnv(t)

	// First client → c1
	conn1, clientID := env.connectWithID("client-a", []string{testExtensionTopic}, []string{testExtensionTopic})
	if clientID != "c1" {
		t.Errorf("expected c1, got %s", clientID)
	}
	writeJSON(t, conn1, BusMessage{Type: "join", Session: "main"})
	msg1 := readMsg(t, conn1)
	if msg1.Type != "joined" || msg1.Session != "main" {
		t.Errorf("expected joined main, got type=%s session=%s", msg1.Type, msg1.Session)
	}

	_, clientID = env.connectWithID("client-b", []string{testExtensionTopic}, []string{testExtensionTopic})
	if clientID != "c2" {
		t.Errorf("expected c2, got %s", clientID)
	}

	_, clientID = env.connectWithID("client-c", []string{testExtensionTopic}, []string{testExtensionTopic})
	if clientID != "c3" {
		t.Errorf("expected c3, got %s", clientID)
	}
}

func TestConnectRequiresKernelClientToken(t *testing.T) {
	env := newTestEnv(t)
	for name, token := range map[string]string{"missing-token": "", "wrong-token": "bad-token"} {
		t.Run(name, func(t *testing.T) {
			conn := env.dial()
			if err := conn.WriteJSON(ClientEnvelope{
				V: ClientProtocolVersion, Kind: "command", Op: "connection.open", ID: name,
				Data: mustMarshalRaw(map[string]any{"name": name, "auth_token": token, "meta": map[string]any{}}),
			}); err != nil {
				t.Fatalf("write connection.open: %v", err)
			}
			msg := readMsg(t, conn)
			if msg.Type != "error" || msg.Topic != "connection.open" {
				t.Fatalf("expected connection.open error, got %+v", msg)
			}
			if !strings.Contains(msg.Text, "client authentication failed") {
				t.Fatalf("unexpected authentication error: %q", msg.Text)
			}
		})
	}
}

func TestInitOnJoin(t *testing.T) {
	env := newTestEnv(t)

	// Client that receives init
	conn := env.connectAndJoin("driver", "main", []string{testExtensionTopic}, []string{TopicSessionInit, testExtensionTopic})
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

	conn := env.connect("driver", []string{testExtensionTopic}, []string{TopicSessionInit, testExtensionTopic})
	writeJSON(t, conn, BusMessage{Type: "join", Session: "tenant-s1", TenantID: "alpha"})
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
	hub := NewHub(toolsJSON, nil)
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
	hub := NewHub(nil, nil)
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

func TestInitToolsPreserveFullRuntimeSchema(t *testing.T) {
	hub := NewHub(nil, nil)
	schema := json.RawMessage(`{"type":"object","properties":{"server":{"type":"string","description":"Configured MCP server name."}},"required":["server"],"additionalProperties":false}`)
	hub.syncRuntimeCapability("local", wire.Capability{
		Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "mcp"},
		Tools: []wire.ToolSpec{{
			Name:        "mcp_list_tools",
			Description: "List tools exposed by a single MCP server.",
			Schema:      schema,
		}},
		State:  wire.CapabilityStateReady,
		Source: wire.CapabilitySourceWorker,
	})

	var tools []map[string]any
	if err := json.Unmarshal(hub.initToolsJSON(), &tools); err != nil {
		t.Fatalf("unmarshal init tools: %v", err)
	}
	if len(tools) != 1 {
		t.Fatalf("expected one tool, got %#v", tools)
	}
	tool := tools[0]
	if got := tool["required"]; fmt.Sprint(got) != "[server]" {
		t.Fatalf("expected legacy required field to remain, got %#v", got)
	}
	decoded, ok := tool["schema"].(map[string]any)
	if !ok {
		t.Fatalf("expected full schema in init tool, got %#v", tool)
	}
	if decoded["additionalProperties"] != false {
		t.Fatalf("expected full schema to preserve additionalProperties=false, got %#v", decoded)
	}
	properties, ok := decoded["properties"].(map[string]any)
	if !ok {
		t.Fatalf("expected schema properties, got %#v", decoded)
	}
	if _, ok := properties["server"].(map[string]any); !ok {
		t.Fatalf("expected server property schema, got %#v", properties)
	}
}

func TestInitSyncsAttachedRuntimeCapabilities(t *testing.T) {
	hub := NewHub(nil, nil)
	ensureRuntimeDefinitionsForTest(t, hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
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
	ensureRuntimeDefinitionsForTest(t, env.Hub, runtimeconfig.Definition{ID: "local", Backend: "local"})
	conn := runtimemock.New().WithCapabilities(wire.Capability{
		Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "mcp"},
		Tools:  []wire.ToolSpec{{Name: "mcp_call"}},
		State:  wire.CapabilityStateManifestLoaded,
		Source: wire.CapabilitySourceManifest,
	})
	if err := env.Hub.runtimes.RegisterHello("local", conn, nil, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}

	driver := env.connectAndJoin("driver", "main", []string{testExtensionTopic}, []string{TopicSessionInit})
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
	conn := env.connectAndJoin("gateway", "main", []string{testExtensionTopic}, []string{testExtensionTopic})

	// Should NOT receive init — expect timeout
	msg := readMsgTimeout(t, conn, 200*time.Millisecond)
	if msg != nil {
		t.Errorf("expected no message, got %s", msg.Type)
	}
}

func TestMessageRouting(t *testing.T) {
	env := newTestEnv(t)

	// Client A: sender
	connA := env.connectAndJoin("sender", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	// Client B: receiver in same session
	connB := env.connectAndJoin("receiver", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	// Client C: different session
	connC := env.connectAndJoin("other-session", "sub-1", []string{testExtensionTopic}, []string{testExtensionTopic})
	// Client D: same session, does NOT receive testExtensionTopic
	connD := env.connectAndJoin("no-msg", "main", []string{testExtensionTopic}, []string{"error"})

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
	connMain := env.connectAndJoin("main-client", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	// Client in "sub-1" session
	connSub := env.connectAndJoin("sub-client", "sub-1", []string{testExtensionTopic}, []string{testExtensionTopic})

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

	// Client that can only send testExtensionTopic
	connSender := env.connectAndJoin("limited", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	// Receiver in same session
	connRecv := env.connectAndJoin("recv", "main", []string{testExtensionTopic}, []string{TopicStreamDelta, testExtensionTopic})

	// Try to send stream.delta (not in sends list) — should be ignored
	writeJSON(t, connSender, BusMessage{Type: string(MsgEvent), Topic: TopicStreamDelta, Data: mustMarshalRaw(map[string]any{"text": "hacked"})})
	time.Sleep(50 * time.Millisecond)

	msg := readMsgTimeout(t, connRecv, 200*time.Millisecond)
	if msg != nil {
		t.Errorf("should not receive unauthorized message, got %s: %s", msg.Type, msg.Text)
	}
}

func TestReceivesGlobal(t *testing.T) {
	env := newTestEnv(t)

	// Client A: joined to "main" session, sends messages
	connA := env.connectAndJoin("sender", "main", []string{testExtensionTopic}, []string{testExtensionTopic})

	// Client B: NOT joined to any session, but has receives_global: [testExtensionTopic]
	connB, _ := env.connectOpen("global-listener", nil, nil, []string{testExtensionTopic}, nil, map[string]any{})

	// Client C: NOT joined, no receives_global — should NOT receive
	connC := env.connect("no-global", []string{}, []string{testExtensionTopic})

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
	sender := env.connectAndJoin("sender", "main", []string{testExtensionTopic}, []string{})
	receiver := env.connectAndJoin("receiver", "main", []string{}, []string{testExtensionTopic})

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

func TestExchangeReplyRequiresEligibleResponder(t *testing.T) {
	testExchangeReplyRequiresEligibleResponder(t, TopicExchangeChoose)
	testExchangeReplyRequiresEligibleResponder(t, TopicExchangeApprove)
}

func TestExchangeApproveHelperReachesTenantSessionResponder(t *testing.T) {
	hub := NewHub(nil, nil)
	hub.sessions.GetOrCreate("s1", "tenant-a")
	responder := addTenantCaptureClient(t, hub, "tenant-a", "responder", "s1", []string{TopicExchangeApprove}, nil)
	help := addTenantCaptureClient(t, hub, "tenant-a", "helper", "s1", []string{TopicExchangeApprove}, nil)
	responder.sends[TopicExchangeApprove] = true
	help.sends[TopicExchangeApprove] = true

	hub.handleExchangeRequest(help, &BusMessage{Type: string(MsgRequest), Topic: TopicExchangeApprove, ID: "approve-1", Session: "s1", TenantID: "tenant-a"})

	request := waitForMessage(t, responder.recvCh)
	if request.Type != string(MsgRequest) || request.Topic != TopicExchangeApprove || request.ID != "approve-1" {
		t.Fatalf("expected approval request to responder, got %+v", request)
	}
	hub.handleExchangeReply(responder, &BusMessage{Type: string(MsgReply), Topic: TopicExchangeApprove, ID: "approve-1", Data: mustMarshalRaw(map[string]any{"choice": "allow once", "index": 0})})
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

	alphaSender := env.connectAndJoinTenant("alpha-sender", "alpha", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	alphaReceiver := env.connectAndJoinTenant("alpha-receiver", "alpha", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	betaReceiver := env.connectAndJoinTenant("beta-receiver", "beta", "main", []string{testExtensionTopic}, []string{testExtensionTopic})

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

	writeJSON(t, alphaSender, BusMessage{Type: string(MsgEvent), Topic: testExtensionTopic, Data: mustMarshalRaw(map[string]any{"text": "hello alpha"})})
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

func testExchangeReplyRequiresEligibleResponder(t *testing.T, topic string) {
	hub := NewHub(nil, nil)
	requester := addCaptureClient(t, hub, "requester", "main", []string{topic}, []string{topic, string(MsgError)})
	responder := addCaptureClient(t, hub, "responder", "main", []string{topic}, []string{topic, string(MsgError)})
	secondResponder := addCaptureClient(t, hub, "second", "main", []string{topic}, []string{topic, string(MsgError)})
	attacker := addCaptureClient(t, hub, "attacker", "main", []string{string(MsgError)}, []string{string(MsgError)})
	requester.sends[topic] = true
	responder.sends[topic] = true
	secondResponder.sends[topic] = true
	attacker.sends[topic] = true

	hub.HandleBusMessage(requester, &BusMessage{
		Type:  string(MsgRequest),
		Topic: topic,
		ID:    "ex-1",
		Data:  mustMarshalRaw(map[string]any{"question": "Allow?", "options": []string{"yes", "no"}}),
	})

	req := waitForMessage(t, responder.recvCh)
	if req.Type != string(MsgRequest) || req.Topic != topic || req.ID != "ex-1" {
		t.Fatalf("expected exchange request for responder, got %+v", req)
	}
	secondReq := waitForMessage(t, secondResponder.recvCh)
	if secondReq.Type != string(MsgRequest) || secondReq.Topic != topic || secondReq.ID != "ex-1" {
		t.Fatalf("expected exchange request for second responder, got %+v", secondReq)
	}
	if msg := readCaptureMessageTimeout(attacker.recvCh, 100*time.Millisecond); msg != nil {
		t.Fatalf("attacker should not receive exchange request, got %+v", msg)
	}

	hub.HandleBusMessage(attacker, &BusMessage{Type: string(MsgReply), Topic: topic, ID: "ex-1", Data: mustMarshalRaw(map[string]any{"choice": "yes"})})
	_ = readCaptureMessageTimeout(attacker.recvCh, 100*time.Millisecond) // spoof rejection is allowed but not required by callers.
	if msg := readCaptureMessageTimeout(requester.recvCh, 100*time.Millisecond); msg != nil {
		t.Fatalf("requester should not receive spoofed reply, got %+v", msg)
	}

	hub.HandleBusMessage(responder, &BusMessage{Type: string(MsgReply), Topic: topic, ID: "ex-1", Data: mustMarshalRaw(map[string]any{"choice": "yes", "index": 0})})
	reply := waitForMessage(t, requester.recvCh)
	if reply.Type != string(MsgReply) || reply.Topic != topic || reply.ID != "ex-1" {
		t.Fatalf("expected exchange reply for requester, got %+v", reply)
	}
	resolved := waitForMessage(t, secondResponder.recvCh)
	if resolved.Type != string(MsgEvent) || resolved.Topic != topic || resolved.ID != "ex-1" {
		t.Fatalf("expected exchange resolved event for other responder, got %+v", resolved)
	}
	hub.HandleBusMessage(secondResponder, &BusMessage{Type: string(MsgReply), Topic: topic, ID: "ex-1", Data: mustMarshalRaw(map[string]any{"choice": "no"})})
	errorMsg := waitForMessage(t, secondResponder.recvCh)
	if errorMsg.Type != string(MsgError) || errorMsg.Text != "client not allowed to answer exchange" {
		t.Fatalf("expected stale exchange reply rejection, got %+v", errorMsg)
	}
}

func TestLateExchangeApprovalReplyAfterRequesterDisconnectIsRejected(t *testing.T) {
	hub := NewHub(nil, nil)
	approval := addCaptureClient(t, hub, "hook-approvals", "main", []string{TopicExchangeApprove, string(MsgError)}, []string{TopicExchangeApprove, string(MsgError)})
	ui := addCaptureClient(t, hub, "gateway-web", "main", []string{TopicExchangeApprove, string(MsgError)}, []string{TopicExchangeApprove, string(MsgError)})
	approval.sends[TopicExchangeApprove] = true
	ui.sends[TopicExchangeApprove] = true
	ui.meta = mustMarshalRaw(map[string]any{"tabula.client_role": "user"})

	hub.HandleBusMessage(approval, &BusMessage{
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
	hub.HandleBusMessage(ui, &BusMessage{
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

func TestPickExchangeResponderPrefersUserClient(t *testing.T) {
	hub := NewHub(nil, nil)
	requester := addCaptureClient(t, hub, "requester", "main", []string{TopicExchangeChoose}, []string{TopicExchangeChoose, string(MsgError)})
	generic := addCaptureClient(t, hub, "generic", "main", []string{TopicExchangeChoose}, nil)
	ui := addCaptureClient(t, hub, "gateway-web", "main", []string{TopicExchangeChoose}, nil)
	requester.sends[TopicExchangeChoose] = true
	generic.sends[TopicExchangeChoose] = true
	ui.sends[TopicExchangeChoose] = true
	generic.id = 1
	ui.id = 2
	ui.meta = mustMarshalRaw(map[string]any{"tabula.client_role": "user"})

	hub.HandleBusMessage(requester, &BusMessage{
		Type:  string(MsgRequest),
		Topic: TopicExchangeChoose,
		ID:    "ex-ui",
		Data:  mustMarshalRaw(map[string]any{"question": "Pick one", "options": []string{"yes", "no"}}),
	})

	req := waitForMessage(t, ui.recvCh)
	if req.ID != "ex-ui" || req.Topic != TopicExchangeChoose {
		t.Fatalf("expected user responder to receive exchange, got %+v", req)
	}
	if msg := readCaptureMessageTimeout(generic.recvCh, 100*time.Millisecond); msg != nil {
		t.Fatalf("generic responder should not receive exchange when UI responder exists, got %+v", msg)
	}
}

func TestPickExchangeResponderFansOutToAllEqualUserClients(t *testing.T) {
	hub := NewHub(nil, nil)
	requester := addCaptureClient(t, hub, "requester", "main", []string{TopicExchangeChoose}, []string{TopicExchangeChoose, string(MsgError)})
	older := addCaptureClient(t, hub, "gateway-old", "main", []string{TopicExchangeChoose}, nil)
	newer := addCaptureClient(t, hub, "gateway-new", "main", []string{TopicExchangeChoose}, nil)
	requester.sends[TopicExchangeChoose] = true
	older.sends[TopicExchangeChoose] = true
	newer.sends[TopicExchangeChoose] = true
	older.id = 10
	newer.id = 11
	older.meta = mustMarshalRaw(map[string]any{"tabula.client_role": "user"})
	newer.meta = mustMarshalRaw(map[string]any{"tabula.client_role": "user"})

	hub.HandleBusMessage(requester, &BusMessage{
		Type:  string(MsgRequest),
		Topic: TopicExchangeChoose,
		ID:    "ex-newest",
		Data:  mustMarshalRaw(map[string]any{"question": "Pick one", "options": []string{"yes", "no"}}),
	})

	req := waitForMessage(t, newer.recvCh)
	if req.ID != "ex-newest" || req.Topic != TopicExchangeChoose {
		t.Fatalf("expected newest user responder to receive exchange, got %+v", req)
	}
	olderReq := waitForMessage(t, older.recvCh)
	if olderReq.ID != "ex-newest" || olderReq.Topic != TopicExchangeChoose {
		t.Fatalf("expected older user responder to also receive exchange, got %+v", olderReq)
	}
}

func TestNotConnected(t *testing.T) {
	env := newTestEnv(t)

	// Dial raw WS, don't send connect — send message directly
	conn := env.dial()
	connRecv := env.connectAndJoin("recv", "main", []string{testExtensionTopic}, []string{testExtensionTopic})

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
	connSender := env.connect("no-join", []string{testExtensionTopic}, []string{testExtensionTopic})
	connRecv := env.connectAndJoin("recv", "main", []string{testExtensionTopic}, []string{testExtensionTopic})

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

	sender := env.connectAndJoin("sender", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	recv1 := env.connectAndJoin("recv1", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	recv2 := env.connectAndJoin("recv2", "main", []string{testExtensionTopic}, []string{testExtensionTopic})

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

	conn1 := env.connectAndJoin("will-disconnect", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	conn2 := env.connectAndJoin("stays", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	conn3 := env.connectAndJoin("sender", "main", []string{testExtensionTopic}, []string{testExtensionTopic})

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
	slowConn := env.connectAndJoin("slow", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	sender := env.connectAndJoin("sender", "main", []string{testExtensionTopic}, []string{testExtensionTopic})
	fast := env.connectAndJoin("fast", "main", []string{testExtensionTopic}, []string{testExtensionTopic})

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

func TestCriticalToolResultWaitsForSlowInternalClient(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	c := &Client{
		hub:      hub,
		name:     "slow-internal",
		recvCh:   make(chan *BusMessage, 1),
		receives: map[string]bool{TopicToolResult: true},
		done:     make(chan struct{}),
		state:    ClientJoined,
	}
	c.recvCh <- &BusMessage{Type: string(MsgEvent), Topic: TopicSessionInit}
	done := make(chan bool, 1)
	go func() {
		done <- c.queueMsg(&BusMessage{Type: string(MsgReply), Topic: TopicToolResult, ID: "tool-1", Name: "fs_read"})
	}()
	select {
	case delivered := <-done:
		t.Fatalf("critical tool result returned before queue drained: delivered=%v", delivered)
	case <-time.After(50 * time.Millisecond):
	}
	<-c.recvCh
	select {
	case delivered := <-done:
		if !delivered {
			t.Fatal("critical tool result was not delivered after queue drained")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for critical tool result delivery")
	}
	msg := <-c.recvCh
	if msg.Topic != TopicToolResult || msg.ID != "tool-1" {
		t.Fatalf("unexpected delivered message: %+v", msg)
	}
}

func TestConcurrentMessages(t *testing.T) {
	env := newTestEnv(t)

	// Create 10 clients in same session
	var clients []*websocket.Conn
	for i := 0; i < 10; i++ {
		c := env.connectAndJoin(
			fmt.Sprintf("client-%d", i), "main",
			[]string{testExtensionTopic}, []string{testExtensionTopic},
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
			[]string{testExtensionTopic}, []string{testExtensionTopic})
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

// --- Protocol v4 validation tests ---

func TestProtocolV3ClientRejected(t *testing.T) {
	env := newTestEnv(t)
	conn := env.dial()
	// Version 0 (omitted) — must be rejected; clients must declare PROTOCOL_VERSION.
	writeRawJSON(t, conn, map[string]any{
		"type": "hello",
		"data": map[string]any{
			"name":        "legacy-client",
			"send_topics": []string{testExtensionTopic},
		},
	})
	msg := readMsg(t, conn)
	if msg.Type != "error" {
		t.Fatalf("expected error for missing protocol version, got %s", msg.Type)
	}
	if !strings.Contains(msg.Text, "unknown field \"type\"") {
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
	writeRawJSON(t, conn, ClientEnvelope{
		V: 999, Kind: "command", Op: "connection.open", ID: "future-client",
		Data: mustMarshalRaw(map[string]any{"name": "future-client", "auth_token": env.Token, "meta": map[string]any{}}),
	})
	msg := readMsg(t, conn)
	if msg.Type != "error" {
		t.Fatalf("expected error for unsupported version, got %s", msg.Type)
	}
	if !strings.Contains(msg.Text, "v must be 4") {
		t.Errorf("unexpected error text: %q", msg.Text)
	}
}
