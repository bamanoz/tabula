package kernel

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"testing"

	runtimeauth "github.com/bamanoz/tabula/internal/runtime/auth"
	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
)

func TestSnapshotSessionsUsesRecordedPID(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)

	sess := hub.sessions.GetOrCreate("s1", "default")
	sess.AddClient("driver")

	cmd := exec.Command("sleep", "60")
	hub.processes.RegisterWithPID(4242, cmd, "sleep 60", "s1")

	raw := hub.SnapshotSessions()

	var snapshot map[string]struct {
		Clients []struct {
			ID   string          `json:"id"`
			Name string          `json:"name"`
			Meta json.RawMessage `json:"meta"`
		} `json:"apps"`
		Processes []struct {
			PID     int    `json:"pid"`
			Command string `json:"command"`
			Alive   bool   `json:"alive"`
		} `json:"processes"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("SnapshotSessions returned invalid JSON: %v", err)
	}

	session, ok := snapshot["default/s1"]
	if !ok {
		t.Fatal("expected session s1 in snapshot")
	}
	if len(session.Processes) != 1 {
		t.Fatalf("expected 1 process in snapshot, got %d", len(session.Processes))
	}
	if session.Processes[0].PID != 4242 {
		t.Fatalf("expected snapshot PID 4242, got %d", session.Processes[0].PID)
	}
	if session.Processes[0].Command != "sleep 60" {
		t.Fatalf("expected command sleep 60, got %q", session.Processes[0].Command)
	}
}

func TestSnapshotSessionsIncludesClientMeta(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	client := &Client{name: "driver", id: 7, tenantID: "tenant", session: "s1", meta: json.RawMessage(`{"tabula.role":"driver"}`), state: ClientJoined}
	if !hub.addClient(client) {
		t.Fatal("add client")
	}
	hub.sessions.GetOrCreate("s1", "tenant").AddClient("driver")

	var snapshot map[string]struct {
		Clients []struct {
			ID   string          `json:"id"`
			Name string          `json:"name"`
			Meta json.RawMessage `json:"meta"`
		} `json:"apps"`
	}
	if err := json.Unmarshal(hub.SnapshotSessions(), &snapshot); err != nil {
		t.Fatalf("SnapshotSessions returned invalid JSON: %v", err)
	}
	clients := snapshot["tenant/s1"].Clients
	if len(clients) != 1 || clients[0].ID != "c7" || clients[0].Name != "driver" || string(clients[0].Meta) != `{"tabula.role":"driver"}` {
		t.Fatalf("unexpected clients: %+v", clients)
	}
}

func TestKernelSessionsSnapshotRequest(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	client := &Client{name: "requester", id: 1, sends: map[string]bool{string(MsgRequest): true}, receives: map[string]bool{string(MsgReply): true}, state: ClientJoined, recvCh: make(chan *BusMessage, 1), done: make(chan struct{})}
	hub.sessions.GetOrCreate("s1", "tenant")

	hub.handleSessionMessage(client, &BusMessage{Type: string(MsgRequest), Topic: TopicKernelSessions, ID: "sessions-1"})

	select {
	case msg := <-client.recvCh:
		if msg.Type != string(MsgReply) || msg.Topic != TopicKernelSessions || msg.ID != "sessions-1" {
			t.Fatalf("unexpected reply: %+v", msg)
		}
		var payload map[string]any
		if err := json.Unmarshal(msg.Data, &payload); err != nil || payload["tenant/s1"] == nil {
			t.Fatalf("unexpected snapshot payload: %s err=%v", string(msg.Data), err)
		}
	default:
		t.Fatal("expected snapshot reply")
	}
}

func TestSnapshotRuntimesSanitizesDetachedLastError(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil)
	hub.runtimes = NewRuntimeRegistry()
	conn := runtimemock.New()
	if err := hub.runtimes.RegisterHello(runtimeauth.LocalRuntimeID, conn, nil, 0); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}

	hub.runtimes.MarkDetached(runtimeauth.LocalRuntimeID, errors.New("token=super-secret password=hunter2"))

	var snapshot struct {
		Runtimes []struct {
			Attached  bool    `json:"attached"`
			LastError *string `json:"last_error"`
		} `json:"runtimes"`
	}
	raw := hub.SnapshotRuntimes()
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("SnapshotRuntimes returned invalid JSON: %v", err)
	}
	if len(snapshot.Runtimes) != 1 {
		t.Fatalf("expected one runtime snapshot, got %s", string(raw))
	}
	got := snapshot.Runtimes[0]
	if got.Attached {
		t.Fatalf("expected runtime to be detached, got %+v", got)
	}
	if got.LastError == nil || *got.LastError != "runtime connection failed" {
		t.Fatalf("expected sanitized runtime last_error, got %+v (snapshot=%s)", got.LastError, string(raw))
	}
	for _, forbidden := range []string{"super-secret", "hunter2", "token=", "password="} {
		if bytes.Contains(raw, []byte(forbidden)) {
			t.Fatalf("runtime snapshot leaked %q: %s", forbidden, string(raw))
		}
	}
}
