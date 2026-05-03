package kernel

import (
	"encoding/json"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

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

// allHookSubscribers returns the union of all WebSocket clients and any
// registered plugins that implement HookSubscriber. Used by the hook
// engine to rebuild its dispatch index whenever the subscriber set
// changes (per creative `creative-plugin-runtime.md` §2).
func (h *Hub) allHookSubscribers() []HookSubscriber {
	clients := h.clients.All()
	pluginHandles := h.pluginHandles()
	runtimeTargets := h.runtimeHookTargets()
	subs := make([]HookSubscriber, 0, len(clients)+len(pluginHandles)+len(runtimeTargets))
	for _, c := range clients {
		subs = append(subs, c)
	}
	for _, p := range pluginHandles {
		subs = append(subs, newPluginHookSubscriber(p))
	}
	for _, target := range runtimeTargets {
		subs = append(subs, newRuntimeHookSubscriber(target))
	}
	return subs
}

func (h *Hub) pluginHandles() []*plugin.Handle {
	if h.plugins == nil {
		return nil
	}
	return h.plugins.All()
}

func (h *Hub) runtimeHookTargets() []runtimeHookTarget {
	if h == nil || h.runtimes == nil {
		return nil
	}
	return h.runtimes.HookTargets()
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
