package kernel

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bamanoz/tabula/internal/agent"
)

const (
	turnIDMetaKey           = "turn_id"
	attemptIDMetaKey        = "attempt_id"
	driverInstanceMetaKey   = "driver_instance_id"
	leaseIDMetaKey          = "lease_id"
	driverGenerationMetaKey = "driver_generation"
)

type toolAttemptContext struct {
	TurnID            string
	AttemptID         string
	DriverInstanceID  string
	LeaseID           string
	DriverGeneration  uint64
	TurnCorrelationID string
}

func toolAttemptContextFromMeta(raw json.RawMessage) (toolAttemptContext, error) {
	meta := metaMap(raw)
	correlation := toolAttemptContext{
		TurnID:            stringMetaValue(meta, turnIDMetaKey),
		AttemptID:         stringMetaValue(meta, attemptIDMetaKey),
		DriverInstanceID:  stringMetaValue(meta, driverInstanceMetaKey),
		LeaseID:           stringMetaValue(meta, leaseIDMetaKey),
		DriverGeneration:  uint64MetaValue(meta, driverGenerationMetaKey),
		TurnCorrelationID: stringMetaValue(meta, turnCorrelationMetaKey),
	}
	if correlation.empty() {
		return correlation, nil
	}
	if correlation.TurnID == "" || correlation.AttemptID == "" || correlation.DriverInstanceID == "" || correlation.LeaseID == "" || correlation.DriverGeneration == 0 || correlation.TurnCorrelationID == "" {
		return toolAttemptContext{}, fmt.Errorf("%w: complete turn, attempt, fence, and turn correlation metadata are required", agent.ErrInvalidArgument)
	}
	return correlation, nil
}

func (c toolAttemptContext) empty() bool {
	return c.TurnID == "" && c.AttemptID == "" && c.DriverInstanceID == "" && c.LeaseID == "" && c.DriverGeneration == 0
}

func (c toolAttemptContext) fence() agent.Fence {
	return agent.Fence{DriverInstanceID: c.DriverInstanceID, LeaseID: c.LeaseID, Generation: c.DriverGeneration}
}

func (c toolAttemptContext) payload() map[string]any {
	if c.empty() {
		if c.TurnCorrelationID == "" {
			return nil
		}
		return map[string]any{turnCorrelationMetaKey: c.TurnCorrelationID}
	}
	return map[string]any{
		turnIDMetaKey:           c.TurnID,
		attemptIDMetaKey:        c.AttemptID,
		driverInstanceMetaKey:   c.DriverInstanceID,
		leaseIDMetaKey:          c.LeaseID,
		driverGenerationMetaKey: c.DriverGeneration,
		turnCorrelationMetaKey:  c.TurnCorrelationID,
	}
}

func (c toolAttemptContext) meta() json.RawMessage {
	if payload := c.payload(); payload != nil {
		return mustMarshalRaw(payload)
	}
	return nil
}

func (h *Hub) validateToolAttempt(tenantID, session string, correlation toolAttemptContext) error {
	if correlation.empty() {
		return nil
	}
	if h == nil || h.agentSessions == nil {
		return fmt.Errorf("%w: durable session repository is unavailable", agent.ErrPermissionDenied)
	}
	record, err := h.agentSessions.Load(context.Background(), agent.SessionKey{TenantID: tenantID, SessionID: session})
	if err != nil {
		return err
	}
	turn, ok := record.State.Turns[correlation.TurnID]
	if !ok || correlation.TurnCorrelationID != turn.ID || record.State.ActiveTurnID != correlation.TurnID || turn.ActiveAttemptID != correlation.AttemptID || turn.Status != agent.TurnExecuting {
		return fmt.Errorf("%w: tool attempt is not active and executing", agent.ErrStaleDriver)
	}
	for _, attempt := range turn.Attempts {
		if attempt.ID != correlation.AttemptID {
			continue
		}
		if attempt.Status != agent.AttemptPermitted || attempt.DriverInstanceID != correlation.DriverInstanceID || attempt.LeaseID != correlation.LeaseID || attempt.DriverGeneration != correlation.DriverGeneration || record.State.Driver.Fence != correlation.fence() {
			return fmt.Errorf("%w: tool attempt fence does not match", agent.ErrStaleDriver)
		}
		return nil
	}
	return fmt.Errorf("%w: tool attempt does not exist", agent.ErrStaleDriver)
}

func stringMetaValue(meta map[string]any, key string) string {
	value, _ := meta[key].(string)
	return value
}

func uint64MetaValue(meta map[string]any, key string) uint64 {
	value, ok := meta[key].(float64)
	if !ok || value <= 0 || value != float64(uint64(value)) {
		return 0
	}
	return uint64(value)
}
