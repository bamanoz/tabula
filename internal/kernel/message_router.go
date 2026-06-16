package kernel

func (h *Hub) handleSessionMessage(sender *Client, msg *Message) {
	if MsgType(msg.Type) == MsgHookReply {
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

	switch MsgType(msg.Type) {
	case MsgRequest:
		if msg.Topic == TopicKernelSessions {
			sender.SendMsg(&Message{Type: string(MsgReply), Topic: TopicKernelSessions, ID: msg.ID, Data: h.SnapshotSessions()})
			return
		}
		if isExchangeTopic(msg.Topic) {
			h.handleExchangeRequest(sender, msg)
			return
		}
		if msg.Topic != TopicToolCall {
			h.forwardSessionMessage(sender, msg)
			return
		}
		h.handleToolUse(sender, msg)
	case MsgReply:
		if isExchangeTopic(msg.Topic) {
			h.handleExchangeReply(sender, msg)
			return
		}
		h.forwardSessionMessage(sender, msg)
	case MsgEvent:
		if msg.Topic == TopicMessageUser {
			h.handleUserMessage(sender, msg)
			return
		}
		if msg.Topic == TopicTurnCancel {
			h.handleCancel(tenantID, h.targetSession(sender, msg))
			return
		}
		h.forwardSessionMessage(sender, msg)
	default:
		h.forwardSessionMessage(sender, msg)
	}
}

// messagePlan holds the result of message processing computation.
type messagePlan struct {
	tenantID      string
	targetSession string
	blocked       bool
	text          string
}

// buildMessagePlan runs the before_message hook and determines the outcome.
func (h *Hub) buildMessagePlan(sender *Client, msg *Message) messagePlan {
	text, blocked := h.policy.BeforeMessage(sender, msg)
	if blocked {
		return messagePlan{blocked: true}
	}

	return messagePlan{
		tenantID:      h.targetTenant(sender, msg),
		targetSession: h.targetSession(sender, msg),
		text:          text,
	}
}

// applyMessagePlan executes the side effects of message processing.
func (h *Hub) applyMessagePlan(sender *Client, msg *Message, plan messagePlan) {
	if plan.blocked {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "message blocked"})
		return
	}

	setMessageText(msg, plan.text)
	h.broadcastToSessionFrom(plan.tenantID, plan.targetSession, messageCapability(msg), msg, sender, sender)
}

func (h *Hub) queueMessagePlan(sender *Client, msg *Message, plan messagePlan) bool {
	if plan.blocked {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "message blocked"})
		return true
	}
	sess, ok := h.sessions.Get(plan.targetSession, plan.tenantID)
	if !ok {
		return false
	}
	queued := cloneMessage(msg)
	setMessageText(queued, plan.text)
	return sess.EnqueueInput(queued, sender)
}

func (h *Hub) handleUserMessage(sender *Client, msg *Message) {
	msg.Meta, _ = ensureTurnCorrelationMeta(msg.Meta)
	plan := h.buildMessagePlan(sender, msg)
	if !plan.blocked && h.shouldStartSessionTurn(sender, plan.tenantID, plan.targetSession) {
		if !h.tryBeginSessionTurn(plan.tenantID, plan.targetSession) {
			if h.queueMessagePlan(sender, msg, plan) {
				h.persistSessionState(plan.tenantID, plan.targetSession)
				return
			}
			sender.SendMsg(&Message{Type: string(MsgError), Text: "session input queue full"})
			return
		}
	}
	h.applyMessagePlan(sender, msg, plan)
}

func (h *Hub) forwardSessionMessage(sender *Client, msg *Message) {
	target := h.targetSession(sender, msg)
	tenantID := h.targetTenant(sender, msg)
	h.broadcastToSessionFrom(tenantID, target, messageCapability(msg), msg, sender, sender)
	switch {
	case msg.Type == string(MsgEvent) && msg.Topic == TopicTurnDone:
		queued, ok := h.completeSessionTurn(tenantID, target)
		h.emitAfterMessage(tenantID, target, sender)
		if ok {
			h.dispatchQueuedInput(tenantID, target, queued)
		}
	case MsgType(msg.Type) == MsgError:
		queued, ok := h.completeSessionTurn(tenantID, target)
		if ok {
			h.dispatchQueuedInput(tenantID, target, queued)
		}
	}
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

func (h *Hub) shouldStartSessionTurn(sender *Client, tenantID, session string) bool {
	if session == "" || sender.depth != 0 {
		return false
	}
	for _, client := range h.sessionClients(tenantID, session) {
		if client == sender {
			continue
		}
		if client.canReceive(TopicMessageUser) && client.canSend(TopicTurnDone) {
			return true
		}
	}
	return false
}

func (h *Hub) tryBeginSessionTurn(tenantID, session string) bool {
	sess, ok := h.sessions.Get(session, tenantID)
	if !ok {
		return false
	}
	ok = sess.BeginTurn()
	if ok {
		h.persistSessionState(tenantID, session)
	}
	return ok
}

func (h *Hub) completeSessionTurn(tenantID, session string) (queuedInput, bool) {
	sess, ok := h.sessions.Get(session, tenantID)
	if !ok {
		return queuedInput{}, false
	}
	queued, hasQueued := sess.CompleteTurn()
	h.persistSessionState(tenantID, session)
	return queued, hasQueued
}

func (h *Hub) dispatchQueuedInput(tenantID, session string, input queuedInput) {
	if input.message == nil {
		return
	}
	h.broadcastToSession(tenantID, session, messageCapability(input.message), input.message, input.exclude)
}
