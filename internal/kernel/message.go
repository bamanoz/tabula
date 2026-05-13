package kernel

import "encoding/json"

// Message is the generic JSON message exchanged over WebSocket.
type Message struct {
	Version  int             `json:"version,omitempty"`
	Type     string          `json:"type"`
	Name     string          `json:"name,omitempty"`
	Session  string          `json:"session,omitempty"`
	TenantID string          `json:"tenant_id,omitempty"`
	ID       string          `json:"id,omitempty"`
	Text     string          `json:"text,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Output   string          `json:"output,omitempty"`
	Context  string          `json:"context,omitempty"`
	Tools    json.RawMessage `json:"tools,omitempty"`
	Sends    []string        `json:"sends,omitempty"`
	Receives []string        `json:"receives,omitempty"`
	// ReceivesGlobal lists message types this client wants to receive from ALL
	// sessions, regardless of whether the client has joined a session. This is
	// useful for global observers (e.g., hook-approvals) that need to receive
	// certain messages (like rule_add commands) without joining every session.
	ReceivesGlobal []string `json:"receives_global,omitempty"`
	Token          string   `json:"token,omitempty"`
	// Hook fields
	Hooks   []HookSubscription `json:"hooks,omitempty"`
	Payload json.RawMessage    `json:"payload,omitempty"`
	Action  string             `json:"action,omitempty"`
	Reason  string             `json:"reason,omitempty"`
	// Meta is an optional opaque JSON object carried alongside the message.
	// The kernel does not interpret its contents; it is forwarded as-is.
	// Conventional uses:
	//   - on `init`:        boot metadata such as `{"default_agent": "build"}`
	//   - on `tool_result`: `{"diff": "...", "files": ["a", "b"], "summary": "..."}`
	Meta json.RawMessage `json:"meta,omitempty"`
}

func cloneMessage(msg *Message) *Message {
	if msg == nil {
		return nil
	}
	clone := *msg
	if msg.Input != nil {
		clone.Input = append(json.RawMessage(nil), msg.Input...)
	}
	if msg.Tools != nil {
		clone.Tools = append(json.RawMessage(nil), msg.Tools...)
	}
	if msg.Payload != nil {
		clone.Payload = append(json.RawMessage(nil), msg.Payload...)
	}
	if msg.Meta != nil {
		clone.Meta = append(json.RawMessage(nil), msg.Meta...)
	}
	if msg.Sends != nil {
		clone.Sends = append([]string(nil), msg.Sends...)
	}
	if msg.Receives != nil {
		clone.Receives = append([]string(nil), msg.Receives...)
	}
	if msg.ReceivesGlobal != nil {
		clone.ReceivesGlobal = append([]string(nil), msg.ReceivesGlobal...)
	}
	if msg.Hooks != nil {
		clone.Hooks = append([]HookSubscription(nil), msg.Hooks...)
	}
	return &clone
}
