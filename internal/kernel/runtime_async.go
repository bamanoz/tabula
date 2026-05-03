package kernel

import (
	"context"
	"fmt"
	"log/slog"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

type hubRuntimeAsyncSink struct {
	hub *Hub
}

func (h *Hub) runtimeAsyncSink() runtimeapi.AsyncSink {
	return hubRuntimeAsyncSink{hub: h}
}

func (s hubRuntimeAsyncSink) CatalogUpdated(runtimeID string, update wire.CatalogUpdate) error {
	if s.hub == nil || s.hub.runtimes == nil {
		return fmt.Errorf("kernel runtime sink is unavailable")
	}
	capability, ok, err := s.hub.runtimes.ApplyCatalogUpdate(runtimeID, update)
	if err != nil {
		return err
	}
	if ok {
		s.hub.syncRuntimeCapability(runtimeID, capability)
	}
	s.hub.rebuildHookIndex()
	return nil
}

func (s hubRuntimeAsyncSink) HookEventReplied(runtimeID string, reply wire.HookEventReply) error {
	if reply.CallID == "" {
		return fmt.Errorf("hook_event_reply call_id is required")
	}
	action, err := kernelHookAction(reply.Action)
	if err != nil {
		return err
	}
	s.hub.hooks.HandleResult(&Message{
		Type:    string(MsgHookResult),
		ID:      reply.CallID,
		Action:  action,
		Payload: reply.Data,
		Reason:  reply.Reason,
	})
	return nil
}

func (s hubRuntimeAsyncSink) PluginSent(runtimeID string, send wire.PluginSend) error {
	if err := send.Target.Validate(); err != nil {
		return err
	}
	if send.Channel != "bus" {
		return fmt.Errorf("plugin_send channel %q is not supported", send.Channel)
	}
	if send.Type == "" {
		return fmt.Errorf("plugin_send type is required")
	}
	s.hub.broadcastToSession(send.SessionID, send.Type, &Message{
		Type:    send.Type,
		Session: send.SessionID,
		Payload: send.Payload,
	}, nil)
	return nil
}

func (s hubRuntimeAsyncSink) PluginLogged(runtimeID string, log wire.PluginLog) {
	if err := log.Target.Validate(); err != nil {
		s.hub.Logger.Warn("dropping invalid runtime plugin log", "runtime_id", runtimeID, "err", err)
		return
	}
	level := slog.LevelInfo
	switch log.Level {
	case "debug":
		level = slog.LevelDebug
	case "info", "":
		level = slog.LevelInfo
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		s.hub.Logger.Warn("dropping runtime plugin log with unknown level", "runtime_id", runtimeID, "target", log.Target.ID, "level", log.Level)
		return
	}
	attrs := []slog.Attr{
		slog.String("runtime_id", runtimeID),
		slog.String("target_kind", string(log.Target.Kind)),
		slog.String("target", log.Target.ID),
	}
	if len(log.Fields) > 0 {
		attrs = append(attrs, slog.Bool("fields_redacted", true))
	}
	s.hub.Logger.LogAttrs(context.Background(), level, log.Message, attrs...)
	_ = runtimeID
}

func (s hubRuntimeAsyncSink) LifecycleNoticed(runtimeID string, notice wire.LifecycleNotice) error {
	if s.hub == nil || s.hub.runtimes == nil {
		return fmt.Errorf("kernel runtime sink is unavailable")
	}
	capability, ok, err := s.hub.runtimes.ApplyLifecycleNotice(runtimeID, notice)
	if err != nil {
		return err
	}
	if ok {
		s.hub.syncRuntimeCapability(runtimeID, capability)
		s.hub.rebuildHookIndex()
	}
	return nil
}

func (s hubRuntimeAsyncSink) RuntimeProtocolError(runtimeID string, err error) {
	if s.hub == nil || s.hub.Logger == nil {
		return
	}
	s.hub.Logger.Warn("runtime protocol error", "runtime_id", runtimeID, "err", err)
}

func kernelHookAction(action wire.HookAction) (string, error) {
	switch action {
	case wire.HookActionOK:
		return string(ActionPass), nil
	case wire.HookActionRewrite:
		return string(ActionModify), nil
	case wire.HookActionDeny:
		return string(ActionBlock), nil
	case wire.HookActionClaim:
		return string(ActionClaim), nil
	default:
		return "", fmt.Errorf("unknown hook_event_reply action %q", action)
	}
}

func (h *Hub) syncRuntimeCapability(runtimeID string, capability runtimeapi.Capability) {
	if h == nil {
		return
	}
	h.toolExecMu.Lock()
	defer h.toolExecMu.Unlock()
	h.removeRuntimeTargetToolsLocked(runtimeID, capability.Target)
	if capability.State != wire.CapabilityStateReady {
		return
	}
	for _, tool := range capability.Tools {
		if tool.Name == "" {
			continue
		}
		if existing, ok := h.toolExec[tool.Name]; ok && existing.Source != toolSourceRuntime {
			h.Logger.Warn("runtime tool shadows existing non-runtime tool", "tool", tool.Name, "runtime_id", runtimeID, "target", capability.Target.ID)
		} else if ok && (existing.RuntimeID != runtimeID || !sameRuntimeTarget(existing.Target, capability.Target)) {
			h.Logger.Warn("runtime tool shadows existing runtime tool", "tool", tool.Name, "runtime_id", runtimeID, "target", capability.Target.ID)
		}
		h.toolExec[tool.Name] = runtimeDispatch(runtimeID, capability.Target, tool.Schema, int(tool.DeadlineMS))
	}
}

func (h *Hub) removeRuntimeTools(runtimeID string) {
	h.toolExecMu.Lock()
	defer h.toolExecMu.Unlock()
	for name, entry := range h.toolExec {
		if entry.Source == toolSourceRuntime && entry.RuntimeID == runtimeID {
			delete(h.toolExec, name)
		}
	}
}

func (h *Hub) removeRuntimeTargetToolsLocked(runtimeID string, target wire.Target) {
	for name, entry := range h.toolExec {
		if entry.Source == toolSourceRuntime && entry.RuntimeID == runtimeID && sameRuntimeTarget(entry.Target, target) {
			delete(h.toolExec, name)
		}
	}
}

func (h *Hub) runtimeConn(runtimeID string) runtimeapi.RuntimeConn {
	if h == nil || h.runtimes == nil {
		return nil
	}
	return h.runtimes.RuntimeConn(runtimeID)
}

// ReloadAttachedRuntime requests a Runtime API reload from one attached runtime.
// The boolean result reports whether the runtime was attached and a reload was
// attempted.
func (h *Hub) ReloadAttachedRuntime(ctx context.Context, runtimeID string, target *wire.Target) (bool, error) {
	conn := h.runtimeConn(runtimeID)
	if conn == nil {
		return false, nil
	}
	_, err := conn.Reload(ctx, runtimeapi.ReloadReq{Target: target})
	return true, err
}
