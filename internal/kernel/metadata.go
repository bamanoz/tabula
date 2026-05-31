package kernel

import (
	"encoding/json"
	"strconv"
	"time"
)

func (h *Hub) prepareRoutedMessage(sender *Client, session string, scope string, msg *Message) *Message {
	routed := cloneMessage(msg)
	if routed == nil {
		return nil
	}
	if routed.V == 0 {
		routed.V = ProtocolVersion
	}
	tenantID := routed.TenantID
	if tenantID == "" && sender != nil {
		tenantID = sender.tenantID
	}
	if routed.Session == "" && session != "" {
		routed.Session = session
	}
	if routed.TenantID == "" {
		if resolved := h.sessionTenantID(tenantID, session); resolved != "" {
			routed.TenantID = resolved
		} else if tenantID != "" {
			routed.TenantID = tenantID
		}
	}
	routed.Meta = withKernelMeta(routed.Meta, h.kernelMeta(sender, tenantID, session, scope, nil))
	return routed
}

func (h *Hub) kernelMeta(sender *Client, tenantID, session string, scope string, recipient *Client) map[string]any {
	meta := map[string]any{
		"sender":      kernelClientMeta(sender),
		"route":       kernelRouteMeta(scope, session, h.sessionTenantID(tenantID, session)),
		"received_at": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if recipient != nil {
		meta["recipient"] = kernelClientMeta(recipient)
	}
	return meta
}

func kernelClientMeta(c *Client) map[string]any {
	if c == nil {
		return map[string]any{"id": "kernel"}
	}
	return map[string]any{
		"id":   c.clientID(),
		"name": c.name,
	}
}

func kernelRouteMeta(scope string, session string, tenantID string) map[string]any {
	route := map[string]any{"scope": scope}
	if session != "" {
		route["session"] = session
	}
	if tenantID != "" {
		route["tenant_id"] = tenantID
	}
	return route
}

func withKernelMeta(raw json.RawMessage, kernel map[string]any) json.RawMessage {
	meta := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	meta["kernel"] = kernel
	encoded, err := json.Marshal(meta)
	if err != nil {
		return raw
	}
	return encoded
}

func (c *Client) clientID() string {
	if c == nil || c.id <= 0 {
		return ""
	}
	return "c" + strconv.Itoa(c.id)
}
