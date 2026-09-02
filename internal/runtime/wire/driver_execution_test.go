package wire

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestDriverExecutionV4RoundTrip(t *testing.T) {
	fence := DriverFence{DriverInstanceID: "drv-1", LeaseID: "lease-1", Generation: 7}
	ref := AttemptRef{TenantID: "tenant", SessionID: "session", TurnID: "turn-1", AttemptID: "attempt-1", CorrelationID: "turn-1", Fence: fence}
	frames := []any{
		&DriverRegister{Op: OpDriverRegister, RequestID: "register-1", TenantID: "tenant", SessionID: "session", ComponentID: "driver", AgentSpecRevision: "sha256:spec", DesiredGeneration: 7, DriverInstanceID: "drv-1"},
		&DriverLeaseGranted{Op: OpDriverLeaseGranted, RequestID: "register-1", TenantID: "tenant", SessionID: "session", Accepted: true, Fence: fence, ExpiresAt: time.Unix(100, 0).UTC(), HeartbeatIntervalMS: 5000, SessionVersion: 42},
		&DriverLeaseGranted{Op: OpDriverLeaseGranted, RequestID: "register-2", TenantID: "tenant", SessionID: "session", Accepted: false, Error: &Error{Code: ErrorProtocolError}},
		&DriverReady{Op: OpDriverReady, RequestID: "ready-1", TenantID: "tenant", SessionID: "session", Fence: fence},
		&DriverHeartbeat{Op: OpDriverHeartbeat, RequestID: "heartbeat-1", TenantID: "tenant", SessionID: "session", Fence: fence, Sequence: 1},
		&DriverResult{Op: OpDriverResult, RequestID: "heartbeat-1", Accepted: true, SessionVersion: 43, Cursor: 12},
		&DriverResult{Op: OpDriverResult, RequestID: "output-1", Accepted: false, ExpectedSequence: 2, Error: &Error{Code: ErrorProtocolError, Retryable: true}},
		&TurnAssign{Op: OpTurnAssign, RequestID: "assign-1", AttemptRef: ref, Input: json.RawMessage(`{"text":"hello"}`), PreparedContext: json.RawMessage(`{"history":1}`), SequenceContext: 9, SessionVersion: 44},
		&TurnPrepared{Op: OpTurnPrepared, RequestID: "assign-1", AttemptRef: ref, Plan: json.RawMessage(`{"model":"m"}`)},
		&TurnPrepareFailed{Op: OpTurnPrepareFailed, RequestID: "assign-1", AttemptRef: ref, Retryable: true, Reason: "configuration unavailable"},
		&TurnPermit{Op: OpTurnPermit, RequestID: "permit-1", AttemptRef: ref, PermitID: "permit-1", SessionVersion: 45, Cursor: 14},
		&TurnCancel{Op: OpTurnCancel, RequestID: "cancel-1", AttemptRef: ref, SessionVersion: 46},
		&TurnOutput{Op: OpTurnOutput, RequestID: "output-1", AttemptRef: ref, Sequence: 1, OutputType: OutputStreamDelta, Payload: json.RawMessage(`{"text":"x"}`)},
		&TurnToolCall{Op: OpTurnToolCall, RequestID: "tool-1", AttemptRef: ref, ToolCallID: "call-1", Name: "search", Input: json.RawMessage(`{"query":"x"}`)},
		&TurnToolResult{Op: OpTurnToolResult, RequestID: "tool-1", AttemptRef: ref, ToolCallID: "call-1", Output: "found", Artifact: json.RawMessage(`{"ref":"a"}`), Truncated: true},
		&TurnCompleted{Op: OpTurnCompleted, RequestID: "terminal-1", AttemptRef: ref, Sequence: 2},
		&TurnFailed{Op: OpTurnFailed, RequestID: "terminal-2", AttemptRef: ref, Sequence: 2, Reason: "provider failed"},
		&TurnCancelled{Op: OpTurnCancelled, RequestID: "terminal-3", AttemptRef: ref, Sequence: 2},
		&TurnUncertain{Op: OpTurnUncertain, RequestID: "terminal-4", AttemptRef: ref, Sequence: 2, Reason: "provider outcome unknown", ReconciliationEvidence: "provider-op-1"},
	}
	for _, frame := range frames {
		data, err := Encode(frame)
		if err != nil {
			t.Fatalf("Encode(%T): %v", frame, err)
		}
		head, decoded, err := Decode(data)
		if err != nil {
			t.Fatalf("Decode(%T): %v", frame, err)
		}
		if head.CallID == "" {
			t.Fatalf("Decode(%T) omitted correlation", frame)
		}
		if !reflect.DeepEqual(frame, decoded) {
			t.Fatalf("round trip %T\nwant %#v\n got %#v", frame, frame, decoded)
		}
	}
}

func TestDriverExecutionV4StrictValidation(t *testing.T) {
	fence := DriverFence{DriverInstanceID: "drv-1", LeaseID: "lease-1", Generation: 7}
	ref := AttemptRef{TenantID: "tenant", SessionID: "session", TurnID: "turn-1", AttemptID: "attempt-1", CorrelationID: "turn-1", Fence: fence}
	large := json.RawMessage(`"` + strings.Repeat("x", 64<<10) + `"`)
	tests := []struct {
		name  string
		frame any
	}{
		{"register missing generation", &DriverRegister{Op: OpDriverRegister, RequestID: "r", TenantID: "tenant", SessionID: "session", ComponentID: "driver", AgentSpecRevision: "rev", DriverInstanceID: "drv-1"}},
		{"lease missing expiry", &DriverLeaseGranted{Op: OpDriverLeaseGranted, RequestID: "r", TenantID: "tenant", SessionID: "session", Accepted: true, Fence: fence, HeartbeatIntervalMS: 1, SessionVersion: 1}},
		{"lease rejection missing error", &DriverLeaseGranted{Op: OpDriverLeaseGranted, RequestID: "r", TenantID: "tenant", SessionID: "session"}},
		{"lease acceptance carrying error", &DriverLeaseGranted{Op: OpDriverLeaseGranted, RequestID: "r", TenantID: "tenant", SessionID: "session", Accepted: true, Fence: fence, ExpiresAt: time.Unix(100, 0).UTC(), HeartbeatIntervalMS: 1, SessionVersion: 1, Error: &Error{Code: ErrorInternal}}},
		{"heartbeat zero sequence", &DriverHeartbeat{Op: OpDriverHeartbeat, RequestID: "r", TenantID: "tenant", SessionID: "session", Fence: fence}},
		{"attempt incomplete fence", &TurnPrepared{Op: OpTurnPrepared, RequestID: "r", AttemptRef: AttemptRef{TenantID: "tenant", SessionID: "session", TurnID: "turn", AttemptID: "attempt", CorrelationID: "turn", Fence: DriverFence{DriverInstanceID: "drv", Generation: 1}}}},
		{"assignment missing session version", &TurnAssign{Op: OpTurnAssign, RequestID: "r", AttemptRef: ref, Input: json.RawMessage(`{}`)}},
		{"permit missing permit id", &TurnPermit{Op: OpTurnPermit, RequestID: "r", AttemptRef: ref, SessionVersion: 1, Cursor: 1}},
		{"unknown output type", &TurnOutput{Op: OpTurnOutput, RequestID: "r", AttemptRef: ref, Sequence: 1, OutputType: "future", Payload: json.RawMessage(`{}`)}},
		{"oversized output", &TurnOutput{Op: OpTurnOutput, RequestID: "r", AttemptRef: ref, Sequence: 1, OutputType: OutputUsage, Payload: large}},
		{"failed missing reason", &TurnFailed{Op: OpTurnFailed, RequestID: "r", AttemptRef: ref, Sequence: 1}},
		{"uncertain zero sequence", &TurnUncertain{Op: OpTurnUncertain, RequestID: "r", AttemptRef: ref, Reason: "unknown"}},
		{"rejection missing error", &DriverResult{Op: OpDriverResult, RequestID: "r"}},
		{"acceptance carrying error", &DriverResult{Op: OpDriverResult, RequestID: "r", Accepted: true, Error: &Error{Code: ErrorInternal}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Encode(tt.frame); err == nil {
				t.Fatalf("Encode(%T) succeeded", tt.frame)
			}
		})
	}
}
