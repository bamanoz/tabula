package kernel

import (
	"fmt"

	"github.com/bamanoz/tabula/internal/kernel/clientmeta"
	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func (h *Hub) runtimeConn(runtimeID string) runtimeapi.RuntimeConn {
	if h == nil || h.runtimes == nil {
		return nil
	}
	return h.runtimes.RuntimeConn(runtimeID)
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
	return clientmeta.NormalizeRuntimeID(sess.PreferredRuntime())
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
