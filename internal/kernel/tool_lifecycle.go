package kernel

import (
	"fmt"
	"time"

	"github.com/bamanoz/tabula/internal/tenant"
)

const toolLifecycleKind = "tool.lifecycle"

type toolLifecycleStore interface {
	AppendToolLifecycle(session, tenantID string, event toolLifecycleEvent) error
	LoadToolLifecycle(session, tenantID string) ([]toolLifecycleEvent, error)
}

type toolLifecycleEvent struct {
	State         string
	ToolID        string
	ToolName      string
	RunID         string
	Status        string
	ApprovalID    string
	Reason        string
	PreviousRunID string
}

func newKernelRunID() string {
	return fmt.Sprintf("kernel-%d", time.Now().UnixNano())
}

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

func (h *Hub) recordToolSuspended(tenantID, session, toolID, toolName, approvalID string) {
	extra := map[string]any{"approval_id": approvalID}
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
	store, ok := h.sessionStore.(toolLifecycleStore)
	if !ok {
		return
	}
	if tenantID == "" {
		tenantID = h.sessionTenantID("", session)
	}
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	event := toolLifecycleEvent{
		State:    state,
		ToolID:   toolID,
		ToolName: toolName,
		RunID:    h.runID,
	}
	for key, value := range extra {
		switch key {
		case "approval_id":
			event.ApprovalID = fmt.Sprint(value)
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
	store, ok := h.sessionStore.(toolLifecycleStore)
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
	states := toolLifecycleStates(events)
	for _, state := range states {
		if state.ToolID == "" || state.Terminal || state.Suspended || state.RunID == "" || state.RunID == h.runID {
			continue
		}
		if err := store.AppendToolLifecycle(session, tenantID, toolLifecycleEvent{
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

func toolLifecycleStates(events []toolLifecycleEvent) map[string]toolLifecycleState {
	states := map[string]toolLifecycleState{}
	for _, event := range events {
		if event.ToolID == "" {
			continue
		}
		state := states[event.ToolID]
		state.ToolID = event.ToolID
		if event.ToolName != "" {
			state.ToolName = event.ToolName
		}
		switch event.State {
		case "started":
			state.RunID = event.RunID
			state.Terminal = false
			state.Suspended = false
		case "suspended":
			state.Suspended = true
		case "terminal":
			state.Terminal = true
			state.Suspended = false
		}
		states[event.ToolID] = state
	}
	return states
}

type toolLifecycleState struct {
	ToolID    string
	ToolName  string
	RunID     string
	Terminal  bool
	Suspended bool
}
