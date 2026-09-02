package agent

import (
	"context"
	"errors"
	"sync"
	"testing"
)

func TestRecoveryServiceCancellationIntentAndDriverOutcomeAreDurable(t *testing.T) {
	repository, key, fence, turnID, attemptID := permittedAttempt(t)
	service, err := NewRecoveryService(repository)
	if err != nil {
		t.Fatal(err)
	}
	record, err := service.Cancel(context.Background(), CancelRequest{Key: key, CommandID: "cancel-1", TurnID: turnID, Actor: ActorClient})
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Turns[turnID].Status != TurnCancelling {
		t.Fatalf("turn = %+v", record.State.Turns[turnID])
	}
	messages, err := repository.ReadOutbox(context.Background(), key, 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	foundCancel := false
	for _, message := range messages {
		foundCancel = foundCancel || message.Message.Topic == "turn.cancel"
	}
	if !foundCancel {
		t.Fatalf("cancel delivery missing: %+v", messages)
	}
	record, err = service.ConfirmCancellation(context.Background(), key, "cancel-confirm-1", "runtime-a", turnID, attemptID, fence)
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Turns[turnID].Status != TurnCancelled || record.State.ActiveTurnID != "" {
		t.Fatalf("cancel outcome = %+v", record.State.Turns[turnID])
	}
}

func TestRecoveryServiceCancelCompletionRaceCommitsOneTerminalOutcome(t *testing.T) {
	for range 25 {
		repository, key, fence, turnID, attemptID := permittedAttempt(t)
		service, _ := NewRecoveryService(repository)
		var wg sync.WaitGroup
		errs := make(chan error, 2)
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := service.Cancel(context.Background(), CancelRequest{Key: key, CommandID: "cancel", TurnID: turnID, Actor: ActorClient})
			if err == nil {
				_, err = service.ConfirmCancellation(context.Background(), key, "confirm", "runtime-a", turnID, attemptID, fence)
			}
			errs <- err
		}()
		go func() {
			defer wg.Done()
			_, err := service.Complete(context.Background(), key, "complete", "runtime-a", turnID, attemptID, fence)
			errs <- err
		}()
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil && !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("race error = %v", err)
			}
		}
		record, err := repository.Load(context.Background(), key)
		if err != nil {
			t.Fatal(err)
		}
		status := record.State.Turns[turnID].Status
		if status != TurnCompleted && status != TurnCancelled {
			t.Fatalf("race status = %s", status)
		}
	}
}

func TestRecoveryServiceDriverReportedPermittedFailureIsDurableAndIdempotent(t *testing.T) {
	repository, key, fence, turnID, attemptID := permittedAttempt(t)
	service, _ := NewRecoveryService(repository)

	record, err := service.Fail(context.Background(), key, "failed-1", "runtime-a", turnID, attemptID, fence, "provider rejected request")
	if err != nil {
		t.Fatal(err)
	}
	turn := record.State.Turns[turnID]
	attempt, _ := findAttempt(turn, attemptID)
	if turn.Status != TurnFailed || attempt.Status != AttemptFailed || attempt.Failure != "provider rejected request" || record.State.ActiveTurnID != "" {
		t.Fatalf("failed outcome: turn=%+v attempt=%+v", turn, attempt)
	}

	duplicate, err := service.Fail(context.Background(), key, "failed-1", "runtime-a", turnID, attemptID, fence, "provider rejected request")
	if err != nil || duplicate.Version != record.Version {
		t.Fatalf("duplicate failure record=%+v err=%v", duplicate, err)
	}
	if _, err := service.Fail(context.Background(), key, "failed-1", "runtime-a", turnID, attemptID, fence, "different failure"); !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("failure command conflict error = %v", err)
	}
	if _, err := service.Complete(context.Background(), key, "complete-after-failure", "runtime-a", turnID, attemptID, fence); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("conflicting completion error = %v", err)
	}
}

func TestRecoveryServiceDriverReportedUncertainRequiresRecoveryAndIsIdempotent(t *testing.T) {
	repository, key, fence, turnID, attemptID := permittedAttempt(t)
	service, _ := NewRecoveryService(repository)

	record, err := service.Uncertain(context.Background(), key, "uncertain-1", "runtime-a", turnID, attemptID, fence, "provider outcome unavailable", "provider-operation-42")
	if err != nil {
		t.Fatal(err)
	}
	turn := record.State.Turns[turnID]
	attempt, _ := findAttempt(turn, attemptID)
	if turn.Status != TurnRecoveryRequired || attempt.Status != AttemptUncertain || attempt.Failure != "provider outcome unavailable" || record.State.ActiveTurnID != turnID {
		t.Fatalf("uncertain outcome: turn=%+v attempt=%+v", turn, attempt)
	}

	duplicate, err := service.Uncertain(context.Background(), key, "uncertain-1", "runtime-a", turnID, attemptID, fence, "provider outcome unavailable", "provider-operation-42")
	if err != nil || duplicate.Version != record.Version {
		t.Fatalf("duplicate uncertainty record=%+v err=%v", duplicate, err)
	}
	if _, err := service.Uncertain(context.Background(), key, "uncertain-1", "runtime-a", turnID, attemptID, fence, "provider outcome unavailable", "different-evidence"); !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("uncertainty command conflict error = %v", err)
	}
	if _, err := service.Fail(context.Background(), key, "failed-after-uncertain", "runtime-a", turnID, attemptID, fence, "late failure"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("conflicting failure error = %v", err)
	}
}

func TestRecoveryServiceDriverOutcomesRequireOwningRuntimeAndFullCurrentFence(t *testing.T) {
	tests := []struct {
		name      string
		runtimeID string
		mutate    func(Fence) Fence
		want      error
	}{
		{name: "wrong runtime", runtimeID: "runtime-b", mutate: func(fence Fence) Fence { return fence }, want: ErrPermissionDenied},
		{name: "driver instance", runtimeID: "runtime-a", mutate: func(fence Fence) Fence { fence.DriverInstanceID = "stale-driver"; return fence }, want: ErrStaleDriver},
		{name: "lease", runtimeID: "runtime-a", mutate: func(fence Fence) Fence { fence.LeaseID = "stale-lease"; return fence }, want: ErrStaleDriver},
		{name: "generation", runtimeID: "runtime-a", mutate: func(fence Fence) Fence { fence.Generation++; return fence }, want: ErrStaleDriver},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository, key, fence, turnID, attemptID := permittedAttempt(t)
			service, _ := NewRecoveryService(repository)
			_, err := service.Uncertain(context.Background(), key, "uncertain", test.runtimeID, turnID, attemptID, test.mutate(fence), "provider outcome unavailable", "")
			if !errors.Is(err, test.want) {
				t.Fatalf("error = %v, want %v", err, test.want)
			}
		})
	}
}

func TestRecoveryServiceRejectsUncertainBeforePermitAndMissingReason(t *testing.T) {
	repository, key, fence := readySessionWithInput(t)
	execution, _ := NewTurnExecutionService(repository)
	assignment, err := execution.AssignNext(context.Background(), key, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := execution.Prepare(context.Background(), key, "runtime-a", assignment.TurnID, assignment.AttemptID, fence); err != nil {
		t.Fatal(err)
	}
	service, _ := NewRecoveryService(repository)
	if _, err := service.Uncertain(context.Background(), key, "uncertain-prepared", "runtime-a", assignment.TurnID, assignment.AttemptID, fence, "provider outcome unavailable", ""); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("prepared uncertainty error = %v", err)
	}
	if _, err := service.Fail(context.Background(), key, "failed-prepared", "runtime-a", assignment.TurnID, assignment.AttemptID, fence, "provider rejected request"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("prepared failure error = %v", err)
	}

	repository, key, fence, turnID, attemptID := permittedAttempt(t)
	service, _ = NewRecoveryService(repository)
	if _, err := service.Uncertain(context.Background(), key, "uncertain-no-reason", "runtime-a", turnID, attemptID, fence, "", "provider-operation-42"); !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("missing uncertainty reason error = %v", err)
	}
}

func TestRecoveryServiceRequiresAuthorityAndPersistsExplicitRetry(t *testing.T) {
	repository, key, _, turnID, _ := permittedAttempt(t)
	lease := newLeaseService(t, repository, &fakeClock{now: fakeTime(130)})
	if _, err := lease.Expire(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	service, _ := NewRecoveryService(repository)
	request := RecoveryRequest{Key: key, CommandID: "retry", TurnID: turnID, Actor: ActorClient, Action: RecoveryRetry}
	if _, err := service.Recover(context.Background(), request); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("client recovery error = %v", err)
	}
	request.Actor = ActorRecoveryService
	record, err := service.Recover(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Turns[turnID].Status != TurnQueued {
		t.Fatalf("retry state = %+v", record.State.Turns[turnID])
	}
	duplicate, err := service.Recover(context.Background(), request)
	if err != nil || duplicate.Version != record.Version {
		t.Fatalf("duplicate recovery record=%+v err=%v", duplicate, err)
	}
}

func TestRecoveryServiceStateSurvivesSQLiteRestart(t *testing.T) {
	path := t.TempDir() + "/sessions.db"
	repository, err := OpenSQLiteRepository(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	key, fence, turnID, attemptID := preparePermittedAttempt(t, repository)
	service, _ := NewRecoveryService(repository)
	if _, err := service.Cancel(context.Background(), CancelRequest{Key: key, CommandID: "cancel-restart", TurnID: turnID, Actor: ActorHuman}); err != nil {
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
	record, err := reopened.Load(context.Background(), key)
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Turns[turnID].Status != TurnCancelling || !record.State.Turns[turnID].CancellationRequested {
		t.Fatalf("restarted turn = %+v", record.State.Turns[turnID])
	}
	restarted, _ := NewRecoveryService(reopened)
	if _, err := restarted.ConfirmCancellation(context.Background(), key, "confirm-restart", "runtime-a", turnID, attemptID, fence); err != nil {
		t.Fatal(err)
	}
}
