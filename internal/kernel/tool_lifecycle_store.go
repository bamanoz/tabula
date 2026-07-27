package kernel

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/bamanoz/tabula/internal/kernel/toolstate"
)

func (s *DiskSessionStore) AppendToolLifecycle(session, tenantID string, event toolstate.Event) error {
	if s == nil || s.home == "" {
		return nil
	}
	payload := map[string]any{
		"state":        event.State,
		"tool_call_id": event.ToolID,
		"tool":         event.ToolName,
		"run_id":       event.RunID,
	}
	if event.Status != "" {
		payload["status"] = event.Status
	}
	if event.ExchangeID != "" {
		payload["exchange_id"] = event.ExchangeID
	}
	if event.Reason != "" {
		payload["reason"] = event.Reason
	}
	if event.PreviousRunID != "" {
		payload["previous_run_id"] = event.PreviousRunID
	}
	return appendSessionLedgerEvent(s.home, session, tenantID, toolstate.Kind, "kernel:tool_lifecycle", payload)
}

func (s *DiskSessionStore) LoadToolLifecycle(session, tenantID string) ([]toolstate.Event, error) {
	if s == nil || s.home == "" {
		return nil, nil
	}
	return loadToolLifecycleEvents(filepath.Join(s.home, "data", "sessions", session, "ledger.jsonl"))
}

func loadToolLifecycleEvents(path string) ([]toolstate.Event, error) {
	file, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer file.Close()

	var events []toolstate.Event
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		var event struct {
			Kind    string `json:"kind"`
			Payload struct {
				State      string `json:"state"`
				ToolCallID string `json:"tool_call_id"`
				Tool       string `json:"tool"`
				RunID      string `json:"run_id"`
			} `json:"payload"`
		}
		if json.Unmarshal(scanner.Bytes(), &event) != nil || event.Kind != toolstate.Kind || event.Payload.ToolCallID == "" {
			continue
		}
		events = append(events, toolstate.Event{
			State:    event.Payload.State,
			ToolID:   event.Payload.ToolCallID,
			ToolName: event.Payload.Tool,
			RunID:    event.Payload.RunID,
		})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return events, nil
}
