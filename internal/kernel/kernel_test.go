package kernel

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// --- Test helper ---

var testUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

const testShellToolName = "test_shell"

// testShellExecCommand is a test-only dynamic skill command that emulates the
// old shell_exec payload shape through SkillExec. It keeps kernel_test coverage
// focused on unified dynamic dispatch without reintroducing kernel builtins.
const testShellExecCommand = `python3 -c 'import json, subprocess, sys
try:
    data=json.load(sys.stdin)
except Exception as exc:
    sys.stderr.write(f"ERROR: invalid input: {exc}\n")
    sys.exit(1)
cmd=data.get("command")
if not isinstance(cmd, str) or not cmd:
    sys.stderr.write("ERROR: missing command\n")
    sys.exit(1)
proc=subprocess.run(cmd, shell=True, text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT)
sys.stdout.write(proc.stdout)
sys.exit(proc.returncode)'`

// testEnv bundles a Hub + httptest server + WS client factory for tests.
type testEnv struct {
	Hub    *Hub
	Server *httptest.Server
	t      *testing.T
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	toolsJSON := json.RawMessage(`[{"name":"echo_tool","description":"echo stdin","params":{"text":{"type":"string","description":"text to echo"}},"required":[]},{"name":"test_shell","description":"test-only shell-style skill","params":{"command":{"type":"string","description":"command to run"}},"required":["command"]}]`)
	skillExec := map[string]string{"echo_tool": "cat", testShellToolName: testShellExecCommand}
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
		// Give readPump goroutines time to finish Unregister after server.Close()
		time.Sleep(50 * time.Millisecond)
	})
	return &testEnv{Hub: hub, Server: server, t: t}
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

// connect dials + sends connect + reads connected response.
func (e *testEnv) connect(name string, sends, receives []string) *websocket.Conn {
	e.t.Helper()
	conn := e.dial()
	writeJSON(e.t, conn, Message{
		Type:     "connect",
		Name:     name,
		Sends:    sends,
		Receives: receives,
		Version:  ProtocolVersion,
	})
	msg := readMsg(e.t, conn)
	if msg.Type != "connected" {
		e.t.Fatalf("expected connected, got %s", msg.Type)
	}
	return conn
}

// connectAndJoin dials + connect + join.
func (e *testEnv) connectAndJoin(name, session string, sends, receives []string) *websocket.Conn {
	e.t.Helper()
	conn := e.connect(name, sends, receives)
	writeJSON(e.t, conn, Message{Type: "join", Session: session})
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

// connectWithDepth dials + sends connect with a spawn token for given depth + reads connected response.
func (e *testEnv) connectWithDepth(name string, depth int, sends, receives []string) *websocket.Conn {
	e.t.Helper()
	// Generate a spawn token in the hub (simulates kernel-issued token)
	token, err := e.Hub.generateSpawnToken(depth)
	if err != nil {
		e.t.Fatalf("generateSpawnToken: %v", err)
	}

	conn := e.dial()
	writeJSON(e.t, conn, Message{
		Type:     "connect",
		Name:     name,
		Sends:    sends,
		Receives: receives,
		Token:    token,
		Version:  ProtocolVersion,
	})
	msg := readMsg(e.t, conn)
	if msg.Type != "connected" {
		e.t.Fatalf("expected connected, got %s", msg.Type)
	}
	return conn
}

// connectAndJoinWithDepth dials + connect with depth + join.
func (e *testEnv) connectAndJoinWithDepth(name, session string, depth int, sends, receives []string) *websocket.Conn {
	e.t.Helper()
	conn := e.connectWithDepth(name, depth, sends, receives)
	writeJSON(e.t, conn, Message{Type: "join", Session: session})
	msg := readMsg(e.t, conn)
	if msg.Type != "joined" {
		e.t.Fatalf("expected joined, got %s", msg.Type)
	}
	return conn
}

// --- Tests ---

func TestConnectJoinHandshake(t *testing.T) {
	env := newTestEnv(t)

	// First client → c1
	conn1 := env.dial()
	writeJSON(t, conn1, Message{
		Type:     "connect",
		Name:     "client-a",
		Sends:    []string{"message"},
		Receives: []string{"message"},
		Version:  ProtocolVersion,
	})
	msg1 := readMsg(t, conn1)
	if msg1.Type != "connected" || msg1.ID != "c1" {
		t.Errorf("expected connected c1, got type=%s id=%s", msg1.Type, msg1.ID)
	}

	// Join
	writeJSON(t, conn1, Message{Type: "join", Session: "main"})
	join1 := readMsg(t, conn1)
	if join1.Type != "joined" || join1.Session != "main" {
		t.Errorf("expected joined main, got type=%s session=%s", join1.Type, join1.Session)
	}

	// Second client → c2
	conn2 := env.dial()
	writeJSON(t, conn2, Message{
		Type:     "connect",
		Name:     "client-b",
		Sends:    []string{"message"},
		Receives: []string{"message"},
		Version:  ProtocolVersion,
	})
	msg2 := readMsg(t, conn2)
	if msg2.ID != "c2" {
		t.Errorf("expected c2, got %s", msg2.ID)
	}

	// Third client → c3
	conn3 := env.dial()
	writeJSON(t, conn3, Message{
		Type:     "connect",
		Name:     "client-c",
		Sends:    []string{"message"},
		Receives: []string{"message"},
		Version:  ProtocolVersion,
	})
	msg3 := readMsg(t, conn3)
	if msg3.ID != "c3" {
		t.Errorf("expected c3, got %s", msg3.ID)
	}
}

func TestInitOnJoin(t *testing.T) {
	env := newTestEnv(t)

	// Client that receives init
	conn := env.connectAndJoin("driver", "main", []string{"message"}, []string{"init", "message"})
	init := readMsg(t, conn)
	if init.Type != "init" {
		t.Fatalf("expected init, got %s", init.Type)
	}
	if init.Context != "" {
		t.Errorf("expected empty init context, got %q", init.Context)
	}
	if len(init.Tools) == 0 {
		t.Error("expected tools, got empty")
	}
}

func TestRemovedKernelBuiltinsNotExposedOrExecutable(t *testing.T) {
	toolsJSON := json.RawMessage(`[{"name":"echo_tool","description":"echo stdin","params":{"text":{"type":"string","description":"text to echo"}},"required":[]}]`)
	hub := NewHub(toolsJSON, map[string]string{"echo_tool": "cat"}, 3, 5, nil)
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
	env := &testEnv{Hub: hub, Server: server, t: t}

	conn := env.connectAndJoin("driver", "main", []string{"tool_use"}, []string{"init", "tool_result"})
	init := readMsg(t, conn)
	if init.Type != "init" {
		t.Fatalf("expected init, got %s", init.Type)
	}
	for _, removed := range []KernelTool{ToolShellExec, ToolProcessSpawn, ToolProcessKill, ToolProcessList} {
		if strings.Contains(string(init.Tools), `"`+string(removed)+`"`) {
			t.Fatalf("unexpected removed kernel builtin %s leaked in init tools: %s", removed, string(init.Tools))
		}
	}

	for i, removed := range []KernelTool{ToolShellExec, ToolProcessSpawn, ToolProcessKill, ToolProcessList} {
		writeJSON(t, conn, Message{Type: "tool_use", ID: fmt.Sprintf("removed-%d", i), Name: string(removed), Input: json.RawMessage(`{}`)})
		result := readMsg(t, conn)
		if result.Type != "tool_result" {
			t.Fatalf("%s: expected tool_result, got %s", removed, result.Type)
		}
		expected := "unknown tool " + string(removed)
		if !strings.Contains(result.Output, expected) {
			t.Fatalf("%s: expected removed builtin to be rejected with %q, got %q", removed, expected, result.Output)
		}
	}
}

func TestInitNotSentWithoutReceive(t *testing.T) {
	env := newTestEnv(t)

	// Client without init in receives
	conn := env.connectAndJoin("gateway", "main", []string{"message"}, []string{"message"})

	// Should NOT receive init — expect timeout
	msg := readMsgTimeout(t, conn, 200*time.Millisecond)
	if msg != nil {
		t.Errorf("expected no message, got %s", msg.Type)
	}
}

func TestMessageRouting(t *testing.T) {
	env := newTestEnv(t)

	// Client A: sender
	connA := env.connectAndJoin("sender", "main", []string{"message"}, []string{"message"})
	// Client B: receiver in same session
	connB := env.connectAndJoin("receiver", "main", []string{"message"}, []string{"message"})
	// Client C: different session
	connC := env.connectAndJoin("other-session", "sub-1", []string{"message"}, []string{"message"})
	// Client D: same session, does NOT receive "message"
	connD := env.connectAndJoin("no-msg", "main", []string{"message"}, []string{"error"})

	// A sends message
	writeJSON(t, connA, Message{Type: "message", Text: "hello"})
	time.Sleep(50 * time.Millisecond)

	// B should receive
	msgB := readMsgTimeout(t, connB, time.Second)
	if msgB == nil || msgB.Type != "message" || msgB.Text != "hello" {
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
	connMain := env.connectAndJoin("main-client", "main", []string{"message"}, []string{"message"})
	// Client in "sub-1" session
	connSub := env.connectAndJoin("sub-client", "sub-1", []string{"message"}, []string{"message"})

	// Sub sends message with explicit session="main"
	writeJSON(t, connSub, Message{Type: "message", Session: "main", Text: "result from sub"})
	time.Sleep(50 * time.Millisecond)

	// Main should receive it
	msg := readMsgTimeout(t, connMain, time.Second)
	if msg == nil || msg.Text != "result from sub" {
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

	// Client that can only send "message"
	connSender := env.connectAndJoin("limited", "main", []string{"message"}, []string{"message"})
	// Receiver in same session
	connRecv := env.connectAndJoin("recv", "main", []string{"message"}, []string{"stream_delta", "message"})

	// Try to send stream_delta (not in sends list) — should be ignored
	writeJSON(t, connSender, Message{Type: "stream_delta", Text: "hacked"})
	time.Sleep(50 * time.Millisecond)

	msg := readMsgTimeout(t, connRecv, 200*time.Millisecond)
	if msg != nil {
		t.Errorf("should not receive unauthorized message, got %s: %s", msg.Type, msg.Text)
	}
}

func TestReceivesGlobal(t *testing.T) {
	env := newTestEnv(t)

	// Client A: joined to "main" session, sends messages
	connA := env.connectAndJoin("sender", "main", []string{"message"}, []string{"message"})

	// Client B: NOT joined to any session, but has receives_global: ["message"]
	connB := env.dial()
	writeJSON(t, connB, Message{
		Type:           "connect",
		Name:           "global-listener",
		Sends:          []string{},
		Receives:       []string{},
		ReceivesGlobal: []string{"message"},
		Version:        ProtocolVersion,
	})
	msgConnected := readMsg(t, connB)
	if msgConnected.Type != "connected" {
		t.Fatalf("expected connected, got %s", msgConnected.Type)
	}

	// Client C: NOT joined, no receives_global — should NOT receive
	connC := env.connect("no-global", []string{}, []string{"message"})

	// A sends message to its session ("main")
	writeJSON(t, connA, Message{Type: "message", Text: "hello global"})
	time.Sleep(50 * time.Millisecond)

	// B (receives_global) should receive it even though not in session
	msgB := readMsgTimeout(t, connB, time.Second)
	if msgB == nil || msgB.Type != "message" || msgB.Text != "hello global" {
		t.Errorf("B: expected message 'hello global', got %+v", msgB)
	}

	// C (no receives_global, not joined) should NOT receive
	msgC := readMsgTimeout(t, connC, 200*time.Millisecond)
	if msgC != nil {
		t.Errorf("C: should not receive without receives_global, got %+v", msgC)
	}
}

func TestNotConnected(t *testing.T) {
	env := newTestEnv(t)

	// Dial raw WS, don't send connect — send message directly
	conn := env.dial()
	connRecv := env.connectAndJoin("recv", "main", []string{"message"}, []string{"message"})

	writeJSON(t, conn, Message{Type: "message", Text: "sneaky"})
	time.Sleep(50 * time.Millisecond)

	msg := readMsgTimeout(t, connRecv, 200*time.Millisecond)
	if msg != nil {
		t.Errorf("should not receive message from unconnected client, got %+v", msg)
	}
}

func TestNotJoined(t *testing.T) {
	env := newTestEnv(t)

	// Connect but don't join
	connSender := env.connect("no-join", []string{"message"}, []string{"message"})
	connRecv := env.connectAndJoin("recv", "main", []string{"message"}, []string{"message"})

	writeJSON(t, connSender, Message{Type: "message", Text: "no session"})
	time.Sleep(50 * time.Millisecond)

	msg := readMsgTimeout(t, connRecv, 200*time.Millisecond)
	if msg != nil {
		t.Errorf("should not receive message from unjoined client, got %+v", msg)
	}
}

func TestExecBasic(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "t1",
		Name:  testShellToolName,
		Input: json.RawMessage(`{"command":"echo hello"}`),
	})

	msg := readMsg(t, conn)
	if msg.Type != "tool_result" || msg.ID != "t1" {
		t.Fatalf("expected tool_result t1, got type=%s id=%s", msg.Type, msg.ID)
	}
	if msg.Output != "hello" {
		t.Errorf("expected 'hello', got %q", msg.Output)
	}
}

func TestExecErrorExit(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "t2",
		Name:  testShellToolName,
		Input: json.RawMessage(`{"command":"exit 1"}`),
	})

	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "exit code 1") {
		t.Errorf("expected 'exit code 1' in output, got %q", msg.Output)
	}
}

func TestExecOutputTruncation(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	// Generate >16KB of output
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "t3",
		Name:  testShellToolName,
		Input: json.RawMessage(`{"command":"python3 -c \"print('A'*32768)\""}`),
	})

	msg := readMsg(t, conn)
	if len(msg.Output) > maxExecOutput+100 { // some slack for trimming
		t.Errorf("output should be truncated to ~%d bytes, got %d", maxExecOutput, len(msg.Output))
	}
	if !strings.HasSuffix(msg.Output, "[truncated]") {
		t.Errorf("truncated output should end with [truncated], got suffix %q", msg.Output[len(msg.Output)-20:])
	}
}

func TestExecStderrMerged(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	// The kernel wraps as: sh -c "COMMAND 2>&1"
	// Use a program that writes to stderr natively
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "t4",
		Name:  testShellToolName,
		Input: json.RawMessage(`{"command":"python3 -c \"import sys; sys.stderr.write('stderr_text\\n')\""}`),
	})

	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "stderr_text") {
		t.Errorf("expected stderr in output, got %q", msg.Output)
	}
}

func TestExecAsyncNonBlocking(t *testing.T) {
	env := newTestEnv(t)

	// Two clients in different sessions, each running EXEC simultaneously
	conn1 := env.connectAndJoin("driver-1", "sess-1",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn1) // init

	conn2 := env.connectAndJoin("driver-2", "sess-2",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn2) // init

	start := time.Now()

	// Fire both EXEC at the same time (sleep 0.5s each)
	writeJSON(t, conn1, Message{
		Type:  "tool_use",
		ID:    "e1",
		Name:  testShellToolName,
		Input: json.RawMessage(`{"command":"sleep 0.5 && echo done1"}`),
	})
	writeJSON(t, conn2, Message{
		Type:  "tool_use",
		ID:    "e2",
		Name:  testShellToolName,
		Input: json.RawMessage(`{"command":"sleep 0.5 && echo done2"}`),
	})

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

func TestSpawnListKill(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	// SPAWN sleep 60
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	spawnMsg := readMsg(t, conn)
	if !strings.HasPrefix(spawnMsg.Output, "PID ") {
		t.Fatalf("expected PID, got %q", spawnMsg.Output)
	}

	// LIST — should contain the PID and alive=true
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "l1",
		Name:  string(ToolProcessList),
		Input: json.RawMessage(`{}`),
	})
	listMsg := readMsg(t, conn)
	if !strings.Contains(listMsg.Output, "alive=true") {
		t.Errorf("LIST: expected alive=true, got %q", listMsg.Output)
	}
	if !strings.Contains(listMsg.Output, "sleep 60") {
		t.Errorf("LIST: expected 'sleep 60', got %q", listMsg.Output)
	}

	// Extract PID from spawn output
	var pid int
	_, _ = fmt.Sscanf(spawnMsg.Output, "PID %d", &pid)

	// KILL
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "k1",
		Name:  string(ToolProcessKill),
		Input: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, pid)),
	})
	killMsg := readMsg(t, conn)
	if killMsg.Output != "OK" {
		t.Errorf("KILL: expected OK, got %q", killMsg.Output)
	}

	// LIST again — alive=false
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "l2",
		Name:  string(ToolProcessList),
		Input: json.RawMessage(`{}`),
	})
	listMsg2 := readMsg(t, conn)
	if !strings.Contains(listMsg2.Output, "alive=false") {
		t.Errorf("LIST after kill: expected alive=false, got %q", listMsg2.Output)
	}
}

func TestSpawnSessionTracking(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)
	env.Hub.StartReaper()

	// Session A: spawns a crashing process
	connA := env.connectAndJoin("driver-a", "sess-a",
		[]string{"tool_use"},
		[]string{"init", "tool_result", "error"})
	readMsg(t, connA) // init

	// Session B: should NOT receive the error
	connB := env.connectAndJoin("driver-b", "sess-b",
		[]string{"tool_use"},
		[]string{"init", "tool_result", "error"})
	readMsg(t, connB) // init

	// Spawn a process that immediately exits with error
	writeJSON(t, connA, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sh -c 'exit 42'"}`),
	})
	spawnMsg := readMsg(t, connA) // tool_result with PID
	if !strings.HasPrefix(spawnMsg.Output, "PID ") {
		t.Fatalf("expected PID, got %q", spawnMsg.Output)
	}

	// Wait for reaper to detect the crash
	time.Sleep(1500 * time.Millisecond)

	// A should have received error
	errMsg := readMsgTimeout(t, connA, time.Second)
	if errMsg == nil || errMsg.Type != "error" {
		t.Fatalf("sess-a: expected error, got %+v", errMsg)
	}
	if !strings.Contains(errMsg.Text, "crashed") {
		t.Errorf("sess-a: expected crash message, got %q", errMsg.Text)
	}

	// B should NOT have received error
	errB := readMsgTimeout(t, connB, 500*time.Millisecond)
	if errB != nil {
		t.Errorf("sess-b: should not receive error from sess-a spawn, got %+v", errB)
	}
}

func TestUnknownTool(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "u1",
		Name:  "UNKNOWN",
		Input: json.RawMessage(`{}`),
	})
	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "unknown tool UNKNOWN") {
		t.Errorf("expected unknown tool error, got %q", msg.Output)
	}
}

func TestInvalidExecCommand(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	// Missing command field
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "x1",
		Name:  testShellToolName,
		Input: json.RawMessage(`{}`),
	})
	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "ERROR") {
		t.Errorf("expected ERROR, got %q", msg.Output)
	}
}

func TestMultipleClientsInSession(t *testing.T) {
	env := newTestEnv(t)

	sender := env.connectAndJoin("sender", "main", []string{"message"}, []string{"message"})
	recv1 := env.connectAndJoin("recv1", "main", []string{"message"}, []string{"message"})
	recv2 := env.connectAndJoin("recv2", "main", []string{"message"}, []string{"message"})

	writeJSON(t, sender, Message{Type: "message", Text: "broadcast"})
	time.Sleep(50 * time.Millisecond)

	// Both receivers get the message
	m1 := readMsgTimeout(t, recv1, time.Second)
	m2 := readMsgTimeout(t, recv2, time.Second)
	if m1 == nil || m1.Text != "broadcast" {
		t.Errorf("recv1: expected 'broadcast', got %+v", m1)
	}
	if m2 == nil || m2.Text != "broadcast" {
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

	conn1 := env.connectAndJoin("will-disconnect", "main", []string{"message"}, []string{"message"})
	conn2 := env.connectAndJoin("stays", "main", []string{"message"}, []string{"message"})
	conn3 := env.connectAndJoin("sender", "main", []string{"message"}, []string{"message"})

	// Disconnect conn1
	conn1.Close()
	time.Sleep(100 * time.Millisecond)

	// Sending message after disconnect should not panic
	writeJSON(t, conn3, Message{Type: "message", Text: "after disconnect"})
	time.Sleep(50 * time.Millisecond)

	// conn2 should still receive
	msg := readMsgTimeout(t, conn2, time.Second)
	if msg == nil || msg.Text != "after disconnect" {
		t.Errorf("conn2: expected 'after disconnect', got %+v", msg)
	}
}

func TestSlowClientDropsMessages(t *testing.T) {
	env := newTestEnv(t)

	// Create a "slow" client that never reads
	slowConn := env.connectAndJoin("slow", "main", []string{"message"}, []string{"message"})
	sender := env.connectAndJoin("sender", "main", []string{"message"}, []string{"message"})
	fast := env.connectAndJoin("fast", "main", []string{"message"}, []string{"message"})

	// Stop reading from slow client (simulate slow consumer)
	// Just flood messages — sendCh is 64 deep, so 100 should overflow
	_ = slowConn // prevent GC

	for i := 0; i < 100; i++ {
		writeJSON(t, sender, Message{Type: "message", Text: fmt.Sprintf("msg-%d", i)})
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
			[]string{"message"}, []string{"message"},
		)
		clients = append(clients, c)
	}

	// All send messages concurrently
	var wg sync.WaitGroup
	for i, c := range clients {
		wg.Add(1)
		go func(idx int, conn *websocket.Conn) {
			defer wg.Done()
			writeJSON(t, conn, Message{Type: "message", Text: fmt.Sprintf("hello from %d", idx)})
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

func TestCancel(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use", "cancel"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	// SPAWN a long-running process
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	spawnMsg := readMsg(t, conn)
	if !strings.HasPrefix(spawnMsg.Output, "PID ") {
		t.Fatalf("expected PID, got %q", spawnMsg.Output)
	}

	var pid int
	_, _ = fmt.Sscanf(spawnMsg.Output, "PID %d", &pid)

	// Send cancel
	writeJSON(t, conn, Message{Type: "cancel"})
	time.Sleep(500 * time.Millisecond)

	// Verify process is no longer alive
	proc, ok := env.Hub.processByPID(pid)
	alive := ok && proc.Alive

	// The process may or may not have been reaped yet, but it should have received SIGINT
	// We can verify by checking if the process is still running after a bit
	time.Sleep(500 * time.Millisecond)

	if proc, ok := env.Hub.processByPID(pid); ok {
		// After SIGINT + wait, the process shouldn't be alive
		// (sleep exits on SIGINT on most platforms)
		_ = proc
	}

	// At minimum, verify no panic occurred and cancel was processed
	_ = alive
}

func TestCancelScopedToSession(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)

	// Session A spawns a process
	connA := env.connectAndJoin("driverA", "sessA",
		[]string{"tool_use", "cancel"},
		[]string{"init", "tool_result"})
	readMsg(t, connA) // init

	writeJSON(t, connA, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	spawnA := readMsg(t, connA)
	var pidA int
	fmt.Sscanf(spawnA.Output, "PID %d", &pidA)

	// Session B spawns a process
	connB := env.connectAndJoin("driverB", "sessB",
		[]string{"tool_use", "cancel"},
		[]string{"init", "tool_result"})
	readMsg(t, connB) // init

	writeJSON(t, connB, Message{
		Type:  "tool_use",
		ID:    "s2",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	spawnB := readMsg(t, connB)
	var pidB int
	fmt.Sscanf(spawnB.Output, "PID %d", &pidB)

	// Session A sends cancel — should NOT affect session B's process
	writeJSON(t, connA, Message{Type: "cancel"})
	time.Sleep(500 * time.Millisecond)

	procB, ok := env.Hub.processByPID(pidB)
	bAlive := ok && procB.Alive

	if !bAlive {
		t.Error("cancel from sessA should not kill sessB's process")
	}

	// Clean up
	if p, ok := env.Hub.processByPID(pidA); ok && p.Alive {
		p.Kill()
	}
	if p, ok := env.Hub.processByPID(pidB); ok && p.Alive {
		p.Kill()
	}
}

func TestSessionBusyBlocksConcurrentRootMessagesAndResetsAfterCancel(t *testing.T) {
	env := newTestEnv(t)
	gateway := env.connectAndJoin("gateway", "main",
		[]string{"message", "cancel"},
		[]string{"error"})
	driver := env.connectAndJoinWithDepth("driver", "main", 1,
		[]string{"done"},
		[]string{"message", "cancel"})

	writeJSON(t, gateway, Message{Type: "message", Text: "first"})
	first := readMsg(t, driver)
	if first.Type != "message" || first.Text != "first" {
		t.Fatalf("expected first turn message, got %+v", first)
	}

	sess, ok := env.Hub.sessions.Get("main")
	if !ok {
		t.Fatal("session main should exist")
	}
	if !sess.IsBusy() {
		t.Fatal("session should be busy after first root message")
	}

	writeJSON(t, gateway, Message{Type: "cancel"})
	cancel := readMsg(t, driver)
	if cancel.Type != "cancel" {
		t.Fatalf("expected cancel to reach driver, got %+v", cancel)
	}
	if !sess.CancelRequested() {
		t.Fatal("session should remember cancel request while turn is inflight")
	}

	writeJSON(t, gateway, Message{Type: "message", Text: "second"})
	errMsg := readMsg(t, gateway)
	if errMsg.Type != "error" || !strings.Contains(errMsg.Text, "session busy") {
		t.Fatalf("expected busy error, got %+v", errMsg)
	}

	writeJSON(t, driver, Message{Type: "done"})
	time.Sleep(50 * time.Millisecond)
	if sess.IsBusy() {
		t.Fatal("session should stop being busy after done")
	}
	if sess.CancelRequested() {
		t.Fatal("cancel state should reset after done")
	}

	writeJSON(t, gateway, Message{Type: "message", Text: "third"})
	third := readMsg(t, driver)
	if third.Type != "message" || third.Text != "third" {
		t.Fatalf("expected turn to resume after done, got %+v", third)
	}
}

func TestChildMessagesStillRouteWhileParentTurnIsBusy(t *testing.T) {
	env := newTestEnv(t)
	gateway := env.connectAndJoin("gateway", "main",
		[]string{"message"},
		[]string{"error"})
	driver := env.connectAndJoinWithDepth("driver", "main", 1,
		[]string{"done"},
		[]string{"message"})
	child := env.connectAndJoinWithDepth("subagent", "main", 2,
		[]string{"message"},
		[]string{})

	writeJSON(t, gateway, Message{Type: "message", Text: "root turn"})
	root := readMsg(t, driver)
	if root.Type != "message" || root.Text != "root turn" {
		t.Fatalf("expected root turn message, got %+v", root)
	}

	writeJSON(t, child, Message{Type: "message", ID: "agent-1", Text: "child result"})
	childMsg := readMsg(t, driver)
	if childMsg.Type != "message" || childMsg.ID != "agent-1" || childMsg.Text != "child result" {
		t.Fatalf("expected child message to route during busy turn, got %+v", childMsg)
	}

	writeJSON(t, driver, Message{Type: "done"})
}

func TestExecWithSpecialCharacters(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "t5",
		Name:  testShellToolName,
		Input: json.RawMessage(`{"command":"echo 'hello world'"}`),
	})

	msg := readMsg(t, conn)
	if msg.Output != "hello world" {
		t.Errorf("expected 'hello world', got %q", msg.Output)
	}
}

func TestToolResultRoutedToCorrectSession(t *testing.T) {
	env := newTestEnv(t)

	// Session A
	connA := env.connectAndJoin("driver-a", "sess-a",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, connA) // init

	// Session B
	connB := env.connectAndJoin("driver-b", "sess-b",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, connB) // init

	// EXEC from session A
	writeJSON(t, connA, Message{
		Type:  "tool_use",
		ID:    "ta1",
		Name:  testShellToolName,
		Input: json.RawMessage(`{"command":"echo from_a"}`),
	})

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
			[]string{"message"}, []string{"message"})
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

// TestParallelToolUseSameSession simulates the LLM sending multiple tool_use
// requests from a single session in quick succession (batch tool calls).
// All results must come back with correct IDs and none should be lost.
func TestParallelToolUseSameSession(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	n := 5
	// Fire N EXEC tool_use requests as fast as possible (simulates batch from LLM)
	for i := range n {
		writeJSON(t, conn, Message{
			Type:  "tool_use",
			ID:    fmt.Sprintf("batch-%d", i),
			Name:  testShellToolName,
			Input: json.RawMessage(fmt.Sprintf(`{"command":"sleep 0.3 && echo result-%d"}`, i)),
		})
	}

	// Collect all N results
	results := make(map[string]string) // id → output
	for range n {
		msg := readMsg(t, conn)
		if msg.Type != "tool_result" {
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

// TestParallelSpawnAndExec simulates the LLM issuing SPAWN (subagents) and EXEC
// tools simultaneously from the same session — verifying they don't block each other.
func TestParallelSpawnAndExec(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	start := time.Now()

	// Send SPAWN (long-running) and EXEC (quick) at the same time
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "spawn-1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "exec-1",
		Name:  string(ToolShellExec),
		Input: json.RawMessage(`{"command":"echo fast"}`),
	})
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "spawn-2",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})

	// Collect all 3 results
	results := make(map[string]string)
	for range 3 {
		msg := readMsg(t, conn)
		if msg.Type != "tool_result" {
			t.Fatalf("expected tool_result, got %s", msg.Type)
		}
		results[msg.ID] = msg.Output
	}
	elapsed := time.Since(start)

	// SPAWN returns immediately with PID
	if !strings.HasPrefix(results["spawn-1"], "PID ") {
		t.Errorf("spawn-1: expected PID, got %q", results["spawn-1"])
	}
	if !strings.HasPrefix(results["spawn-2"], "PID ") {
		t.Errorf("spawn-2: expected PID, got %q", results["spawn-2"])
	}
	// EXEC should return quickly
	if results["exec-1"] != "fast" {
		t.Errorf("exec-1: expected 'fast', got %q", results["exec-1"])
	}
	// Total time should be small — nothing blocked
	if elapsed > 3*time.Second {
		t.Errorf("tools should not block each other: took %v", elapsed)
	}

	// Cleanup spawned processes
	for _, id := range []string{"spawn-1", "spawn-2"} {
		var pid int
		_, _ = fmt.Sscanf(results[id], "PID %d", &pid)
		writeJSON(t, conn, Message{
			Type:  "tool_use",
			ID:    "kill-" + id,
			Name:  string(ToolProcessKill),
			Input: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, pid)),
		})
		readMsg(t, conn) // kill result
	}
}

// TestLongRunningExecBatch simulates multiple slow EXEC calls (like subagent work)
// running in parallel from the same session. Verifies all complete concurrently.
func TestLongRunningExecBatch(t *testing.T) {
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	n := 4
	start := time.Now()

	// Fire N slow EXEC commands (each sleeps 0.5s)
	for i := range n {
		writeJSON(t, conn, Message{
			Type:  "tool_use",
			ID:    fmt.Sprintf("slow-%d", i),
			Name:  testShellToolName,
			Input: json.RawMessage(fmt.Sprintf(`{"command":"sleep 0.5 && echo done-%d"}`, i)),
		})
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
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, connA) // init

	connB := env.connectAndJoin("driver-b", "sess-b",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, connB) // init

	// Both sessions fire EXEC at the same time
	writeJSON(t, connA, Message{
		Type: "tool_use", ID: "a1", Name: testShellToolName,
		Input: json.RawMessage(`{"command":"sleep 0.2 && echo from-a"}`),
	})
	writeJSON(t, connB, Message{
		Type: "tool_use", ID: "b1", Name: testShellToolName,
		Input: json.RawMessage(`{"command":"sleep 0.2 && echo from-b"}`),
	})

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

func TestSpawnDeniedAtMaxDepth(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)

	// Client at depth=maxSpawnDepth should be denied SPAWN
	conn := env.connectAndJoinWithDepth("deep-agent", "sub-deep", env.Hub.MaxSpawnDepth,
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, conn) // init

	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "max spawn depth") {
		t.Errorf("expected max spawn depth error, got %q", msg.Output)
	}
}

func TestSpawnAllowedBelowMaxDepth(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)

	// Client at depth=0 (default) should be allowed to SPAWN
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, conn) // init

	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	msg := readMsg(t, conn)
	if !strings.HasPrefix(msg.Output, "PID ") {
		t.Fatalf("expected PID, got %q", msg.Output)
	}

	// Cleanup
	var pid int
	_, _ = fmt.Sscanf(msg.Output, "PID %d", &pid)
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "k1",
		Name:  string(ToolProcessKill),
		Input: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, pid)),
	})
	readMsg(t, conn)
}

func TestSpawnDeniedAtMaxChildren(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)

	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, conn) // init

	// Spawn MaxChildren processes
	var pids []int
	for i := range env.Hub.MaxChildren {
		writeJSON(t, conn, Message{
			Type:  "tool_use",
			ID:    fmt.Sprintf("s%d", i),
			Name:  string(ToolProcessSpawn),
			Input: json.RawMessage(`{"command":"sleep 60"}`),
		})
		msg := readMsg(t, conn)
		if !strings.HasPrefix(msg.Output, "PID ") {
			t.Fatalf("spawn %d: expected PID, got %q", i, msg.Output)
		}
		var pid int
		_, _ = fmt.Sscanf(msg.Output, "PID %d", &pid)
		pids = append(pids, pid)
	}

	// Next SPAWN should be denied
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "overflow",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "too many active subagents") {
		t.Errorf("expected max children error, got %q", msg.Output)
	}

	// Kill one, then SPAWN should succeed again
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "k1",
		Name:  string(ToolProcessKill),
		Input: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, pids[0])),
	})
	readMsg(t, conn) // kill result

	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "retry",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	retryMsg := readMsg(t, conn)
	if !strings.HasPrefix(retryMsg.Output, "PID ") {
		t.Errorf("expected PID after kill, got %q", retryMsg.Output)
	}

	// Cleanup all
	var retryPid int
	_, _ = fmt.Sscanf(retryMsg.Output, "PID %d", &retryPid)
	allPids := append(pids[1:], retryPid)
	for i, pid := range allPids {
		writeJSON(t, conn, Message{
			Type:  "tool_use",
			ID:    fmt.Sprintf("cleanup-%d", i),
			Name:  string(ToolProcessKill),
			Input: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, pid)),
		})
		readMsg(t, conn)
	}
}

func TestSpawnTokenPropagatedInEnv(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)

	// Client at depth=1 spawns a process — child should get TABULA_SPAWN_TOKEN
	conn := env.connectAndJoinWithDepth("subagent", "sub-1", 1,
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, conn) // init

	// Spawn a process that prints TABULA_SPAWN_TOKEN
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"echo $TABULA_SPAWN_TOKEN > /tmp/tabula_token_test.txt"}`),
	})
	spawnMsg := readMsg(t, conn)
	if !strings.HasPrefix(spawnMsg.Output, "PID ") {
		t.Fatalf("expected PID, got %q", spawnMsg.Output)
	}

	// Wait for process to write
	time.Sleep(500 * time.Millisecond)

	// Read the file via EXEC
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "e1",
		Name:  string(ToolShellExec),
		Input: json.RawMessage(`{"command":"cat /tmp/tabula_token_test.txt"}`),
	})
	execMsg := readMsg(t, conn)
	token := strings.TrimSpace(execMsg.Output)
	if len(token) != 32 { // 16 bytes hex-encoded
		t.Errorf("expected 32-char hex token, got %q (len %d)", token, len(token))
	}

	// Verify the token resolves to depth=2 in hub
	entry, ok := env.Hub.spawnTokenEntry(token)
	if !ok {
		t.Errorf("token %q not found in hub spawnTokens", token)
	} else if entry.depth != 2 {
		t.Errorf("expected depth 2, got %d", entry.depth)
	}

	// Cleanup
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "e2",
		Name:  string(ToolShellExec),
		Input: json.RawMessage(`{"command":"rm -f /tmp/tabula_token_test.txt"}`),
	})
	readMsg(t, conn)
}

func TestSpawnChildrenCountedPerSession(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)

	// Session A can spawn up to max
	connA := env.connectAndJoin("driver-a", "sess-a",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, connA) // init

	// Session B is independent
	connB := env.connectAndJoin("driver-b", "sess-b",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, connB) // init

	// Fill session A to max
	for i := range env.Hub.MaxChildren {
		writeJSON(t, connA, Message{
			Type:  "tool_use",
			ID:    fmt.Sprintf("a%d", i),
			Name:  string(ToolProcessSpawn),
			Input: json.RawMessage(`{"command":"sleep 60"}`),
		})
		readMsg(t, connA)
	}

	// Session B should still be able to spawn
	writeJSON(t, connB, Message{
		Type:  "tool_use",
		ID:    "b1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	msg := readMsg(t, connB)
	if !strings.HasPrefix(msg.Output, "PID ") {
		t.Errorf("session B should be able to spawn, got %q", msg.Output)
	}
}

func TestSpawnTokenOneTimeUse(t *testing.T) {
	env := newTestEnv(t)

	// Create a token for depth=1
	token, err := env.Hub.generateSpawnToken(1)
	if err != nil {
		t.Fatalf("generateSpawnToken: %v", err)
	}

	// First connect with token — should get depth=1
	conn1 := env.dial()
	writeJSON(t, conn1, Message{
		Type:     "connect",
		Name:     "first",
		Sends:    []string{"tool_use"},
		Receives: []string{"init", "tool_result"},
		Token:    token,
		Version:  ProtocolVersion,
	})
	msg1 := readMsg(t, conn1)
	if msg1.Type != "connected" {
		t.Fatalf("expected connected, got %s", msg1.Type)
	}

	// Verify depth was set
	firstDepth, _ := env.Hub.clientDepthByName("first")
	if firstDepth != 1 {
		t.Errorf("first client: expected depth 1, got %d", firstDepth)
	}

	// Second connect with same token should be rejected.
	conn2 := env.dial()
	writeJSON(t, conn2, Message{
		Type:     "connect",
		Name:     "second",
		Sends:    []string{"tool_use"},
		Receives: []string{"init", "tool_result"},
		Token:    token,
		Version:  ProtocolVersion,
	})
	msg2 := readMsg(t, conn2)
	if msg2.Type != "error" {
		t.Fatalf("expected error, got %s", msg2.Type)
	}
	if !strings.Contains(msg2.Text, "invalid or expired spawn token") {
		t.Fatalf("unexpected error text: %q", msg2.Text)
	}
}

func TestNoTokenMeansDepthZero(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)

	// Connect without token — depth should be 0 (boot process)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, conn) // init

	depth, _ := env.Hub.clientDepthByName("driver")

	if depth != 0 {
		t.Errorf("no-token client: expected depth 0, got %d", depth)
	}

	// Should be able to spawn (depth 0 < maxSpawnDepth)
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	msg := readMsg(t, conn)
	if !strings.HasPrefix(msg.Output, "PID ") {
		t.Errorf("depth-0 client should be able to spawn, got %q", msg.Output)
	}
}

func TestKillScopedToSession(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)

	// Session A spawns a process
	connA := env.connectAndJoin("driver-a", "sess-a",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, connA) // init

	writeJSON(t, connA, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	spawnMsg := readMsg(t, connA)
	var pid int
	_, _ = fmt.Sscanf(spawnMsg.Output, "PID %d", &pid)

	// Session B tries to kill A's process — should fail
	connB := env.connectAndJoin("driver-b", "sess-b",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, connB) // init

	writeJSON(t, connB, Message{
		Type:  "tool_use",
		ID:    "k1",
		Name:  string(ToolProcessKill),
		Input: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, pid)),
	})
	killMsg := readMsg(t, connB)
	if !strings.Contains(killMsg.Output, "not your process") {
		t.Errorf("session B should not kill A's process, got %q", killMsg.Output)
	}

	// Session A can kill its own process
	writeJSON(t, connA, Message{
		Type:  "tool_use",
		ID:    "k2",
		Name:  string(ToolProcessKill),
		Input: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, pid)),
	})
	killMsg2 := readMsg(t, connA)
	if killMsg2.Output != "OK" {
		t.Errorf("session A should kill its own process, got %q", killMsg2.Output)
	}
}

func TestListScopedToSession(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)

	// Session A spawns a process
	connA := env.connectAndJoin("driver-a", "sess-a",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, connA) // init

	writeJSON(t, connA, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	readMsg(t, connA) // PID

	// Session B spawns a different process
	connB := env.connectAndJoin("driver-b", "sess-b",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, connB) // init

	writeJSON(t, connB, Message{
		Type:  "tool_use",
		ID:    "s2",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 61"}`),
	})
	readMsg(t, connB) // PID

	// Session A lists — should only see "sleep 60"
	writeJSON(t, connA, Message{
		Type:  "tool_use",
		ID:    "l1",
		Name:  string(ToolProcessList),
		Input: json.RawMessage(`{}`),
	})
	listA := readMsg(t, connA)
	if !strings.Contains(listA.Output, "sleep 60") {
		t.Errorf("A should see its own process, got %q", listA.Output)
	}
	if strings.Contains(listA.Output, "sleep 61") {
		t.Errorf("A should NOT see B's process, got %q", listA.Output)
	}

	// Session B lists — should only see "sleep 61"
	writeJSON(t, connB, Message{
		Type:  "tool_use",
		ID:    "l2",
		Name:  string(ToolProcessList),
		Input: json.RawMessage(`{}`),
	})
	listB := readMsg(t, connB)
	if !strings.Contains(listB.Output, "sleep 61") {
		t.Errorf("B should see its own process, got %q", listB.Output)
	}
	if strings.Contains(listB.Output, "sleep 60") {
		t.Errorf("B should NOT see A's process, got %q", listB.Output)
	}
}

func TestShutdownGraceful(t *testing.T) {
	skipKernelBuiltinRemoved(t)
	env := newTestEnv(t)
	env.Hub.ShutdownTimeout = 5 * time.Second

	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, conn) // init

	// Spawn a process that exits quickly on SIGINT
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  string(ToolProcessSpawn),
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	msg := readMsg(t, conn)
	if !strings.HasPrefix(msg.Output, "PID ") {
		t.Fatalf("expected PID, got %q", msg.Output)
	}

	// Shutdown should complete (process gets killed after grace period or immediately)
	done := make(chan struct{})
	go func() {
		env.Hub.Shutdown()
		close(done)
	}()

	select {
	case <-done:
		// ok
	case <-time.After(10 * time.Second):
		t.Fatal("Shutdown did not complete in time")
	}

	// Verify process is no longer alive
	env.Hub.forEachProcess(func(_ int, proc *SpawnedProcess) {
		if proc.Alive {
			t.Error("process still alive after shutdown")
		}
	})
}

func TestSpawnTokenExpiry(t *testing.T) {
	env := newTestEnv(t)

	env.Hub.seedSpawnToken("expired-token", spawnTokenEntry{
		depth:     1,
		createdAt: time.Now().Add(-2 * spawnTokenTTL),
	})

	token, err := env.Hub.generateSpawnToken(3)
	if err != nil {
		t.Fatalf("generateSpawnToken: %v", err)
	}
	_, expiredExists := env.Hub.spawnTokenEntry("expired-token")
	_, freshExists := env.Hub.spawnTokenEntry(token)

	if expiredExists {
		t.Error("expired token should have been pruned")
	}
	if !freshExists {
		t.Error("fresh token should exist")
	}
}

func TestExpiredSpawnTokenRejectedOnConnect(t *testing.T) {
	env := newTestEnv(t)

	env.Hub.seedSpawnToken("expired-token", spawnTokenEntry{
		depth:     1,
		createdAt: time.Now().Add(-2 * spawnTokenTTL),
	})

	conn := env.dial()
	writeJSON(t, conn, Message{
		Type:     "connect",
		Name:     "expired",
		Sends:    []string{"tool_use"},
		Receives: []string{"init", "tool_result"},
		Token:    "expired-token",
		Version:  ProtocolVersion,
	})

	msg := readMsg(t, conn)
	if msg.Type != "error" {
		t.Fatalf("expected error, got %s", msg.Type)
	}
	if !strings.Contains(msg.Text, "invalid or expired spawn token") {
		t.Fatalf("unexpected error text: %q", msg.Text)
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
	conn := env.dial()
	writeJSON(t, conn, Message{
		Type:     "connect",
		Name:     "versioned-client",
		Sends:    []string{"message"},
		Receives: []string{"init"},
		Version:  ProtocolVersion,
	})
	msg := readMsg(t, conn)
	if msg.Type != "connected" {
		t.Fatalf("expected connected, got %s", msg.Type)
	}
	if msg.Version != ProtocolVersion {
		t.Errorf("expected version %d, got %d", ProtocolVersion, msg.Version)
	}
}

func TestLegacyClientRejected(t *testing.T) {
	env := newTestEnv(t)
	conn := env.dial()
	// Version 0 (omitted) — must be rejected; clients must declare PROTOCOL_VERSION.
	writeJSON(t, conn, Message{
		Type:  "connect",
		Name:  "legacy-client",
		Sends: []string{"message"},
	})
	msg := readMsg(t, conn)
	if msg.Type != "error" {
		t.Fatalf("expected error for missing protocol version, got %s", msg.Type)
	}
	if !strings.Contains(msg.Text, "unsupported protocol version") {
		t.Errorf("unexpected error text: %q", msg.Text)
	}
}

func TestUnsupportedProtocolVersionRejected(t *testing.T) {
	env := newTestEnv(t)
	conn := env.dial()
	writeJSON(t, conn, Message{
		Type:    "connect",
		Name:    "future-client",
		Sends:   []string{"message"},
		Version: 999, // future incompatible version
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

func TestValidateConnectMissingName(t *testing.T) {
	err := validateMessage(&Message{Type: "connect"})
	if err == nil {
		t.Fatal("expected error for connect without name")
	}
	if !strings.Contains(err.Error(), "missing name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateJoinMissingSession(t *testing.T) {
	err := validateMessage(&Message{Type: "join"})
	if err == nil {
		t.Fatal("expected error for join without session")
	}
	if !strings.Contains(err.Error(), "missing session") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateToolUseMissingFields(t *testing.T) {
	err := validateMessage(&Message{Type: "tool_use"})
	if err == nil {
		t.Fatal("expected error for tool_use without id/name")
	}
	if !strings.Contains(err.Error(), "missing id") {
		t.Errorf("unexpected error: %v", err)
	}

	err = validateMessage(&Message{Type: "tool_use", ID: "t1"})
	if err == nil {
		t.Fatal("expected error for tool_use without name")
	}
	if !strings.Contains(err.Error(), "missing name") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateHookResultMissingFields(t *testing.T) {
	err := validateMessage(&Message{Type: "hook_result"})
	if err == nil {
		t.Fatal("expected error for hook_result without id/action")
	}
	if !strings.Contains(err.Error(), "missing id") {
		t.Errorf("unexpected error: %v", err)
	}

	err = validateMessage(&Message{Type: "hook_result", ID: "h1"})
	if err == nil {
		t.Fatal("expected error for hook_result without action")
	}
	if !strings.Contains(err.Error(), "missing action") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateValidMessages(t *testing.T) {
	valid := []*Message{
		{Type: "connect", Name: "test", Sends: []string{"message"}},
		{Type: "join", Session: "main"},
		{Type: "message", Text: "hello"},
		{Type: "tool_use", ID: "t1", Name: string(ToolShellExec)},
		{Type: "hook_result", ID: "h1", Action: "pass"},
		{Type: "done"},
		{Type: "cancel"},
	}
	for _, msg := range valid {
		if err := validateMessage(msg); err != nil {
			t.Errorf("expected valid message %+v, got error: %v", msg, err)
		}
	}
}
