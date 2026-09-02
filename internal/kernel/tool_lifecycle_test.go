package kernel

import (
	"encoding/json"
	"testing"

	"github.com/bamanoz/tabula/internal/kernel/toolstate"
	"github.com/bamanoz/tabula/internal/sessionrecord"
)

func TestReconcileInterruptedToolsMarksOldStartedToolTerminal(t *testing.T) {
	store := &memorySessionRecordStore{}
	hub := NewHub(nil, nil)
	hub.SetSessionRecordStore(store)
	hub.runID = "run-old"
	hub.recordToolStarted("tenant-a", "main", "call-1", "exec_run")
	hub.runID = "run-new"

	hub.reconcileInterruptedTools("tenant-a", "main")

	events := readToolLifecycleEvents(t, store)
	if len(events) != 2 {
		t.Fatalf("expected started and terminal lifecycle events, got %d: %+v", len(events), events)
	}
	terminal := events[1]
	if terminal.State != "terminal" || terminal.Status != "interrupted" || terminal.Reason != "kernel_restarted" || terminal.PreviousRunID != "run-old" {
		t.Fatalf("expected interrupted terminal event, got %+v", terminal)
	}
}

func TestReconcileInterruptedToolsDoesNotMarkCurrentRunTool(t *testing.T) {
	store := &memorySessionRecordStore{}
	hub := NewHub(nil, nil)
	hub.SetSessionRecordStore(store)
	hub.runID = "run-current"
	hub.recordToolStarted("tenant-a", "main", "call-1", "exec_run")

	hub.reconcileInterruptedTools("tenant-a", "main")

	events := readToolLifecycleEvents(t, store)
	if len(events) != 1 || events[0].State != "started" {
		t.Fatalf("expected only started event, got %+v", events)
	}
}

func TestReconcileInterruptedToolsDoesNotMarkSuspendedApproval(t *testing.T) {
	store := &memorySessionRecordStore{}
	hub := NewHub(nil, nil)
	hub.SetSessionRecordStore(store)
	hub.runID = "run-old"
	hub.recordToolStarted("tenant-a", "main", "call-1", "exec_run")
	hub.recordToolSuspended("tenant-a", "main", "call-1", "exec_run", "approve-1")
	hub.runID = "run-new"

	hub.reconcileInterruptedTools("tenant-a", "main")

	events := readToolLifecycleEvents(t, store)
	if len(events) != 2 || events[1].State != "suspended" {
		t.Fatalf("expected started+suspended only, got %+v", events)
	}
}

func TestToolLifecycleUsesStoreAbstraction(t *testing.T) {
	store := &memorySessionRecordStore{}
	hub := NewHub(nil, nil)
	hub.SetSessionRecordStore(store)
	hub.runID = "run-old"
	hub.recordToolStarted("tenant-a", "main", "call-1", "exec_run")
	hub.runID = "run-new"

	hub.reconcileInterruptedTools("tenant-a", "main")

	events := readToolLifecycleEvents(t, store)
	if len(events) != 2 {
		t.Fatalf("expected started and interrupted terminal event, got %+v", events)
	}
	terminal := events[1]
	if terminal.State != "terminal" || terminal.Status != "interrupted" || terminal.PreviousRunID != "run-old" {
		t.Fatalf("expected interrupted terminal event through store abstraction, got %+v", terminal)
	}
}

func readToolLifecycleEvents(t *testing.T, store *memorySessionRecordStore) []toolstate.Event {
	t.Helper()
	return decodeToolLifecycleRecords(t, store.snapshot(toolstate.Kind))
}

func decodeToolLifecycleRecords(t *testing.T, records []sessionrecord.StoredRecord) []toolstate.Event {
	t.Helper()
	events := make([]toolstate.Event, 0, len(records))
	for _, record := range records {
		var payload struct {
			State             string `json:"state"`
			ToolCallID        string `json:"tool_call_id"`
			Tool              string `json:"tool"`
			RunID             string `json:"run_id"`
			Status            string `json:"status"`
			Reason            string `json:"reason"`
			PreviousRunID     string `json:"previous_run_id"`
			TurnID            string `json:"turn_id"`
			AttemptID         string `json:"attempt_id"`
			DriverInstanceID  string `json:"driver_instance_id"`
			LeaseID           string `json:"lease_id"`
			DriverGeneration  uint64 `json:"driver_generation"`
			TurnCorrelationID string `json:"turn_correlation_id"`
		}
		if err := json.Unmarshal(record.Payload, &payload); err != nil {
			t.Fatalf("decode tool lifecycle record: %v", err)
		}
		events = append(events, toolstate.Event{
			State: payload.State, ToolID: payload.ToolCallID, ToolName: payload.Tool, RunID: payload.RunID,
			Status: payload.Status, Reason: payload.Reason, PreviousRunID: payload.PreviousRunID,
			TurnID: payload.TurnID, AttemptID: payload.AttemptID, DriverInstanceID: payload.DriverInstanceID,
			LeaseID: payload.LeaseID, DriverGeneration: payload.DriverGeneration, TurnCorrelationID: payload.TurnCorrelationID,
		})
	}
	return events
}
