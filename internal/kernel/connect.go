package kernel

import (
	"encoding/json"
	"fmt"
)

// connectPlan holds the complete result of a connect computation.
type connectPlan struct {
	name           string
	sends          []string
	receives       []string
	receivesGlobal []string
	hooks          []HookSubscription
	depth          int
	clientID       int
	errorMsg       string
	connectedMsg   *Message
}

// buildConnectPlan performs pure computation for a connect:
// validates protocol version, spawn token, computes the next client ID.
// No state mutations or messages are sent during this phase.
func (h *Hub) buildConnectPlan(c *Client, msg *Message) connectPlan {
	plan := connectPlan{
		name:           msg.Name,
		sends:          msg.Sends,
		receives:       msg.Receives,
		receivesGlobal: msg.ReceivesGlobal,
		hooks:          msg.Hooks,
	}

	// Protocol version must match exactly. Version 0 is no longer accepted —
	// clients must declare PROTOCOL_VERSION on connect.
	if msg.Version != ProtocolVersion {
		return connectPlan{errorMsg: fmt.Sprintf("unsupported protocol version %d (kernel expects %d)", msg.Version, ProtocolVersion)}
	}

	depth, err := h.policy.CanConnect(msg.Token, msg.AuthToken)
	if err != nil {
		return connectPlan{errorMsg: err.Error()}
	}
	plan.depth = depth

	// Compute the next ID without mutating the client.
	id := h.nextClientID()
	plan.clientID = id
	plan.connectedMsg = &Message{
		Type:    string(MsgConnected),
		Version: ProtocolVersion,
		ID:      fmt.Sprintf("c%d", id),
	}

	return plan
}

// applyConnectPlan executes the side effects of a connect:
// mutates state (configure client, rebuild hooks) and sends messages.
func (h *Hub) applyConnectPlan(c *Client, plan connectPlan) {
	if plan.errorMsg != "" {
		h.Logger.Warn("client connect rejected", "from", plan.name, "reason", plan.errorMsg)
		c.SendMsg(&Message{
			Type: string(MsgError),
			Text: plan.errorMsg,
		})
		return
	}

	// Mutate state.
	h.configureClient(c, plan.name, plan.sends, plan.receives, plan.receivesGlobal, plan.hooks, plan.depth)
	c.MarkProtocolReady()
	h.rebuildHookIndex()

	// Send messages.
	c.SendMsg(plan.connectedMsg)
	h.Logger.Info("client connected", "name", c.name, "id", plan.clientID)
}

// nextClientID returns the next client ID without incrementing the counter.
func (h *Hub) nextClientID() int {
	return h.clients.NextID()
}

func (h *Hub) handleConnect(c *Client, msg *Message) {
	plan := h.buildConnectPlan(c, msg)
	h.applyConnectPlan(c, plan)
}

func (h *Hub) handleJoin(c *Client, msg *Message) {
	plan := h.buildJoinPlan(c, msg.Session, msg.TenantID)
	h.applyJoinPlan(c, plan)
}

func (h *Hub) handleCancel(session string) {
	if session == "" {
		return
	}
	sess, ok := h.sessions.Get(session)
	if !ok || !sess.RequestCancel() {
		return
	}
	h.persistSessionState(session)
	payload, _ := json.Marshal(map[string]string{"session": session})
	h.dispatchHook("cancel", payload, session)
	h.broadcastToSession(session, string(MsgCancel), &Message{Type: string(MsgCancel)}, nil)
	h.forEachProcess(func(pid int, proc *SpawnedProcess) {
		if proc.Alive && proc.Session == session {
			h.Logger.Debug("sending SIGINT", "pid", pid)
			proc.Signal()
		}
	})
}

func makeCapabilitySet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}
