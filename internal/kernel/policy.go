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
func (pe *PolicyEngine) CanConnect(spawnToken string, authToken string) (int, error) {
	if spawnToken != "" {
		return 0, &PolicyError{Reason: "spawn tokens are no longer accepted by the kernel"}
	}
	if pe == nil || pe.hub == nil || !tokenMatches(pe.hub.clientAuthToken, authToken) {
		return 0, &PolicyError{Reason: "invalid kernel client token"}
	}
	return 0, nil
}

func (pe *PolicyEngine) CanRespondHook(sender *Client, msg *Message) error {
	if sender == nil || !sender.IsConnected() {
		return &PolicyError{Reason: "client not connected"}
	}
	if msg == nil || msg.ID == "" {
		return &PolicyError{Reason: "hook_reply missing id"}
	}
	if pe == nil || pe.hub == nil || pe.hub.hooks == nil || !pe.hub.hooks.CanHandleResult(sender, msg.ID) {
		return &PolicyError{Reason: "client not allowed to answer hook"}
	}
	return nil
}

func (pe *PolicyEngine) joinHookPayload(session string, tenantID string, clientName string) []byte {
	hookPayload, _ := json.Marshal(map[string]string{
		"session":   session,
		"tenant_id": tenantID,
		"client":    clientName,
	})
	return hookPayload
}

// StartSession runs the session_start hook once for a newly-created session and
// returns (context, blocked). Returns ("", true) if the hook blocks startup.
func (pe *PolicyEngine) StartSession(session string, tenantID string, clientName string) (string, bool) {
	hookPayload := pe.joinHookPayload(session, tenantID, clientName)
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

// SessionJoin emits a non-blocking observability event for every successful
// join, including joins to existing sessions.
func (pe *PolicyEngine) SessionJoin(session string, tenantID string, clientName string) {
	pe.hub.dispatchHook("session_join", pe.joinHookPayload(session, tenantID, clientName), session)
}

// BeforePromptBuild lets plugins contribute prompt-build context for init-capable
// clients without mutating the persisted session_start init context.
func (pe *PolicyEngine) BeforePromptBuild(session string, tenantID string, clientName string, context string, tools json.RawMessage, meta json.RawMessage) string {
	hookPayload, _ := json.Marshal(map[string]any{
		"session":   session,
		"tenant_id": tenantID,
		"client":    clientName,
		"context":   context,
		"tools":     tools,
		"meta":      meta,
	})
	result, ok := pe.hub.dispatchHook("before_prompt_build", hookPayload, session)
	if !ok {
		return context
	}

	var hookData struct{ Context string }
	if json.Unmarshal(result, &hookData) == nil && hookData.Context != "" {
		return hookData.Context
	}
	return context
}

// CanSend checks whether a client is allowed to send a message.
// Combines state, capability, and session membership checks.
func (pe *PolicyEngine) CanSend(sender *Client, msg *Message) error {
	if !sender.IsConnected() {
		return &PolicyError{Reason: "client not connected"}
	}
	capability := messageCapability(msg)
	if !sender.canSend(capability) {
		return &PolicyError{Reason: fmt.Sprintf("client not allowed to send %s", capability)}
	}
	if MsgType(msg.Type) == MsgRequest && isExchangeTopic(msg.Topic) && sender.session == "" && msg.Session != "" {
		return nil
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
		"text":   messageText(msg),
		"sender": sender.name,
	})
	result, ok := pe.hub.dispatchHook("before_message", payload, sender.session)
	if !ok {
		return "", true
	}

	text := messageText(msg)
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
		"tool": toolName, "id": toolID, "input": input, "tenant_id": pe.hub.sessionTenantID(session),
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
