package agent

import (
	"encoding/json"
	"fmt"
	"time"
)

func Apply(state State, envelope Envelope) (State, error) {
	if envelope.Body == nil {
		return state, fmt.Errorf("%w: event is nil", ErrInvalidArgument)
	}
	state = cloneState(state)

	switch event := envelope.Body.(type) {
	case SessionCreated:
		state.TenantID = event.TenantID
		state.SessionID = event.SessionID
		state.DriverComponentID = event.DriverComponentID
		state.AgentSpecRevision = event.AgentSpecRevision
		state.Status = SessionOpen
	case SessionArchived:
		state.Archived = true
	case SessionUnarchived:
		state.Archived = false
	case SessionSuspendedEvent:
		state.Status = SessionSuspended
	case SessionResumedEvent:
		state.Status = SessionOpen
	case SessionDeletedEvent:
		state.Status = SessionClosed
	case InputSubmitted:
		state.Inputs[event.Input.ID] = cloneInput(event.Input)
		state.Turns[event.Turn.ID] = cloneTurn(event.Turn)
		if event.Turn.Position > state.NextTurnPosition {
			state.NextTurnPosition = event.Turn.Position
		}
		state.Queue = append(state.Queue, event.Turn.ID)
	case DriverLeaseGranted:
		state.Driver = event.Driver
	case DriverBecameReady:
		state.Driver.Status = DriverReady
	case DriverLeaseRenewed:
		state.Driver.Deadline = event.Deadline
		state.Driver.HeartbeatSequence = event.HeartbeatSequence
		if state.Driver.Status == DriverSuspect {
			state.Driver.Status = DriverReady
		}
	case DriverBecameSuspect:
		state.Driver.Status = DriverSuspect
	case DriverLeaseReleased, DriverLeaseExpired:
		state.Driver.Status = DriverAbsent
		state.Driver.Fence = Fence{}
		state.Driver.RuntimeID = ""
		state.Driver.ComponentID = ""
		state.Driver.AgentSpecRevision = ""
		state.Driver.HeartbeatSequence = 0
		state.Driver.Deadline = time.Time{}
	case AttemptAssignedEvent:
		turn, err := stateTurn(state, event.TurnID)
		if err != nil {
			return State{}, err
		}
		turn.Status = TurnPreparing
		turn.ActiveAttemptID = event.Attempt.ID
		turn.Attempts = append(turn.Attempts, event.Attempt)
		state.Turns[turn.ID] = turn
		state.ActiveTurnID = turn.ID
	case AttemptPreparedContextSet:
		if err := updateAttempt(&state, event.TurnID, event.AttemptID, func(turn *Turn, attempt *Attempt) {
			attempt.PreparedContext = append(json.RawMessage(nil), event.PreparedContext...)
			attempt.PreparedContextSet = true
		}); err != nil {
			return State{}, err
		}
	case AttemptPreparedEvent:
		if err := updateAttempt(&state, event.TurnID, event.AttemptID, func(turn *Turn, attempt *Attempt) {
			attempt.Status = AttemptPrepared
		}); err != nil {
			return State{}, err
		}
	case AttemptPermittedEvent:
		if err := updateAttempt(&state, event.TurnID, event.AttemptID, func(turn *Turn, attempt *Attempt) {
			attempt.Status = AttemptPermitted
			attempt.PermitID = event.PermitID
			turn.Status = TurnExecuting
		}); err != nil {
			return State{}, err
		}
	case AttemptOutputAppended:
		if err := updateAttempt(&state, event.TurnID, event.AttemptID, func(turn *Turn, attempt *Attempt) {
			output := event.Output
			output.Payload = append([]byte(nil), output.Payload...)
			attempt.Outputs = retainAttemptOutputs(append(attempt.Outputs, output))
		}); err != nil {
			return State{}, err
		}
		retainSessionOutputs(&state)
	case AttemptCompletedEvent:
		if err := updateAttempt(&state, event.TurnID, event.AttemptID, func(turn *Turn, attempt *Attempt) {
			attempt.Status = AttemptCompleted
			turn.Status = TurnCompleted
		}); err != nil {
			return State{}, err
		}
		finishTurn(&state, event.TurnID)
	case AttemptFailedEvent:
		if err := updateAttempt(&state, event.TurnID, event.AttemptID, func(turn *Turn, attempt *Attempt) {
			attempt.Status = AttemptFailed
			attempt.Failure = event.Reason
			if event.Retryable && attempt.PermitID == "" {
				turn.Status = TurnQueued
				turn.ActiveAttemptID = ""
			} else {
				turn.Status = TurnFailed
			}
		}); err != nil {
			return State{}, err
		}
		turn := state.Turns[event.TurnID]
		state.ActiveTurnID = ""
		if turn.Status.Terminal() {
			removeQueueHead(&state, event.TurnID)
		}
	case AttemptAbandonedEvent:
		if err := updateAttempt(&state, event.TurnID, event.AttemptID, func(turn *Turn, attempt *Attempt) {
			attempt.Status = AttemptAbandoned
			attempt.Failure = event.Reason
			if turn.CancellationRequested {
				turn.Status = TurnCancelled
			} else {
				turn.Status = TurnQueued
			}
			turn.ActiveAttemptID = ""
		}); err != nil {
			return State{}, err
		}
		state.ActiveTurnID = ""
		if state.Turns[event.TurnID].Status == TurnCancelled {
			removeQueuedTurn(&state, event.TurnID)
		}
	case AttemptUncertainEvent:
		if err := updateAttempt(&state, event.TurnID, event.AttemptID, func(turn *Turn, attempt *Attempt) {
			attempt.Status = AttemptUncertain
			attempt.Failure = event.Reason
			turn.Status = TurnRecoveryRequired
			turn.RecoveryReason = event.Reason
		}); err != nil {
			return State{}, err
		}
		state.ActiveTurnID = event.TurnID
	case TurnCancellationRequested:
		turn, err := stateTurn(state, event.TurnID)
		if err != nil {
			return State{}, err
		}
		turn.CancellationRequested = true
		switch turn.Status {
		case TurnQueued, TurnRecoveryRequired:
			turn.Status = TurnCancelled
			state.Turns[turn.ID] = turn
			finishTurn(&state, turn.ID)
		default:
			turn.Status = TurnCancelling
			state.Turns[turn.ID] = turn
		}
	case AttemptCancelledEvent:
		if err := updateAttempt(&state, event.TurnID, event.AttemptID, func(turn *Turn, attempt *Attempt) {
			attempt.Status = AttemptCancelled
			turn.Status = TurnCancelled
		}); err != nil {
			return State{}, err
		}
		finishTurn(&state, event.TurnID)
	case AttemptResumedEvent:
		if err := updateAttempt(&state, event.TurnID, event.AttemptID, func(turn *Turn, attempt *Attempt) {
			attempt.Status = AttemptPermitted
			attempt.DriverInstanceID = event.Fence.DriverInstanceID
			attempt.DriverGeneration = event.Fence.Generation
			attempt.LeaseID = event.Fence.LeaseID
			attempt.ReconciliationEvidence = event.Evidence
			turn.Status = TurnExecuting
			turn.RecoveryReason = ""
		}); err != nil {
			return State{}, err
		}
		state.ActiveTurnID = event.TurnID
	case AttemptSupersededEvent:
		if err := updateAttempt(&state, event.TurnID, event.AttemptID, func(turn *Turn, attempt *Attempt) {
			attempt.Status = AttemptSuperseded
			turn.Status = TurnQueued
			turn.ActiveAttemptID = ""
			turn.RecoveryReason = ""
		}); err != nil {
			return State{}, err
		}
		state.ActiveTurnID = ""
	case TurnDiscardedEvent:
		turn, err := stateTurn(state, event.TurnID)
		if err != nil {
			return State{}, err
		}
		turn.Status = TurnDiscarded
		turn.RecoveryReason = ""
		state.Turns[turn.ID] = turn
		finishTurn(&state, turn.ID)
	default:
		return State{}, fmt.Errorf("%w: unsupported event %T", ErrInvalidArgument, event)
	}

	state.Version++
	if envelope.CommandID != "" {
		state.AppliedCommands[envelope.CommandID] = envelope.CommandDigest
	}
	return state, Validate(state)
}

func ApplyAll(state State, events []Envelope) (State, error) {
	var err error
	for _, event := range events {
		state, err = Apply(state, event)
		if err != nil {
			return State{}, err
		}
	}
	return state, nil
}

func Validate(state State) error {
	if state.Status == "" {
		return nil
	}
	if state.TenantID == "" || state.SessionID == "" {
		return fmt.Errorf("session identity is incomplete")
	}
	if state.Driver.Status == DriverAbsent {
		if state.Driver.Fence != (Fence{}) {
			return fmt.Errorf("absent driver has a fence")
		}
	} else if state.Driver.Fence == (Fence{}) || state.Driver.Generation != state.Driver.Fence.Generation {
		return fmt.Errorf("active driver fence is invalid")
	}
	activeCount := 0
	for id, turn := range state.Turns {
		if _, ok := state.Inputs[turn.InputID]; !ok {
			return fmt.Errorf("turn %q references missing input", id)
		}
		if turn.Position == 0 || turn.Position > state.NextTurnPosition {
			return fmt.Errorf("turn %q has invalid position", id)
		}
		if turn.ActiveAttemptID != "" {
			if _, ok := findAttempt(turn, turn.ActiveAttemptID); !ok {
				return fmt.Errorf("turn %q references missing active attempt", id)
			}
		}
		if !turn.Status.Terminal() && turn.Status != TurnQueued {
			activeCount++
			if state.ActiveTurnID != id {
				return fmt.Errorf("non-terminal turn %q is not active", id)
			}
		}
	}
	if activeCount > 1 {
		return fmt.Errorf("multiple turns are active")
	}
	if state.ActiveTurnID != "" {
		if len(state.Queue) == 0 || state.Queue[0] != state.ActiveTurnID {
			return fmt.Errorf("active turn is not the queue head")
		}
		if _, ok := state.Turns[state.ActiveTurnID]; !ok {
			return fmt.Errorf("active turn does not exist")
		}
	}
	seen := make(map[string]bool, len(state.Queue))
	for _, id := range state.Queue {
		turn, ok := state.Turns[id]
		if !ok {
			return fmt.Errorf("queue references missing turn %q", id)
		}
		if turn.Status.Terminal() {
			return fmt.Errorf("queue contains terminal turn %q", id)
		}
		if seen[id] {
			return fmt.Errorf("queue contains duplicate turn %q", id)
		}
		seen[id] = true
	}
	return nil
}

func finishTurn(state *State, turnID string) {
	if state.ActiveTurnID == turnID {
		state.ActiveTurnID = ""
	}
	removeQueuedTurn(state, turnID)
}

func removeQueueHead(state *State, turnID string) {
	if len(state.Queue) > 0 && state.Queue[0] == turnID {
		state.Queue = append([]string(nil), state.Queue[1:]...)
	}
}

func removeQueuedTurn(state *State, turnID string) {
	for i, queuedTurnID := range state.Queue {
		if queuedTurnID != turnID {
			continue
		}
		state.Queue = append(state.Queue[:i:i], state.Queue[i+1:]...)
		return
	}
}

func stateTurn(state State, turnID string) (Turn, error) {
	turn, ok := state.Turns[turnID]
	if !ok {
		return Turn{}, fmt.Errorf("%w: turn %q", ErrInvalidArgument, turnID)
	}
	return cloneTurn(turn), nil
}

func updateAttempt(state *State, turnID, attemptID string, update func(*Turn, *Attempt)) error {
	turn, err := stateTurn(*state, turnID)
	if err != nil {
		return err
	}
	for i := range turn.Attempts {
		if turn.Attempts[i].ID == attemptID {
			update(&turn, &turn.Attempts[i])
			state.Turns[turnID] = turn
			return nil
		}
	}
	return fmt.Errorf("%w: attempt %q", ErrInvalidArgument, attemptID)
}

func cloneState(state State) State {
	clone := state
	clone.Inputs = make(map[string]Input, len(state.Inputs))
	for id, input := range state.Inputs {
		clone.Inputs[id] = cloneInput(input)
	}
	clone.Turns = make(map[string]Turn, len(state.Turns))
	for id, turn := range state.Turns {
		clone.Turns[id] = cloneTurn(turn)
	}
	clone.Queue = append([]string(nil), state.Queue...)
	clone.AppliedCommands = make(map[string]string, len(state.AppliedCommands))
	for id, digest := range state.AppliedCommands {
		clone.AppliedCommands[id] = digest
	}
	return clone
}

func cloneInput(input Input) Input {
	input.Content = append([]byte(nil), input.Content...)
	return input
}

func cloneTurn(turn Turn) Turn {
	turn.Attempts = append([]Attempt(nil), turn.Attempts...)
	for i := range turn.Attempts {
		turn.Attempts[i].PreparedContext = append(json.RawMessage(nil), turn.Attempts[i].PreparedContext...)
		turn.Attempts[i].Outputs = append([]AttemptOutput(nil), turn.Attempts[i].Outputs...)
		for j := range turn.Attempts[i].Outputs {
			turn.Attempts[i].Outputs[j].Payload = append([]byte(nil), turn.Attempts[i].Outputs[j].Payload...)
		}
	}
	return turn
}
