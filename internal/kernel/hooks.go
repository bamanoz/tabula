package kernel

import "encoding/json"

// HookSubscription declares a client's interest in a hook event.
//
// TimeoutMs, if non-nil, overrides the default hook wait timeout:
//   - nil: use HookEngine default (5s).
//   - 0:   wait indefinitely (until the subscriber disconnects). Use sparingly:
//     human approval waits should suspend the tool call instead of holding a
//     hook dispatch open.
//   - >0:  override with the specified number of milliseconds.
type HookSubscription struct {
	Event     string `json:"event"`
	Priority  int    `json:"priority"`
	TimeoutMs *int   `json:"timeout_ms,omitempty"`
}

// HookEventType classifies the role of a hook event in the system.
type HookEventType string

const (
	// HookSecurity events enforce policy boundaries.
	// Timeout means block (fail-closed) — a silent hook is treated as a denial.
	HookSecurity HookEventType = "security"

	// HookDomain events contribute domain logic (context injection, content transforms).
	// Timeout means pass through (fail-open) — a silent hook is treated as a no-op.
	HookDomain HookEventType = "domain"

	// HookObservability events are for logging, metrics, or auditing.
	// These are fire-and-forget and never block execution.
	HookObservability HookEventType = "observability"
)

// HookEventDef defines a hook event's strategy and security classification.
type HookEventDef struct {
	Strategy   hookStrategy
	Type       HookEventType
	BusyPolicy HookBusyPolicy
}

// HookBusyPolicy determines how a dispatch handles an already-running runtime
// hook for the same target.
type HookBusyPolicy string

const (
	// HookBusySkip continues without a busy domain hook.
	HookBusySkip HookBusyPolicy = "skip"
	// HookBusyWait waits for a busy hook target before dispatching.
	HookBusyWait HookBusyPolicy = "wait"
)

// HookEvents is the canonical registry of all hook events.
// Defined as a package-level var so it can be overridden in tests.
var HookEvents = map[string]HookEventDef{
	"before_message":     {Strategy: strategyModifying, Type: HookDomain},
	"after_message":      {Strategy: strategyVoid, Type: HookObservability},
	"before_tool_call":   {Strategy: strategyModifying, Type: HookSecurity},
	"before_tool_result": {Strategy: strategyModifying, Type: HookDomain},
	"after_tool_call":    {Strategy: strategyVoid, Type: HookObservability},
	"session_start":      {Strategy: strategyModifying, Type: HookDomain},
	// Prompt construction must not use a partial hook set merely because a
	// concurrent catalog refresh is still running against one target.
	"before_prompt_build": {Strategy: strategyModifying, Type: HookDomain, BusyPolicy: HookBusyWait},
	"before_turn":         {Strategy: strategyModifying, Type: HookDomain},
	"after_turn":          {Strategy: strategyVoid, Type: HookObservability},
	"before_compaction":   {Strategy: strategyModifying, Type: HookDomain},
	"session_join":        {Strategy: strategyVoid, Type: HookObservability},
	"session_end":         {Strategy: strategyVoid, Type: HookObservability},
	"cancel":              {Strategy: strategyVoid, Type: HookObservability},
	"approval_resolved":   {Strategy: strategyVoid, Type: HookObservability},
}

func (h *Hub) rebuildHookIndex() {
	h.hooks.RebuildIndex(h.allHookSubscribers())
}

// dispatchHook is the single entry point for all hook dispatch.
// It validates the event, logs the dispatch, and delegates to the hook engine.
// Returns (payload, true) on success, (nil, false) if blocked.
func (h *Hub) dispatchHook(event string, payload json.RawMessage, tenantID, session string) (json.RawMessage, bool) {
	return h.dispatchHookExcept(event, payload, tenantID, session, nil)
}

func (h *Hub) dispatchHookExcept(event string, payload json.RawMessage, tenantID, session string, exclude HookSubscriber) (json.RawMessage, bool) {
	def, known := HookEvents[event]
	if !known {
		h.Logger.Warn("unknown hook event", "event", event)
		return payload, true
	}

	h.Logger.Debug("dispatching hook", "event", event, "type", def.Type, "session", session)
	result, ok := h.hooks.DispatchExcept(event, payload, tenantID, session, exclude)

	if !ok {
		h.Logger.Info("hook blocked event", "event", event, "type", def.Type, "tenant_id", tenantID, "session", session, "payload_bytes", len(payload))
	}
	return result, ok
}

func (h *Hub) handleHookResult(sender *Client, msg *Message) {
	h.hooks.HandleResult(sender, msg)
}
