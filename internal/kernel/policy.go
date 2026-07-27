package kernel

import (
	"encoding/json"
	"fmt"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"

	"github.com/bamanoz/tabula/internal/kernel/clientauth"
)

// PolicyError describes why a policy check failed.
type PolicyError struct{ Reason string }

func (e *PolicyError) Error() string { return e.Reason }

// PolicyEngine is the single audit point for all security boundaries.
// It coordinates existing components (HookEngine, process supervision, ClientRegistry)
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
	if pe == nil || pe.hub == nil || !clientauth.Matches(pe.hub.clientAuthToken, authToken) {
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

func (pe *PolicyEngine) joinHookPayload(session string, tenantID string, clientName string, clientMeta json.RawMessage) []byte {
	payload := map[string]any{
		"session":   session,
		"tenant_id": tenantID,
		"client":    clientName,
	}
	if len(clientMeta) > 0 {
		var meta map[string]any
		if json.Unmarshal(clientMeta, &meta) == nil && len(meta) > 0 {
			payload["meta"] = meta
		}
	}
	hookPayload, _ := json.Marshal(payload)
	return hookPayload
}

// StartSession runs the session_start hook once for a newly-created session and
// returns (context, blocked). Returns ("", true) if the hook blocks startup.
func (pe *PolicyEngine) StartSession(session string, tenantID string, clientName string) (string, bool) {
	hookPayload := pe.joinHookPayload(session, tenantID, clientName, nil)
	result, ok := pe.hub.dispatchHook("session_start", hookPayload, tenantID, session)
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
func (pe *PolicyEngine) SessionJoin(session string, tenantID string, clientName string, clientMeta json.RawMessage) {
	pe.hub.dispatchHook("session_join", pe.joinHookPayload(session, tenantID, clientName, clientMeta), tenantID, session)
}

// BeforePromptBuild lets plugins contribute prompt-build context and filter the
// init tool surface for init-capable clients without mutating persisted session
// state.
func (pe *PolicyEngine) BeforePromptBuild(session string, tenantID string, clientName string, context string, tools json.RawMessage, meta json.RawMessage) (string, json.RawMessage) {
	hookPayload, _ := json.Marshal(map[string]any{
		"session":   session,
		"tenant_id": tenantID,
		"client":    clientName,
		"context":   context,
		"tools":     tools,
		"meta":      meta,
	})
	result, ok := pe.hub.dispatchHook("before_prompt_build", hookPayload, tenantID, session)
	if !ok {
		return context, tools
	}

	var hookData struct {
		Context string          `json:"context"`
		Tools   json.RawMessage `json:"tools"`
	}
	if json.Unmarshal(result, &hookData) != nil {
		return context, tools
	}
	if hookData.Context != "" {
		context = hookData.Context
	}
	if len(hookData.Tools) > 0 {
		tools = append(json.RawMessage(nil), hookData.Tools...)
	}
	return context, tools
}

// BeforeTurn lets plugins attach transient per-turn context without mutating
// the persisted session init context or rewriting the user's text.
func (pe *PolicyEngine) BeforeTurn(session string, tenantID string, msg *Message) string {
	if msg == nil {
		return ""
	}
	hookPayload, _ := json.Marshal(map[string]any{
		"session":             session,
		"tenant_id":           tenantID,
		"message_id":          msg.ID,
		"text":                messageText(msg),
		"meta":                metaMap(msg.Meta),
		"turn_correlation_id": metaString(msg.Meta, turnCorrelationMetaKey),
	})
	result, ok := pe.hub.dispatchHook("before_turn", hookPayload, tenantID, session)
	if !ok {
		return ""
	}

	var hookData struct {
		Context string `json:"context"`
	}
	if json.Unmarshal(result, &hookData) != nil {
		return ""
	}
	return hookData.Context
}

// CanSend checks whether a client is allowed to send a message.
// Combines state, capability, and session membership checks.
func (pe *PolicyEngine) CanSend(sender *Client, msg *Message) error {
	if !sender.IsConnected() {
		return &PolicyError{Reason: "client not connected"}
	}
	capability := messageCapability(msg)
	if capability == TopicKernelSessions {
		return nil
	}
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
	result, ok := pe.hub.dispatchHook("before_message", payload, sender.tenantID, sender.session)
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

// CanUseTool runs the before_tool_call hook and returns the effective input.
// If a hook stops dispatch before invoke, blocked contains the generic hook facts.
func (pe *PolicyEngine) CanUseTool(sender *Client, toolName string, toolID string, input json.RawMessage, meta json.RawMessage, session string) (json.RawMessage, *khooks.DispatchDecision) {
	hookPayload, _ := json.Marshal(map[string]any{
		"tool": toolName, "id": toolID, "input": input, "meta": meta, "tenant_id": sender.tenantID,
	})
	result, ok, blocked := pe.hub.hooks.DispatchDetailedExcept("before_tool_call", hookPayload, sender.tenantID, session, sender)

	var modified struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(result, &modified); err != nil {
		return input, blocked
	}
	if modified.Input == nil {
		return input, blocked
	}
	if !ok {
		return modified.Input, blocked
	}
	return modified.Input, nil
}
