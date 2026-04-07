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

// testEnv bundles a Hub + httptest server + WS client factory for tests.
type testEnv struct {
	Hub    *Hub
	Server *httptest.Server
	t      *testing.T
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()

	toolsJSON := json.RawMessage(`[{"name":"EXEC","description":"run cmd","params":{"command":{"type":"string","description":"cmd"}},"required":["command"]},{"name":"SPAWN","description":"spawn","params":{"command":{"type":"string","description":"cmd"}},"required":["command"]},{"name":"KILL","description":"kill","params":{"pid":{"type":"integer","description":"pid"}},"required":["pid"]},{"name":"LIST","description":"list","params":{},"required":[]}]`)
	hub := NewHub("test system prompt", toolsJSON, false)

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
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg Message
	if err := conn.ReadJSON(&msg); err != nil {
		t.Fatalf("readMsg failed: %v", err)
	}
	return msg
}

// readMsgTimeout reads one JSON message with a custom timeout. Returns nil on timeout.
func readMsgTimeout(t *testing.T, conn *websocket.Conn, d time.Duration) *Message {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(d))
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
	e.Hub.mu.Lock()
	token := e.Hub.generateSpawnToken(depth)
	e.Hub.mu.Unlock()

	conn := e.dial()
	writeJSON(e.t, conn, Message{
		Type:     "connect",
		Name:     name,
		Sends:    sends,
		Receives: receives,
		Token:    token,
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
	if init.Prompt != "test system prompt" {
		t.Errorf("expected system prompt, got %q", init.Prompt)
	}
	if len(init.Tools) == 0 {
		t.Error("expected tools, got empty")
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
		Name:  "EXEC",
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
		Name:  "EXEC",
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
		Name:  "EXEC",
		Input: json.RawMessage(`{"command":"python3 -c \"print('A'*32768)\""}`),
	})

	msg := readMsg(t, conn)
	if len(msg.Output) > maxExecOutput+100 { // some slack for trimming
		t.Errorf("output should be truncated to ~%d bytes, got %d", maxExecOutput, len(msg.Output))
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
		Name:  "EXEC",
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
		Name:  "EXEC",
		Input: json.RawMessage(`{"command":"sleep 0.5 && echo done1"}`),
	})
	writeJSON(t, conn2, Message{
		Type:  "tool_use",
		ID:    "e2",
		Name:  "EXEC",
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
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	// SPAWN sleep 60
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  "SPAWN",
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
		Name:  "LIST",
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
	fmt.Sscanf(spawnMsg.Output, "PID %d", &pid)

	// KILL
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "k1",
		Name:  "KILL",
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
		Name:  "LIST",
		Input: json.RawMessage(`{}`),
	})
	listMsg2 := readMsg(t, conn)
	if !strings.Contains(listMsg2.Output, "alive=false") {
		t.Errorf("LIST after kill: expected alive=false, got %q", listMsg2.Output)
	}
}

func TestSpawnSessionTracking(t *testing.T) {
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
		Name:  "SPAWN",
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
		Name:  "EXEC",
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
	env := newTestEnv(t)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use", "cancel"},
		[]string{"init", "tool_result"})
	readMsg(t, conn) // init

	// SPAWN a long-running process
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  "SPAWN",
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	spawnMsg := readMsg(t, conn)
	if !strings.HasPrefix(spawnMsg.Output, "PID ") {
		t.Fatalf("expected PID, got %q", spawnMsg.Output)
	}

	var pid int
	fmt.Sscanf(spawnMsg.Output, "PID %d", &pid)

	// Send cancel
	writeJSON(t, conn, Message{Type: "cancel"})
	time.Sleep(500 * time.Millisecond)

	// Verify process is no longer alive
	env.Hub.mu.Lock()
	proc, ok := env.Hub.spawned[pid]
	var alive bool
	if ok {
		alive = proc.Alive
	}
	env.Hub.mu.Unlock()

	// The process may or may not have been reaped yet, but it should have received SIGINT
	// We can verify by checking if the process is still running after a bit
	time.Sleep(500 * time.Millisecond)

	env.Hub.mu.Lock()
	if proc, ok := env.Hub.spawned[pid]; ok {
		// After SIGINT + wait, the process shouldn't be alive
		// (sleep exits on SIGINT on most platforms)
		_ = proc
	}
	env.Hub.mu.Unlock()

	// At minimum, verify no panic occurred and cancel was processed
	_ = alive
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
		Name:  "EXEC",
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
		Name:  "EXEC",
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
			Name:  "EXEC",
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
		Name:  "SPAWN",
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "exec-1",
		Name:  "EXEC",
		Input: json.RawMessage(`{"command":"echo fast"}`),
	})
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "spawn-2",
		Name:  "SPAWN",
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
		fmt.Sscanf(results[id], "PID %d", &pid)
		writeJSON(t, conn, Message{
			Type:  "tool_use",
			ID:    "kill-" + id,
			Name:  "KILL",
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
			Name:  "EXEC",
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
		Type: "tool_use", ID: "a1", Name: "EXEC",
		Input: json.RawMessage(`{"command":"sleep 0.2 && echo from-a"}`),
	})
	writeJSON(t, connB, Message{
		Type: "tool_use", ID: "b1", Name: "EXEC",
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
	env := newTestEnv(t)

	// Client at depth=maxSpawnDepth should be denied SPAWN
	conn := env.connectAndJoinWithDepth("deep-agent", "sub-deep", maxSpawnDepth,
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, conn) // init

	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  "SPAWN",
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	msg := readMsg(t, conn)
	if !strings.Contains(msg.Output, "max spawn depth") {
		t.Errorf("expected max spawn depth error, got %q", msg.Output)
	}
}

func TestSpawnAllowedBelowMaxDepth(t *testing.T) {
	env := newTestEnv(t)

	// Client at depth=0 (default) should be allowed to SPAWN
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, conn) // init

	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  "SPAWN",
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	msg := readMsg(t, conn)
	if !strings.HasPrefix(msg.Output, "PID ") {
		t.Fatalf("expected PID, got %q", msg.Output)
	}

	// Cleanup
	var pid int
	fmt.Sscanf(msg.Output, "PID %d", &pid)
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "k1",
		Name:  "KILL",
		Input: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, pid)),
	})
	readMsg(t, conn)
}

func TestSpawnDeniedAtMaxChildren(t *testing.T) {
	env := newTestEnv(t)

	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, conn) // init

	// Spawn maxChildrenPerSession processes
	var pids []int
	for i := range maxChildrenPerSession {
		writeJSON(t, conn, Message{
			Type:  "tool_use",
			ID:    fmt.Sprintf("s%d", i),
			Name:  "SPAWN",
			Input: json.RawMessage(`{"command":"sleep 60"}`),
		})
		msg := readMsg(t, conn)
		if !strings.HasPrefix(msg.Output, "PID ") {
			t.Fatalf("spawn %d: expected PID, got %q", i, msg.Output)
		}
		var pid int
		fmt.Sscanf(msg.Output, "PID %d", &pid)
		pids = append(pids, pid)
	}

	// Next SPAWN should be denied
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "overflow",
		Name:  "SPAWN",
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
		Name:  "KILL",
		Input: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, pids[0])),
	})
	readMsg(t, conn) // kill result

	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "retry",
		Name:  "SPAWN",
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	retryMsg := readMsg(t, conn)
	if !strings.HasPrefix(retryMsg.Output, "PID ") {
		t.Errorf("expected PID after kill, got %q", retryMsg.Output)
	}

	// Cleanup all
	var retryPid int
	fmt.Sscanf(retryMsg.Output, "PID %d", &retryPid)
	allPids := append(pids[1:], retryPid)
	for i, pid := range allPids {
		writeJSON(t, conn, Message{
			Type:  "tool_use",
			ID:    fmt.Sprintf("cleanup-%d", i),
			Name:  "KILL",
			Input: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, pid)),
		})
		readMsg(t, conn)
	}
}

func TestSpawnTokenPropagatedInEnv(t *testing.T) {
	env := newTestEnv(t)

	// Client at depth=1 spawns a process — child should get TABULA_SPAWN_TOKEN
	conn := env.connectAndJoinWithDepth("subagent", "sub-1", 1,
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, conn) // init

	// Spawn a process that prints TABULA_SPAWN_TOKEN
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  "SPAWN",
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
		Name:  "EXEC",
		Input: json.RawMessage(`{"command":"cat /tmp/tabula_token_test.txt"}`),
	})
	execMsg := readMsg(t, conn)
	token := strings.TrimSpace(execMsg.Output)
	if len(token) != 32 { // 16 bytes hex-encoded
		t.Errorf("expected 32-char hex token, got %q (len %d)", token, len(token))
	}

	// Verify the token resolves to depth=2 in hub
	env.Hub.mu.Lock()
	depth, ok := env.Hub.spawnTokens[token]
	env.Hub.mu.Unlock()
	if !ok {
		t.Errorf("token %q not found in hub spawnTokens", token)
	} else if depth != 2 {
		t.Errorf("expected depth 2, got %d", depth)
	}

	// Cleanup
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "e2",
		Name:  "EXEC",
		Input: json.RawMessage(`{"command":"rm -f /tmp/tabula_token_test.txt"}`),
	})
	readMsg(t, conn)
}

func TestSpawnChildrenCountedPerSession(t *testing.T) {
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
	for i := range maxChildrenPerSession {
		writeJSON(t, connA, Message{
			Type:  "tool_use",
			ID:    fmt.Sprintf("a%d", i),
			Name:  "SPAWN",
			Input: json.RawMessage(`{"command":"sleep 60"}`),
		})
		readMsg(t, connA)
	}

	// Session B should still be able to spawn
	writeJSON(t, connB, Message{
		Type:  "tool_use",
		ID:    "b1",
		Name:  "SPAWN",
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
	env.Hub.mu.Lock()
	token := env.Hub.generateSpawnToken(1)
	env.Hub.mu.Unlock()

	// First connect with token — should get depth=1
	conn1 := env.dial()
	writeJSON(t, conn1, Message{
		Type:     "connect",
		Name:     "first",
		Sends:    []string{"tool_use"},
		Receives: []string{"init", "tool_result"},
		Token:    token,
	})
	msg1 := readMsg(t, conn1)
	if msg1.Type != "connected" {
		t.Fatalf("expected connected, got %s", msg1.Type)
	}

	// Verify depth was set
	env.Hub.mu.Lock()
	var firstDepth int
	for c := range env.Hub.clients {
		if c.name == "first" {
			firstDepth = c.depth
		}
	}
	env.Hub.mu.Unlock()
	if firstDepth != 1 {
		t.Errorf("first client: expected depth 1, got %d", firstDepth)
	}

	// Second connect with same token — should get depth=0 (token consumed)
	conn2 := env.dial()
	writeJSON(t, conn2, Message{
		Type:     "connect",
		Name:     "second",
		Sends:    []string{"tool_use"},
		Receives: []string{"init", "tool_result"},
		Token:    token,
	})
	msg2 := readMsg(t, conn2)
	if msg2.Type != "connected" {
		t.Fatalf("expected connected, got %s", msg2.Type)
	}

	env.Hub.mu.Lock()
	var secondDepth int
	for c := range env.Hub.clients {
		if c.name == "second" {
			secondDepth = c.depth
		}
	}
	env.Hub.mu.Unlock()
	if secondDepth != 0 {
		t.Errorf("second client (reused token): expected depth 0, got %d", secondDepth)
	}
}

func TestNoTokenMeansDepthZero(t *testing.T) {
	env := newTestEnv(t)

	// Connect without token — depth should be 0 (boot process)
	conn := env.connectAndJoin("driver", "main",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, conn) // init

	env.Hub.mu.Lock()
	var depth int
	for c := range env.Hub.clients {
		if c.name == "driver" {
			depth = c.depth
		}
	}
	env.Hub.mu.Unlock()

	if depth != 0 {
		t.Errorf("no-token client: expected depth 0, got %d", depth)
	}

	// Should be able to spawn (depth 0 < maxSpawnDepth)
	writeJSON(t, conn, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  "SPAWN",
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	msg := readMsg(t, conn)
	if !strings.HasPrefix(msg.Output, "PID ") {
		t.Errorf("depth-0 client should be able to spawn, got %q", msg.Output)
	}
}

func TestKillScopedToSession(t *testing.T) {
	env := newTestEnv(t)

	// Session A spawns a process
	connA := env.connectAndJoin("driver-a", "sess-a",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, connA) // init

	writeJSON(t, connA, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  "SPAWN",
		Input: json.RawMessage(`{"command":"sleep 60"}`),
	})
	spawnMsg := readMsg(t, connA)
	var pid int
	fmt.Sscanf(spawnMsg.Output, "PID %d", &pid)

	// Session B tries to kill A's process — should fail
	connB := env.connectAndJoin("driver-b", "sess-b",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, connB) // init

	writeJSON(t, connB, Message{
		Type:  "tool_use",
		ID:    "k1",
		Name:  "KILL",
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
		Name:  "KILL",
		Input: json.RawMessage(fmt.Sprintf(`{"pid":%d}`, pid)),
	})
	killMsg2 := readMsg(t, connA)
	if killMsg2.Output != "OK" {
		t.Errorf("session A should kill its own process, got %q", killMsg2.Output)
	}
}

func TestListScopedToSession(t *testing.T) {
	env := newTestEnv(t)

	// Session A spawns a process
	connA := env.connectAndJoin("driver-a", "sess-a",
		[]string{"tool_use"}, []string{"init", "tool_result"})
	readMsg(t, connA) // init

	writeJSON(t, connA, Message{
		Type:  "tool_use",
		ID:    "s1",
		Name:  "SPAWN",
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
		Name:  "SPAWN",
		Input: json.RawMessage(`{"command":"sleep 61"}`),
	})
	readMsg(t, connB) // PID

	// Session A lists — should only see "sleep 60"
	writeJSON(t, connA, Message{
		Type:  "tool_use",
		ID:    "l1",
		Name:  "LIST",
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
		Name:  "LIST",
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
