package kernel

import (
	"encoding/json"
	"fmt"
)

// broadcastToSession sends a message to all clients in a session that can receive the given type.
// Also delivers to clients with receives_global for that type (regardless of session).
func (h *Hub) broadcastToSession(session, msgType string, msg *Message, exclude *Client) {
	delivered := 0
	seen := make(map[*Client]bool)

	// Session members
	for _, c := range h.sessionClients(session) {
		if c == exclude {
			continue
		}
		if !c.canReceive(msgType) {
			continue
		}
		c.SendMsg(msg)
		seen[c] = true
		delivered++
	}

	// Global receivers (clients that want this msgType from all sessions)
	for _, c := range h.allClients() {
		if c == exclude || seen[c] {
			continue
		}
		if !c.canReceiveGlobal(msgType) {
			continue
		}
		globalMsg := cloneMessage(msg)
		if globalMsg != nil {
			if globalMsg.Session == "" {
				globalMsg.Session = session
			}
			if globalMsg.TenantID == "" {
				if sess, ok := h.sessions.Get(session); ok {
					globalMsg.TenantID = sess.TenantID
				}
			}
			if globalMsg.Meta == nil {
				globalMsg.Meta = json.RawMessage([]byte("{}"))
			}
			c.SendMsg(globalMsg)
		} else {
			c.SendMsg(msg)
		}
		delivered++
	}

	h.Logger.Debug("broadcast", "type", msgType, "session", session, "delivered", delivered)
}

func (h *Hub) sendToolResultForTool(session, toolID, toolName, output string) {
	h.broadcastToSession(session, string(MsgToolResult), &Message{
		Type:   string(MsgToolResult),
		ID:     toolID,
		Name:   toolName,
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
	if queued, ok := h.completeSessionTurn(session); ok {
		h.dispatchQueuedInput(session, queued)
	}
}
