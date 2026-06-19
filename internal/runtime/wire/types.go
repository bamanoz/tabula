// Package wire defines the transport-independent Runtime API frames exchanged
// between the Tabula kernel and a runtime daemon.
package wire

import (
	"encoding/json"
	"fmt"
	"regexp"
	"time"
)

// Operation identifies the Runtime API frame payload carried by a JSON frame.
type Operation string

const (
	// OpHello is sent by a runtime to begin authentication and capability preview.
	OpHello Operation = "hello"
	// OpHelloAck is sent by the kernel to accept or reject a Hello frame.
	OpHelloAck Operation = "hello_ack"
	// OpInvoke requests a tenant-scoped tool call on a runtime target.
	OpInvoke Operation = "invoke"
	// OpInvokeResult is the terminal runtime response for an Invoke call_id.
	OpInvokeResult Operation = "invoke_result"
	// OpInvokeResultStart starts a streamed successful invoke result payload.
	OpInvokeResultStart Operation = "invoke_result_start"
	// OpInvokeResultDelta carries one streamed invoke result chunk.
	OpInvokeResultDelta Operation = "invoke_result_delta"
	// OpInvokeResultEnd ends a streamed successful invoke result payload.
	OpInvokeResultEnd Operation = "invoke_result_end"
	// OpCancel asks the runtime to abort an in-flight call_id.
	OpCancel Operation = "cancel"
	// OpCancelAck acknowledges that cancellation for a call_id was observed.
	OpCancelAck Operation = "cancel_ack"
	// OpHealth asks the runtime for system liveness information.
	OpHealth Operation = "health"
	// OpHealthResp is the runtime liveness response.
	OpHealthResp Operation = "health_resp"
	// OpListCapabilities asks the runtime for its target capability list.
	OpListCapabilities Operation = "list_capabilities"
	// OpListCapabilitiesResp returns the runtime target capability list.
	OpListCapabilitiesResp Operation = "list_capabilities_resp"
	// OpReload asks the runtime to refresh worker/config state.
	OpReload Operation = "reload"
	// OpReloadAck returns the targets affected by a Reload request.
	OpReloadAck Operation = "reload_ack"
	// OpHookEvent delivers a kernel-originated hook event to a runtime target.
	OpHookEvent Operation = "hook_event"
	// OpCatalogUpdate carries an authoritative runtime-originated catalog update.
	OpCatalogUpdate Operation = "catalog_update"
	// OpHookEventReply carries the terminal result for one hook_event call_id.
	OpHookEventReply Operation = "hook_event_reply"
	// OpPluginSend carries a runtime-originated plugin bus emission.
	OpPluginSend Operation = "plugin_send"
	// OpPluginLog carries a runtime-originated structured log record.
	OpPluginLog Operation = "plugin_log"
	// OpLifecycleNotice carries runtime-originated target lifecycle diagnostics.
	OpLifecycleNotice Operation = "lifecycle_notice"
)

// ErrorCode is a canonical Runtime API wire error code.
type ErrorCode string

const (
	// ErrorUnauthorized reports token/certificate authentication failure.
	ErrorUnauthorized ErrorCode = "unauthorized"
	// ErrorRuntimeUnavailable reports transport disconnect or no live runtime.
	ErrorRuntimeUnavailable ErrorCode = "runtime_unavailable"
	// ErrorRuntimeBusy reports worker pool overflow or cold-worker contention.
	ErrorRuntimeBusy ErrorCode = "runtime_busy"
	// ErrorUnknownRuntime reports a runtime id absent from registry/auth context.
	ErrorUnknownRuntime ErrorCode = "unknown_runtime"
	// ErrorTenantUnknown reports a syntactically valid but unknown tenant id.
	ErrorTenantUnknown ErrorCode = "tenant_unknown"
	// ErrorTenantForbidden reports a runtime/tenant whitelist denial.
	ErrorTenantForbidden ErrorCode = "tenant_forbidden"
	// ErrorTargetUnknown reports a missing runtime capability target.
	ErrorTargetUnknown ErrorCode = "target_unknown"
	// ErrorTargetForbidden reports policy denial for an existing target.
	ErrorTargetForbidden ErrorCode = "target_forbidden"
	// ErrorToolNotFound reports a missing tool on an existing target.
	ErrorToolNotFound ErrorCode = "tool_not_found"
	// ErrorTimeout reports a per-call deadline expiry.
	ErrorTimeout ErrorCode = "timeout"
	// ErrorCancelled reports an explicit Cancel operation.
	ErrorCancelled ErrorCode = "cancelled"
	// ErrorProtocolError reports malformed frames or missing required fields.
	ErrorProtocolError ErrorCode = "protocol_error"
	// ErrorInternal reports an unexpected kernel/runtime failure.
	ErrorInternal ErrorCode = "internal_error"
	// ErrorSkillExecFailed reports a skill harness/subprocess non-zero result.
	ErrorSkillExecFailed ErrorCode = "skill_exec_failed"
)

// AllErrorCodes lists the canonical Runtime API wire error roster. Plugin-local
// errors such as fs_outside_root and exec_denied are intentionally excluded.
var AllErrorCodes = []ErrorCode{
	ErrorUnauthorized,
	ErrorRuntimeUnavailable,
	ErrorRuntimeBusy,
	ErrorUnknownRuntime,
	ErrorTenantUnknown,
	ErrorTenantForbidden,
	ErrorTargetUnknown,
	ErrorTargetForbidden,
	ErrorToolNotFound,
	ErrorTimeout,
	ErrorCancelled,
	ErrorProtocolError,
	ErrorInternal,
	ErrorSkillExecFailed,
}

var allErrorCodeSet = func() map[ErrorCode]struct{} {
	set := make(map[ErrorCode]struct{}, len(AllErrorCodes))
	for _, code := range AllErrorCodes {
		set[code] = struct{}{}
	}
	return set
}()

// IsErrorCode reports whether code is in the canonical Runtime API wire roster.
func IsErrorCode(code ErrorCode) bool {
	_, ok := allErrorCodeSet[code]
	return ok
}

// Error is the structured error payload carried by rejected handshakes and
// failed InvokeResult frames.
type Error struct {
	// Code is one canonical Runtime API wire error code.
	Code ErrorCode `json:"code"`
	// Message is human-readable diagnostic text. It must not contain secrets.
	Message string `json:"message,omitempty"`
	// Retryable tells callers whether retrying the same request may succeed.
	Retryable bool `json:"retryable"`
}

// Validate returns a protocol error when the error code is not canonical.
func (e Error) Validate() error {
	if !IsErrorCode(e.Code) {
		return ProtocolErrorf("unknown error code %q", e.Code)
	}
	return nil
}

// TargetKind identifies which runtime target namespace an Invoke uses.
type TargetKind string

const (
	// TargetKindSkill addresses a skill target.
	TargetKindSkill TargetKind = "skill"
	// TargetKindPlugin addresses a plugin target.
	TargetKindPlugin TargetKind = "plugin"
)

// Target is the Runtime API wire target object. Display strings like
// "skill:timer" are not accepted as wire target forms.
type Target struct {
	// Kind is either skill or plugin.
	Kind TargetKind `json:"kind"`
	// ID is the target identifier within Kind. It is opaque to the wire layer but
	// must be present.
	ID string `json:"id"`
}

// Validate returns a protocol error when the target object is incomplete or has
// an unknown kind.
func (t Target) Validate() error {
	switch t.Kind {
	case TargetKindSkill, TargetKindPlugin:
	default:
		return ProtocolErrorf("unknown target kind %q", t.Kind)
	}
	if t.ID == "" {
		return ProtocolErrorf("target id is required")
	}
	return nil
}

// ToolSpec describes one tool exposed by a capability target.
type ToolSpec struct {
	// Name is the target-local tool name.
	Name string `json:"name"`
	// Description is optional human-readable tool metadata.
	Description string `json:"description,omitempty"`
	// Schema carries the authoritative JSON schema/params payload when known.
	Schema json.RawMessage `json:"schema,omitempty"`
	// DeadlineMS is the target-local default deadline when non-zero.
	DeadlineMS int64 `json:"deadline_ms,omitempty"`
	// Concurrency declares whether this tool may overlap with other inflight calls.
	Concurrency ToolConcurrency `json:"concurrency,omitempty"`
	// ExecutionGroup is the plugin-declared execution domain for this tool.
	ExecutionGroup string `json:"execution_group,omitempty"`
	// ConflictsWithGroups lists execution groups that must be idle before this tool runs.
	ConflictsWithGroups []string `json:"conflicts_with_groups,omitempty"`
}

// ToolConcurrency describes whether a tool remains serial or may overlap with
// other inflight calls on the same warm worker.
type ToolConcurrency string

const (
	ToolConcurrencySerial   ToolConcurrency = "serial"
	ToolConcurrencyParallel ToolConcurrency = "parallel"
)

// Validate returns a protocol error when the tool metadata is incomplete.
func (t ToolSpec) Validate() error {
	if t.Name == "" {
		return ProtocolErrorf("tool name is required")
	}
	switch t.Concurrency {
	case "", ToolConcurrencySerial, ToolConcurrencyParallel:
	default:
		return ProtocolErrorf("unknown tool concurrency %q", t.Concurrency)
	}
	if t.ExecutionGroup == "" && len(t.ConflictsWithGroups) > 0 {
		return ProtocolErrorf("execution_group is required when conflicts_with_groups is set")
	}
	for i, group := range t.ConflictsWithGroups {
		if group == "" {
			return ProtocolErrorf("conflicts_with_groups[%d] is required", i)
		}
	}
	return nil
}

// HookSpec describes one hook subscription exposed by a capability target.
type HookSpec struct {
	// Event is the canonical hook event name.
	Event string `json:"event"`
	// Priority is the runtime/kernel ordering hint for the subscription.
	Priority int `json:"priority,omitempty"`
	// TimeoutMS optionally overrides the default hook timeout.
	TimeoutMS *int64 `json:"timeout_ms,omitempty"`
}

// Validate returns a protocol error when the hook metadata is incomplete.
func (h HookSpec) Validate() error {
	if h.Event == "" {
		return ProtocolErrorf("hook event is required")
	}
	return nil
}

// CapabilityState describes whether capability metadata is only diagnostic or
// actually invokable.
type CapabilityState string

const (
	CapabilityStateManifestLoaded CapabilityState = "manifest_loaded"
	CapabilityStateInitializing   CapabilityState = "initializing"
	CapabilityStateReady          CapabilityState = "ready"
	CapabilityStateFailed         CapabilityState = "failed"
	CapabilityStateStale          CapabilityState = "stale"
)

// CapabilitySource identifies where the current capability metadata came from.
type CapabilitySource string

const (
	CapabilitySourceManifest CapabilitySource = "manifest"
	CapabilitySourceWorker   CapabilitySource = "worker"
)

// WorkerMode describes whether a target is reused or spawned per call.
type WorkerMode string

const (
	WorkerModeWarm WorkerMode = "warm"
	WorkerModeCold WorkerMode = "cold"
)

// HarnessKind identifies which runtime-side harness/execution family serves a target.
type HarnessKind string

const (
	HarnessKindUnknown HarnessKind = "unknown"
	HarnessKindBash    HarnessKind = "bash"
	HarnessKindPython  HarnessKind = "python"
	HarnessKindNode    HarnessKind = "node"
)

// Capability describes one target and its authoritative tool/hook metadata.
type Capability struct {
	// Target is the skill or plugin capability owner.
	Target Target `json:"target"`
	// Tenants optionally scopes this target metadata. Empty means all tenants
	// served by the runtime.
	Tenants []string `json:"tenants,omitempty"`
	// Tools lists the current authoritative tool metadata for Target.
	Tools []ToolSpec `json:"tools,omitempty"`
	// Hooks lists the current authoritative hook subscriptions for Target.
	Hooks []HookSpec `json:"hooks,omitempty"`
	// Revision is the monotonic runtime-local generation for Target metadata.
	Revision int64 `json:"revision,omitempty"`
	// State reports whether Target is merely known or actually ready.
	State CapabilityState `json:"state"`
	// Source reports whether Target metadata came from manifest or worker data.
	Source CapabilitySource `json:"source"`
	// WorkerMode reports whether Target is warm-reused or cold-spawned.
	WorkerMode WorkerMode `json:"worker_mode,omitempty"`
	// HarnessKind reports which harness/runtime family serves Target.
	HarnessKind HarnessKind `json:"harness_kind,omitempty"`
}

// Validate returns a protocol error when the capability metadata is incomplete.
func (c Capability) Validate() error {
	if err := c.Target.Validate(); err != nil {
		return err
	}
	if err := validateTenantsServed(c.Tenants); err != nil {
		return err
	}
	for _, tool := range c.Tools {
		if err := tool.Validate(); err != nil {
			return err
		}
	}
	for _, hook := range c.Hooks {
		if err := hook.Validate(); err != nil {
			return err
		}
	}
	switch c.State {
	case CapabilityStateManifestLoaded, CapabilityStateInitializing, CapabilityStateReady, CapabilityStateFailed, CapabilityStateStale:
	default:
		return ProtocolErrorf("unknown capability state %q", c.State)
	}
	switch c.Source {
	case CapabilitySourceManifest, CapabilitySourceWorker:
	default:
		return ProtocolErrorf("unknown capability source %q", c.Source)
	}
	switch c.WorkerMode {
	case "", WorkerModeWarm, WorkerModeCold:
	default:
		return ProtocolErrorf("unknown worker mode %q", c.WorkerMode)
	}
	switch c.HarnessKind {
	case "", HarnessKindUnknown, HarnessKindBash, HarnessKindPython, HarnessKindNode:
	default:
		return ProtocolErrorf("unknown harness kind %q", c.HarnessKind)
	}
	return nil
}

// Envelope carries the op discriminator and optional call_id correlation for a
// decoded frame. Payload is the concrete frame struct returned by Decode.
type Envelope struct {
	// Op identifies the concrete frame type.
	Op Operation `json:"op"`
	// CallID correlates Invoke, InvokeResult, Cancel, and CancelAck frames.
	CallID string `json:"call_id,omitempty"`
}

// Hello is the runtime-to-kernel authentication and capability preview frame.
type Hello struct {
	// Op must be "hello".
	Op Operation `json:"op"`
	// RuntimeID is the runtime's stable id, validated like tenant/runtime ids.
	RuntimeID string `json:"runtime_id"`
	// Token is bearer auth material for local or remote runtime authentication.
	Token string `json:"token,omitempty"`
	// ProtocolVersion is the Runtime API version supported by the runtime.
	ProtocolVersion string `json:"protocol_version"`
	// Capabilities is an optional pre-auth target capability preview.
	Capabilities []Capability `json:"capabilities,omitempty"`
	// TenantsServed is the runtime's configured tenant allow-list. ["*"] means
	// any tenant known to the kernel.
	TenantsServed []string `json:"tenants_served,omitempty"`
}

// HelloAck is the kernel-to-runtime handshake decision frame.
type HelloAck struct {
	// Op must be "hello_ack".
	Op Operation `json:"op"`
	// Accepted reports whether the runtime may continue using the connection.
	Accepted bool `json:"accepted"`
	// KernelID is the authenticated kernel id when Accepted is true.
	KernelID string `json:"kernel_id,omitempty"`
	// Error explains rejection when Accepted is false.
	Error *Error `json:"error,omitempty"`
}

// Invoke is a tenant-scoped tool call from kernel to runtime.
type Invoke struct {
	// Op must be "invoke".
	Op Operation `json:"op"`
	// CallID is an opaque kernel-generated correlation id.
	CallID string `json:"call_id"`
	// TenantID is mandatory for every tool call, including default-tenant periods.
	TenantID string `json:"tenant_id"`
	// SessionID optionally carries the originating kernel session id.
	SessionID string `json:"session_id,omitempty"`
	// TurnCorrelationID correlates all events within one logical agent turn.
	TurnCorrelationID string `json:"turn_correlation_id,omitempty"`
	// Target is the skill or plugin target object.
	Target Target `json:"target"`
	// Tool is the tool name within Target.
	Tool string `json:"tool"`
	// Args is raw JSON so the wire contract stays decoupled from tool schemas.
	Args json.RawMessage `json:"args,omitempty"`
	// TimeoutMS is an optional per-call deadline in milliseconds.
	TimeoutMS int64 `json:"timeout_ms,omitempty"`
}

// InvokeResult is the terminal response for one Invoke call_id.
type InvokeResult struct {
	// Op must be "invoke_result".
	Op Operation `json:"op"`
	// CallID is the Invoke correlation id.
	CallID string `json:"call_id"`
	// OK reports whether Data contains a successful tool result.
	OK bool `json:"ok"`
	// Data is raw JSON returned by the target tool when OK is true.
	Data json.RawMessage `json:"data,omitempty"`
	// Error is set when OK is false.
	Error *Error `json:"error,omitempty"`
}

// InvokeResultStart starts a streamed successful Invoke response.
type InvokeResultStart struct {
	// Op must be "invoke_result_start".
	Op Operation `json:"op"`
	// CallID is the Invoke correlation id.
	CallID string `json:"call_id"`
}

// InvokeResultDelta carries one chunk of a streamed successful Invoke response.
type InvokeResultDelta struct {
	// Op must be "invoke_result_delta".
	Op Operation `json:"op"`
	// CallID is the Invoke correlation id.
	CallID string `json:"call_id"`
	// Seq is a positive monotonic chunk sequence number starting at 1.
	Seq int64 `json:"seq"`
	// Data is one UTF-8-safe slice of the raw successful result JSON payload.
	Data string `json:"data"`
}

// InvokeResultEnd ends a streamed successful Invoke response.
type InvokeResultEnd struct {
	// Op must be "invoke_result_end".
	Op Operation `json:"op"`
	// CallID is the Invoke correlation id.
	CallID string `json:"call_id"`
	// Bytes is the total payload byte count reconstructed from deltas.
	Bytes int64 `json:"bytes,omitempty"`
}

// Cancel asks the runtime to abort an in-flight call.
type Cancel struct {
	// Op must be "cancel".
	Op Operation `json:"op"`
	// CallID is the in-flight call to abort.
	CallID string `json:"call_id"`
}

// CancelAck acknowledges cancellation observation for a call.
type CancelAck struct {
	// Op must be "cancel_ack".
	Op Operation `json:"op"`
	// CallID is the cancelled call.
	CallID string `json:"call_id"`
}

// Health is a kernel-to-runtime liveness request. It is intentionally tenantless.
type Health struct {
	// Op must be "health".
	Op Operation `json:"op"`
}

// HealthResp returns runtime liveness details.
type HealthResp struct {
	// Op must be "health_resp".
	Op Operation `json:"op"`
	// OK reports whether the runtime considers itself healthy.
	OK bool `json:"ok"`
	// UptimeMS is runtime process uptime in milliseconds.
	UptimeMS int64 `json:"uptime_ms,omitempty"`
	// WorkerCount is the number of workers currently known to the runtime.
	WorkerCount int `json:"worker_count,omitempty"`
}

// ListCapabilities asks for target capabilities. It is intentionally tenantless.
type ListCapabilities struct {
	// Op must be "list_capabilities".
	Op Operation `json:"op"`
}

// ListCapabilitiesResp returns target capabilities known to the runtime.
type ListCapabilitiesResp struct {
	// Op must be "list_capabilities_resp".
	Op Operation `json:"op"`
	// Targets lists runtime targets and their exposed tools.
	Targets []Capability `json:"targets"`
}

// Reload asks the runtime to refresh worker/config state.
type Reload struct {
	// Op must be "reload".
	Op Operation `json:"op"`
	// Target optionally scopes reload to a single target. Nil means all targets.
	Target *Target `json:"target,omitempty"`
	// Tenants optionally scopes reload to specific tenant/app catalogs. Empty
	// means every tenant served by this runtime.
	Tenants []string `json:"tenants,omitempty"`
}

// ReloadAck reports which targets were evicted/refreshed by Reload.
type ReloadAck struct {
	// Op must be "reload_ack".
	Op Operation `json:"op"`
	// EvictedTargets lists targets whose worker/config state was evicted.
	EvictedTargets []Target `json:"evicted_targets,omitempty"`
}

// CatalogUpdate is the authoritative runtime-originated target metadata update.
type CatalogUpdate struct {
	Op Operation `json:"op"`
	// Target identifies the target whose metadata changed.
	Target Target `json:"target"`
	// Tenants optionally scopes this target metadata. Empty means all tenants
	// served by the runtime.
	Tenants []string `json:"tenants,omitempty"`
	// Tools is the full authoritative current tool set.
	Tools []ToolSpec `json:"tools,omitempty"`
	// Hooks is the full authoritative current hook set.
	Hooks []HookSpec `json:"hooks,omitempty"`
	// Removed is diagnostic only; kernel replaces from the full tool set.
	Removed []string `json:"removed,omitempty"`
	// Revision is the monotonic runtime-local generation for Target metadata.
	Revision int64 `json:"revision"`
	// State reports the target readiness state.
	State CapabilityState `json:"state"`
	// Source reports whether metadata is manifest- or worker-derived.
	Source CapabilitySource `json:"source"`
	// Diagnostic is an optional sanitized status string.
	Diagnostic string `json:"diagnostic,omitempty"`
}

// HookEvent is a kernel-originated hook event routed to one runtime target.
type HookEvent struct {
	Op Operation `json:"op"`
	// TenantID scopes the hook to a tenant/app catalog. Empty means default.
	TenantID string `json:"tenant_id,omitempty"`
	// CallID is the correlation id for the hook event.
	CallID string `json:"call_id"`
	// Target identifies the target handling the hook event.
	Target Target `json:"target"`
	// Event is the canonical hook event name.
	Event string `json:"event"`
	// ReplyMode declares whether this hook expects a terminal reply.
	ReplyMode HookReplyMode `json:"reply_mode"`
	// Data is the raw JSON hook payload.
	Data json.RawMessage `json:"data,omitempty"`
	// SessionID optionally carries the session id for diagnostics/routing.
	SessionID string `json:"session_id,omitempty"`
	// TurnCorrelationID correlates all events within one logical agent turn.
	TurnCorrelationID string `json:"turn_correlation_id,omitempty"`
}

// HookReplyMode declares whether a runtime hook event expects a reply.
type HookReplyMode string

const (
	HookReplyModeNone      HookReplyMode = "none"
	HookReplyModeModifying HookReplyMode = "modifying"
	HookReplyModeClaiming  HookReplyMode = "claiming"
)

// HookAction is the canonical action returned by a hook event reply.
type HookAction string

const (
	HookActionOK      HookAction = "ok"
	HookActionRewrite HookAction = "rewrite"
	HookActionDeny    HookAction = "deny"
	HookActionClaim   HookAction = "claim"
	HookActionSuspend HookAction = "suspend"
)

// HookEventReply is the terminal result for one hook_event call_id.
type HookEventReply struct {
	Op Operation `json:"op"`
	// CallID correlates this reply with its HookEvent.
	CallID string `json:"call_id"`
	// Action is one canonical hook reply action.
	Action HookAction `json:"action"`
	// Data optionally carries rewritten or claimed payload.
	Data json.RawMessage `json:"data,omitempty"`
	// Reason is an optional sanitized explanation.
	Reason string `json:"reason,omitempty"`
}

// PluginSend is a runtime-originated plugin bus emission.
type PluginSend struct {
	Op Operation `json:"op"`
	// Target identifies the emitting plugin target.
	Target Target `json:"target"`
	// Channel is the transport channel. M2 only permits "bus".
	Channel string `json:"channel"`
	// Type is the emitted message type.
	Type string `json:"type"`
	// Payload is the raw JSON message payload.
	Payload json.RawMessage `json:"payload,omitempty"`
	// SessionID optionally scopes the send to one session.
	SessionID string `json:"session_id,omitempty"`
	// TenantID optionally scopes the send to one tenant/app. Empty means default.
	TenantID string `json:"tenant_id,omitempty"`
}

// PluginLog is a runtime-originated structured plugin log record.
type PluginLog struct {
	Op Operation `json:"op"`
	// Target identifies the emitting plugin target.
	Target Target `json:"target"`
	// Level is the structured log level.
	Level string `json:"level"`
	// Message is the sanitized human-readable log text.
	Message string `json:"message"`
	// Fields is an optional structured JSON object.
	Fields json.RawMessage `json:"fields,omitempty"`
}

// LifecycleState is the canonical target lifecycle state.
type LifecycleState string

const (
	LifecycleStateStarting LifecycleState = "starting"
	LifecycleStateReady    LifecycleState = "ready"
	LifecycleStateStopping LifecycleState = "stopping"
	LifecycleStateExited   LifecycleState = "exited"
	LifecycleStateCrashed  LifecycleState = "crashed"
)

// LifecycleNotice is a runtime-originated target lifecycle update.
type LifecycleNotice struct {
	Op Operation `json:"op"`
	// Target identifies the target whose lifecycle changed.
	Target Target `json:"target"`
	// State is the canonical lifecycle state.
	State LifecycleState `json:"state"`
	// PID is the worker pid when known.
	PID int `json:"pid,omitempty"`
	// Message is an optional sanitized diagnostic string.
	Message string `json:"message,omitempty"`
}

// ProtocolError reports a Runtime API protocol validation failure.
type ProtocolError struct {
	Message string
}

func (e ProtocolError) Error() string { return "protocol error: " + e.Message }

// ProtocolErrorf creates a ProtocolError with formatted text.
func ProtocolErrorf(format string, args ...any) error {
	return ProtocolError{Message: fmt.Sprintf(format, args...)}
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

// ValidateTenantID validates Runtime API tenant id syntax. The system-managed
// "default" tenant is valid; hard-reserved creation policy is handled by M4.
func ValidateTenantID(id string) error {
	if !idPattern.MatchString(id) {
		return ProtocolErrorf("invalid tenant_id %q", id)
	}
	return nil
}

// ValidateRuntimeID validates Runtime API runtime id syntax and M10 hard
// reserved runtime ids.
func ValidateRuntimeID(id string) error {
	if !idPattern.MatchString(id) {
		return ProtocolErrorf("invalid runtime_id %q", id)
	}
	switch id {
	case "kernel", "system", "admin":
		return ProtocolErrorf("reserved runtime_id %q", id)
	}
	return nil
}

// TimeoutDuration converts an Invoke timeout to a duration. A non-positive
// timeout means no per-call timeout was requested at the wire layer.
func (i Invoke) TimeoutDuration() time.Duration {
	if i.TimeoutMS <= 0 {
		return 0
	}
	return time.Duration(i.TimeoutMS) * time.Millisecond
}
