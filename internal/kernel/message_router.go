package kernel

func (h *Hub) handleSessionMessage(sender *Client, msg *BusMessage) {
	if BusMessageType(msg.Type) == MsgHookReply {
		if err := h.policy.CanRespondHook(sender, msg); err != nil {
			h.Logger.Warn("policy denied hook_reply", "reason", err.Error(), "client", sender.name)
			return
		}
		h.handleHookResult(sender, msg)
		return
	}
	if err := h.policy.CanSend(sender, msg); err != nil {
		h.Logger.Warn("policy denied", "reason", err.Error(), "client", sender.name)
		return
	}

	tenantID := h.targetTenant(sender, msg)
	h.touchSessionActivity(tenantID, h.targetSession(sender, msg))

	switch BusMessageType(msg.Type) {
	case MsgRequest:
		if msg.Topic == TopicKernelSessions {
			sender.SendMsg(&BusMessage{Type: string(MsgReply), Topic: TopicKernelSessions, ID: msg.ID, Data: h.SnapshotSessions()})
			return
		}
		if isExchangeTopic(msg.Topic) {
			h.handleExchangeRequest(sender, msg)
			return
		}
		if msg.Topic == TopicToolCall {
			h.handleToolUse(sender, msg)
			return
		}
		h.forwardSessionMessage(sender, msg)
	case MsgReply:
		if isExchangeTopic(msg.Topic) {
			h.handleExchangeReply(sender, msg)
			return
		}
		h.forwardSessionMessage(sender, msg)
	case MsgEvent:
		switch msg.Topic {
		case TopicSessionArchive:
			h.handleSessionArchive(sender, msg)
		case TopicSessionDelete:
			h.handleSessionDelete(sender, msg)
		default:
			h.forwardSessionMessage(sender, msg)
		}
	default:
		h.forwardSessionMessage(sender, msg)
	}
}

func (h *Hub) forwardSessionMessage(sender *Client, msg *BusMessage) {
	target := h.targetSession(sender, msg)
	tenantID := h.targetTenant(sender, msg)
	if msg.Type == string(MsgEvent) && msg.Topic == TopicCompactionStart {
		h.emitBeforeCompaction(tenantID, target, sender, msg)
	}
	h.broadcastToSessionFrom(tenantID, target, messageCapability(msg), msg, sender, sender)
}

func (h *Hub) touchSessionActivity(tenantID, session string) {
	if session == "" {
		return
	}
	sess, ok := h.sessions.Get(session, tenantID)
	if !ok {
		return
	}
	sess.Touch()
}
