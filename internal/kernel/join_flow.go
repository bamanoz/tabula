package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	runtimeconn "github.com/bamanoz/tabula/internal/runtime/conn"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	"github.com/bamanoz/tabula/internal/tenant"
)

// joinPlan holds the complete result of a join computation.
// Built in the build phase, applied in the side-effect phase.
type joinPlan struct {
	session       string
	tenantID      string
	clientMeta    json.RawMessage
	context       string
	joined        *Message
	memberJoined  *Message
	blockedReason string
}

// buildJoinPlan resolves session_start policy and prepares protocol-visible join messages.
func (h *Hub) buildJoinPlan(c *Client, session, tenantID string) joinPlan {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	plan := joinPlan{
		session:    session,
		tenantID:   tenantID,
		clientMeta: append(json.RawMessage(nil), c.meta...),
		joined: &Message{
			Type:     string(MsgJoined),
			Session:  session,
			TenantID: tenantID,
		},
		memberJoined: &Message{
			Type:    string(MsgEvent),
			Topic:   TopicSessionMemberJoined,
			Name:    c.name,
			Session: session,
		},
	}

	h.finalizeJoinPlan(c, &plan)
	return plan
}

func (h *Hub) applyJoinPlan(c *Client, plan joinPlan) {
	if plan.blockedReason != "" {
		c.SendMsg(&Message{
			Type: string(MsgError),
			Text: plan.blockedReason,
		})
		return
	}

	h.leaveCurrentSession(c, plan.tenantID, plan.session)
	h.persistSessionState(plan.tenantID, plan.session)
	h.assignClientSession(c, plan.tenantID, plan.session)
	c.MarkJoined()
	h.reconcileInterruptedTools(plan.tenantID, plan.session)

	c.SendMsg(plan.joined)
	h.Logger.Info("client joined session", "name", c.name, "session", plan.session)

	h.broadcastToSession(plan.tenantID, plan.session, TopicSessionMemberJoined, plan.memberJoined, c)

	if init := h.buildInitAfterJoin(c, plan); init != nil {
		c.SendMsg(init)
		h.Logger.Debug("sent init", "client", c.name)
	}
	h.resendPendingSuspendedExchanges(c, plan.tenantID, plan.session)

	h.policy.SessionJoin(plan.session, plan.tenantID, c.name, plan.clientMeta)
	if c.canReceive(TopicMessageUser) && c.canSend(TopicTurnDone) {
		if queued, ok := h.beginQueuedInput(plan.tenantID, plan.session); ok {
			h.dispatchQueuedInput(plan.tenantID, plan.session, queued)
		}
	}
}

func (h *Hub) leaveCurrentSession(c *Client, nextTenantID, nextSession string) {
	if h.sessions == nil || c.session == "" || (c.session == nextSession && c.tenantID == nextTenantID) {
		return
	}
	sess, ok := h.sessions.Get(c.session, c.tenantID)
	if !ok {
		return
	}
	h.removeSuspendedExchangeResponder(c)
	sess.RemoveClient(c.name)
	if sess.ClientCount() == 0 {
		h.deleteSessionState(c.session, sess.TenantID)
		h.emitSessionEnd(sess.TenantID, c.session)
		h.sessions.Remove(c.session, sess.TenantID)
		return
	}
	h.persistSessionState(sess.TenantID, c.session)
}

func (h *Hub) finalizeJoinPlan(c *Client, plan *joinPlan) {
	if h.tenants != nil {
		_, ok, err := h.tenants.Get(plan.tenantID)
		if err != nil {
			plan.blockedReason = err.Error()
			return
		}
		if !ok {
			plan.blockedReason = "tenant_unknown"
			return
		}
	}
	if h.sessions != nil {
		sess, created := h.sessions.GetOrCreateStatus(plan.session, plan.tenantID)
		if created {
			h.observePersistedSessionRestart(sess)
		}
		sess.AddClient(c.name)
		sess.BindPreferredRuntime(decodeClientMeta(c.meta).RuntimeID)
		if created {
			context, blocked := h.policy.StartSession(plan.session, plan.tenantID, c.name)
			if blocked {
				sess.RemoveClient(c.name)
				h.sessions.Remove(plan.session, plan.tenantID)
				plan.blockedReason = "session blocked"
				return
			}
			sess.SetInitContext(context)
			plan.context = context
		} else {
			plan.context = sess.GetInitContext()
		}
	} else {
		context, blocked := h.policy.StartSession(plan.session, plan.tenantID, c.name)
		if blocked {
			plan.blockedReason = "session blocked"
			return
		}
		plan.context = context
	}

}

func (h *Hub) buildInitAfterJoin(c *Client, plan joinPlan) *Message {
	if !c.canReceive(TopicSessionInit) {
		return nil
	}
	tools := h.initToolsJSON(plan.tenantID)
	meta := h.initMetaJSON(plan.tenantID)
	context, tools := h.policy.BeforePromptBuild(plan.session, plan.tenantID, c.name, plan.context, tools, meta)
	return h.initMessage(context, tools, meta)
}

func (h *Hub) initMessage(context string, tools json.RawMessage, meta json.RawMessage) *Message {
	msg := &Message{
		Type:    string(MsgEvent),
		Topic:   TopicSessionInit,
		Context: context,
		Tools:   tools,
	}
	if len(meta) > 0 {
		msg.Meta = meta
	}
	return msg
}

func (h *Hub) initMetaJSON(tenantID string) json.RawMessage {
	meta := map[string]any{}
	if len(h.initMeta) > 0 {
		_ = json.Unmarshal(h.initMeta, &meta)
	}
	if raw := h.tenantInitMeta[tenantID]; len(raw) > 0 {
		tenantMeta := map[string]any{}
		if err := json.Unmarshal(raw, &tenantMeta); err == nil {
			for key, value := range tenantMeta {
				meta[key] = value
			}
		}
	}
	if len(meta) > 0 {
		if raw, err := json.Marshal(meta); err == nil {
			return raw
		}
	}
	return nil
}

func (h *Hub) initToolsJSON(tenantID ...string) json.RawMessage {
	resolvedTenantID := ""
	if len(tenantID) > 0 {
		resolvedTenantID = tenantID[0]
	}
	h.syncAttachedRuntimeCapabilities()
	var tools []map[string]any
	if len(h.toolsJSON) > 0 {
		_ = json.Unmarshal(h.toolsJSON, &tools)
	}
	seen := make(map[string]bool, len(tools))
	for _, tool := range tools {
		if name, _ := tool["name"].(string); name != "" {
			seen[name] = true
		}
	}
	h.toolExecMu.RLock()
	for name, entry := range h.toolExec {
		if entry.Source != toolSourceRuntime || !entry.Advertise || name == "" || !toolExecVisible(entry, resolvedTenantID) {
			continue
		}
		toolName := name
		if idx := strings.LastIndex(name, "\x00"); idx >= 0 {
			toolName = name[idx+1:]
		}
		if toolName == "" || seen[toolName] {
			continue
		}
		tools = appendInitTool(tools, seen, toolName, entry.Schema)
	}
	h.toolExecMu.RUnlock()
	raw, err := json.Marshal(tools)
	if err != nil {
		return h.toolsJSON
	}
	return raw
}

func runtimeServesTenant(tenants []string, tenantID string) bool {
	if len(tenants) == 0 {
		return true
	}
	for _, item := range tenants {
		if item == "*" || item == tenantID {
			return true
		}
	}
	return false
}

func (h *Hub) syncAttachedRuntimeCapabilities() {
	if h == nil || h.runtimes == nil {
		return
	}
	changed := false
	for _, attachment := range h.runtimes.Snapshot() {
		if !attachment.Attached || attachment.ID == "" {
			continue
		}
		conn := h.runtimes.RuntimeConn(attachment.ID)
		if conn == nil {
			continue
		}
		if _, ok := conn.(*runtimeconn.Conn); ok {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		resp, err := conn.ListCapabilities(ctx)
		cancel()
		if err != nil {
			h.Logger.Warn("runtime capability sync failed", "runtime_id", attachment.ID, "err", err)
			continue
		}
		for _, capability := range resp.Targets {
			revision := capability.Revision
			if revision <= 0 {
				revision = 1
			}
			tenants := append([]string(nil), capability.Tenants...)
			if len(tenants) == 0 {
				tenants = append([]string(nil), attachment.TenantsServed...)
			}
			if _, ok, err := h.runtimes.ApplyCatalogUpdate(attachment.ID, wire.CatalogUpdate{
				Op:       wire.OpCatalogUpdate,
				Target:   capability.Target,
				Tenants:  tenants,
				Tools:    capability.Tools,
				Hooks:    capability.Hooks,
				Revision: revision,
				State:    capability.State,
				Source:   capability.Source,
			}); err != nil {
				h.Logger.Warn("runtime capability apply failed", "runtime_id", attachment.ID, "target", capability.Target.ID, "err", err)
			} else if ok {
				h.syncRuntimeCapability(attachment.ID, capability)
				changed = true
			}
		}
	}
	if changed {
		h.rebuildHookIndex()
	}
}

func appendInitTool(tools []map[string]any, seen map[string]bool, name string, schema json.RawMessage) []map[string]any {
	if name == "" || seen[name] {
		return tools
	}
	item := map[string]any{
		"name":     name,
		"required": []string{},
		"params":   map[string]any{},
	}
	if len(schema) > 0 {
		var decoded map[string]any
		if json.Unmarshal(schema, &decoded) == nil {
			item["schema"] = decoded
			if props, ok := decoded["properties"].(map[string]any); ok {
				item["params"] = props
			}
			if required, ok := decoded["required"].([]any); ok {
				items := make([]string, 0, len(required))
				for _, raw := range required {
					if s, ok := raw.(string); ok {
						items = append(items, s)
					}
				}
				item["required"] = items
			}
		}
	}
	tools = append(tools, item)
	seen[name] = true
	return tools
}
