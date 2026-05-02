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

// Capability describes one target and the tools exposed by that target.
type Capability struct {
	// Target is the skill or plugin capability owner.
	Target Target `json:"target"`
	// Tools lists tool names available on Target.
	Tools []string `json:"tools,omitempty"`
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
}

// ReloadAck reports which targets were evicted/refreshed by Reload.
type ReloadAck struct {
	// Op must be "reload_ack".
	Op Operation `json:"op"`
	// EvictedTargets lists targets whose worker/config state was evicted.
	EvictedTargets []Target `json:"evicted_targets,omitempty"`
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
