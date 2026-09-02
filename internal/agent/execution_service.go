package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

var ErrNoAssignableTurn = errors.New("no assignable turn")

const executionCommitRetries = 16

type Assignment struct {
	Key                SessionKey      `json:"key"`
	TurnID             string          `json:"turn_id"`
	AttemptID          string          `json:"attempt_id"`
	TurnCorrelationID  string          `json:"turn_correlation_id"`
	Input              json.RawMessage `json:"input"`
	PreparedContext    json.RawMessage `json:"prepared_context,omitempty"`
	PreparedContextSet bool            `json:"-"`
	Fence              Fence           `json:"fence"`
	SequenceContext    Cursor          `json:"sequence_context"`
	SessionVersion     uint64          `json:"session_version"`
}

type ExecutionPermit struct {
	Key               SessionKey `json:"key"`
	TurnID            string     `json:"turn_id"`
	AttemptID         string     `json:"attempt_id"`
	TurnCorrelationID string     `json:"turn_correlation_id"`
	PermitID          string     `json:"permit_id"`
	Fence             Fence      `json:"fence"`
	SessionVersion    uint64     `json:"session_version"`
	Cursor            Cursor     `json:"cursor"`
}

type TurnExecutionService struct {
	repository SessionRepository
}

func NewTurnExecutionService(repository SessionRepository) (*TurnExecutionService, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: session repository is required", ErrInvalidArgument)
	}
	return &TurnExecutionService{repository: repository}, nil
}

// AssignNext durably assigns the FIFO head before publishing it to the driver.
func (s *TurnExecutionService) AssignNext(ctx context.Context, key SessionKey, preparedContext json.RawMessage) (Assignment, error) {
	for range executionCommitRetries {
		record, err := s.repository.Load(ctx, key)
		if err != nil {
			return Assignment{}, err
		}
		if assignment, ok := currentAssignment(record, preparedContext); ok {
			return assignment, nil
		}
		if record.State.Driver.Status != DriverReady || len(record.State.Queue) == 0 || record.State.ActiveTurnID != "" {
			return Assignment{}, ErrNoAssignableTurn
		}
		turnID := record.State.Queue[0]
		turn := record.State.Turns[turnID]
		attemptID := fmt.Sprintf("attempt-%s-%d-%d", turnID, record.State.Driver.Generation, len(turn.Attempts)+1)
		command := AssignTurn{
			Meta:      CommandMeta{ID: "turn-assign:" + attemptID, Actor: ActorKernel},
			TurnID:    turnID,
			AttemptID: attemptID,
			Fence:     record.State.Driver.Fence,
		}
		events, projection, digest, err := decideProjection(record, command)
		if err != nil {
			return Assignment{}, err
		}
		assignment, err := assignmentFromState(key, projection, attemptID, preparedContext, record.Version+uint64(len(events)), record.Cursor+Cursor(len(events)+1))
		if err != nil {
			return Assignment{}, err
		}
		payload, err := json.Marshal(assignment)
		if err != nil {
			return Assignment{}, err
		}
		result, err := s.repository.Commit(ctx, Commit{
			Key:             key,
			CommandID:       command.Meta.ID,
			CommandDigest:   digest,
			ExpectedVersion: record.Version,
			Events:          events,
			Projection:      projection,
			Outbox:          []OutboxMessage{{ID: "turn.assign/" + attemptID, Topic: "turn.assign", Payload: payload}},
		})
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		if err != nil {
			return Assignment{}, err
		}
		return assignmentFromState(key, result.Record.State, attemptID, preparedContext, result.Record.Version, result.Record.Cursor)
	}
	return Assignment{}, fmt.Errorf("assign turn after %d conflicts: %w", executionCommitRetries, ErrVersionConflict)
}

// SetPreparedContext durably records the before_turn result under the assigned attempt fence.
func (s *TurnExecutionService) SetPreparedContext(ctx context.Context, key SessionKey, turnID, attemptID string, fence Fence, preparedContext json.RawMessage) (Assignment, error) {
	command := SetAttemptPreparedContext{
		Meta:            CommandMeta{ID: "turn-prepared-context:" + attemptID, Actor: ActorKernel},
		TurnID:          turnID,
		AttemptID:       attemptID,
		Fence:           fence,
		PreparedContext: preparedContext,
	}
	for range executionCommitRetries {
		record, err := s.repository.Load(ctx, key)
		if err != nil {
			return Assignment{}, err
		}
		if assignment, ok := currentPreparedAssignment(record, turnID, attemptID, fence); ok {
			return assignment, nil
		}
		events, projection, digest, err := decideProjection(record, command)
		if err != nil {
			return Assignment{}, err
		}
		result, err := s.repository.Commit(ctx, Commit{
			Key:             key,
			CommandID:       command.Meta.ID,
			CommandDigest:   digest,
			ExpectedVersion: record.Version,
			Events:          events,
			Projection:      projection,
		})
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		if err != nil {
			return Assignment{}, err
		}
		return assignmentFromState(key, result.Record.State, attemptID, nil, result.Record.Version, result.Record.Cursor)
	}
	return Assignment{}, fmt.Errorf("set prepared context after %d conflicts: %w", executionCommitRetries, ErrVersionConflict)
}

// Prepare acknowledges side-effect-free driver preparation under the active fence.
func (s *TurnExecutionService) Prepare(ctx context.Context, key SessionKey, runtimeID, turnID, attemptID string, fence Fence) (Record, error) {
	return s.driverCommand(ctx, key, runtimeID, PrepareAttempt{
		Meta:      CommandMeta{ID: "turn-prepared:" + attemptID, Actor: ActorDriver},
		TurnID:    turnID,
		AttemptID: attemptID,
		Fence:     fence,
	}, nil)
}

// PrepareFailed records a pre-permit failure. Retryable failures return the turn to FIFO head.
func (s *TurnExecutionService) PrepareFailed(ctx context.Context, key SessionKey, runtimeID, turnID, attemptID string, fence Fence, reason string, retryable bool) (Record, error) {
	result, err := s.PrepareFailedResult(ctx, key, runtimeID, turnID, attemptID, fence, reason, retryable)
	return result.Record, err
}

func (s *TurnExecutionService) PrepareFailedResult(ctx context.Context, key SessionKey, runtimeID, turnID, attemptID string, fence Fence, reason string, retryable bool) (TransitionResult, error) {
	return s.driverCommandResult(ctx, key, runtimeID, FailAttempt{
		Meta:      CommandMeta{ID: "turn-prepare-failed:" + attemptID, Actor: ActorDriver},
		TurnID:    turnID,
		AttemptID: attemptID,
		Fence:     fence,
		Reason:    reason,
		Retryable: retryable,
	}, nil)
}

// Permit commits execution authority before publishing the redeliverable permit.
func (s *TurnExecutionService) Permit(ctx context.Context, key SessionKey, turnID, attemptID string, fence Fence) (ExecutionPermit, error) {
	permitID := "permit-" + attemptID
	command := PermitAttempt{
		Meta:      CommandMeta{ID: "turn-permit:" + attemptID, Actor: ActorKernel},
		TurnID:    turnID,
		AttemptID: attemptID,
		PermitID:  permitID,
		Fence:     fence,
	}
	for range executionCommitRetries {
		record, err := s.repository.Load(ctx, key)
		if err != nil {
			return ExecutionPermit{}, err
		}
		if permit, ok := currentPermit(record, turnID, attemptID, permitID, fence); ok {
			return permit, nil
		}
		events, projection, digest, err := decideProjection(record, command)
		if err != nil {
			return ExecutionPermit{}, err
		}
		permit := ExecutionPermit{Key: key, TurnID: turnID, AttemptID: attemptID, TurnCorrelationID: turnID, PermitID: permitID, Fence: fence, SessionVersion: record.Version + uint64(len(events)), Cursor: record.Cursor + Cursor(len(events)+1)}
		payload, err := json.Marshal(permit)
		if err != nil {
			return ExecutionPermit{}, err
		}
		result, err := s.repository.Commit(ctx, Commit{
			Key:             key,
			CommandID:       command.Meta.ID,
			CommandDigest:   digest,
			ExpectedVersion: record.Version,
			Events:          events,
			Projection:      projection,
			Outbox:          []OutboxMessage{{ID: "turn.permit/" + permitID, Topic: "turn.permit", Payload: payload}},
		})
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		if err != nil {
			return ExecutionPermit{}, err
		}
		permit.SessionVersion = result.Record.Version
		permit.Cursor = result.Record.Cursor
		return permit, nil
	}
	return ExecutionPermit{}, fmt.Errorf("permit turn after %d conflicts: %w", executionCommitRetries, ErrVersionConflict)
}

func (s *TurnExecutionService) driverCommand(ctx context.Context, key SessionKey, runtimeID string, command Command, outbox []OutboxMessage) (Record, error) {
	result, err := s.driverCommandResult(ctx, key, runtimeID, command, outbox)
	return result.Record, err
}

func (s *TurnExecutionService) driverCommandResult(ctx context.Context, key SessionKey, runtimeID string, command Command, outbox []OutboxMessage) (TransitionResult, error) {
	for range executionCommitRetries {
		record, err := s.repository.Load(ctx, key)
		if err != nil {
			return TransitionResult{}, err
		}
		if runtimeID == "" || runtimeID != record.State.Driver.RuntimeID {
			return TransitionResult{}, fmt.Errorf("%w: runtime identity does not own this lease", ErrPermissionDenied)
		}
		events, projection, digest, err := decideProjection(record, command)
		if err != nil {
			return TransitionResult{}, err
		}
		result, err := s.repository.Commit(ctx, Commit{Key: key, CommandID: command.Metadata().ID, CommandDigest: digest, ExpectedVersion: record.Version, Events: events, Projection: projection, Outbox: outbox})
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		if err != nil {
			return TransitionResult{}, err
		}
		return TransitionResult{Record: result.Record, Duplicate: result.Duplicate}, nil
	}
	return TransitionResult{}, fmt.Errorf("commit driver command after %d conflicts: %w", executionCommitRetries, ErrVersionConflict)
}

func decideProjection(record Record, command Command) ([]Envelope, State, string, error) {
	digest, err := DigestCommand(command)
	if err != nil {
		return nil, State{}, "", err
	}
	events, err := Decide(record.State, command)
	if err != nil {
		return nil, State{}, "", err
	}
	projection, err := ApplyAll(record.State, events)
	if err != nil {
		return nil, State{}, "", err
	}
	return events, projection, digest, nil
}

func assignmentFromState(key SessionKey, state State, attemptID string, preparedContext json.RawMessage, version uint64, cursor Cursor) (Assignment, error) {
	turn, attempt, ok := findAttemptByID(state, attemptID)
	if !ok {
		return Assignment{}, fmt.Errorf("%w: assigned attempt %q missing", ErrCorruptState, attemptID)
	}
	input, ok := state.Inputs[turn.InputID]
	if !ok {
		return Assignment{}, fmt.Errorf("%w: input %q missing", ErrCorruptState, turn.InputID)
	}
	if attempt.PreparedContextSet {
		preparedContext = attempt.PreparedContext
	}
	return Assignment{Key: key, TurnID: turn.ID, AttemptID: attempt.ID, TurnCorrelationID: turn.ID, Input: append(json.RawMessage(nil), input.Content...), PreparedContext: append(json.RawMessage(nil), preparedContext...), PreparedContextSet: attempt.PreparedContextSet, Fence: Fence{DriverInstanceID: attempt.DriverInstanceID, LeaseID: attempt.LeaseID, Generation: attempt.DriverGeneration}, SequenceContext: cursor, SessionVersion: version}, nil
}

func currentAssignment(record Record, preparedContext json.RawMessage) (Assignment, bool) {
	if record.State.ActiveTurnID == "" {
		return Assignment{}, false
	}
	turn := record.State.Turns[record.State.ActiveTurnID]
	if turn.ActiveAttemptID == "" {
		return Assignment{}, false
	}
	attempt, ok := findAttempt(turn, turn.ActiveAttemptID)
	if !ok || (attempt.Status != AttemptAssigned && attempt.Status != AttemptPrepared) {
		return Assignment{}, false
	}
	assignment, err := assignmentFromState(record.Key, record.State, attempt.ID, preparedContext, record.Version, record.Cursor)
	return assignment, err == nil
}

func currentPreparedAssignment(record Record, turnID, attemptID string, fence Fence) (Assignment, bool) {
	turn, ok := record.State.Turns[turnID]
	if !ok || turn.ActiveAttemptID != attemptID {
		return Assignment{}, false
	}
	attempt, ok := findAttempt(turn, attemptID)
	if !ok || !attempt.PreparedContextSet || attempt.DriverInstanceID != fence.DriverInstanceID || attempt.LeaseID != fence.LeaseID || attempt.DriverGeneration != fence.Generation {
		return Assignment{}, false
	}
	assignment, err := assignmentFromState(record.Key, record.State, attempt.ID, nil, record.Version, record.Cursor)
	return assignment, err == nil
}

func currentPermit(record Record, turnID, attemptID, permitID string, fence Fence) (ExecutionPermit, bool) {
	turn, ok := record.State.Turns[turnID]
	if !ok {
		return ExecutionPermit{}, false
	}
	attempt, ok := findAttempt(turn, attemptID)
	if !ok || attempt.Status != AttemptPermitted || attempt.PermitID != permitID || attempt.DriverInstanceID != fence.DriverInstanceID || attempt.LeaseID != fence.LeaseID || attempt.DriverGeneration != fence.Generation {
		return ExecutionPermit{}, false
	}
	return ExecutionPermit{Key: record.Key, TurnID: turnID, AttemptID: attemptID, TurnCorrelationID: turnID, PermitID: permitID, Fence: fence, SessionVersion: record.Version, Cursor: record.Cursor}, true
}

func findAttemptByID(state State, attemptID string) (Turn, Attempt, bool) {
	for _, turn := range state.Turns {
		if attempt, ok := findAttempt(turn, attemptID); ok {
			return turn, attempt, true
		}
	}
	return Turn{}, Attempt{}, false
}
