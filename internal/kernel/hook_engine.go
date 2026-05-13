package kernel

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
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
	sub      HookSubscriber
	subscrip HookSubscription
}

type HookResult struct {
	Action  string
	Payload json.RawMessage
	Reason  string
}

type HookEngine struct {
	mu             sync.RWMutex
	index          map[string][]hookEntry
	pending        map[string]chan *HookResult
	logger         *slog.Logger
	sessionTenants func(string) string
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

func (e *HookEngine) SetSessionTenantResolver(resolve func(string) string) {
	if e == nil {
		return
	}
	e.sessionTenants = resolve
}

// RebuildIndex reconstructs the hook index from the given subscribers
// (typically the union of WebSocket clients and registered plugins).
func (e *HookEngine) RebuildIndex(subs []HookSubscriber) {
	idx := make(map[string][]hookEntry)
	for _, s := range subs {
		if !s.IsConnected() {
			continue
		}
		for _, sub := range s.Hooks() {
			idx[sub.Event] = append(idx[sub.Event], hookEntry{sub: s, subscrip: sub})
		}
	}
	for event := range idx {
		sort.Slice(idx[event], func(i, j int) bool {
			return idx[event][i].subscrip.Priority > idx[event][j].subscrip.Priority
		})
	}

	e.mu.Lock()
	e.index = idx
	e.mu.Unlock()
}

func (e *HookEngine) Dispatch(event string, payload json.RawMessage, session string) (json.RawMessage, bool) {
	return e.DispatchExcept(event, payload, session, nil)
}

func (e *HookEngine) DispatchExcept(event string, payload json.RawMessage, session string, exclude HookSubscriber) (json.RawMessage, bool) {
	entries := e.entries(event)
	if len(entries) == 0 {
		return payload, true
	}

	// Inject session into payload so observers can correlate events.
	payload = e.injectSession(payload, session)

	var relevant []hookEntry
	for _, entry := range entries {
		if exclude != nil && entry.sub == exclude {
			continue
		}
		if !entry.sub.IsConnected() {
			continue
		}
		if entry.sub.IsBusy() && e.eventType(event) != HookSecurity {
			continue
		}
		if entry.sub.Session() == session || entry.sub.Session() == "" {
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
		e.dispatchVoid(event, payload, session, relevant)
		return payload, true
	case strategyModifying:
		return e.dispatchModifying(event, payload, session, relevant, def.Type == HookSecurity)
	case strategyClaiming:
		return e.dispatchClaiming(event, payload, session, relevant)
	default:
		return payload, true
	}
}

func (e *HookEngine) eventType(event string) HookEventType {
	if def, ok := HookEvents[event]; ok {
		return def.Type
	}
	return HookSecurity
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

func (e *HookEngine) sessionTenantID(session string) string {
	if e != nil && e.sessionTenants != nil {
		return e.sessionTenants(session)
	}
	return ""
}

func (e *HookEngine) dispatchVoid(event string, payload json.RawMessage, session string, entries []hookEntry) {
	for _, entry := range entries {
		entry.sub.SendMsg(&Message{
			Type:     "hook",
			ID:       generateHookID(),
			Name:     event,
			Session:  session,
			TenantID: e.sessionTenantID(session),
			Payload:  payload,
		})
	}
}

func (e *HookEngine) dispatchModifying(event string, payload json.RawMessage, session string, entries []hookEntry, secure bool) (json.RawMessage, bool) {
	current := payload
	for _, entry := range entries {
		result := e.sendAndWait(entry, event, current, session)
		if result == nil {
			if secure {
				e.logger.Info("security hook timeout, blocking", "event", event, "client", entry.sub.Name())
				return nil, false
			}
			continue
		}
		switch HookAction(result.Action) {
		case ActionBlock:
			e.logger.Info("hook blocked event", "event", event, "client", entry.sub.Name(), "reason", result.Reason)
			return nil, false
		case ActionModify:
			if result.Payload != nil {
				current = e.mergePayload(event, current, result.Payload)
			}
		}
	}
	return current, true
}

// mergePayload merges a hook's modify response with the running payload.
// For context-contributing hook events, the `context` field is additive across
// hooks: each subscriber contributes a system-prompt fragment, so we
// concatenate rather than replace. All other fields are taken from the new
// payload.
func (e *HookEngine) mergePayload(event string, prev, next json.RawMessage) json.RawMessage {
	if !contextAdditiveHookEvent(event) || len(prev) == 0 {
		return next
	}
	var prevMap, nextMap map[string]any
	if err := json.Unmarshal(prev, &prevMap); err != nil {
		return next
	}
	if err := json.Unmarshal(next, &nextMap); err != nil {
		return next
	}
	merged := make(map[string]any, len(prevMap)+len(nextMap))
	for k, v := range prevMap {
		merged[k] = v
	}
	for k, v := range nextMap {
		merged[k] = v
	}
	prevCtx, _ := prevMap["context"].(string)
	nextCtx, _ := nextMap["context"].(string)
	if prevCtx != "" && nextCtx != "" {
		merged["context"] = prevCtx + "\n\n" + nextCtx
	} else if prevCtx != "" {
		merged["context"] = prevCtx
	} else if nextCtx != "" {
		merged["context"] = nextCtx
	}
	out, err := json.Marshal(merged)
	if err != nil {
		return next
	}
	return out
}

func contextAdditiveHookEvent(event string) bool {
	switch event {
	case "session_start", "before_prompt_build":
		return true
	default:
		return false
	}
}

func (e *HookEngine) dispatchClaiming(event string, payload json.RawMessage, session string, entries []hookEntry) (json.RawMessage, bool) {
	for _, entry := range entries {
		result := e.sendAndWait(entry, event, payload, session)
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

func (e *HookEngine) sendAndWait(entry hookEntry, event string, payload json.RawMessage, session string) *HookResult {
	s := entry.sub
	id := generateHookID()
	ch := make(chan *HookResult, 1)
	e.addPendingHook(id, ch)

	s.SendMsg(&Message{
		Type:     "hook",
		ID:       id,
		Name:     event,
		Session:  session,
		TenantID: e.sessionTenantID(session),
		Payload:  payload,
	})

	var result *HookResult
	if entry.subscrip.TimeoutMs != nil && *entry.subscrip.TimeoutMs == 0 {
		// Wait indefinitely; only the subscriber disconnecting unblocks us.
		select {
		case result = <-ch:
		case <-s.Done():
			e.logger.Info("hook subscriber disconnected", "event", event, "client", s.Name(), "id", id)
		}
	} else {
		d := hookTimeout
		if entry.subscrip.TimeoutMs != nil && *entry.subscrip.TimeoutMs > 0 {
			d = time.Duration(*entry.subscrip.TimeoutMs) * time.Millisecond
		}
		timer := time.NewTimer(d)
		select {
		case result = <-ch:
			timer.Stop()
		case <-s.Done():
			timer.Stop()
			e.logger.Info("hook subscriber disconnected", "event", event, "client", s.Name(), "id", id)
		case <-timer.C:
			e.logger.Warn("hook timeout", "event", event, "client", s.Name(), "id", id)
		}
	}

	e.removePendingHook(id)
	return result
}

func (e *HookEngine) injectSession(payload json.RawMessage, session string) json.RawMessage {
	tenantID := e.sessionTenantID(session)
	if session == "" && tenantID == "" {
		return payload
	}
	var m map[string]interface{}
	if json.Unmarshal(payload, &m) != nil {
		return payload
	}
	changed := false
	if _, ok := m["session"]; !ok {
		m["session"] = session
		changed = true
	}
	if tenantID != "" {
		if _, ok := m["tenant_id"]; !ok {
			m["tenant_id"] = tenantID
			changed = true
		}
	}
	if changed {
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
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Errorf("generate hook id: %w", err))
	}
	return "h-" + hex.EncodeToString(b)
}
