// Package wire defines the runtime-to-worker protocol used after a runtime
// daemon has spawned a skill or plugin worker process.
package wire

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	runtimewire "github.com/bamanoz/tabula/internal/runtime/wire"
)

// ErrEOF reports a cleanly closed worker protocol stream.
var ErrEOF = errors.New("worker wire: eof")

// Operation identifies one concrete worker-protocol frame.
type Operation string

const (
	OpInit         Operation = "init"
	OpInitAck      Operation = "init_ack"
	OpCall         Operation = "call"
	OpResult       Operation = "result"
	OpEvent        Operation = "event"
	OpEventReply   Operation = "event_reply"
	OpToolsUpdated Operation = "tools_updated"
	OpSend         Operation = "send"
	OpLog          Operation = "log"
	OpShutdown     Operation = "shutdown"
	OpError        Operation = "error"

	// Runtime-to-driver execution v4 operations.
	OpRegister      Operation = "register"
	OpRegisterAck   Operation = "register_ack"
	OpReady         Operation = "ready"
	OpReadyAck      Operation = "ready_ack"
	OpHeartbeat     Operation = "heartbeat"
	OpHeartbeatAck  Operation = "heartbeat_ack"
	OpAssign        Operation = "assign"
	OpPrepared      Operation = "prepared"
	OpPrepareFailed Operation = "prepare_failed"
	OpPermit        Operation = "permit"
	OpOutput        Operation = "output"
	OpOutputAck     Operation = "output_ack"
	OpToolCall      Operation = "tool_call"
	OpToolResult    Operation = "tool_result"
	OpTerminal      Operation = "terminal"
	OpCancel        Operation = "cancel"
	OpCancelAck     Operation = "cancel_ack"
)

// ReplyMode declares whether a worker event expects a reply.
type ReplyMode string

const (
	ReplyModeNone      ReplyMode = "none"
	ReplyModeModifying ReplyMode = "modifying"
	ReplyModeClaiming  ReplyMode = "claiming"
)

// Envelope carries the op discriminator and optional call correlation.
type Envelope struct {
	Op     Operation `json:"op"`
	CallID string    `json:"call_id,omitempty"`
}

// WorkerRegister starts one runtime-attested driver worker channel.
type WorkerRegister struct {
	Op                Operation `json:"op"`
	TenantID          string    `json:"tenant_id"`
	SessionID         string    `json:"session_id"`
	CorrelationID     string    `json:"correlation_id"`
	ComponentID       string    `json:"component_id"`
	AgentSpecRevision string    `json:"agent_spec_revision"`
	DesiredGeneration uint64    `json:"desired_generation"`
	DriverInstanceID  string    `json:"driver_instance_id"`
}

// WorkerRegisterAck acknowledges registration and grants the current fence.
type WorkerRegisterAck struct {
	Op                  Operation               `json:"op"`
	TenantID            string                  `json:"tenant_id"`
	SessionID           string                  `json:"session_id"`
	CorrelationID       string                  `json:"correlation_id"`
	Accepted            bool                    `json:"accepted"`
	Fence               runtimewire.DriverFence `json:"fence,omitempty"`
	Generation          uint64                  `json:"generation"`
	HeartbeatIntervalMS uint64                  `json:"heartbeat_interval_ms,omitempty"`
	Error               *WorkerErrorBody        `json:"error,omitempty"`
}

// WorkerReady reports that registered driver is ready for assignment.
type WorkerReady struct {
	Op            Operation               `json:"op"`
	TenantID      string                  `json:"tenant_id"`
	SessionID     string                  `json:"session_id"`
	CorrelationID string                  `json:"correlation_id"`
	Fence         runtimewire.DriverFence `json:"fence"`
	Generation    uint64                  `json:"generation"`
}

// WorkerReadyAck acknowledges readiness.
type WorkerReadyAck struct {
	Op            Operation               `json:"op"`
	TenantID      string                  `json:"tenant_id"`
	SessionID     string                  `json:"session_id"`
	CorrelationID string                  `json:"correlation_id"`
	Fence         runtimewire.DriverFence `json:"fence"`
	Generation    uint64                  `json:"generation"`
	Accepted      bool                    `json:"accepted"`
	Error         *WorkerErrorBody        `json:"error,omitempty"`
}

// WorkerHeartbeat renews an active worker lease.
type WorkerHeartbeat struct {
	Op            Operation               `json:"op"`
	TenantID      string                  `json:"tenant_id"`
	SessionID     string                  `json:"session_id"`
	CorrelationID string                  `json:"correlation_id"`
	Fence         runtimewire.DriverFence `json:"fence"`
	Generation    uint64                  `json:"generation"`
	Sequence      uint64                  `json:"sequence"`
}

// WorkerHeartbeatAck acknowledges one heartbeat sequence.
type WorkerHeartbeatAck struct {
	Op            Operation               `json:"op"`
	TenantID      string                  `json:"tenant_id"`
	SessionID     string                  `json:"session_id"`
	CorrelationID string                  `json:"correlation_id"`
	Fence         runtimewire.DriverFence `json:"fence"`
	Generation    uint64                  `json:"generation"`
	Sequence      uint64                  `json:"sequence"`
	Accepted      bool                    `json:"accepted"`
	Error         *WorkerErrorBody        `json:"error,omitempty"`
}

// WorkerAttemptScope identifies one fenced turn attempt.
type WorkerAttemptScope struct {
	TenantID          string                  `json:"tenant_id"`
	SessionID         string                  `json:"session_id"`
	TurnID            string                  `json:"turn_id"`
	AttemptID         string                  `json:"attempt_id"`
	CorrelationID     string                  `json:"correlation_id"`
	TurnCorrelationID string                  `json:"turn_correlation_id"`
	Fence             runtimewire.DriverFence `json:"fence"`
	Generation        uint64                  `json:"generation"`
}

// WorkerAssign assigns preparation work. Driver must not execute external work.
type WorkerAssign struct {
	Op Operation `json:"op"`
	WorkerAttemptScope
	Input           json.RawMessage `json:"input"`
	PreparedContext json.RawMessage `json:"prepared_context,omitempty"`
	SequenceContext uint64          `json:"sequence_context"`
	SessionVersion  uint64          `json:"session_version"`
}

// WorkerPrepared reports successful attempt preparation.
type WorkerPrepared struct {
	Op Operation `json:"op"`
	WorkerAttemptScope
	Plan json.RawMessage `json:"plan,omitempty"`
}

// WorkerPrepareFailed reports preparation failure before permit.
type WorkerPrepareFailed struct {
	Op Operation `json:"op"`
	WorkerAttemptScope
	Retryable bool   `json:"retryable"`
	Reason    string `json:"reason"`
}

// WorkerPermit grants permission for external provider/tool side effects.
type WorkerPermit struct {
	Op Operation `json:"op"`
	WorkerAttemptScope
	PermitID       string `json:"permit_id"`
	Sequence       uint64 `json:"sequence"`
	SessionVersion uint64 `json:"session_version"`
	Cursor         uint64 `json:"cursor"`
}

// WorkerOutput is one ordered, bounded attempt output frame.
type WorkerOutput struct {
	Op Operation `json:"op"`
	WorkerAttemptScope
	Sequence   uint64                 `json:"sequence"`
	OutputType runtimewire.OutputType `json:"output_type"`
	Payload    json.RawMessage        `json:"payload"`
}

// WorkerOutputAck acknowledges or rejects one output sequence.
type WorkerOutputAck struct {
	Op Operation `json:"op"`
	WorkerAttemptScope
	Sequence         uint64           `json:"sequence"`
	Accepted         bool             `json:"accepted"`
	ExpectedSequence uint64           `json:"expected_sequence,omitempty"`
	Error            *WorkerErrorBody `json:"error,omitempty"`
}

// WorkerToolCall requests one tool invocation for a permitted attempt.
type WorkerToolCall struct {
	Op Operation `json:"op"`
	WorkerAttemptScope
	ToolCallID string          `json:"tool_call_id"`
	Name       string          `json:"name"`
	Input      json.RawMessage `json:"input"`
}

// WorkerToolResult returns one terminal tool result to the requesting worker.
type WorkerToolResult struct {
	Op Operation `json:"op"`
	WorkerAttemptScope
	ToolCallID       string          `json:"tool_call_id"`
	Output           string          `json:"output"`
	Artifact         json.RawMessage `json:"artifact,omitempty"`
	Truncated        bool            `json:"truncated,omitempty"`
	SyntheticFailure string          `json:"synthetic_failure,omitempty"`
}

// TerminalOutcome identifies one driver terminal report.
type TerminalOutcome string

const (
	TerminalCompleted TerminalOutcome = "completed"
	TerminalFailed    TerminalOutcome = "failed"
	TerminalCancelled TerminalOutcome = "cancelled"
	TerminalUncertain TerminalOutcome = "uncertain"
)

// WorkerTerminal reports one terminal or recovery outcome.
type WorkerTerminal struct {
	Op Operation `json:"op"`
	WorkerAttemptScope
	Sequence uint64          `json:"sequence"`
	Outcome  TerminalOutcome `json:"outcome"`
	Reason   string          `json:"reason,omitempty"`
}

// WorkerCancel requests cooperative cancellation of one attempt.
type WorkerCancel struct {
	Op Operation `json:"op"`
	WorkerAttemptScope
	Sequence       uint64 `json:"sequence"`
	SessionVersion uint64 `json:"session_version"`
}

// WorkerCancelAck acknowledges cancellation observation.
type WorkerCancelAck struct {
	Op Operation `json:"op"`
	WorkerAttemptScope
	Sequence uint64           `json:"sequence"`
	Accepted bool             `json:"accepted"`
	Error    *WorkerErrorBody `json:"error,omitempty"`
}

// ProtocolError reports malformed worker protocol input.
type ProtocolError struct {
	Message string
}

func (e ProtocolError) Error() string { return "worker protocol error: " + e.Message }

// ProtocolErrorf creates a ProtocolError with formatted text.
func ProtocolErrorf(format string, args ...any) error {
	return ProtocolError{Message: fmt.Sprintf(format, args...)}
}

// WorkerInit is sent by the runtime as the first frame after worker spawn.
type WorkerInit struct {
	Op Operation `json:"op"`
	// KernelID identifies the kernel this worker serves.
	KernelID string `json:"kernel_id"`
	// TenantID identifies the tenant context for all worker state/config.
	TenantID string `json:"tenant_id"`
	// TargetID identifies the skill/plugin target this worker serves.
	TargetID string `json:"target_id"`
	// SessionID binds a session-scoped driver worker to one kernel session.
	SessionID string `json:"session_id,omitempty"`
	// AgentSpecRevision pins the external agent configuration selected by policy.
	AgentSpecRevision string `json:"agent_spec_revision,omitempty"`
	// DriverInstanceID is the runtime-attested process identity.
	DriverInstanceID string `json:"driver_instance_id,omitempty"`
	// DesiredGeneration fences obsolete process generations before lease grant.
	DesiredGeneration uint64 `json:"desired_generation,omitempty"`
	// Manifest carries the manifest subset needed by the worker harness.
	Manifest json.RawMessage `json:"manifest,omitempty"`
	// Env carries explicit worker environment values.
	Env map[string]string `json:"env,omitempty"`
}

// WorkerInitAck reports whether the worker is ready to accept calls and carries
// its authoritative initial tool and hook metadata.
type WorkerInitAck struct {
	Op Operation `json:"op"`
	// Ready is true when initialization succeeded.
	Ready bool `json:"ready"`
	// Tools is the authoritative initial tool set when Ready is true.
	Tools []runtimewire.ToolSpec `json:"tools"`
	// Subscriptions is the authoritative initial hook set when Ready is true.
	Subscriptions []runtimewire.HookSpec `json:"subscriptions"`
	// Error is set when Ready is false.
	Error *WorkerErrorBody `json:"error,omitempty"`
}

// WorkerCall is a single tool call from runtime to worker.
type WorkerCall struct {
	Op Operation `json:"op"`
	// CallID correlates this call with its WorkerResult.
	CallID string `json:"call_id"`
	// TenantID identifies the tenant that originated this call.
	TenantID string `json:"tenant_id,omitempty"`
	// Tool is the target-local tool name.
	Tool string `json:"tool"`
	// Args is raw JSON so the worker protocol stays tool-schema neutral.
	Args json.RawMessage `json:"args,omitempty"`
	// SessionID optionally carries the originating kernel session id.
	SessionID string `json:"session_id,omitempty"`
	// TurnCorrelationID correlates all events within one logical agent turn.
	TurnCorrelationID string `json:"turn_correlation_id,omitempty"`
	// Meta carries opaque trusted call context separately from tool arguments.
	Meta json.RawMessage `json:"meta,omitempty"`
}

// WorkerResult is the terminal worker response for one call.
type WorkerResult struct {
	Op Operation `json:"op"`
	// CallID correlates this result with its WorkerCall.
	CallID string `json:"call_id"`
	// OK reports whether Data contains a successful result.
	OK bool `json:"ok"`
	// Data is raw JSON returned by the worker when OK is true.
	Data json.RawMessage `json:"data,omitempty"`
	// Error is set when OK is false.
	Error *WorkerErrorBody `json:"error,omitempty"`
}

// WorkerEvent is a runtime-originated hook event delivered to a worker.
type WorkerEvent struct {
	Op Operation `json:"op"`
	// CallID correlates this event with an optional WorkerEventReply.
	CallID string `json:"call_id,omitempty"`
	// TenantID identifies the tenant that originated this event.
	TenantID string `json:"tenant_id,omitempty"`
	// Event is the canonical hook event name.
	Event string `json:"event"`
	// ReplyMode declares whether the worker must answer.
	ReplyMode ReplyMode `json:"reply_mode"`
	// SessionID optionally carries the originating session id.
	SessionID string `json:"session_id,omitempty"`
	// TurnCorrelationID correlates all events within one logical agent turn.
	TurnCorrelationID string `json:"turn_correlation_id,omitempty"`
	// Data carries the raw hook payload.
	Data json.RawMessage `json:"data,omitempty"`
}

// WorkerEventReply is the worker's terminal response to a WorkerEvent.
type WorkerEventReply struct {
	Op Operation `json:"op"`
	// CallID correlates the reply to WorkerEvent.CallID.
	CallID string `json:"call_id"`
	// Action is the canonical runtime hook action.
	Action runtimewire.HookAction `json:"action"`
	// Data optionally carries rewrite/claim payload.
	Data json.RawMessage `json:"data,omitempty"`
	// Reason is an optional sanitized explanation.
	Reason string `json:"reason,omitempty"`
}

// WorkerToolsUpdated is an authoritative post-init catalog mutation.
type WorkerToolsUpdated struct {
	Op Operation `json:"op"`
	// Revision is the positive monotonic generation for this target.
	Revision int64 `json:"revision"`
	// Tools is the full authoritative current tool set.
	Tools []runtimewire.ToolSpec `json:"tools"`
	// Removed is diagnostic only.
	Removed []string `json:"removed,omitempty"`
}

// WorkerSend is a worker-originated bus emission.
type WorkerSend struct {
	Op Operation `json:"op"`
	// Channel is currently a closed enum with only "bus" allowed.
	Channel string `json:"channel"`
	// Type is the emitted message type.
	Type string `json:"type"`
	// SessionID optionally scopes the send to one session.
	SessionID string `json:"session_id,omitempty"`
	// Payload is the raw JSON message payload.
	Payload json.RawMessage `json:"payload,omitempty"`
}

// WorkerLog is a structured diagnostic emitted by the worker.
type WorkerLog struct {
	Op Operation `json:"op"`
	// Level is a structured log level.
	Level string `json:"level"`
	// Message is the sanitized log text.
	Message string `json:"msg"`
	// Fields is an optional structured JSON object.
	Fields json.RawMessage `json:"fields,omitempty"`
}

// WorkerShutdown asks the worker to exit cooperatively.
type WorkerShutdown struct {
	Op Operation `json:"op"`
	// Reason is optional diagnostic text for shutdown.
	Reason string `json:"reason,omitempty"`
	// Final is true when the runtime process is exiting, not replacing this worker.
	Final bool `json:"final,omitempty"`
}

// WorkerError is an unsolicited async error frame from worker to runtime.
type WorkerError struct {
	Op Operation `json:"op"`
	// Error contains the protocol/panic diagnostic.
	Error WorkerErrorBody `json:"error"`
}

// WorkerErrorBody describes worker-side errors without using Runtime API wire
// error-code namespace.
type WorkerErrorBody struct {
	// Code is a worker-local error code.
	Code string `json:"code"`
	// Message is human-readable diagnostic text.
	Message string `json:"message,omitempty"`
}

// Validate checks the worker-local error payload.
func (e WorkerErrorBody) Validate() error {
	if e.Code == "" {
		return ProtocolErrorf("worker error code is required")
	}
	return nil
}

// DecodeFrame unmarshals and validates one worker NDJSON payload.
func DecodeFrame(data []byte) (Envelope, any, error) {
	var head Envelope
	if err := json.Unmarshal(data, &head); err != nil {
		return Envelope{}, nil, ProtocolErrorf("decode frame: %v", err)
	}
	if head.Op == "" {
		return Envelope{}, nil, ProtocolErrorf("op is required")
	}

	var frame any
	switch head.Op {
	case OpInit:
		frame = &WorkerInit{}
	case OpInitAck:
		frame = &WorkerInitAck{}
	case OpCall:
		frame = &WorkerCall{}
	case OpResult:
		frame = &WorkerResult{}
	case OpEvent:
		frame = &WorkerEvent{}
	case OpEventReply:
		frame = &WorkerEventReply{}
	case OpToolsUpdated:
		frame = &WorkerToolsUpdated{}
	case OpSend:
		frame = &WorkerSend{}
	case OpLog:
		frame = &WorkerLog{}
	case OpShutdown:
		frame = &WorkerShutdown{}
	case OpError:
		frame = &WorkerError{}
	case OpRegister:
		frame = &WorkerRegister{}
	case OpRegisterAck:
		frame = &WorkerRegisterAck{}
	case OpReady:
		frame = &WorkerReady{}
	case OpReadyAck:
		frame = &WorkerReadyAck{}
	case OpHeartbeat:
		frame = &WorkerHeartbeat{}
	case OpHeartbeatAck:
		frame = &WorkerHeartbeatAck{}
	case OpAssign:
		frame = &WorkerAssign{}
	case OpPrepared:
		frame = &WorkerPrepared{}
	case OpPrepareFailed:
		frame = &WorkerPrepareFailed{}
	case OpPermit:
		frame = &WorkerPermit{}
	case OpOutput:
		frame = &WorkerOutput{}
	case OpOutputAck:
		frame = &WorkerOutputAck{}
	case OpToolCall:
		frame = &WorkerToolCall{}
	case OpToolResult:
		frame = &WorkerToolResult{}
	case OpTerminal:
		frame = &WorkerTerminal{}
	case OpCancel:
		frame = &WorkerCancel{}
	case OpCancelAck:
		frame = &WorkerCancelAck{}
	default:
		return Envelope{}, nil, ProtocolErrorf("unknown op %q", head.Op)
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(frame); err != nil {
		return Envelope{}, nil, ProtocolErrorf("decode %s: %v", head.Op, err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return Envelope{}, nil, ProtocolErrorf("decode %s: trailing JSON value", head.Op)
		}
		return Envelope{}, nil, ProtocolErrorf("decode %s: %v", head.Op, err)
	}
	if err := validateFrame(frame); err != nil {
		return Envelope{}, nil, err
	}

	switch f := frame.(type) {
	case *WorkerCall:
		head.CallID = f.CallID
	case *WorkerResult:
		head.CallID = f.CallID
	case *WorkerEvent:
		head.CallID = f.CallID
	case *WorkerEventReply:
		head.CallID = f.CallID
	}
	return head, frame, nil
}

// WriteFrame marshals msg to one validated NDJSON frame and writes it to w.
func WriteFrame(w io.Writer, msg any) error {
	if err := validateFrame(msg); err != nil {
		return err
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = w.Write(data)
	return err
}

// ReadFrame reads and decodes one NDJSON frame. EOF is reported as ErrEOF.
func ReadFrame(r *bufio.Reader) (Envelope, any, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		if errors.Is(err, io.EOF) {
			return Envelope{}, nil, ErrEOF
		}
		return Envelope{}, nil, err
	}
	return DecodeFrame(line)
}

func validateFrame(frame any) error {
	switch f := frame.(type) {
	case WorkerInit:
		return validateFrame(&f)
	case *WorkerInit:
		if f.Op != OpInit {
			return ProtocolErrorf("init op must be %q", OpInit)
		}
		if f.KernelID == "" {
			return ProtocolErrorf("kernel_id is required")
		}
		if f.TenantID == "" {
			return ProtocolErrorf("tenant_id is required")
		}
		if f.TargetID == "" {
			return ProtocolErrorf("target_id is required")
		}
		return nil
	case WorkerInitAck:
		return validateFrame(&f)
	case *WorkerInitAck:
		if f.Op != OpInitAck {
			return ProtocolErrorf("init_ack op must be %q", OpInitAck)
		}
		if f.Ready {
			if f.Tools == nil {
				return ProtocolErrorf("ready init_ack requires tools")
			}
			if f.Subscriptions == nil {
				return ProtocolErrorf("ready init_ack requires subscriptions")
			}
			for _, tool := range f.Tools {
				if err := tool.Validate(); err != nil {
					return err
				}
			}
			for _, hook := range f.Subscriptions {
				if err := hook.Validate(); err != nil {
					return err
				}
			}
			return nil
		}
		if f.Error == nil {
			return ProtocolErrorf("rejected init_ack requires error")
		}
		return f.Error.Validate()
	case WorkerCall:
		return validateFrame(&f)
	case *WorkerCall:
		if f.Op != OpCall {
			return ProtocolErrorf("call op must be %q", OpCall)
		}
		if f.CallID == "" {
			return ProtocolErrorf("call_id is required")
		}
		if f.Tool == "" {
			return ProtocolErrorf("tool is required")
		}
		return nil
	case WorkerResult:
		return validateFrame(&f)
	case *WorkerResult:
		if f.Op != OpResult {
			return ProtocolErrorf("result op must be %q", OpResult)
		}
		if f.CallID == "" {
			return ProtocolErrorf("call_id is required")
		}
		if !f.OK {
			if f.Error == nil {
				return ProtocolErrorf("failed result requires error")
			}
			return f.Error.Validate()
		}
		return nil
	case WorkerEvent:
		return validateFrame(&f)
	case *WorkerEvent:
		if f.Op != OpEvent {
			return ProtocolErrorf("event op must be %q", OpEvent)
		}
		if f.Event == "" {
			return ProtocolErrorf("event is required")
		}
		switch f.ReplyMode {
		case ReplyModeNone:
			return nil
		case ReplyModeModifying, ReplyModeClaiming:
			if f.CallID == "" {
				return ProtocolErrorf("call_id is required when reply_mode expects a reply")
			}
			return nil
		default:
			return ProtocolErrorf("unknown reply_mode %q", f.ReplyMode)
		}
	case WorkerEventReply:
		return validateFrame(&f)
	case *WorkerEventReply:
		if f.Op != OpEventReply {
			return ProtocolErrorf("event_reply op must be %q", OpEventReply)
		}
		if f.CallID == "" {
			return ProtocolErrorf("call_id is required")
		}
		switch f.Action {
		case runtimewire.HookActionOK, runtimewire.HookActionRewrite, runtimewire.HookActionDeny, runtimewire.HookActionClaim, runtimewire.HookActionSuspend:
			return nil
		default:
			return ProtocolErrorf("unknown hook action %q", f.Action)
		}
	case WorkerToolsUpdated:
		return validateFrame(&f)
	case *WorkerToolsUpdated:
		if f.Op != OpToolsUpdated {
			return ProtocolErrorf("tools_updated op must be %q", OpToolsUpdated)
		}
		if f.Revision <= 0 {
			return ProtocolErrorf("revision must be > 0")
		}
		if f.Tools == nil {
			return ProtocolErrorf("tools are required")
		}
		for _, tool := range f.Tools {
			if err := tool.Validate(); err != nil {
				return err
			}
		}
		return nil
	case WorkerSend:
		return validateFrame(&f)
	case *WorkerSend:
		if f.Op != OpSend {
			return ProtocolErrorf("send op must be %q", OpSend)
		}
		if f.Channel != "bus" {
			return ProtocolErrorf("unknown send channel %q", f.Channel)
		}
		if f.Type == "" {
			return ProtocolErrorf("type is required")
		}
		return nil
	case WorkerLog:
		return validateFrame(&f)
	case *WorkerLog:
		if f.Op != OpLog {
			return ProtocolErrorf("log op must be %q", OpLog)
		}
		switch f.Level {
		case "debug", "info", "warn", "error":
		default:
			return ProtocolErrorf("unknown log level %q", f.Level)
		}
		if f.Message == "" {
			return ProtocolErrorf("msg is required")
		}
		return nil
	case WorkerShutdown:
		return validateFrame(&f)
	case *WorkerShutdown:
		if f.Op != OpShutdown {
			return ProtocolErrorf("shutdown op must be %q", OpShutdown)
		}
		return nil
	case WorkerError:
		return validateFrame(&f)
	case *WorkerError:
		if f.Op != OpError {
			return ProtocolErrorf("error op must be %q", OpError)
		}
		return f.Error.Validate()
	case WorkerRegister:
		return validateFrame(&f)
	case *WorkerRegister:
		if f.Op != OpRegister || f.ComponentID == "" || f.AgentSpecRevision == "" || f.DesiredGeneration == 0 || f.DriverInstanceID == "" {
			return ProtocolErrorf("register requires matching op, component_id, agent_spec_revision, positive desired_generation, and driver_instance_id")
		}
		return validateWorkerScope(f.TenantID, f.SessionID, f.CorrelationID)
	case WorkerRegisterAck:
		return validateFrame(&f)
	case *WorkerRegisterAck:
		if f.Op != OpRegisterAck {
			return ProtocolErrorf("register_ack op must be %q", OpRegisterAck)
		}
		if err := validateWorkerScope(f.TenantID, f.SessionID, f.CorrelationID); err != nil {
			return err
		}
		if f.Accepted {
			if f.Error != nil {
				return ProtocolErrorf("accepted register_ack must not carry error")
			}
			if f.HeartbeatIntervalMS == 0 {
				return ProtocolErrorf("accepted register_ack requires heartbeat_interval_ms")
			}
			return validateWorkerFence(f.Fence, f.Generation)
		}
		return validateWorkerRejection("register_ack", f.Error)
	case WorkerReady:
		return validateFrame(&f)
	case *WorkerReady:
		return validateWorkerDriverFrame(f.Op, OpReady, f.TenantID, f.SessionID, f.CorrelationID, f.Fence, f.Generation)
	case WorkerReadyAck:
		return validateFrame(&f)
	case *WorkerReadyAck:
		if err := validateWorkerDriverFrame(f.Op, OpReadyAck, f.TenantID, f.SessionID, f.CorrelationID, f.Fence, f.Generation); err != nil {
			return err
		}
		return validateWorkerAck("ready_ack", f.Accepted, f.Error)
	case WorkerHeartbeat:
		return validateFrame(&f)
	case *WorkerHeartbeat:
		if err := validateWorkerDriverFrame(f.Op, OpHeartbeat, f.TenantID, f.SessionID, f.CorrelationID, f.Fence, f.Generation); err != nil {
			return err
		}
		return validatePositiveSequence("heartbeat", f.Sequence)
	case WorkerHeartbeatAck:
		return validateFrame(&f)
	case *WorkerHeartbeatAck:
		if err := validateWorkerDriverFrame(f.Op, OpHeartbeatAck, f.TenantID, f.SessionID, f.CorrelationID, f.Fence, f.Generation); err != nil {
			return err
		}
		if err := validatePositiveSequence("heartbeat_ack", f.Sequence); err != nil {
			return err
		}
		return validateWorkerAck("heartbeat_ack", f.Accepted, f.Error)
	case WorkerAssign:
		return validateFrame(&f)
	case *WorkerAssign:
		if err := validateWorkerAttempt(f.Op, OpAssign, f.WorkerAttemptScope); err != nil {
			return err
		}
		if f.SessionVersion == 0 {
			return ProtocolErrorf("assign session_version is required")
		}
		return validateWorkerJSON("input", f.Input, 64<<10)
	case WorkerPrepared:
		return validateFrame(&f)
	case *WorkerPrepared:
		if err := validateWorkerAttempt(f.Op, OpPrepared, f.WorkerAttemptScope); err != nil {
			return err
		}
		return validateOptionalWorkerJSON("plan", f.Plan, 64<<10)
	case WorkerPrepareFailed:
		return validateFrame(&f)
	case *WorkerPrepareFailed:
		if err := validateWorkerAttempt(f.Op, OpPrepareFailed, f.WorkerAttemptScope); err != nil {
			return err
		}
		if f.Reason == "" {
			return ProtocolErrorf("prepare_failed reason is required")
		}
		return nil
	case WorkerPermit:
		return validateFrame(&f)
	case *WorkerPermit:
		if err := validateWorkerAttempt(f.Op, OpPermit, f.WorkerAttemptScope); err != nil {
			return err
		}
		if f.PermitID == "" || f.SessionVersion == 0 || f.Cursor == 0 {
			return ProtocolErrorf("permit requires permit_id, session_version, and cursor")
		}
		return validatePositiveSequence("permit", f.Sequence)
	case WorkerOutput:
		return validateFrame(&f)
	case *WorkerOutput:
		if err := validateWorkerAttempt(f.Op, OpOutput, f.WorkerAttemptScope); err != nil {
			return err
		}
		if err := validatePositiveSequence("output", f.Sequence); err != nil {
			return err
		}
		switch f.OutputType {
		case runtimewire.OutputStreamDelta, runtimewire.OutputReasoning, runtimewire.OutputUsage, runtimewire.OutputProviderRetry, runtimewire.OutputProviderError, runtimewire.OutputCompaction, runtimewire.OutputToolResult:
		default:
			return ProtocolErrorf("unknown output_type %q", f.OutputType)
		}
		return validateWorkerJSON("payload", f.Payload, 64<<10)
	case WorkerOutputAck:
		return validateFrame(&f)
	case *WorkerOutputAck:
		if err := validateWorkerAttempt(f.Op, OpOutputAck, f.WorkerAttemptScope); err != nil {
			return err
		}
		if err := validatePositiveSequence("output_ack", f.Sequence); err != nil {
			return err
		}
		if !f.Accepted && f.ExpectedSequence == 0 {
			return ProtocolErrorf("rejected output_ack requires expected_sequence")
		}
		return validateWorkerAck("output_ack", f.Accepted, f.Error)
	case WorkerToolCall:
		return validateFrame(&f)
	case *WorkerToolCall:
		if err := validateWorkerAttempt(f.Op, OpToolCall, f.WorkerAttemptScope); err != nil {
			return err
		}
		if f.ToolCallID == "" || f.Name == "" {
			return ProtocolErrorf("tool_call requires tool_call_id and name")
		}
		return validateWorkerJSON("input", f.Input, 64<<10)
	case WorkerToolResult:
		return validateFrame(&f)
	case *WorkerToolResult:
		if err := validateWorkerAttempt(f.Op, OpToolResult, f.WorkerAttemptScope); err != nil {
			return err
		}
		if f.ToolCallID == "" {
			return ProtocolErrorf("tool_result requires tool_call_id")
		}
		return validateOptionalWorkerJSON("artifact", f.Artifact, 64<<10)
	case WorkerTerminal:
		return validateFrame(&f)
	case *WorkerTerminal:
		if err := validateWorkerAttempt(f.Op, OpTerminal, f.WorkerAttemptScope); err != nil {
			return err
		}
		if err := validatePositiveSequence("terminal", f.Sequence); err != nil {
			return err
		}
		switch f.Outcome {
		case TerminalCompleted, TerminalCancelled:
			if f.Reason != "" {
				return ProtocolErrorf("%s terminal must not carry reason", f.Outcome)
			}
		case TerminalFailed, TerminalUncertain:
			if f.Reason == "" {
				return ProtocolErrorf("%s terminal requires reason", f.Outcome)
			}
		default:
			return ProtocolErrorf("unknown terminal outcome %q", f.Outcome)
		}
		return nil
	case WorkerCancel:
		return validateFrame(&f)
	case *WorkerCancel:
		if err := validateWorkerAttempt(f.Op, OpCancel, f.WorkerAttemptScope); err != nil {
			return err
		}
		if f.SessionVersion == 0 {
			return ProtocolErrorf("cancel session_version is required")
		}
		return validatePositiveSequence("cancel", f.Sequence)
	case WorkerCancelAck:
		return validateFrame(&f)
	case *WorkerCancelAck:
		if err := validateWorkerAttempt(f.Op, OpCancelAck, f.WorkerAttemptScope); err != nil {
			return err
		}
		if err := validatePositiveSequence("cancel_ack", f.Sequence); err != nil {
			return err
		}
		return validateWorkerAck("cancel_ack", f.Accepted, f.Error)
	default:
		return ProtocolErrorf("unsupported frame type %T", frame)
	}
}

func validateWorkerScope(tenantID, sessionID, correlationID string) error {
	if tenantID == "" {
		return ProtocolErrorf("tenant_id is required")
	}
	if sessionID == "" {
		return ProtocolErrorf("session_id is required")
	}
	if correlationID == "" {
		return ProtocolErrorf("correlation_id is required")
	}
	return nil
}

func validateWorkerFence(fence runtimewire.DriverFence, generation uint64) error {
	if fence.DriverInstanceID == "" || fence.LeaseID == "" || fence.Generation == 0 {
		return ProtocolErrorf("complete fence is required")
	}
	if generation == 0 {
		return ProtocolErrorf("generation must be positive")
	}
	if fence.Generation != generation {
		return ProtocolErrorf("generation must match fence generation")
	}
	return nil
}

func validateWorkerDriverFrame(op, expected Operation, tenantID, sessionID, correlationID string, fence runtimewire.DriverFence, generation uint64) error {
	if op != expected {
		return ProtocolErrorf("%s op must be %q", expected, expected)
	}
	if err := validateWorkerScope(tenantID, sessionID, correlationID); err != nil {
		return err
	}
	return validateWorkerFence(fence, generation)
}

func validateWorkerAttempt(op, expected Operation, scope WorkerAttemptScope) error {
	if err := validateWorkerDriverFrame(op, expected, scope.TenantID, scope.SessionID, scope.CorrelationID, scope.Fence, scope.Generation); err != nil {
		return err
	}
	if scope.TurnID == "" || scope.AttemptID == "" || scope.TurnCorrelationID == "" {
		return ProtocolErrorf("%s requires turn_id, attempt_id, and turn_correlation_id", expected)
	}
	return nil
}

func validatePositiveSequence(name string, sequence uint64) error {
	if sequence == 0 {
		return ProtocolErrorf("%s sequence must be positive", name)
	}
	return nil
}

func validateWorkerAck(name string, accepted bool, workerError *WorkerErrorBody) error {
	if accepted {
		if workerError != nil {
			return ProtocolErrorf("accepted %s must not carry error", name)
		}
		return nil
	}
	return validateWorkerRejection(name, workerError)
}

func validateWorkerRejection(name string, workerError *WorkerErrorBody) error {
	if workerError == nil {
		return ProtocolErrorf("rejected %s requires error", name)
	}
	return workerError.Validate()
}

func validateOptionalWorkerJSON(name string, raw json.RawMessage, limit int) error {
	if len(raw) == 0 {
		return nil
	}
	return validateWorkerJSON(name, raw, limit)
}

func validateWorkerJSON(name string, raw json.RawMessage, limit int) error {
	if len(raw) == 0 || !json.Valid(raw) {
		return ProtocolErrorf("%s must be valid JSON", name)
	}
	if len(raw) > limit {
		return ProtocolErrorf("%s exceeds %d bytes", name, limit)
	}
	return nil
}
