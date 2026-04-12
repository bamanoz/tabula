package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"sort"
	"time"
)

// HookSubscription declares a client's interest in a hook event.
type HookSubscription struct {
	Event    string `json:"event"`
	Priority int    `json:"priority"`
}

// hookEntry pairs a client with its subscription for index lookup.
type hookEntry struct {
	client *Client
	sub    HookSubscription
}

// HookResult is the parsed response from a hook subscriber.
type HookResult struct {
	Action  string          // "pass", "modify", "block", "claim"
	Payload json.RawMessage // modified payload (for modify/claim)
	Reason  string          // explanation (for block)
}

// hookStrategy defines how an event's hooks execute.
type hookStrategy string

const (
	strategyVoid      hookStrategy = "void"
	strategyModifying hookStrategy = "modifying"
	strategyClaiming  hookStrategy = "claiming"
)

const hookTimeout = 5 * time.Second

// eventStrategies maps event names to their execution strategy.
var eventStrategies = map[string]hookStrategy{
	"before_message":   strategyModifying,
	"after_message":    strategyVoid,
	"before_tool_call": strategyModifying,
	"after_tool_call":  strategyVoid,
	"session_start":    strategyModifying,
	"session_end":      strategyVoid,
	"cancel":           strategyVoid,
	"before_spawn":     strategyModifying,
	"after_spawn":      strategyVoid,
}

// rebuildHookIndex rebuilds the hookIndex from all connected clients.
// Caller must hold h.mu.
func (h *Hub) rebuildHookIndex() {
	idx := make(map[string][]hookEntry)
	for c := range h.clients {
		if !c.connected {
			continue
		}
		for _, sub := range c.hooks {
			idx[sub.Event] = append(idx[sub.Event], hookEntry{client: c, sub: sub})
		}
	}
	// Sort each event's entries by priority descending (higher = first).
	for event := range idx {
		sort.Slice(idx[event], func(i, j int) bool {
			return idx[event][i].sub.Priority > idx[event][j].sub.Priority
		})
	}
	h.hookIndex = idx
}

// dispatchHook dispatches a hook event and returns the (possibly modified) payload.
// Returns (payload, true) on success, (nil, false) if blocked.
// Caller must hold h.mu. For modifying/claiming, mu is released during wait.
func (h *Hub) dispatchHook(event string, payload json.RawMessage, session string) (json.RawMessage, bool) {
	entries := h.hookIndex[event]
	if len(entries) == 0 {
		return payload, true
	}

	// Filter to subscribers in this session or global (session == "").
	var relevant []hookEntry
	for _, e := range entries {
		if e.client.session == session || e.client.session == "" {
			relevant = append(relevant, e)
		}
	}
	if len(relevant) == 0 {
		return payload, true
	}

	strategy := eventStrategies[event]

	switch strategy {
	case strategyVoid:
		h.dispatchVoid(event, payload, relevant)
		return payload, true

	case strategyModifying:
		return h.dispatchModifying(event, payload, relevant)

	case strategyClaiming:
		return h.dispatchClaiming(event, payload, relevant)
	}

	return payload, true
}

// dispatchVoid sends the hook to all subscribers without waiting.
// Caller must hold h.mu.
func (h *Hub) dispatchVoid(event string, payload json.RawMessage, entries []hookEntry) {
	for _, e := range entries {
		id := generateHookID()
		msg := &Message{
			Type:    "hook",
			ID:      id,
			Name:    event,
			Payload: payload,
		}
		e.client.SendMsg(msg)
	}
}

// dispatchModifying sends hooks sequentially, waiting for each response.
// Each subscriber can modify the payload or block. Mutex is released during waits.
// Caller must hold h.mu.
func (h *Hub) dispatchModifying(event string, payload json.RawMessage, entries []hookEntry) (json.RawMessage, bool) {
	current := payload
	for _, e := range entries {
		result := h.sendAndWait(e.client, event, current)
		if result == nil {
			continue // timeout — treat as pass
		}
		switch result.Action {
		case "block":
			h.Logger.Info("hook blocked event", "event", event, "client", e.client.name, "reason", result.Reason)
			return nil, false
		case "modify":
			if result.Payload != nil {
				current = result.Payload
			}
		}
		// "pass" or unknown — continue with current payload
	}
	return current, true
}

// dispatchClaiming sends hooks sequentially; first "claim" wins.
// Caller must hold h.mu.
func (h *Hub) dispatchClaiming(event string, payload json.RawMessage, entries []hookEntry) (json.RawMessage, bool) {
	for _, e := range entries {
		result := h.sendAndWait(e.client, event, payload)
		if result == nil {
			continue
		}
		if result.Action == "claim" {
			if result.Payload != nil {
				return result.Payload, true
			}
			return payload, true
		}
	}
	return payload, true
}

// sendAndWait sends a hook to a client, releases the mutex, waits for response
// with timeout, then re-acquires the mutex. Returns nil on timeout.
// Caller must hold h.mu; on return, h.mu is held again.
func (h *Hub) sendAndWait(c *Client, event string, payload json.RawMessage) *HookResult {
	id := generateHookID()
	ch := make(chan *HookResult, 1)
	h.pendingHooks[id] = ch

	msg := &Message{
		Type:    "hook",
		ID:      id,
		Name:    event,
		Payload: payload,
	}
	c.SendMsg(msg)

	h.mu.Unlock()

	var result *HookResult
	select {
	case result = <-ch:
	case <-time.After(hookTimeout):
		h.Logger.Warn("hook timeout", "event", event, "client", c.name, "id", id)
	}

	h.mu.Lock()
	delete(h.pendingHooks, id)
	return result
}

// handleHookResult routes a hook_result to the pending channel.
// Caller must hold h.mu.
func (h *Hub) handleHookResult(msg *Message) {
	ch, ok := h.pendingHooks[msg.ID]
	if !ok {
		return
	}
	result := &HookResult{
		Action:  msg.Action,
		Payload: msg.Payload,
		Reason:  msg.Reason,
	}
	select {
	case ch <- result:
	default:
	}
}

func generateHookID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "h-" + hex.EncodeToString(b)
}
