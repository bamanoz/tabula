package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type CancelRequest struct {
	Key       SessionKey
	CommandID string
	TurnID    string
	Actor     ActorKind
}

type RecoveryRequest struct {
	Key                    SessionKey
	CommandID              string
	TurnID                 string
	Actor                  ActorKind
	Action                 RecoveryAction
	Fence                  Fence
	ReconciliationEvidence string
}

type RecoveryService struct {
	repository SessionRepository
}

type TransitionResult struct {
	Record    Record
	Duplicate bool
}

func NewRecoveryService(repository SessionRepository) (*RecoveryService, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: session repository is required", ErrInvalidArgument)
	}
	return &RecoveryService{repository: repository}, nil
}

func (s *RecoveryService) Cancel(ctx context.Context, request CancelRequest) (Record, error) {
	result, err := s.CancelResult(ctx, request)
	return result.Record, err
}

func (s *RecoveryService) CancelResult(ctx context.Context, request CancelRequest) (TransitionResult, error) {
	if err := request.Key.Validate(); err != nil {
		return TransitionResult{}, err
	}
	if request.CommandID == "" || request.TurnID == "" {
		return TransitionResult{}, fmt.Errorf("%w: command and turn ids are required", ErrInvalidArgument)
	}
	command := CancelTurn{Meta: CommandMeta{ID: request.CommandID, Actor: request.Actor}, TurnID: request.TurnID}
	return s.commitResult(ctx, request.Key, command)
}

func (s *RecoveryService) ConfirmCancellation(ctx context.Context, key SessionKey, commandID, runtimeID, turnID, attemptID string, fence Fence) (Record, error) {
	result, err := s.ConfirmCancellationResult(ctx, key, commandID, runtimeID, turnID, attemptID, fence)
	return result.Record, err
}

func (s *RecoveryService) ConfirmCancellationResult(ctx context.Context, key SessionKey, commandID, runtimeID, turnID, attemptID string, fence Fence) (TransitionResult, error) {
	if commandID == "" {
		return TransitionResult{}, fmt.Errorf("%w: command id is required", ErrInvalidArgument)
	}
	return s.commitDriverResult(ctx, key, runtimeID, ConfirmCancellation{Meta: CommandMeta{ID: commandID, Actor: ActorDriver}, TurnID: turnID, AttemptID: attemptID, Fence: fence})
}

func (s *RecoveryService) Complete(ctx context.Context, key SessionKey, commandID, runtimeID, turnID, attemptID string, fence Fence) (Record, error) {
	result, err := s.CompleteResult(ctx, key, commandID, runtimeID, turnID, attemptID, fence)
	return result.Record, err
}

func (s *RecoveryService) CompleteResult(ctx context.Context, key SessionKey, commandID, runtimeID, turnID, attemptID string, fence Fence) (TransitionResult, error) {
	if commandID == "" {
		return TransitionResult{}, fmt.Errorf("%w: command id is required", ErrInvalidArgument)
	}
	return s.commitDriverResult(ctx, key, runtimeID, CompleteAttempt{Meta: CommandMeta{ID: commandID, Actor: ActorDriver}, TurnID: turnID, AttemptID: attemptID, Fence: fence})
}

func (s *RecoveryService) Fail(ctx context.Context, key SessionKey, commandID, runtimeID, turnID, attemptID string, fence Fence, reason string) (Record, error) {
	result, err := s.FailResult(ctx, key, commandID, runtimeID, turnID, attemptID, fence, reason)
	return result.Record, err
}

func (s *RecoveryService) FailResult(ctx context.Context, key SessionKey, commandID, runtimeID, turnID, attemptID string, fence Fence, reason string) (TransitionResult, error) {
	if commandID == "" {
		return TransitionResult{}, fmt.Errorf("%w: command id is required", ErrInvalidArgument)
	}
	return s.commitPermittedDriverResult(ctx, key, runtimeID, FailAttempt{Meta: CommandMeta{ID: commandID, Actor: ActorDriver}, TurnID: turnID, AttemptID: attemptID, Fence: fence, Reason: reason}, turnID, attemptID, fence)
}

func (s *RecoveryService) Uncertain(ctx context.Context, key SessionKey, commandID, runtimeID, turnID, attemptID string, fence Fence, reason, reconciliationEvidence string) (Record, error) {
	result, err := s.UncertainResult(ctx, key, commandID, runtimeID, turnID, attemptID, fence, reason, reconciliationEvidence)
	return result.Record, err
}

func (s *RecoveryService) UncertainResult(ctx context.Context, key SessionKey, commandID, runtimeID, turnID, attemptID string, fence Fence, reason, reconciliationEvidence string) (TransitionResult, error) {
	if commandID == "" {
		return TransitionResult{}, fmt.Errorf("%w: command id is required", ErrInvalidArgument)
	}
	return s.commitDriverResult(ctx, key, runtimeID, ReportAttemptUncertain{
		Meta:                   CommandMeta{ID: commandID, Actor: ActorDriver},
		TurnID:                 turnID,
		AttemptID:              attemptID,
		Fence:                  fence,
		Reason:                 reason,
		ReconciliationEvidence: reconciliationEvidence,
	})
}

func (s *RecoveryService) Recover(ctx context.Context, request RecoveryRequest) (Record, error) {
	result, err := s.RecoverResult(ctx, request)
	return result.Record, err
}

func (s *RecoveryService) RecoverResult(ctx context.Context, request RecoveryRequest) (TransitionResult, error) {
	if err := request.Key.Validate(); err != nil {
		return TransitionResult{}, err
	}
	if request.CommandID == "" || request.TurnID == "" {
		return TransitionResult{}, fmt.Errorf("%w: command and turn ids are required", ErrInvalidArgument)
	}
	command := RecoverTurn{
		Meta:                   CommandMeta{ID: request.CommandID, Actor: request.Actor},
		TurnID:                 request.TurnID,
		Action:                 request.Action,
		Fence:                  request.Fence,
		ReconciliationEvidence: request.ReconciliationEvidence,
	}
	return s.commitResult(ctx, request.Key, command)
}

func (s *RecoveryService) commitDriverResult(ctx context.Context, key SessionKey, runtimeID string, command Command) (TransitionResult, error) {
	return s.commitDriverRecordResult(ctx, key, runtimeID, command, nil)
}

func (s *RecoveryService) commitPermittedDriverResult(ctx context.Context, key SessionKey, runtimeID string, command Command, turnID, attemptID string, fence Fence) (TransitionResult, error) {
	return s.commitDriverRecordResult(ctx, key, runtimeID, command, func(state State) error {
		turn, attempt, err := requireAttempt(state, turnID, attemptID, fence)
		if err != nil {
			return err
		}
		if turn.Status != TurnExecuting && turn.Status != TurnCancelling || attempt.Status != AttemptPermitted {
			return fmt.Errorf("%w: attempt is not permitted", ErrInvalidTransition)
		}
		return nil
	})
}

func (s *RecoveryService) commitDriverRecordResult(ctx context.Context, key SessionKey, runtimeID string, command Command, validate func(State) error) (TransitionResult, error) {
	for range executionCommitRetries {
		record, err := s.repository.Load(ctx, key)
		if err != nil {
			return TransitionResult{}, err
		}
		if runtimeID == "" || runtimeID != record.State.Driver.RuntimeID {
			return TransitionResult{}, fmt.Errorf("%w: runtime identity does not own this lease", ErrPermissionDenied)
		}
		if validate != nil {
			if _, replay := record.State.AppliedCommands[command.Metadata().ID]; !replay {
				if err := validate(record.State); err != nil {
					return TransitionResult{}, err
				}
			}
		}
		result, retry, err := s.commitRecord(ctx, key, record, command)
		if retry {
			continue
		}
		return result, err
	}
	return TransitionResult{}, fmt.Errorf("commit driver outcome after %d conflicts: %w", executionCommitRetries, ErrVersionConflict)
}

func (s *RecoveryService) commitResult(ctx context.Context, key SessionKey, command Command) (TransitionResult, error) {
	for range executionCommitRetries {
		record, err := s.repository.Load(ctx, key)
		if err != nil {
			return TransitionResult{}, err
		}
		result, retry, err := s.commitRecord(ctx, key, record, command)
		if retry {
			continue
		}
		return result, err
	}
	return TransitionResult{}, fmt.Errorf("commit recovery after %d conflicts: %w", executionCommitRetries, ErrVersionConflict)
}

func (s *RecoveryService) commitRecord(ctx context.Context, key SessionKey, record Record, command Command) (TransitionResult, bool, error) {
	events, projection, digest, err := decideProjection(record, command)
	if err != nil {
		return TransitionResult{}, false, err
	}
	outbox, err := recoveryOutbox(key, command, projection, events)
	if err != nil {
		return TransitionResult{}, false, err
	}
	result, err := s.repository.Commit(ctx, Commit{Key: key, CommandID: command.Metadata().ID, CommandDigest: digest, ExpectedVersion: record.Version, Events: events, Projection: projection, Outbox: outbox})
	if errors.Is(err, ErrVersionConflict) {
		return TransitionResult{}, true, nil
	}
	if err != nil {
		return TransitionResult{}, false, err
	}
	return TransitionResult{Record: result.Record, Duplicate: result.Duplicate}, false, nil
}

func recoveryOutbox(key SessionKey, command Command, state State, events []Envelope) ([]OutboxMessage, error) {
	if len(events) == 0 {
		return nil, nil
	}
	turnID := ""
	topic := "turn.state_changed"
	switch value := command.(type) {
	case CancelTurn:
		turnID = value.TurnID
		if state.Turns[turnID].Status == TurnCancelling {
			topic = "turn.cancel"
		}
	case ConfirmCancellation:
		turnID = value.TurnID
	case CompleteAttempt:
		turnID = value.TurnID
	case FailAttempt:
		turnID = value.TurnID
	case ReportAttemptUncertain:
		turnID = value.TurnID
	case RecoverTurn:
		turnID = value.TurnID
	}
	turn, ok := state.Turns[turnID]
	if !ok {
		return nil, fmt.Errorf("%w: recovery turn %q missing", ErrCorruptState, turnID)
	}
	payload, err := json.Marshal(struct {
		Key  SessionKey `json:"key"`
		Turn Turn       `json:"turn"`
	}{Key: key, Turn: turn})
	if err != nil {
		return nil, err
	}
	return []OutboxMessage{{ID: command.Metadata().ID + ":turn-state", Topic: topic, Payload: payload}}, nil
}
