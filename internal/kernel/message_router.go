package kernel

func (h *Hub) handleSessionMessage(sender *Client, msg *Message) {
	if MsgType(msg.Type) == MsgHookResult {
		if err := h.policy.CanRespondHook(sender, msg); err != nil {
			h.Logger.Warn("policy denied hook_result", "reason", err.Error(), "client", sender.name)
			return
		}
		h.handleHookResult(sender, msg)
		return
	}
	if err := h.policy.CanSend(sender, msg); err != nil {
		h.Logger.Warn("policy denied", "reason", err.Error(), "client", sender.name)
		return
	}

	h.touchSessionActivity(h.targetSession(sender, msg))

	switch MsgType(msg.Type) {
	case MsgToolUse:
		h.handleToolUse(sender, msg)
	case MsgCancel:
		h.handleCancel(sender.session)
	case MsgMessage:
		h.handleUserMessage(sender, msg)
	default:
		h.forwardSessionMessage(sender, msg)
	}
}

// messagePlan holds the result of message processing computation.
type messagePlan struct {
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
		targetSession: h.targetSession(sender, msg),
		text:          text,
	}
}

// applyMessagePlan executes the side effects of message processing.
func (h *Hub) applyMessagePlan(sender *Client, msg *Message, plan messagePlan) {
	if plan.blocked {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "message blocked by hook"})
		return
	}

	msg.Text = plan.text
	h.broadcastToSession(plan.targetSession, msg.Type, msg, sender)
}

func (h *Hub) queueMessagePlan(sender *Client, msg *Message, plan messagePlan) bool {
	if plan.blocked {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "message blocked by hook"})
		return true
	}
	sess, ok := h.sessions.Get(plan.targetSession)
	if !ok {
		return false
	}
	queued := cloneMessage(msg)
	queued.Text = plan.text
	return sess.EnqueueInput(queued, sender)
}

func (h *Hub) handleUserMessage(sender *Client, msg *Message) {
	plan := h.buildMessagePlan(sender, msg)
	if !plan.blocked && h.shouldStartSessionTurn(sender, plan.targetSession) {
		if !h.tryBeginSessionTurn(plan.targetSession) {
			if h.queueMessagePlan(sender, msg, plan) {
				h.persistSessionState(plan.targetSession)
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
	h.broadcastToSession(target, msg.Type, msg, sender)
	switch MsgType(msg.Type) {
	case MsgDone:
		queued, ok := h.completeSessionTurn(target)
		h.emitAfterMessage(target, sender)
		if ok {
			h.dispatchQueuedInput(target, queued)
		}
	case MsgError:
		queued, ok := h.completeSessionTurn(target)
		if ok {
			h.dispatchQueuedInput(target, queued)
		}
	}
}

func (h *Hub) touchSessionActivity(session string) {
	if session == "" {
		return
	}
	sess, ok := h.sessions.Get(session)
	if !ok {
		return
	}
	sess.Touch()
}

func (h *Hub) shouldStartSessionTurn(sender *Client, session string) bool {
	if session == "" || sender.depth != 0 {
		return false
	}
	for _, client := range h.sessionClients(session) {
		if client == sender {
			continue
		}
		if client.canReceive(string(MsgMessage)) && client.canSend(string(MsgDone)) {
			return true
		}
	}
	return false
}

func (h *Hub) tryBeginSessionTurn(session string) bool {
	sess, ok := h.sessions.Get(session)
	if !ok {
		return false
	}
	ok = sess.BeginTurn()
	if ok {
		h.persistSessionState(session)
	}
	return ok
}

func (h *Hub) completeSessionTurn(session string) (queuedInput, bool) {
	sess, ok := h.sessions.Get(session)
	if !ok {
		return queuedInput{}, false
	}
	queued, hasQueued := sess.CompleteTurn()
	h.persistSessionState(session)
	return queued, hasQueued
}

func (h *Hub) dispatchQueuedInput(session string, input queuedInput) {
	if input.message == nil {
		return
	}
	h.broadcastToSession(session, input.message.Type, input.message, input.exclude)
}
