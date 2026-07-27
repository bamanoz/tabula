package kernel

import (
	"bufio"

	"encoding/json"
	"github.com/bamanoz/tabula/internal/kernel/toolstate"
	"os"
	"path/filepath"
	"testing"
)

func TestReconcileInterruptedToolsMarksOldStartedToolTerminal(t *testing.T) {
	home := t.TempDir()
	hub := NewHub(nil, nil)
	hub.SetSessionStore(NewDiskSessionStore(home))
	hub.runID = "run-old"
	hub.recordToolStarted("tenant-a", "main", "call-1", "exec_run")
	hub.runID = "run-new"

	hub.reconcileInterruptedTools("tenant-a", "main")

	events := readToolLifecycleEvents(t, home, "main")
	if len(events) != 2 {
		t.Fatalf("expected started and terminal lifecycle events, got %d: %+v", len(events), events)
	}
	terminal := events[1]
	if terminal.Payload.State != "terminal" || terminal.Payload.Status != "interrupted" || terminal.Payload.Reason != "kernel_restarted" || terminal.Payload.PreviousRunID != "run-old" {
		t.Fatalf("expected interrupted terminal event, got %+v", terminal.Payload)
	}
}

func TestReconcileInterruptedToolsDoesNotMarkCurrentRunTool(t *testing.T) {
	home := t.TempDir()
	hub := NewHub(nil, nil)
	hub.SetSessionStore(NewDiskSessionStore(home))
	hub.runID = "run-current"
	hub.recordToolStarted("tenant-a", "main", "call-1", "exec_run")

	hub.reconcileInterruptedTools("tenant-a", "main")

	events := readToolLifecycleEvents(t, home, "main")
	if len(events) != 1 || events[0].Payload.State != "started" {
		t.Fatalf("expected only started event, got %+v", events)
	}
}

func TestReconcileInterruptedToolsDoesNotMarkSuspendedApproval(t *testing.T) {
	home := t.TempDir()
	hub := NewHub(nil, nil)
	hub.SetSessionStore(NewDiskSessionStore(home))
	hub.runID = "run-old"
	hub.recordToolStarted("tenant-a", "main", "call-1", "exec_run")
	hub.recordToolSuspended("tenant-a", "main", "call-1", "exec_run", "approve-1")
	hub.runID = "run-new"

	hub.reconcileInterruptedTools("tenant-a", "main")

	events := readToolLifecycleEvents(t, home, "main")
	if len(events) != 2 || events[1].Payload.State != "suspended" {
		t.Fatalf("expected started+suspended only, got %+v", events)
	}
}

func TestToolLifecycleUsesStoreAbstraction(t *testing.T) {
	store := &memoryToolLifecycleStore{}
	hub := NewHub(nil, nil)
	hub.SetSessionStore(store)
	hub.runID = "run-old"
	hub.recordToolStarted("tenant-a", "main", "call-1", "exec_run")
	hub.runID = "run-new"

	hub.reconcileInterruptedTools("tenant-a", "main")

	if len(store.events) != 2 {
		t.Fatalf("expected started and interrupted terminal event, got %+v", store.events)
	}
	terminal := store.events[1]
	if terminal.State != "terminal" || terminal.Status != "interrupted" || terminal.PreviousRunID != "run-old" {
		t.Fatalf("expected interrupted terminal event through store abstraction, got %+v", terminal)
	}
}

type memoryToolLifecycleStore struct {
	events []toolstate.Event
}

func (s *memoryToolLifecycleStore) Save(*Session) error { return nil }

func (s *memoryToolLifecycleStore) Load(string, string) (*sessionFile, error) { return nil, nil }

func (s *memoryToolLifecycleStore) Delete(string, string) error { return nil }

func (s *memoryToolLifecycleStore) AppendToolLifecycle(_ string, _ string, event toolstate.Event) error {
	s.events = append(s.events, event)
	return nil
}

func (s *memoryToolLifecycleStore) LoadToolLifecycle(_ string, _ string) ([]toolstate.Event, error) {
	return append([]toolstate.Event(nil), s.events...), nil
}

type testToolLifecycleEvent struct {
	Kind    string `json:"kind"`
	Payload struct {
		State         string `json:"state"`
		ToolCallID    string `json:"tool_call_id"`
		Tool          string `json:"tool"`
		RunID         string `json:"run_id"`
		Status        string `json:"status"`
		Reason        string `json:"reason"`
		PreviousRunID string `json:"previous_run_id"`
	} `json:"payload"`
}

func readToolLifecycleEvents(t *testing.T, home, session string) []testToolLifecycleEvent {
	t.Helper()
	file, err := os.Open(filepath.Join(home, "data", "sessions", session, "ledger.jsonl"))
	if err != nil {
		t.Fatalf("open ledger: %v", err)
	}
	defer file.Close()

	var events []testToolLifecycleEvent
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event testToolLifecycleEvent
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			t.Fatalf("unmarshal ledger event: %v", err)
		}
		if event.Kind == toolstate.Kind {
			events = append(events, event)
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan ledger: %v", err)
	}
	return events
}
