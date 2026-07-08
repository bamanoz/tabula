package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"
)

const turnCorrelationMetaKey = "turn_correlation_id"
const preferredRuntimeKernelMetaKey = "preferred_runtime_id"
const turnContextKernelMetaKey = "turn_context"

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
	if runtimeID := h.sessionPreferredRuntime(tenantID, session); runtimeID != "" {
		meta[preferredRuntimeKernelMetaKey] = runtimeID
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
	merged := map[string]any{}
	if existing, ok := meta["kernel"].(map[string]any); ok {
		for key, value := range existing {
			merged[key] = value
		}
	}
	for key, value := range kernel {
		merged[key] = value
	}
	meta["kernel"] = merged
	encoded, err := json.Marshal(meta)
	if err != nil {
		return raw
	}
	return encoded
}

func withKernelPreferredRuntime(raw json.RawMessage, runtimeID string) json.RawMessage {
	return withKernelMetaString(raw, preferredRuntimeKernelMetaKey, runtimeID)
}

func withKernelTurnContext(raw json.RawMessage, context string) json.RawMessage {
	return withKernelMetaString(raw, turnContextKernelMetaKey, context)
}

func withKernelMetaString(raw json.RawMessage, key, value string) json.RawMessage {
	if value == "" {
		return raw
	}
	meta := metaMap(raw)
	kernel := map[string]any{}
	if existing, ok := meta["kernel"].(map[string]any); ok {
		for existingKey, existingValue := range existing {
			kernel[existingKey] = existingValue
		}
	}
	kernel[key] = value
	meta["kernel"] = kernel
	encoded, err := json.Marshal(meta)
	if err != nil {
		return raw
	}
	return encoded
}

func metaMap(raw json.RawMessage) map[string]any {
	meta := map[string]any{}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &meta)
	}
	return meta
}

func metaString(raw json.RawMessage, key string) string {
	meta := metaMap(raw)
	value, _ := meta[key].(string)
	return value
}

func withMetaString(raw json.RawMessage, key, value string) json.RawMessage {
	if value == "" {
		return raw
	}
	meta := metaMap(raw)
	meta[key] = value
	encoded, err := json.Marshal(meta)
	if err != nil {
		return raw
	}
	return encoded
}

func kernelPreferredRuntime(raw json.RawMessage) string {
	meta := metaMap(raw)
	kernel, ok := meta["kernel"].(map[string]any)
	if !ok {
		return ""
	}
	runtimeID, _ := kernel[preferredRuntimeKernelMetaKey].(string)
	return normalizeClientRuntimeID(runtimeID)
}

func ensureTurnCorrelationMeta(raw json.RawMessage) (json.RawMessage, string) {
	if existing := metaString(raw, turnCorrelationMetaKey); existing != "" {
		return raw, existing
	}
	generated := generateTurnCorrelationID()
	return withMetaString(raw, turnCorrelationMetaKey, generated), generated
}

func generateTurnCorrelationID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return "tc-" + hex.EncodeToString(b)
}

func (c *Client) clientID() string {
	if c == nil || c.id <= 0 {
		return ""
	}
	return "c" + strconv.Itoa(c.id)
}
