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

type HookDispatchAudit struct {
	Event          string
	TenantID       string
	Session        string
	Target         string
	HookID         string
	ReplyAction    string
	DispatchEffect string
	Reason         string
	Status         string
	DurationMs     int64
	TimeoutMs      int64
	Payload        json.RawMessage
}

type HookDispatchDecision struct {
	Event        string
	Target       string
	HookID       string
	ReplyAction  string
	Reason       string
	Status       string
	Payload      json.RawMessage
	Pending      bool
	continuation *hookContinuation
}

type hookContinuation struct {
	event    string
	tenantID string
	session  string
	current  json.RawMessage
	entries  []hookEntry
	secure   bool
}

type hookSendOutcome struct {
	result     *HookResult
	status     string
	durationMs int64
	timeoutMs  int64
	hookID     string
}

type pendingHook struct {
	subscriber HookSubscriber
	runtimeID  string
	ch         chan *HookResult
}

type HookEngine struct {
	mu             sync.RWMutex
	index          map[string][]hookEntry
	pending        map[string]pendingHook
	logger         *slog.Logger
	sessionTenants func(string, string) string
	audit          func(HookDispatchAudit)
}

func NewHookEngine(logger *slog.Logger) *HookEngine {
	if logger == nil {
		logger = slog.Default()
	}
	return &HookEngine{
		index:   make(map[string][]hookEntry),
		pending: make(map[string]pendingHook),
		logger:  logger,
	}
}

func (e *HookEngine) SetSessionTenantResolver(resolve func(string, string) string) {
	if e == nil {
		return
	}
	e.sessionTenants = resolve
}

func (e *HookEngine) SetAuditRecorder(record func(HookDispatchAudit)) {
	if e == nil {
		return
	}
	e.audit = record
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

func (e *HookEngine) Dispatch(event string, payload json.RawMessage, tenantID, session string) (json.RawMessage, bool) {
	result, ok, _ := e.DispatchDetailedExcept(event, payload, tenantID, session, nil)
	return result, ok
}

func (e *HookEngine) DispatchExcept(event string, payload json.RawMessage, tenantID, session string, exclude HookSubscriber) (json.RawMessage, bool) {
	result, ok, _ := e.DispatchDetailedExcept(event, payload, tenantID, session, exclude)
	return result, ok
}

func (e *HookEngine) DispatchDetailedExcept(event string, payload json.RawMessage, tenantID, session string, exclude HookSubscriber) (json.RawMessage, bool, *HookDispatchDecision) {
	entries := e.entries(event)
	if len(entries) == 0 {
		e.recordAudit(HookDispatchAudit{Event: event, TenantID: tenantID, Session: session, Status: "missing", DispatchEffect: "continued", Payload: payload})
		return payload, true, nil
	}

	// Inject session into payload so observers can correlate events.
	payload = e.injectSession(payload, tenantID, session)

	var relevant []hookEntry
	for _, entry := range entries {
		if exclude != nil && entry.sub == exclude {
			continue
		}
		if !entry.sub.IsConnected() {
			continue
		}
		if !entry.sub.ServesTenant(tenantID) {
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
		e.recordAudit(HookDispatchAudit{Event: event, TenantID: tenantID, Session: session, Status: "unavailable", DispatchEffect: "continued", Payload: payload})
		return payload, true, nil
	}

	def, ok := HookEvents[event]
	if !ok {
		return payload, true, nil
	}

	switch def.Strategy {
	case strategyVoid:
		e.dispatchVoid(event, payload, tenantID, session, relevant)
		return payload, true, nil
	case strategyModifying:
		return e.dispatchModifying(event, payload, tenantID, session, relevant, def.Type == HookSecurity)
	case strategyClaiming:
		result, ok := e.dispatchClaiming(event, payload, tenantID, session, relevant)
		return result, ok, nil
	default:
		return payload, true, nil
	}
}

func (e *HookEngine) eventType(event string) HookEventType {
	if def, ok := HookEvents[event]; ok {
		return def.Type
	}
	return HookSecurity
}

func (e *HookEngine) CanHandleResult(sender HookSubscriber, id string) bool {
	pending, ok := e.pendingHook(id)
	return ok && pending.subscriber == sender
}

func (e *HookEngine) HandleRuntimeResult(runtimeID string, msg *Message) {
	pending, ok := e.pendingHook(msg.ID)
	if !ok || pending.runtimeID != runtimeID {
		return
	}
	e.deliverResult(pending, msg)
}

func (e *HookEngine) HandleResult(sender HookSubscriber, msg *Message) {
	pending, ok := e.pendingHook(msg.ID)
	if !ok {
		return
	}
	if pending.subscriber != sender {
		return
	}
	e.deliverResult(pending, msg)
}

func (e *HookEngine) deliverResult(pending pendingHook, msg *Message) {
	payload := msg.Payload
	if payload == nil {
		payload = msg.Data
	}
	result := &HookResult{
		Action:  msg.Action,
		Payload: payload,
		Reason:  msg.Reason,
	}
	select {
	case pending.ch <- result:
	default:
	}
}

func (e *HookEngine) sessionTenantID(tenantID, session string) string {
	if tenantID != "" {
		return tenantID
	}
	if e != nil && e.sessionTenants != nil {
		return e.sessionTenants(tenantID, session)
	}
	return ""
}

func (e *HookEngine) dispatchVoid(event string, payload json.RawMessage, tenantID, session string, entries []hookEntry) {
	for _, entry := range entries {
		entry.sub.SendMsg(&Message{
			Type:     "hook",
			ID:       generateHookID(),
			Name:     event,
			Session:  session,
			TenantID: e.sessionTenantID(tenantID, session),
			Payload:  payload,
		})
	}
}

func (e *HookEngine) dispatchModifying(event string, payload json.RawMessage, tenantID, session string, entries []hookEntry, secure bool) (json.RawMessage, bool, *HookDispatchDecision) {
	return e.dispatchModifyingFrom(event, payload, tenantID, session, entries, secure)
}

func (e *HookEngine) ResumeModifying(decision *HookDispatchDecision) (json.RawMessage, bool, *HookDispatchDecision) {
	if decision == nil || decision.continuation == nil {
		return nil, false, decision
	}
	c := decision.continuation
	return e.dispatchModifyingFrom(c.event, c.current, c.tenantID, c.session, c.entries, c.secure)
}

func (e *HookEngine) dispatchModifyingFrom(event string, payload json.RawMessage, tenantID, session string, entries []hookEntry, secure bool) (json.RawMessage, bool, *HookDispatchDecision) {
	current := payload
	for i, entry := range entries {
		outcome := e.sendAndWait(entry, event, current, tenantID, session)
		result := outcome.result
		if result == nil {
			e.recordAudit(HookDispatchAudit{Event: event, TenantID: tenantID, Session: session, Target: entry.sub.Name(), HookID: outcome.hookID, Status: outcome.status, DispatchEffect: mapHookNilEffect(secure), DurationMs: outcome.durationMs, TimeoutMs: outcome.timeoutMs, Payload: current})
			if secure {
				e.logger.Info("security hook timeout, blocking", hookLogAttrs(event, entry.sub.Name(), current, tenantID, session)...)
				return nil, false, &HookDispatchDecision{Event: event, Target: entry.sub.Name(), HookID: outcome.hookID, Status: outcome.status}
			}
			continue
		}
		switch HookAction(result.Action) {
		case ActionSuspend:
			e.recordAudit(HookDispatchAudit{Event: event, TenantID: tenantID, Session: session, Target: entry.sub.Name(), HookID: outcome.hookID, ReplyAction: result.Action, DispatchEffect: "tool_suspended", Reason: result.Reason, Status: "reply", DurationMs: outcome.durationMs, TimeoutMs: outcome.timeoutMs, Payload: current})
			attrs := hookLogAttrs(event, entry.sub.Name(), current, tenantID, session)
			attrs = append(attrs, "reason", result.Reason)
			e.logger.Info("hook suspended event", attrs...)
			remaining := append([]hookEntry(nil), entries[i+1:]...)
			return current, false, &HookDispatchDecision{Event: event, Target: entry.sub.Name(), HookID: outcome.hookID, ReplyAction: result.Action, Reason: result.Reason, Status: outcome.status, Payload: append(json.RawMessage(nil), result.Payload...), Pending: true, continuation: &hookContinuation{event: event, tenantID: tenantID, session: session, current: append(json.RawMessage(nil), current...), entries: remaining, secure: secure}}
		case ActionBlock:
			e.recordAudit(HookDispatchAudit{Event: event, TenantID: tenantID, Session: session, Target: entry.sub.Name(), HookID: outcome.hookID, ReplyAction: result.Action, DispatchEffect: "tool_not_invoked", Reason: result.Reason, Status: "reply", DurationMs: outcome.durationMs, TimeoutMs: outcome.timeoutMs, Payload: current})
			attrs := hookLogAttrs(event, entry.sub.Name(), current, tenantID, session)
			attrs = append(attrs, "reason", result.Reason)
			e.logger.Info("hook blocked event", attrs...)
			return nil, false, &HookDispatchDecision{Event: event, Target: entry.sub.Name(), HookID: outcome.hookID, ReplyAction: result.Action, Reason: result.Reason, Status: outcome.status, Payload: append(json.RawMessage(nil), result.Payload...)}
		case ActionModify:
			e.recordAudit(HookDispatchAudit{Event: event, TenantID: tenantID, Session: session, Target: entry.sub.Name(), HookID: outcome.hookID, ReplyAction: result.Action, DispatchEffect: "payload_rewritten", Reason: result.Reason, Status: "reply", DurationMs: outcome.durationMs, TimeoutMs: outcome.timeoutMs, Payload: current})
			if result.Payload != nil {
				current = e.mergePayload(event, current, result.Payload)
			}
		default:
			e.recordAudit(HookDispatchAudit{Event: event, TenantID: tenantID, Session: session, Target: entry.sub.Name(), HookID: outcome.hookID, ReplyAction: result.Action, DispatchEffect: "continued", Reason: result.Reason, Status: "reply", DurationMs: outcome.durationMs, TimeoutMs: outcome.timeoutMs, Payload: current})
		}
	}
	return current, true, nil
}

func mapHookNilEffect(secure bool) string {
	if secure {
		return "failed_closed"
	}
	return "failed_open"
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

func (e *HookEngine) dispatchClaiming(event string, payload json.RawMessage, tenantID, session string, entries []hookEntry) (json.RawMessage, bool) {
	for _, entry := range entries {
		outcome := e.sendAndWait(entry, event, payload, tenantID, session)
		result := outcome.result
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

func (e *HookEngine) sendAndWait(entry hookEntry, event string, payload json.RawMessage, tenantID, session string) hookSendOutcome {
	s := entry.sub
	id := generateHookID()
	ch := make(chan *HookResult, 1)
	e.addPendingHook(id, s, ch)
	start := time.Now()
	outcome := hookSendOutcome{status: "timeout", hookID: id}

	s.SendMsg(&Message{
		Type:     "hook",
		ID:       id,
		Name:     event,
		Session:  session,
		TenantID: e.sessionTenantID(tenantID, session),
		Payload:  payload,
	})

	var result *HookResult
	if entry.subscrip.TimeoutMs != nil && *entry.subscrip.TimeoutMs == 0 {
		// Wait indefinitely; only the subscriber disconnecting unblocks us.
		select {
		case result = <-ch:
			outcome.status = "reply"
		case <-s.Done():
			outcome.status = "disconnected"
			attrs := hookLogAttrs(event, s.Name(), payload, tenantID, session)
			attrs = append(attrs, "hook_id", id)
			e.logger.Info("hook subscriber disconnected", attrs...)
		}
	} else {
		d := hookTimeout
		if entry.subscrip.TimeoutMs != nil && *entry.subscrip.TimeoutMs > 0 {
			d = time.Duration(*entry.subscrip.TimeoutMs) * time.Millisecond
		}
		outcome.timeoutMs = d.Milliseconds()
		timer := time.NewTimer(d)
		select {
		case result = <-ch:
			outcome.status = "reply"
			timer.Stop()
		case <-s.Done():
			outcome.status = "disconnected"
			timer.Stop()
			attrs := hookLogAttrs(event, s.Name(), payload, tenantID, session)
			attrs = append(attrs, "hook_id", id, "timeout_ms", d.Milliseconds())
			e.logger.Info("hook subscriber disconnected", attrs...)
		case <-timer.C:
			outcome.status = "timeout"
			attrs := hookLogAttrs(event, s.Name(), payload, tenantID, session)
			attrs = append(attrs, "hook_id", id, "timeout_ms", d.Milliseconds())
			e.logger.Warn("hook timeout", attrs...)
		}
	}

	e.removePendingHook(id)
	outcome.result = result
	outcome.durationMs = time.Since(start).Milliseconds()
	return outcome
}

func (e *HookEngine) recordAudit(event HookDispatchAudit) {
	if e == nil || e.audit == nil {
		return
	}
	e.audit(event)
}

func hookLogAttrs(event, client string, payload json.RawMessage, tenantID, session string) []any {
	attrs := []any{
		"event", event,
		"client", client,
		"tenant_id", tenantID,
		"session", session,
		"payload_bytes", len(payload),
	}
	var body struct {
		Tool     string          `json:"tool"`
		ID       string          `json:"id"`
		TenantID string          `json:"tenant_id"`
		Meta     map[string]any  `json:"meta"`
		Input    json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(payload, &body); err == nil {
		if body.Tool != "" {
			attrs = append(attrs, "tool", body.Tool)
		}
		if body.ID != "" {
			attrs = append(attrs, "tool_call_id", body.ID)
		}
		if turnCorrelationID, _ := body.Meta[turnCorrelationMetaKey].(string); turnCorrelationID != "" {
			attrs = append(attrs, "turn_correlation_id", turnCorrelationID)
		}
		if tenantID == "" && body.TenantID != "" {
			attrs = append(attrs, "payload_tenant_id", body.TenantID)
		}
		if len(body.Input) > 0 {
			attrs = append(attrs, "input_bytes", len(body.Input))
		}
	}
	return attrs
}

func (e *HookEngine) injectSession(payload json.RawMessage, tenantID, session string) json.RawMessage {
	tenantID = e.sessionTenantID(tenantID, session)
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

func (e *HookEngine) addPendingHook(id string, subscriber HookSubscriber, ch chan *HookResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	pending := pendingHook{subscriber: subscriber, ch: ch}
	if runtimeSub, ok := subscriber.(*runtimeHookSubscriber); ok {
		pending.runtimeID = runtimeSub.runtimeID
	}
	e.pending[id] = pending
}

func (e *HookEngine) addPendingRuntimeHook(id string, runtimeID string, ch chan *HookResult) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.pending[id] = pendingHook{runtimeID: runtimeID, ch: ch}
}

func (e *HookEngine) removePendingHook(id string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.pending, id)
}

func (e *HookEngine) pendingHook(id string) (pendingHook, bool) {
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
