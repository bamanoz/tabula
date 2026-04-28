package kernel

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

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
	if err := h.validatePluginHandleCatalog(handle); err != nil {
		h.Logger.Warn("plugin registration rejected", "plugin", handle.ID(), "err", err)
		handle.Close()
		return
	}
	if h.plugins == nil {
		h.plugins = plugin.NewRegistry()
	}
	if prior := h.plugins.Add(handle); prior != nil {
		h.removePluginToolsLocked(prior)
		prior.Close()
	}
	h.replacePluginTools(handle)
	h.rebuildHookIndex()
}

func (h *Hub) replacePluginTools(handle *plugin.Handle) {
	if handle == nil {
		return
	}
	h.toolExecMu.Lock()
	defer h.toolExecMu.Unlock()
	h.removePluginToolsLocked(handle)
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
	h.toolExecMu.Lock()
	defer h.toolExecMu.Unlock()
	h.removePluginToolsLocked(handle)
}

func (h *Hub) removePluginToolsLocked(handle *plugin.Handle) {
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
		if err := validatePluginToolResult(params); err != nil {
			h.Logger.Warn("invalid plugin tool_result", "plugin", handle.ID(), "err", err)
			if params.CallID != "" {
				handle.CancelPending(params.CallID)
			}
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
		if _, err := plugin.NormalizeUpdateToolsParams(&params); err != nil {
			h.Logger.Warn("invalid plugin update_tools", "plugin", handle.ID(), "err", err)
			return
		}
		if err := handle.ApplyUpdateTools(&params); err != nil {
			h.Logger.Warn("invalid plugin update_tools", "plugin", handle.ID(), "err", err)
			return
		}
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
	if params.CallID == "" {
		h.Logger.Warn("invalid plugin event_reply", "plugin", handle.ID(), "err", "callId is required")
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
		handle.CancelPending(params.CallID)
		return
	}
	if !handle.DeliverResult(params.CallID, msg) {
		h.Logger.Warn("unsolicited plugin event_reply", "plugin", handle.ID(), "callId", params.CallID)
		return
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

func (h *Hub) validatePluginRegisterParams(reg *plugin.RegisterParams) error {
	if reg == nil {
		return fmt.Errorf("register params are required")
	}
	if _, err := plugin.NormalizeRegisterParams(reg); err != nil {
		return err
	}
	for i, sub := range reg.Subscriptions {
		event := strings.TrimSpace(sub.Event)
		if _, ok := HookEvents[event]; !ok {
			return fmt.Errorf("subscriptions[%d].event %q is not supported", i, event)
		}
	}
	return nil
}

func (h *Hub) validatePluginHandleCatalog(handle *plugin.Handle) error {
	return h.validatePluginRegisterParams(&plugin.RegisterParams{
		PluginID:      handle.ID(),
		Tools:         handle.Tools(),
		Subscriptions: handle.Subscriptions(),
	})
}

func validatePluginToolResult(params plugin.ToolResultParams) error {
	if params.CallID == "" {
		return fmt.Errorf("callId is required")
	}
	hasResult := len(params.Result) > 0
	hasError := params.Error != ""
	if hasResult == hasError {
		return fmt.Errorf("exactly one of result or error is required")
	}
	return nil
}
