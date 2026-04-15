package kernel

func (h *Hub) handleSessionMessage(sender *Client, msg *Message) {
	if MsgType(msg.Type) == MsgHookResult {
		h.handleHookResult(msg)
		return
	}
	if err := h.policy.CanSend(sender, msg); err != nil {
		h.Logger.Warn("policy denied", "reason", err.Error(), "client", sender.name)
		return
	}

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

func (h *Hub) handleUserMessage(sender *Client, msg *Message) {
	plan := h.buildMessagePlan(sender, msg)
	h.applyMessagePlan(sender, msg, plan)
}

func (h *Hub) forwardSessionMessage(sender *Client, msg *Message) {
	target := h.targetSession(sender, msg)
	h.broadcastToSession(target, msg.Type, msg, sender)
	if MsgType(msg.Type) == MsgDone {
		h.emitAfterMessage(target, sender)
	}
}
