package kernel

import (
	"context"
	"log/slog"

	"github.com/bamanoz/tabula/internal/kernel/plugin"
)

// registerPluginHandle installs an already-started plugin handle into the Hub.
// The full Hub.RegisterPlugin builder will call this after runtime spawn and
// register handshake complete; tests can use it directly to validate the
// dispatch table and hook subscriber merge point before runtime.go lands.
func (h *Hub) registerPluginHandle(handle *plugin.Handle) {
	if handle == nil {
		return
	}
	if h.plugins == nil {
		h.plugins = plugin.NewRegistry()
	}
	if prior := h.plugins.Add(handle); prior != nil {
		h.removePluginTools(prior)
		prior.Close()
	}
	h.replacePluginTools(handle)
	h.rebuildHookIndex()
}

func (h *Hub) replacePluginTools(handle *plugin.Handle) {
	if handle == nil {
		return
	}
	h.removePluginTools(handle)
	for _, tool := range handle.Tools() {
		if tool.Name == "" {
			continue
		}
		if existing, ok := h.toolExec[tool.Name]; ok && existing.Source != toolSourcePlugin {
			h.Logger.Warn("plugin tool shadows existing skill tool", "tool", tool.Name, "plugin", handle.ID())
		} else if ok && existing.Plugin != handle {
			h.Logger.Warn("plugin tool shadows existing plugin tool", "tool", tool.Name, "plugin", handle.ID())
		}
		h.toolExec[tool.Name] = toolDispatch{
			Source:     toolSourcePlugin,
			Plugin:     handle,
			Schema:     tool.Schema,
			DeadlineMs: tool.DeadlineMs,
		}
	}
}

func (h *Hub) removePluginTools(handle *plugin.Handle) {
	if handle == nil {
		return
	}
	for name, entry := range h.toolExec {
		if entry.Source == toolSourcePlugin && entry.Plugin == handle {
			delete(h.toolExec, name)
		}
	}
}

// handlePluginProtocolMessage applies inbound plugin protocol messages that
// mutate kernel-side state or complete pending calls. The runtime stdout loop
// will use this as its dispatch target after runtime.go lands.
func (h *Hub) handlePluginProtocolMessage(handle *plugin.Handle, msg *plugin.Message) {
	if handle == nil || msg == nil {
		return
	}
	switch msg.Method {
	case plugin.MethodToolResult:
		var params plugin.ToolResultParams
		if err := msg.DecodeParams(&params); err != nil {
			h.Logger.Warn("invalid plugin tool_result", "plugin", handle.ID(), "err", err)
			return
		}
		if !handle.DeliverResult(params.CallID, msg) {
			h.Logger.Warn("unsolicited plugin tool_result", "plugin", handle.ID(), "callId", params.CallID)
		}
	case plugin.MethodEventReply:
		h.handlePluginEventReply(handle, msg)
	case plugin.MethodUpdateTools:
		var params plugin.UpdateToolsParams
		if err := msg.DecodeParams(&params); err != nil {
			h.Logger.Warn("invalid plugin update_tools", "plugin", handle.ID(), "err", err)
			return
		}
		handle.ApplyUpdateTools(&params)
		h.replacePluginTools(handle)
	case plugin.MethodLog:
		h.logPluginMessage(handle, msg)
	case plugin.MethodSend:
		h.handlePluginSend(handle, msg)
	default:
		h.Logger.Warn("unknown plugin protocol method", "plugin", handle.ID(), "method", msg.Method)
	}
}

func (h *Hub) handlePluginSend(handle *plugin.Handle, msg *plugin.Message) {
	var params plugin.SendParams
	if err := msg.DecodeParams(&params); err != nil {
		h.Logger.Warn("invalid plugin send", "plugin", handle.ID(), "err", err)
		return
	}
	if params.Channel != "bus" {
		h.Logger.Warn("plugin send with unknown channel", "plugin", handle.ID(), "channel", params.Channel)
		return
	}
	if params.Type == "" {
		h.Logger.Warn("plugin send missing type", "plugin", handle.ID())
		return
	}

	h.broadcastToSession(params.Session, params.Type, &Message{
		Type:    params.Type,
		Session: params.Session,
		Payload: params.Payload,
	}, nil)
}

func (h *Hub) handlePluginEventReply(handle *plugin.Handle, msg *plugin.Message) {
	var params plugin.EventReplyParams
	if err := msg.DecodeParams(&params); err != nil {
		h.Logger.Warn("invalid plugin event_reply", "plugin", handle.ID(), "err", err)
		return
	}
	if !handle.DeliverResult(params.CallID, msg) {
		h.Logger.Warn("unsolicited plugin event_reply", "plugin", handle.ID(), "callId", params.CallID)
		return
	}
	action := string(ActionPass)
	switch params.Action {
	case plugin.ActionOK:
		action = string(ActionPass)
	case plugin.ActionRewrite:
		action = string(ActionModify)
	case plugin.ActionDeny:
		action = string(ActionBlock)
	case plugin.ActionClaim:
		action = string(ActionClaim)
	default:
		h.Logger.Warn("unknown plugin event_reply action", "plugin", handle.ID(), "action", params.Action)
	}
	h.hooks.HandleResult(&Message{
		Type:    string(MsgHookResult),
		ID:      params.CallID,
		Action:  action,
		Payload: params.Data,
		Reason:  params.Reason,
	})
}

func (h *Hub) logPluginMessage(handle *plugin.Handle, msg *plugin.Message) {
	var params plugin.LogParams
	if err := msg.DecodeParams(&params); err != nil {
		h.Logger.Warn("invalid plugin log", "plugin", handle.ID(), "err", err)
		return
	}
	level := slog.LevelInfo
	switch params.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	h.Logger.LogAttrs(context.Background(), level, params.Msg, slog.String("plugin", handle.ID()))
}
