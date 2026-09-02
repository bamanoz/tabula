package kernel

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bamanoz/tabula/internal/kernel/toolstate"
	"github.com/bamanoz/tabula/internal/sessionrecord"
)

const toolLifecycleRecordPageSize = 256

func appendToolLifecycleRecord(store sessionrecord.Store, session, tenantID string, event toolstate.Event) error {
	if store == nil {
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
	if event.TurnID != "" {
		payload["turn_id"] = event.TurnID
	}
	if event.AttemptID != "" {
		payload["attempt_id"] = event.AttemptID
	}
	if event.DriverInstanceID != "" {
		payload["driver_instance_id"] = event.DriverInstanceID
	}
	if event.LeaseID != "" {
		payload["lease_id"] = event.LeaseID
	}
	if event.DriverGeneration != 0 {
		payload["driver_generation"] = event.DriverGeneration
	}
	if event.TurnCorrelationID != "" {
		payload["turn_correlation_id"] = event.TurnCorrelationID
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode tool lifecycle record: %w", err)
	}
	_, err = store.Append(context.Background(), sessionrecord.Record{
		TenantID:  tenantID,
		SessionID: session,
		Kind:      toolstate.Kind,
		Producer:  "kernel:tool_lifecycle",
		Payload:   raw,
	})
	return err
}

func loadToolLifecycleRecords(store sessionrecord.Store, session, tenantID string) ([]toolstate.Event, error) {
	if store == nil {
		return nil, nil
	}
	events := make([]toolstate.Event, 0)
	var afterID int64
	for {
		records, err := store.Read(context.Background(), sessionrecord.Query{
			TenantID:  tenantID,
			SessionID: session,
			Kind:      toolstate.Kind,
			AfterID:   afterID,
			Limit:     toolLifecycleRecordPageSize,
		})
		if err != nil {
			return nil, err
		}
		for _, record := range records {
			var payload struct {
				State             string `json:"state"`
				ToolCallID        string `json:"tool_call_id"`
				Tool              string `json:"tool"`
				RunID             string `json:"run_id"`
				TurnID            string `json:"turn_id"`
				AttemptID         string `json:"attempt_id"`
				DriverInstanceID  string `json:"driver_instance_id"`
				LeaseID           string `json:"lease_id"`
				DriverGeneration  uint64 `json:"driver_generation"`
				TurnCorrelationID string `json:"turn_correlation_id"`
				Status            string `json:"status"`
				ExchangeID        string `json:"exchange_id"`
				Reason            string `json:"reason"`
				PreviousRunID     string `json:"previous_run_id"`
			}
			if json.Unmarshal(record.Payload, &payload) != nil || payload.ToolCallID == "" {
				continue
			}
			events = append(events, toolstate.Event{
				State: payload.State, ToolID: payload.ToolCallID, ToolName: payload.Tool, RunID: payload.RunID,
				TurnID: payload.TurnID, AttemptID: payload.AttemptID, DriverInstanceID: payload.DriverInstanceID,
				LeaseID: payload.LeaseID, DriverGeneration: payload.DriverGeneration, TurnCorrelationID: payload.TurnCorrelationID,
				Status: payload.Status, ExchangeID: payload.ExchangeID, Reason: payload.Reason, PreviousRunID: payload.PreviousRunID,
			})
			afterID = record.ID
		}
		if len(records) < toolLifecycleRecordPageSize {
			break
		}
	}
	return events, nil
}
