package kernel

import (
	"encoding/json"
	"strconv"
	"unicode/utf8"

	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
	"github.com/bamanoz/tabula/internal/kernel/process"
	"github.com/bamanoz/tabula/internal/tenant"
)

const afterToolCallOutputPreviewBytes = 16 << 10

func (h *Hub) targetSession(sender *Client, msg *BusMessage) string {
	if msg.Session != "" {
		return msg.Session
	}
	return sender.session
}

func (h *Hub) targetTenant(sender *Client, msg *BusMessage) string {
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
func (h *Hub) allHookSubscribers() []khooks.Subscriber {
	clients := h.clients.All()
	runtimeTargets := h.runtimeHookTargets()
	subs := make([]khooks.Subscriber, 0, len(clients)+len(runtimeTargets))
	for _, c := range clients {
		subs = append(subs, c)
	}
	for _, target := range runtimeTargets {
		subs = append(subs, newRuntimeHookSubscriber(target, h.isRuntimeTargetBusy, h.markRuntimeTargetBusy, h.tryMarkRuntimeTargetBusy, h.runtimeTargetBusyDone, h.Logger))
	}
	return subs
}

func (h *Hub) runtimeHookTargets() []runtimeHookTarget {
	if h == nil || h.runtimes == nil {
		return nil
	}
	return h.runtimes.HookTargets()
}

func (h *Hub) sessionProcesses(session string) []*process.Spawned {
	processes := make([]*process.Spawned, 0)
	h.processes.ForEach(func(_ int, proc *process.Spawned) {
		if proc == nil {
			return
		}
		if session == "" || proc.Session == session {
			processes = append(processes, proc)
		}
	})
	return processes
}

func (h *Hub) emitBeforeCompaction(tenantID, session string, sender *Client, msg *BusMessage) {
	if msg == nil || session == "" {
		return
	}
	payload := map[string]any{
		"session":             session,
		"tenant_id":           tenantID,
		"sender":              kernelClientMeta(sender),
		"message_id":          msg.ID,
		"type":                msg.Type,
		"topic":               msg.Topic,
		"name":                msg.Name,
		"text":                msg.Text,
		"meta":                metaMap(msg.Meta),
		"turn_correlation_id": metaString(msg.Meta, turnCorrelationMetaKey),
	}
	if len(msg.Data) > 0 {
		payload["data"] = msg.Data
	}
	hookPayload, _ := json.Marshal(payload)
	// This hook is synchronous so pre-compaction save hooks can finish before the
	// compaction event reaches observers. The result is intentionally ignored:
	// this is not an authorization boundary.
	h.dispatchHook("before_compaction", hookPayload, tenantID, session)
}

func (h *Hub) emitAfterToolCall(tenantID, session, toolID string, payload map[string]string) {
	payload["tenant_id"] = tenantID
	if output, ok := payload["output"]; ok {
		if preview, truncated := truncateHookOutput(output); truncated {
			payload["output"] = preview
			payload["output_truncated"] = "true"
			payload["output_bytes"] = strconv.Itoa(len(output))
		}
	}
	hookPayload, _ := json.Marshal(payload)
	h.dispatchHook("after_tool_call", hookPayload, tenantID, session)
}

func truncateHookOutput(output string) (string, bool) {
	if len(output) <= afterToolCallOutputPreviewBytes {
		return output, false
	}
	cut := afterToolCallOutputPreviewBytes
	for cut > 0 && !utf8.ValidString(output[:cut]) {
		cut--
	}
	return output[:cut], true
}

func (h *Hub) emitSessionEnd(tenantID, session string) {
	payload, _ := json.Marshal(map[string]string{"session": session, "tenant_id": tenantID})
	h.dispatchHook("session_end", payload, tenantID, session)
}
