package kernel

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

type pendingExchange struct {
	requester *Client
	responder *Client
	tenantID  string
	session   string
	topic     string
}

type pendingApprovalExchange struct {
	responder  *Client
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
	responder := h.pickExchangeResponder(sender, tenantID, session, msg.Topic)
	if responder == nil {
		sender.SendMsg(&Message{Type: string(MsgError), Text: fmt.Sprintf("no responder for %s", msg.Topic)})
		return
	}
	h.exchangesMu.Lock()
	h.exchanges[msg.ID] = pendingExchange{requester: sender, responder: responder, tenantID: tenantID, session: session, topic: msg.Topic}
	h.exchangesMu.Unlock()
	msg.TenantID = tenantID
	responder.SendMsg(h.prepareRoutedMessage(sender, session, "exchange", msg))
}

func (h *Hub) handleExchangeReply(sender *Client, msg *Message) {
	if msg.Topic == TopicExchangeApprove && h.handleApprovalExchangeReply(sender, msg) {
		return
	}
	h.exchangesMu.Lock()
	pending, ok := h.exchanges[msg.ID]
	if ok && pending.responder == sender && pending.topic == msg.Topic {
		delete(h.exchanges, msg.ID)
	}
	h.exchangesMu.Unlock()
	if !ok || pending.responder != sender || pending.topic != msg.Topic {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "client not allowed to answer exchange"})
		return
	}
	if pending.requester == nil || !pending.requester.IsConnected() {
		return
	}
	msg.TenantID = pending.tenantID
	pending.requester.SendMsg(h.prepareRoutedMessage(sender, pending.session, "exchange", msg))
}

func (h *Hub) requestApprovalForPendingTool(pending pendingToolCall, blocked *HookDispatchDecision) {
	if h == nil {
		return
	}
	responder := h.pickExchangeResponder(nil, pending.TenantID, pending.Session, TopicExchangeApprove)
	if responder == nil {
		h.Logger.Warn("no approval responder for suspended tool", "approval_id", pending.ApprovalID, "tool", pending.ToolName, "tool_call_id", pending.ToolID, "tenant_id", pending.TenantID, "session", pending.Session)
		h.tools.resolvePendingApproval(pending.ApprovalID, false, "no responder")
		return
	}
	h.approvalMu.Lock()
	h.approvalExchanges[pending.ApprovalID] = pendingApprovalExchange{responder: responder, tenantID: pending.TenantID, session: pending.Session, approvalID: pending.ApprovalID}
	h.approvalMu.Unlock()
	data := approvalRequestData(pending, blocked)
	responder.SendMsg(h.prepareRoutedMessage(nil, pending.Session, "approval", &Message{Type: string(MsgRequest), Topic: TopicExchangeApprove, ID: pending.ApprovalID, Session: pending.Session, TenantID: pending.TenantID, Data: data}))
}

func (h *Hub) resendPendingApprovals(c *Client, tenantID, session string) {
	if h == nil || c == nil || !c.canReceive(TopicExchangeApprove) || !c.canSend(TopicExchangeApprove) {
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
		h.approvalMu.Lock()
		h.approvalExchanges[pending.ApprovalID] = pendingApprovalExchange{responder: c, tenantID: tenantID, session: session, approvalID: pending.ApprovalID}
		h.approvalMu.Unlock()
		c.SendMsg(h.prepareRoutedMessage(nil, session, "approval", &Message{Type: string(MsgRequest), Topic: TopicExchangeApprove, ID: pending.ApprovalID, Session: session, TenantID: tenantID, Data: approvalRequestData(pending, nil)}))
	}
}

func (h *Hub) handleApprovalExchangeReply(sender *Client, msg *Message) bool {
	h.approvalMu.Lock()
	pending, ok := h.approvalExchanges[msg.ID]
	if ok && pending.responder == sender {
		delete(h.approvalExchanges, msg.ID)
	}
	h.approvalMu.Unlock()
	if !ok {
		return false
	}
	if pending.responder != sender {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "client not allowed to answer approval"})
		return true
	}
	choice := approvalChoice(msg.Data)
	approved := choice == "allow once" || choice == "allow always"
	if choice == "" {
		approved = false
		choice = "deny once"
	}
	h.tools.resolvePendingApproval(pending.approvalID, approved, choice)
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
	for _, key := range []string{"details", "options"} {
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

func (h *Hub) pickExchangeResponder(sender *Client, tenantID, session string, topic string) *Client {
	if responder := pickPreferredExchangeResponder(h.sessionClients(tenantID, session), func(c *Client) bool {
		return c != sender && c.IsConnected() && c.canReceive(topic) && c.canSend(topic)
	}); responder != nil {
		return responder
	}
	return pickPreferredExchangeResponder(h.allClients(), func(c *Client) bool {
		if c == sender || !c.IsConnected() || !c.canReceiveGlobal(topic) || !c.canSend(topic) {
			return false
		}
		return c.tenantID == "" || c.tenantID == tenantID
	})
}

func pickPreferredExchangeResponder(clients []*Client, eligible func(*Client) bool) *Client {
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
	return candidates[0]
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

// cancelExchangesForClient aborts any pending exchanges that involve the
// given client (as requester or responder) and notifies the surviving peer
// with an error so that hooks/clients waiting on a reply do not hang forever
// when their counterpart disconnects.
func (h *Hub) cancelExchangesForClient(c *Client) {
	if c == nil {
		return
	}
	h.exchangesMu.Lock()
	affected := make([]pendingExchange, 0)
	abortedIDs := make([]string, 0)
	for id, pending := range h.exchanges {
		if pending.requester == c || pending.responder == c {
			affected = append(affected, pending)
			abortedIDs = append(abortedIDs, id)
		}
	}
	for _, id := range abortedIDs {
		delete(h.exchanges, id)
	}
	h.exchangesMu.Unlock()

	h.approvalMu.Lock()
	for id, approval := range h.approvalExchanges {
		if approval.responder == c {
			delete(h.approvalExchanges, id)
		}
	}
	h.approvalMu.Unlock()

	for _, pending := range affected {
		if pending.responder == c && pending.requester != nil && pending.requester.IsConnected() {
			pending.requester.SendMsg(&Message{
				Type:  string(MsgError),
				Topic: pending.topic,
				Text:  fmt.Sprintf("exchange %s aborted: responder disconnected", pending.topic),
			})
		}
	}
}
