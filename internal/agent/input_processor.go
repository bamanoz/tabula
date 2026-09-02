package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type InputSubmitRequest struct {
	Key             SessionKey
	CommandID       string
	InputID         string
	Content         json.RawMessage
	SourceDigest    string
	ActorID         string
	ExpectedVersion *uint64
}

type InputAcceptance struct {
	InputID        string `json:"input_id"`
	TurnID         string `json:"turn_id"`
	Position       uint64 `json:"position"`
	SessionVersion uint64 `json:"session_version"`
	Cursor         Cursor `json:"cursor"`
	Duplicate      bool   `json:"duplicate"`
}

type InputProcessor struct {
	repository SessionRepository
}

func NewInputProcessor(repository SessionRepository) (*InputProcessor, error) {
	if repository == nil {
		return nil, fmt.Errorf("%w: repository is nil", ErrInvalidArgument)
	}
	return &InputProcessor{repository: repository}, nil
}

func (p *InputProcessor) Submit(ctx context.Context, request InputSubmitRequest) (InputAcceptance, error) {
	if err := validateInputSubmitRequest(request); err != nil {
		return InputAcceptance{}, err
	}
	_, sourceDigest, err := canonicalJSON(request.Content)
	if err != nil {
		return InputAcceptance{}, fmt.Errorf("digest input content: %w", err)
	}
	if request.SourceDigest != "" {
		sourceDigest = request.SourceDigest
	}
	turnID := turnIDForCommand(request.CommandID)
	for {
		if err := ctx.Err(); err != nil {
			return InputAcceptance{}, err
		}
		record, err := p.repository.Load(ctx, request.Key)
		if err != nil {
			return InputAcceptance{}, err
		}
		if existingInput, exists := record.State.Inputs[request.InputID]; exists {
			if existingInput.SourceDigest != sourceDigest {
				return InputAcceptance{}, fmt.Errorf("%w: input id %q was already used", ErrInputConflict, request.InputID)
			}
			return acceptanceFromInput(record.State, existingInput, true)
		}
		if request.ExpectedVersion != nil && *request.ExpectedVersion != record.Version {
			return InputAcceptance{}, fmt.Errorf("%w: expected %d, current %d", ErrVersionConflict, *request.ExpectedVersion, record.Version)
		}
		command := SubmitInput{
			Meta: CommandMeta{ID: request.CommandID, Actor: ActorClient}, InputID: request.InputID, TurnID: turnID,
			Content: request.Content, SourceDigest: sourceDigest, ActorID: request.ActorID,
			AcceptanceVersion: record.Version + 1, AcceptanceCursor: record.Cursor + 2,
		}
		commandDigest, err := DigestCommand(command)
		if err != nil {
			return InputAcceptance{}, fmt.Errorf("digest input command: %w", err)
		}
		events, err := Decide(record.State, command)
		if err != nil {
			return InputAcceptance{}, err
		}
		next, err := ApplyAll(record.State, events)
		if err != nil {
			return InputAcceptance{}, fmt.Errorf("apply input command: %w", err)
		}
		outboxPayload, err := json.Marshal(InputAcceptance{
			InputID: request.InputID, TurnID: command.TurnID, Position: next.Turns[command.TurnID].Position,
			SessionVersion: command.AcceptanceVersion, Cursor: command.AcceptanceCursor,
		})
		if err != nil {
			return InputAcceptance{}, fmt.Errorf("encode input acceptance: %w", err)
		}
		commit := Commit{
			Key:             request.Key,
			CommandID:       request.CommandID,
			CommandDigest:   commandDigest,
			ExpectedVersion: record.Version,
			Events:          events,
			Projection:      next,
			Outbox: []OutboxMessage{{
				ID:      inputAcceptanceID(request.InputID),
				Topic:   "input.accepted",
				Payload: outboxPayload,
			}},
		}
		result, err := p.repository.Commit(ctx, commit)
		if errors.Is(err, ErrVersionConflict) && request.ExpectedVersion == nil {
			continue
		}
		if err != nil {
			return InputAcceptance{}, err
		}
		return acceptanceFromResult(result, request.InputID, command.TurnID)
	}
}

func (p *InputProcessor) Get(ctx context.Context, key SessionKey) (Record, error) {
	if err := key.Validate(); err != nil {
		return Record{}, err
	}
	return p.repository.Load(ctx, key)
}

func validateInputSubmitRequest(request InputSubmitRequest) error {
	if err := request.Key.Validate(); err != nil {
		return err
	}
	if request.CommandID == "" || request.InputID == "" {
		return fmt.Errorf("%w: command id and input id are required", ErrInvalidArgument)
	}
	if len(request.Content) == 0 {
		return fmt.Errorf("%w: input content is required", ErrInvalidArgument)
	}
	return nil
}

func acceptanceFromResult(result CommitResult, inputID, fallbackTurnID string) (InputAcceptance, error) {
	input, ok := result.Record.State.Inputs[inputID]
	if !ok {
		return InputAcceptance{}, fmt.Errorf("%w: committed input %q is missing", ErrCorruptState, inputID)
	}
	acceptance, err := acceptanceFromInput(result.Record.State, input, result.Duplicate)
	if err != nil {
		return InputAcceptance{}, err
	}
	if acceptance.TurnID == "" {
		acceptance.TurnID = fallbackTurnID
	}
	return acceptance, nil
}

func acceptanceFromInput(state State, input Input, duplicate bool) (InputAcceptance, error) {
	turn, ok := findTurnByInput(state, input.ID)
	if !ok {
		return InputAcceptance{}, fmt.Errorf("%w: committed input %q has no turn", ErrCorruptState, input.ID)
	}
	if input.AcceptanceVersion == 0 || input.AcceptanceCursor == 0 {
		return InputAcceptance{}, fmt.Errorf("%w: committed input %q has no acceptance metadata", ErrCorruptState, input.ID)
	}
	return InputAcceptance{
		InputID: input.ID, TurnID: turn.ID, Position: turn.Position,
		SessionVersion: input.AcceptanceVersion, Cursor: input.AcceptanceCursor, Duplicate: duplicate,
	}, nil
}

func findTurnByInput(state State, inputID string) (Turn, bool) {
	for _, turn := range state.Turns {
		if turn.InputID == inputID {
			return turn, true
		}
	}
	return Turn{}, false
}

func turnIDForCommand(commandID string) string {
	return "turn-" + commandID
}

func inputAcceptanceID(inputID string) string {
	return "input.accepted/" + inputID
}

var _ = json.Valid
