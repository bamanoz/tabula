package kernel

import (
	"encoding/json"
	"fmt"
)

// PolicyError describes why a policy check failed.
type PolicyError struct{ Reason string }

func (e *PolicyError) Error() string { return e.Reason }

// PolicyEngine is the single audit point for all security boundaries.
// It coordinates existing components (HookEngine, ProcessSupervisor, ClientRegistry)
// without duplicating their logic.
type PolicyEngine struct {
	hub *Hub
}

// NewPolicyEngine creates a PolicyEngine.
func NewPolicyEngine(hub *Hub) *PolicyEngine {
	return &PolicyEngine{hub: hub}
}

// CanConnect validates connect-time policy. Kernel-managed spawn tokens were
// removed when subagent process ownership moved to plugin-side orchestration.
func (pe *PolicyEngine) CanConnect(token string) (int, error) {
	if token != "" {
		return 0, &PolicyError{Reason: "spawn tokens are no longer accepted by the kernel"}
	}
	return 0, nil
}

// CanJoin runs the session_start hook and returns (context, blocked).
// Returns ("", true) if the hook blocks the join.
func (pe *PolicyEngine) CanJoin(session string, clientName string) (string, bool) {
	hookPayload, _ := json.Marshal(map[string]string{
		"session": session,
		"client":  clientName,
	})
	result, ok := pe.hub.dispatchHook("session_start", hookPayload, session)
	if !ok {
		return "", true
	}

	var hookData struct{ Context string }
	if json.Unmarshal(result, &hookData) == nil && hookData.Context != "" {
		return hookData.Context, false
	}
	return "", false
}

// CanSend checks whether a client is allowed to send a message.
// Combines state, capability, and session membership checks.
func (pe *PolicyEngine) CanSend(sender *Client, msg *Message) error {
	if !sender.IsConnected() {
		return &PolicyError{Reason: "client not connected"}
	}
	if !sender.canSend(msg.Type) {
		return &PolicyError{Reason: fmt.Sprintf("client not allowed to send %s", msg.Type)}
	}
	if sender.session == "" {
		return &PolicyError{Reason: "client not in a session"}
	}
	return nil
}

// BeforeMessage runs the before_message hook and returns (modifiedText, blocked).
// Returns ("", true) if the hook blocks the message.
func (pe *PolicyEngine) BeforeMessage(sender *Client, msg *Message) (string, bool) {
	payload, _ := json.Marshal(map[string]string{
		"text":   msg.Text,
		"sender": sender.name,
	})
	result, ok := pe.hub.dispatchHook("before_message", payload, sender.session)
	if !ok {
		return "", true
	}

	text := msg.Text
	var modified struct{ Text string }
	if json.Unmarshal(result, &modified) == nil && modified.Text != "" {
		text = modified.Text
	}
	return text, false
}

// CanUseTool runs the before_tool_call hook and returns (result, ok).
// Returns (nil, false) if the hook blocks the tool use.
func (pe *PolicyEngine) CanUseTool(sender *Client, toolName string, toolID string, input json.RawMessage, session string) (json.RawMessage, bool) {
	hookPayload, _ := json.Marshal(map[string]any{
		"tool": toolName, "id": toolID, "input": input,
	})
	result, ok := pe.hub.dispatchHookExcept("before_tool_call", hookPayload, session, sender)
	if !ok {
		return nil, false
	}

	var modified struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(result, &modified); err != nil {
		return input, true
	}
	if modified.Input == nil {
		return input, true
	}
	return modified.Input, true
}
