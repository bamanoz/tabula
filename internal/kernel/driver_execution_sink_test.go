package kernel

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/bamanoz/tabula/internal/agent"
	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	runtimemock "github.com/bamanoz/tabula/internal/runtime/mock"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestHubRuntimeAsyncSinkDriverExecutionHappyPath(t *testing.T) {
	hub, repository, sink := newDriverExecutionSinkTest(t)
	ctx := context.Background()
	lease := registerDriverForSinkTest(t, sink)

	ready, err := sink.DriverReady(ctx, "runtime-a", wire.DriverReady{
		Op: wire.OpDriverReady, RequestID: "ready-1", TenantID: "tenant", SessionID: "session", Fence: lease.Fence,
	})
	assertAcceptedDriverResult(t, ready, err)

	key, assignment, _ := assignDriverTurnForSinkTest(t, repository)
	prepared, err := sink.TurnPrepared(ctx, "runtime-a", wire.TurnPrepared{
		Op: wire.OpTurnPrepared, RequestID: "prepared-1", AttemptRef: sinkAttemptRef(assignment, lease.Fence),
	})
	assertAcceptedDriverResult(t, prepared, err)
	output, err := sink.TurnOutput(ctx, "runtime-a", wire.TurnOutput{
		Op: wire.OpTurnOutput, RequestID: "output-1", AttemptRef: sinkAttemptRef(assignment, lease.Fence),
		Sequence: 1, OutputType: wire.OutputStreamDelta, Payload: json.RawMessage(`{"text":"hello"}`),
	})
	assertAcceptedDriverResult(t, output, err)
	if output.Cursor == 0 || output.SessionVersion == 0 {
		t.Fatalf("output result lacks durable position: %+v", output)
	}

	completed, err := sink.TurnCompleted(ctx, "runtime-a", wire.TurnCompleted{
		Op: wire.OpTurnCompleted, RequestID: "completed-1", AttemptRef: sinkAttemptRef(assignment, lease.Fence), Sequence: 2,
	})
	assertAcceptedDriverResult(t, completed, err)
	record, err := repository.Load(ctx, key)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if record.State.Turns[assignment.TurnID].Status != agent.TurnCompleted {
		t.Fatalf("turn status = %s, want %s", record.State.Turns[assignment.TurnID].Status, agent.TurnCompleted)
	}
	if hub.driverLeases == nil || hub.turnExecutions == nil || hub.outputs == nil || hub.recovery == nil {
		t.Fatal("SetAgentSessionRepository did not construct driver execution services")
	}
}

func TestHubRuntimeAsyncSinkAssignsQueuedTurnAfterCompletion(t *testing.T) {
	hub, repository, sink := newDriverExecutionSinkTest(t)
	ctx := context.Background()
	lease := registerDriverForSinkTest(t, sink)
	ready, err := sink.DriverReady(ctx, "runtime-a", wire.DriverReady{
		Op: wire.OpDriverReady, RequestID: "ready-sequential", TenantID: "tenant", SessionID: "session", Fence: lease.Fence,
	})
	assertAcceptedDriverResult(t, ready, err)
	key, first, _ := assignDriverTurnForSinkTest(t, repository)
	processor, err := agent.NewInputProcessor(repository)
	if err != nil {
		t.Fatalf("NewInputProcessor: %v", err)
	}
	second, err := processor.Submit(ctx, agent.InputSubmitRequest{
		Key: key, CommandID: "submit-2", InputID: "input-2", Content: json.RawMessage(`{"text":"second"}`),
	})
	if err != nil {
		t.Fatalf("Submit second: %v", err)
	}
	prepared, err := sink.TurnPrepared(ctx, "runtime-a", wire.TurnPrepared{
		Op: wire.OpTurnPrepared, RequestID: "prepared-sequential", AttemptRef: sinkAttemptRef(first, lease.Fence),
	})
	assertAcceptedDriverResult(t, prepared, err)
	completed, err := sink.TurnCompleted(ctx, "runtime-a", wire.TurnCompleted{
		Op: wire.OpTurnCompleted, RequestID: "completed-sequential", AttemptRef: sinkAttemptRef(first, lease.Fence), Sequence: 1,
	})
	assertAcceptedDriverResult(t, completed, err)

	conn := hub.runtimeConn("runtime-a").(*executionRecordingConn)
	conn.mu.Lock()
	assigns := append([]wire.TurnAssign(nil), conn.assigns...)
	conn.mu.Unlock()
	if len(assigns) != 1 || assigns[0].TurnID != second.TurnID || !strings.Contains(string(assigns[0].Input), `"text":"second"`) {
		t.Fatalf("queued turn assignments = %+v, want turn %s with second input", assigns, second.TurnID)
	}
	duplicate, err := sink.TurnCompleted(ctx, "runtime-a", wire.TurnCompleted{
		Op: wire.OpTurnCompleted, RequestID: "completed-sequential", AttemptRef: sinkAttemptRef(first, lease.Fence), Sequence: 1,
	})
	assertAcceptedDriverResult(t, duplicate, err)
	conn.mu.Lock()
	assignCount := len(conn.assigns)
	conn.mu.Unlock()
	if assignCount != 1 {
		t.Fatalf("duplicate terminal redelivered next assignment: %d deliveries", assignCount)
	}
	record, err := repository.Load(ctx, key)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if record.State.ActiveTurnID != second.TurnID || record.State.Turns[second.TurnID].Status != agent.TurnPreparing {
		t.Fatalf("next turn state = active %q status %q", record.State.ActiveTurnID, record.State.Turns[second.TurnID].Status)
	}
}

func TestHubRuntimeAsyncSinkPublishesCommittedOutputToSessionSubscribers(t *testing.T) {
	hub, repository, sink := newDriverExecutionSinkTest(t)
	ctx := context.Background()
	lease := registerDriverForSinkTest(t, sink)
	ready, err := sink.DriverReady(ctx, "runtime-a", wire.DriverReady{
		Op: wire.OpDriverReady, RequestID: "ready-live-output", TenantID: "tenant", SessionID: "session", Fence: lease.Fence,
	})
	assertAcceptedDriverResult(t, ready, err)
	key, assignment, _ := assignDriverTurnForSinkTest(t, repository)
	prepared, err := sink.TurnPrepared(ctx, "runtime-a", wire.TurnPrepared{
		Op: wire.OpTurnPrepared, RequestID: "prepared-live-output", AttemptRef: sinkAttemptRef(assignment, lease.Fence),
	})
	assertAcceptedDriverResult(t, prepared, err)

	subscriber := &Client{hub: hub, name: "subscriber", sendCh: make(chan []byte, 1), done: make(chan struct{}), state: ClientProtocolReady}
	other := &Client{hub: hub, name: "other", sendCh: make(chan []byte, 1), done: make(chan struct{}), state: ClientProtocolReady}
	if !hub.addClient(subscriber) || !hub.addClient(other) {
		t.Fatal("failed to register output subscribers")
	}
	subscriber.subscribeAgentSession(key)
	other.subscribeAgentSession(agent.SessionKey{TenantID: "tenant", SessionID: "other"})

	output, err := sink.TurnOutput(ctx, "runtime-a", wire.TurnOutput{
		Op: wire.OpTurnOutput, RequestID: "output-live", AttemptRef: sinkAttemptRef(assignment, lease.Fence),
		Sequence: 1, OutputType: wire.OutputStreamDelta, Payload: json.RawMessage(`{"text":"hello"}`),
	})
	assertAcceptedDriverResult(t, output, err)

	select {
	case raw := <-subscriber.sendCh:
		var envelope ClientEnvelope
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatalf("decode live output envelope: %v", err)
		}
		if envelope.Kind != "event" || envelope.Op != "stream.delta" || envelope.TenantID != key.TenantID || envelope.SessionID != key.SessionID {
			t.Fatalf("live output envelope = %+v", envelope)
		}
		var stored clientStoredEvent
		if err := json.Unmarshal(envelope.Data, &stored); err != nil {
			t.Fatalf("decode committed live output: %v", err)
		}
		if stored.EventID == "" || stored.Cursor == "" || stored.SessionVersion != output.SessionVersion || stored.Type != "stream.delta" {
			t.Fatalf("committed live output = %+v", stored)
		}
		var data struct {
			OutputType string            `json:"output_type"`
			Sequence   uint64            `json:"sequence"`
			Payload    map[string]string `json:"payload"`
		}
		if err := json.Unmarshal(stored.Data, &data); err != nil {
			t.Fatalf("decode live output data: %v", err)
		}
		if data.OutputType != "stream.delta" || data.Sequence != 1 || data.Payload["text"] != "hello" {
			t.Fatalf("live output data = %+v", data)
		}
	default:
		t.Fatal("subscriber received no committed live output")
	}
	select {
	case raw := <-other.sendCh:
		t.Fatalf("other session received live output: %s", raw)
	default:
	}
}

func TestHubRuntimeAsyncSinkDispatchesDriverToolCallAndReturnsResult(t *testing.T) {
	hub, repository, sink := newDriverExecutionSinkTest(t)
	ctx := context.Background()
	lease := registerDriverForSinkTest(t, sink)
	ready, err := sink.DriverReady(ctx, "runtime-a", wire.DriverReady{
		Op: wire.OpDriverReady, RequestID: "ready-tool", TenantID: "tenant", SessionID: "session", Fence: lease.Fence,
	})
	assertAcceptedDriverResult(t, ready, err)
	_, assignment, _ := assignDriverTurnForSinkTest(t, repository)
	ref := sinkAttemptRef(assignment, lease.Fence)
	prepared, err := sink.TurnPrepared(ctx, "runtime-a", wire.TurnPrepared{Op: wire.OpTurnPrepared, RequestID: "prepared-tool", AttemptRef: ref})
	assertAcceptedDriverResult(t, prepared, err)
	record, err := repository.Load(ctx, assignment.Key)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	turn := record.State.Turns[assignment.TurnID]
	if turn.Status != agent.TurnExecuting || turn.Attempts[len(turn.Attempts)-1].Status != agent.AttemptPermitted {
		t.Fatalf("turn not permitted: status=%s attempt=%s", turn.Status, turn.Attempts[len(turn.Attempts)-1].Status)
	}

	result, err := sink.TurnToolCall(ctx, "runtime-a", wire.TurnToolCall{
		Op: wire.OpTurnToolCall, RequestID: "tool-request-1", AttemptRef: ref,
		ToolCallID: "call-1", Name: "missing_tool", Input: json.RawMessage(`{"value":1}`),
	})
	if err != nil || !result.Accepted || result.Error != nil || result.RequestID != "tool-request-1" {
		t.Fatalf("TurnToolCall result = %+v, %v", result, err)
	}

	conn := hub.runtimeConn("runtime-a").(*executionRecordingConn)
	conn.mu.Lock()
	defer conn.mu.Unlock()
	if len(conn.toolResults) != 1 {
		t.Fatalf("tool results = %d, want 1", len(conn.toolResults))
	}
	got := conn.toolResults[0]
	if got.RequestID != "tool-request-1" || got.ToolCallID != "call-1" || got.Output != "ERROR: unknown tool missing_tool" || got.AttemptRef != ref {
		t.Fatalf("TurnToolResult = %#v", got)
	}
}

func TestHubRuntimeAsyncSinkDeliversQueuedTurnAfterReadyAcknowledged(t *testing.T) {
	hub, repository, sink := newDriverExecutionSinkTest(t)
	ctx := context.Background()
	key := agent.SessionKey{TenantID: "tenant", SessionID: "session"}
	processor, err := agent.NewInputProcessor(repository)
	if err != nil {
		t.Fatalf("NewInputProcessor: %v", err)
	}
	if _, err := processor.Submit(ctx, agent.InputSubmitRequest{
		Key: key, CommandID: "submit-before-ready", InputID: "input-before-ready", Content: json.RawMessage(`{"text":"hello"}`),
	}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	lease := registerDriverForSinkTest(t, sink)
	readyReq := wire.DriverReady{
		Op: wire.OpDriverReady, RequestID: "ready-queued", TenantID: key.TenantID, SessionID: key.SessionID, Fence: lease.Fence,
	}
	ready, err := sink.DriverReady(ctx, "runtime-a", readyReq)
	assertAcceptedDriverResult(t, ready, err)

	conn, _, err := hub.runtimes.RuntimeForTenant(key.TenantID, "runtime-a")
	if err != nil {
		t.Fatalf("RuntimeForTenant: %v", err)
	}
	recorder := conn.(*executionRecordingConn)
	recorder.mu.Lock()
	assignsBeforeAck := len(recorder.assigns)
	recorder.mu.Unlock()
	if assignsBeforeAck != 0 {
		t.Fatalf("assignments before ready acknowledgment = %d, want 0", assignsBeforeAck)
	}

	if err := sink.DriverReadyAcknowledged(ctx, "runtime-a", readyReq); err != nil {
		t.Fatalf("DriverReadyAcknowledged: %v", err)
	}
	recorder.mu.Lock()
	assigns := append([]wire.TurnAssign(nil), recorder.assigns...)
	recorder.mu.Unlock()
	if len(assigns) != 1 || assigns[0].TenantID != key.TenantID || assigns[0].SessionID != key.SessionID {
		t.Fatalf("assignments after ready acknowledgment = %+v", assigns)
	}
}

func TestHubRuntimeAsyncSinkRejectsDriverRegistrationWithoutClosingRuntime(t *testing.T) {
	_, _, sink := newDriverExecutionSinkTest(t)
	first := registerDriverForSinkTest(t, sink)
	result, err := sink.DriverRegister(context.Background(), "runtime-a", wire.DriverRegister{
		Op: wire.OpDriverRegister, RequestID: "register-2", TenantID: "tenant", SessionID: "session",
		ComponentID: "driver", AgentSpecRevision: "sha256:spec", DesiredGeneration: first.Fence.Generation + 1, DriverInstanceID: "driver-2",
	})
	if err != nil {
		t.Fatalf("DriverRegister returned transport error: %v", err)
	}
	if result.Accepted || result.Error == nil || result.Error.Code != wire.ErrorProtocolError {
		t.Fatalf("registration rejection = %+v", result)
	}
}

func TestHubRuntimeAsyncSinkRejectsStaleRuntimeAndFence(t *testing.T) {
	_, repository, sink := newDriverExecutionSinkTest(t)
	ctx := context.Background()
	lease := registerDriverForSinkTest(t, sink)
	ready, err := sink.DriverReady(ctx, "runtime-a", wire.DriverReady{Op: wire.OpDriverReady, RequestID: "ready", TenantID: "tenant", SessionID: "session", Fence: lease.Fence})
	assertAcceptedDriverResult(t, ready, err)
	_, assignment, _ := assignDriverTurnForSinkTest(t, repository)

	tests := []struct {
		name      string
		runtimeID string
		fence     wire.DriverFence
		code      wire.ErrorCode
	}{
		{name: "runtime does not own tenant", runtimeID: "runtime-b", fence: lease.Fence, code: wire.ErrorUnauthorized},
		{name: "stale fence", runtimeID: "runtime-a", fence: wire.DriverFence{DriverInstanceID: lease.Fence.DriverInstanceID, LeaseID: lease.Fence.LeaseID, Generation: lease.Fence.Generation + 1}, code: wire.ErrorUnauthorized},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := sink.TurnPrepared(ctx, tt.runtimeID, wire.TurnPrepared{
				Op: wire.OpTurnPrepared, RequestID: "prepared-rejected", AttemptRef: sinkAttemptRef(assignment, tt.fence),
			})
			if err != nil {
				t.Fatalf("TurnPrepared returned transport error: %v", err)
			}
			if result.Accepted || result.Error == nil || result.Error.Code != tt.code || result.Error.Message != "" {
				t.Fatalf("rejected result = %+v", result)
			}
		})
	}
}

func TestHubRuntimeAsyncSinkRequestsOutputSequenceReplay(t *testing.T) {
	_, repository, sink := newDriverExecutionSinkTest(t)
	ctx := context.Background()
	lease := registerDriverForSinkTest(t, sink)
	ready, err := sink.DriverReady(ctx, "runtime-a", wire.DriverReady{Op: wire.OpDriverReady, RequestID: "ready", TenantID: "tenant", SessionID: "session", Fence: lease.Fence})
	assertAcceptedDriverResult(t, ready, err)
	_, assignment, _ := assignDriverTurnForSinkTest(t, repository)
	prepared, err := sink.TurnPrepared(ctx, "runtime-a", wire.TurnPrepared{Op: wire.OpTurnPrepared, RequestID: "prepared", AttemptRef: sinkAttemptRef(assignment, lease.Fence)})
	assertAcceptedDriverResult(t, prepared, err)
	result, err := sink.TurnOutput(ctx, "runtime-a", wire.TurnOutput{
		Op: wire.OpTurnOutput, RequestID: "output-gap", AttemptRef: sinkAttemptRef(assignment, lease.Fence),
		Sequence: 2, OutputType: wire.OutputStreamDelta, Payload: json.RawMessage(`{"text":"gap"}`),
	})
	if err != nil {
		t.Fatalf("TurnOutput returned transport error: %v", err)
	}
	if result.Accepted || result.ExpectedSequence != 1 || result.Error == nil || result.Error.Code != wire.ErrorProtocolError || !result.Error.Retryable || result.Error.Message != "" {
		t.Fatalf("replay result = %+v", result)
	}
}

func TestHubRuntimeAsyncSinkRejectsPostTerminalOutputWithExpectedSequence(t *testing.T) {
	_, repository, sink := newDriverExecutionSinkTest(t)
	ctx := context.Background()
	lease := registerDriverForSinkTest(t, sink)
	ready, err := sink.DriverReady(ctx, "runtime-a", wire.DriverReady{Op: wire.OpDriverReady, RequestID: "ready", TenantID: "tenant", SessionID: "session", Fence: lease.Fence})
	assertAcceptedDriverResult(t, ready, err)
	_, assignment, _ := assignDriverTurnForSinkTest(t, repository)
	prepared, err := sink.TurnPrepared(ctx, "runtime-a", wire.TurnPrepared{Op: wire.OpTurnPrepared, RequestID: "prepared", AttemptRef: sinkAttemptRef(assignment, lease.Fence)})
	assertAcceptedDriverResult(t, prepared, err)
	completed, err := sink.TurnCompleted(ctx, "runtime-a", wire.TurnCompleted{
		Op: wire.OpTurnCompleted, RequestID: "completed", AttemptRef: sinkAttemptRef(assignment, lease.Fence), Sequence: 1,
	})
	assertAcceptedDriverResult(t, completed, err)

	result, err := sink.TurnOutput(ctx, "runtime-a", wire.TurnOutput{
		Op: wire.OpTurnOutput, RequestID: "late-output", AttemptRef: sinkAttemptRef(assignment, lease.Fence),
		Sequence: 1, OutputType: wire.OutputStreamDelta, Payload: json.RawMessage(`{"text":"late"}`),
	})
	if err != nil {
		t.Fatalf("TurnOutput returned transport error: %v", err)
	}
	if result.Accepted || result.ExpectedSequence != 1 || result.Error == nil || result.Error.Code != wire.ErrorProtocolError || result.Error.Retryable || result.Error.Message != "" {
		t.Fatalf("post-terminal output result = %+v", result)
	}
}

func newDriverExecutionSinkTest(t *testing.T) (*Hub, *agent.MemoryRepository, hubRuntimeAsyncSink) {
	t.Helper()
	repository := agent.NewMemoryRepository()
	commitDriverSinkSession(t, repository)
	hub := NewHub(json.RawMessage(`[]`), nil)
	if err := hub.runtimes.RegisterHello("runtime-a", newExecutionRecordingConn(), nil, 0, []string{"tenant"}); err != nil {
		t.Fatalf("RegisterHello: %v", err)
	}
	hub.SetAgentSessionRepository(repository)
	return hub, repository, hubRuntimeAsyncSink{hub: hub}
}

func commitDriverSinkSession(t *testing.T, repository agent.SessionRepository) {
	t.Helper()
	state := agent.NewState()
	command := agent.CreateSession{
		Meta: agent.CommandMeta{ID: "create", Actor: agent.ActorClient}, TenantID: "tenant", SessionID: "session",
		DriverComponentID: "driver", AgentSpecRevision: "sha256:spec",
	}
	events, err := agent.Decide(state, command)
	if err != nil {
		t.Fatalf("Decide CreateSession: %v", err)
	}
	projection, err := agent.ApplyAll(state, events)
	if err != nil {
		t.Fatalf("ApplyAll CreateSession: %v", err)
	}
	digest, err := agent.DigestCommand(command)
	if err != nil {
		t.Fatalf("DigestCommand CreateSession: %v", err)
	}
	_, err = repository.Commit(context.Background(), agent.Commit{
		Key: agent.SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: command.Meta.ID,
		CommandDigest: digest, ExpectedVersion: 0, Events: events, Projection: projection,
	})
	if err != nil {
		t.Fatalf("Commit CreateSession: %v", err)
	}
}

func registerDriverForSinkTest(t *testing.T, sink hubRuntimeAsyncSink) wire.DriverLeaseGranted {
	t.Helper()
	lease, err := sink.DriverRegister(context.Background(), "runtime-a", wire.DriverRegister{
		Op: wire.OpDriverRegister, RequestID: "register-1", TenantID: "tenant", SessionID: "session",
		ComponentID: "driver", AgentSpecRevision: "sha256:spec", DesiredGeneration: 1, DriverInstanceID: "driver-1",
	})
	if err != nil {
		t.Fatalf("DriverRegister: %v", err)
	}
	if lease.Op != wire.OpDriverLeaseGranted || lease.RequestID != "register-1" || !lease.Accepted || lease.Fence.Generation != 1 || lease.HeartbeatIntervalMS != 10000 || lease.SessionVersion == 0 {
		t.Fatalf("lease = %+v", lease)
	}
	return lease
}

func assignDriverTurnForSinkTest(t *testing.T, repository agent.SessionRepository) (agent.SessionKey, agent.Assignment, *agent.TurnExecutionService) {
	t.Helper()
	ctx := context.Background()
	key := agent.SessionKey{TenantID: "tenant", SessionID: "session"}
	processor, err := agent.NewInputProcessor(repository)
	if err != nil {
		t.Fatalf("NewInputProcessor: %v", err)
	}
	if _, err := processor.Submit(ctx, agent.InputSubmitRequest{Key: key, CommandID: "submit-1", InputID: "input-1", Content: json.RawMessage(`{"text":"hello"}`)}); err != nil {
		t.Fatalf("Submit: %v", err)
	}
	execution, err := agent.NewTurnExecutionService(repository)
	if err != nil {
		t.Fatalf("NewTurnExecutionService: %v", err)
	}
	assignment, err := execution.AssignNext(ctx, key, nil)
	if err != nil {
		t.Fatalf("AssignNext: %v", err)
	}
	return key, assignment, execution
}

func sinkAttemptRef(assignment agent.Assignment, fence wire.DriverFence) wire.AttemptRef {
	return wire.AttemptRef{TenantID: assignment.Key.TenantID, SessionID: assignment.Key.SessionID, TurnID: assignment.TurnID, AttemptID: assignment.AttemptID, CorrelationID: assignment.TurnID, Fence: fence}
}

type executionRecordingConn struct {
	*runtimemock.RuntimeConn
	mu          sync.Mutex
	assigns     []wire.TurnAssign
	permits     []wire.TurnPermit
	toolResults []wire.TurnToolResult
	cancels     []wire.TurnCancel
}

var _ runtimeapi.DriverExecutionConn = (*executionRecordingConn)(nil)

func newExecutionRecordingConn() *executionRecordingConn {
	return &executionRecordingConn{RuntimeConn: runtimemock.New()}
}

func (c *executionRecordingConn) TurnAssign(_ context.Context, req wire.TurnAssign) (wire.DriverResult, error) {
	c.mu.Lock()
	c.assigns = append(c.assigns, req)
	c.mu.Unlock()
	return acceptedExecutionDelivery(req.RequestID), nil
}

func (c *executionRecordingConn) TurnPermit(_ context.Context, req wire.TurnPermit) (wire.DriverResult, error) {
	c.mu.Lock()
	c.permits = append(c.permits, req)
	c.mu.Unlock()
	return acceptedExecutionDelivery(req.RequestID), nil
}

func (c *executionRecordingConn) TurnToolResult(_ context.Context, req wire.TurnToolResult) (wire.DriverResult, error) {
	c.mu.Lock()
	c.toolResults = append(c.toolResults, req)
	c.mu.Unlock()
	return acceptedExecutionDelivery(req.RequestID), nil
}

func (c *executionRecordingConn) TurnCancel(_ context.Context, req wire.TurnCancel) (wire.DriverResult, error) {
	c.mu.Lock()
	c.cancels = append(c.cancels, req)
	c.mu.Unlock()
	return acceptedExecutionDelivery(req.RequestID), nil
}

func acceptedExecutionDelivery(requestID string) wire.DriverResult {
	return wire.DriverResult{Op: wire.OpDriverResult, RequestID: requestID, Accepted: true}
}

func assertAcceptedDriverResult(t *testing.T, result wire.DriverResult, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("driver mutation returned error: %v", err)
	}
	if !result.Accepted || result.Error != nil || result.SessionVersion == 0 || result.Cursor == 0 {
		t.Fatalf("driver result = %+v", result)
	}
}
