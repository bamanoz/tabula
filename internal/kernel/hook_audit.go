package kernel

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/bamanoz/tabula/internal/agent"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
	"github.com/bamanoz/tabula/internal/sessionrecord"
	"github.com/bamanoz/tabula/internal/tenant"
)

const hookDispatchAuditKind = "hook.dispatch.audit"

func hookAttemptCorrelation(raw []byte) (toolAttemptContext, error) {
	var body struct {
		Meta json.RawMessage `json:"meta"`
	}
	if len(raw) == 0 {
		return toolAttemptContext{}, nil
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return toolAttemptContext{}, err
	}
	if len(body.Meta) == 0 {
		return toolAttemptContext{}, nil
	}
	return toolAttemptContextFromMeta(body.Meta)
}

func hookAttemptPayload(correlation toolAttemptContext) map[string]any {
	return correlation.payload()
}

func (h *Hub) validateHookAttempt(event, tenantID, session string, correlation toolAttemptContext) error {
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
	if !ok || correlation.TurnCorrelationID != turn.ID {
		return fmt.Errorf("%w: hook turn correlation does not match durable state", agent.ErrStaleDriver)
	}
	attempt, ok := coordinatorAttempt(turn, correlation.AttemptID)
	if !ok || attempt.DriverInstanceID != correlation.DriverInstanceID || attempt.LeaseID != correlation.LeaseID || attempt.DriverGeneration != correlation.DriverGeneration {
		return fmt.Errorf("%w: hook attempt fence does not match durable state", agent.ErrStaleDriver)
	}
	if event == "before_turn" {
		if record.State.ActiveTurnID != turn.ID || turn.ActiveAttemptID != attempt.ID || turn.Status != agent.TurnPreparing || (attempt.Status != agent.AttemptAssigned && attempt.Status != agent.AttemptPrepared) || record.State.Driver.Fence != correlation.fence() {
			return fmt.Errorf("%w: before_turn attempt is not active and preparing", agent.ErrStaleDriver)
		}
		return nil
	}
	if event == "after_turn" {
		return nil
	}
	return h.validateToolAttempt(tenantID, session, correlation)
}

func (h *Hub) validateHookAttemptPayload(event string, payload json.RawMessage, tenantID, session string) error {
	correlation, err := hookAttemptCorrelation(payload)
	if err != nil {
		return fmt.Errorf("%w: invalid hook attempt metadata: %v", agent.ErrInvalidArgument, err)
	}
	return h.validateHookAttempt(event, tenantID, session, correlation)
}

func (h *Hub) recordHookDispatchAudit(a khooks.DispatchAudit) {
	if h == nil || a.Event == "" || a.Session == "" {
		return
	}
	store := h.sessionRecords
	if store == nil {
		return
	}
	tenantID := a.TenantID
	if tenantID == "" {
		tenantID = h.sessionTenantID("", a.Session)
	}
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	payload := map[string]any{
		"hook":            a.Event,
		"tenant_id":       tenantID,
		"session":         a.Session,
		"target":          a.Target,
		"hook_id":         a.HookID,
		"reply_action":    a.ReplyAction,
		"dispatch_effect": a.DispatchEffect,
		"status":          a.Status,
		"duration_ms":     a.DurationMs,
		"timeout_ms":      a.TimeoutMs,
		"input_summary":   khooks.SummarizePayload(a.Payload),
	}
	if a.Reason != "" {
		payload["reason"] = a.Reason
	}
	correlation, correlationErr := hookAttemptCorrelation(a.Payload)
	if correlationErr != nil {
		payload["attempt_correlation_valid"] = false
		payload["attempt_correlation_error"] = "invalid_metadata"
	} else {
		for key, value := range hookAttemptPayload(correlation) {
			payload[key] = value
		}
		if !correlation.empty() {
			if err := h.validateHookAttempt(a.Event, tenantID, a.Session, correlation); err != nil {
				payload["attempt_correlation_valid"] = false
				payload["attempt_correlation_error"] = "stale_or_mismatched"
			} else {
				payload["attempt_correlation_valid"] = true
			}
		}
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		h.Logger.Warn("encode hook dispatch audit", "session", a.Session, "tenant_id", tenantID, "err", err)
		return
	}
	if _, err := store.Append(context.Background(), sessionrecord.Record{
		TenantID:  tenantID,
		SessionID: a.Session,
		Kind:      hookDispatchAuditKind,
		Producer:  "kernel:hook_dispatch",
		Payload:   raw,
	}); err != nil {
		h.Logger.Warn("record hook dispatch audit", "session", a.Session, "tenant_id", tenantID, "err", err)
	}
}
