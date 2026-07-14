package kernel

import (
	"encoding/json"
	"fmt"
	"sort"
)

type pendingExchange struct {
	requester  *Client
	responders map[*Client]bool
	tenantID   string
	session    string
	topic      string
}

type pendingSuspendedExchange struct {
	responders map[*Client]bool
	tenantID   string
	session    string
	exchangeID string
}

func (h *Hub) handleExchangeRequest(sender *Client, msg *Message) {
	if msg.ID == "" {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "exchange request missing id"})
		return
	}
	session := h.targetSession(sender, msg)
	tenantID := h.targetTenant(sender, msg)
	responders := h.pickExchangeResponders(sender, tenantID, session, msg.Topic)
	if len(responders) == 0 {
		sender.SendMsg(&Message{Type: string(MsgError), Text: fmt.Sprintf("no responder for %s", msg.Topic)})
		return
	}
	responderSet := clientSet(responders)
	h.exchangesMu.Lock()
	h.exchanges[msg.ID] = pendingExchange{requester: sender, responders: responderSet, tenantID: tenantID, session: session, topic: msg.Topic}
	h.exchangesMu.Unlock()
	msg.TenantID = tenantID
	for _, responder := range responders {
		responder.SendMsg(h.prepareRoutedMessage(sender, session, "exchange", msg))
	}
}

func (h *Hub) handleExchangeReply(sender *Client, msg *Message) {
	if h.handleSuspendedExchangeReply(sender, msg) {
		return
	}
	h.exchangesMu.Lock()
	pending, ok := h.exchanges[msg.ID]
	if ok && pending.responders[sender] && pending.topic == msg.Topic {
		delete(h.exchanges, msg.ID)
	}
	h.exchangesMu.Unlock()
	if !ok || !pending.responders[sender] || pending.topic != msg.Topic {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "client not allowed to answer exchange"})
		return
	}
	if pending.requester == nil || !pending.requester.IsConnected() {
		return
	}
	msg.TenantID = pending.tenantID
	pending.requester.SendMsg(h.prepareRoutedMessage(sender, pending.session, "exchange", msg))
	h.broadcastExchangeResolved(pending.responders, sender, pending.tenantID, pending.session, pending.topic, msg.ID, msg.Data)
}

func (h *Hub) requestExchangeForPendingTool(pending pendingToolCall, blocked *HookDispatchDecision) {
	if h == nil {
		return
	}
	topic := pending.ExchangeTopic
	if topic == "" {
		topic = TopicExchangeApprove
	}
	responders := h.pickExchangeResponders(nil, pending.TenantID, pending.Session, topic)
	h.suspendedExchangeMu.Lock()
	h.suspendedExchanges[pending.ExchangeID] = pendingSuspendedExchange{responders: clientSet(responders), tenantID: pending.TenantID, session: pending.Session, exchangeID: pending.ExchangeID}
	h.suspendedExchangeMu.Unlock()
	if len(responders) == 0 {
		h.Logger.Warn("no responder for suspended exchange", "exchange_id", pending.ExchangeID, "topic", topic, "tool", pending.ToolName, "tool_call_id", pending.ToolID, "tenant_id", pending.TenantID, "session", pending.Session)
		return
	}
	data := exchangeRequestData(pending, blocked)
	for _, responder := range responders {
		responder.SendMsg(h.prepareRoutedMessage(nil, pending.Session, "exchange", &Message{Type: string(MsgRequest), Topic: topic, ID: pending.ExchangeID, Session: pending.Session, TenantID: pending.TenantID, Data: data}))
	}
}

func (h *Hub) resendPendingSuspendedExchanges(c *Client, tenantID, session string) {
	if h == nil || c == nil {
		return
	}
	if c.tenantID != "" && c.tenantID != tenantID {
		return
	}
	h.tools.mu.Lock()
	pendingCalls := make([]pendingToolCall, 0, len(h.tools.pendingCalls))
	for _, pending := range h.tools.pendingCalls {
		if pending.TenantID == tenantID && pending.Session == session {
			pendingCalls = append(pendingCalls, pending)
		}
	}
	h.tools.mu.Unlock()
	for _, pending := range pendingCalls {
		topic := pending.ExchangeTopic
		if topic == "" {
			topic = TopicExchangeApprove
		}
		if !c.canReceive(topic) || !c.canSend(topic) {
			continue
		}
		h.suspendedExchangeMu.Lock()
		exchange := h.suspendedExchanges[pending.ExchangeID]
		if exchange.responders == nil {
			exchange.responders = map[*Client]bool{}
		}
		exchange.responders[c] = true
		exchange.tenantID = tenantID
		exchange.session = session
		exchange.exchangeID = pending.ExchangeID
		h.suspendedExchanges[pending.ExchangeID] = exchange
		h.suspendedExchangeMu.Unlock()
		c.SendMsg(h.prepareRoutedMessage(nil, session, "exchange", &Message{Type: string(MsgRequest), Topic: topic, ID: pending.ExchangeID, Session: session, TenantID: tenantID, Data: exchangeRequestData(pending, nil)}))
	}
}

func (h *Hub) handleSuspendedExchangeReply(sender *Client, msg *Message) bool {
	h.suspendedExchangeMu.Lock()
	pending, ok := h.suspendedExchanges[msg.ID]
	if ok && pending.responders[sender] {
		delete(h.suspendedExchanges, msg.ID)
	}
	h.suspendedExchangeMu.Unlock()
	if !ok {
		return false
	}
	if !pending.responders[sender] {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "client not allowed to answer exchange"})
		return true
	}
	toolPending, found := h.tools.pendingCall(msg.ID)
	if !found {
		return true
	}
	if toolPending.ExchangeTopic != "" && msg.Topic != toolPending.ExchangeTopic {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "client answered suspended tool on wrong exchange topic"})
		return true
	}
	h.tools.setPendingExchangeReply(pending.exchangeID, msg.Data)
	h.tools.resolvePendingExchange(pending.exchangeID)
	h.broadcastExchangeResolved(pending.responders, sender, pending.tenantID, pending.session, msg.Topic, msg.ID, msg.Data)
	return true
}

func exchangeRequestData(pending pendingToolCall, blocked *HookDispatchDecision) json.RawMessage {
	if len(pending.ExchangeData) > 0 {
		var data map[string]any
		if json.Unmarshal(pending.ExchangeData, &data) == nil {
			data["exchange_id"] = pending.ExchangeID
			return mustMarshalRaw(data)
		}
		return append(json.RawMessage(nil), pending.ExchangeData...)
	}
	return mustMarshalRaw(map[string]any{"exchange_id": pending.ExchangeID})
}

func exchangeRequestDataFromDecision(toolName string, blocked *HookDispatchDecision) json.RawMessage {
	_ = toolName
	if blocked == nil || len(blocked.Payload) == 0 {
		return nil
	}
	var payload map[string]any
	if json.Unmarshal(blocked.Payload, &payload) != nil {
		return nil
	}
	data := map[string]any{}
	for key, value := range payload {
		if key == "exchange_id" || key == "topic" {
			continue
		}
		data[key] = value
	}
	if len(data) == 0 {
		return nil
	}
	return mustMarshalRaw(data)
}

func (h *Hub) pickExchangeResponders(sender *Client, tenantID, session string, topic string) []*Client {
	if responders := pickPreferredExchangeResponders(h.sessionClients(tenantID, session), func(c *Client) bool {
		return c != sender && c.IsConnected() && c.canReceive(topic) && c.canSend(topic)
	}); len(responders) > 0 {
		return responders
	}
	return pickPreferredExchangeResponders(h.allClients(), func(c *Client) bool {
		if c == sender || !c.IsConnected() || !c.canReceiveGlobal(topic) || !c.canSend(topic) {
			return false
		}
		return c.tenantID == "" || c.tenantID == tenantID
	})
}

func pickPreferredExchangeResponders(clients []*Client, eligible func(*Client) bool) []*Client {
	candidates := make([]*Client, 0, len(clients))
	for _, c := range clients {
		if eligible(c) {
			candidates = append(candidates, c)
		}
	}
	if len(candidates) == 0 {
		return nil
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		left := exchangeResponderScore(candidates[i])
		right := exchangeResponderScore(candidates[j])
		if left != right {
			return left > right
		}
		return candidates[i].id > candidates[j].id
	})
	bestScore := exchangeResponderScore(candidates[0])
	selected := make([]*Client, 0, len(candidates))
	for _, candidate := range candidates {
		if exchangeResponderScore(candidate) != bestScore {
			break
		}
		selected = append(selected, candidate)
	}
	return selected
}

func clientSet(clients []*Client) map[*Client]bool {
	set := make(map[*Client]bool, len(clients))
	for _, client := range clients {
		set[client] = true
	}
	return set
}

func (h *Hub) broadcastExchangeResolved(responders map[*Client]bool, winner *Client, tenantID, session, topic, id string, data json.RawMessage) {
	if len(responders) == 0 {
		return
	}
	resolved := &Message{
		Type:     string(MsgEvent),
		Topic:    topic,
		ID:       id,
		Session:  session,
		TenantID: tenantID,
		Data: mustMarshalRaw(map[string]any{
			"type":        "exchange.resolved",
			"id":          id,
			"topic":       topic,
			"resolved_by": clientExchangeResolverData(winner),
			"reply":       json.RawMessage(data),
		}),
	}
	for responder := range responders {
		if responder == nil || !responder.IsConnected() {
			continue
		}
		responder.SendMsg(h.prepareRoutedMessage(winner, session, "exchange", resolved))
	}
}

func (h *Hub) removeSuspendedExchangeResponder(c *Client) {
	if h == nil || c == nil {
		return
	}
	h.suspendedExchangeMu.Lock()
	defer h.suspendedExchangeMu.Unlock()
	for id, exchange := range h.suspendedExchanges {
		if exchange.responders[c] {
			delete(exchange.responders, c)
		}
		if len(exchange.responders) == 0 {
			delete(h.suspendedExchanges, id)
			continue
		}
		h.suspendedExchanges[id] = exchange
	}
}

func clientExchangeResolverData(client *Client) map[string]any {
	if client == nil {
		return map[string]any{}
	}
	data := map[string]any{"id": client.id, "name": client.name}
	if len(client.meta) > 0 {
		var meta map[string]any
		if json.Unmarshal(client.meta, &meta) == nil {
			data["meta"] = meta
		}
	}
	return data
}

func exchangeResponderScore(c *Client) int {
	if c == nil || len(c.meta) == 0 {
		return 0
	}
	meta := decodeClientMeta(c.meta)
	score := 0
	if meta.Role == "ui" {
		score += 2
	}
	if meta.Managed {
		score++
	}
	return score
}

// cancelExchangesForClient aborts any pending requester-owned exchange when the
// requester disconnects, and removes the client from responder sets otherwise.
func (h *Hub) cancelExchangesForClient(c *Client) {
	if c == nil {
		return
	}
	h.exchangesMu.Lock()
	affected := make([]pendingExchange, 0)
	abortedIDs := make([]string, 0)
	for id, pending := range h.exchanges {
		if pending.requester == c {
			affected = append(affected, pending)
			abortedIDs = append(abortedIDs, id)
			continue
		}
		if pending.responders[c] {
			delete(pending.responders, c)
			if len(pending.responders) == 0 {
				affected = append(affected, pending)
				abortedIDs = append(abortedIDs, id)
			} else {
				h.exchanges[id] = pending
			}
		}
	}
	for _, id := range abortedIDs {
		delete(h.exchanges, id)
	}
	h.exchangesMu.Unlock()

	h.removeSuspendedExchangeResponder(c)

	for _, pending := range affected {
		if pending.requester != c && pending.requester != nil && pending.requester.IsConnected() {
			pending.requester.SendMsg(&Message{
				Type:  string(MsgError),
				Topic: pending.topic,
				Text:  fmt.Sprintf("exchange %s aborted: responder disconnected", pending.topic),
			})
		}
	}
}
