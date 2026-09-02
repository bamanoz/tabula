package kernel

import (
	"encoding/json"
	"fmt"
)

// broadcastToSession sends a message to all clients in a session that can receive the given type.
// Also delivers to clients with receives_global for that type (regardless of session).
func (h *Hub) broadcastToSession(tenantID, session, msgType string, msg *BusMessage, exclude *Client) int {
	return h.broadcastToSessionFrom(tenantID, session, msgType, msg, nil, exclude)
}

func (h *Hub) broadcastToSessionFrom(tenantID, session, msgType string, msg *BusMessage, sender *Client, exclude *Client) int {
	delivered := 0
	seen := make(map[*Client]bool)
	if tenantID == "" && sender != nil {
		tenantID = sender.tenantID
	}

	// Session members
	for _, c := range h.sessionClients(tenantID, session) {
		if c == exclude {
			continue
		}
		if !c.IsConnected() {
			continue
		}
		if !c.canReceive(msgType) {
			continue
		}
		if !c.queueMsg(h.prepareRoutedMessage(sender, session, "session", msg)) {
			continue
		}
		seen[c] = true
		delivered++
	}

	// Global receivers (clients that want this msgType from all sessions)
	for _, c := range h.allClients() {
		if c == exclude || seen[c] {
			continue
		}
		if !c.IsConnected() {
			continue
		}
		if !c.canReceiveGlobal(msgType) {
			continue
		}
		globalMsg := h.prepareRoutedMessage(sender, session, "global", msg)
		if globalMsg != nil {
			if !c.queueMsg(globalMsg) {
				continue
			}
		} else if !c.queueMsg(msg) {
			continue
		}
		delivered++
	}

	h.Logger.Debug("broadcast", "type", msgType, "session", session, "delivered", delivered)
	return delivered
}

func (h *Hub) sendToolResultForTool(tenantID, session, toolID, toolName, output string, artifact json.RawMessage, truncated bool, correlations ...toolAttemptContext) bool {
	correlation := toolCorrelation(correlations)
	if err := h.validateToolAttempt(tenantID, session, correlation); err != nil {
		h.Logger.Warn("stale tool result rejected", "tenant_id", tenantID, "session", session, "tool", toolName, "tool_call_id", toolID, "turn_id", correlation.TurnID, "attempt_id", correlation.AttemptID, "driver_generation", correlation.DriverGeneration, "err", err)
		return false
	}
	if !h.claimToolTerminal(tenantID, session, toolID, correlation) {
		return false
	}
	v4Delivered := h.tools.sendV4ToolResult(tenantID, session, toolID, toolName, output, artifact, truncated, correlation)
	runtimeDelivered := h.tools.sendRuntimeToolResult(tenantID, session, toolID, toolName, output, artifact, truncated, correlation)
	h.recordClaimedToolTerminal(tenantID, session, toolID, toolName, "completed", correlation)
	delivered := h.broadcastToSession(tenantID, session, TopicToolResult, &BusMessage{
		Type:      string(MsgReply),
		Topic:     TopicToolResult,
		ID:        toolID,
		Name:      toolName,
		Output:    output,
		Artifact:  artifact,
		Truncated: truncated,
		Meta:      correlation.meta(),
	}, nil)
	h.Logger.Info("tool result broadcast", "tenant_id", tenantID, "session", session, "tool", toolName, "tool_call_id", toolID, "delivered", delivered, "v4_delivered", v4Delivered, "runtime_delivered", runtimeDelivered, "output_bytes", len(output), "artifact_bytes", len(artifact), "truncated", truncated)
	return true
}

// broadcastProcessError sends an error message about a crashed process.
func (h *Hub) broadcastProcessError(session string, pid int, command string, exitCode int) {
	h.Logger.Error("process crashed", "pid", pid, "exit_code", exitCode, "command", command)
	if session == "" {
		return
	}
	tenantID := h.sessionTenantID("", session)
	h.broadcastToSession(tenantID, session, string(MsgError), &BusMessage{
		Type: string(MsgError),
		Text: fmt.Sprintf("process %d crashed (exit %d)", pid, exitCode),
	}, nil)
}
