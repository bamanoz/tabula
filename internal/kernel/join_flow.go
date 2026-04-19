package kernel

// joinPlan holds the complete result of a join computation.
// Built in the build phase, applied in the side-effect phase.
type joinPlan struct {
	session       string
	joined        *Message
	memberJoined  *Message
	init          *Message
	blockedReason string
}

// buildJoinPlan resolves session_start policy and prepares protocol-visible join messages.
func (h *Hub) buildJoinPlan(c *Client, session string) joinPlan {
	plan := joinPlan{
		session: session,
		joined: &Message{
			Type:    string(MsgJoined),
			Session: session,
		},
		memberJoined: &Message{
			Type:    string(MsgMemberJoined),
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

	sess := h.sessions.GetOrCreate(plan.session)
	sess.AddClient(c.name)
	h.assignClientSession(c, plan.session)
	c.MarkJoined()

	c.SendMsg(plan.joined)
	h.Logger.Info("client joined session", "name", c.name, "session", plan.session)

	h.broadcastToSession(plan.session, string(MsgMemberJoined), plan.memberJoined, c)

	if plan.init != nil {
		c.SendMsg(plan.init)
		h.Logger.Debug("sent init", "client", c.name)
	}
}

func (h *Hub) finalizeJoinPlan(c *Client, plan *joinPlan) {
	context, blocked := h.policy.CanJoin(plan.session, c.name)
	if blocked {
		plan.blockedReason = "session blocked by hook"
		return
	}

	if c.canReceive(string(MsgInit)) {
		plan.init = h.initMessage(context)
	}
}

func (h *Hub) initMessage(context string) *Message {
	return &Message{
		Type:    string(MsgInit),
		Context: context,
		Tools:   h.toolsJSON,
	}
}
