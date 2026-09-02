package agent

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

func TestTurnExecutionServiceAssignPreparePermitIsDurableAndRedeliverable(t *testing.T) {
	repository, key, fence := readySessionWithInput(t)
	service, err := NewTurnExecutionService(repository)
	if err != nil {
		t.Fatal(err)
	}
	assignment, err := service.AssignNext(context.Background(), key, json.RawMessage(`{"tools":["read"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if assignment.TurnID == "" || assignment.AttemptID == "" || assignment.Fence != fence || string(assignment.Input) != `{"text":"hello"}` {
		t.Fatalf("assignment = %+v", assignment)
	}
	duplicate, err := service.AssignNext(context.Background(), key, assignment.PreparedContext)
	if err != nil || duplicate.AttemptID != assignment.AttemptID {
		t.Fatalf("duplicate assignment = %+v err=%v", duplicate, err)
	}
	if _, err := service.Prepare(context.Background(), key, "runtime-a", assignment.TurnID, assignment.AttemptID, fence); err != nil {
		t.Fatal(err)
	}
	permit, err := service.Permit(context.Background(), key, assignment.TurnID, assignment.AttemptID, fence)
	if err != nil {
		t.Fatal(err)
	}
	duplicatePermit, err := service.Permit(context.Background(), key, assignment.TurnID, assignment.AttemptID, fence)
	if err != nil || duplicatePermit.PermitID != permit.PermitID {
		t.Fatalf("duplicate permit = %+v err=%v", duplicatePermit, err)
	}
	messages, err := repository.ReadOutbox(context.Background(), key, 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	topics := map[string]bool{}
	for _, message := range messages {
		topics[message.Message.Topic] = true
	}
	if !topics["turn.assign"] || !topics["turn.permit"] {
		t.Fatalf("outbox = %+v", messages)
	}
}

func TestTurnExecutionServicePrepareFailureIsSafelyRetryable(t *testing.T) {
	repository, key, fence := readySessionWithInput(t)
	service, _ := NewTurnExecutionService(repository)
	assignment, err := service.AssignNext(context.Background(), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	record, err := service.PrepareFailed(context.Background(), key, "runtime-a", assignment.TurnID, assignment.AttemptID, fence, "configuration unavailable", true)
	if err != nil {
		t.Fatal(err)
	}
	turn := record.State.Turns[assignment.TurnID]
	if turn.Status != TurnQueued || record.State.ActiveTurnID != "" || turn.Attempts[0].Status != AttemptFailed {
		t.Fatalf("turn = %+v", turn)
	}
	next, err := service.AssignNext(context.Background(), key, nil)
	if err != nil || next.AttemptID == assignment.AttemptID {
		t.Fatalf("next assignment = %+v err=%v", next, err)
	}
}

func TestTurnExecutionServiceRejectsWrongRuntimeAndUnpreparedPermit(t *testing.T) {
	repository, key, fence := readySessionWithInput(t)
	service, _ := NewTurnExecutionService(repository)
	assignment, err := service.AssignNext(context.Background(), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Prepare(context.Background(), key, "runtime-b", assignment.TurnID, assignment.AttemptID, fence); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("wrong runtime error = %v", err)
	}
	if _, err := service.Permit(context.Background(), key, assignment.TurnID, assignment.AttemptID, fence); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("unprepared permit error = %v", err)
	}
}

func TestCrashWindowsRemainRetryableBeforePermitAndUncertainAfterPermit(t *testing.T) {
	repository, key, fence := readySessionWithInput(t)
	service, _ := NewTurnExecutionService(repository)
	assignment, _ := service.AssignNext(context.Background(), key, nil)
	lease := newLeaseService(t, repository, &fakeClock{now: time.Unix(130, 0)})
	before, err := lease.Expire(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if before.State.Turns[assignment.TurnID].Status != TurnQueued {
		t.Fatalf("before permit turn = %+v", before.State.Turns[assignment.TurnID])
	}

	grant, err := lease.Register(context.Background(), leaseIdentity("driver-2"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Ready(context.Background(), key, "runtime-a", grant.Fence); err != nil {
		t.Fatal(err)
	}
	second, _ := service.AssignNext(context.Background(), key, nil)
	if _, err := service.Prepare(context.Background(), key, "runtime-a", second.TurnID, second.AttemptID, grant.Fence); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Permit(context.Background(), key, second.TurnID, second.AttemptID, grant.Fence); err != nil {
		t.Fatal(err)
	}
	lease.clock = &fakeClock{now: time.Unix(160, 0)}
	after, err := lease.Expire(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if after.State.Turns[second.TurnID].Status != TurnRecoveryRequired {
		t.Fatalf("after permit turn = %+v", after.State.Turns[second.TurnID])
	}
	_ = fence
}

func readySessionWithInput(t *testing.T) (SessionRepository, SessionKey, Fence) {
	t.Helper()
	repository := NewMemoryRepository()
	commitSession(t, repository, "session")
	key := sessionKey()
	lease := newLeaseService(t, repository, &fakeClock{now: time.Unix(100, 0)})
	grant, err := lease.Register(context.Background(), leaseIdentity("driver-1"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := lease.Ready(context.Background(), key, "runtime-a", grant.Fence); err != nil {
		t.Fatal(err)
	}
	processor, err := NewInputProcessor(repository)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := processor.Submit(context.Background(), InputSubmitRequest{Key: key, CommandID: "input-1", InputID: "input-1", Content: json.RawMessage(`{"text":"hello"}`)}); err != nil {
		t.Fatal(err)
	}
	return repository, key, grant.Fence
}
