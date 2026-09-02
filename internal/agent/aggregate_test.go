package agent

import (
	"encoding/json"
	"errors"
	"math/rand"
	"testing"
	"time"
)

func TestTurnLifecycleAndFIFO(t *testing.T) {
	state := createdState(t)
	state = run(t, state, SubmitInput{Meta: clientMeta("submit-1"), InputID: "input-1", TurnID: "turn-1", Content: json.RawMessage(`{"text":"first"}`)})
	state = run(t, state, SubmitInput{Meta: clientMeta("submit-2"), InputID: "input-2", TurnID: "turn-2", Content: json.RawMessage(`{"text":"second"}`)})
	fence := grantReadyDriver(t, &state, 1)

	state = run(t, state, AssignTurn{Meta: kernelMeta("assign-1"), TurnID: "turn-1", AttemptID: "attempt-1", Fence: fence})
	state = run(t, state, PrepareAttempt{Meta: driverMeta("prepare-1"), TurnID: "turn-1", AttemptID: "attempt-1", Fence: fence})
	state = run(t, state, PermitAttempt{Meta: kernelMeta("permit-1"), TurnID: "turn-1", AttemptID: "attempt-1", PermitID: "permit-1", Fence: fence})
	state = run(t, state, CompleteAttempt{Meta: driverMeta("complete-1"), TurnID: "turn-1", AttemptID: "attempt-1", Fence: fence})

	if state.Turns["turn-1"].Status != TurnCompleted {
		t.Fatalf("turn-1 status = %s", state.Turns["turn-1"].Status)
	}
	if len(state.Queue) != 1 || state.Queue[0] != "turn-2" || state.ActiveTurnID != "" {
		t.Fatalf("unexpected queue after completion: queue=%v active=%q", state.Queue, state.ActiveTurnID)
	}

	state = run(t, state, AssignTurn{Meta: kernelMeta("assign-2"), TurnID: "turn-2", AttemptID: "attempt-2", Fence: fence})
	if state.ActiveTurnID != "turn-2" || state.Turns["turn-2"].Status != TurnPreparing {
		t.Fatalf("second turn was not assigned: %+v", state.Turns["turn-2"])
	}
}

func TestSubmitInputIdempotencyAndConflict(t *testing.T) {
	state := createdState(t)
	command := SubmitInput{Meta: clientMeta("submit-1"), InputID: "input-1", TurnID: "turn-1", Content: json.RawMessage(`{"text":"hello"}`)}
	state = run(t, state, command)

	events, err := Decide(state, command)
	if err != nil || len(events) != 0 {
		t.Fatalf("duplicate command: events=%v err=%v", events, err)
	}

	_, err = Decide(state, SubmitInput{Meta: clientMeta("submit-2"), InputID: "input-1", TurnID: "turn-2", Content: json.RawMessage(`{"text":"different"}`)})
	if !errors.Is(err, ErrInputConflict) {
		t.Fatalf("expected input conflict, got %v", err)
	}

	_, err = Decide(state, SubmitInput{Meta: clientMeta("submit-1"), InputID: "input-2", TurnID: "turn-2", Content: json.RawMessage(`{"text":"hello"}`)})
	if !errors.Is(err, ErrCommandConflict) {
		t.Fatalf("expected command conflict, got %v", err)
	}
}

func TestDriverDisconnectGraceAndStaleGeneration(t *testing.T) {
	state := createdState(t)
	state = run(t, state, SubmitInput{Meta: clientMeta("submit"), InputID: "input", TurnID: "turn", Content: json.RawMessage(`{"text":"hello"}`)})
	first := grantReadyDriver(t, &state, 1)
	state = run(t, state, MarkDriverSuspect{Meta: kernelMeta("suspect"), Fence: first})

	if state.Driver.Status != DriverSuspect {
		t.Fatalf("driver status = %s", state.Driver.Status)
	}
	_, err := Decide(state, AssignTurn{Meta: kernelMeta("assign-suspect"), TurnID: "turn", AttemptID: "attempt", Fence: first})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected suspect driver assignment rejection, got %v", err)
	}

	state = run(t, state, RenewDriverLease{Meta: driverMeta("renew"), Fence: first, RuntimeID: "runtime", HeartbeatSequence: 1, Deadline: time.Unix(20, 0)})
	if state.Driver.Status != DriverReady || state.Driver.Fence != first {
		t.Fatalf("driver did not resume same lease: %+v", state.Driver)
	}
	state = run(t, state, MarkDriverSuspect{Meta: kernelMeta("suspect-2"), Fence: first})
	state = run(t, state, ExpireDriverLease{Meta: kernelMeta("expire"), Now: time.Unix(20, 0)})
	second := grantReadyDriver(t, &state, 2)

	if second.Generation != first.Generation+1 {
		t.Fatalf("generation = %d, want %d", second.Generation, first.Generation+1)
	}
	_, err = Decide(state, MarkDriverReady{Meta: driverMeta("stale-ready"), Fence: first})
	if !errors.Is(err, ErrStaleDriver) {
		t.Fatalf("expected stale driver, got %v", err)
	}
}

func TestLeaseExpiryBeforePermitRetriesSafely(t *testing.T) {
	state := createdState(t)
	state = run(t, state, SubmitInput{Meta: clientMeta("submit"), InputID: "input", TurnID: "turn", Content: json.RawMessage(`{"text":"hello"}`)})
	fence := grantReadyDriver(t, &state, 1)
	state = run(t, state, AssignTurn{Meta: kernelMeta("assign"), TurnID: "turn", AttemptID: "attempt-1", Fence: fence})
	state = run(t, state, PrepareAttempt{Meta: driverMeta("prepare"), TurnID: "turn", AttemptID: "attempt-1", Fence: fence})
	state = run(t, state, ExpireDriverLease{Meta: kernelMeta("expire"), Now: time.Unix(10, 0)})

	turn := state.Turns["turn"]
	if turn.Status != TurnQueued || turn.ActiveAttemptID != "" || state.ActiveTurnID != "" {
		t.Fatalf("pre-permit loss was not safely queued: %+v", turn)
	}
	attempt, _ := findAttempt(turn, "attempt-1")
	if attempt.Status != AttemptAbandoned {
		t.Fatalf("attempt status = %s", attempt.Status)
	}
}

func TestLeaseExpiryBeforePermitFinishesRequestedCancellation(t *testing.T) {
	state := createdState(t)
	state = run(t, state, SubmitInput{Meta: clientMeta("submit"), InputID: "input", TurnID: "turn", Content: json.RawMessage(`{"text":"hello"}`)})
	fence := grantReadyDriver(t, &state, 1)
	state = run(t, state, AssignTurn{Meta: kernelMeta("assign"), TurnID: "turn", AttemptID: "attempt", Fence: fence})
	state = run(t, state, CancelTurn{Meta: clientMeta("cancel"), TurnID: "turn"})
	state = run(t, state, ExpireDriverLease{Meta: kernelMeta("expire"), Now: time.Unix(10, 0)})
	if state.Turns["turn"].Status != TurnCancelled || state.ActiveTurnID != "" || len(state.Queue) != 0 {
		t.Fatalf("cancelled pre-permit turn was requeued: %+v", state.Turns["turn"])
	}
}

func TestLeaseExpiryAfterPermitRequiresRecovery(t *testing.T) {
	state := permittedState(t)
	state = run(t, state, ExpireDriverLease{Meta: kernelMeta("expire"), Now: time.Unix(10, 0)})

	turn := state.Turns["turn"]
	if turn.Status != TurnRecoveryRequired || state.ActiveTurnID != "turn" {
		t.Fatalf("post-permit loss did not require recovery: %+v", turn)
	}
	attempt, _ := activeAttempt(turn)
	if attempt.Status != AttemptUncertain {
		t.Fatalf("attempt status = %s", attempt.Status)
	}

	_, err := Decide(state, RecoverTurn{Meta: clientMeta("retry-client"), TurnID: "turn", Action: RecoveryRetry})
	if !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("client recovery should be denied, got %v", err)
	}

	state = run(t, state, RecoverTurn{Meta: humanMeta("retry-human"), TurnID: "turn", Action: RecoveryRetry})
	turn = state.Turns["turn"]
	if turn.Status != TurnQueued || turn.ActiveAttemptID != "" || state.ActiveTurnID != "" {
		t.Fatalf("explicit retry did not requeue turn: %+v", turn)
	}
}

func TestRecoveryRequiredCanBeCancelled(t *testing.T) {
	state := permittedState(t)
	state = run(t, state, ExpireDriverLease{Meta: kernelMeta("expire"), Now: time.Unix(10, 0)})
	state = run(t, state, CancelTurn{Meta: humanMeta("cancel"), TurnID: "turn"})
	if state.Turns["turn"].Status != TurnCancelled || state.ActiveTurnID != "" || len(state.Queue) != 0 {
		t.Fatalf("recovery cancellation did not finish turn: %+v", state.Turns["turn"])
	}
}

func TestRetryablePreparationFailureCreatesNewAttempt(t *testing.T) {
	state := createdState(t)
	state = run(t, state, SubmitInput{Meta: clientMeta("submit"), InputID: "input", TurnID: "turn", Content: json.RawMessage(`{"text":"hello"}`)})
	fence := grantReadyDriver(t, &state, 1)
	state = run(t, state, AssignTurn{Meta: kernelMeta("assign-1"), TurnID: "turn", AttemptID: "attempt-1", Fence: fence})
	state = run(t, state, FailAttempt{Meta: driverMeta("fail-1"), TurnID: "turn", AttemptID: "attempt-1", Fence: fence, Reason: "temporary initialization failure", Retryable: true})
	if state.Turns["turn"].Status != TurnQueued || state.ActiveTurnID != "" {
		t.Fatalf("retryable preparation failure was not queued: %+v", state.Turns["turn"])
	}
	state = run(t, state, AssignTurn{Meta: kernelMeta("assign-2"), TurnID: "turn", AttemptID: "attempt-2", Fence: fence})
	if len(state.Turns["turn"].Attempts) != 2 || state.Turns["turn"].ActiveAttemptID != "attempt-2" {
		t.Fatalf("replacement attempt was not created: %+v", state.Turns["turn"])
	}
}

func TestResumeRequiresEvidenceAndCurrentFence(t *testing.T) {
	state := permittedState(t)
	state = run(t, state, ExpireDriverLease{Meta: kernelMeta("expire"), Now: time.Unix(10, 0)})
	newFence := grantReadyDriver(t, &state, 2)

	_, err := Decide(state, RecoverTurn{Meta: humanMeta("resume-no-evidence"), TurnID: "turn", Action: RecoveryResume, Fence: newFence})
	if !errors.Is(err, ErrInvalidArgument) {
		t.Fatalf("expected missing evidence error, got %v", err)
	}
	state = run(t, state, RecoverTurn{Meta: humanMeta("resume"), TurnID: "turn", Action: RecoveryResume, Fence: newFence, ReconciliationEvidence: "provider-operation-42"})
	turn := state.Turns["turn"]
	attempt, _ := activeAttempt(turn)
	if turn.Status != TurnExecuting || attempt.Status != AttemptPermitted || attempt.DriverGeneration != newFence.Generation {
		t.Fatalf("attempt was not resumed under new fence: turn=%+v attempt=%+v", turn, attempt)
	}
}

func TestQueuedCancellationIsTerminal(t *testing.T) {
	state := createdState(t)
	state = run(t, state, SubmitInput{Meta: clientMeta("submit"), InputID: "input", TurnID: "turn", Content: json.RawMessage(`{"text":"hello"}`)})
	state = run(t, state, CancelTurn{Meta: clientMeta("cancel"), TurnID: "turn"})
	if state.Turns["turn"].Status != TurnCancelled || len(state.Queue) != 0 {
		t.Fatalf("queued cancellation did not finish turn: %+v", state.Turns["turn"])
	}
}

func TestCompletionWinsAfterCancellationIntent(t *testing.T) {
	state := permittedState(t)
	fence := state.Driver.Fence
	state = run(t, state, CancelTurn{Meta: clientMeta("cancel"), TurnID: "turn"})
	state = run(t, state, CompleteAttempt{Meta: driverMeta("complete"), TurnID: "turn", AttemptID: "attempt", Fence: fence})
	if state.Turns["turn"].Status != TurnCompleted {
		t.Fatalf("completion did not win: %s", state.Turns["turn"].Status)
	}
	_, err := Decide(state, ConfirmCancellation{Meta: driverMeta("cancelled"), TurnID: "turn", AttemptID: "attempt", Fence: fence})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected terminal cancellation rejection, got %v", err)
	}
}

func TestArchiveSuspendAndDeleteRules(t *testing.T) {
	state := createdState(t)
	state = run(t, state, ArchiveSession{Meta: humanMeta("archive")})
	_, err := Decide(state, SubmitInput{Meta: clientMeta("submit"), InputID: "input", TurnID: "turn", Content: json.RawMessage(`{"text":"hello"}`)})
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("archived session accepted input: %v", err)
	}
	state = run(t, state, UnarchiveSession{Meta: humanMeta("unarchive")})
	state = run(t, state, SuspendSession{Meta: humanMeta("suspend")})
	state = run(t, state, ResumeSession{Meta: humanMeta("resume")})
	state = run(t, state, DeleteSession{Meta: humanMeta("delete")})
	if state.Status != SessionClosed {
		t.Fatalf("session status = %s", state.Status)
	}
}

func TestGeneratedCommandSequencesPreserveInvariants(t *testing.T) {
	for seed := int64(0); seed < 100; seed++ {
		rng := rand.New(rand.NewSource(seed))
		state := createdState(t)
		for step := 0; step < 100; step++ {
			command := generatedCommand(state, rng, seed, step)
			events, err := Decide(state, command)
			if err != nil {
				continue
			}
			state, err = ApplyAll(state, events)
			if err != nil {
				t.Fatalf("seed=%d step=%d command=%T apply: %v", seed, step, command, err)
			}
			if err := Validate(state); err != nil {
				t.Fatalf("seed=%d step=%d command=%T validate: %v", seed, step, command, err)
			}
		}
	}
}

func generatedCommand(state State, rng *rand.Rand, seed int64, step int) Command {
	id := func(prefix string) string { return prefix + "-" + stringID(seed, step) }
	turnID := "turn-" + stringID(seed, rng.Intn(step+1))
	switch rng.Intn(8) {
	case 0:
		return SubmitInput{Meta: clientMeta(id("submit")), InputID: "input-" + stringID(seed, step), TurnID: "turn-" + stringID(seed, step), Content: json.RawMessage(`{"text":"generated"}`)}
	case 1:
		return CancelTurn{Meta: clientMeta(id("cancel")), TurnID: turnID}
	case 2:
		return ArchiveSession{Meta: humanMeta(id("archive"))}
	case 3:
		return UnarchiveSession{Meta: humanMeta(id("unarchive"))}
	case 4:
		return GrantDriverLease{Meta: kernelMeta(id("grant")), RuntimeID: "runtime", ComponentID: "driver", AgentSpecRevision: "sha256:spec", DriverInstanceID: id("driver"), LeaseID: id("lease"), Deadline: time.Unix(int64(step+10), 0)}
	case 5:
		return MarkDriverReady{Meta: driverMeta(id("ready")), Fence: state.Driver.Fence}
	case 6:
		if len(state.Queue) > 0 {
			return AssignTurn{Meta: kernelMeta(id("assign")), TurnID: state.Queue[0], AttemptID: id("attempt"), Fence: state.Driver.Fence}
		}
		return CancelTurn{Meta: clientMeta(id("cancel-empty")), TurnID: turnID}
	default:
		return ExpireDriverLease{Meta: kernelMeta(id("expire")), Now: time.Unix(int64(step+100), 0)}
	}
}

func createdState(t *testing.T) State {
	t.Helper()
	return run(t, NewState(), CreateSession{Meta: clientMeta("create"), TenantID: "tenant", SessionID: "session", DriverComponentID: "driver", AgentSpecRevision: "sha256:spec"})
}

func permittedState(t *testing.T) State {
	t.Helper()
	state := createdState(t)
	state = run(t, state, SubmitInput{Meta: clientMeta("submit"), InputID: "input", TurnID: "turn", Content: json.RawMessage(`{"text":"hello"}`)})
	fence := grantReadyDriver(t, &state, 1)
	state = run(t, state, AssignTurn{Meta: kernelMeta("assign"), TurnID: "turn", AttemptID: "attempt", Fence: fence})
	state = run(t, state, PrepareAttempt{Meta: driverMeta("prepare"), TurnID: "turn", AttemptID: "attempt", Fence: fence})
	return run(t, state, PermitAttempt{Meta: kernelMeta("permit"), TurnID: "turn", AttemptID: "attempt", PermitID: "permit", Fence: fence})
}

func grantReadyDriver(t *testing.T, state *State, instance int) Fence {
	t.Helper()
	prefix := string(rune('0' + instance))
	*state = run(t, *state, GrantDriverLease{Meta: kernelMeta("grant-" + prefix), RuntimeID: "runtime", ComponentID: "driver", AgentSpecRevision: "sha256:spec", DriverInstanceID: "driver-" + prefix, LeaseID: "lease-" + prefix, Deadline: time.Unix(10, 0)})
	fence := state.Driver.Fence
	*state = run(t, *state, MarkDriverReady{Meta: driverMeta("ready-" + prefix), Fence: fence})
	return fence
}

func run(t *testing.T, state State, command Command) State {
	t.Helper()
	events, err := Decide(state, command)
	if err != nil {
		t.Fatalf("decide %T: %v", command, err)
	}
	state, err = ApplyAll(state, events)
	if err != nil {
		t.Fatalf("apply %T: %v", command, err)
	}
	return state
}

func clientMeta(id string) CommandMeta { return CommandMeta{ID: id, Actor: ActorClient} }
func driverMeta(id string) CommandMeta { return CommandMeta{ID: id, Actor: ActorDriver} }
func kernelMeta(id string) CommandMeta { return CommandMeta{ID: id, Actor: ActorKernel} }
func humanMeta(id string) CommandMeta  { return CommandMeta{ID: id, Actor: ActorHuman} }

func stringID(seed int64, step int) string {
	return time.Unix(seed, int64(step)).UTC().Format("150405.000000000")
}
