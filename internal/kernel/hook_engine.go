package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"
)

type hookStrategy string

const (
	strategyVoid      hookStrategy = "void"
	strategyModifying hookStrategy = "modifying"
	strategyClaiming  hookStrategy = "claiming"
)

const hookTimeout = 5 * time.Second


type hookEntry struct {
	client *Client
	sub    HookSubscription
}

type HookResult struct {
	Action  string
	Payload json.RawMessage
	Reason  string
}

type HookEngine struct {
	mu      sync.RWMutex
	index   map[string][]hookEntry
	pending map[string]chan *HookResult
	logger  *slog.Logger
}

func NewHookEngine(logger *slog.Logger) *HookEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &HookEngine{
		index:   make(map[string][]hookEntry),
		pending: make(map[string]chan *HookResult),
		logger:  logger,
	}
}

func (e *HookEngine) RebuildIndex(clients []*Client) {
	idx := make(map[string][]hookEntry)
	for _, c := range clients {
		if !c.IsConnected() {
			continue
		}
		for _, sub := range c.hooks {
			idx[sub.Event] = append(idx[sub.Event], hookEntry{client: c, sub: sub})
		}
	}
	for event := range idx {
		sort.Slice(idx[event], func(i, j int) bool {
			return idx[event][i].sub.Priority > idx[event][j].sub.Priority
		})
	}

	e.mu.Lock()
	e.index = idx
	e.mu.Unlock()
}

func (e *HookEngine) Dispatch(event string, payload json.RawMessage, session string) (json.RawMessage, bool) {
	entries := e.entries(event)
	if len(entries) == 0 {
		return payload, true
	}

	var relevant []hookEntry
	for _, entry := range entries {
		if entry.client.session == session || entry.client.session == "" {
			relevant = append(relevant, entry)
		}
	}
	if len(relevant) == 0 {
		return payload, true
	}

	def, ok := HookEvents[event]
	if !ok {
		return payload, true
	}

	switch def.Strategy {
	case strategyVoid:
		e.dispatchVoid(event, payload, relevant)
		return payload, true
	case strategyModifying:
		return e.dispatchModifying(event, payload, relevant, def.Type == HookSecurity)
	case strategyClaiming:
		return e.dispatchClaiming(event, payload, relevant)
	default:
		return payload, true
	}
}

func (e *HookEngine) HandleResult(msg *Message) {
	ch, ok := e.pendingHook(msg.ID)
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

func (e *HookEngine) dispatchVoid(event string, payload json.RawMessage, entries []hookEntry) {
	for _, entry := range entries {
		entry.client.SendMsg(&Message{
			Type:    "hook",
			ID:      generateHookID(),
			Name:    event,
			Payload: payload,
		})
	}
}

func (e *HookEngine) dispatchModifying(event string, payload json.RawMessage, entries []hookEntry, secure bool) (json.RawMessage, bool) {
	current := payload
	for _, entry := range entries {
		result := e.sendAndWait(entry.client, event, current)
		if result == nil {
			if secure {
				e.logger.Info("security hook timeout, blocking", "event", event, "client", entry.client.name)
				return nil, false
			}
			continue
		}
		switch HookAction(result.Action) {
		case ActionBlock:
			e.logger.Info("hook blocked event", "event", event, "client", entry.client.name, "reason", result.Reason)
			return nil, false
		case ActionModify:
			if result.Payload != nil {
				current = result.Payload
			}
		}
	}
	return current, true
}

func (e *HookEngine) dispatchClaiming(event string, payload json.RawMessage, entries []hookEntry) (json.RawMessage, bool) {
	for _, entry := range entries {
		result := e.sendAndWait(entry.client, event, payload)
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

func (e *HookEngine) sendAndWait(c *Client, event string, payload json.RawMessage) *HookResult {
	id := generateHookID()
	ch := make(chan *HookResult, 1)
	e.addPendingHook(id, ch)

	c.SendMsg(&Message{
		Type:    "hook",
		ID:      id,
		Name:    event,
		Payload: payload,
	})

	var result *HookResult
	select {
	case result = <-ch:
	case <-time.After(hookTimeout):
		e.logger.Warn("hook timeout", "event", event, "client", c.name, "id", id)
	}

	e.removePendingHook(id)
	return result
}

func (e *HookEngine) entries(event string) []hookEntry {
	e.mu.RLock()
	defer e.mu.RUnlock()
	entries := e.index[event]
	if len(entries) == 0 {
		return nil
	}
	out := make([]hookEntry, len(entries))
	copy(out, entries)
	return out
}

func (e *HookEngine) addPendingHook(id string, ch chan *HookResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pending[id] = ch
}

func (e *HookEngine) removePendingHook(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.pending, id)
}

func (e *HookEngine) pendingHook(id string) (chan *HookResult, bool) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	ch, ok := e.pending[id]
	return ch, ok
}

func generateHookID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return "h-" + hex.EncodeToString(b)
}
