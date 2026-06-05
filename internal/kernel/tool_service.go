package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

const maxExecOutput = 16 * 1024 // 16KB

type ToolService struct {
	hub *Hub
}

func NewToolService(hub *Hub) *ToolService {
	return &ToolService{hub: hub}
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

	s.hub.Logger.Debug("tool.call", "tool", toolName, "tenant_id", tenantID, "session", session, "id", toolID)

	effectiveInput, ok := s.hub.policy.CanUseTool(sender, toolName, toolID, msg.Input, msg.Meta, session)
	if !ok {
		s.hub.Logger.Warn("tool call blocked by policy", "tool", toolName, "tool_call_id", toolID, "tenant_id", tenantID, "session", session, "input_bytes", len(msg.Input))
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: blocked", nil, false)
		return
	}
	msg.Input = effectiveInput
	s.hub.broadcastToSessionFrom(tenantID, session, TopicToolCall, &Message{
		Type:  string(MsgRequest),
		Topic: TopicToolCall,
		ID:    toolID,
		Name:  toolName,
		Input: msg.Input,
		Meta:  msg.Meta,
	}, sender, sender)

	s.handleDynamicTool(tenantID, session, toolID, toolName, msg.Input)
}

func (s *ToolService) handleDynamicTool(tenantID, session, toolID, toolName string, input json.RawMessage) {
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
		s.handleRuntimeTool(tenantID, session, toolID, toolName, entry, input)
	default:
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: tool %s has unknown dispatch source", toolName), nil, false)
	}
}
func (s *ToolService) handleRuntimeTool(tenantID, session, toolID, toolName string, entry toolDispatch, input json.RawMessage) {
	go func() {
		var conn runtimeapi.RuntimeConn
		pickedRuntimeID := entry.RuntimeID
		var code wire.ErrorCode
		var pickErr error
		if pickedRuntimeID == "" {
			conn, pickedRuntimeID, code, pickErr = s.hub.pickRuntime(tenantID)
		} else {
			conn, code, pickErr = s.hub.runtimeForTenant(tenantID, pickedRuntimeID)
		}
		if pickErr != nil {
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: "+pickErr.Error(), nil, false)
			if code != "" {
				s.hub.Logger.Warn("runtime pick failed", "tool", toolName, "tool_call_id", toolID, "runtime_id", pickedRuntimeID, "tenant_id", tenantID, "session", session, "code", code, "err", pickErr)
			}
			return
		}
		if conn == nil {
			s.hub.Logger.Warn("runtime tool unavailable", "tool", toolName, "tool_call_id", toolID, "runtime_id", pickedRuntimeID, "tenant_id", tenantID, "session", session, "target_kind", string(entry.Target.Kind), "target", entry.Target.ID)
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: runtime tool %s is unavailable", toolName), nil, false)
			return
		}
		releaseRuntimeTarget := s.hub.markRuntimeTargetBusy(pickedRuntimeID, entry.Target)
		defer releaseRuntimeTarget()
		deadline := resolveToolDeadline(entry.DeadlineMs)
		ctx, cancel := context.WithTimeout(context.Background(), runtimeInvokeDeadline(deadline))
		defer cancel()

		resp, err := conn.Invoke(ctx, runtimeapi.InvokeReq{
			CallID:    toolID,
			TenantID:  tenantID,
			SessionID: session,
			Target:    entry.Target,
			Tool:      toolName,
			Args:      input,
			TimeoutMS: int64(deadline / time.Millisecond),
		})
		if err != nil {
			s.hub.Logger.Warn("runtime tool invoke failed", "tool", toolName, "tool_call_id", toolID, "runtime_id", pickedRuntimeID, "tenant_id", tenantID, "session", session, "target_kind", string(entry.Target.Kind), "target", entry.Target.ID, "err", err)
			if ctx.Err() != nil {
				s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: invoke timed out", nil, false)
				return
			}
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: runtime tool %s failed: %v", toolName, err), nil, false)
			return
		}
		result := runtimeToolOutput(resp)
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, result.Output, result.Artifact, result.Truncated)
		s.hub.emitAfterToolCall(tenantID, session, toolID, map[string]string{
			"tool": toolName, "id": toolID, "output": result.Output,
		})
	}()
}

type runtimeToolResult struct {
	Output    string
	Artifact  json.RawMessage
	Truncated bool
}

func runtimeToolOutput(resp runtimeapi.InvokeResp) runtimeToolResult {
	if resp.OK {
		if len(resp.Data) == 0 {
			return runtimeToolResult{Output: "OK"}
		}
		var payload map[string]json.RawMessage
		if err := json.Unmarshal(resp.Data, &payload); err == nil {
			if _, ok := payload["output"]; ok {
				var parsed struct {
					Output    string          `json:"output"`
					Artifact  json.RawMessage `json:"artifact,omitempty"`
					Truncated bool            `json:"truncated,omitempty"`
				}
				if err := json.Unmarshal(resp.Data, &parsed); err == nil {
					return runtimeToolResult{Output: parsed.Output, Artifact: parsed.Artifact, Truncated: parsed.Truncated}
				}
			}
		}
		var text string
		if err := json.Unmarshal(resp.Data, &text); err == nil {
			return runtimeToolResult{Output: text}
		}
		return runtimeToolResult{Output: strings.TrimSpace(string(resp.Data))}
	}
	if resp.Error == nil {
		return runtimeToolResult{Output: "ERROR: runtime returned empty error"}
	}
	if resp.Error.Message != "" {
		return runtimeToolResult{Output: "ERROR: " + resp.Error.Message}
	}
	return runtimeToolResult{Output: "ERROR: " + string(resp.Error.Code)}
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
	if deadlineMs > 600000 {
		deadlineMs = 600000
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
