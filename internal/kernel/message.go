package kernel

import "encoding/json"

// Message is the generic JSON message exchanged over WebSocket.
type Message struct {
	V        int             `json:"v,omitempty"`
	Type     string          `json:"type"`
	Name     string          `json:"name,omitempty"`
	Session  string          `json:"session,omitempty"`
	TenantID string          `json:"tenant_id,omitempty"`
	ID       string          `json:"id,omitempty"`
	Topic    string          `json:"topic,omitempty"`
	Data     json.RawMessage `json:"data,omitempty"`
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
	AuthToken      string   `json:"auth_token,omitempty"`
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
	if msg.Data != nil {
		clone.Data = append(json.RawMessage(nil), msg.Data...)
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

func mustMarshalRaw(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

func eventText(msg *Message) string {
	if msg == nil || len(msg.Data) == 0 {
		return ""
	}
	var data struct {
		Text string `json:"text"`
	}
	if json.Unmarshal(msg.Data, &data) != nil {
		return ""
	}
	return data.Text
}

func messageText(msg *Message) string {
	if msg == nil || msg.Topic != TopicMessageUser {
		return ""
	}
	return eventText(msg)
}

func setMessageText(msg *Message, text string) {
	if msg == nil {
		return
	}
	var data map[string]any
	if len(msg.Data) > 0 {
		_ = json.Unmarshal(msg.Data, &data)
	}
	if data == nil {
		data = map[string]any{}
	}
	data["text"] = text
	msg.Data = mustMarshalRaw(data)
}

func messageCapability(msg *Message) string {
	if msg == nil {
		return ""
	}
	if (MsgType(msg.Type) == MsgEvent || MsgType(msg.Type) == MsgRequest || MsgType(msg.Type) == MsgReply) && msg.Topic != "" {
		return msg.Topic
	}
	return msg.Type
}
