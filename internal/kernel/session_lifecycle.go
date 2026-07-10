package kernel

func (h *Hub) handleSessionArchive(sender *Client, msg *Message) {
	tenantID := h.targetTenant(sender, msg)
	session := h.targetSession(sender, msg)
	if session == "" {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "session.archive missing session"})
		return
	}
	sess := h.sessions.GetOrCreate(session, tenantID)
	archivedAt := sess.Archive()
	h.persistSessionState(tenantID, session)
	h.broadcastSessionLifecycle(tenantID, session, TopicSessionArchived, map[string]any{"archived_at": formatSnapshotTime(archivedAt)})
}

func (h *Hub) handleSessionDelete(sender *Client, msg *Message) {
	tenantID := h.targetTenant(sender, msg)
	session := h.targetSession(sender, msg)
	if session == "" {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "session.delete missing session"})
		return
	}
	sess := h.sessions.GetOrCreate(session, tenantID)
	deletedAt := sess.MarkDeleted()
	h.persistSessionState(tenantID, session)
	h.broadcastSessionLifecycle(tenantID, session, TopicSessionDeleted, map[string]any{"deleted_at": formatSnapshotTime(deletedAt)})
}

func (h *Hub) broadcastSessionLifecycle(tenantID, session, topic string, extra map[string]any) {
	data := map[string]any{"session": session, "tenant_id": tenantID}
	for key, value := range extra {
		data[key] = value
	}
	h.broadcastToSession(tenantID, session, topic, &Message{
		Type:     string(MsgEvent),
		Topic:    topic,
		Session:  session,
		TenantID: tenantID,
		Data:     mustMarshalRaw(data),
	}, nil)
}

func (h *Hub) sessionDeleted(tenantID, session string) bool {
	if h == nil || h.sessions == nil || session == "" {
		return false
	}
	sess, ok := h.sessions.Get(session, tenantID)
	return ok && sess.IsDeleted()
}
