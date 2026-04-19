package kernel

import (
	"encoding/json"
	"os/exec"
	"testing"
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
