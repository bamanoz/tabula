package kernel

import (
	"encoding/json"
	"strings"

	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
)

func (s *ToolService) suspendForExchange(tenantID, session, toolID, toolName string, input, meta json.RawMessage, correlation toolAttemptContext, blocked *khooks.DispatchDecision) {
	turnCorrelationID := correlation.TurnCorrelationID
	exchangeID := exchangeIDFromPayload(blocked.Payload)
	if exchangeID == "" {
		exchangeID = generateExchangeID()
	}
	exchangeTopic := exchangeTopicFromPayload(blocked.Payload)
	pending := pendingToolCall{
		ExchangeID:        exchangeID,
		ExchangeData:      exchangeRequestDataFromDecision(toolName, blocked),
		BlockedDecision:   blocked,
		ExchangeTopic:     exchangeTopic,
		TenantID:          tenantID,
		Session:           session,
		ToolID:            toolID,
		ToolName:          toolName,
		Input:             append(json.RawMessage(nil), input...),
		Meta:              append(json.RawMessage(nil), meta...),
		TurnCorrelationID: turnCorrelationID,
		AttemptContext:    correlation,
	}
	s.mu.Lock()
	s.pendingCalls[exchangeID] = pending
	s.mu.Unlock()
	s.hub.Logger.Info("tool call suspended for exchange", "exchange_id", exchangeID, "tool", toolName, "tool_call_id", toolID, "turn_correlation_id", turnCorrelationID, "tenant_id", tenantID, "session", session)
	s.hub.recordToolSuspended(tenantID, session, toolID, toolName, exchangeID, correlation)
	s.hub.broadcastToSession(tenantID, session, "tool.suspended", &BusMessage{Type: string(MsgEvent), Topic: "tool.suspended", ID: toolID, Name: toolName, Data: mustMarshalRaw(map[string]any{"exchange_id": exchangeID, "tool_call_id": toolID, "tool": toolName, "reason": blocked.Reason})}, nil)
	s.hub.requestExchangeForPendingTool(pending, blocked)
}

func (s *ToolService) resolvePendingExchange(exchangeID string) bool {
	s.mu.Lock()
	pending, ok := s.pendingCalls[exchangeID]
	if ok {
		delete(s.pendingCalls, exchangeID)
	}
	s.mu.Unlock()
	if !ok {
		return false
	}
	releaseBlockedHookDecision(pending.BlockedDecision)
	if err := s.hub.validateToolAttempt(pending.TenantID, pending.Session, pending.AttemptContext); err != nil {
		s.hub.Logger.Warn("stale suspended tool exchange rejected", "exchange_id", exchangeID, "tool", pending.ToolName, "tool_call_id", pending.ToolID, "turn_id", pending.AttemptContext.TurnID, "attempt_id", pending.AttemptContext.AttemptID, "driver_generation", pending.AttemptContext.DriverGeneration, "err", err)
		s.hub.recordToolTerminal(pending.TenantID, pending.Session, pending.ToolID, pending.ToolName, "fenced", pending.AttemptContext)
		return true
	}
	s.hub.broadcastToSession(pending.TenantID, pending.Session, "tool.resumed", &BusMessage{Type: string(MsgEvent), Topic: "tool.resumed", ID: pending.ToolID, Name: pending.ToolName, Data: mustMarshalRaw(map[string]any{"exchange_id": exchangeID, "tool_call_id": pending.ToolID, "tool": pending.ToolName})}, nil)

	inputWithReply := markToolInputExchangeReply(pending.Input, pending.ExchangeReply)
	hookPayload, _ := json.Marshal(map[string]any{
		"tool": pending.ToolName, "id": pending.ToolID, "input": inputWithReply, "meta": pending.Meta, "tenant_id": pending.TenantID,
	})
	effectivePayload, ok, blocked := s.hub.hooks.DispatchDetailedExcept("before_tool_call", hookPayload, pending.TenantID, pending.Session, nil)
	effectiveInput := inputFromToolHookPayload(effectivePayload, inputWithReply)
	if blocked != nil {
		if blocked.Pending {
			s.suspendForExchange(pending.TenantID, pending.Session, pending.ToolID, pending.ToolName, effectiveInput, pending.Meta, pending.AttemptContext, blocked)
			return true
		}
		s.hub.Logger.Warn("tool call blocked by policy after exchange", "tool", pending.ToolName, "tool_call_id", pending.ToolID, "turn_correlation_id", pending.TurnCorrelationID, "tenant_id", pending.TenantID, "session", pending.Session, "input_bytes", len(pending.Input))
		s.hub.sendToolResultForTool(pending.TenantID, pending.Session, pending.ToolID, pending.ToolName, buildNotInvokedToolResult(blocked), nil, false, pending.AttemptContext)
		return true
	}
	if !ok {
		s.hub.sendToolResultForTool(pending.TenantID, pending.Session, pending.ToolID, pending.ToolName, buildNotInvokedToolResult(&khooks.DispatchDecision{Reason: "tool hook dispatch did not complete after exchange"}), nil, false, pending.AttemptContext)
		return true
	}
	s.broadcastFinalizedToolCall(pending.TenantID, pending.Session, pending.ToolID, pending.ToolName, effectiveInput, pending.Meta, nil)
	s.handleDynamicToolWithAttempt(pending.TenantID, pending.Session, pending.ToolID, pending.ToolName, effectiveInput, pending.Meta, pending.AttemptContext)
	return true
}

func releaseBlockedHookDecision(decision *khooks.DispatchDecision) {
	khooks.ReleaseDecision(decision)
}

func (s *ToolService) pendingCall(exchangeID string) (pendingToolCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.pendingCalls[exchangeID]
	return pending, ok
}

func (s *ToolService) setPendingExchangeReply(exchangeID string, reply json.RawMessage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	pending, ok := s.pendingCalls[exchangeID]
	if !ok {
		return
	}
	pending.ExchangeReply = append(json.RawMessage(nil), reply...)
	s.pendingCalls[exchangeID] = pending
}

func inputFromToolHookPayload(raw json.RawMessage, fallback json.RawMessage) json.RawMessage {
	var payload struct {
		Input json.RawMessage `json:"input"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &payload) == nil && len(payload.Input) > 0 {
		return payload.Input
	}
	return fallback
}

func exchangeIDFromPayload(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload struct {
		ExchangeID string `json:"exchange_id"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return ""
	}
	return strings.TrimSpace(payload.ExchangeID)
}

func exchangeTopicFromPayload(raw json.RawMessage) string {
	if len(raw) == 0 {
		return TopicExchangeApprove
	}
	var payload struct {
		Topic string `json:"topic"`
	}
	if json.Unmarshal(raw, &payload) != nil {
		return TopicExchangeApprove
	}
	switch strings.TrimSpace(payload.Topic) {
	case TopicExchangeChoose:
		return TopicExchangeChoose
	default:
		return TopicExchangeApprove
	}
}

func generateExchangeID() string {
	return "exchange-" + khooks.GenerateID()
}

func markToolInputExchangeReply(raw json.RawMessage, reply json.RawMessage) json.RawMessage {
	var input map[string]any
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &input)
	}
	if input == nil {
		input = map[string]any{}
	}
	var data any = map[string]any{}
	if len(reply) > 0 {
		_ = json.Unmarshal(reply, &data)
	}
	input["__tabula_exchange_reply"] = data
	return mustMarshalRaw(input)
}
