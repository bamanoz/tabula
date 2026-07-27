package kernel

import (
	"context"
	"fmt"
	"log/slog"

	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
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
	if ok {
		s.hub.broadcastRuntimeCatalogUpdate(capability)
	}
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
	s.hub.hooks.HandleRuntimeResult(runtimeID, &khooks.Message{
		Type:    string(MsgHookReply),
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
	tenantID := send.TenantID
	if tenantID == "" {
		tenantID = s.hub.sessionTenantID("", send.SessionID)
	}
	s.hub.broadcastToSession(tenantID, send.SessionID, send.Type, busMessage(send.Type, send.SessionID, send.Payload), nil)
	return nil
}

func (s hubRuntimeAsyncSink) PluginLogged(runtimeID string, log wire.PluginLog) {
	if err := log.Target.Validate(); err != nil {
		s.hub.Logger.Warn("dropping invalid runtime plugin log", "runtime_id", runtimeID, "err", err)
		return
	}
	var level slog.Level
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
		return string(khooks.ActionPass), nil
	case wire.HookActionRewrite:
		return string(khooks.ActionModify), nil
	case wire.HookActionDeny:
		return string(khooks.ActionBlock), nil
	case wire.HookActionClaim:
		return string(khooks.ActionClaim), nil
	case wire.HookActionSuspend:
		return string(khooks.ActionSuspend), nil
	default:
		return "", fmt.Errorf("unknown hook_event_reply action %q", action)
	}
}
