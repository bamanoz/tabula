package agent

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
)

func DigestCommand(command Command) (string, error) {
	if command == nil {
		return "", fmt.Errorf("%w: command is nil", ErrInvalidArgument)
	}
	return digestValue(command)
}

func Decide(state State, command Command) ([]Envelope, error) {
	if command == nil {
		return nil, fmt.Errorf("%w: command is nil", ErrInvalidArgument)
	}
	meta := command.Metadata()
	if meta.ID == "" {
		return nil, fmt.Errorf("%w: command id is required", ErrInvalidArgument)
	}
	digest, err := DigestCommand(command)
	if err != nil {
		return nil, fmt.Errorf("digest command: %w", err)
	}
	if previous, ok := state.AppliedCommands[meta.ID]; ok {
		if previous == digest {
			return []Envelope{}, nil
		}
		return nil, fmt.Errorf("%w: command id %q was already used", ErrCommandConflict, meta.ID)
	}

	var events []Event
	switch command := command.(type) {
	case CreateSession:
		events, err = decideCreate(state, command)
	case ArchiveSession:
		events, err = decideArchive(state, command)
	case UnarchiveSession:
		events, err = decideUnarchive(state, command)
	case SuspendSession:
		events, err = decideSuspend(state, command)
	case ResumeSession:
		events, err = decideResume(state, command)
	case DeleteSession:
		events, err = decideDelete(state, command)
	case SubmitInput:
		events, err = decideSubmitInput(state, command)
	case GrantDriverLease:
		events, err = decideGrantLease(state, command)
	case MarkDriverReady:
		events, err = decideDriverReady(state, command)
	case RenewDriverLease:
		events, err = decideRenewLease(state, command)
	case MarkDriverSuspect:
		events, err = decideDriverSuspect(state, command)
	case ReleaseDriverLease:
		events, err = decideReleaseLease(state, command)
	case ExpireDriverLease:
		events, err = decideExpireLease(state, command)
	case AssignTurn:
		events, err = decideAssignTurn(state, command)
	case SetAttemptPreparedContext:
		events, err = decideSetAttemptPreparedContext(state, command)
	case PrepareAttempt:
		events, err = decidePrepareAttempt(state, command)
	case PermitAttempt:
		events, err = decidePermitAttempt(state, command)
	case AppendAttemptOutput:
		events, err = decideAppendOutput(state, command)
	case CompleteAttempt:
		events, err = decideCompleteAttempt(state, command)
	case FailAttempt:
		events, err = decideFailAttempt(state, command)
	case ReportAttemptUncertain:
		events, err = decideReportAttemptUncertain(state, command)
	case CancelTurn:
		events, err = decideCancelTurn(state, command)
	case ConfirmCancellation:
		events, err = decideConfirmCancellation(state, command)
	case RecoverTurn:
		events, err = decideRecoverTurn(state, command)
	default:
		return nil, fmt.Errorf("%w: unsupported command %T", ErrInvalidArgument, command)
	}
	if err != nil {
		return nil, err
	}

	envelopes := make([]Envelope, 0, len(events))
	for _, event := range events {
		envelopes = append(envelopes, Envelope{CommandID: meta.ID, CommandDigest: digest, Body: event})
	}
	return envelopes, nil
}

func decideCreate(state State, command CreateSession) ([]Event, error) {
	if state.Status != "" {
		return nil, fmt.Errorf("%w: session already exists", ErrInvalidTransition)
	}
	if command.Meta.Actor != ActorClient && command.Meta.Actor != ActorKernel {
		return nil, ErrPermissionDenied
	}
	if command.TenantID == "" || command.SessionID == "" || command.DriverComponentID == "" || command.AgentSpecRevision == "" {
		return nil, fmt.Errorf("%w: tenant, session, driver component, and agent spec revision are required", ErrInvalidArgument)
	}
	return []Event{SessionCreated{
		TenantID:          command.TenantID,
		SessionID:         command.SessionID,
		DriverComponentID: command.DriverComponentID,
		AgentSpecRevision: command.AgentSpecRevision,
	}}, nil
}

func decideArchive(state State, command ArchiveSession) ([]Event, error) {
	if err := requireClientOrHuman(command.Meta); err != nil {
		return nil, err
	}
	if err := requireOpen(state); err != nil {
		return nil, err
	}
	if state.Archived {
		return []Event{}, nil
	}
	if len(state.Queue) != 0 {
		return nil, fmt.Errorf("%w: session has unfinished turns", ErrInvalidTransition)
	}
	return []Event{SessionArchived{}}, nil
}

func decideUnarchive(state State, command UnarchiveSession) ([]Event, error) {
	if err := requireClientOrHuman(command.Meta); err != nil {
		return nil, err
	}
	if err := requireOpen(state); err != nil {
		return nil, err
	}
	if !state.Archived {
		return []Event{}, nil
	}
	return []Event{SessionUnarchived{}}, nil
}

func decideSuspend(state State, command SuspendSession) ([]Event, error) {
	if err := requireClientOrHuman(command.Meta); err != nil {
		return nil, err
	}
	if err := requireOpen(state); err != nil {
		return nil, err
	}
	return []Event{SessionSuspendedEvent{}}, nil
}

func decideResume(state State, command ResumeSession) ([]Event, error) {
	if err := requireClientOrHuman(command.Meta); err != nil {
		return nil, err
	}
	if state.Status != SessionSuspended {
		return nil, fmt.Errorf("%w: session is %s", ErrInvalidTransition, state.Status)
	}
	return []Event{SessionResumedEvent{}}, nil
}

func decideDelete(state State, command DeleteSession) ([]Event, error) {
	if err := requireClientOrHuman(command.Meta); err != nil {
		return nil, err
	}
	if state.Status != SessionOpen && state.Status != SessionSuspended {
		return nil, fmt.Errorf("%w: session is %s", ErrInvalidTransition, state.Status)
	}
	if len(state.Queue) != 0 {
		return nil, fmt.Errorf("%w: session has unfinished turns", ErrInvalidTransition)
	}
	return []Event{SessionDeletedEvent{}}, nil
}

func decideSubmitInput(state State, command SubmitInput) ([]Event, error) {
	if command.Meta.Actor != ActorClient {
		return nil, ErrPermissionDenied
	}
	if err := requireOpen(state); err != nil {
		return nil, err
	}
	if state.Archived {
		return nil, fmt.Errorf("%w: session is archived", ErrInvalidTransition)
	}
	if command.InputID == "" || command.TurnID == "" || len(command.Content) == 0 {
		return nil, fmt.Errorf("%w: input id, turn id, and content are required", ErrInvalidArgument)
	}
	content, digest, err := canonicalJSON(command.Content)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid input content: %v", ErrInvalidArgument, err)
	}
	sourceDigest := command.SourceDigest
	if sourceDigest == "" {
		sourceDigest = digest
	}
	if existing, ok := state.Inputs[command.InputID]; ok {
		if existing.SourceDigest == sourceDigest {
			return []Event{}, nil
		}
		return nil, fmt.Errorf("%w: input id %q was already used", ErrInputConflict, command.InputID)
	}
	if _, ok := state.Turns[command.TurnID]; ok {
		return nil, fmt.Errorf("%w: turn id %q was already used", ErrInvalidArgument, command.TurnID)
	}
	input := Input{
		ID: command.InputID, Content: content, Digest: digest, SourceDigest: sourceDigest,
		ActorKind: command.Meta.Actor, ActorID: command.ActorID,
		AcceptanceVersion: command.AcceptanceVersion, AcceptanceCursor: command.AcceptanceCursor,
	}
	turn := Turn{ID: command.TurnID, Position: state.NextTurnPosition + 1, InputID: command.InputID, Status: TurnQueued, Attempts: []Attempt{}}
	return []Event{InputSubmitted{Input: input, Turn: turn}}, nil
}

func decideGrantLease(state State, command GrantDriverLease) ([]Event, error) {
	if command.Meta.Actor != ActorKernel {
		return nil, ErrPermissionDenied
	}
	if err := requireOpen(state); err != nil {
		return nil, err
	}
	if command.RuntimeID == "" || command.ComponentID == "" || command.AgentSpecRevision == "" || command.DriverInstanceID == "" || command.LeaseID == "" || command.Deadline.IsZero() {
		return nil, fmt.Errorf("%w: runtime, component, agent spec revision, driver instance, lease, and deadline are required", ErrInvalidArgument)
	}
	if command.ComponentID != state.DriverComponentID || command.AgentSpecRevision != state.AgentSpecRevision {
		return nil, fmt.Errorf("%w: driver does not match pinned session specification", ErrPermissionDenied)
	}
	if state.Driver.Status != DriverAbsent {
		return nil, fmt.Errorf("%w: driver lease is already active", ErrInvalidTransition)
	}
	generation := state.Driver.Generation + 1
	driver := Driver{
		Status:            DriverInitializing,
		Fence:             Fence{DriverInstanceID: command.DriverInstanceID, LeaseID: command.LeaseID, Generation: generation},
		Generation:        generation,
		RuntimeID:         command.RuntimeID,
		ComponentID:       command.ComponentID,
		AgentSpecRevision: command.AgentSpecRevision,
		Deadline:          command.Deadline,
	}
	return []Event{DriverLeaseGranted{Driver: driver}}, nil
}

func decideDriverReady(state State, command MarkDriverReady) ([]Event, error) {
	if command.Meta.Actor != ActorDriver {
		return nil, ErrPermissionDenied
	}
	if err := requireFence(state, command.Fence); err != nil {
		return nil, err
	}
	if state.Driver.Status == DriverReady {
		return []Event{}, nil
	}
	if state.Driver.Status != DriverInitializing && state.Driver.Status != DriverSuspect {
		return nil, fmt.Errorf("%w: driver is %s", ErrInvalidTransition, state.Driver.Status)
	}
	return []Event{DriverBecameReady{}}, nil
}

func decideRenewLease(state State, command RenewDriverLease) ([]Event, error) {
	if command.Meta.Actor != ActorDriver {
		return nil, ErrPermissionDenied
	}
	if err := requireFence(state, command.Fence); err != nil {
		return nil, err
	}
	if command.RuntimeID == "" || command.RuntimeID != state.Driver.RuntimeID {
		return nil, fmt.Errorf("%w: runtime identity does not own this lease", ErrPermissionDenied)
	}
	if command.HeartbeatSequence <= state.Driver.HeartbeatSequence {
		return nil, fmt.Errorf("%w: heartbeat sequence must advance", ErrInvalidArgument)
	}
	if !command.Deadline.After(state.Driver.Deadline) {
		return nil, fmt.Errorf("%w: lease deadline must advance", ErrInvalidArgument)
	}
	return []Event{DriverLeaseRenewed{Deadline: command.Deadline, HeartbeatSequence: command.HeartbeatSequence}}, nil
}

func decideDriverSuspect(state State, command MarkDriverSuspect) ([]Event, error) {
	if command.Meta.Actor != ActorKernel {
		return nil, ErrPermissionDenied
	}
	if err := requireFence(state, command.Fence); err != nil {
		return nil, err
	}
	if state.Driver.Status == DriverSuspect {
		return []Event{}, nil
	}
	return []Event{DriverBecameSuspect{}}, nil
}

func decideReleaseLease(state State, command ReleaseDriverLease) ([]Event, error) {
	if command.Meta.Actor != ActorDriver {
		return nil, ErrPermissionDenied
	}
	if err := requireFence(state, command.Fence); err != nil {
		return nil, err
	}
	if command.RuntimeID == "" || command.RuntimeID != state.Driver.RuntimeID {
		return nil, fmt.Errorf("%w: runtime identity does not own this lease", ErrPermissionDenied)
	}
	if state.ActiveTurnID != "" {
		return nil, fmt.Errorf("%w: driver has an active turn", ErrInvalidTransition)
	}
	return []Event{DriverLeaseReleased{}}, nil
}

func decideExpireLease(state State, command ExpireDriverLease) ([]Event, error) {
	if command.Meta.Actor != ActorKernel {
		return nil, ErrPermissionDenied
	}
	if state.Driver.Status == DriverAbsent {
		return []Event{}, nil
	}
	if command.Now.Before(state.Driver.Deadline) {
		return nil, fmt.Errorf("%w: driver lease has not expired", ErrInvalidTransition)
	}
	events := make([]Event, 0, 2)
	if state.ActiveTurnID != "" {
		turn := state.Turns[state.ActiveTurnID]
		attempt, ok := activeAttempt(turn)
		if ok {
			switch attempt.Status {
			case AttemptAssigned, AttemptPrepared:
				events = append(events, AttemptAbandonedEvent{TurnID: turn.ID, AttemptID: attempt.ID, Reason: "driver lease expired before permit"})
			case AttemptPermitted:
				events = append(events, AttemptUncertainEvent{TurnID: turn.ID, AttemptID: attempt.ID, Reason: "driver lease expired after permit"})
			}
		}
	}
	events = append(events, DriverLeaseExpired{})
	return events, nil
}

func decideAssignTurn(state State, command AssignTurn) ([]Event, error) {
	if command.Meta.Actor != ActorKernel {
		return nil, ErrPermissionDenied
	}
	if err := requireFence(state, command.Fence); err != nil {
		return nil, err
	}
	if state.Driver.Status != DriverReady {
		return nil, fmt.Errorf("%w: driver is not ready", ErrInvalidTransition)
	}
	if state.ActiveTurnID != "" {
		return nil, fmt.Errorf("%w: turn %q is active", ErrInvalidTransition, state.ActiveTurnID)
	}
	if len(state.Queue) == 0 || state.Queue[0] != command.TurnID {
		return nil, fmt.Errorf("%w: turn is not the queue head", ErrInvalidTransition)
	}
	turn, ok := state.Turns[command.TurnID]
	if !ok || turn.Status != TurnQueued || command.AttemptID == "" {
		return nil, fmt.Errorf("%w: turn is not assignable", ErrInvalidTransition)
	}
	if _, ok := findAttempt(turn, command.AttemptID); ok {
		return nil, fmt.Errorf("%w: attempt id %q was already used", ErrInvalidArgument, command.AttemptID)
	}
	attempt := Attempt{ID: command.AttemptID, Status: AttemptAssigned, DriverInstanceID: command.Fence.DriverInstanceID, DriverGeneration: command.Fence.Generation, LeaseID: command.Fence.LeaseID}
	return []Event{AttemptAssignedEvent{TurnID: command.TurnID, Attempt: attempt}}, nil
}

func decideSetAttemptPreparedContext(state State, command SetAttemptPreparedContext) ([]Event, error) {
	if command.Meta.Actor != ActorKernel {
		return nil, ErrPermissionDenied
	}
	turn, attempt, err := requireAttempt(state, command.TurnID, command.AttemptID, command.Fence)
	if err != nil {
		return nil, err
	}
	if turn.Status != TurnPreparing || attempt.Status != AttemptAssigned {
		return nil, fmt.Errorf("%w: attempt is not assigned", ErrInvalidTransition)
	}
	var context json.RawMessage
	if len(command.PreparedContext) != 0 {
		context, _, err = canonicalJSON(command.PreparedContext)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid prepared context: %v", ErrInvalidArgument, err)
		}
	}
	if attempt.PreparedContextSet {
		if bytes.Equal(attempt.PreparedContext, context) {
			return []Event{}, nil
		}
		return nil, fmt.Errorf("%w: prepared context is already set", ErrInvalidTransition)
	}
	return []Event{AttemptPreparedContextSet{TurnID: turn.ID, AttemptID: attempt.ID, PreparedContext: context}}, nil
}

func decidePrepareAttempt(state State, command PrepareAttempt) ([]Event, error) {
	if command.Meta.Actor != ActorDriver {
		return nil, ErrPermissionDenied
	}
	turn, attempt, err := requireAttempt(state, command.TurnID, command.AttemptID, command.Fence)
	if err != nil {
		return nil, err
	}
	if turn.Status != TurnPreparing || attempt.Status != AttemptAssigned {
		return nil, fmt.Errorf("%w: attempt is not assigned", ErrInvalidTransition)
	}
	return []Event{AttemptPreparedEvent{TurnID: turn.ID, AttemptID: attempt.ID}}, nil
}

func decidePermitAttempt(state State, command PermitAttempt) ([]Event, error) {
	if command.Meta.Actor != ActorKernel {
		return nil, ErrPermissionDenied
	}
	turn, attempt, err := requireAttempt(state, command.TurnID, command.AttemptID, command.Fence)
	if err != nil {
		return nil, err
	}
	if command.PermitID == "" {
		return nil, fmt.Errorf("%w: permit id is required", ErrInvalidArgument)
	}
	if turn.Status != TurnPreparing || attempt.Status != AttemptPrepared {
		return nil, fmt.Errorf("%w: attempt is not prepared", ErrInvalidTransition)
	}
	return []Event{AttemptPermittedEvent{TurnID: turn.ID, AttemptID: attempt.ID, PermitID: command.PermitID}}, nil
}

func decideAppendOutput(state State, command AppendAttemptOutput) ([]Event, error) {
	if command.Meta.Actor != ActorDriver {
		return nil, ErrPermissionDenied
	}
	turn, attempt, err := requireAttempt(state, command.TurnID, command.AttemptID, command.Fence)
	if err != nil {
		return nil, err
	}
	if turn.Status != TurnExecuting && turn.Status != TurnCancelling || attempt.Status != AttemptPermitted {
		return nil, fmt.Errorf("%w: attempt is not permitted", ErrInvalidTransition)
	}
	if command.Sequence == 0 || command.Type == "" || len(command.Payload) == 0 {
		return nil, fmt.Errorf("%w: output sequence, type, and payload are required", ErrInvalidArgument)
	}
	payload, digest, err := canonicalJSON(command.Payload)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid output payload: %v", ErrInvalidArgument, err)
	}
	for _, output := range attempt.Outputs {
		if output.Sequence != command.Sequence {
			continue
		}
		if output.Type == command.Type && output.Digest == digest {
			return []Event{}, nil
		}
		return nil, fmt.Errorf("%w: sequence %d was already committed", ErrOutputConflict, command.Sequence)
	}
	expected := uint64(1)
	if len(attempt.Outputs) != 0 {
		expected = attempt.Outputs[len(attempt.Outputs)-1].Sequence + 1
	}
	if command.Sequence != expected {
		return nil, &OutputReplayRequiredError{ExpectedSequence: expected, ReceivedSequence: command.Sequence}
	}
	return []Event{AttemptOutputAppended{TurnID: turn.ID, AttemptID: attempt.ID, Output: AttemptOutput{Sequence: command.Sequence, Type: command.Type, Payload: payload, Digest: digest}}}, nil
}

func decideCompleteAttempt(state State, command CompleteAttempt) ([]Event, error) {
	if command.Meta.Actor != ActorDriver {
		return nil, ErrPermissionDenied
	}
	turn, attempt, err := requireAttempt(state, command.TurnID, command.AttemptID, command.Fence)
	if err != nil {
		return nil, err
	}
	if turn.Status != TurnExecuting && turn.Status != TurnCancelling {
		return nil, fmt.Errorf("%w: turn is %s", ErrInvalidTransition, turn.Status)
	}
	if attempt.Status != AttemptPermitted {
		return nil, fmt.Errorf("%w: attempt is not permitted", ErrInvalidTransition)
	}
	return []Event{AttemptCompletedEvent{TurnID: turn.ID, AttemptID: attempt.ID}}, nil
}

func decideFailAttempt(state State, command FailAttempt) ([]Event, error) {
	if command.Meta.Actor != ActorDriver {
		return nil, ErrPermissionDenied
	}
	turn, attempt, err := requireAttempt(state, command.TurnID, command.AttemptID, command.Fence)
	if err != nil {
		return nil, err
	}
	if command.Reason == "" {
		return nil, fmt.Errorf("%w: failure reason is required", ErrInvalidArgument)
	}
	switch attempt.Status {
	case AttemptAssigned, AttemptPrepared:
		return []Event{AttemptFailedEvent{TurnID: turn.ID, AttemptID: attempt.ID, Reason: command.Reason, Retryable: command.Retryable}}, nil
	case AttemptPermitted:
		return []Event{AttemptFailedEvent{TurnID: turn.ID, AttemptID: attempt.ID, Reason: command.Reason}}, nil
	default:
		return nil, fmt.Errorf("%w: attempt is %s", ErrInvalidTransition, attempt.Status)
	}
}

func decideReportAttemptUncertain(state State, command ReportAttemptUncertain) ([]Event, error) {
	if command.Meta.Actor != ActorDriver {
		return nil, ErrPermissionDenied
	}
	turn, attempt, err := requireAttempt(state, command.TurnID, command.AttemptID, command.Fence)
	if err != nil {
		return nil, err
	}
	if command.Reason == "" {
		return nil, fmt.Errorf("%w: uncertainty reason is required", ErrInvalidArgument)
	}
	if turn.Status != TurnExecuting && turn.Status != TurnCancelling {
		return nil, fmt.Errorf("%w: turn is %s", ErrInvalidTransition, turn.Status)
	}
	if attempt.Status != AttemptPermitted {
		return nil, fmt.Errorf("%w: attempt is not permitted", ErrInvalidTransition)
	}
	return []Event{AttemptUncertainEvent{
		TurnID:                 turn.ID,
		AttemptID:              attempt.ID,
		Reason:                 command.Reason,
		ReconciliationEvidence: command.ReconciliationEvidence,
	}}, nil
}

func decideCancelTurn(state State, command CancelTurn) ([]Event, error) {
	if command.Meta.Actor != ActorClient && command.Meta.Actor != ActorHuman && command.Meta.Actor != ActorRecoveryService {
		return nil, ErrPermissionDenied
	}
	turn, ok := state.Turns[command.TurnID]
	if !ok {
		return nil, fmt.Errorf("%w: turn %q", ErrInvalidArgument, command.TurnID)
	}
	if turn.Status.Terminal() || turn.CancellationRequested {
		return []Event{}, nil
	}
	return []Event{TurnCancellationRequested{TurnID: turn.ID}}, nil
}

func decideConfirmCancellation(state State, command ConfirmCancellation) ([]Event, error) {
	if command.Meta.Actor != ActorDriver {
		return nil, ErrPermissionDenied
	}
	turn, attempt, err := requireAttempt(state, command.TurnID, command.AttemptID, command.Fence)
	if err != nil {
		return nil, err
	}
	if turn.Status != TurnCancelling || !turn.CancellationRequested {
		return nil, fmt.Errorf("%w: turn is not cancelling", ErrInvalidTransition)
	}
	if attempt.Status != AttemptAssigned && attempt.Status != AttemptPrepared && attempt.Status != AttemptPermitted {
		return nil, fmt.Errorf("%w: attempt cannot be cancelled", ErrInvalidTransition)
	}
	return []Event{AttemptCancelledEvent{TurnID: turn.ID, AttemptID: attempt.ID}}, nil
}

func decideRecoverTurn(state State, command RecoverTurn) ([]Event, error) {
	if command.Meta.Actor != ActorHuman && command.Meta.Actor != ActorRecoveryService {
		return nil, ErrPermissionDenied
	}
	turn, ok := state.Turns[command.TurnID]
	if !ok || turn.Status != TurnRecoveryRequired {
		return nil, fmt.Errorf("%w: turn does not require recovery", ErrInvalidTransition)
	}
	attempt, ok := activeAttempt(turn)
	if !ok || attempt.Status != AttemptUncertain {
		return nil, fmt.Errorf("%w: turn has no uncertain attempt", ErrInvalidTransition)
	}
	switch command.Action {
	case RecoveryResume:
		if command.ReconciliationEvidence == "" {
			return nil, fmt.Errorf("%w: reconciliation evidence is required", ErrInvalidArgument)
		}
		if err := requireFence(state, command.Fence); err != nil {
			return nil, err
		}
		return []Event{AttemptResumedEvent{TurnID: turn.ID, AttemptID: attempt.ID, Fence: command.Fence, Evidence: command.ReconciliationEvidence}}, nil
	case RecoveryRetry:
		return []Event{AttemptSupersededEvent{TurnID: turn.ID, AttemptID: attempt.ID}}, nil
	case RecoveryDiscard:
		return []Event{TurnDiscardedEvent{TurnID: turn.ID}}, nil
	default:
		return nil, fmt.Errorf("%w: unknown recovery action %q", ErrInvalidArgument, command.Action)
	}
}

func requireOpen(state State) error {
	if state.Status != SessionOpen {
		return fmt.Errorf("%w: session is %s", ErrInvalidTransition, state.Status)
	}
	return nil
}

func requireClientOrHuman(meta CommandMeta) error {
	if meta.Actor != ActorClient && meta.Actor != ActorHuman {
		return ErrPermissionDenied
	}
	return nil
}

func requireFence(state State, fence Fence) error {
	if state.Driver.Status == DriverAbsent || fence != state.Driver.Fence {
		return fmt.Errorf("%w: driver fence does not own the session", ErrStaleDriver)
	}
	return nil
}

func requireAttempt(state State, turnID, attemptID string, fence Fence) (Turn, Attempt, error) {
	if err := requireFence(state, fence); err != nil {
		return Turn{}, Attempt{}, err
	}
	turn, ok := state.Turns[turnID]
	if !ok || turn.ActiveAttemptID != attemptID || state.ActiveTurnID != turnID {
		return Turn{}, Attempt{}, fmt.Errorf("%w: attempt is not active", ErrInvalidTransition)
	}
	attempt, ok := findAttempt(turn, attemptID)
	if !ok || attempt.DriverGeneration != fence.Generation || attempt.LeaseID != fence.LeaseID || attempt.DriverInstanceID != fence.DriverInstanceID {
		return Turn{}, Attempt{}, fmt.Errorf("%w: attempt fence does not match", ErrStaleDriver)
	}
	return turn, attempt, nil
}

func findAttempt(turn Turn, attemptID string) (Attempt, bool) {
	for _, attempt := range turn.Attempts {
		if attempt.ID == attemptID {
			return attempt, true
		}
	}
	return Attempt{}, false
}

func activeAttempt(turn Turn) (Attempt, bool) {
	return findAttempt(turn, turn.ActiveAttemptID)
}

func digestValue(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

// DigestJSON returns the digest used for durable JSON identity comparisons.
func DigestJSON(raw json.RawMessage) (string, error) {
	_, digest, err := canonicalJSON(raw)
	return digest, err
}

func canonicalJSON(raw json.RawMessage) (json.RawMessage, string, error) {
	var value any
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err != nil {
		return nil, "", err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return nil, "", fmt.Errorf("multiple JSON values")
		}
		return nil, "", err
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil, "", err
	}
	digest, err := digestValue(value)
	if err != nil {
		return nil, "", err
	}
	return data, digest, nil
}
