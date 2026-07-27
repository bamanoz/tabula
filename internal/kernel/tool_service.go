package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
	"os/exec"
	"strings"
	"sync"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

const maxExecOutput = 16 * 1024 // 16KB

type ToolService struct {
	hub *Hub

	mu           sync.Mutex
	activeCalls  map[sessionKey]map[string]activeRuntimeCall
	pendingCalls map[string]pendingToolCall
}

func NewToolService(hub *Hub) *ToolService {
	return &ToolService{hub: hub, activeCalls: make(map[sessionKey]map[string]activeRuntimeCall), pendingCalls: make(map[string]pendingToolCall)}
}

func (h *Hub) handleToolUse(sender *Client, msg *Message) {
	h.tools.HandleToolUse(sender, msg)
}

type activeRuntimeCall struct {
	callID string
	conn   runtimeapi.RuntimeConn
}

type pendingToolCall struct {
	ExchangeID        string
	ExchangeData      json.RawMessage
	BlockedDecision   *khooks.DispatchDecision
	ExchangeTopic     string
	ExchangeReply     json.RawMessage
	TenantID          string
	Session           string
	ToolID            string
	ToolName          string
	Input             json.RawMessage
	Meta              json.RawMessage
	TurnCorrelationID string
}

// HandleToolUse routes a tool.call request through the before_tool_call hook
// and dispatches the result to a runtime-hosted tool.
//
// The kernel does not host builtin LLM tools. Runtime tools are dispatched
// through the unified tool registry populated by attached runtime targets.
func (s *ToolService) HandleToolUse(sender *Client, msg *Message) {
	toolName := msg.Name
	toolID := msg.ID
	session := sender.session
	tenantID := sender.tenantID

	turnCorrelationID := metaString(msg.Meta, turnCorrelationMetaKey)
	s.hub.Logger.Debug("tool.call", "tool", toolName, "tenant_id", tenantID, "session", session, "id", toolID, "turn_correlation_id", turnCorrelationID)
	s.hub.recordToolStarted(tenantID, session, toolID, toolName)

	effectiveInput, blocked := s.hub.policy.CanUseTool(sender, toolName, toolID, msg.Input, msg.Meta, session)
	if blocked != nil {
		if blocked.Pending {
			s.suspendForExchange(tenantID, session, toolID, toolName, effectiveInput, msg.Meta, turnCorrelationID, blocked)
			return
		}
		s.hub.Logger.Warn("tool call blocked by policy", "tool", toolName, "tool_call_id", toolID, "turn_correlation_id", turnCorrelationID, "tenant_id", tenantID, "session", session, "input_bytes", len(msg.Input))
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, buildNotInvokedToolResult(blocked), nil, false)
		return
	}
	s.executeFinalizedToolCall(tenantID, session, toolID, toolName, effectiveInput, msg.Meta, turnCorrelationID, sender)
}

func (s *ToolService) executeFinalizedToolCall(tenantID, session, toolID, toolName string, input, meta json.RawMessage, turnCorrelationID string, sender *Client) {
	s.broadcastFinalizedToolCall(tenantID, session, toolID, toolName, input, meta, sender)
	s.handleDynamicTool(tenantID, session, toolID, toolName, input, turnCorrelationID)
}

func (s *ToolService) broadcastFinalizedToolCall(tenantID, session, toolID, toolName string, input, meta json.RawMessage, sender *Client) {
	s.hub.broadcastToSessionFrom(tenantID, session, TopicToolCall, &Message{
		Type:  string(MsgRequest),
		Topic: TopicToolCall,
		ID:    toolID,
		Name:  toolName,
		Input: input,
		Meta:  meta,
	}, sender, sender)
}

func (s *ToolService) handleDynamicTool(tenantID, session, toolID, toolName string, input json.RawMessage, turnCorrelationID string) {
	s.hub.toolExecMu.RLock()
	entry, ok := s.hub.toolExec[toolExecKey(tenantID, toolName)]
	if !ok {
		entry, ok = s.hub.toolExec[toolName]
	}
	s.hub.toolExecMu.RUnlock()
	if !ok {
		count, visibleElsewhere := s.hub.toolDispatchDiagnostics(tenantID, toolName)
		s.hub.Logger.Warn(
			"unknown tool dispatch",
			"tool", toolName,
			"tool_call_id", toolID,
			"turn_correlation_id", turnCorrelationID,
			"tenant_id", tenantID,
			"session", session,
			"tool_registry_entries", count,
			"tool_visible_in_other_tenant", visibleElsewhere,
		)
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: unknown tool %s", toolName), nil, false)
		return
	}
	switch entry.Source {
	case toolSourceRuntime:
		s.handleRuntimeTool(tenantID, session, toolID, toolName, entry, input, turnCorrelationID)
	default:
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: tool %s has unknown dispatch source", toolName), nil, false)
	}
}

func buildNotInvokedToolResult(blocked *khooks.DispatchDecision) string {
	hook := map[string]any{
		"event":        blocked.Event,
		"target":       blocked.Target,
		"hook_id":      blocked.HookID,
		"reply_action": blocked.ReplyAction,
		"reason":       blocked.Reason,
		"status":       blocked.Status,
	}
	result := map[string]any{
		"ok":    false,
		"error": "not_invoked",
		"hook":  hook,
	}
	switch blocked.Status {
	case "disconnected":
		result["kind"] = "hook_disconnected"
		result["retryable"] = true
	case "timeout":
		result["kind"] = "hook_timeout"
		result["retryable"] = false
	}
	if len(blocked.Payload) > 0 {
		var details any
		if json.Unmarshal(blocked.Payload, &details) == nil {
			hook["details"] = details
			if m, ok := details.(map[string]any); ok {
				if kind, ok := m["kind"].(string); ok && kind != "" {
					result["kind"] = kind
				}
				if retryable, ok := m["retryable"].(bool); ok {
					result["retryable"] = retryable
				}
			}
		}
	}
	return string(mustMarshalRaw(result))
}

func (s *ToolService) handleRuntimeTool(tenantID, session, toolID, toolName string, entry toolDispatch, input json.RawMessage, turnCorrelationID string) {
	go func() {
		var conn runtimeapi.RuntimeConn
		pickedRuntimeID := entry.RuntimeID
		var code wire.ErrorCode
		var pickErr error
		if pickedRuntimeID == "" {
			conn, pickedRuntimeID, code, pickErr = s.hub.pickRuntimeForSession(tenantID, session)
		} else {
			conn, code, pickErr = s.hub.runtimeForTenant(tenantID, pickedRuntimeID)
		}
		if pickErr != nil {
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: "+pickErr.Error(), nil, false)
			if code != "" {
				s.hub.Logger.Warn("runtime pick failed", "tool", toolName, "tool_call_id", toolID, "turn_correlation_id", turnCorrelationID, "runtime_id", pickedRuntimeID, "tenant_id", tenantID, "session", session, "code", code, "err", pickErr)
			}
			return
		}
		if conn == nil {
			s.hub.Logger.Warn("runtime tool unavailable", "tool", toolName, "tool_call_id", toolID, "turn_correlation_id", turnCorrelationID, "runtime_id", pickedRuntimeID, "tenant_id", tenantID, "session", session, "target_kind", string(entry.Target.Kind), "target", entry.Target.ID)
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: runtime tool %s is unavailable", toolName), nil, false)
			return
		}
		unregisterActiveCall := s.registerActiveRuntimeCall(tenantID, session, toolID, activeRuntimeCall{callID: toolID, conn: conn})
		defer unregisterActiveCall()
		deadline := resolveToolDeadline(entry.DeadlineMs)
		ctx, cancel := context.WithTimeout(context.Background(), runtimeInvokeDeadline(deadline))
		defer cancel()
		spool, err := newInvokeResultSpool(s.hub, tenantID, session, toolID)
		if err != nil {
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: runtime tool %s failed: %v", toolName, err), nil, false)
			return
		}
		spool.toolName = toolName
		defer spool.Close()
		stopWatch := spool.watchIdle(ctx, cancel, toolResultSpoolIdleTimeout)
		defer stopWatch()

		resp, err := conn.InvokeStream(ctx, runtimeapi.InvokeReq{
			CallID:            toolID,
			TenantID:          tenantID,
			SessionID:         session,
			TurnCorrelationID: turnCorrelationID,
			Target:            entry.Target,
			Tool:              toolName,
			Args:              input,
			TimeoutMS:         int64(deadline / time.Millisecond),
		}, spool)
		if err != nil {
			if ctx.Err() == context.Canceled && spool.TerminalState() == invokeResultSpoolTimedOut {
				s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: streamed tool result timed out", nil, false)
				return
			}
			spool.MarkTerminal(invokeResultSpoolFailed)
			s.hub.Logger.Warn("runtime tool invoke failed", "tool", toolName, "tool_call_id", toolID, "turn_correlation_id", turnCorrelationID, "runtime_id", pickedRuntimeID, "tenant_id", tenantID, "session", session, "target_kind", string(entry.Target.Kind), "target", entry.Target.ID, "err", err)
			if ctx.Err() != nil {
				spool.MarkTerminal(invokeResultSpoolTimedOut)
				s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: invoke timed out", nil, false)
				return
			}
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: runtime tool %s failed: %v", toolName, err), nil, false)
			return
		}
		if resp.OK {
			spool.MarkTerminal(invokeResultSpoolCompleted)
		} else if resp.Error != nil && resp.Error.Code == wire.ErrorCancelled {
			spool.MarkTerminal(invokeResultSpoolCancelled)
		} else if resp.Error != nil && resp.Error.Code == wire.ErrorTimeout {
			spool.MarkTerminal(invokeResultSpoolTimedOut)
		} else {
			spool.MarkTerminal(invokeResultSpoolFailed)
		}
		result, resultErr := s.finalizeRuntimeToolResult(tenantID, session, toolID, toolName, resp, spool)
		if resultErr != nil {
			result = runtimeToolResult{Output: "ERROR: " + resultErr.Error()}
		}
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, result.Output, result.Artifact, result.Truncated)
		s.hub.emitAfterToolCall(tenantID, session, toolID, map[string]string{
			"tool": toolName, "id": toolID, "output": result.Output,
		})
	}()
}

func (s *ToolService) registerActiveRuntimeCall(tenantID, session, toolID string, call activeRuntimeCall) func() {
	if s == nil || session == "" || toolID == "" || call.conn == nil {
		return func() {}
	}
	key := sessionRegistryKey(session, tenantID)
	s.mu.Lock()
	if s.activeCalls[key] == nil {
		s.activeCalls[key] = make(map[string]activeRuntimeCall)
	}
	s.activeCalls[key][toolID] = call
	s.mu.Unlock()
	return func() {
		s.mu.Lock()
		calls := s.activeCalls[key]
		if calls != nil {
			delete(calls, toolID)
			if len(calls) == 0 {
				delete(s.activeCalls, key)
			}
		}
		s.mu.Unlock()
	}
}

func (s *ToolService) CancelSession(tenantID, session string) {
	if s == nil || session == "" {
		return
	}
	key := sessionRegistryKey(session, tenantID)
	s.mu.Lock()
	calls := s.activeCalls[key]
	active := make([]activeRuntimeCall, 0, len(calls))
	for _, call := range calls {
		active = append(active, call)
	}
	s.mu.Unlock()
	for _, call := range active {
		go func(call activeRuntimeCall) {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := call.conn.Cancel(ctx, call.callID); err != nil {
				s.hub.Logger.Warn("runtime tool cancel failed", "tool_call_id", call.callID, "tenant_id", tenantID, "session", session, "err", err)
			}
		}(call)
	}
}

type runtimeToolResult struct {
	Output    string
	Artifact  json.RawMessage
	Truncated bool
}

func runtimeInvokeDeadline(toolDeadline time.Duration) time.Duration {
	if toolDeadline <= 0 {
		toolDeadline = 30 * time.Second
	}
	grace := 5 * time.Second
	if toolDeadline/10 > grace {
		grace = toolDeadline / 10
	}
	return toolDeadline + grace
}

func resolveToolDeadline(deadlineMs int) time.Duration {
	if deadlineMs <= 0 {
		deadlineMs = 30000
	}
	maxDeadline := int((time.Hour) / time.Millisecond)
	if deadlineMs > maxDeadline {
		deadlineMs = maxDeadline
	}
	return time.Duration(deadlineMs) * time.Millisecond
}

func formatCommandResult(out []byte, err error) string {
	if len(out) > maxExecOutput {
		out = append(out[:maxExecOutput], []byte("\n[truncated]")...)
	}

	result := strings.TrimSpace(string(out))
	if err != nil && result == "" {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result = fmt.Sprintf("ERROR: exit code %d", exitErr.ExitCode())
		} else {
			result = fmt.Sprintf("ERROR: %v", err)
		}
	}
	if result == "" {
		return "OK"
	}
	return result
}
