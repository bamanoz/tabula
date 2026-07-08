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
		if msg.Topic == TopicTurnSteer {
			h.handleTurnSteer(sender, msg)
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
	h.applyBeforeTurnContext(plan.tenantID, plan.targetSession, msg)
	if clientIsManagedUserInput(sender) && !h.hasTurnReceiver(sender, plan.tenantID, plan.targetSession) {
		if h.queueMessagePlan(sender, msg, plan) {
			h.endSessionTurn(plan.tenantID, plan.targetSession)
			sender.SendMsg(&Message{Type: string(MsgEvent), Topic: TopicSessionStatus, Session: plan.targetSession, TenantID: plan.tenantID, Data: mustMarshalRaw(map[string]any{"state": "waiting_for_driver", "reason": "turn_receiver_unavailable"})})
			return
		}
		sender.SendMsg(&Message{Type: string(MsgError), Text: "session input queue full"})
		return
	}
	delivered := 0
	if clientIsManagedUserInput(sender) {
		delivered = h.broadcastToTurnReceivers(plan.tenantID, plan.targetSession, msg, sender, sender)
	} else {
		delivered = h.broadcastToSessionFrom(plan.tenantID, plan.targetSession, messageCapability(msg), msg, sender, sender)
	}
	if delivered == 0 && clientIsManagedUserInput(sender) {
		if h.queueMessagePlan(sender, msg, plan) {
			h.endSessionTurn(plan.tenantID, plan.targetSession)
			sender.SendMsg(&Message{Type: string(MsgEvent), Topic: TopicSessionStatus, Session: plan.targetSession, TenantID: plan.tenantID, Data: mustMarshalRaw(map[string]any{"state": "waiting_for_driver", "reason": "turn_receiver_unavailable"})})
			return
		}
		sender.SendMsg(&Message{Type: string(MsgError), Text: "session input queue full"})
	} else if delivered > 0 && clientIsManagedUserInput(sender) {
		h.setInflightInput(plan.tenantID, plan.targetSession, msg, sender)
	}
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
	queued.Session = plan.targetSession
	queued.TenantID = plan.tenantID
	return sess.EnqueueInput(queued, sender)
}

func (h *Hub) handleUserMessage(sender *Client, msg *Message) {
	msg.Meta, _ = ensureTurnCorrelationMeta(msg.Meta)
	plan := h.buildMessagePlan(sender, msg)
	h.stampMessagePreferredRuntime(sender, plan.tenantID, plan.targetSession, msg)
	if !plan.blocked && h.sessionStuckSuspended(plan.tenantID, plan.targetSession) {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "session suspended_stuck"})
		return
	}
	if !plan.blocked && clientIsManagedUserInput(sender) && !h.hasTurnReceiver(sender, plan.tenantID, plan.targetSession) {
		if h.queueMessagePlan(sender, msg, plan) {
			h.persistSessionState(plan.tenantID, plan.targetSession)
			return
		}
		sender.SendMsg(&Message{Type: string(MsgError), Text: "session input queue full"})
		return
	}
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
	h.applyMessagePreferredRuntime(plan.tenantID, plan.targetSession, msg)
	h.applyMessagePlan(sender, msg, plan)
}

func (h *Hub) sessionStuckSuspended(tenantID, session string) bool {
	if h == nil || h.sessions == nil || session == "" {
		return false
	}
	sess, ok := h.sessions.Get(session, tenantID)
	return ok && sess.IsStuckSuspended()
}

func (h *Hub) handleTurnSteer(sender *Client, msg *Message) {
	msg.Meta, _ = ensureTurnCorrelationMeta(msg.Meta)
	plan := h.buildMessagePlan(sender, msg)
	h.stampMessagePreferredRuntime(sender, plan.tenantID, plan.targetSession, msg)
	if plan.blocked {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "message blocked"})
		return
	}
	setMessageText(msg, plan.text)
	steer := cloneMessage(msg)
	steer.Topic = TopicTurnSteer
	steer.Session = plan.targetSession
	steer.TenantID = plan.tenantID
	if sess, ok := h.sessions.Get(plan.targetSession, plan.tenantID); ok && sess.HasActiveToolCalls() {
		if sess.EnqueueSteer(steer, sender) {
			h.persistSessionState(plan.tenantID, plan.targetSession)
			return
		}
		sender.SendMsg(&Message{Type: string(MsgError), Text: "session steer queue full"})
		return
	}
	h.applyMessagePreferredRuntime(plan.tenantID, plan.targetSession, steer)
	h.applyBeforeTurnContext(plan.tenantID, plan.targetSession, steer)
	h.broadcastToSessionFrom(plan.tenantID, plan.targetSession, TopicTurnSteer, steer, sender, sender)
}

func (h *Hub) forwardSessionMessage(sender *Client, msg *Message) {
	target := h.targetSession(sender, msg)
	tenantID := h.targetTenant(sender, msg)
	if msg.Type == string(MsgEvent) && msg.Topic == TopicCompactionStart {
		h.emitBeforeCompaction(tenantID, target, sender, msg)
	}
	h.broadcastToSessionFrom(tenantID, target, messageCapability(msg), msg, sender, sender)
	switch {
	case msg.Type == string(MsgEvent) && msg.Topic == TopicTurnDone:
		h.emitAfterTurn(tenantID, target, sender, msg)
		queued, ok := h.completeSessionTurn(tenantID, target)
		h.emitAfterMessage(tenantID, target, sender)
		if ok {
			h.dispatchQueuedInput(tenantID, target, queued)
		}
	case MsgType(msg.Type) == MsgError:
		h.emitAfterTurn(tenantID, target, sender, msg)
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
	return h.hasTurnReceiver(sender, tenantID, session)
}

func (h *Hub) hasTurnReceiver(sender *Client, tenantID, session string) bool {
	if session == "" || sender.depth != 0 {
		return false
	}
	for _, client := range h.sessionClients(tenantID, session) {
		if client == sender {
			continue
		}
		if !client.IsConnected() {
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

func (h *Hub) endSessionTurn(tenantID, session string) {
	sess, ok := h.sessions.Get(session, tenantID)
	if !ok {
		return
	}
	sess.EndTurn()
	h.persistSessionState(tenantID, session)
}

func (h *Hub) setInflightInput(tenantID, session string, msg *Message, exclude *Client) {
	sess, ok := h.sessions.Get(session, tenantID)
	if !ok {
		return
	}
	sess.SetInflightInput(msg, exclude)
	h.persistSessionState(tenantID, session)
}

func (h *Hub) interruptSessionTurn(tenantID, session string) (queuedInput, bool) {
	sess, ok := h.sessions.Get(session, tenantID)
	if !ok {
		return queuedInput{}, false
	}
	input, hasInput := sess.InterruptTurn()
	h.persistSessionState(tenantID, session)
	return input, hasInput
}

func (h *Hub) beginQueuedInput(tenantID, session string) (queuedInput, bool) {
	sess, ok := h.sessions.Get(session, tenantID)
	if !ok {
		return queuedInput{}, false
	}
	queued, hasQueued := sess.BeginQueuedInput()
	if hasQueued {
		h.persistSessionState(tenantID, session)
	}
	return queued, hasQueued
}

func (h *Hub) dispatchQueuedInput(tenantID, session string, input queuedInput) {
	if input.message == nil {
		return
	}
	if input.message.TenantID != "" {
		tenantID = input.message.TenantID
	}
	if input.message.Session != "" {
		session = input.message.Session
	}
	h.applyMessagePreferredRuntime(tenantID, session, input.message)
	h.applyBeforeTurnContext(tenantID, session, input.message)
	delivered := 0
	if clientIsManagedUserInput(input.exclude) {
		delivered = h.broadcastToTurnReceivers(tenantID, session, input.message, nil, nil)
	} else {
		delivered = h.broadcastToSession(tenantID, session, messageCapability(input.message), input.message, nil)
	}
	if delivered == 0 && clientIsManagedUserInput(input.exclude) {
		plan := messagePlan{tenantID: tenantID, targetSession: session, text: messageText(input.message)}
		h.endSessionTurn(tenantID, session)
		if h.queueMessagePlan(input.exclude, input.message, plan) {
			input.exclude.SendMsg(&Message{Type: string(MsgEvent), Topic: TopicSessionStatus, Session: session, TenantID: tenantID, Data: mustMarshalRaw(map[string]any{"state": "waiting_for_driver", "reason": "turn_receiver_unavailable"})})
			return
		}
		input.exclude.SendMsg(&Message{Type: string(MsgError), Text: "session input queue full"})
	} else if delivered > 0 && clientIsManagedUserInput(input.exclude) {
		h.setInflightInput(tenantID, session, input.message, input.exclude)
	}
}

func (h *Hub) dispatchQueuedSteer(tenantID, session string, input queuedInput) {
	if input.message == nil {
		return
	}
	if input.message.TenantID != "" {
		tenantID = input.message.TenantID
	}
	if input.message.Session != "" {
		session = input.message.Session
	}
	h.applyMessagePreferredRuntime(tenantID, session, input.message)
	h.applyBeforeTurnContext(tenantID, session, input.message)
	h.broadcastToSession(tenantID, session, TopicTurnSteer, input.message, nil)
}
