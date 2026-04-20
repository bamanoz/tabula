package kernel

import (
	"fmt"
)

// broadcastToSession sends a message to all clients in a session that can receive the given type.
func (h *Hub) broadcastToSession(session, msgType string, msg *Message, exclude *Client) {
	delivered := 0
	for _, c := range h.sessionClients(session) {
		if c == exclude {
			continue
		}
		if !c.canReceive(msgType) {
			continue
		}
		c.SendMsg(msg)
		delivered++
	}
	h.Logger.Debug("broadcast", "type", msgType, "session", session, "delivered", delivered)
}

// sendToolResult sends a tool_result to a session.
func (h *Hub) sendToolResult(session, toolID, output string) {
	h.broadcastToSession(session, string(MsgToolResult), &Message{
		Type:   string(MsgToolResult),
		ID:     toolID,
		Output: output,
	}, nil)
}

// broadcastProcessError sends an error message about a crashed process.
func (h *Hub) broadcastProcessError(session string, pid int, command string, exitCode int) {
	h.Logger.Error("process crashed", "pid", pid, "exit_code", exitCode, "command", command)
	if session == "" {
		return
	}
	h.broadcastToSession(session, string(MsgError), &Message{
		Type: string(MsgError),
		Text: fmt.Sprintf("process %d crashed (exit %d)", pid, exitCode),
	}, nil)
	h.completeSessionTurn(session)
}
