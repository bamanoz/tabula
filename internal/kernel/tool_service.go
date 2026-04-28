package kernel

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

const maxExecOutput = 16 * 1024 // 16KB

type ToolService struct {
	hub     *Hub
	process *ProcessManager
	skill   *SkillExec
}

func NewToolService(hub *Hub) *ToolService {
	pm := NewProcessManager(hub, nil)
	return &ToolService{
		hub:     hub,
		process: pm,
		skill:   NewSkillExec(pm),
	}
}

// HandleToolUse routes a tool_use message through the before_tool_call hook
// and dispatches the result to a registered skill-exec tool.
//
// As of Phase 1 D1.2 of the skill/plugin architecture migration, kernel no
// longer hosts builtin LLM tools (shell_exec, process_spawn, process_kill,
// process_list). All tools flow through Hub.toolExec (skill exec dispatch).
// Phase 2 will extend this with plugin-tool dispatch.
func (s *ToolService) HandleToolUse(sender *Client, msg *Message) {
	toolName := msg.Name
	toolID := msg.ID
	session := sender.session

	s.hub.Logger.Debug("tool_use", "tool", toolName, "session", session, "id", toolID)

	effectiveInput, ok := s.hub.policy.CanUseTool(sender, toolName, toolID, msg.Input, session)
	if !ok {
		s.hub.sendToolResultForTool(session, toolID, toolName, "ERROR: blocked by hook")
		return
	}
	msg.Input = effectiveInput
	s.hub.broadcastToSession(session, string(MsgToolUse), &Message{
		Type:  string(MsgToolUse),
		ID:    toolID,
		Name:  toolName,
		Input: msg.Input,
	}, sender)

	s.handleDynamicTool(session, toolID, toolName, msg.Input)
}

func (s *ToolService) handleDynamicTool(session, toolID, toolName string, input json.RawMessage) {
	s.hub.toolExecMu.RLock()
	entry, ok := s.hub.toolExec[toolName]
	s.hub.toolExecMu.RUnlock()
	if !ok {
		s.hub.sendToolResultForTool(session, toolID, toolName, fmt.Sprintf("ERROR: unknown tool %s", toolName))
		return
	}
	switch entry.Source {
	case toolSourceSkill:
		s.skill.Run(session, toolID, toolName, entry.Command, input)
	case toolSourcePlugin:
		s.handlePluginTool(session, toolID, toolName, entry, input)
	default:
		s.hub.sendToolResultForTool(session, toolID, toolName, fmt.Sprintf("ERROR: tool %s has unknown dispatch source", toolName))
	}
}

func (s *ToolService) handlePluginTool(session, toolID, toolName string, entry toolDispatch, input json.RawMessage) {
	if entry.Plugin == nil || !entry.Plugin.IsAlive() || !entry.Plugin.IsRegistered() {
		s.hub.sendToolResultForTool(session, toolID, toolName, fmt.Sprintf("ERROR: plugin tool %s is unavailable", toolName))
		return
	}
	deadline := resolveToolDeadline(entry.DeadlineMs)
	ch, err := entry.Plugin.SendToolCall(&plugin.ToolCallParams{
		CallID:     toolID,
		Name:       toolName,
		Args:       input,
		Session:    session,
		DeadlineMs: int(deadline / time.Millisecond),
	})
	if err != nil {
		s.hub.Logger.Warn("plugin tool_call failed", "tool", toolName, "session", session, "err", err)
		s.hub.sendToolResultForTool(session, toolID, toolName, fmt.Sprintf("ERROR: plugin tool %s failed: %v", toolName, err))
		return
	}

	go func() {
		var output string
		select {
		case msg, ok := <-ch:
			if !ok {
				output = "ERROR: plugin call cancelled"
			} else {
				output = pluginToolOutput(msg)
			}
		case <-entry.Plugin.Done():
			entry.Plugin.CancelPending(toolID)
			output = "ERROR: plugin crashed"
		case <-time.After(deadline):
			entry.Plugin.CancelPending(toolID)
			output = fmt.Sprintf("ERROR: plugin timeout after %dms", int(deadline/time.Millisecond))
		}
		s.hub.sendToolResultForTool(session, toolID, toolName, output)
		s.hub.emitAfterToolCall(session, toolID, map[string]string{
			"tool": toolName, "id": toolID, "output": output,
		})
	}()
}

func pluginToolOutput(msg *plugin.Message) string {
	if msg == nil {
		return "ERROR: plugin returned empty result"
	}
	var result plugin.ToolResultParams
	if err := msg.DecodeParams(&result); err != nil {
		return fmt.Sprintf("ERROR: invalid plugin tool_result: %v", err)
	}
	if result.Error != "" {
		return "ERROR: " + result.Error
	}
	if len(result.Result) == 0 {
		return "OK"
	}
	var text string
	if err := json.Unmarshal(result.Result, &text); err == nil {
		return text
	}
	return strings.TrimSpace(string(result.Result))
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

type commandToolInput struct {
	Command string `json:"command"`
}

// parseCommandToolInput is kept as a small reusable helper for tools whose
// JSON shape is `{"command": "..."}`. After kernel cleanup it has no
// in-tree caller; tests may still reference it. Skill or plugin tools
// implementing shell-style semantics may use it via shared package import
// in the future.
//
//nolint:unused // retained per Phase 1 D1.11(b) dead-code-keep policy
func parseCommandToolInput(input json.RawMessage) (commandToolInput, error) {
	var parsed commandToolInput
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.Command == "" {
		return commandToolInput{}, fmt.Errorf("missing or invalid command")
	}
	return parsed, nil
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
