package kernel

import "fmt"

type pendingExchange struct {
	requester *Client
	responder *Client
	session   string
	topic     string
}

func (h *Hub) handleExchangeRequest(sender *Client, msg *Message) {
	if msg.ID == "" {
		sender.SendMsg(&Message{Type: string(MsgError), Text: "exchange request missing id"})
		return
	}
	session := h.targetSession(sender, msg)
	responder := h.pickExchangeResponder(sender, session, msg.Topic)
	if responder == nil {
		sender.SendMsg(&Message{Type: string(MsgError), Text: fmt.Sprintf("no responder for %s", msg.Topic)})
		return
	}
	h.exchangesMu.Lock()
	h.exchanges[msg.ID] = pendingExchange{requester: sender, responder: responder, session: session, topic: msg.Topic}
	h.exchangesMu.Unlock()
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
	pending.requester.SendMsg(h.prepareRoutedMessage(sender, pending.session, "exchange", msg))
}

func (h *Hub) pickExchangeResponder(sender *Client, session string, topic string) *Client {
	for _, c := range h.sessionClients(session) {
		if c == sender || !c.IsConnected() || !c.canReceive(topic) || !c.canSend(topic) {
			continue
		}
		return c
	}
	for _, c := range h.allClients() {
		if c == sender || !c.IsConnected() || !c.canReceiveGlobal(topic) || !c.canSend(topic) {
			continue
		}
		return c
	}
	return nil
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
