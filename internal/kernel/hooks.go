package kernel

import (
	"encoding/json"

	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
)

func (h *Hub) rebuildHookIndex() {
	h.hooks.RebuildIndex(h.allHookSubscribers())
}

// dispatchHook is the single entry point for all hook dispatch.
func (h *Hub) dispatchHook(event string, payload json.RawMessage, tenantID, session string) (json.RawMessage, bool) {
	return h.dispatchHookExcept(event, payload, tenantID, session, nil)
}

func (h *Hub) dispatchHookExcept(event string, payload json.RawMessage, tenantID, session string, exclude khooks.Subscriber) (json.RawMessage, bool) {
	def, known := khooks.Events[event]
	if !known {
		h.Logger.Warn("unknown hook event", "event", event)
		return payload, true
	}
	if err := h.validateHookAttemptPayload(event, payload, tenantID, session); err != nil {
		h.Logger.Warn("hook dispatch rejected by attempt fence", "event", event, "tenant_id", tenantID, "session", session, "err", err)
		return nil, false
	}

	h.Logger.Debug("dispatching hook", "event", event, "type", def.Type, "session", session)
	result, ok := h.hooks.DispatchExcept(event, payload, tenantID, session, exclude)

	if !ok {
		h.Logger.Info("hook blocked event", "event", event, "type", def.Type, "tenant_id", tenantID, "session", session, "payload_bytes", len(payload))
	}
	return result, ok
}

func (h *Hub) handleHookResult(sender *Client, msg *BusMessage) {
	h.hooks.HandleResult(sender, hookMessageFromKernel(msg))
}

func hookMessageFromKernel(msg *BusMessage) *khooks.Message {
	if msg == nil {
		return nil
	}
	return &khooks.Message{
		Type:     msg.Type,
		ID:       msg.ID,
		Name:     msg.Name,
		Session:  msg.Session,
		TenantID: msg.TenantID,
		Payload:  msg.Payload,
		Data:     msg.Data,
		Action:   msg.Action,
		Reason:   msg.Reason,
		Release:  msg.release,
	}
}
