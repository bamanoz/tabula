package kernel

import (
	"fmt"
	"strings"
	"time"
)

// OneShotConfig holds parameters for a single prompt-response exchange.
type OneShotConfig struct {
	Prompt  string
	Session string
	Timeout time.Duration
}

// RunOneShot starts the kernel, sends a single prompt, collects the response,
// and shuts down. It uses an internal client (no WebSocket) to drive the exchange.
func (h *Hub) RunOneShot(cfg OneShotConfig) (string, error) {
	if cfg.Session == "" {
		cfg.Session = "oneshot"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 120 * time.Second
	}

	// Create internal client with receive channel.
	recvCh := make(chan *Message, 256)
	c := &Client{
		hub:    h,
		sendCh: make(chan []byte, sendBufSize),
		recvCh: recvCh,
		state:  ClientSocketConnected,
	}
	if !h.Register(c) {
		return "", fmt.Errorf("hub at capacity")
	}

	// Connect.
	connectMsg := &Message{
		Type:     string(MsgConnect),
		Name:     "oneshot",
		Sends:    []string{string(MsgMessage), string(MsgDone)},
		Receives: []string{string(MsgMessage), string(MsgStreamStart), string(MsgStreamDelta), string(MsgStreamEnd), string(MsgDone), string(MsgError)},
	}
	plan := h.buildConnectPlan(c, connectMsg)
	h.applyConnectPlan(c, plan)
	if plan.errorMsg != "" {
		return "", fmt.Errorf("connect failed: %s", plan.errorMsg)
	}

	// Join session.
	joinPlan := h.buildJoinPlan(c, cfg.Session)
	h.applyJoinPlan(c, joinPlan)
	if joinPlan.blockedReason != "" {
		return "", fmt.Errorf("join blocked: %s", joinPlan.blockedReason)
	}

	// Drain any non-message responses (joined, member_joined, init).
	deadline := time.Now().Add(cfg.Timeout)
	drainDeadline := time.Now().Add(2 * time.Second)
drainLoop:
	for time.Now().Before(drainDeadline) {
		select {
		case msg := <-recvCh:
			if msg == nil {
				return "", fmt.Errorf("internal client disconnected")
			}
			// Stop draining once we see a message-related type from the driver.
			if msg.Type == string(MsgMessage) || msg.Type == string(MsgStreamStart) ||
				msg.Type == string(MsgStreamDelta) || msg.Type == string(MsgDone) ||
				msg.Type == string(MsgError) {
				// Process this message below.
				var sb strings.Builder
				return h.collectResponse(recvCh, msg, &sb, deadline)
			}
		case <-time.After(100 * time.Millisecond):
			break drainLoop
		}
	}

	// Send the prompt.
	h.HandleMessage(c, &Message{
		Type:    string(MsgMessage),
		Text:    cfg.Prompt,
		Session: cfg.Session,
	})

	// Collect response.
	var sb strings.Builder
	return h.collectResponse(recvCh, nil, &sb, deadline)
}

// collectResponse reads from recvCh, accumulating text until done/error/timeout.
func (h *Hub) collectResponse(recvCh chan *Message, first *Message, sb *strings.Builder, deadline time.Time) (string, error) {
	msg := first
	for {
		if msg == nil {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return sb.String(), fmt.Errorf("timeout waiting for response")
			}
			select {
			case m := <-recvCh:
				if m == nil {
					return sb.String(), fmt.Errorf("internal client disconnected")
				}
				msg = m
			case <-time.After(remaining):
				return sb.String(), fmt.Errorf("timeout waiting for response")
			}
		}

		switch msg.Type {
		case string(MsgStreamDelta):
			if msg.Text != "" {
				sb.WriteString(msg.Text)
			}
		case string(MsgMessage):
			if msg.Text != "" {
				if sb.Len() > 0 {
					sb.WriteString("\n")
				}
				sb.WriteString(msg.Text)
			}
		case string(MsgDone):
			return strings.TrimSpace(sb.String()), nil
		case string(MsgError):
			return "", fmt.Errorf("driver error: %s", msg.Text)
		}

		msg = nil
	}
}
