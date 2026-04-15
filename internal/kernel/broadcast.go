package kernel

import (
	"encoding/json"
	"fmt"
)

// broadcastToSession sends a message to all clients in a session that can receive the given type.
func (h *Hub) broadcastToSession(session, msgType string, msg *Message, exclude *Client) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	delivered := 0
	for _, c := range h.sessionClients(session) {
		if c == exclude {
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
func (h *Hub) broadcastToSessionRaw(session, msgType string, data []byte) {
	delivered := 0
	for _, c := range h.sessionClients(session) {
		if !c.canReceive(msgType) {
			continue
		}
		c.SendRaw(data)
		delivered++
	}
	h.Logger.Debug("broadcast", "type", msgType, "session", session, "delivered", delivered)
}

// sendToolResult sends a tool_result to a session.
func (h *Hub) sendToolResult(session, toolID, output string) {
	h.broadcastJSONToSession(session, string(MsgToolResult), &Message{
		Type:   string(MsgToolResult),
		ID:     toolID,
		Output: output,
	})
}

func (h *Hub) broadcastJSONToSession(session, msgType string, msg *Message) {
	data, err := json.Marshal(msg)
	if err != nil {
		return
	}
	h.broadcastToSessionRaw(session, msgType, data)
}

// broadcastProcessError sends an error message about a crashed process.
func (h *Hub) broadcastProcessError(session string, pid int, command string, exitCode int) {
	h.Logger.Error("process crashed", "pid", pid, "exit_code", exitCode, "command", command)
	if session == "" {
		return
	}
	errMsg := &Message{
		Type: string(MsgError),
		Text: fmt.Sprintf("process %d crashed (exit %d)", pid, exitCode),
	}
	data, err := json.Marshal(errMsg)
	if err != nil {
		return
	}
	h.broadcastToSessionRaw(session, string(MsgError), data)
}
