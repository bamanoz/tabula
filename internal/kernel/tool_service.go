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
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: blocked")
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
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: unknown tool %s", toolName))
		return
	}
	switch entry.Source {
	case toolSourceRuntime:
		s.handleRuntimeTool(tenantID, session, toolID, toolName, entry, input)
	default:
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: tool %s has unknown dispatch source", toolName))
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
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: "+pickErr.Error())
			if code != "" {
				s.hub.Logger.Warn("runtime pick failed", "tool", toolName, "runtime_id", pickedRuntimeID, "tenant_id", tenantID, "code", code, "err", pickErr)
			}
			return
		}
		if conn == nil {
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: runtime tool %s is unavailable", toolName))
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
			s.hub.Logger.Warn("runtime tool invoke failed", "tool", toolName, "runtime_id", pickedRuntimeID, "session", session, "err", err)
			if ctx.Err() != nil {
				s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, "ERROR: invoke timed out")
				return
			}
			s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, fmt.Sprintf("ERROR: runtime tool %s failed: %v", toolName, err))
			return
		}
		output := runtimeToolOutput(resp)
		s.hub.sendToolResultForTool(tenantID, session, toolID, toolName, output)
		s.hub.emitAfterToolCall(tenantID, session, toolID, map[string]string{
			"tool": toolName, "id": toolID, "output": output,
		})
	}()
}
func runtimeToolOutput(resp runtimeapi.InvokeResp) string {
	if resp.OK {
		if len(resp.Data) == 0 {
			return "OK"
		}
		var text string
		if err := json.Unmarshal(resp.Data, &text); err == nil {
			return text
		}
		return strings.TrimSpace(string(resp.Data))
	}
	if resp.Error == nil {
		return "ERROR: runtime returned empty error"
	}
	if resp.Error.Message != "" {
		return "ERROR: " + resp.Error.Message
	}
	return "ERROR: " + string(resp.Error.Code)
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
