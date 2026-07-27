package kernel

import (
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
	"github.com/bamanoz/tabula/internal/tenant"
)

const hookDispatchAuditKind = "hook.dispatch.audit"

func (h *Hub) recordHookDispatchAudit(a khooks.DispatchAudit) {
	if h == nil || a.Event == "" || a.Session == "" {
		return
	}
	store, ok := h.sessionStore.(SessionLedgerStore)
	if !ok {
		return
	}
	tenantID := a.TenantID
	if tenantID == "" {
		tenantID = h.sessionTenantID("", a.Session)
	}
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	payload := map[string]any{
		"hook":            a.Event,
		"target":          a.Target,
		"hook_id":         a.HookID,
		"reply_action":    a.ReplyAction,
		"dispatch_effect": a.DispatchEffect,
		"status":          a.Status,
		"duration_ms":     a.DurationMs,
		"timeout_ms":      a.TimeoutMs,
		"input_summary":   khooks.SummarizePayload(a.Payload),
	}
	if a.Reason != "" {
		payload["reason"] = a.Reason
	}
	_ = store.AppendLedgerEvent(a.Session, tenantID, hookDispatchAuditKind, "kernel:hook_dispatch", payload)
}
