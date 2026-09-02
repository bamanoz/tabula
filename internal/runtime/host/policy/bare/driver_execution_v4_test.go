package bare

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	runtimewire "github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

func TestDriverExecutionFramesRouteThroughSingleReader(t *testing.T) {
	worker := spawnDriverWorker(t, driverExecutionWorkerScript)
	defer func() { _ = worker.Shutdown(context.Background(), policy.Shutdown{Reason: "worker stopped"}) }()

	for _, want := range []any{
		&workerwire.WorkerRegister{},
		&workerwire.WorkerReady{},
		&workerwire.WorkerHeartbeat{},
		&workerwire.WorkerPrepared{},
		&workerwire.WorkerPrepareFailed{},
		&workerwire.WorkerOutput{},
		&workerwire.WorkerToolCall{},
		&workerwire.WorkerTerminal{},
		&workerwire.WorkerCancelAck{},
	} {
		select {
		case event := <-worker.Events():
			if event.Err != nil {
				t.Fatalf("Events: %v", event.Err)
			}
			if event.Envelope.Op == "" {
				t.Fatal("event envelope op is empty")
			}
			switch want.(type) {
			case *workerwire.WorkerRegister:
				if _, ok := event.Frame.(*workerwire.WorkerRegister); !ok {
					t.Fatalf("frame = %T, want *WorkerRegister", event.Frame)
				}
			case *workerwire.WorkerReady:
				if _, ok := event.Frame.(*workerwire.WorkerReady); !ok {
					t.Fatalf("frame = %T, want *WorkerReady", event.Frame)
				}
			case *workerwire.WorkerHeartbeat:
				if _, ok := event.Frame.(*workerwire.WorkerHeartbeat); !ok {
					t.Fatalf("frame = %T, want *WorkerHeartbeat", event.Frame)
				}
			case *workerwire.WorkerPrepared:
				if _, ok := event.Frame.(*workerwire.WorkerPrepared); !ok {
					t.Fatalf("frame = %T, want *WorkerPrepared", event.Frame)
				}
			case *workerwire.WorkerPrepareFailed:
				if _, ok := event.Frame.(*workerwire.WorkerPrepareFailed); !ok {
					t.Fatalf("frame = %T, want *WorkerPrepareFailed", event.Frame)
				}
			case *workerwire.WorkerOutput:
				if _, ok := event.Frame.(*workerwire.WorkerOutput); !ok {
					t.Fatalf("frame = %T, want *WorkerOutput", event.Frame)
				}
			case *workerwire.WorkerToolCall:
				if _, ok := event.Frame.(*workerwire.WorkerToolCall); !ok {
					t.Fatalf("frame = %T, want *WorkerToolCall", event.Frame)
				}
			case *workerwire.WorkerTerminal:
				if _, ok := event.Frame.(*workerwire.WorkerTerminal); !ok {
					t.Fatalf("frame = %T, want *WorkerTerminal", event.Frame)
				}
			case *workerwire.WorkerCancelAck:
				if _, ok := event.Frame.(*workerwire.WorkerCancelAck); !ok {
					t.Fatalf("frame = %T, want *WorkerCancelAck", event.Frame)
				}
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %T", want)
		}
	}
}

func TestDriverExecutionTypedDeliveryAndCorrelatedCancelAck(t *testing.T) {
	worker := spawnDriverWorker(t, driverCommandWorkerScript)
	defer func() { _ = worker.Shutdown(context.Background(), policy.Shutdown{Reason: "worker stopped"}) }()

	scope := testWorkerAttemptScope("cancel-correlation")
	if err := worker.RegisterAck(context.Background(), workerwire.WorkerRegisterAck{
		TenantID: "tenant-a", SessionID: "session-a", CorrelationID: "register-correlation",
		Accepted: true, Fence: scope.Fence, Generation: 1, HeartbeatIntervalMS: 5000,
	}); err != nil {
		t.Fatalf("RegisterAck: %v", err)
	}
	if err := worker.ReadyAck(context.Background(), workerwire.WorkerReadyAck{
		TenantID: "tenant-a", SessionID: "session-a", CorrelationID: "ready-correlation",
		Accepted: true, Fence: scope.Fence, Generation: 1,
	}); err != nil {
		t.Fatalf("ReadyAck: %v", err)
	}
	if err := worker.HeartbeatAck(context.Background(), workerwire.WorkerHeartbeatAck{
		TenantID: "tenant-a", SessionID: "session-a", CorrelationID: "heartbeat-correlation",
		Accepted: true, Fence: scope.Fence, Generation: 1, Sequence: 1,
	}); err != nil {
		t.Fatalf("HeartbeatAck: %v", err)
	}
	if err := worker.Assign(context.Background(), workerwire.WorkerAssign{
		WorkerAttemptScope: scope, Input: json.RawMessage(`{"message":"hello"}`), SessionVersion: 1,
	}); err != nil {
		t.Fatalf("Assign: %v", err)
	}
	if err := worker.Permit(context.Background(), workerwire.WorkerPermit{
		WorkerAttemptScope: scope, PermitID: "permit-1", Sequence: 1, SessionVersion: 1, Cursor: 1,
	}); err != nil {
		t.Fatalf("Permit: %v", err)
	}
	if err := worker.OutputAck(context.Background(), workerwire.WorkerOutputAck{
		WorkerAttemptScope: scope, Sequence: 1, Accepted: true,
	}); err != nil {
		t.Fatalf("OutputAck: %v", err)
	}
	if err := worker.CancelAck(context.Background(), workerwire.WorkerCancelAck{
		WorkerAttemptScope: scope, Sequence: 1, Accepted: true,
	}); err != nil {
		t.Fatalf("CancelAck: %v", err)
	}

	ack, err := worker.Cancel(context.Background(), workerwire.WorkerCancel{
		WorkerAttemptScope: scope, Sequence: 2, SessionVersion: 1,
	})
	if err != nil {
		t.Fatalf("Cancel: %v", err)
	}
	if ack.CorrelationID != scope.CorrelationID || !ack.Accepted || ack.Sequence != 2 {
		t.Fatalf("Cancel ack = %#v", ack)
	}

	select {
	case event := <-worker.Events():
		ack, ok := event.Frame.(*workerwire.WorkerCancelAck)
		if !ok || ack.CorrelationID != "unrelated-correlation" {
			t.Fatalf("async frame = %#v, want unrelated cancel ack", event.Frame)
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for unrelated cancel ack")
	}
}

func TestCancelContextCancellationRemovesPendingWaiter(t *testing.T) {
	worker := spawnDriverWorker(t, driverDelayedCancelWorkerScript)
	defer func() { _ = worker.Shutdown(context.Background(), policy.Shutdown{Reason: "worker stopped"}) }()

	scope := testWorkerAttemptScope("reused-correlation")
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	_, err := worker.Cancel(ctx, workerwire.WorkerCancel{
		WorkerAttemptScope: scope, Sequence: 1, SessionVersion: 1,
	})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Cancel error = %v, want deadline exceeded", err)
	}

	ack, err := worker.Cancel(context.Background(), workerwire.WorkerCancel{
		WorkerAttemptScope: scope, Sequence: 2, SessionVersion: 1,
	})
	if err != nil {
		t.Fatalf("second Cancel: %v", err)
	}
	if ack.Sequence != 2 {
		t.Fatalf("second Cancel ack sequence = %d, want 2", ack.Sequence)
	}
}

func TestCancelWaiterFailsWhenWorkerCloses(t *testing.T) {
	worker := spawnDriverWorker(t, driverCloseOnCancelWorkerScript)
	scope := testWorkerAttemptScope("close-correlation")

	_, err := worker.Cancel(context.Background(), workerwire.WorkerCancel{
		WorkerAttemptScope: scope, Sequence: 1, SessionVersion: 1,
	})
	if err == nil {
		t.Fatal("Cancel succeeded after worker closed")
	}
}

func spawnDriverWorker(t *testing.T, scriptContents string) policy.Worker {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TABULA_HOME", filepath.Join(dir, "home"))
	script := filepath.Join(dir, "worker.py")
	writeFile(t, script, scriptContents)
	worker, err := New().Spawn(context.Background(), policy.SpawnReq{
		KernelID: "main", TenantID: "tenant-a", TargetID: "driver-a", SessionID: "session-a",
		Command: []string{testPython(t), "worker.py"}, Manifest: json.RawMessage(`{"id":"driver-a"}`),
		WorkingDir: dir, Mode: policy.SpawnModeWarm,
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	if _, err := worker.Init(context.Background(), workerwire.WorkerInit{
		KernelID: "main", TenantID: "tenant-a", TargetID: "driver-a", SessionID: "session-a",
		Manifest: json.RawMessage(`{"id":"driver-a"}`),
	}); err != nil {
		t.Fatalf("Init: %v", err)
	}
	return worker
}

func testWorkerAttemptScope(correlationID string) workerwire.WorkerAttemptScope {
	return workerwire.WorkerAttemptScope{
		TenantID: "tenant-a", SessionID: "session-a", TurnID: "turn-1", AttemptID: "attempt-1",
		CorrelationID:     correlationID,
		TurnCorrelationID: "turn-1",
		Fence:             runtimewire.DriverFence{DriverInstanceID: "driver-instance-1", LeaseID: "lease-1", Generation: 1},
		Generation:        1,
	}
}

const driverScriptPreamble = `#!/usr/bin/env python3
import json
import sys
import time

line = sys.stdin.readline()
if not line:
    sys.exit(2)
json.loads(line)
sys.stdout.write(json.dumps({"op": "init_ack", "ready": True, "tools": [], "subscriptions": []}) + "\n")
sys.stdout.flush()
`

const driverExecutionWorkerScript = driverScriptPreamble + `
scope = {"tenant_id":"tenant-a","session_id":"session-a","turn_id":"turn-1","attempt_id":"attempt-1","correlation_id":"event-correlation","turn_correlation_id":"turn-1","fence":{"driver_instance_id":"driver-instance-1","lease_id":"lease-1","generation":1},"generation":1}
frames = [
    {"op":"register","tenant_id":"tenant-a","session_id":"session-a","correlation_id":"register-correlation","component_id":"driver-a","agent_spec_revision":"rev-1","desired_generation":1,"driver_instance_id":"driver-instance-1"},
    {"op":"ready","tenant_id":"tenant-a","session_id":"session-a","correlation_id":"ready-correlation","fence":scope["fence"],"generation":1},
    {"op":"heartbeat","tenant_id":"tenant-a","session_id":"session-a","correlation_id":"heartbeat-correlation","fence":scope["fence"],"generation":1,"sequence":1},
    dict(scope, op="prepared", plan={"model":"test"}),
    dict(scope, op="prepare_failed", retryable=True, reason="retry"),
    dict(scope, op="output", sequence=1, output_type="stream.delta", payload={"text":"hello"}),
    dict(scope, op="tool_call", tool_call_id="call-1", name="search", input={"query":"x"}),
    dict(scope, op="terminal", sequence=2, outcome="completed"),
    dict(scope, op="cancel_ack", sequence=3, accepted=True),
]
for frame in frames:
    sys.stdout.write(json.dumps(frame) + "\n")
sys.stdout.flush()
for line in sys.stdin:
    if json.loads(line).get("op") == "shutdown":
        sys.exit(0)
`

const driverCommandWorkerScript = driverScriptPreamble + `
for line in sys.stdin:
    frame = json.loads(line)
    if frame.get("op") == "shutdown":
        sys.exit(0)
    if frame.get("op") != "cancel":
        continue
    unrelated = dict(frame)
    unrelated["op"] = "cancel_ack"
    unrelated.pop("session_version", None)
    unrelated["correlation_id"] = "unrelated-correlation"
    unrelated["accepted"] = True
    sys.stdout.write(json.dumps(unrelated) + "\n")
    ack = dict(frame)
    ack["op"] = "cancel_ack"
    ack.pop("session_version", None)
    ack["accepted"] = True
    sys.stdout.write(json.dumps(ack) + "\n")
    sys.stdout.flush()
`

const driverDelayedCancelWorkerScript = driverScriptPreamble + `
count = 0
for line in sys.stdin:
    frame = json.loads(line)
    if frame.get("op") == "shutdown":
        sys.exit(0)
    if frame.get("op") != "cancel":
        continue
    count += 1
    if count == 1:
        time.sleep(0.1)
        continue
    ack = dict(frame)
    ack["op"] = "cancel_ack"
    ack.pop("session_version", None)
    ack["accepted"] = True
    sys.stdout.write(json.dumps(ack) + "\n")
    sys.stdout.flush()
`

const driverCloseOnCancelWorkerScript = driverScriptPreamble + `
for line in sys.stdin:
    if json.loads(line).get("op") == "cancel":
        sys.exit(7)
`
