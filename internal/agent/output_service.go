package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

const (
	MaxOutputPayloadBytes = 64 << 10
	OutputReplayLimit     = 256
)

var outputTypes = map[string]bool{
	"stream.delta":   true,
	"reasoning":      true,
	"usage":          true,
	"provider.retry": true,
	"provider.error": true,
	"compaction":     true,
	"tool.result":    true,
}

type OutputRequest struct {
	Key       SessionKey
	RuntimeID string
	TurnID    string
	AttemptID string
	Fence     Fence
	Sequence  uint64
	Type      string
	Payload   json.RawMessage
}

type OutputAcceptance struct {
	Sequence       uint64      `json:"sequence"`
	SessionVersion uint64      `json:"session_version"`
	Cursor         Cursor      `json:"cursor"`
	Event          StoredEvent `json:"event,omitempty"`
	Duplicate      bool        `json:"duplicate"`
}

type OutputService struct {
	repository SessionRepository
}

func NewOutputService(repository SessionRepository) (*OutputService, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: session repository is required", ErrInvalidArgument)
	}
	return &OutputService{repository: repository}, nil
}

func (s *OutputService) Append(ctx context.Context, request OutputRequest) (OutputAcceptance, error) {
	if err := validateOutputRequest(request); err != nil {
		return OutputAcceptance{}, err
	}
	command := AppendAttemptOutput{
		Meta:      CommandMeta{ID: fmt.Sprintf("turn-output:%s:%d", request.AttemptID, request.Sequence), Actor: ActorDriver},
		TurnID:    request.TurnID,
		AttemptID: request.AttemptID,
		Fence:     request.Fence,
		Sequence:  request.Sequence,
		Type:      request.Type,
		Payload:   request.Payload,
	}
	for range executionCommitRetries {
		record, err := s.repository.Load(ctx, request.Key)
		if err != nil {
			return OutputAcceptance{}, err
		}
		if request.RuntimeID != record.State.Driver.RuntimeID {
			return OutputAcceptance{}, fmt.Errorf("%w: runtime identity does not own this lease", ErrPermissionDenied)
		}
		events, projection, digest, err := decideProjection(record, command)
		if err != nil {
			return OutputAcceptance{}, err
		}
		if len(events) == 0 {
			return OutputAcceptance{Sequence: request.Sequence, SessionVersion: record.Version, Cursor: record.Cursor, Duplicate: true}, nil
		}
		payload, err := json.Marshal(struct {
			Key       SessionKey      `json:"key"`
			TurnID    string          `json:"turn_id"`
			AttemptID string          `json:"attempt_id"`
			Fence     Fence           `json:"fence"`
			Sequence  uint64          `json:"sequence"`
			Type      string          `json:"type"`
			Payload   json.RawMessage `json:"payload"`
		}{request.Key, request.TurnID, request.AttemptID, request.Fence, request.Sequence, request.Type, request.Payload})
		if err != nil {
			return OutputAcceptance{}, err
		}
		result, err := s.repository.Commit(ctx, Commit{
			Key:             request.Key,
			CommandID:       command.Meta.ID,
			CommandDigest:   digest,
			ExpectedVersion: record.Version,
			Events:          events,
			Projection:      projection,
			Outbox:          []OutboxMessage{{ID: fmt.Sprintf("turn.output/%s/%d", request.AttemptID, request.Sequence), Topic: "turn.output", Payload: payload}},
		})
		if errors.Is(err, ErrVersionConflict) {
			continue
		}
		if err != nil {
			return OutputAcceptance{}, err
		}
		if len(events) != 1 || len(result.Outbox) != 1 || result.Record.Cursor == 0 {
			return OutputAcceptance{}, fmt.Errorf("%w: output commit did not produce one event and one outbox message", ErrCorruptState)
		}
		eventCursor := result.Record.Cursor - 1
		return OutputAcceptance{
			Sequence:       request.Sequence,
			SessionVersion: result.Record.Version,
			Cursor:         result.Record.Cursor,
			Event: StoredEvent{
				ID:      eventID(request.Key, eventCursor),
				Cursor:  eventCursor,
				Version: result.Record.Version,
				Event:   cloneEnvelope(events[0]),
			},
		}, nil
	}
	return OutputAcceptance{}, fmt.Errorf("append output after %d conflicts: %w", executionCommitRetries, ErrVersionConflict)
}

func (s *OutputService) Replay(ctx context.Context, key SessionKey, after Cursor, limit int) ([]StoredEvent, error) {
	if limit <= 0 || limit > OutputReplayLimit {
		return nil, fmt.Errorf("%w: replay limit must be between 1 and %d", ErrInvalidArgument, OutputReplayLimit)
	}
	return s.repository.ReadEvents(ctx, key, after, limit)
}

func validateOutputRequest(request OutputRequest) error {
	if err := request.Key.Validate(); err != nil {
		return err
	}
	if request.RuntimeID == "" || request.TurnID == "" || request.AttemptID == "" || request.Sequence == 0 || !outputTypes[request.Type] {
		return fmt.Errorf("%w: runtime, turn, attempt, sequence, and documented output type are required", ErrInvalidArgument)
	}
	if len(request.Payload) == 0 || len(request.Payload) > MaxOutputPayloadBytes {
		return fmt.Errorf("%w: output payload must be between 1 and %d bytes", ErrInvalidArgument, MaxOutputPayloadBytes)
	}
	return nil
}
