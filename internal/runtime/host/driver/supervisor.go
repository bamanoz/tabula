// Package driver supervises session-scoped driver worker processes.
package driver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/bamanoz/tabula/internal/runtime/host/manifest"
	"github.com/bamanoz/tabula/internal/runtime/host/policy"
	"github.com/bamanoz/tabula/internal/runtime/wire"
	workerwire "github.com/bamanoz/tabula/internal/runtime/worker/wire"
)

var (
	ErrComponentNotFound = errors.New("driver component not found")
	ErrNotDriver         = errors.New("component is not a driver")
	ErrWorkerNotFound    = errors.New("driver worker not found")
	ErrStaleGeneration   = errors.New("stale driver generation")
	ErrStaleInstance     = errors.New("stale driver instance")
)

type manifestStore interface {
	GetForTenant(tenantID, id string) (manifest.Plugin, bool)
}

type workerRun struct {
	desired    wire.DriverEnsure
	worker     policy.Worker
	instanceID string
	stopped    bool

	done      chan struct{}
	doneOnce  sync.Once
	attemptMu sync.Mutex
	sequences map[string]uint64
}

type workerEntry struct {
	mu      sync.Mutex
	desired wire.DriverEnsure
	current *workerRun
}

type responseKind uint8

const (
	responseRegister responseKind = iota + 1
	responseReady
	responseHeartbeat
	responseOutput
	responseCancelAck
	responseNone
)

type pendingResponse struct {
	run      *workerRun
	kind     responseKind
	scope    workerwire.WorkerAttemptScope
	sequence uint64
}

// Supervisor owns at most one driver worker per tenant/session.
type Supervisor struct {
	kernelID string
	store    manifestStore
	policy   policy.PluginExecPolicy
	env      func(string) map[string]string

	mu              sync.Mutex
	workers         map[string]*workerEntry
	events          chan wire.DriverLifecycle
	executionEvents chan any
	pending         map[string]pendingResponse
	done            chan struct{}
	forwarders      sync.WaitGroup
	eventMu         sync.RWMutex
	channelsClosed  bool
	closed          bool
}

// New creates an empty driver supervisor.
func New(kernelID string, store manifestStore, policy policy.PluginExecPolicy, env func(string) map[string]string) *Supervisor {
	return &Supervisor{
		kernelID:        kernelID,
		store:           store,
		policy:          policy,
		env:             env,
		workers:         make(map[string]*workerEntry),
		events:          make(chan wire.DriverLifecycle, 128),
		executionEvents: make(chan any, 128),
		pending:         make(map[string]pendingResponse),
		done:            make(chan struct{}),
	}
}

// Events returns runtime-observed driver process lifecycle facts.
func (s *Supervisor) Events() <-chan wire.DriverLifecycle {
	if s == nil {
		return nil
	}
	return s.events
}

// ExecutionEvents returns worker-originated driver execution frames translated
// to Runtime API frames.
func (s *Supervisor) ExecutionEvents() <-chan any {
	if s == nil {
		return nil
	}
	return s.executionEvents
}

// Ensure converges one session to the requested component, revision, and generation.
func (s *Supervisor) Ensure(ctx context.Context, desired wire.DriverEnsure) error {
	if s == nil || s.store == nil || s.policy == nil {
		return errors.New("driver supervisor is not configured")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	key := workerKey(desired.TenantID, desired.SessionID)
	entry, err := s.entry(key)
	if err != nil {
		return err
	}

	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.desired.DesiredGeneration > desired.DesiredGeneration {
		return fmt.Errorf("%w: current %d, requested %d", ErrStaleGeneration, entry.desired.DesiredGeneration, desired.DesiredGeneration)
	}
	if sameDesired(entry.desired, desired) && entry.current != nil && entry.current.worker.IsAlive() {
		return nil
	}
	if entry.current != nil {
		entry.current.stopped = true
		if err := entry.current.worker.Shutdown(ctx, policy.Shutdown{Reason: "worker stopped"}); err != nil {
			return fmt.Errorf("stop replaced driver worker: %w", err)
		}
		entry.current = nil
	}

	plugin, ok := s.store.GetForTenant(desired.TenantID, desired.ComponentID)
	if !ok {
		return fmt.Errorf("%w: %s", ErrComponentNotFound, desired.ComponentID)
	}
	if !plugin.IsDriver() {
		return fmt.Errorf("%w: %s", ErrNotDriver, desired.ComponentID)
	}
	instanceID, err := newInstanceID()
	if err != nil {
		return fmt.Errorf("create driver instance id: %w", err)
	}
	worker, err := s.policy.Spawn(ctx, s.spawnReq(plugin, desired))
	if err != nil {
		return fmt.Errorf("start driver worker: %w", err)
	}
	run := &workerRun{
		desired:    desired,
		worker:     worker,
		instanceID: instanceID,
		done:       make(chan struct{}),
		sequences:  make(map[string]uint64),
	}
	entry.desired = desired
	entry.current = run
	s.emit(lifecycle(desired, instanceID, wire.DriverLifecycleStarted, policy.ExitInfo{}, ""))

	_, err = worker.Init(ctx, workerwire.WorkerInit{
		KernelID:          s.kernelID,
		TenantID:          desired.TenantID,
		TargetID:          desired.ComponentID,
		SessionID:         desired.SessionID,
		AgentSpecRevision: desired.AgentSpecRevision,
		DriverInstanceID:  instanceID,
		DesiredGeneration: desired.DesiredGeneration,
		Manifest:          plugin.RawJSON(),
	})
	if err != nil {
		entry.current = nil
		run.stopped = true
		s.emit(lifecycle(desired, instanceID, wire.DriverLifecycleInitFailed, policy.ExitInfo{}, err.Error()))
		_ = worker.Shutdown(context.Background(), policy.Shutdown{Reason: "worker stopped"})
		return fmt.Errorf("initialize driver worker: %w", err)
	}
	s.emit(lifecycle(desired, instanceID, wire.DriverLifecycleReady, policy.ExitInfo{}, ""))
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		entry.current = nil
		run.stopped = true
		_ = worker.Shutdown(context.Background(), policy.Shutdown{Reason: "worker stopped"})
		return errors.New("driver supervisor is closed")
	}
	s.forwarders.Add(2)
	s.mu.Unlock()
	go func() {
		defer s.forwarders.Done()
		s.forwardWorkerEvents(run)
	}()
	go func() {
		defer s.forwarders.Done()
		s.watch(key, entry, run)
	}()
	return nil
}

// TurnAssign validates the runtime fence and delivers one assignment to the
// exact session-scoped worker.
func (s *Supervisor) TurnAssign(ctx context.Context, in wire.TurnAssign) (wire.DriverResult, error) {
	entry, run, err := s.currentRun(in.TenantID, in.SessionID, in.Fence)
	if err != nil {
		return wire.DriverResult{}, err
	}
	defer entry.mu.Unlock()
	scope := workerAttemptScope(in.AttemptRef, in.RequestID)
	if err := run.worker.Assign(ctx, workerwire.WorkerAssign{
		Op:                 workerwire.OpAssign,
		WorkerAttemptScope: scope,
		Input:              cloneRaw(in.Input),
		PreparedContext:    cloneRaw(in.PreparedContext),
		SequenceContext:    in.SequenceContext,
		SessionVersion:     in.SessionVersion,
	}); err != nil {
		return wire.DriverResult{}, fmt.Errorf("deliver turn assignment: %w", err)
	}
	run.setSequence(scope, 0)
	return acceptedResult(in.RequestID), nil
}

// TurnPermit validates the runtime fence and delivers execution authority to
// the exact session-scoped worker.
func (s *Supervisor) TurnPermit(ctx context.Context, in wire.TurnPermit) (wire.DriverResult, error) {
	entry, run, err := s.currentRun(in.TenantID, in.SessionID, in.Fence)
	if err != nil {
		return wire.DriverResult{}, err
	}
	defer entry.mu.Unlock()
	scope := workerAttemptScope(in.AttemptRef, in.RequestID)
	sequence := run.nextSequence(scope)
	if err := run.worker.Permit(ctx, workerwire.WorkerPermit{
		Op:                 workerwire.OpPermit,
		WorkerAttemptScope: scope,
		PermitID:           in.PermitID,
		Sequence:           sequence,
		SessionVersion:     in.SessionVersion,
		Cursor:             in.Cursor,
	}); err != nil {
		return wire.DriverResult{}, fmt.Errorf("deliver turn permit: %w", err)
	}
	run.setSequence(scope, sequence)
	return acceptedResult(in.RequestID), nil
}

// TurnToolResult routes one terminal tool result to the exact requesting worker.
func (s *Supervisor) TurnToolResult(ctx context.Context, in wire.TurnToolResult) error {
	entry, run, err := s.currentRun(in.TenantID, in.SessionID, in.Fence)
	if err != nil {
		return err
	}
	defer entry.mu.Unlock()
	return run.worker.ToolResult(ctx, workerwire.WorkerToolResult{
		Op:                 workerwire.OpToolResult,
		WorkerAttemptScope: workerAttemptScope(in.AttemptRef, in.RequestID),
		ToolCallID:         in.ToolCallID,
		Output:             in.Output,
		Artifact:           cloneRaw(in.Artifact),
		Truncated:          in.Truncated,
		SyntheticFailure:   in.SyntheticFailure,
	})
}

// TurnCancel validates the runtime fence, requests cooperative cancellation,
// and translates the correlated worker acknowledgement.
func (s *Supervisor) TurnCancel(ctx context.Context, in wire.TurnCancel) (wire.DriverResult, error) {
	entry, run, err := s.currentRun(in.TenantID, in.SessionID, in.Fence)
	if err != nil {
		return wire.DriverResult{}, err
	}
	defer entry.mu.Unlock()
	scope := workerAttemptScope(in.AttemptRef, in.RequestID)
	sequence := run.nextSequence(scope)
	ack, err := run.worker.Cancel(ctx, workerwire.WorkerCancel{
		Op:                 workerwire.OpCancel,
		WorkerAttemptScope: scope,
		Sequence:           sequence,
		SessionVersion:     in.SessionVersion,
	})
	if err != nil {
		return wire.DriverResult{}, fmt.Errorf("cancel driver turn: %w", err)
	}
	result := wire.DriverResult{Op: wire.OpDriverResult, RequestID: in.RequestID, Accepted: ack.Accepted}
	if !ack.Accepted {
		result.Error = runtimeError(ack.Error)
	}
	return result, nil
}

// DriverLeaseGranted routes one kernel registration response back to the
// worker that originated its request.
func (s *Supervisor) DriverLeaseGranted(ctx context.Context, in wire.DriverLeaseGranted) error {
	pending, err := s.takePending(in.RequestID, responseRegister)
	if errors.Is(err, ErrWorkerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	if pending.run.desired.TenantID != in.TenantID || pending.run.desired.SessionID != in.SessionID {
		return fmt.Errorf("%w: lease response scope does not match worker", ErrWorkerNotFound)
	}
	if !in.Accepted {
		return pending.run.worker.RegisterAck(ctx, workerwire.WorkerRegisterAck{
			Op:            workerwire.OpRegisterAck,
			TenantID:      in.TenantID,
			SessionID:     in.SessionID,
			CorrelationID: in.RequestID,
			Accepted:      false,
			Generation:    pending.run.desired.DesiredGeneration,
			Error:         workerError(in.Error),
		})
	}
	if err := validateRunFence(pending.run, in.Fence); err != nil {
		return err
	}
	return pending.run.worker.RegisterAck(ctx, workerwire.WorkerRegisterAck{
		Op:                  workerwire.OpRegisterAck,
		TenantID:            in.TenantID,
		SessionID:           in.SessionID,
		CorrelationID:       in.RequestID,
		Accepted:            true,
		Fence:               in.Fence,
		Generation:          in.Fence.Generation,
		HeartbeatIntervalMS: in.HeartbeatIntervalMS,
	})
}

// DriverResult routes one correlated kernel mutation response back to its
// originating worker when worker protocol defines an acknowledgement.
func (s *Supervisor) DriverResult(ctx context.Context, in wire.DriverResult) error {
	pending, err := s.takePending(in.RequestID, 0)
	if errors.Is(err, ErrWorkerNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	workerError := workerError(in.Error)
	switch pending.kind {
	case responseReady:
		return pending.run.worker.ReadyAck(ctx, workerwire.WorkerReadyAck{
			Op: workerwire.OpReadyAck, TenantID: pending.run.desired.TenantID,
			SessionID: pending.run.desired.SessionID, CorrelationID: in.RequestID,
			Fence: pending.scope.Fence, Generation: pending.scope.Generation,
			Accepted: in.Accepted, Error: workerError,
		})
	case responseHeartbeat:
		return pending.run.worker.HeartbeatAck(ctx, workerwire.WorkerHeartbeatAck{
			Op: workerwire.OpHeartbeatAck, TenantID: pending.run.desired.TenantID,
			SessionID: pending.run.desired.SessionID, CorrelationID: in.RequestID,
			Fence: pending.scope.Fence, Generation: pending.scope.Generation,
			Sequence: pending.sequence, Accepted: in.Accepted, Error: workerError,
		})
	case responseOutput:
		return pending.run.worker.OutputAck(ctx, workerwire.WorkerOutputAck{
			Op: workerwire.OpOutputAck, WorkerAttemptScope: pending.scope,
			Sequence: pending.sequence, Accepted: in.Accepted,
			ExpectedSequence: in.ExpectedSequence, Error: workerError,
		})
	case responseNone:
		return nil
	default:
		return wire.ProtocolErrorf("unsupported pending driver response kind %d", pending.kind)
	}
}

// Stop converges one session-scoped driver worker to absent.
func (s *Supervisor) Stop(ctx context.Context, tenantID, sessionID string) error {
	return s.stop(ctx, tenantID, sessionID, policy.Shutdown{Reason: "driver stopped"})
}

func (s *Supervisor) stop(ctx context.Context, tenantID, sessionID string, shutdown policy.Shutdown) error {
	if s == nil {
		return nil
	}
	key := workerKey(tenantID, sessionID)
	s.mu.Lock()
	entry := s.workers[key]
	s.mu.Unlock()
	if entry == nil {
		return nil
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.current == nil {
		s.deleteEntry(key, entry)
		return nil
	}
	entry.current.stopped = true
	if err := entry.current.worker.Shutdown(ctx, shutdown); err != nil {
		return fmt.Errorf("stop driver worker: %w", err)
	}
	entry.current = nil
	return nil
}

// Reconcile ensures every desired worker and stops workers omitted from desired.
func (s *Supervisor) Reconcile(ctx context.Context, desired []wire.DriverEnsure) error {
	wanted := make(map[string]wire.DriverEnsure, len(desired))
	for _, item := range desired {
		wanted[workerKey(item.TenantID, item.SessionID)] = item
	}
	for _, key := range s.keys() {
		if _, ok := wanted[key]; ok {
			continue
		}
		tenantID, sessionID := splitWorkerKey(key)
		if err := s.Stop(ctx, tenantID, sessionID); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(wanted))
	for key := range wanted {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := s.Ensure(ctx, wanted[key]); err != nil {
			return err
		}
	}
	return nil
}

// Close stops all supervised driver workers.
func (s *Supervisor) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.done)
	keys := make([]string, 0, len(s.workers))
	for key := range s.workers {
		keys = append(keys, key)
	}
	s.mu.Unlock()
	for _, key := range keys {
		tenantID, sessionID := splitWorkerKey(key)
		if err := s.stop(context.Background(), tenantID, sessionID, policy.Shutdown{Reason: "runtime stopping", Final: true}); err != nil {
			return err
		}
	}
	s.forwarders.Wait()
	s.eventMu.Lock()
	s.channelsClosed = true
	close(s.events)
	close(s.executionEvents)
	s.eventMu.Unlock()
	return nil
}

func (s *Supervisor) entry(key string) (*workerEntry, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, errors.New("driver supervisor is closed")
	}
	entry := s.workers[key]
	if entry == nil {
		entry = &workerEntry{}
		s.workers[key] = entry
	}
	return entry, nil
}

func (s *Supervisor) keys() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	keys := make([]string, 0, len(s.workers))
	for key := range s.workers {
		keys = append(keys, key)
	}
	return keys
}

func (s *Supervisor) deleteEntry(key string, entry *workerEntry) {
	s.mu.Lock()
	if s.workers[key] == entry {
		delete(s.workers, key)
	}
	s.mu.Unlock()
}

func (s *Supervisor) spawnReq(plugin manifest.Plugin, desired wire.DriverEnsure) policy.SpawnReq {
	req := policy.SpawnReq{
		KernelID:    s.kernelID,
		TenantID:    desired.TenantID,
		TargetID:    desired.ComponentID,
		SessionID:   desired.SessionID,
		HarnessKind: plugin.Capability().HarnessKind,
		Command:     plugin.LaunchCommand(),
		Runtime:     plugin.Runtime,
		Entry:       plugin.Entry,
		Manifest:    plugin.RawJSON(),
		WorkingDir:  plugin.RootDir,
		Mode:        policy.SpawnModeWarm,
	}
	if s.env != nil {
		req.Env = s.env(desired.TenantID)
	}
	return req
}

func (s *Supervisor) forwardWorkerEvents(run *workerRun) {
	events := run.worker.Events()
	if events == nil {
		return
	}
	for {
		select {
		case <-run.done:
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			if event.Err != nil || event.Frame == nil {
				return
			}
			frame, pending, err := translateWorkerEvent(run, event.Frame)
			if err != nil {
				continue
			}
			if pending != nil {
				if err := s.addPending(frameRequestID(frame), *pending); err != nil {
					continue
				}
			}
			select {
			case s.executionEvents <- frame:
			case <-run.done:
				if pending != nil {
					s.removePending(frameRequestID(frame), run)
				}
				return
			case <-s.done:
				if pending != nil {
					s.removePending(frameRequestID(frame), run)
				}
				return
			}
		}
	}
}

func (s *Supervisor) watch(key string, entry *workerEntry, run *workerRun) {
	info, err := run.worker.Wait()
	run.closeDone()
	s.removeRunPending(run)
	entry.mu.Lock()
	if entry.current == run {
		entry.current = nil
	}
	entry.mu.Unlock()
	state := wire.DriverLifecycleExited
	if run.stopped {
		state = wire.DriverLifecycleStopped
	}
	message := strings.TrimSpace(info.Message)
	if err != nil {
		waitMessage := strings.TrimSpace(err.Error())
		switch {
		case message == "":
			message = waitMessage
		case waitMessage != "" && !strings.Contains(message, waitMessage) && !strings.Contains(waitMessage, message):
			message = waitMessage + "; " + message
		case strings.Contains(waitMessage, message):
			message = waitMessage
		}
	}
	s.emit(lifecycle(run.desired, run.instanceID, state, info, message))
	s.deleteEntry(key, entry)
}

func (s *Supervisor) emit(event wire.DriverLifecycle) {
	s.eventMu.RLock()
	defer s.eventMu.RUnlock()
	if s.channelsClosed {
		return
	}
	select {
	case s.events <- event:
	case <-s.done:
	}
}

func (s *Supervisor) currentRun(tenantID, sessionID string, fence wire.DriverFence) (*workerEntry, *workerRun, error) {
	if s == nil {
		return nil, nil, ErrWorkerNotFound
	}
	s.mu.Lock()
	entry := s.workers[workerKey(tenantID, sessionID)]
	s.mu.Unlock()
	if entry == nil {
		return nil, nil, fmt.Errorf("%w: %s/%s", ErrWorkerNotFound, tenantID, sessionID)
	}
	entry.mu.Lock()
	if entry.current == nil || !entry.current.worker.IsAlive() {
		entry.mu.Unlock()
		return nil, nil, fmt.Errorf("%w: %s/%s", ErrWorkerNotFound, tenantID, sessionID)
	}
	if err := validateRunFence(entry.current, fence); err != nil {
		entry.mu.Unlock()
		return nil, nil, err
	}
	return entry, entry.current, nil
}

func validateRunFence(run *workerRun, fence wire.DriverFence) error {
	if fence.DriverInstanceID != run.instanceID {
		return fmt.Errorf("%w: current %s, requested %s", ErrStaleInstance, run.instanceID, fence.DriverInstanceID)
	}
	if fence.Generation != run.desired.DesiredGeneration {
		return fmt.Errorf("%w: current desired %d, requested fence %d", ErrStaleGeneration, run.desired.DesiredGeneration, fence.Generation)
	}
	return nil
}

func (s *Supervisor) addPending(requestID string, pending pendingResponse) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errors.New("driver supervisor is closed")
	}
	if _, exists := s.pending[requestID]; exists {
		return wire.ProtocolErrorf("duplicate driver request_id %q", requestID)
	}
	s.pending[requestID] = pending
	return nil
}

func (s *Supervisor) takePending(requestID string, kind responseKind) (pendingResponse, error) {
	s.mu.Lock()
	pending, ok := s.pending[requestID]
	if ok && (kind == 0 || pending.kind == kind) {
		delete(s.pending, requestID)
	}
	s.mu.Unlock()
	if !ok {
		return pendingResponse{}, fmt.Errorf("%w: response request_id %q no longer has a live worker", ErrWorkerNotFound, requestID)
	}
	if kind != 0 && pending.kind != kind {
		return pendingResponse{}, wire.ProtocolErrorf("driver response request_id %q has wrong response kind", requestID)
	}
	select {
	case <-pending.run.done:
		return pendingResponse{}, fmt.Errorf("%w: worker exited before response", ErrWorkerNotFound)
	default:
		return pending, nil
	}
}

func (s *Supervisor) removePending(requestID string, run *workerRun) {
	s.mu.Lock()
	if pending, ok := s.pending[requestID]; ok && pending.run == run {
		delete(s.pending, requestID)
	}
	s.mu.Unlock()
}

func (s *Supervisor) removeRunPending(run *workerRun) {
	s.mu.Lock()
	for requestID, pending := range s.pending {
		if pending.run == run {
			delete(s.pending, requestID)
		}
	}
	s.mu.Unlock()
}

func translateWorkerEvent(run *workerRun, frame any) (any, *pendingResponse, error) {
	desired := run.desired
	select {
	case <-run.done:
		return nil, nil, ErrWorkerNotFound
	default:
	}
	switch f := frame.(type) {
	case *workerwire.WorkerRegister:
		if err := validateWorkerIdentity(run, f.TenantID, f.SessionID, f.DriverInstanceID, f.DesiredGeneration); err != nil {
			return nil, nil, err
		}
		out := wire.DriverRegister{Op: wire.OpDriverRegister, RequestID: f.CorrelationID, TenantID: f.TenantID, SessionID: f.SessionID, ComponentID: f.ComponentID, AgentSpecRevision: f.AgentSpecRevision, DesiredGeneration: f.DesiredGeneration, DriverInstanceID: f.DriverInstanceID}
		return out, &pendingResponse{run: run, kind: responseRegister}, nil
	case *workerwire.WorkerReady:
		if err := validateWorkerFrame(run, f.TenantID, f.SessionID, f.Fence, f.Generation); err != nil {
			return nil, nil, err
		}
		scope := workerwire.WorkerAttemptScope{TenantID: f.TenantID, SessionID: f.SessionID, CorrelationID: f.CorrelationID, Fence: f.Fence, Generation: f.Generation}
		out := wire.DriverReady{Op: wire.OpDriverReady, RequestID: f.CorrelationID, TenantID: f.TenantID, SessionID: f.SessionID, Fence: f.Fence}
		return out, &pendingResponse{run: run, kind: responseReady, scope: scope}, nil
	case *workerwire.WorkerHeartbeat:
		if err := validateWorkerFrame(run, f.TenantID, f.SessionID, f.Fence, f.Generation); err != nil {
			return nil, nil, err
		}
		scope := workerwire.WorkerAttemptScope{TenantID: f.TenantID, SessionID: f.SessionID, CorrelationID: f.CorrelationID, Fence: f.Fence, Generation: f.Generation}
		out := wire.DriverHeartbeat{Op: wire.OpDriverHeartbeat, RequestID: f.CorrelationID, TenantID: f.TenantID, SessionID: f.SessionID, Fence: f.Fence, Sequence: f.Sequence}
		return out, &pendingResponse{run: run, kind: responseHeartbeat, scope: scope, sequence: f.Sequence}, nil
	case *workerwire.WorkerPrepared:
		return translateAttempt(run, f.WorkerAttemptScope, responseNone, func(ref wire.AttemptRef) any {
			return wire.TurnPrepared{Op: wire.OpTurnPrepared, RequestID: f.CorrelationID, AttemptRef: ref, Plan: cloneRaw(f.Plan)}
		})
	case *workerwire.WorkerPrepareFailed:
		return translateAttempt(run, f.WorkerAttemptScope, responseNone, func(ref wire.AttemptRef) any {
			return wire.TurnPrepareFailed{Op: wire.OpTurnPrepareFailed, RequestID: f.CorrelationID, AttemptRef: ref, Retryable: f.Retryable, Reason: f.Reason}
		})
	case *workerwire.WorkerOutput:
		run.setSequence(f.WorkerAttemptScope, f.Sequence)
		return translateAttempt(run, f.WorkerAttemptScope, responseOutput, func(ref wire.AttemptRef) any {
			return wire.TurnOutput{Op: wire.OpTurnOutput, RequestID: f.CorrelationID, AttemptRef: ref, Sequence: f.Sequence, OutputType: f.OutputType, Payload: cloneRaw(f.Payload)}
		})
	case *workerwire.WorkerToolCall:
		return translateAttempt(run, f.WorkerAttemptScope, responseNone, func(ref wire.AttemptRef) any {
			return wire.TurnToolCall{Op: wire.OpTurnToolCall, RequestID: f.CorrelationID, AttemptRef: ref, ToolCallID: f.ToolCallID, Name: f.Name, Input: cloneRaw(f.Input)}
		})
	case *workerwire.WorkerTerminal:
		run.setSequence(f.WorkerAttemptScope, f.Sequence)
		return translateAttempt(run, f.WorkerAttemptScope, responseNone, func(ref wire.AttemptRef) any {
			switch f.Outcome {
			case workerwire.TerminalCompleted:
				return wire.TurnCompleted{Op: wire.OpTurnCompleted, RequestID: f.CorrelationID, AttemptRef: ref, Sequence: f.Sequence}
			case workerwire.TerminalFailed:
				return wire.TurnFailed{Op: wire.OpTurnFailed, RequestID: f.CorrelationID, AttemptRef: ref, Sequence: f.Sequence, Reason: f.Reason}
			case workerwire.TerminalCancelled:
				return wire.TurnCancelled{Op: wire.OpTurnCancelled, RequestID: f.CorrelationID, AttemptRef: ref, Sequence: f.Sequence}
			case workerwire.TerminalUncertain:
				return wire.TurnUncertain{Op: wire.OpTurnUncertain, RequestID: f.CorrelationID, AttemptRef: ref, Sequence: f.Sequence, Reason: f.Reason}
			default:
				return nil
			}
		})
	default:
		return nil, nil, wire.ProtocolErrorf("unsupported driver worker frame %T for %s/%s", frame, desired.TenantID, desired.SessionID)
	}
}

func translateAttempt(run *workerRun, scope workerwire.WorkerAttemptScope, kind responseKind, build func(wire.AttemptRef) any) (any, *pendingResponse, error) {
	if err := validateWorkerFrame(run, scope.TenantID, scope.SessionID, scope.Fence, scope.Generation); err != nil {
		return nil, nil, err
	}
	frame := build(runtimeAttemptRef(scope))
	if frame == nil {
		return nil, nil, wire.ProtocolErrorf("unsupported terminal outcome")
	}
	return frame, &pendingResponse{run: run, kind: kind, scope: scope, sequence: workerFrameSequence(frame)}, nil
}

func validateWorkerIdentity(run *workerRun, tenantID, sessionID, instanceID string, generation uint64) error {
	if tenantID != run.desired.TenantID || sessionID != run.desired.SessionID {
		return fmt.Errorf("%w: worker emitted scope %s/%s", ErrWorkerNotFound, tenantID, sessionID)
	}
	return validateRunFence(run, wire.DriverFence{DriverInstanceID: instanceID, Generation: generation})
}

func validateWorkerFrame(run *workerRun, tenantID, sessionID string, fence wire.DriverFence, generation uint64) error {
	if generation != fence.Generation {
		return fmt.Errorf("%w: frame generation %d, fence generation %d", ErrStaleGeneration, generation, fence.Generation)
	}
	return validateWorkerIdentity(run, tenantID, sessionID, fence.DriverInstanceID, generation)
}

func workerAttemptScope(ref wire.AttemptRef, correlationID string) workerwire.WorkerAttemptScope {
	return workerwire.WorkerAttemptScope{TenantID: ref.TenantID, SessionID: ref.SessionID, TurnID: ref.TurnID, AttemptID: ref.AttemptID, CorrelationID: correlationID, TurnCorrelationID: ref.CorrelationID, Fence: ref.Fence, Generation: ref.Fence.Generation}
}

func runtimeAttemptRef(scope workerwire.WorkerAttemptScope) wire.AttemptRef {
	return wire.AttemptRef{TenantID: scope.TenantID, SessionID: scope.SessionID, TurnID: scope.TurnID, AttemptID: scope.AttemptID, CorrelationID: scope.TurnCorrelationID, Fence: scope.Fence}
}

func attemptKey(scope workerwire.WorkerAttemptScope) string {
	return scope.TurnID + "\x00" + scope.AttemptID
}

func (r *workerRun) setSequence(scope workerwire.WorkerAttemptScope, sequence uint64) {
	r.attemptMu.Lock()
	if sequence > r.sequences[attemptKey(scope)] {
		r.sequences[attemptKey(scope)] = sequence
	}
	r.attemptMu.Unlock()
}

func (r *workerRun) nextSequence(scope workerwire.WorkerAttemptScope) uint64 {
	r.attemptMu.Lock()
	defer r.attemptMu.Unlock()
	return r.sequences[attemptKey(scope)] + 1
}

func (r *workerRun) closeDone() {
	r.doneOnce.Do(func() { close(r.done) })
}

func acceptedResult(requestID string) wire.DriverResult {
	return wire.DriverResult{Op: wire.OpDriverResult, RequestID: requestID, Accepted: true}
}

func cloneRaw(raw []byte) []byte { return append([]byte(nil), raw...) }

func workerError(in *wire.Error) *workerwire.WorkerErrorBody {
	if in == nil {
		return nil
	}
	return &workerwire.WorkerErrorBody{Code: string(in.Code), Message: in.Message}
}

func runtimeError(in *workerwire.WorkerErrorBody) *wire.Error {
	if in == nil {
		return &wire.Error{Code: wire.ErrorInternal, Retryable: false, Message: "worker rejected cancellation"}
	}
	return &wire.Error{Code: wire.ErrorProtocolError, Retryable: false, Message: in.Message}
}

func frameRequestID(frame any) string {
	switch f := frame.(type) {
	case wire.DriverRegister:
		return f.RequestID
	case wire.DriverReady:
		return f.RequestID
	case wire.DriverHeartbeat:
		return f.RequestID
	case wire.TurnPrepared:
		return f.RequestID
	case wire.TurnPrepareFailed:
		return f.RequestID
	case wire.TurnOutput:
		return f.RequestID
	case wire.TurnToolCall:
		return f.RequestID
	case wire.TurnCompleted:
		return f.RequestID
	case wire.TurnFailed:
		return f.RequestID
	case wire.TurnCancelled:
		return f.RequestID
	case wire.TurnUncertain:
		return f.RequestID
	default:
		return ""
	}
}

func workerFrameSequence(frame any) uint64 {
	switch f := frame.(type) {
	case wire.TurnOutput:
		return f.Sequence
	case wire.TurnCompleted:
		return f.Sequence
	case wire.TurnFailed:
		return f.Sequence
	case wire.TurnCancelled:
		return f.Sequence
	case wire.TurnUncertain:
		return f.Sequence
	default:
		return 0
	}
}

func lifecycle(desired wire.DriverEnsure, instanceID string, state wire.DriverLifecycleState, info policy.ExitInfo, message string) wire.DriverLifecycle {
	return wire.DriverLifecycle{
		Op:                wire.OpDriverLifecycle,
		TenantID:          desired.TenantID,
		SessionID:         desired.SessionID,
		ComponentID:       desired.ComponentID,
		AgentSpecRevision: desired.AgentSpecRevision,
		DesiredGeneration: desired.DesiredGeneration,
		DriverInstanceID:  instanceID,
		State:             state,
		ExitCode:          info.Code,
		Message:           message,
	}
}

func sameDesired(left, right wire.DriverEnsure) bool {
	return left.TenantID == right.TenantID &&
		left.SessionID == right.SessionID &&
		left.ComponentID == right.ComponentID &&
		left.AgentSpecRevision == right.AgentSpecRevision &&
		left.DesiredGeneration == right.DesiredGeneration
}

func workerKey(tenantID, sessionID string) string { return tenantID + "\x00" + sessionID }

func splitWorkerKey(key string) (string, string) {
	for i := range key {
		if key[i] == 0 {
			return key[:i], key[i+1:]
		}
	}
	return key, ""
}

// SpawnEnv builds the standard runtime worker environment for driver processes.
func SpawnEnv(tabulaHome, kernelURL string, pythonPath []string) func(string) map[string]string {
	return func(tenantID string) map[string]string {
		env := map[string]string{}
		if tabulaHome != "" {
			env["TABULA_HOME"] = tabulaHome
			env["TABULA_TENANT_DIR"] = filepath.Join(tabulaHome, "tenants", tenantID)
		}
		if kernelURL != "" {
			env["TABULA_URL"] = kernelURL
		}
		if venv := strings.TrimSpace(os.Getenv("TABULA_VENV")); venv != "" {
			env["TABULA_VENV"] = venv
		}
		if venv := strings.TrimSpace(os.Getenv("VIRTUAL_ENV")); venv != "" {
			env["VIRTUAL_ENV"] = venv
		}
		if len(pythonPath) != 0 {
			items := append([]string(nil), pythonPath...)
			if existing := strings.TrimSpace(os.Getenv("PYTHONPATH")); existing != "" {
				items = append(items, existing)
			}
			env["PYTHONPATH"] = strings.Join(items, string(os.PathListSeparator))
		}
		return env
	}
}

func newInstanceID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "drv_" + hex.EncodeToString(raw[:]), nil
}
