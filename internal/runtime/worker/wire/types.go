// Package wire defines the runtime-to-worker protocol used after a runtime
// daemon has spawned a skill or plugin worker process.
package wire

import (
	"bufio"
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
	// Tool is the target-local tool name.
	Tool string `json:"tool"`
	// Args is raw JSON so the worker protocol stays tool-schema neutral.
	Args json.RawMessage `json:"args,omitempty"`
	// SessionID optionally carries the originating kernel session id.
	SessionID string `json:"session_id,omitempty"`
	// TurnCorrelationID correlates all events within one logical agent turn.
	TurnCorrelationID string `json:"turn_correlation_id,omitempty"`
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
	default:
		return Envelope{}, nil, ProtocolErrorf("unknown op %q", head.Op)
	}
	if err := json.Unmarshal(data, frame); err != nil {
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
	default:
		return ProtocolErrorf("unsupported frame type %T", frame)
	}
}
