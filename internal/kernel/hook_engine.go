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
	return e.DispatchExcept(event, payload, session, nil)
}

func (e *HookEngine) DispatchExcept(event string, payload json.RawMessage, session string, exclude *Client) (json.RawMessage, bool) {
	entries := e.entries(event)
	if len(entries) == 0 {
		return payload, true
	}

	// Inject session into payload so observers can correlate events.
	payload = e.injectSession(payload, session)

	var relevant []hookEntry
	for _, entry := range entries {
		if exclude != nil && entry.client == exclude {
			continue
		}
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
		result := e.sendAndWait(entry, event, current)
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
		result := e.sendAndWait(entry, event, payload)
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

func (e *HookEngine) sendAndWait(entry hookEntry, event string, payload json.RawMessage) *HookResult {
	c := entry.client
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
	if entry.sub.TimeoutMs != nil && *entry.sub.TimeoutMs == 0 {
		// Wait indefinitely; only the subscriber disconnecting unblocks us.
		select {
		case result = <-ch:
		case <-c.Done():
			e.logger.Info("hook subscriber disconnected", "event", event, "client", c.name, "id", id)
		}
	} else {
		d := hookTimeout
		if entry.sub.TimeoutMs != nil && *entry.sub.TimeoutMs > 0 {
			d = time.Duration(*entry.sub.TimeoutMs) * time.Millisecond
		}
		select {
		case result = <-ch:
		case <-c.Done():
			e.logger.Info("hook subscriber disconnected", "event", event, "client", c.name, "id", id)
		case <-time.After(d):
			e.logger.Warn("hook timeout", "event", event, "client", c.name, "id", id)
		}
	}

	e.removePendingHook(id)
	return result
}

func (e *HookEngine) injectSession(payload json.RawMessage, session string) json.RawMessage {
	if session == "" {
		return payload
	}
	var m map[string]interface{}
	if json.Unmarshal(payload, &m) != nil {
		return payload
	}
	if _, ok := m["session"]; !ok {
		m["session"] = session
		out, _ := json.Marshal(m)
		return out
	}
	return payload
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
