package conn

import (
	"context"
	"net"
	"testing"
	"time"

	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/codec"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

func TestDriverControlRoundTripAndLifecycle(t *testing.T) {
	clientNet, serverNet := net.Pipe()
	clientCodec := codec.NewReadWriteCloser(clientNet)
	serverCodec := codec.NewReadWriteCloser(serverNet)
	handler := &driverTestHandler{lifecycle: make(chan any, 1)}
	serveDone := make(chan error, 1)
	go func() { serveDone <- Serve(context.Background(), serverCodec, handler) }()

	sink := &driverTestSink{lifecycle: make(chan wire.DriverLifecycle, 1), catalog: make(chan wire.CatalogUpdate, 1)}
	client := NewWithSink(clientCodec, "local", sink)
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Fatal("Serve did not stop")
		}
	})

	if err := client.PrepareTenant(context.Background(), runtimeapi.PrepareTenantReq{RequestID: "prepare-1", TenantID: "tenant"}); err != nil {
		t.Fatal(err)
	}
	select {
	case update := <-sink.catalog:
		if update.Target.ID != "fs" || update.State != wire.CapabilityStateReady {
			t.Fatalf("catalog = %+v", update)
		}
	case <-time.After(time.Second):
		t.Fatal("prepared tenant catalog not applied before return")
	}

	if err := client.EnsureDriver(context.Background(), runtimeapi.DriverEnsureReq{
		RequestID:         "ensure-1",
		TenantID:          "tenant",
		SessionID:         "session",
		ComponentID:       "driver",
		AgentSpecRevision: "sha256:spec",
		DesiredGeneration: 2,
	}); err != nil {
		t.Fatal(err)
	}
	if handler.ensure.DesiredGeneration != 2 {
		t.Fatalf("ensure = %+v", handler.ensure)
	}

	handler.lifecycle <- wire.DriverLifecycle{
		Op:                wire.OpDriverLifecycle,
		TenantID:          "tenant",
		SessionID:         "session",
		ComponentID:       "driver",
		AgentSpecRevision: "sha256:spec",
		DesiredGeneration: 2,
		DriverInstanceID:  "drv_1",
		State:             wire.DriverLifecycleReady,
	}
	select {
	case event := <-sink.lifecycle:
		if event.DriverInstanceID != "drv_1" {
			t.Fatalf("lifecycle = %+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("lifecycle not delivered")
	}

	if err := client.StopDriver(context.Background(), runtimeapi.DriverStopReq{RequestID: "stop-1", TenantID: "tenant", SessionID: "session"}); err != nil {
		t.Fatal(err)
	}
	if handler.stop.SessionID != "session" {
		t.Fatalf("stop = %+v", handler.stop)
	}
}

type driverTestHandler struct {
	testHandler
	ensure    wire.DriverEnsure
	stop      wire.DriverStop
	lifecycle chan any
}

func (h *driverTestHandler) AsyncFrames() <-chan any { return h.lifecycle }
func (h *driverTestHandler) PrepareTenant(_ context.Context, in wire.PrepareTenant) (wire.PrepareTenantAck, error) {
	return wire.PrepareTenantAck{
		Op:        wire.OpPrepareTenantAck,
		RequestID: in.RequestID,
		Capabilities: []wire.Capability{{
			Target: wire.Target{Kind: wire.TargetKindPlugin, ID: "fs"}, Tools: []wire.ToolSpec{{Name: "read"}}, State: wire.CapabilityStateReady, Source: wire.CapabilitySourceWorker,
		}},
	}, nil
}
func (h *driverTestHandler) DriverEnsure(_ context.Context, in wire.DriverEnsure) (wire.DriverEnsureAck, error) {
	h.ensure = in
	return wire.DriverEnsureAck{Op: wire.OpDriverEnsureAck, RequestID: in.RequestID}, nil
}
func (h *driverTestHandler) DriverStop(_ context.Context, in wire.DriverStop) (wire.DriverStopAck, error) {
	h.stop = in
	return wire.DriverStopAck{Op: wire.OpDriverStopAck, RequestID: in.RequestID}, nil
}

type driverTestSink struct {
	lifecycle chan wire.DriverLifecycle
	catalog   chan wire.CatalogUpdate
}

func (s *driverTestSink) CatalogUpdated(_ string, update wire.CatalogUpdate) error {
	if s.catalog != nil {
		s.catalog <- update
	}
	return nil
}
func (s *driverTestSink) HookEventReplied(string, wire.HookEventReply) error  { return nil }
func (s *driverTestSink) PluginSent(string, wire.PluginSend) error            { return nil }
func (s *driverTestSink) PluginLogged(string, wire.PluginLog)                 {}
func (s *driverTestSink) LifecycleNoticed(string, wire.LifecycleNotice) error { return nil }
func (s *driverTestSink) RuntimeProtocolError(string, error)                  {}
func (s *driverTestSink) DriverLifecycleNoticed(_ string, event wire.DriverLifecycle) error {
	s.lifecycle <- event
	return nil
}

func TestDriverExecutionV4BidirectionalRouting(t *testing.T) {
	clientNet, serverNet := net.Pipe()
	clientCodec := codec.NewReadWriteCloser(clientNet)
	serverCodec := codec.NewReadWriteCloser(serverNet)
	frames := make(chan any, 16)
	handler := &driverExecutionTestHandler{driverTestHandler: driverTestHandler{lifecycle: frames}, responses: make(chan string, 16)}
	serveDone := make(chan error, 1)
	go func() { serveDone <- Serve(context.Background(), serverCodec, handler) }()

	sink := &driverExecutionTestSink{driverTestSink: driverTestSink{lifecycle: make(chan wire.DriverLifecycle, 1)}, calls: make(chan string, 16)}
	client := NewWithSink(clientCodec, "authenticated-runtime", sink)
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Fatal("Serve did not stop")
		}
	})

	ref := wire.AttemptRef{TenantID: "tenant", SessionID: "session", TurnID: "turn", AttemptID: "attempt", CorrelationID: "turn", Fence: wire.DriverFence{DriverInstanceID: "driver", LeaseID: "lease", Generation: 1}}
	controls := []struct {
		name string
		call func() (wire.DriverResult, error)
	}{
		{"assign", func() (wire.DriverResult, error) {
			return client.TurnAssign(context.Background(), wire.TurnAssign{Op: wire.OpTurnAssign, RequestID: "assign", AttemptRef: ref, Input: []byte(`{}`), SessionVersion: 1})
		}},
		{"permit", func() (wire.DriverResult, error) {
			return client.TurnPermit(context.Background(), wire.TurnPermit{Op: wire.OpTurnPermit, RequestID: "permit", AttemptRef: ref, PermitID: "permit", SessionVersion: 1, Cursor: 1})
		}},
		{"tool-result", func() (wire.DriverResult, error) {
			return client.TurnToolResult(context.Background(), wire.TurnToolResult{Op: wire.OpTurnToolResult, RequestID: "tool-result", AttemptRef: ref, ToolCallID: "call-1", Output: "found"})
		}},
		{"cancel", func() (wire.DriverResult, error) {
			return client.TurnCancel(context.Background(), wire.TurnCancel{Op: wire.OpTurnCancel, RequestID: "cancel", AttemptRef: ref, SessionVersion: 1})
		}},
	}
	for _, control := range controls {
		result, err := control.call()
		if err != nil || !result.Accepted || result.RequestID != control.name {
			t.Fatalf("%s result = %+v, %v", control.name, result, err)
		}
	}

	frames <- wire.DriverRegister{Op: wire.OpDriverRegister, RequestID: "register", TenantID: "tenant", SessionID: "session", ComponentID: "driver", AgentSpecRevision: "rev", DesiredGeneration: 1, DriverInstanceID: "driver"}
	mutations := []any{
		wire.DriverReady{Op: wire.OpDriverReady, RequestID: "ready", TenantID: "tenant", SessionID: "session", Fence: ref.Fence},
		wire.DriverHeartbeat{Op: wire.OpDriverHeartbeat, RequestID: "heartbeat", TenantID: "tenant", SessionID: "session", Fence: ref.Fence, Sequence: 1},
		wire.TurnPrepared{Op: wire.OpTurnPrepared, RequestID: "prepared", AttemptRef: ref},
		wire.TurnPrepareFailed{Op: wire.OpTurnPrepareFailed, RequestID: "prepare-failed", AttemptRef: ref, Retryable: true, Reason: "retry"},
		wire.TurnOutput{Op: wire.OpTurnOutput, RequestID: "output", AttemptRef: ref, Sequence: 1, OutputType: wire.OutputUsage, Payload: []byte(`{}`)},
		wire.TurnToolCall{Op: wire.OpTurnToolCall, RequestID: "tool-call", AttemptRef: ref, ToolCallID: "call-1", Name: "search", Input: []byte(`{"query":"x"}`)},
		wire.TurnCompleted{Op: wire.OpTurnCompleted, RequestID: "completed", AttemptRef: ref, Sequence: 2},
		wire.TurnFailed{Op: wire.OpTurnFailed, RequestID: "failed", AttemptRef: ref, Sequence: 2, Reason: "failed"},
		wire.TurnCancelled{Op: wire.OpTurnCancelled, RequestID: "cancelled", AttemptRef: ref, Sequence: 2},
		wire.TurnUncertain{Op: wire.OpTurnUncertain, RequestID: "uncertain", AttemptRef: ref, Sequence: 2, Reason: "unknown"},
	}
	for _, frame := range mutations {
		frames <- frame
	}
	for i := 0; i < 11; i++ {
		select {
		case runtimeID := <-sink.calls:
			if runtimeID != "authenticated-runtime" {
				t.Fatalf("runtime identity = %q", runtimeID)
			}
		case <-time.After(time.Second):
			t.Fatal("runtime-originated frame not routed")
		}
		select {
		case <-handler.responses:
		case <-time.After(time.Second):
			t.Fatal("correlated response not routed back")
		}
	}
}

func TestDriverExecutionMutationsPreserveTransportOrder(t *testing.T) {
	clientNet, serverNet := net.Pipe()
	clientCodec := codec.NewReadWriteCloser(clientNet)
	serverCodec := codec.NewReadWriteCloser(serverNet)
	frames := make(chan any, 2)
	handler := &driverExecutionTestHandler{
		driverTestHandler: driverTestHandler{lifecycle: frames},
		responses:         make(chan string, 2),
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- Serve(context.Background(), serverCodec, handler) }()

	releaseOutput := make(chan struct{}, 1)
	sink := &orderedDriverExecutionSink{
		driverExecutionTestSink: &driverExecutionTestSink{
			driverTestSink: driverTestSink{lifecycle: make(chan wire.DriverLifecycle, 1)},
			calls:          make(chan string, 1),
		},
		outputStarted: make(chan struct{}, 1),
		releaseOutput: releaseOutput,
		mutations:     make(chan string, 2),
	}
	client := NewWithSink(clientCodec, "authenticated-runtime", sink)
	t.Cleanup(func() {
		releaseOutput <- struct{}{}
		_ = client.Close()
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Fatal("Serve did not stop")
		}
	})

	ref := wire.AttemptRef{
		TenantID: "tenant", SessionID: "session", TurnID: "turn", AttemptID: "attempt", CorrelationID: "turn",
		Fence: wire.DriverFence{DriverInstanceID: "driver", LeaseID: "lease", Generation: 1},
	}
	frames <- wire.TurnOutput{
		Op: wire.OpTurnOutput, RequestID: "output", AttemptRef: ref,
		Sequence: 1, OutputType: wire.OutputStreamDelta, Payload: []byte(`{"text":"hello"}`),
	}
	select {
	case <-sink.outputStarted:
	case <-time.After(time.Second):
		t.Fatal("TurnOutput did not start")
	}
	frames <- wire.TurnCompleted{Op: wire.OpTurnCompleted, RequestID: "completed", AttemptRef: ref, Sequence: 2}
	select {
	case mutation := <-sink.mutations:
		t.Fatalf("mutation %q overtook blocked output", mutation)
	case <-time.After(50 * time.Millisecond):
	}

	releaseOutput <- struct{}{}
	for _, want := range []string{"output", "completed"} {
		select {
		case got := <-sink.mutations:
			if got != want {
				t.Fatalf("mutation = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}

func TestDriverReadyAcknowledgedAfterResponseWrite(t *testing.T) {
	clientNet, serverNet := net.Pipe()
	clientCodec := codec.NewReadWriteCloser(clientNet)
	serverCodec := codec.NewReadWriteCloser(serverNet)
	frames := make(chan any, 1)
	events := make(chan string, 2)
	handler := &driverReadyOrderingHandler{
		driverExecutionTestHandler: &driverExecutionTestHandler{
			driverTestHandler: driverTestHandler{lifecycle: frames},
			responses:         make(chan string, 1),
		},
		events: events,
	}
	serveDone := make(chan error, 1)
	go func() { serveDone <- Serve(context.Background(), serverCodec, handler) }()

	baseSink := &driverExecutionTestSink{
		driverTestSink: driverTestSink{lifecycle: make(chan wire.DriverLifecycle, 1)},
		calls:          make(chan string, 1),
	}
	var client *Conn
	sink := &driverReadyOrderingSink{
		driverExecutionTestSink: baseSink,
		afterReady: func(ctx context.Context) error {
			_, err := client.TurnAssign(ctx, wire.TurnAssign{
				Op: wire.OpTurnAssign, RequestID: "assign", AttemptRef: wire.AttemptRef{
					TenantID: "tenant", SessionID: "session", TurnID: "turn", AttemptID: "attempt", CorrelationID: "turn",
					Fence: wire.DriverFence{DriverInstanceID: "driver", LeaseID: "lease", Generation: 1},
				},
				Input: []byte(`{}`), SessionVersion: 1,
			})
			return err
		},
	}
	client = NewWithSink(clientCodec, "authenticated-runtime", sink)
	t.Cleanup(func() {
		_ = client.Close()
		select {
		case <-serveDone:
		case <-time.After(time.Second):
			t.Fatal("Serve did not stop")
		}
	})

	frames <- wire.DriverReady{
		Op: wire.OpDriverReady, RequestID: "ready", TenantID: "tenant", SessionID: "session",
		Fence: wire.DriverFence{DriverInstanceID: "driver", LeaseID: "lease", Generation: 1},
	}
	for _, want := range []string{"ready-ack", "assign"} {
		select {
		case got := <-events:
			if got != want {
				t.Fatalf("event = %q, want %q", got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timed out waiting for %s", want)
		}
	}
}

type orderedDriverExecutionSink struct {
	*driverExecutionTestSink
	outputStarted chan struct{}
	releaseOutput chan struct{}
	mutations     chan string
}

func (s *orderedDriverExecutionSink) TurnOutput(_ context.Context, _ string, in wire.TurnOutput) (wire.DriverResult, error) {
	s.outputStarted <- struct{}{}
	<-s.releaseOutput
	s.mutations <- "output"
	return acceptedDriverResult(in.RequestID), nil
}

func (s *orderedDriverExecutionSink) TurnCompleted(_ context.Context, _ string, in wire.TurnCompleted) (wire.DriverResult, error) {
	s.mutations <- "completed"
	return acceptedDriverResult(in.RequestID), nil
}

type driverExecutionTestHandler struct {
	driverTestHandler
	responses chan string
}

func (h *driverExecutionTestHandler) TurnAssign(_ context.Context, in wire.TurnAssign) (wire.DriverResult, error) {
	return acceptedDriverResult(in.RequestID), nil
}
func (h *driverExecutionTestHandler) TurnPermit(_ context.Context, in wire.TurnPermit) (wire.DriverResult, error) {
	return acceptedDriverResult(in.RequestID), nil
}
func (h *driverExecutionTestHandler) TurnToolResult(_ context.Context, in wire.TurnToolResult) (wire.DriverResult, error) {
	return acceptedDriverResult(in.RequestID), nil
}
func (h *driverExecutionTestHandler) TurnCancel(_ context.Context, in wire.TurnCancel) (wire.DriverResult, error) {
	return acceptedDriverResult(in.RequestID), nil
}
func (h *driverExecutionTestHandler) DriverLeaseGranted(_ context.Context, in wire.DriverLeaseGranted) error {
	h.responses <- in.RequestID
	return nil
}
func (h *driverExecutionTestHandler) DriverResult(_ context.Context, in wire.DriverResult) error {
	h.responses <- in.RequestID
	return nil
}

type driverReadyOrderingHandler struct {
	*driverExecutionTestHandler
	events chan string
}

func (h *driverReadyOrderingHandler) TurnAssign(_ context.Context, in wire.TurnAssign) (wire.DriverResult, error) {
	h.events <- "assign"
	return acceptedDriverResult(in.RequestID), nil
}

func (h *driverReadyOrderingHandler) DriverResult(_ context.Context, _ wire.DriverResult) error {
	h.events <- "ready-ack"
	return nil
}

type driverReadyOrderingSink struct {
	*driverExecutionTestSink
	afterReady func(context.Context) error
}

func (s *driverReadyOrderingSink) DriverReadyAcknowledged(ctx context.Context, _ string, _ wire.DriverReady) error {
	return s.afterReady(ctx)
}

func acceptedDriverResult(requestID string) wire.DriverResult {
	return wire.DriverResult{Op: wire.OpDriverResult, RequestID: requestID, Accepted: true}
}

type driverExecutionTestSink struct {
	driverTestSink
	calls chan string
}

func (s *driverExecutionTestSink) record(runtimeID string, requestID string) wire.DriverResult {
	s.calls <- runtimeID
	return acceptedDriverResult(requestID)
}
func (s *driverExecutionTestSink) DriverRegister(_ context.Context, runtimeID string, in wire.DriverRegister) (wire.DriverLeaseGranted, error) {
	s.calls <- runtimeID
	return wire.DriverLeaseGranted{Op: wire.OpDriverLeaseGranted, RequestID: in.RequestID, TenantID: in.TenantID, SessionID: in.SessionID, Accepted: true, Fence: wire.DriverFence{DriverInstanceID: in.DriverInstanceID, LeaseID: "lease", Generation: in.DesiredGeneration}, ExpiresAt: time.Now().Add(time.Minute), HeartbeatIntervalMS: 1000, SessionVersion: 1}, nil
}
func (s *driverExecutionTestSink) DriverReady(_ context.Context, id string, in wire.DriverReady) (wire.DriverResult, error) {
	return s.record(id, in.RequestID), nil
}
func (s *driverExecutionTestSink) DriverHeartbeat(_ context.Context, id string, in wire.DriverHeartbeat) (wire.DriverResult, error) {
	return s.record(id, in.RequestID), nil
}
func (s *driverExecutionTestSink) TurnPrepared(_ context.Context, id string, in wire.TurnPrepared) (wire.DriverResult, error) {
	return s.record(id, in.RequestID), nil
}
func (s *driverExecutionTestSink) TurnPrepareFailed(_ context.Context, id string, in wire.TurnPrepareFailed) (wire.DriverResult, error) {
	return s.record(id, in.RequestID), nil
}
func (s *driverExecutionTestSink) TurnOutput(_ context.Context, id string, in wire.TurnOutput) (wire.DriverResult, error) {
	return s.record(id, in.RequestID), nil
}
func (s *driverExecutionTestSink) TurnToolCall(_ context.Context, id string, in wire.TurnToolCall) (wire.DriverResult, error) {
	return s.record(id, in.RequestID), nil
}
func (s *driverExecutionTestSink) TurnCompleted(_ context.Context, id string, in wire.TurnCompleted) (wire.DriverResult, error) {
	return s.record(id, in.RequestID), nil
}
func (s *driverExecutionTestSink) TurnFailed(_ context.Context, id string, in wire.TurnFailed) (wire.DriverResult, error) {
	return s.record(id, in.RequestID), nil
}
func (s *driverExecutionTestSink) TurnCancelled(_ context.Context, id string, in wire.TurnCancelled) (wire.DriverResult, error) {
	return s.record(id, in.RequestID), nil
}
func (s *driverExecutionTestSink) TurnUncertain(_ context.Context, id string, in wire.TurnUncertain) (wire.DriverResult, error) {
	return s.record(id, in.RequestID), nil
}
