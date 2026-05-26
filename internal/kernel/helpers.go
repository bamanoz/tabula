package kernel

import (
	"encoding/json"

	"github.com/bamanoz/tabula/internal/tenant"
)

func (h *Hub) targetSession(sender *Client, msg *Message) string {
	if msg.Session != "" {
		return msg.Session
	}
	return sender.session
}

func (h *Hub) targetTenant(sender *Client, msg *Message) string {
	if msg.TenantID != "" {
		return msg.TenantID
	}
	if sender.tenantID != "" {
		return sender.tenantID
	}
	return tenant.DefaultID
}

func (h *Hub) sessionClients(tenantID, session string) []*Client {
	return h.clients.InSession(tenantID, session)
}

func (h *Hub) allClients() []*Client {
	return h.clients.All()
}

// allHookSubscribers returns the union of all WebSocket clients and any
// runtime-owned hook targets. Used by the hook engine to rebuild its dispatch
// index whenever the subscriber set changes.
func (h *Hub) allHookSubscribers() []HookSubscriber {
	clients := h.clients.All()
	runtimeTargets := h.runtimeHookTargets()
	subs := make([]HookSubscriber, 0, len(clients)+len(runtimeTargets))
	for _, c := range clients {
		subs = append(subs, c)
	}
	for _, target := range runtimeTargets {
		subs = append(subs, newRuntimeHookSubscriber(target, h.isRuntimeTargetBusy))
	}
	return subs
}

func (h *Hub) runtimeHookTargets() []runtimeHookTarget {
	if h == nil || h.runtimes == nil {
		return nil
	}
	return h.runtimes.HookTargets()
}

func (h *Hub) sessionProcesses(session string) []*SpawnedProcess {
	processes := make([]*SpawnedProcess, 0)
	h.processes.ForEach(func(_ int, proc *SpawnedProcess) {
		if proc == nil {
			return
		}
		if session == "" || proc.Session == session {
			processes = append(processes, proc)
		}
	})
	return processes
}

func (h *Hub) emitAfterMessage(tenantID, session string, sender *Client) {
	payload, _ := json.Marshal(map[string]string{
		"session":   session,
		"tenant_id": tenantID,
		"sender":    sender.name,
		"type":      "done",
	})
	h.dispatchHook("after_message", payload, tenantID, session)
}

func (h *Hub) emitAfterToolCall(tenantID, session, toolID string, payload map[string]string) {
	payload["tenant_id"] = tenantID
	hookPayload, _ := json.Marshal(payload)
	h.dispatchHook("after_tool_call", hookPayload, tenantID, session)
}

func (h *Hub) emitSessionEnd(tenantID, session string) {
	payload, _ := json.Marshal(map[string]string{"session": session, "tenant_id": tenantID})
	h.dispatchHook("session_end", payload, tenantID, session)
}
