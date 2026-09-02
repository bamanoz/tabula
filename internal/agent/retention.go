package agent

import "sort"

const (
	MaxRetainedOutputsPerAttempt       = 256
	MaxRetainedOutputBytesPerAttempt   = 1 << 20
	MaxRetainedOutputsPerSession       = 1024
	MaxRetainedOutputBytesPerSession   = 4 << 20
	MaxRetainedReplayRecordsPerSession = 4096
)

func retainAttemptOutputs(outputs []AttemptOutput) []AttemptOutput {
	count := len(outputs)
	bytes := outputPayloadBytes(outputs)
	first := 0
	for first < count && (count-first > MaxRetainedOutputsPerAttempt || bytes > MaxRetainedOutputBytesPerAttempt) {
		bytes -= len(outputs[first].Payload)
		first++
	}
	if first == 0 {
		return outputs
	}
	return append([]AttemptOutput(nil), outputs[first:]...)
}

func retainSessionOutputs(state *State) {
	if state == nil {
		return
	}
	type attemptRef struct {
		turnID       string
		turnPosition uint64
		attemptIndex int
	}
	refs := make([]attemptRef, 0)
	totalCount := 0
	totalBytes := 0
	for turnID, turn := range state.Turns {
		for attemptIndex, attempt := range turn.Attempts {
			if len(attempt.Outputs) == 0 {
				continue
			}
			refs = append(refs, attemptRef{turnID: turnID, turnPosition: turn.Position, attemptIndex: attemptIndex})
			totalCount += len(attempt.Outputs)
			totalBytes += outputPayloadBytes(attempt.Outputs)
		}
	}
	if totalCount <= MaxRetainedOutputsPerSession && totalBytes <= MaxRetainedOutputBytesPerSession {
		return
	}
	sort.Slice(refs, func(i, j int) bool {
		if refs[i].turnPosition != refs[j].turnPosition {
			return refs[i].turnPosition < refs[j].turnPosition
		}
		if refs[i].turnID != refs[j].turnID {
			return refs[i].turnID < refs[j].turnID
		}
		return refs[i].attemptIndex < refs[j].attemptIndex
	})
	for _, ref := range refs {
		turn := state.Turns[ref.turnID]
		outputs := turn.Attempts[ref.attemptIndex].Outputs
		first := 0
		for first < len(outputs) && (totalCount > MaxRetainedOutputsPerSession || totalBytes > MaxRetainedOutputBytesPerSession) {
			totalCount--
			totalBytes -= len(outputs[first].Payload)
			first++
		}
		if first > 0 {
			turn.Attempts[ref.attemptIndex].Outputs = append([]AttemptOutput(nil), outputs[first:]...)
			state.Turns[ref.turnID] = turn
		}
		if totalCount <= MaxRetainedOutputsPerSession && totalBytes <= MaxRetainedOutputBytesPerSession {
			return
		}
	}
}

func outputPayloadBytes(outputs []AttemptOutput) int {
	total := 0
	for _, output := range outputs {
		total += len(output.Payload)
	}
	return total
}

func retainedCursor(cursor Cursor, limit Cursor) Cursor {
	if limit == 0 || cursor <= limit {
		return 0
	}
	return cursor - limit
}
