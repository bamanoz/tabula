package kernel

import "encoding/json"

func busMessage(msgType, session string, payload json.RawMessage) *BusMessage {
	return &BusMessage{Type: msgType, Session: session, Payload: payload}
}
