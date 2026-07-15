package kernel

import (
	"encoding/json"
	"fmt"
)

// broadcastToSession sends a message to all clients in a session that can receive the given type.
// Also delivers to clients with receives_global for that type (regardless of session).
func (h *Hub) broadcastToSession(tenantID, session, msgType string, msg *Message, exclude *Client) int {
	return h.broadcastToSessionFrom(tenantID, session, msgType, msg, nil, exclude)
}

func (h *Hub) broadcastToSessionFrom(tenantID, session, msgType string, msg *Message, sender *Client, exclude *Client) int {
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

func (h *Hub) broadcastToTurnReceivers(tenantID, session string, msg *Message, sender *Client, exclude *Client) int {
	receiver := h.pickTurnReceiver(tenantID, session, sender, exclude)
	if receiver == nil {
		h.Logger.Debug("broadcast turn input", "session", session, "delivered", 0)
		return 0
	}
	if !receiver.queueMsg(h.prepareRoutedMessage(sender, session, "session", msg)) {
		h.Logger.Debug("broadcast turn input", "session", session, "delivered", 0, "receiver", receiver.name)
		return 0
	}
	h.Logger.Debug("broadcast turn input", "session", session, "delivered", 1, "receiver", receiver.name)
	return 1
}

func (h *Hub) pickTurnReceiver(tenantID, session string, sender *Client, exclude *Client) *Client {
	if tenantID == "" && sender != nil {
		tenantID = sender.tenantID
	}
	var receiver *Client
	count := 0
	for _, c := range h.sessionClients(tenantID, session) {
		if c == exclude || !c.IsConnected() {
			continue
		}
		if !c.canReceive(TopicMessageUser) || !c.canSend(TopicTurnDone) {
			continue
		}
		count++
		receiver = c
	}
	if count > 1 {
		h.Logger.Warn("ambiguous turn receivers", "tenant", tenantID, "session", session, "count", count)
		return nil
	}
	return receiver
}

func (h *Hub) mirrorToNonTurnReceivers(tenantID, session string, msg *Message, sender *Client, exclude *Client) int {
	delivered := 0
	seen := make(map[*Client]bool)
	if tenantID == "" && sender != nil {
		tenantID = sender.tenantID
	}
	for _, c := range h.sessionClients(tenantID, session) {
		if c == exclude || !c.IsConnected() {
			continue
		}
		if !c.canReceive(TopicMessageUser) || c.canSend(TopicTurnDone) {
			continue
		}
		if !c.queueMsg(h.prepareRoutedMessage(sender, session, "session", msg)) {
			continue
		}
		seen[c] = true
		delivered++
	}
	for _, c := range h.allClients() {
		if c == exclude || seen[c] || !c.IsConnected() {
			continue
		}
		if c.canSend(TopicTurnDone) || !c.canReceiveGlobal(TopicMessageUser) {
			continue
		}
		if !c.queueMsg(h.prepareRoutedMessage(sender, session, "global", msg)) {
			continue
		}
		delivered++
	}
	h.Logger.Debug("mirror managed input", "session", session, "delivered", delivered)
	return delivered
}

func (h *Hub) sendToolResultForTool(tenantID, session, toolID, toolName, output string, artifact json.RawMessage, truncated bool) {
	h.recordToolTerminal(tenantID, session, toolID, toolName, "completed")
	delivered := h.broadcastToSession(tenantID, session, TopicToolResult, &Message{
		Type:      string(MsgReply),
		Topic:     TopicToolResult,
		ID:        toolID,
		Name:      toolName,
		Output:    output,
		Artifact:  artifact,
		Truncated: truncated,
	}, nil)
	h.Logger.Info("tool result broadcast", "tenant_id", tenantID, "session", session, "tool", toolName, "tool_call_id", toolID, "delivered", delivered, "output_bytes", len(output), "artifact_bytes", len(artifact), "truncated", truncated)
}

// broadcastProcessError sends an error message about a crashed process.
func (h *Hub) broadcastProcessError(session string, pid int, command string, exitCode int) {
	h.Logger.Error("process crashed", "pid", pid, "exit_code", exitCode, "command", command)
	if session == "" {
		return
	}
	tenantID := h.sessionTenantID("", session)
	h.broadcastToSession(tenantID, session, string(MsgError), &Message{
		Type: string(MsgError),
		Text: fmt.Sprintf("process %d crashed (exit %d)", pid, exitCode),
	}, nil)
	if queued, ok := h.completeSessionTurn(tenantID, session); ok {
		h.dispatchQueuedInput(tenantID, session, queued)
	}
}
