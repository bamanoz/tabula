package kernel

import (
	"fmt"

	"github.com/bamanoz/tabula/internal/kernel/toolstate"
	"github.com/bamanoz/tabula/internal/tenant"
)

func (h *Hub) recordToolStarted(tenantID, session, toolID, toolName string) {
	if h != nil && session != "" {
		resolvedTenant := tenantID
		if resolvedTenant == "" {
			resolvedTenant = h.sessionTenantID("", session)
		}
		h.sessions.GetOrCreate(session, resolvedTenant).BeginToolCall()
	}
	h.recordToolLifecycle(tenantID, session, toolID, toolName, "started", nil)
}

func (h *Hub) recordToolSuspended(tenantID, session, toolID, toolName, exchangeID string) {
	extra := map[string]any{"exchange_id": exchangeID}
	h.recordToolLifecycle(tenantID, session, toolID, toolName, "suspended", extra)
}

func (h *Hub) recordToolTerminal(tenantID, session, toolID, toolName, status string) {
	extra := map[string]any{"status": status}
	h.recordToolLifecycle(tenantID, session, toolID, toolName, "terminal", extra)
	if h == nil || session == "" {
		return
	}
	resolvedTenant := tenantID
	if resolvedTenant == "" {
		resolvedTenant = h.sessionTenantID("", session)
	}
	for _, steer := range h.sessions.GetOrCreate(session, resolvedTenant).CompleteToolCall() {
		h.dispatchQueuedSteer(resolvedTenant, session, steer)
	}
}

func (h *Hub) recordToolLifecycle(tenantID, session, toolID, toolName, state string, extra map[string]any) {
	if h == nil || session == "" || toolID == "" || state == "" {
		return
	}
	store, ok := h.sessionStore.(toolstate.Store)
	if !ok {
		return
	}
	if tenantID == "" {
		tenantID = h.sessionTenantID("", session)
	}
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	event := toolstate.Event{
		State:    state,
		ToolID:   toolID,
		ToolName: toolName,
		RunID:    h.runID,
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
	if err := store.AppendToolLifecycle(session, tenantID, event); err != nil {
		h.Logger.Warn("record tool lifecycle failed", "session", session, "tool_call_id", toolID, "state", state, "err", err)
	}
}

func (h *Hub) reconcileInterruptedTools(tenantID, session string) {
	if h == nil || session == "" {
		return
	}
	store, ok := h.sessionStore.(toolstate.Store)
	if !ok {
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
	events, err := store.LoadToolLifecycle(session, tenantID)
	if err != nil {
		h.Logger.Warn("load tool lifecycle failed", "session", session, "tenant_id", tenantID, "err", err)
		return
	}
	states := toolstate.States(events)
	for _, state := range states {
		if state.ToolID == "" || state.Terminal || state.Suspended || state.RunID == "" || state.RunID == h.runID {
			continue
		}
		if err := store.AppendToolLifecycle(session, tenantID, toolstate.Event{
			State:         "terminal",
			ToolID:        state.ToolID,
			ToolName:      state.ToolName,
			RunID:         h.runID,
			Status:        "interrupted",
			PreviousRunID: state.RunID,
			Reason:        "kernel_restarted",
		}); err != nil {
			h.Logger.Warn("record interrupted tool lifecycle failed", "session", session, "tool_call_id", state.ToolID, "err", err)
			continue
		}
	}
}
