package driver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	"github.com/bamanoz/tabula/internal/runtime/host/policy/bare"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

func TestEnsureIsIdempotentAndFencesOlderGeneration(t *testing.T) {
	worker := newFakeWorker()
	pol := &fakePolicy{workers: []*fakeWorker{worker}}
	s := New("kernel", fakeStore{plugin: driverPlugin()}, pol, nil)
	t.Cleanup(func() { _ = s.Close() })

	first := desired(2)
	if err := s.Ensure(context.Background(), first); err != nil {
		t.Fatalf("Ensure first: %v", err)
	}
	if err := s.Ensure(context.Background(), first); err != nil {
		t.Fatalf("Ensure duplicate: %v", err)
	}
	if pol.count() != 1 {
		t.Fatalf("spawn count = %d, want 1", pol.count())
	}
	if err := s.Ensure(context.Background(), desired(1)); !errors.Is(err, ErrStaleGeneration) {
		t.Fatalf("stale Ensure error = %v", err)
	}

	started := receiveState(t, s.Events(), wire.DriverLifecycleStarted)
	ready := receiveState(t, s.Events(), wire.DriverLifecycleReady)
	if started.DriverInstanceID == "" || ready.DriverInstanceID != started.DriverInstanceID {
		t.Fatalf("lifecycle identity mismatch: started=%+v ready=%+v", started, ready)
	}
	init := worker.initFrame()
	if init.SessionID != "session" || init.AgentSpecRevision != "sha256:spec" || init.DesiredGeneration != 2 || init.DriverInstanceID != started.DriverInstanceID {
		t.Fatalf("unexpected init: %+v", init)
	}
}

func TestEnsureNewGenerationStopsOldWorkerBeforeReplacement(t *testing.T) {
	first := newFakeWorker()
	second := newFakeWorker()
	pol := &fakePolicy{workers: []*fakeWorker{first, second}}
	s := New("kernel", fakeStore{plugin: driverPlugin()}, pol, nil)
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Ensure(context.Background(), desired(1)); err != nil {
		t.Fatal(err)
	}
	receiveState(t, s.Events(), wire.DriverLifecycleStarted)
	receiveState(t, s.Events(), wire.DriverLifecycleReady)
	if err := s.Ensure(context.Background(), desired(2)); err != nil {
		t.Fatal(err)
	}
	if !first.shutdownCalled() || pol.count() != 2 {
		t.Fatalf("old shutdown=%v spawn count=%d", first.shutdownCalled(), pol.count())
	}
	if second.initFrame().DesiredGeneration != 2 {
		t.Fatalf("replacement init = %+v", second.initFrame())
	}
}

func TestCrashAndStopEmitDistinctLifecycle(t *testing.T) {
	crashed := newFakeWorker()
	stopped := newFakeWorker()
	pol := &fakePolicy{workers: []*fakeWorker{crashed, stopped}}
	s := New("kernel", fakeStore{plugin: driverPlugin()}, pol, nil)
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Ensure(context.Background(), desired(1)); err != nil {
		t.Fatal(err)
	}
	receiveState(t, s.Events(), wire.DriverLifecycleStarted)
	receiveState(t, s.Events(), wire.DriverLifecycleReady)
	crashed.exit(policy.ExitInfo{Code: 9, Message: "worker stderr captured: RuntimeError"}, errors.New("crashed"))
	exited := receiveState(t, s.Events(), wire.DriverLifecycleExited)
	if exited.ExitCode != 9 || !strings.Contains(exited.Message, "crashed") || !strings.Contains(exited.Message, "RuntimeError") {
		t.Fatalf("exit = %+v", exited)
	}

	if err := s.Ensure(context.Background(), desired(2)); err != nil {
		t.Fatal(err)
	}
	receiveState(t, s.Events(), wire.DriverLifecycleStarted)
	receiveState(t, s.Events(), wire.DriverLifecycleReady)
	if err := s.Stop(context.Background(), "tenant", "session"); err != nil {
		t.Fatal(err)
	}
	shutdown := stopped.shutdownFrame()
	if shutdown.Reason != "driver stopped" || shutdown.Final {
		t.Fatalf("Stop shutdown = %+v, want non-final driver stop", shutdown)
	}
	receiveState(t, s.Events(), wire.DriverLifecycleStopped)
}

func TestTurnAssignPermitCancelTranslateAndFenceExactWorker(t *testing.T) {
	first := newFakeWorker()
	second := newFakeWorker()
	pol := &fakePolicy{workers: []*fakeWorker{first, second}}
	s := New("kernel", fakeStore{plugin: driverPlugin()}, pol, nil)
	t.Cleanup(func() { _ = s.Close() })

	firstDesired := desired(4)
	if err := s.Ensure(context.Background(), firstDesired); err != nil {
		t.Fatal(err)
	}
	secondDesired := desired(4)
	secondDesired.TenantID = "other-tenant"
	secondDesired.SessionID = "other-session"
	if err := s.Ensure(context.Background(), secondDesired); err != nil {
		t.Fatal(err)
	}
	fence := wire.DriverFence{DriverInstanceID: first.initFrame().DriverInstanceID, LeaseID: "lease-1", Generation: 4}
	ref := wire.AttemptRef{TenantID: "tenant", SessionID: "session", TurnID: "turn-1", AttemptID: "attempt-1", CorrelationID: "turn-1", Fence: fence}

	assignResult, err := s.TurnAssign(context.Background(), wire.TurnAssign{
		Op: wire.OpTurnAssign, RequestID: "assign-1", AttemptRef: ref,
		Input: json.RawMessage(`{"prompt":"hello"}`), PreparedContext: json.RawMessage(`{"memory":true}`),
		SequenceContext: 7, SessionVersion: 11,
	})
	if err != nil || !assignResult.Accepted || assignResult.RequestID != "assign-1" {
		t.Fatalf("TurnAssign = %#v, %v", assignResult, err)
	}
	assign := first.lastAssign()
	if assign.Op != workerwire.OpAssign || assign.TenantID != "tenant" || assign.SessionID != "session" || assign.TurnID != "turn-1" || assign.AttemptID != "attempt-1" || assign.CorrelationID != "assign-1" || assign.Fence != fence || assign.Generation != 4 || string(assign.Input) != `{"prompt":"hello"}` || string(assign.PreparedContext) != `{"memory":true}` || assign.SequenceContext != 7 || assign.SessionVersion != 11 {
		t.Fatalf("worker assign = %#v", assign)
	}
	if len(second.assignFrames()) != 0 {
		t.Fatalf("assignment reached other worker: %#v", second.assignFrames())
	}

	permitResult, err := s.TurnPermit(context.Background(), wire.TurnPermit{
		Op: wire.OpTurnPermit, RequestID: "permit-1", AttemptRef: ref,
		PermitID: "permit-token", SessionVersion: 12, Cursor: 23,
	})
	if err != nil || !permitResult.Accepted || permitResult.RequestID != "permit-1" {
		t.Fatalf("TurnPermit = %#v, %v", permitResult, err)
	}
	permit := first.lastPermit()
	if permit.Op != workerwire.OpPermit || permit.CorrelationID != "permit-1" || permit.PermitID != "permit-token" || permit.Sequence != 1 || permit.SessionVersion != 12 || permit.Cursor != 23 || permit.Fence != fence {
		t.Fatalf("worker permit = %#v", permit)
	}

	for name, test := range map[string]struct {
		ref  wire.AttemptRef
		want error
	}{
		"tenant/session": {ref: wire.AttemptRef{TenantID: "tenant", SessionID: "other-session", TurnID: "turn-1", AttemptID: "attempt-1", CorrelationID: "turn-1", Fence: fence}, want: ErrWorkerNotFound},
		"generation":     {ref: wire.AttemptRef{TenantID: "tenant", SessionID: "session", TurnID: "turn-1", AttemptID: "attempt-1", CorrelationID: "turn-1", Fence: wire.DriverFence{DriverInstanceID: fence.DriverInstanceID, Generation: 3}}, want: ErrStaleGeneration},
		"instance":       {ref: wire.AttemptRef{TenantID: "tenant", SessionID: "session", TurnID: "turn-1", AttemptID: "attempt-1", CorrelationID: "turn-1", Fence: wire.DriverFence{DriverInstanceID: "stale", Generation: 4}}, want: ErrStaleInstance},
	} {
		t.Run("reject_"+name, func(t *testing.T) {
			_, err := s.TurnAssign(context.Background(), wire.TurnAssign{RequestID: "bad", AttemptRef: test.ref})
			if !errors.Is(err, test.want) {
				t.Fatalf("TurnAssign error = %v, want %v", err, test.want)
			}
		})
	}

	cancelResult, err := s.TurnCancel(context.Background(), wire.TurnCancel{Op: wire.OpTurnCancel, RequestID: "cancel-1", AttemptRef: ref, SessionVersion: 13})
	if err != nil || !cancelResult.Accepted || cancelResult.RequestID != "cancel-1" {
		t.Fatalf("TurnCancel = %#v, %v", cancelResult, err)
	}
	cancel := first.lastCancel()
	if cancel.Op != workerwire.OpCancel || cancel.CorrelationID != "cancel-1" || cancel.Sequence != 2 || cancel.SessionVersion != 13 || cancel.Fence != fence {
		t.Fatalf("worker cancel = %#v", cancel)
	}
}

func TestWorkerRegisterOutputCompletedTranslateAndResponsesCorrelate(t *testing.T) {
	worker := newFakeWorker()
	s := New("kernel", fakeStore{plugin: driverPlugin()}, &fakePolicy{workers: []*fakeWorker{worker}}, nil)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Ensure(context.Background(), desired(6)); err != nil {
		t.Fatal(err)
	}
	init := worker.initFrame()
	fence := wire.DriverFence{DriverInstanceID: init.DriverInstanceID, LeaseID: "lease-6", Generation: 6}

	worker.emit(&workerwire.WorkerRegister{Op: workerwire.OpRegister, TenantID: "tenant", SessionID: "session", CorrelationID: "register-1", ComponentID: "driver", AgentSpecRevision: "sha256:spec", DesiredGeneration: 6, DriverInstanceID: init.DriverInstanceID})
	register := receiveExecution[wire.DriverRegister](t, s.ExecutionEvents())
	if register.RequestID != "register-1" || register.TenantID != "tenant" || register.SessionID != "session" || register.DriverInstanceID != init.DriverInstanceID || register.DesiredGeneration != 6 {
		t.Fatalf("DriverRegister = %#v", register)
	}
	if err := s.DriverLeaseGranted(context.Background(), wire.DriverLeaseGranted{Op: wire.OpDriverLeaseGranted, RequestID: "register-1", TenantID: "tenant", SessionID: "session", Accepted: true, Fence: fence, HeartbeatIntervalMS: 5000}); err != nil {
		t.Fatalf("DriverLeaseGranted: %v", err)
	}
	registerAck := worker.lastRegisterAck()
	if registerAck.Op != workerwire.OpRegisterAck || registerAck.CorrelationID != "register-1" || !registerAck.Accepted || registerAck.Fence != fence || registerAck.Generation != 6 || registerAck.HeartbeatIntervalMS != 5000 {
		t.Fatalf("register ack = %#v", registerAck)
	}

	scope := workerwire.WorkerAttemptScope{TenantID: "tenant", SessionID: "session", TurnID: "turn-6", AttemptID: "attempt-6", CorrelationID: "output-1", TurnCorrelationID: "turn-6", Fence: fence, Generation: 6}
	worker.emit(&workerwire.WorkerOutput{Op: workerwire.OpOutput, WorkerAttemptScope: scope, Sequence: 3, OutputType: wire.OutputStreamDelta, Payload: json.RawMessage(`{"text":"hi"}`)})
	output := receiveExecution[wire.TurnOutput](t, s.ExecutionEvents())
	if output.RequestID != "output-1" || output.AttemptRef != (wire.AttemptRef{TenantID: "tenant", SessionID: "session", TurnID: "turn-6", AttemptID: "attempt-6", CorrelationID: "turn-6", Fence: fence}) || output.Sequence != 3 || output.OutputType != wire.OutputStreamDelta || string(output.Payload) != `{"text":"hi"}` {
		t.Fatalf("TurnOutput = %#v", output)
	}
	if err := s.DriverResult(context.Background(), wire.DriverResult{Op: wire.OpDriverResult, RequestID: "output-1", Accepted: false, ExpectedSequence: 4, Error: &wire.Error{Code: wire.ErrorProtocolError, Message: "gap"}}); err != nil {
		t.Fatalf("DriverResult: %v", err)
	}
	outputAck := worker.lastOutputAck()
	if outputAck.Op != workerwire.OpOutputAck || outputAck.CorrelationID != "output-1" || outputAck.Sequence != 3 || outputAck.Accepted || outputAck.ExpectedSequence != 4 || outputAck.Error == nil || outputAck.Error.Message != "gap" {
		t.Fatalf("output ack = %#v", outputAck)
	}

	scope.CorrelationID = "tool-1"
	worker.emit(&workerwire.WorkerToolCall{Op: workerwire.OpToolCall, WorkerAttemptScope: scope, ToolCallID: "call-1", Name: "search", Input: json.RawMessage(`{"query":"x"}`)})
	toolCall := receiveExecution[wire.TurnToolCall](t, s.ExecutionEvents())
	if toolCall.RequestID != "tool-1" || toolCall.ToolCallID != "call-1" || toolCall.Name != "search" || string(toolCall.Input) != `{"query":"x"}` {
		t.Fatalf("TurnToolCall = %#v", toolCall)
	}
	if err := s.DriverResult(context.Background(), wire.DriverResult{Op: wire.OpDriverResult, RequestID: "tool-1", Accepted: true}); err != nil {
		t.Fatalf("tool call DriverResult: %v", err)
	}
	if err := s.TurnToolResult(context.Background(), wire.TurnToolResult{Op: wire.OpTurnToolResult, RequestID: "tool-1", AttemptRef: toolCall.AttemptRef, ToolCallID: "call-1", Output: "found", Artifact: json.RawMessage(`{"ref":"a"}`)}); err != nil {
		t.Fatalf("TurnToolResult: %v", err)
	}
	toolResult := worker.lastToolResult()
	if toolResult.Op != workerwire.OpToolResult || toolResult.CorrelationID != "tool-1" || toolResult.ToolCallID != "call-1" || toolResult.Output != "found" || string(toolResult.Artifact) != `{"ref":"a"}` {
		t.Fatalf("worker tool result = %#v", toolResult)
	}

	scope.CorrelationID = "tool-2"
	worker.emit(&workerwire.WorkerToolCall{Op: workerwire.OpToolCall, WorkerAttemptScope: scope, ToolCallID: "call-2", Name: "read", Input: json.RawMessage(`{"path":"x"}`)})
	toolCall = receiveExecution[wire.TurnToolCall](t, s.ExecutionEvents())
	if toolCall.RequestID != "tool-2" || toolCall.ToolCallID != "call-2" || toolCall.Name != "read" || string(toolCall.Input) != `{"path":"x"}` {
		t.Fatalf("second TurnToolCall = %#v", toolCall)
	}
	if err := s.DriverResult(context.Background(), wire.DriverResult{Op: wire.OpDriverResult, RequestID: "tool-2", Accepted: true}); err != nil {
		t.Fatalf("second tool call DriverResult: %v", err)
	}

	scope.CorrelationID = "completed-1"
	worker.emit(&workerwire.WorkerTerminal{Op: workerwire.OpTerminal, WorkerAttemptScope: scope, Sequence: 4, Outcome: workerwire.TerminalCompleted})
	completed := receiveExecution[wire.TurnCompleted](t, s.ExecutionEvents())
	if completed.RequestID != "completed-1" || completed.Sequence != 4 || completed.TurnID != "turn-6" || completed.Fence != fence {
		t.Fatalf("TurnCompleted = %#v", completed)
	}
	if err := s.DriverResult(context.Background(), wire.DriverResult{Op: wire.OpDriverResult, RequestID: "completed-1", Accepted: true}); err != nil {
		t.Fatalf("completed DriverResult: %v", err)
	}
}

func TestLateDriverResponseAfterWorkerExitIsIgnored(t *testing.T) {
	worker := newFakeWorker()
	s := New("kernel", fakeStore{plugin: driverPlugin()}, &fakePolicy{workers: []*fakeWorker{worker}}, nil)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Ensure(context.Background(), desired(6)); err != nil {
		t.Fatal(err)
	}
	init := worker.initFrame()
	worker.emit(&workerwire.WorkerRegister{
		Op: workerwire.OpRegister, TenantID: "tenant", SessionID: "session", CorrelationID: "register-late",
		ComponentID: "driver", AgentSpecRevision: "sha256:spec", DesiredGeneration: 6, DriverInstanceID: init.DriverInstanceID,
	})
	receiveExecution[wire.DriverRegister](t, s.ExecutionEvents())
	worker.exit(policy.ExitInfo{Code: 9}, errors.New("crashed"))
	receiveState(t, s.Events(), wire.DriverLifecycleExited)
	if err := s.DriverLeaseGranted(context.Background(), wire.DriverLeaseGranted{
		Op: wire.OpDriverLeaseGranted, RequestID: "register-late", TenantID: "tenant", SessionID: "session",
		Accepted: false, Error: &wire.Error{Code: wire.ErrorProtocolError},
	}); err != nil {
		t.Fatalf("late DriverLeaseGranted: %v", err)
	}
}

func TestWorkerRegistrationRejectionReturnsWorkerAck(t *testing.T) {
	worker := newFakeWorker()
	s := New("kernel", fakeStore{plugin: driverPlugin()}, &fakePolicy{workers: []*fakeWorker{worker}}, nil)
	t.Cleanup(func() { _ = s.Close() })
	if err := s.Ensure(context.Background(), desired(6)); err != nil {
		t.Fatal(err)
	}
	init := worker.initFrame()
	worker.emit(&workerwire.WorkerRegister{
		Op: workerwire.OpRegister, TenantID: "tenant", SessionID: "session", CorrelationID: "register-rejected",
		ComponentID: "driver", AgentSpecRevision: "sha256:spec", DesiredGeneration: 6, DriverInstanceID: init.DriverInstanceID,
	})
	receiveExecution[wire.DriverRegister](t, s.ExecutionEvents())
	if err := s.DriverLeaseGranted(context.Background(), wire.DriverLeaseGranted{
		Op: wire.OpDriverLeaseGranted, RequestID: "register-rejected", TenantID: "tenant", SessionID: "session",
		Accepted: false, Error: &wire.Error{Code: wire.ErrorProtocolError},
	}); err != nil {
		t.Fatalf("DriverLeaseGranted rejection: %v", err)
	}
	ack := worker.lastRegisterAck()
	if ack.Accepted || ack.Generation != 6 || ack.Error == nil || ack.Error.Code != string(wire.ErrorProtocolError) {
		t.Fatalf("register rejection ack = %#v", ack)
	}
}

func TestCloseSendsFinalShutdown(t *testing.T) {
	worker := newFakeWorker()
	s := New("kernel", fakeStore{plugin: driverPlugin()}, &fakePolicy{workers: []*fakeWorker{worker}}, nil)
	if err := s.Ensure(context.Background(), desired(1)); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	shutdown := worker.shutdownFrame()
	if shutdown.Reason != "runtime stopping" || !shutdown.Final {
		t.Fatalf("Close shutdown = %+v, want final runtime stop", shutdown)
	}
}

func TestCloseAfterWorkerExitClosesChannelsWithoutPanic(t *testing.T) {
	worker := newFakeWorker()
	s := New("kernel", fakeStore{plugin: driverPlugin()}, &fakePolicy{workers: []*fakeWorker{worker}}, nil)
	if err := s.Ensure(context.Background(), desired(1)); err != nil {
		t.Fatal(err)
	}
	worker.exit(policy.ExitInfo{Code: 0}, nil)
	receiveState(t, s.Events(), wire.DriverLifecycleExited)

	done := make(chan error, 1)
	go func() { done <- s.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Close: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Close leaked after worker exit")
	}
	if _, ok := <-s.ExecutionEvents(); ok {
		t.Fatal("execution events channel remains open")
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

func TestBarePolicyStartsAndStopsInstalledDriverWorker(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "driver.sh")
	body := `#!/bin/sh
IFS= read -r init
printf '%s\n' '{"op":"init_ack","ready":true,"tools":[],"subscriptions":[]}'
IFS= read -r shutdown
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	plugin := driverPlugin()
	plugin.RootDir = dir
	plugin.Worker.Command = []string{script}
	s := New("kernel", fakeStore{plugin: plugin}, bare.New(), nil)
	t.Cleanup(func() { _ = s.Close() })

	if err := s.Ensure(context.Background(), desired(1)); err != nil {
		t.Fatal(err)
	}
	receiveState(t, s.Events(), wire.DriverLifecycleStarted)
	receiveState(t, s.Events(), wire.DriverLifecycleReady)
	if err := s.Stop(context.Background(), "tenant", "session"); err != nil {
		t.Fatal(err)
	}
	receiveState(t, s.Events(), wire.DriverLifecycleStopped)
}

func TestConcurrentEnsureSpawnsOneWorker(t *testing.T) {
	worker := newFakeWorker()
	pol := &fakePolicy{workers: []*fakeWorker{worker}}
	s := New("kernel", fakeStore{plugin: driverPlugin()}, pol, nil)
	t.Cleanup(func() { _ = s.Close() })

	var wg sync.WaitGroup
	errs := make(chan error, 16)
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs <- s.Ensure(context.Background(), desired(1))
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	if pol.count() != 1 {
		t.Fatalf("spawn count = %d, want 1", pol.count())
	}
}

func desired(generation uint64) wire.DriverEnsure {
	return wire.DriverEnsure{
		Op:                wire.OpDriverEnsure,
		RequestID:         "request",
		TenantID:          "tenant",
		SessionID:         "session",
		ComponentID:       "driver",
		AgentSpecRevision: "sha256:spec",
		DesiredGeneration: generation,
	}
}

func driverPlugin() manifest.Plugin {
	return manifest.Plugin{
		ID:          "driver",
		Name:        "Driver",
		Version:     "1.0.0",
		WorkerMode:  wire.WorkerModeWarm,
		WorkerScope: wire.WorkerScopeSession,
		Kind:        &manifest.Kind{Name: "driver", Singleton: true},
		Worker:      &manifest.Worker{Command: []string{"driver"}, Mode: wire.WorkerModeWarm, Scope: wire.WorkerScopeSession},
	}
}

type fakeStore struct{ plugin manifest.Plugin }

func (s fakeStore) GetForTenant(_, id string) (manifest.Plugin, bool) {
	return s.plugin, id == s.plugin.ID
}

type fakePolicy struct {
	mu      sync.Mutex
	workers []*fakeWorker
	spawns  int
}

func (p *fakePolicy) Spawn(context.Context, policy.SpawnReq) (policy.Worker, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	worker := p.workers[p.spawns]
	p.spawns++
	return worker, nil
}

func (p *fakePolicy) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.spawns
}

type fakeWorker struct {
	mu              sync.Mutex
	alive           bool
	init            workerwire.WorkerInit
	shutdown        bool
	shutdownRequest policy.Shutdown
	done            chan struct{}
	events          chan policy.WorkerAsyncEvent
	exitInfo        policy.ExitInfo
	exitErr         error
	assigns         []workerwire.WorkerAssign
	permits         []workerwire.WorkerPermit
	cancels         []workerwire.WorkerCancel
	cancelAck       workerwire.WorkerCancelAck
	registerACK     []workerwire.WorkerRegisterAck
	outputACK       []workerwire.WorkerOutputAck
	toolResults     []workerwire.WorkerToolResult
	once            sync.Once
}

func newFakeWorker() *fakeWorker {
	return &fakeWorker{alive: true, done: make(chan struct{}), events: make(chan policy.WorkerAsyncEvent, 16), cancelAck: workerwire.WorkerCancelAck{Accepted: true}}
}

func (w *fakeWorker) Init(_ context.Context, in workerwire.WorkerInit) (workerwire.WorkerInitAck, error) {
	w.mu.Lock()
	w.init = in
	w.mu.Unlock()
	return workerwire.WorkerInitAck{Op: workerwire.OpInitAck, Ready: true, Tools: []wire.ToolSpec{}, Subscriptions: []wire.HookSpec{}}, nil
}
func (w *fakeWorker) Call(context.Context, workerwire.WorkerCall) (workerwire.WorkerResult, error) {
	return workerwire.WorkerResult{}, nil
}
func (w *fakeWorker) HookEvent(context.Context, workerwire.WorkerEvent) (*workerwire.WorkerEventReply, error) {
	return nil, nil
}
func (w *fakeWorker) RegisterAck(_ context.Context, ack workerwire.WorkerRegisterAck) error {
	w.mu.Lock()
	w.registerACK = append(w.registerACK, ack)
	w.mu.Unlock()
	return nil
}
func (w *fakeWorker) ReadyAck(context.Context, workerwire.WorkerReadyAck) error { return nil }
func (w *fakeWorker) HeartbeatAck(context.Context, workerwire.WorkerHeartbeatAck) error {
	return nil
}
func (w *fakeWorker) Assign(_ context.Context, assign workerwire.WorkerAssign) error {
	w.mu.Lock()
	w.assigns = append(w.assigns, assign)
	w.mu.Unlock()
	return nil
}
func (w *fakeWorker) Permit(_ context.Context, permit workerwire.WorkerPermit) error {
	w.mu.Lock()
	w.permits = append(w.permits, permit)
	w.mu.Unlock()
	return nil
}
func (w *fakeWorker) OutputAck(_ context.Context, ack workerwire.WorkerOutputAck) error {
	w.mu.Lock()
	w.outputACK = append(w.outputACK, ack)
	w.mu.Unlock()
	return nil
}
func (w *fakeWorker) ToolResult(_ context.Context, result workerwire.WorkerToolResult) error {
	w.mu.Lock()
	w.toolResults = append(w.toolResults, result)
	w.mu.Unlock()
	return nil
}
func (w *fakeWorker) Cancel(_ context.Context, cancel workerwire.WorkerCancel) (workerwire.WorkerCancelAck, error) {
	w.mu.Lock()
	w.cancels = append(w.cancels, cancel)
	ack := w.cancelAck
	w.mu.Unlock()
	ack.Op = workerwire.OpCancelAck
	ack.WorkerAttemptScope = cancel.WorkerAttemptScope
	ack.Sequence = cancel.Sequence
	return ack, nil
}
func (w *fakeWorker) CancelAck(context.Context, workerwire.WorkerCancelAck) error { return nil }
func (w *fakeWorker) Shutdown(_ context.Context, shutdown policy.Shutdown) error {
	w.mu.Lock()
	w.shutdown = true
	w.shutdownRequest = shutdown
	w.mu.Unlock()
	w.exit(policy.ExitInfo{}, nil)
	return nil
}
func (w *fakeWorker) Wait() (policy.ExitInfo, error) {
	<-w.done
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.exitInfo, w.exitErr
}
func (w *fakeWorker) IsAlive() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.alive
}
func (w *fakeWorker) Events() <-chan policy.WorkerAsyncEvent { return w.events }
func (w *fakeWorker) emit(frame any) {
	w.events <- policy.WorkerAsyncEvent{Frame: frame}
}
func (w *fakeWorker) assignFrames() []workerwire.WorkerAssign {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]workerwire.WorkerAssign(nil), w.assigns...)
}
func (w *fakeWorker) lastAssign() workerwire.WorkerAssign {
	frames := w.assignFrames()
	return frames[len(frames)-1]
}
func (w *fakeWorker) lastPermit() workerwire.WorkerPermit {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.permits[len(w.permits)-1]
}
func (w *fakeWorker) lastCancel() workerwire.WorkerCancel {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cancels[len(w.cancels)-1]
}
func (w *fakeWorker) lastRegisterAck() workerwire.WorkerRegisterAck {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.registerACK[len(w.registerACK)-1]
}
func (w *fakeWorker) lastOutputAck() workerwire.WorkerOutputAck {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.outputACK[len(w.outputACK)-1]
}
func (w *fakeWorker) lastToolResult() workerwire.WorkerToolResult {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.toolResults[len(w.toolResults)-1]
}
func (w *fakeWorker) initFrame() workerwire.WorkerInit {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.init
}
func (w *fakeWorker) shutdownCalled() bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.shutdown
}
func (w *fakeWorker) shutdownFrame() policy.Shutdown {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.shutdownRequest
}
func (w *fakeWorker) exit(info policy.ExitInfo, err error) {
	w.once.Do(func() {
		w.mu.Lock()
		w.alive = false
		w.exitInfo = info
		w.exitErr = err
		w.mu.Unlock()
		close(w.done)
	})
}

func receiveExecution[T any](t *testing.T, events <-chan any) T {
	t.Helper()
	select {
	case event := <-events:
		frame, ok := event.(T)
		if !ok {
			t.Fatalf("execution event type = %T, want %T", event, *new(T))
		}
		return frame
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %T", *new(T))
		return *new(T)
	}
}

func receiveState(t *testing.T, events <-chan wire.DriverLifecycle, state wire.DriverLifecycleState) wire.DriverLifecycle {
	t.Helper()
	timer := time.NewTimer(time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if event.State == state {
				return event
			}
		case <-timer.C:
			t.Fatalf("timed out waiting for %s", state)
		}
	}
}
