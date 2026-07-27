package kernel

import (
	"encoding/json"
	"fmt"
	khooks "github.com/bamanoz/tabula/internal/kernel/hooks"
)

// connectPlan holds the complete result of a connect computation.
type connectPlan struct {
	name           string
	sends          []string
	receives       []string
	receivesGlobal []string
	hooks          []khooks.Subscription
	meta           json.RawMessage
	depth          int
	clientID       int
	requestID      string
	errorMsg       string
	connectedMsg   *Message
}

// buildConnectPlan performs pure computation for a connect:
// validates protocol version, spawn token, computes the next client ID.
// No state mutations or messages are sent during this phase.
func (h *Hub) buildConnectPlan(c *Client, msg *Message) connectPlan {
	data, err := decodeHelloData(msg)
	if err != nil {
		return connectPlan{errorMsg: err.Error()}
	}
	plan := connectPlan{
		name:           data.Name,
		sends:          data.SendTopics,
		receives:       data.ReceiveTopics,
		receivesGlobal: data.GlobalTopics,
		hooks:          data.Hooks,
		meta:           append(json.RawMessage(nil), data.Meta...),
		requestID:      msg.ID,
	}

	// Protocol version must match exactly. Version 0 is no longer accepted —
	// clients must declare PROTOCOL_VERSION on connect.
	if msg.V != ProtocolVersion {
		return connectPlan{errorMsg: fmt.Sprintf("unsupported protocol version %d (kernel expects %d)", msg.V, ProtocolVersion)}
	}

	depth, err := h.policy.CanConnect("", data.AuthToken)
	if err != nil {
		return connectPlan{errorMsg: err.Error()}
	}
	plan.depth = depth

	// Compute the next ID without mutating the client.
	id := h.nextClientID()
	plan.clientID = id
	ackData, _ := json.Marshal(map[string]any{
		"client_id":       fmt.Sprintf("c%d", id),
		"server_protocol": ProtocolVersion,
	})
	plan.connectedMsg = &Message{
		V:    ProtocolVersion,
		Type: string(MsgHelloAck),
		ID:   plan.requestID,
		Data: ackData,
	}

	return plan
}

// applyConnectPlan executes the side effects of a connect:
// mutates state (configure client, rebuild hooks) and sends messages.
func (h *Hub) applyConnectPlan(c *Client, plan connectPlan) {
	if plan.errorMsg != "" {
		h.Logger.Warn("client connect rejected", "from", plan.name, "reason", plan.errorMsg)
		c.SendMsg(&Message{
			V:    ProtocolVersion,
			Type: string(MsgError),
			Text: plan.errorMsg,
		})
		return
	}

	// Mutate state.
	h.configureClient(c, plan.name, plan.sends, plan.receives, plan.receivesGlobal, plan.hooks, plan.meta, plan.depth)
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

func (h *Hub) handleCancel(tenantID, session string) {
	if session == "" {
		return
	}
	if h.tools != nil {
		h.tools.CancelSession(tenantID, session)
	}
	sess, ok := h.sessions.Get(session, tenantID)
	if ok && sess.RequestCancel() {
		h.persistSessionState(tenantID, session)
	}
	payload, _ := json.Marshal(map[string]string{"session": session, "tenant_id": tenantID})
	h.dispatchHook("cancel", payload, tenantID, session)
	delivered := h.broadcastToSession(tenantID, session, TopicTurnCancel, &Message{Type: string(MsgEvent), Topic: TopicTurnCancel}, nil)
	h.Logger.Info("turn cancel broadcast", "tenant_id", tenantID, "session", session, "delivered", delivered)
}

func makeCapabilitySet(items []string) map[string]bool {
	set := make(map[string]bool, len(items))
	for _, item := range items {
		set[item] = true
	}
	return set
}
