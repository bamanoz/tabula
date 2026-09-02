package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestOutputServiceCommitsBeforePublishAndReplaysFromCursor(t *testing.T) {
	repository, key, fence, turnID, attemptID := permittedAttempt(t)
	service, err := NewOutputService(repository)
	if err != nil {
		t.Fatal(err)
	}
	before, err := repository.Load(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := service.Append(context.Background(), OutputRequest{Key: key, RuntimeID: "runtime-a", TurnID: turnID, AttemptID: attemptID, Fence: fence, Sequence: 1, Type: "stream.delta", Payload: json.RawMessage(`{"text":"hi"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if accepted.Cursor <= before.Cursor {
		t.Fatalf("cursor did not advance: before=%d after=%d", before.Cursor, accepted.Cursor)
	}
	events, err := service.Replay(context.Background(), key, before.Cursor, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %+v", events)
	}
	output, ok := events[0].Event.Body.(AttemptOutputAppended)
	if !ok || output.Output.Sequence != 1 || string(output.Output.Payload) != `{"text":"hi"}` {
		t.Fatalf("output event = %#v", events[0].Event.Body)
	}
	messages, err := repository.ReadOutbox(context.Background(), key, before.Cursor, 10)
	if err != nil || len(messages) != 1 || messages[0].Cursor <= events[0].Cursor {
		t.Fatalf("commit/publish ordering events=%+v outbox=%+v err=%v", events, messages, err)
	}
}

func TestOutputServiceDuplicateGapConflictAndFence(t *testing.T) {
	repository, key, fence, turnID, attemptID := permittedAttempt(t)
	service, _ := NewOutputService(repository)
	request := OutputRequest{Key: key, RuntimeID: "runtime-a", TurnID: turnID, AttemptID: attemptID, Fence: fence, Sequence: 1, Type: "usage", Payload: json.RawMessage(`{"tokens":1}`)}
	if _, err := service.Append(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	duplicate, err := service.Append(context.Background(), request)
	if err != nil || !duplicate.Duplicate {
		t.Fatalf("duplicate = %+v err=%v", duplicate, err)
	}
	conflict := request
	conflict.Payload = json.RawMessage(`{"tokens":2}`)
	if _, err := service.Append(context.Background(), conflict); !errors.Is(err, ErrCommandConflict) && !errors.Is(err, ErrOutputConflict) {
		t.Fatalf("conflict error = %v", err)
	}
	gap := request
	gap.Sequence = 3
	if _, err := service.Append(context.Background(), gap); err == nil {
		t.Fatal("expected replay required")
	} else {
		var replay *OutputReplayRequiredError
		if !errors.As(err, &replay) || replay.ExpectedSequence != 2 {
			t.Fatalf("gap error = %#v", err)
		}
	}
	stale := request
	stale.Sequence = 2
	stale.Fence.Generation--
	if _, err := service.Append(context.Background(), stale); !errors.Is(err, ErrStaleDriver) {
		t.Fatalf("stale error = %v", err)
	}
}

func TestOutputServiceSurvivesSQLiteReopen(t *testing.T) {
	path := t.TempDir() + "/sessions.db"
	repository, err := OpenSQLiteRepository(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	key, fence, turnID, attemptID := preparePermittedAttempt(t, repository)
	service, _ := NewOutputService(repository)
	first, err := service.Append(context.Background(), OutputRequest{Key: key, RuntimeID: "runtime-a", TurnID: turnID, AttemptID: attemptID, Fence: fence, Sequence: 1, Type: "tool.result", Payload: json.RawMessage(`{"tool":"read","ok":true}`)})
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLiteRepository(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	replay, err := reopened.ReadEvents(context.Background(), key, first.Cursor-2, 10)
	if err != nil || len(replay) != 1 {
		t.Fatalf("replay=%+v err=%v", replay, err)
	}
	record, err := reopened.Load(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	turn := record.State.Turns[turnID]
	if len(turn.Attempts[0].Outputs) != 1 {
		t.Fatalf("projection outputs = %+v", turn.Attempts[0].Outputs)
	}
}

func permittedAttempt(t *testing.T) (SessionRepository, SessionKey, Fence, string, string) {
	t.Helper()
	repository := NewMemoryRepository()
	key, fence, turnID, attemptID := preparePermittedAttempt(t, repository)
	return repository, key, fence, turnID, attemptID
}

func preparePermittedAttempt(t *testing.T, repository SessionRepository) (SessionKey, Fence, string, string) {
	t.Helper()
	commitSession(t, repository, "session")
	key := sessionKey()
	lease := newLeaseService(t, repository, &fakeClock{now: fakeTime(100)})
	grant, err := lease.Register(context.Background(), leaseIdentity("driver-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Ready(context.Background(), key, "runtime-a", grant.Fence); err != nil {
		t.Fatal(err)
	}
	processor, _ := NewInputProcessor(repository)
	if _, err := processor.Submit(context.Background(), InputSubmitRequest{Key: key, CommandID: "input-1", InputID: "input-1", Content: json.RawMessage(`{"text":"hello"}`)}); err != nil {
		t.Fatal(err)
	}
	execution, _ := NewTurnExecutionService(repository)
	assignment, err := execution.AssignNext(context.Background(), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execution.Prepare(context.Background(), key, "runtime-a", assignment.TurnID, assignment.AttemptID, grant.Fence); err != nil {
		t.Fatal(err)
	}
	if _, err := execution.Permit(context.Background(), key, assignment.TurnID, assignment.AttemptID, grant.Fence); err != nil {
		t.Fatal(err)
	}
	return key, grant.Fence, assignment.TurnID, assignment.AttemptID
}

func fakeTime(seconds int64) time.Time { return time.Unix(seconds, 0) }
