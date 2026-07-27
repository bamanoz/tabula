package hooks

import (
	"encoding/json"

	"github.com/bamanoz/tabula/internal/runtime/wire"
)

// Subscription declares a subscriber's interest in a hook event.
//
// TimeoutMs, if non-nil, overrides the default hook wait timeout:
//   - nil: use Engine default (5s).
//   - 0:   wait indefinitely (until the subscriber disconnects). Use sparingly:
//     human approval waits should suspend the tool call instead of holding a
//     hook dispatch open.
//   - >0:  override with the specified number of milliseconds.
type Subscription struct {
	Event     string `json:"event"`
	Priority  int    `json:"priority"`
	TimeoutMs *int   `json:"timeout_ms,omitempty"`
}

// EventType classifies the role of a hook event in the system.
type EventType string

const (
	// Security events enforce policy boundaries.
	// Timeout means block (fail-closed): a silent hook is treated as a denial.
	Security EventType = "security"

	// Domain events contribute domain logic (context injection, content transforms).
	// Timeout means pass through (fail-open): a silent hook is treated as a no-op.
	Domain EventType = "domain"

	// Observability events are for logging, metrics, or auditing.
	// These are fire-and-forget and never block execution.
	Observability EventType = "observability"
)

// EventDef defines a hook event's strategy and security classification.
type EventDef struct {
	Strategy   strategy
	Type       EventType
	BusyPolicy BusyPolicy
}

// BusyPolicy determines how a dispatch handles an already-running runtime hook
// for the same target.
type BusyPolicy string

const (
	// BusySkip continues without a busy domain hook.
	BusySkip BusyPolicy = "skip"
	// BusyWait waits for a busy hook target before dispatching.
	BusyWait BusyPolicy = "wait"
)

// Events is the canonical registry of all hook events.
// Defined as a package-level var so tests can override it.
var Events = map[string]EventDef{
	"before_message":      {Strategy: strategyModifying, Type: Domain},
	"after_message":       {Strategy: strategyVoid, Type: Observability},
	"before_tool_call":    {Strategy: strategyModifying, Type: Security},
	"before_tool_result":  {Strategy: strategyModifying, Type: Domain},
	"after_tool_call":     {Strategy: strategyVoid, Type: Observability},
	"session_start":       {Strategy: strategyModifying, Type: Domain},
	"before_prompt_build": {Strategy: strategyModifying, Type: Domain, BusyPolicy: BusyWait},
	"before_turn":         {Strategy: strategyModifying, Type: Domain},
	"after_turn":          {Strategy: strategyVoid, Type: Observability},
	"before_compaction":   {Strategy: strategyModifying, Type: Domain},
	"session_join":        {Strategy: strategyVoid, Type: Observability},
	"session_end":         {Strategy: strategyVoid, Type: Observability},
	"cancel":              {Strategy: strategyVoid, Type: Observability},
}

// ReplyMode returns the Runtime API reply mode for a hook event.
func ReplyMode(event string) wire.HookReplyMode {
	def, ok := Events[event]
	if !ok {
		return wire.HookReplyModeModifying
	}
	switch def.Strategy {
	case strategyVoid:
		return wire.HookReplyModeNone
	case strategyClaiming:
		return wire.HookReplyModeClaiming
	default:
		return wire.HookReplyModeModifying
	}
}

// Action constants for hook responses.
type Action string

const (
	ActionPass    Action = "pass"
	ActionModify  Action = "modify"
	ActionBlock   Action = "block"
	ActionClaim   Action = "claim"
	ActionSuspend Action = "suspend"
)

// Message carries one hook dispatch or reply inside the hook engine.
type Message struct {
	Type     string
	ID       string
	Name     string
	Session  string
	TenantID string
	Payload  json.RawMessage
	Data     json.RawMessage
	Action   string
	Reason   string
	Release  func()
}
