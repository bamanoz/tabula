package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	"github.com/bamanoz/tabula/internal/agent"
	runtimeapi "github.com/bamanoz/tabula/internal/runtime"
	"github.com/bamanoz/tabula/internal/runtime/wire"
)

var (
	ErrExecutionRuntimeUnavailable = errors.New("execution runtime unavailable")
	ErrExecutionDeliveryRejected   = errors.New("execution delivery rejected")
)

type executionRuntimeResolver interface {
	RuntimeForTenant(tenantID, runtimeID string) (runtimeapi.RuntimeConn, wire.ErrorCode, error)
}

// ExecutionCoordinator bridges durable v4 session decisions to the runtime
// connection authenticated as the durable driver's owner.
type ExecutionCoordinator struct {
	repository  agent.SessionRepository
	execution   *agent.TurnExecutionService
	recovery    *agent.RecoveryService
	runtimes    executionRuntimeResolver
	prepareTurn func(agent.Assignment) (json.RawMessage, error)

	locksMu sync.Mutex
	locks   map[agent.SessionKey]*sync.Mutex
}

func newExecutionCoordinator(repository agent.SessionRepository, execution *agent.TurnExecutionService, recovery *agent.RecoveryService, runtimes executionRuntimeResolver) (*ExecutionCoordinator, error) {
	if repository == nil || execution == nil || recovery == nil || runtimes == nil {
		return nil, fmt.Errorf("%w: execution coordinator dependencies are required", agent.ErrInvalidArgument)
	}
	return &ExecutionCoordinator{repository: repository, execution: execution, recovery: recovery, runtimes: runtimes, locks: make(map[agent.SessionKey]*sync.Mutex)}, nil
}

// AssignNext durably assigns the FIFO head and then delivers it to its owning runtime.
func (c *ExecutionCoordinator) AssignNext(ctx context.Context, key agent.SessionKey, preparedContext json.RawMessage) (agent.Assignment, error) {
	unlock := c.lock(key)
	defer unlock()
	assignment, err := c.execution.AssignNext(ctx, key, preparedContext)
	if err != nil {
		return agent.Assignment{}, err
	}
	assignment, err = c.bindPreparedContext(ctx, assignment, preparedContext)
	if err != nil {
		return assignment, err
	}
	if err := c.deliverAssignment(ctx, assignment); err != nil {
		return assignment, err
	}
	return assignment, nil
}

// DeliverCurrent redelivers durable assigned, prepared, permitted, or cancelling state.
// It is safe for duplicate readiness and runtime reconnect reconciliation.
func (c *ExecutionCoordinator) DeliverCurrent(ctx context.Context, key agent.SessionKey) error {
	unlock := c.lock(key)
	defer unlock()
	return c.deliverCurrentLocked(ctx, key)
}

// Prepared commits driver preparation before committing and delivering permission.
func (c *ExecutionCoordinator) Prepared(ctx context.Context, key agent.SessionKey, runtimeID, turnID, attemptID string, fence agent.Fence) (agent.ExecutionPermit, error) {
	unlock := c.lock(key)
	defer unlock()
	if _, err := c.execution.Prepare(ctx, key, runtimeID, turnID, attemptID, fence); err != nil {
		return agent.ExecutionPermit{}, err
	}
	permit, err := c.execution.Permit(ctx, key, turnID, attemptID, fence)
	if err != nil {
		return agent.ExecutionPermit{}, err
	}
	if err := c.deliverPermit(ctx, permit); err != nil {
		return permit, err
	}
	return permit, nil
}

// Cancel commits cancellation intent and routes active cancellation to the owner.
func (c *ExecutionCoordinator) Cancel(ctx context.Context, request agent.CancelRequest) (agent.Record, error) {
	unlock := c.lock(request.Key)
	defer unlock()
	record, err := c.recovery.Cancel(ctx, request)
	if err != nil {
		return agent.Record{}, err
	}
	if turn, ok := record.State.Turns[request.TurnID]; ok && turn.Status == agent.TurnCancelling {
		if err := c.deliverCancellation(ctx, record, turn); err != nil {
			return record, err
		}
	}
	return record, nil
}

// ReconcileRuntime redelivers active durable execution state owned by runtimeID.
func (c *ExecutionCoordinator) ReconcileRuntime(ctx context.Context, tenantIDs []string, runtimeID string) error {
	for _, tenantID := range tenantIDs {
		after := ""
		for {
			page, err := c.repository.List(ctx, agent.SessionQuery{TenantID: tenantID, Statuses: []agent.SessionStatus{agent.SessionOpen}, IncludeArchived: false, AfterSessionID: after, Limit: 100})
			if err != nil {
				return fmt.Errorf("list execution sessions for tenant %s: %w", tenantID, err)
			}
			for _, record := range page.Records {
				if record.State.Driver.RuntimeID != runtimeID {
					continue
				}
				if err := c.DeliverCurrent(ctx, record.Key); err != nil && !errors.Is(err, agent.ErrNoAssignableTurn) {
					return fmt.Errorf("redeliver execution %s/%s: %w", record.Key.TenantID, record.Key.SessionID, err)
				}
			}
			if page.NextAfterSessionID == "" {
				break
			}
			after = page.NextAfterSessionID
		}
	}
	return nil
}

func (c *ExecutionCoordinator) deliverCurrentLocked(ctx context.Context, key agent.SessionKey) error {
	record, err := c.repository.Load(ctx, key)
	if err != nil {
		return err
	}
	if record.State.Driver.Status != agent.DriverReady {
		return agent.ErrNoAssignableTurn
	}
	if record.State.ActiveTurnID == "" {
		if len(record.State.Queue) == 0 {
			return agent.ErrNoAssignableTurn
		}
		assignment, err := c.execution.AssignNext(ctx, key, nil)
		if err != nil {
			return err
		}
		assignment, err = c.bindPreparedContext(ctx, assignment, nil)
		if err != nil {
			return err
		}
		return c.deliverAssignment(ctx, assignment)
	}
	turn, ok := record.State.Turns[record.State.ActiveTurnID]
	if !ok || turn.ActiveAttemptID == "" {
		return fmt.Errorf("%w: active turn or attempt missing", agent.ErrCorruptState)
	}
	attempt, ok := coordinatorAttempt(turn, turn.ActiveAttemptID)
	if !ok {
		return fmt.Errorf("%w: active attempt missing", agent.ErrCorruptState)
	}
	if turn.Status == agent.TurnCancelling {
		return c.deliverCancellation(ctx, record, turn)
	}
	switch attempt.Status {
	case agent.AttemptAssigned, agent.AttemptPrepared:
		assignment, err := c.assignmentFromOutbox(ctx, record, attempt.ID)
		if err != nil {
			return err
		}
		assignment, err = c.bindPreparedContext(ctx, assignment, nil)
		if err != nil {
			return err
		}
		return c.deliverAssignment(ctx, assignment)
	case agent.AttemptPermitted:
		permit, err := c.permitFromOutbox(ctx, record, attempt.ID)
		if err != nil {
			return err
		}
		return c.deliverPermit(ctx, permit)
	default:
		return agent.ErrNoAssignableTurn
	}
}

func (c *ExecutionCoordinator) bindPreparedContext(ctx context.Context, assignment agent.Assignment, preparedContext json.RawMessage) (agent.Assignment, error) {
	if assignment.PreparedContextSet {
		return assignment, nil
	}
	if len(preparedContext) == 0 {
		preparedContext = json.RawMessage(`{}`)
		if c.prepareTurn != nil {
			var err error
			preparedContext, err = c.prepareTurn(assignment)
			if err != nil {
				return assignment, err
			}
		}
	}
	return c.execution.SetPreparedContext(ctx, assignment.Key, assignment.TurnID, assignment.AttemptID, assignment.Fence, preparedContext)
}

func (c *ExecutionCoordinator) deliverAssignment(ctx context.Context, assignment agent.Assignment) error {
	conn, err := c.executionConn(ctx, assignment.Key)
	if err != nil {
		return err
	}
	result, err := conn.TurnAssign(ctx, wire.TurnAssign{
		Op: wire.OpTurnAssign, RequestID: "turn.assign/" + assignment.AttemptID,
		AttemptRef: wire.AttemptRef{TenantID: assignment.Key.TenantID, SessionID: assignment.Key.SessionID, TurnID: assignment.TurnID, AttemptID: assignment.AttemptID, CorrelationID: assignment.TurnCorrelationID, Fence: wireDriverFence(assignment.Fence)},
		Input:      assignment.Input, PreparedContext: assignment.PreparedContext, SequenceContext: uint64(assignment.SequenceContext), SessionVersion: assignment.SessionVersion,
	})
	return executionDeliveryResult("turn.assign", result, err)
}

func (c *ExecutionCoordinator) deliverPermit(ctx context.Context, permit agent.ExecutionPermit) error {
	conn, err := c.executionConn(ctx, permit.Key)
	if err != nil {
		return err
	}
	result, err := conn.TurnPermit(ctx, wire.TurnPermit{
		Op: wire.OpTurnPermit, RequestID: "turn.permit/" + permit.PermitID,
		AttemptRef: wire.AttemptRef{TenantID: permit.Key.TenantID, SessionID: permit.Key.SessionID, TurnID: permit.TurnID, AttemptID: permit.AttemptID, CorrelationID: permit.TurnCorrelationID, Fence: wireDriverFence(permit.Fence)},
		PermitID:   permit.PermitID, SessionVersion: permit.SessionVersion, Cursor: uint64(permit.Cursor),
	})
	return executionDeliveryResult("turn.permit", result, err)
}

func (c *ExecutionCoordinator) deliverCancellation(ctx context.Context, record agent.Record, turn agent.Turn) error {
	attempt, ok := coordinatorAttempt(turn, turn.ActiveAttemptID)
	if !ok {
		return fmt.Errorf("%w: cancelling attempt missing", agent.ErrCorruptState)
	}
	conn, err := c.executionConnForRecord(record)
	if err != nil {
		return err
	}
	fence := agent.Fence{DriverInstanceID: attempt.DriverInstanceID, LeaseID: attempt.LeaseID, Generation: attempt.DriverGeneration}
	result, err := conn.TurnCancel(ctx, wire.TurnCancel{
		Op: wire.OpTurnCancel, RequestID: "turn.cancel/" + attempt.ID,
		AttemptRef:     wire.AttemptRef{TenantID: record.Key.TenantID, SessionID: record.Key.SessionID, TurnID: turn.ID, AttemptID: attempt.ID, CorrelationID: turn.ID, Fence: wireDriverFence(fence)},
		SessionVersion: record.Version,
	})
	return executionDeliveryResult("turn.cancel", result, err)
}

func (c *ExecutionCoordinator) executionConn(ctx context.Context, key agent.SessionKey) (runtimeapi.DriverExecutionConn, error) {
	record, err := c.repository.Load(ctx, key)
	if err != nil {
		return nil, err
	}
	return c.executionConnForRecord(record)
}

func (c *ExecutionCoordinator) executionConnForRecord(record agent.Record) (runtimeapi.DriverExecutionConn, error) {
	runtimeID := record.State.Driver.RuntimeID
	if runtimeID == "" {
		return nil, fmt.Errorf("%w: durable driver has no runtime", ErrExecutionRuntimeUnavailable)
	}
	conn, _, err := c.runtimes.RuntimeForTenant(record.Key.TenantID, runtimeID)
	if err != nil {
		return nil, fmt.Errorf("%w: runtime %q: %v", ErrExecutionRuntimeUnavailable, runtimeID, err)
	}
	executionConn, ok := conn.(runtimeapi.DriverExecutionConn)
	if !ok {
		return nil, fmt.Errorf("%w: runtime %q does not support driver execution", ErrExecutionRuntimeUnavailable, runtimeID)
	}
	return executionConn, nil
}

func (c *ExecutionCoordinator) assignmentFromOutbox(ctx context.Context, record agent.Record, attemptID string) (agent.Assignment, error) {
	var assignment agent.Assignment
	if err := c.readOutboxPayload(ctx, record.Key, "turn.assign/"+attemptID, &assignment); err != nil {
		return agent.Assignment{}, err
	}
	turn := record.State.Turns[record.State.ActiveTurnID]
	attempt, ok := coordinatorAttempt(turn, attemptID)
	if !ok {
		return agent.Assignment{}, fmt.Errorf("%w: attempt %q missing", agent.ErrCorruptState, attemptID)
	}
	assignment.TurnCorrelationID = turn.ID
	assignment.PreparedContext = append(json.RawMessage(nil), attempt.PreparedContext...)
	assignment.PreparedContextSet = attempt.PreparedContextSet
	assignment.SessionVersion = record.Version
	assignment.SequenceContext = record.Cursor
	return assignment, nil
}

func (c *ExecutionCoordinator) permitFromOutbox(ctx context.Context, record agent.Record, attemptID string) (agent.ExecutionPermit, error) {
	turn := record.State.Turns[record.State.ActiveTurnID]
	attempt, _ := coordinatorAttempt(turn, attemptID)
	var permit agent.ExecutionPermit
	if err := c.readOutboxPayload(ctx, record.Key, "turn.permit/"+attempt.PermitID, &permit); err != nil {
		return agent.ExecutionPermit{}, err
	}
	permit.TurnCorrelationID = turn.ID
	permit.SessionVersion = record.Version
	permit.Cursor = record.Cursor
	return permit, nil
}

func (c *ExecutionCoordinator) readOutboxPayload(ctx context.Context, key agent.SessionKey, id string, target any) error {
	var cursor agent.Cursor
	for {
		messages, err := c.repository.ReadOutbox(ctx, key, cursor, 100)
		if err != nil {
			return err
		}
		for _, stored := range messages {
			if stored.Message.ID == id {
				if err := json.Unmarshal(stored.Message.Payload, target); err != nil {
					return fmt.Errorf("%w: decode outbox %q: %v", agent.ErrCorruptState, id, err)
				}
				return nil
			}
			cursor = stored.Cursor
		}
		if len(messages) < 100 {
			return fmt.Errorf("%w: outbox %q missing", agent.ErrCorruptState, id)
		}
	}
}

func (c *ExecutionCoordinator) lock(key agent.SessionKey) func() {
	c.locksMu.Lock()
	mutex := c.locks[key]
	if mutex == nil {
		mutex = &sync.Mutex{}
		c.locks[key] = mutex
	}
	c.locksMu.Unlock()
	mutex.Lock()
	return mutex.Unlock
}

func coordinatorAttempt(turn agent.Turn, attemptID string) (agent.Attempt, bool) {
	for _, attempt := range turn.Attempts {
		if attempt.ID == attemptID {
			return attempt, true
		}
	}
	return agent.Attempt{}, false
}

func executionDeliveryResult(operation string, result wire.DriverResult, err error) error {
	if err != nil {
		return fmt.Errorf("%w: %s: %v", ErrExecutionRuntimeUnavailable, operation, err)
	}
	if !result.Accepted {
		if result.Error == nil {
			return fmt.Errorf("%w: %s without wire error", ErrExecutionDeliveryRejected, operation)
		}
		return fmt.Errorf("%w: %s: %s", ErrExecutionDeliveryRejected, operation, result.Error.Code)
	}
	return nil
}
