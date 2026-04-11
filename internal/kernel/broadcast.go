package kernel

import (
	"encoding/json"
	"fmt"
)

// broadcastToSession sends a message to all clients in a session that can receive the given type.
// Caller must hold h.mu.
func (h *Hub) broadcastToSession(session, msgType string, msg *Message, exclude *Client) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	delivered := 0
	for c := range h.clients {
		if c == exclude {
			continue
		}
		if !c.connected || c.session == "" {
			continue
		}
		if c.session != session {
			continue
		}
		if !c.canReceive(msgType) {
			continue
		}
		c.SendRaw(data)
		delivered++
	}
	h.Logger.Debug("broadcast", "type", msgType, "session", session, "delivered", delivered)
}

// broadcastToSessionRaw sends raw JSON bytes to a session.
// Caller must hold h.mu.
func (h *Hub) broadcastToSessionRaw(session, msgType string, data []byte) {
	delivered := 0
	for c := range h.clients {
		if !c.connected || c.session == "" {
			continue
		}
		if c.session != session {
			continue
		}
		if !c.canReceive(msgType) {
			continue
		}
		c.SendRaw(data)
		delivered++
	}
	h.Logger.Debug("broadcast", "type", msgType, "session", session, "delivered", delivered)
}

// sendToolResult sends a tool_result to a session.
// Caller must hold h.mu.
func (h *Hub) sendToolResult(session, toolID, output string) {
	msg := &Message{
		Type:   "tool_result",
		ID:     toolID,
		Output: output,
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.broadcastToSessionRaw(session, "tool_result", data)
}

// broadcastProcessError sends an error message about a crashed process.
// Caller must hold h.mu.
func (h *Hub) broadcastProcessError(session string, pid int, command string, exitCode int) {
	h.Logger.Error("process crashed", "pid", pid, "exit_code", exitCode, "command", command)
	if session == "" {
		return
	}
	errMsg := &Message{
		Type: "error",
		Text: fmt.Sprintf("process %d crashed (exit %d)", pid, exitCode),
	}
	data, err := json.Marshal(errMsg)
	if err != nil {
		return
	}
	h.broadcastToSessionRaw(session, "error", data)
}
