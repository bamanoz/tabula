package kernel

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type pendingExchange struct {
	requester  *Client
	responders map[*Client]bool
	tenantID   string
	session    string
	topic      string
}

type pendingApprovalExchange struct {
	responders map[*Client]bool
	tenantID   string
	session    string
	approvalID string
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
	if h.handleSuspendedToolExchangeReply(sender, msg) {
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

func (h *Hub) requestApprovalForPendingTool(pending pendingToolCall, blocked *HookDispatchDecision) {
	if h == nil {
		return
	}
	topic := pending.ExchangeTopic
	if topic == "" {
		topic = TopicExchangeApprove
	}
	responders := h.pickExchangeResponders(nil, pending.TenantID, pending.Session, topic)
	if len(responders) == 0 {
		h.Logger.Warn("no approval responder for suspended tool", "approval_id", pending.ApprovalID, "tool", pending.ToolName, "tool_call_id", pending.ToolID, "tenant_id", pending.TenantID, "session", pending.Session)
		h.tools.resolvePendingApproval(pending.ApprovalID, false, "no responder")
		return
	}
	h.approvalMu.Lock()
	h.approvalExchanges[pending.ApprovalID] = pendingApprovalExchange{responders: clientSet(responders), tenantID: pending.TenantID, session: pending.Session, approvalID: pending.ApprovalID}
	h.approvalMu.Unlock()
	data := approvalRequestData(pending, blocked)
	for _, responder := range responders {
		responder.SendMsg(h.prepareRoutedMessage(nil, pending.Session, "approval", &Message{Type: string(MsgRequest), Topic: topic, ID: pending.ApprovalID, Session: pending.Session, TenantID: pending.TenantID, Data: data}))
	}
}

func (h *Hub) resendPendingApprovals(c *Client, tenantID, session string) {
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
		h.approvalMu.Lock()
		approval := h.approvalExchanges[pending.ApprovalID]
		if approval.responders == nil {
			approval.responders = map[*Client]bool{}
		}
		approval.responders[c] = true
		approval.tenantID = tenantID
		approval.session = session
		approval.approvalID = pending.ApprovalID
		h.approvalExchanges[pending.ApprovalID] = approval
		h.approvalMu.Unlock()
		c.SendMsg(h.prepareRoutedMessage(nil, session, "approval", &Message{Type: string(MsgRequest), Topic: topic, ID: pending.ApprovalID, Session: session, TenantID: tenantID, Data: approvalRequestData(pending, nil)}))
	}
}

func (h *Hub) handleSuspendedToolExchangeReply(sender *Client, msg *Message) bool {
	h.approvalMu.Lock()
	pending, ok := h.approvalExchanges[msg.ID]
	if ok && pending.responders[sender] {
		delete(h.approvalExchanges, msg.ID)
	}
	h.approvalMu.Unlock()
	if !ok {
		return false
	}
	if !pending.responders[sender] {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "client not allowed to answer approval"})
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
	choice := approvalChoice(msg.Data)
	approved := true
	if msg.Topic == TopicExchangeApprove {
		approved = choice == "allow once" || choice == "allow always"
		if choice == "" {
			approved = false
			choice = "deny once"
		}
	}
	h.tools.setPendingExchangeReply(pending.approvalID, msg.Data)
	h.tools.resolvePendingApproval(pending.approvalID, approved, choice)
	h.broadcastExchangeResolved(pending.responders, sender, pending.tenantID, pending.session, msg.Topic, msg.ID, msg.Data)
	return true
}

func approvalRequestData(pending pendingToolCall, blocked *HookDispatchDecision) json.RawMessage {
	if len(pending.ApprovalData) > 0 && blocked == nil {
		var data map[string]any
		if json.Unmarshal(pending.ApprovalData, &data) == nil {
			data["approval_id"] = pending.ApprovalID
			return mustMarshalRaw(data)
		}
		return append(json.RawMessage(nil), pending.ApprovalData...)
	}
	data := map[string]any{
		"approval_id": pending.ApprovalID,
		"question":    fmt.Sprintf("Approve tool %s?", pending.ToolName),
		"details": map[string]any{
			"tool":         pending.ToolName,
			"tool_call_id": pending.ToolID,
			"input":        json.RawMessage(pending.Input),
		},
		"options": []string{"allow once", "allow always", "deny once", "deny always"},
	}
	if blocked != nil {
		if blocked.Reason != "" {
			data["question"] = blocked.Reason
		}
		if len(blocked.Payload) > 0 {
			var payload map[string]any
			if json.Unmarshal(blocked.Payload, &payload) == nil {
				for _, key := range []string{"question", "options"} {
					if value, ok := payload[key]; ok {
						data[key] = value
					}
				}
			}
		}
	}
	return mustMarshalRaw(data)
}

func approvalRequestDataFromDecision(toolName string, blocked *HookDispatchDecision) json.RawMessage {
	if blocked == nil || len(blocked.Payload) == 0 {
		return nil
	}
	var payload map[string]any
	if json.Unmarshal(blocked.Payload, &payload) != nil {
		return nil
	}
	data := map[string]any{}
	if question, ok := payload["question"]; ok {
		data["question"] = question
	} else if blocked.Reason != "" {
		data["question"] = blocked.Reason
	} else {
		data["question"] = fmt.Sprintf("Approve tool %s?", toolName)
	}
	for _, key := range []string{"details", "options", "questions", "kind"} {
		if value, ok := payload[key]; ok {
			data[key] = value
		}
	}
	if len(data) == 0 {
		return nil
	}
	return mustMarshalRaw(data)
}

func approvalChoice(raw json.RawMessage) string {
	var body struct {
		Choice string `json:"choice"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return strings.TrimSpace(body.Choice)
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

	h.approvalMu.Lock()
	for id, approval := range h.approvalExchanges {
		if approval.responders[c] {
			delete(approval.responders, c)
		}
		if len(approval.responders) == 0 {
			delete(h.approvalExchanges, id)
		} else {
			h.approvalExchanges[id] = approval
		}
	}
	h.approvalMu.Unlock()

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
