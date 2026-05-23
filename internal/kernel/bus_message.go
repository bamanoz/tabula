package kernel

import "encoding/json"

func busMessage(msgType, session string, payload json.RawMessage) *Message {
	msg := &Message{
		Type:    msgType,
		Session: session,
		Payload: payload,
	}
	if msgType != TopicMessageUser || len(payload) == 0 {
		return msg
	}
	msg.Type = string(MsgEvent)
	msg.Topic = TopicMessageUser
	var body struct {
		ID   string          `json:"id"`
		Text string          `json:"text"`
		Meta json.RawMessage `json:"meta"`
	}
	if json.Unmarshal(payload, &body) != nil {
		return msg
	}
	if body.ID != "" {
		msg.ID = body.ID
	}
	if body.Text != "" {
		setMessageText(msg, body.Text)
	}
	if len(body.Meta) > 0 {
		msg.Meta = body.Meta
	}
	return msg
}
