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
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)

	sess := hub.sessions.GetOrCreate("s1")
	sess.AddClient("driver")

	cmd := exec.Command("sleep", "60")
	hub.processes.RegisterWithPID(4242, cmd, "sleep 60", "s1")

	raw := hub.SnapshotSessions()

	var snapshot map[string]struct {
		Clients   []string `json:"clients"`
		Processes []struct {
			PID     int    `json:"pid"`
			Command string `json:"command"`
			Alive   bool   `json:"alive"`
		} `json:"processes"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("SnapshotSessions returned invalid JSON: %v", err)
	}

	session, ok := snapshot["s1"]
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

func TestSnapshotPluginsReportsRunningPluginDiagnostics(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	handle := registeredTestPlugin(t, "hello", "hello_ping")
	handle.SetPID(42424)
	hub.registerPluginHandle(handle)
	hub.recordPluginStart(handle)

	raw := hub.SnapshotPlugins()

	var snapshot struct {
		Plugins []struct {
			ID              string   `json:"id"`
			Status          string   `json:"status"`
			PID             int      `json:"pid"`
			RestartCount    int      `json:"restart_count"`
			LastError       *string  `json:"last_error"`
			RegisteredTools []string `json:"registered_tools"`
			Subscriptions   []string `json:"subscriptions"`
			RegisteredAt    string   `json:"registered_at"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("SnapshotPlugins returned invalid JSON: %v", err)
	}
	if len(snapshot.Plugins) != 1 {
		t.Fatalf("expected one plugin snapshot, got %+v", snapshot.Plugins)
	}
	got := snapshot.Plugins[0]
	if got.ID != "hello" || got.Status != "running" || got.PID != 42424 {
		t.Fatalf("unexpected plugin snapshot: %+v", got)
	}
	if got.RestartCount != 0 || got.LastError != nil {
		t.Fatalf("unexpected restart/error diagnostics: %+v", got)
	}
	if len(got.RegisteredTools) != 1 || got.RegisteredTools[0] != "hello_ping" {
		t.Fatalf("registered tools missing: %+v", got.RegisteredTools)
	}
	if len(got.Subscriptions) != 1 || got.Subscriptions[0] != "before_tool_call" {
		t.Fatalf("subscriptions missing: %+v", got.Subscriptions)
	}
	if got.RegisteredAt == "" {
		t.Fatal("registered_at should be populated for a running handle")
	}
}

func TestSnapshotPluginsRetainsFailedLifecycleState(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
	handle := registeredTestPlugin(t, "flaky", "flaky_ping")
	hub.registerPluginHandle(handle)
	hub.recordPluginStart(handle)
	hub.recordPluginRestart("flaky", handle, errors.New("boom"), 2)
	hub.handlePluginFinalExit(handle, errors.New("restart budget exhausted"), nil)

	raw := hub.SnapshotPlugins()

	var snapshot struct {
		Plugins []struct {
			ID           string   `json:"id"`
			Status       string   `json:"status"`
			RestartCount int      `json:"restart_count"`
			LastError    *string  `json:"last_error"`
			Tools        []string `json:"registered_tools"`
		} `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		t.Fatalf("SnapshotPlugins returned invalid JSON: %v", err)
	}
	if len(snapshot.Plugins) != 1 {
		t.Fatalf("expected retained failed plugin, got %+v", snapshot.Plugins)
	}
	got := snapshot.Plugins[0]
	if got.ID != "flaky" || got.Status != "failed" || got.RestartCount != 2 {
		t.Fatalf("unexpected failed plugin snapshot: %+v", got)
	}
	if got.LastError == nil || *got.LastError != "restart budget exhausted" {
		t.Fatalf("last_error not retained: %+v", got.LastError)
	}
	if len(got.Tools) != 0 {
		t.Fatalf("failed plugin should not expose live tools: %+v", got.Tools)
	}
}

func TestSnapshotRuntimesSanitizesDetachedLastError(t *testing.T) {
	hub := NewHub(json.RawMessage(`[]`), nil, 3, 5, nil)
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
