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
	if ok {
		go s.hub.broadcastRuntimeCatalogUpdate(capability)
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
	s.hub.hooks.HandleRuntimeResult(runtimeID, &Message{
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
		return string(ActionPass), nil
	case wire.HookActionRewrite:
		return string(ActionModify), nil
	case wire.HookActionDeny:
		return string(ActionBlock), nil
	case wire.HookActionClaim:
		return string(ActionClaim), nil
	case wire.HookActionSuspend:
		return string(ActionSuspend), nil
	default:
		return "", fmt.Errorf("unknown hook_event_reply action %q", action)
	}
}

func (h *Hub) syncRuntimeCapability(runtimeID string, capability runtimeapi.Capability) {
	if h == nil {
		return
	}
	if capability.Target.Kind != wire.TargetKindPlugin {
		return
	}
	h.toolExecMu.Lock()
	defer h.toolExecMu.Unlock()
	h.removeRuntimeTargetToolsLocked(runtimeID, capability.Target, capability.Tenants)
	advertise := capability.State == wire.CapabilityStateReady || capability.State == wire.CapabilityStateManifestLoaded || capability.State == wire.CapabilityStateInitializing
	if !advertise && len(capability.Tools) == 0 {
		return
	}
	tenants := capability.Tenants
	if len(tenants) == 0 {
		tenants = []string{"*"}
	}
	if h.runtimes != nil && len(capability.Tenants) == 0 {
		tenants = h.runtimes.TenantsServed(runtimeID)
	}
	for _, tool := range capability.Tools {
		if tool.Name == "" {
			continue
		}
		for _, tenantID := range tenants {
			key := toolExecKey(tenantID, tool.Name)
			if existing, ok := h.toolExec[key]; ok && (existing.RuntimeID != runtimeID || !sameRuntimeTarget(existing.Target, capability.Target)) {
				h.Logger.Warn("runtime tool shadows existing runtime tool", "tool", tool.Name, "runtime_id", runtimeID, "target", capability.Target.ID, "tenant_id", tenantID)
			}
			dispatch := runtimeDispatch(runtimeID, tenantID, capability.Target, tool.Schema, int(tool.DeadlineMS))
			dispatch.Advertise = advertise
			h.toolExec[key] = dispatch
		}
	}
}

func (h *Hub) broadcastRuntimeCatalogUpdate(capability runtimeapi.Capability) {
	if h == nil || h.sessions == nil || capability.Target.Kind != wire.TargetKindPlugin {
		return
	}
	h.broadcastRuntimeCatalogRefreshForTenants(capability.Tenants)
}

func (h *Hub) broadcastRuntimeCatalogRefreshForTenants(tenants []string) {
	if h == nil || h.sessions == nil {
		return
	}
	sessionCount := 0
	clientCount := 0
	for _, sess := range h.sessions.All() {
		if sess == nil || sess.ID == "" || !runtimeServesTenant(tenants, sess.TenantID) {
			continue
		}
		sessionCount++
		tools := h.initToolsJSON(sess.TenantID)
		meta := h.initMetaJSON(sess.TenantID)
		for _, client := range h.sessionClients(sess.TenantID, sess.ID) {
			if client.canReceive(TopicSessionInit) {
				context, filteredTools := h.policy.BeforePromptBuild(sess.ID, sess.TenantID, client.name, sess.GetInitContext(), tools, meta)
				msg := h.initMessage(context, filteredTools, meta)
				client.SendMsg(msg)
				clientCount++
			}
		}
	}
	if sessionCount > 0 || clientCount > 0 {
		h.Logger.Info("runtime catalog refresh broadcast", "tenants", tenants, "sessions", sessionCount, "apps", clientCount)
	}
}

func capabilityVisibleToTenant(capability runtimeapi.Capability, tenantID string) bool {
	if tenantID == "" || len(capability.Tenants) == 0 {
		return true
	}
	return runtimeServesTenant(capability.Tenants, tenantID)
}

func (h *Hub) removeRuntimeTools(runtimeID string) int {
	h.toolExecMu.Lock()
	defer h.toolExecMu.Unlock()
	removed := 0
	for name, entry := range h.toolExec {
		if entry.Source == toolSourceRuntime && entry.RuntimeID == runtimeID {
			delete(h.toolExec, name)
			removed++
		}
	}
	return removed
}

func (h *Hub) removeRuntimeTargetToolsLocked(runtimeID string, target wire.Target, tenants []string) {
	for name, entry := range h.toolExec {
		if entry.Source == toolSourceRuntime && entry.RuntimeID == runtimeID && sameRuntimeTarget(entry.Target, target) && toolDispatchMatchesTenants(entry, tenants) {
			delete(h.toolExec, name)
		}
	}
}

func toolDispatchMatchesTenants(entry toolDispatch, tenants []string) bool {
	if len(tenants) == 0 {
		return true
	}
	if entry.TenantID == "" {
		return true
	}
	for _, tenantID := range tenants {
		if tenantID == "*" || tenantID == entry.TenantID {
			return true
		}
	}
	return false
}

func (h *Hub) runtimeConn(runtimeID string) runtimeapi.RuntimeConn {
	if h == nil || h.runtimes == nil {
		return nil
	}
	return h.runtimes.RuntimeConn(runtimeID)
}

func (h *Hub) markRuntimeTargetBusy(runtimeID string, target wire.Target) func() {
	if h == nil || runtimeID == "" {
		return func() {}
	}
	key := runtimeTargetBusyKey(runtimeID, target)
	h.runtimeBusyMu.Lock()
	h.runtimeBusy[key]++
	h.runtimeBusyMu.Unlock()
	return func() {
		h.runtimeBusyMu.Lock()
		defer h.runtimeBusyMu.Unlock()
		if h.runtimeBusy[key] <= 1 {
			delete(h.runtimeBusy, key)
			return
		}
		h.runtimeBusy[key]--
	}
}

func (h *Hub) tryMarkRuntimeTargetBusy(runtimeID string, target wire.Target) (func(), bool) {
	if h == nil || runtimeID == "" {
		return func() {}, true
	}
	key := runtimeTargetBusyKey(runtimeID, target)
	h.runtimeBusyMu.Lock()
	defer h.runtimeBusyMu.Unlock()
	if h.runtimeBusy[key] > 0 {
		return nil, false
	}
	h.runtimeBusy[key] = 1
	return func() {
		h.runtimeBusyMu.Lock()
		defer h.runtimeBusyMu.Unlock()
		if h.runtimeBusy[key] <= 1 {
			delete(h.runtimeBusy, key)
			return
		}
		h.runtimeBusy[key]--
	}, true
}

func (h *Hub) isRuntimeTargetBusy(runtimeID string, target wire.Target) bool {
	if h == nil || runtimeID == "" {
		return false
	}
	h.runtimeBusyMu.RLock()
	defer h.runtimeBusyMu.RUnlock()
	return h.runtimeBusy[runtimeTargetBusyKey(runtimeID, target)] > 0
}

func runtimeTargetBusyKey(runtimeID string, target wire.Target) string {
	return runtimeID + "\x00" + string(target.Kind) + "\x00" + target.ID
}

func (h *Hub) pickRuntime(tenantID string) (runtimeapi.RuntimeConn, string, wire.ErrorCode, error) {
	if h == nil || h.runtimes == nil {
		return nil, "", wire.ErrorRuntimeUnavailable, fmt.Errorf("runtime registry is unavailable")
	}
	return h.runtimes.Pick(tenantID)
}

func (h *Hub) pickRuntimeForSession(tenantID, session string) (runtimeapi.RuntimeConn, string, wire.ErrorCode, error) {
	if h == nil || h.runtimes == nil {
		return nil, "", wire.ErrorRuntimeUnavailable, fmt.Errorf("runtime registry is unavailable")
	}
	if runtimeID := h.sessionPreferredRuntime(tenantID, session); runtimeID != "" {
		if conn, code, err := h.runtimes.RuntimeForTenant(tenantID, runtimeID); err == nil && conn != nil {
			return conn, runtimeID, code, nil
		} else {
			fallbackConn, fallbackRuntimeID, fallbackCode, fallbackErr := h.runtimes.Pick(tenantID)
			h.logPreferredRuntimeFallback(tenantID, session, runtimeID, code, err, fallbackRuntimeID, fallbackCode, fallbackErr)
			return fallbackConn, fallbackRuntimeID, fallbackCode, fallbackErr
		}
	}
	return h.runtimes.Pick(tenantID)
}

func (h *Hub) runtimeForTenant(tenantID, runtimeID string) (runtimeapi.RuntimeConn, wire.ErrorCode, error) {
	if h == nil || h.runtimes == nil {
		return nil, wire.ErrorRuntimeUnavailable, fmt.Errorf("runtime registry is unavailable")
	}
	return h.runtimes.RuntimeForTenant(tenantID, runtimeID)
}

func (h *Hub) sessionPreferredRuntime(tenantID, session string) string {
	if h == nil || h.sessions == nil || session == "" {
		return ""
	}
	sess, ok := h.sessions.Get(session, tenantID)
	if !ok || sess == nil {
		return ""
	}
	return normalizeClientRuntimeID(sess.PreferredRuntime())
}

func (h *Hub) logPreferredRuntimeFallback(tenantID, session, preferredRuntimeID string, reasonCode wire.ErrorCode, reasonErr error, selectedRuntimeID string, selectedCode wire.ErrorCode, selectedErr error) {
	if h == nil || h.Logger == nil {
		return
	}
	attrs := []any{
		"tenant_id", tenantID,
		"session", session,
		"preferred_runtime_id", preferredRuntimeID,
	}
	if reasonCode != "" {
		attrs = append(attrs, "reason_code", reasonCode)
	}
	if reasonErr != nil {
		attrs = append(attrs, "reason", reasonErr.Error())
	}
	if selectedRuntimeID != "" {
		attrs = append(attrs, "selected_runtime_id", selectedRuntimeID)
	}
	if selectedCode != "" {
		attrs = append(attrs, "selected_code", selectedCode)
	}
	if selectedErr != nil {
		attrs = append(attrs, "selected_error", selectedErr.Error())
		h.Logger.Warn("preferred runtime fallback failed", attrs...)
		return
	}
	h.Logger.Info("preferred runtime fallback", attrs...)
}

// ReloadAttachedRuntime requests a Runtime API reload from one attached runtime.
// The boolean result reports whether the runtime was attached and a reload was
// attempted.
func (h *Hub) ReloadAttachedRuntime(ctx context.Context, runtimeID string, target *wire.Target, tenants ...string) (bool, error) {
	conn := h.runtimeConn(runtimeID)
	if conn == nil {
		return false, nil
	}
	_, err := conn.Reload(ctx, runtimeapi.ReloadReq{Target: target, Tenants: tenants})
	return true, err
}
