package kernel

import (
	"encoding/json"
	"fmt"
	"sort"
)

type pendingExchange struct {
	requester *Client
	responder *Client
	tenantID  string
	session   string
	topic     string
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
	var meta struct {
		Role    string `json:"tabula.client_role"`
		Managed bool   `json:"tabula.managed"`
	}
	if err := json.Unmarshal(c.meta, &meta); err != nil {
		return 0
	}
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
