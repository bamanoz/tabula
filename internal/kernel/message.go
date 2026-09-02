package kernel

import "encoding/json"

// BusMessage is an internal adapter for topic-based extension routing. It is
// encoded only inside protocol v4 extension.send/extension.event data.
type BusMessage struct {
	Type      string          `json:"type"`
	Name      string          `json:"name,omitempty"`
	Session   string          `json:"session,omitempty"`
	TenantID  string          `json:"tenant_id,omitempty"`
	ID        string          `json:"id,omitempty"`
	Topic     string          `json:"topic,omitempty"`
	Data      json.RawMessage `json:"data,omitempty"`
	Text      string          `json:"text,omitempty"`
	State     string          `json:"state,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	Output    string          `json:"output,omitempty"`
	Artifact  json.RawMessage `json:"artifact,omitempty"`
	Truncated bool            `json:"truncated,omitempty"`
	Context   string          `json:"context,omitempty"`
	Tools     json.RawMessage `json:"tools,omitempty"`
	// Hook fields
	Payload json.RawMessage `json:"payload,omitempty"`
	Action  string          `json:"action,omitempty"`
	Reason  string          `json:"reason,omitempty"`
	// Meta is an optional opaque JSON object carried alongside the message.
	// The kernel does not interpret its contents; it is forwarded as-is.
	// Conventional uses:
	//   - on `init`:        boot metadata such as `{"default_agent": "build"}`
	//   - on `tool_result`: `{"diff": "...", "files": ["a", "b"], "summary": "..."}`
	Meta    json.RawMessage `json:"meta,omitempty"`
	release func()
}

func cloneMessage(msg *BusMessage) *BusMessage {
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
	if msg.Artifact != nil {
		clone.Artifact = append(json.RawMessage(nil), msg.Artifact...)
	}
	if msg.Meta != nil {
		clone.Meta = append(json.RawMessage(nil), msg.Meta...)
	}
	if msg.Data != nil {
		clone.Data = append(json.RawMessage(nil), msg.Data...)
	}
	clone.release = nil
	return &clone
}

func mustMarshalRaw(value any) json.RawMessage {
	raw, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return raw
}

func eventText(msg *BusMessage) string {
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

func messageText(msg *BusMessage) string {
	if msg == nil || BusMessageType(msg.Type) != MsgEvent {
		return ""
	}
	return eventText(msg)
}

func messageCapability(msg *BusMessage) string {
	if msg == nil {
		return ""
	}
	if (BusMessageType(msg.Type) == MsgEvent || BusMessageType(msg.Type) == MsgRequest || BusMessageType(msg.Type) == MsgReply) && msg.Topic != "" {
		return msg.Topic
	}
	return msg.Type
}
