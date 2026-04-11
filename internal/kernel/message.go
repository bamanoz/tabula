package kernel

import "encoding/json"

// Message is the generic JSON message exchanged over WebSocket.
type Message struct {
	Type     string          `json:"type"`
	Name     string          `json:"name,omitempty"`
	Session  string          `json:"session,omitempty"`
	ID       string          `json:"id,omitempty"`
	Text     string          `json:"text,omitempty"`
	Input    json.RawMessage `json:"input,omitempty"`
	Output   string          `json:"output,omitempty"`
	Prompt   string          `json:"prompt,omitempty"`
	Tools    json.RawMessage `json:"tools,omitempty"`
	Sends    []string        `json:"sends,omitempty"`
	Receives []string        `json:"receives,omitempty"`
	Token    string          `json:"token,omitempty"`
	// Hook fields
	Hooks   []HookSubscription `json:"hooks,omitempty"`
	Payload json.RawMessage    `json:"payload,omitempty"`
	Action  string             `json:"action,omitempty"`
	Reason  string             `json:"reason,omitempty"`
}
