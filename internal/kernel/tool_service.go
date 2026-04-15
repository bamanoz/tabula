package kernel

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
)

const maxExecOutput = 16 * 1024 // 16KB

type ToolService struct {
	hub      *Hub
	process  *ProcessManager
}

func NewToolService(hub *Hub) *ToolService {
	return &ToolService{
		hub:     hub,
		process: NewProcessManager(hub, nil),
	}
}

func (s *ToolService) HandleToolUse(sender *Client, msg *Message) {
	toolName := msg.Name
	toolID := msg.ID
	session := sender.session

	s.hub.Logger.Debug("tool_use", "tool", toolName, "session", session, "id", toolID)

	if _, ok := s.hub.policy.CanUseTool(toolName, toolID, msg.Input, session); !ok {
		s.hub.sendToolResult(session, toolID, "ERROR: blocked by hook")
		return
	}

	switch KernelTool(toolName) {
	case ToolEXEC:
		s.handleExec(session, toolID, msg.Input)
	case ToolSPAWN:
		s.handleSpawn(sender, toolID, msg.Input)
	case ToolKILL:
		s.handleKill(session, toolID, msg.Input)
	case ToolLIST:
		s.handleList(session, toolID)
	default:
		s.handleDynamicTool(session, toolID, toolName, msg.Input)
	}
}

func (s *ToolService) handleExec(session, toolID string, input json.RawMessage) {
	parsed, err := parseCommandToolInput(input)
	if err != nil {
		s.hub.sendToolResult(session, toolID, "ERROR: missing or invalid command")
		return
	}
	s.process.RunCommand(session, toolID, parsed.Command)
}

func (s *ToolService) handleSpawn(sender *Client, toolID string, input json.RawMessage) {
	parsed, err := parseCommandToolInput(input)
	if err != nil {
		s.hub.sendToolResult(sender.session, toolID, "ERROR: missing or invalid command")
		return
	}

	if err := s.hub.policy.CanSpawn(sender, parsed.Command, toolID, sender.session); err != nil {
		s.hub.sendToolResult(sender.session, toolID, fmt.Sprintf("ERROR: %v", err))
		return
	}

	result := s.process.Spawn(parsed.Command, sender.session, sender.depth+1)
	if result.Error != "" {
		s.hub.sendToolResult(sender.session, toolID, fmt.Sprintf("ERROR: %v", result.Error))
		return
	}

	s.hub.sendToolResult(sender.session, toolID, fmt.Sprintf("PID %d", result.PID))
	spawnHookPayload, _ := json.Marshal(map[string]any{
		"tool": string(ToolSPAWN), "id": toolID, "command": parsed.Command, "pid": result.PID,
	})
	s.hub.dispatchHook("after_spawn", spawnHookPayload, sender.session)
}

func (s *ToolService) handleKill(session, toolID string, input json.RawMessage) {
	var parsed struct {
		PID int `json:"pid"`
	}
	if err := json.Unmarshal(input, &parsed); err != nil || parsed.PID == 0 {
		s.hub.sendToolResult(session, toolID, "ERROR: missing or invalid pid")
		return
	}

	if err := s.process.Kill(parsed.PID, session); err != nil {
		s.hub.sendToolResult(session, toolID, fmt.Sprintf("ERROR: %v", err))
		return
	}
	s.hub.sendToolResult(session, toolID, "OK")
}

func (s *ToolService) handleList(session, toolID string) {
	result := s.process.List(session)
	s.hub.sendToolResult(session, toolID, result)
}

func (s *ToolService) handleDynamicTool(session, toolID, toolName string, input json.RawMessage) {
	execCmd, ok := s.hub.toolExec[toolName]
	if !ok {
		s.hub.sendToolResult(session, toolID, fmt.Sprintf("ERROR: unknown tool %s", toolName))
		return
	}
	s.process.RunSkillTool(session, toolID, toolName, execCmd, input)
}

type commandToolInput struct {
	Command string `json:"command"`
}

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
