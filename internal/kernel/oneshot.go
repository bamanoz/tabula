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
		done:   make(chan struct{}),
	}
	if !h.Register(c) {
		return "", fmt.Errorf("hub at capacity")
	}

	// Connect.
	connectMsg := &Message{
		V:    ProtocolVersion,
		Type: string(MsgHello),
		Data: mustMarshalRaw(map[string]any{
			"name":           "oneshot",
			"send_topics":    []string{TopicMessageUser, TopicTurnDone},
			"receive_topics": []string{TopicMessageUser, TopicStreamStart, TopicStreamDelta, TopicStreamEnd, TopicTurnDone, string(MsgError)},
			"auth_token":     h.clientAuthToken,
		}),
	}
	plan := h.buildConnectPlan(c, connectMsg)
	h.applyConnectPlan(c, plan)
	if plan.errorMsg != "" {
		return "", fmt.Errorf("connect failed: %s", plan.errorMsg)
	}

	// Join session.
	joinPlan := h.buildJoinPlan(c, cfg.Session, "")
	h.applyJoinPlan(c, joinPlan)
	if joinPlan.blockedReason != "" {
		return "", fmt.Errorf("join blocked: %s", joinPlan.blockedReason)
	}

	// Send the prompt.
	deadline := time.Now().Add(cfg.Timeout)
	h.Logger.Info("oneshot: sending prompt", "session", cfg.Session, "prompt_len", len(cfg.Prompt))
	h.HandleMessage(c, &Message{
		Type:    string(MsgEvent),
		Topic:   TopicMessageUser,
		Data:    mustMarshalRaw(map[string]any{"text": cfg.Prompt}),
		Session: cfg.Session,
	})
	h.Logger.Info("oneshot: prompt dispatched, waiting for response")

	// Collect response — skips non-response messages (joined, init, etc).
	var sb strings.Builder
	return h.collectResponse(recvCh, nil, &sb, deadline)
}

// collectResponse reads from recvCh, accumulating text until done/error/timeout.
// Messages that aren't stream.delta/message.user/done/error are silently skipped.
func (h *Hub) collectResponse(recvCh chan *Message, first *Message, sb *strings.Builder, deadline time.Time) (string, error) {
	msg := first
	for {
		if msg == nil {
			remaining := time.Until(deadline)
			if remaining <= 0 {
				return sb.String(), fmt.Errorf("timeout waiting for response")
			}
			timer := time.NewTimer(remaining)
			select {
			case m := <-recvCh:
				timer.Stop()
				if m == nil {
					return sb.String(), fmt.Errorf("internal client disconnected")
				}
				msg = m
			case <-timer.C:
				return sb.String(), fmt.Errorf("timeout waiting for response")
			}
		}

		switch msg.Type {
		case string(MsgEvent):
			switch msg.Topic {
			case TopicStreamDelta:
				if eventText(msg) != "" {
					sb.WriteString(eventText(msg))
				}
			case TopicMessageUser:
				if messageText(msg) != "" {
					if sb.Len() > 0 {
						sb.WriteString("\n")
					}
					sb.WriteString(messageText(msg))
				}
			case TopicTurnDone:
				return strings.TrimSpace(sb.String()), nil
			}
		case string(MsgError):
			return "", fmt.Errorf("driver error: %s", msg.Text)
		default:
			// Skip protocol messages (joined, member_joined, init, etc).
		}

		msg = nil
	}
}
