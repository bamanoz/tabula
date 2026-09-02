package kernel

import (
	"fmt"

	"github.com/bamanoz/tabula/internal/kernel/toolstate"
	"github.com/bamanoz/tabula/internal/tenant"
)

type toolLifecycleKey struct {
	tenantID    string
	session     string
	toolID      string
	correlation toolAttemptContext
}

func (h *Hub) recordToolStarted(tenantID, session, toolID, toolName string, correlation ...toolAttemptContext) bool {
	attempt := toolCorrelation(correlation)
	resolvedTenant := h.resolvedToolTenant(tenantID, session)
	if h == nil || h.tools == nil || !h.tools.beginToolLifecycle(toolLifecycleKey{tenantID: resolvedTenant, session: session, toolID: toolID, correlation: attempt}) {
		return false
	}
	if session != "" {
		h.sessions.GetOrCreate(session, resolvedTenant).BeginToolCall()
	}
	h.recordToolLifecycle(tenantID, session, toolID, toolName, "started", attempt, nil)
	return true
}

func (h *Hub) recordToolSuspended(tenantID, session, toolID, toolName, exchangeID string, correlation ...toolAttemptContext) {
	extra := map[string]any{"exchange_id": exchangeID}
	h.recordToolLifecycle(tenantID, session, toolID, toolName, "suspended", toolCorrelation(correlation), extra)
}

func (h *Hub) recordToolTerminal(tenantID, session, toolID, toolName, status string, correlation ...toolAttemptContext) bool {
	attempt := toolCorrelation(correlation)
	if !h.claimToolTerminal(tenantID, session, toolID, attempt) {
		return false
	}
	h.tools.removeV4ToolCall(v4ToolCallKey{tenantID: tenantID, session: session, callID: toolID}, nil)
	h.recordClaimedToolTerminal(tenantID, session, toolID, toolName, status, attempt)
	return true
}

func (h *Hub) claimToolTerminal(tenantID, session, toolID string, correlation toolAttemptContext) bool {
	resolvedTenant := h.resolvedToolTenant(tenantID, session)
	return h != nil && h.tools != nil && h.tools.finishToolLifecycle(toolLifecycleKey{tenantID: resolvedTenant, session: session, toolID: toolID, correlation: correlation})
}

func (h *Hub) recordClaimedToolTerminal(tenantID, session, toolID, toolName, status string, correlation toolAttemptContext) {
	extra := map[string]any{"status": status}
	h.recordToolLifecycle(tenantID, session, toolID, toolName, "terminal", correlation, extra)
	if session != "" {
		h.sessions.GetOrCreate(session, h.resolvedToolTenant(tenantID, session)).CompleteToolCall()
	}
}

func (h *Hub) resolvedToolTenant(tenantID, session string) string {
	if tenantID != "" || h == nil {
		return tenantID
	}
	return h.sessionTenantID("", session)
}

func (s *ToolService) beginToolLifecycle(key toolLifecycleKey) bool {
	if s == nil || key.session == "" || key.toolID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeToolLifecycles == nil {
		s.activeToolLifecycles = make(map[toolLifecycleKey]struct{})
	}
	if _, exists := s.activeToolLifecycles[key]; exists {
		return false
	}
	s.activeToolLifecycles[key] = struct{}{}
	return true
}

func (s *ToolService) finishToolLifecycle(key toolLifecycleKey) bool {
	if s == nil || key.session == "" || key.toolID == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.activeToolLifecycles[key]; !exists {
		return false
	}
	delete(s.activeToolLifecycles, key)
	return true
}

func toolCorrelation(correlation []toolAttemptContext) toolAttemptContext {
	if len(correlation) == 0 {
		return toolAttemptContext{}
	}
	return correlation[0]
}

func (h *Hub) recordToolLifecycle(tenantID, session, toolID, toolName, state string, correlation toolAttemptContext, extra map[string]any) {
	if h == nil || session == "" || toolID == "" || state == "" {
		return
	}
	store := h.sessionRecords
	if store == nil {
		return
	}
	if tenantID == "" {
		tenantID = h.sessionTenantID("", session)
	}
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	event := toolstate.Event{
		State:             state,
		ToolID:            toolID,
		ToolName:          toolName,
		RunID:             h.runID,
		TurnID:            correlation.TurnID,
		AttemptID:         correlation.AttemptID,
		DriverInstanceID:  correlation.DriverInstanceID,
		LeaseID:           correlation.LeaseID,
		DriverGeneration:  correlation.DriverGeneration,
		TurnCorrelationID: correlation.TurnCorrelationID,
	}
	for key, value := range extra {
		switch key {
		case "exchange_id":
			event.ExchangeID = fmt.Sprint(value)
		case "status":
			event.Status = fmt.Sprint(value)
		case "reason":
			event.Reason = fmt.Sprint(value)
		case "previous_run_id":
			event.PreviousRunID = fmt.Sprint(value)
		}
	}
	h.toolLifecycleMu.Lock()
	defer h.toolLifecycleMu.Unlock()
	if err := appendToolLifecycleRecord(store, session, tenantID, event); err != nil {
		h.Logger.Warn("record tool lifecycle failed", "session", session, "tool_call_id", toolID, "state", state, "err", err)
	}
}

func (h *Hub) reconcileInterruptedTools(tenantID, session string) {
	if h == nil || session == "" {
		return
	}
	store := h.sessionRecords
	if store == nil {
		return
	}
	if tenantID == "" {
		tenantID = h.sessionTenantID("", session)
	}
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}

	h.toolLifecycleMu.Lock()
	defer h.toolLifecycleMu.Unlock()
	events, err := loadToolLifecycleRecords(store, session, tenantID)
	if err != nil {
		h.Logger.Warn("load tool lifecycle failed", "session", session, "tenant_id", tenantID, "err", err)
		return
	}
	states := toolstate.States(events)
	for _, state := range states {
		if state.ToolID == "" || state.Terminal || state.Suspended || state.RunID == "" || state.RunID == h.runID {
			continue
		}
		if err := appendToolLifecycleRecord(store, session, tenantID, toolstate.Event{
			State:             "terminal",
			ToolID:            state.ToolID,
			ToolName:          state.ToolName,
			RunID:             h.runID,
			Status:            "interrupted",
			PreviousRunID:     state.RunID,
			Reason:            "kernel_restarted",
			TurnID:            state.TurnID,
			AttemptID:         state.AttemptID,
			DriverInstanceID:  state.DriverInstanceID,
			LeaseID:           state.LeaseID,
			DriverGeneration:  state.DriverGeneration,
			TurnCorrelationID: state.TurnCorrelationID,
		}); err != nil {
			h.Logger.Warn("record interrupted tool lifecycle failed", "session", session, "tool_call_id", state.ToolID, "err", err)
			continue
		}
	}
}
