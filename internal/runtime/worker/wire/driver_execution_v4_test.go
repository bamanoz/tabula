package wire

import (
	"bufio"
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	runtimewire "github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestDriverExecutionV4WorkerFramesRoundTrip(t *testing.T) {
	fence := runtimewire.DriverFence{DriverInstanceID: "drv-1", LeaseID: "lease-1", Generation: 7}
	scope := WorkerAttemptScope{TenantID: "tenant", SessionID: "session", TurnID: "turn-1", AttemptID: "attempt-1", CorrelationID: "correlation-1", TurnCorrelationID: "turn-1", Fence: fence, Generation: 7}
	frames := []any{
		&WorkerRegister{Op: OpRegister, TenantID: "tenant", SessionID: "session", CorrelationID: "register-1", ComponentID: "driver", AgentSpecRevision: "sha256:spec", DesiredGeneration: 7, DriverInstanceID: "drv-1"},
		&WorkerRegisterAck{Op: OpRegisterAck, TenantID: "tenant", SessionID: "session", CorrelationID: "register-1", Accepted: true, Fence: fence, Generation: 7, HeartbeatIntervalMS: 5000},
		&WorkerReady{Op: OpReady, TenantID: "tenant", SessionID: "session", CorrelationID: "ready-1", Fence: fence, Generation: 7},
		&WorkerReadyAck{Op: OpReadyAck, TenantID: "tenant", SessionID: "session", CorrelationID: "ready-1", Fence: fence, Generation: 7, Accepted: true},
		&WorkerHeartbeat{Op: OpHeartbeat, TenantID: "tenant", SessionID: "session", CorrelationID: "heartbeat-1", Fence: fence, Generation: 7, Sequence: 1},
		&WorkerHeartbeatAck{Op: OpHeartbeatAck, TenantID: "tenant", SessionID: "session", CorrelationID: "heartbeat-1", Fence: fence, Generation: 7, Sequence: 1, Accepted: true},
		&WorkerAssign{Op: OpAssign, WorkerAttemptScope: scope, Input: json.RawMessage(`{"text":"hello"}`), PreparedContext: json.RawMessage(`{"history":1}`), SequenceContext: 4, SessionVersion: 42},
		&WorkerPrepared{Op: OpPrepared, WorkerAttemptScope: scope, Plan: json.RawMessage(`{"model":"m"}`)},
		&WorkerPrepareFailed{Op: OpPrepareFailed, WorkerAttemptScope: scope, Retryable: true, Reason: "configuration unavailable"},
		&WorkerPermit{Op: OpPermit, WorkerAttemptScope: scope, PermitID: "permit-1", Sequence: 1, SessionVersion: 43, Cursor: 12},
		&WorkerOutput{Op: OpOutput, WorkerAttemptScope: scope, Sequence: 1, OutputType: runtimewire.OutputStreamDelta, Payload: json.RawMessage(`{"text":"x"}`)},
		&WorkerOutputAck{Op: OpOutputAck, WorkerAttemptScope: scope, Sequence: 1, Accepted: true},
		&WorkerTerminal{Op: OpTerminal, WorkerAttemptScope: scope, Sequence: 2, Outcome: TerminalCompleted},
		&WorkerCancel{Op: OpCancel, WorkerAttemptScope: scope, Sequence: 2, SessionVersion: 44},
		&WorkerCancelAck{Op: OpCancelAck, WorkerAttemptScope: scope, Sequence: 2, Accepted: true},
	}

	for _, frame := range frames {
		var buffer bytes.Buffer
		if err := WriteFrame(&buffer, frame); err != nil {
			t.Fatalf("WriteFrame(%T): %v", frame, err)
		}
		_, decoded, err := ReadFrame(bufio.NewReader(&buffer))
		if err != nil {
			t.Fatalf("ReadFrame(%T): %v", frame, err)
		}
		if !reflect.DeepEqual(frame, decoded) {
			t.Fatalf("round trip %T\nwant %#v\n got %#v", frame, frame, decoded)
		}
	}
}

func TestDriverExecutionV4WorkerFramesStrictValidation(t *testing.T) {
	fence := runtimewire.DriverFence{DriverInstanceID: "drv-1", LeaseID: "lease-1", Generation: 7}
	scope := WorkerAttemptScope{TenantID: "tenant", SessionID: "session", TurnID: "turn-1", AttemptID: "attempt-1", CorrelationID: "correlation-1", TurnCorrelationID: "turn-1", Fence: fence, Generation: 7}
	large := json.RawMessage(`"` + strings.Repeat("x", 64<<10) + `"`)
	tests := []struct {
		name  string
		frame any
	}{
		{"register missing tenant", &WorkerRegister{Op: OpRegister, SessionID: "session", CorrelationID: "c", ComponentID: "driver", AgentSpecRevision: "rev", DesiredGeneration: 7, DriverInstanceID: "drv-1"}},
		{"register missing correlation", &WorkerRegister{Op: OpRegister, TenantID: "tenant", SessionID: "session", ComponentID: "driver", AgentSpecRevision: "rev", DesiredGeneration: 7, DriverInstanceID: "drv-1"}},
		{"register missing generation", &WorkerRegister{Op: OpRegister, TenantID: "tenant", SessionID: "session", CorrelationID: "c", ComponentID: "driver", AgentSpecRevision: "rev", DriverInstanceID: "drv-1"}},
		{"register ack missing heartbeat interval", &WorkerRegisterAck{Op: OpRegisterAck, TenantID: "tenant", SessionID: "session", CorrelationID: "c", Accepted: true, Fence: fence, Generation: 7}},
		{"register ack incomplete fence", &WorkerRegisterAck{Op: OpRegisterAck, TenantID: "tenant", SessionID: "session", CorrelationID: "c", Accepted: true, Fence: runtimewire.DriverFence{DriverInstanceID: "drv-1", Generation: 7}, Generation: 7, HeartbeatIntervalMS: 5000}},
		{"ready mismatched generation", &WorkerReady{Op: OpReady, TenantID: "tenant", SessionID: "session", CorrelationID: "c", Fence: fence, Generation: 8}},
		{"heartbeat zero sequence", &WorkerHeartbeat{Op: OpHeartbeat, TenantID: "tenant", SessionID: "session", CorrelationID: "c", Fence: fence, Generation: 7}},
		{"heartbeat ack rejection missing error", &WorkerHeartbeatAck{Op: OpHeartbeatAck, TenantID: "tenant", SessionID: "session", CorrelationID: "c", Fence: fence, Generation: 7, Sequence: 1}},
		{"attempt missing turn", &WorkerPrepared{Op: OpPrepared, WorkerAttemptScope: WorkerAttemptScope{TenantID: "tenant", SessionID: "session", AttemptID: "attempt", CorrelationID: "c", Fence: fence, Generation: 7}}},
		{"attempt missing attempt", &WorkerPrepared{Op: OpPrepared, WorkerAttemptScope: WorkerAttemptScope{TenantID: "tenant", SessionID: "session", TurnID: "turn", CorrelationID: "c", Fence: fence, Generation: 7}}},
		{"assign invalid input", &WorkerAssign{Op: OpAssign, WorkerAttemptScope: scope, Input: json.RawMessage(`{`), SessionVersion: 1}},
		{"prepare failed missing reason", &WorkerPrepareFailed{Op: OpPrepareFailed, WorkerAttemptScope: scope}},
		{"permit zero sequence", &WorkerPermit{Op: OpPermit, WorkerAttemptScope: scope, PermitID: "permit", SessionVersion: 1, Cursor: 1}},
		{"output unknown type", &WorkerOutput{Op: OpOutput, WorkerAttemptScope: scope, Sequence: 1, OutputType: "future", Payload: json.RawMessage(`{}`)}},
		{"output oversized payload", &WorkerOutput{Op: OpOutput, WorkerAttemptScope: scope, Sequence: 1, OutputType: runtimewire.OutputUsage, Payload: large}},
		{"output ack rejection missing expected sequence", &WorkerOutputAck{Op: OpOutputAck, WorkerAttemptScope: scope, Sequence: 1, Error: &WorkerErrorBody{Code: "replay_required"}}},
		{"terminal unknown outcome", &WorkerTerminal{Op: OpTerminal, WorkerAttemptScope: scope, Sequence: 1, Outcome: "future"}},
		{"failed terminal missing reason", &WorkerTerminal{Op: OpTerminal, WorkerAttemptScope: scope, Sequence: 1, Outcome: TerminalFailed}},
		{"cancel missing session version", &WorkerCancel{Op: OpCancel, WorkerAttemptScope: scope, Sequence: 1}},
		{"cancel ack accepted with error", &WorkerCancelAck{Op: OpCancelAck, WorkerAttemptScope: scope, Sequence: 1, Accepted: true, Error: &WorkerErrorBody{Code: "conflict"}}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var buffer bytes.Buffer
			if err := WriteFrame(&buffer, test.frame); err == nil {
				t.Fatalf("WriteFrame(%T) succeeded", test.frame)
			}
		})
	}
}

func TestDriverExecutionV4DecodeRejectsUnknownFields(t *testing.T) {
	_, _, err := DecodeFrame([]byte(`{"op":"heartbeat","tenant_id":"tenant","session_id":"session","correlation_id":"c","fence":{"driver_instance_id":"drv-1","lease_id":"lease-1","generation":7},"generation":7,"sequence":1,"unexpected":true}`))
	if err == nil {
		t.Fatal("DecodeFrame accepted unknown field")
	}
}
