package kernel

import "encoding/json"

func (h *Hub) targetSession(sender *Client, msg *Message) string {
	if msg.Session != "" {
		return msg.Session
	}
	return sender.session
}

func (h *Hub) sessionClients(session string) []*Client {
	return h.clients.InSession(session)
}

func (h *Hub) allClients() []*Client {
	return h.clients.All()
}

func (h *Hub) sessionProcesses(session string) []*SpawnedProcess {
	processes := make([]*SpawnedProcess, 0)
	h.processes.ForEach(func(_ int, proc *SpawnedProcess) {
		if proc == nil {
			return
		}
		if session == "" || proc.Session == session {
			processes = append(processes, proc)
		}
	})
	return processes
}

func (h *Hub) emitAfterMessage(session string, sender *Client) {
	payload, _ := json.Marshal(map[string]string{
		"session": session,
		"sender":  sender.name,
		"type":    "done",
	})
	h.dispatchHook("after_message", payload, session)
}

func (h *Hub) emitAfterToolCall(session, toolID string, payload map[string]string) {
	hookPayload, _ := json.Marshal(payload)
	h.dispatchHook("after_tool_call", hookPayload, session)
}

func (h *Hub) emitSessionEnd(session string) {
	payload, _ := json.Marshal(map[string]string{"session": session})
	h.dispatchHook("session_end", payload, session)
}
