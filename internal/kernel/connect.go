package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"
)

func (h *Hub) handleConnect(c *Client, msg *Message) {
	c.name = msg.Name
	c.sends = make(map[string]bool)
	for _, s := range msg.Sends {
		c.sends[s] = true
	}
	c.receives = make(map[string]bool)
	for _, r := range msg.Receives {
		c.receives[r] = true
	}
	c.id = h.nextClientID
	// Resolve depth from spawn token (one-time use)
	if msg.Token != "" {
		if entry, ok := h.spawnTokens[msg.Token]; ok {
			c.depth = entry.depth
			delete(h.spawnTokens, msg.Token)
		} else {
			h.Logger.Warn("invalid spawn token", "from", msg.Name)
		}
	}
	h.nextClientID++
	c.connected = true
	// Hook subscriptions
	c.hooks = msg.Hooks
	h.rebuildHookIndex()

	resp := &Message{
		Type: "connected",
		ID:   fmt.Sprintf("c%d", c.id),
	}
	c.SendMsg(resp)
	h.Logger.Info("client connected", "name", c.name, "id", c.id)
}

func (h *Hub) handleJoin(c *Client, msg *Message) {
	c.session = msg.Session
	resp := &Message{
		Type:    "joined",
		Session: c.session,
	}
	c.SendMsg(resp)
	h.Logger.Info("client joined session", "name", c.name, "session", c.session)

	// Notify other clients in the session
	joinedNotify := &Message{
		Type:    "member_joined",
		Name:    c.name,
		Session: c.session,
	}
	h.broadcastToSession(c.session, "member_joined", joinedNotify, c)

	// Fire session_start hook (modifying) before sending init.
	// Hooks can inject extra context via payload.context field.
	hookPayload, _ := json.Marshal(map[string]string{
		"session": c.session,
		"client":  c.name,
	})
	result, _ := h.dispatchHook("session_start", hookPayload, c.session)

	if c.canReceive("init") {
		prompt := h.systemPrompt
		// Apply context injection from session_start hooks.
		var hookData struct{ Context string }
		if json.Unmarshal(result, &hookData) == nil && hookData.Context != "" {
			prompt = prompt + "\n\n" + hookData.Context
		}
		h.sendInitWithPrompt(c, prompt)
	}
}

func (h *Hub) sendInitWithPrompt(c *Client, prompt string) {
	resp := &Message{
		Type:   "init",
		Prompt: prompt,
		Tools:  h.toolsJSON,
	}
	c.SendMsg(resp)
	h.Logger.Debug("sent init", "client", c.name)
}

func (h *Hub) handleCancel(session string) {
	// Broadcast cancel to session members (e.g. driver can abort LLM streaming)
	h.broadcastToSession(session, "cancel", &Message{Type: "cancel"}, nil)
	// Also SIGINT spawned processes in this session
	for pid, proc := range h.spawned {
		if proc.Alive && proc.Session == session {
			h.Logger.Debug("sending SIGINT", "pid", pid)
			proc.Signal()
		}
	}
}

func (h *Hub) generateSpawnToken(childDepth int) (string, error) {
	// Prune expired tokens
	now := time.Now()
	for k, v := range h.spawnTokens {
		if now.Sub(v.createdAt) > spawnTokenTTL {
			delete(h.spawnTokens, k)
		}
	}

	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate spawn token: %w", err)
	}
	token := hex.EncodeToString(b)
	h.spawnTokens[token] = spawnTokenEntry{depth: childDepth, createdAt: now}
	return token, nil
}
