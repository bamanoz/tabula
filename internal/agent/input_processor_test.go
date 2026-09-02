package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func TestInputProcessorDurableAcceptance(t *testing.T) {
	ctx := context.Background()
	repository := NewMemoryRepository()
	createSessionForProcessor(t, repository)
	processor, err := NewInputProcessor(repository)
	if err != nil {
		t.Fatal(err)
	}

	request := InputSubmitRequest{
		Key:       SessionKey{TenantID: "tenant", SessionID: "session"},
		CommandID: "command-1",
		InputID:   "input-1",
		Content:   json.RawMessage(`{"type":"text","text":"hello"}`),
		ActorID:   "client-1",
	}
	acceptance, err := processor.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if acceptance.InputID != request.InputID || acceptance.TurnID != "turn-command-1" || acceptance.Position != 1 || acceptance.SessionVersion != 2 || acceptance.Cursor != 3 || acceptance.Duplicate {
		t.Fatalf("unexpected acceptance: %+v", acceptance)
	}

	record, err := processor.Get(ctx, request.Key)
	if err != nil {
		t.Fatal(err)
	}
	if record.State.Turns[acceptance.TurnID].Status != TurnQueued || record.State.Turns[acceptance.TurnID].Position != acceptance.Position {
		t.Fatalf("unexpected authoritative state: %+v", record.State.Turns[acceptance.TurnID])
	}
	outbox, err := repository.ReadOutbox(ctx, request.Key, 0, 10)
	if err != nil || len(outbox) != 1 || outbox[0].Message.ID != "input.accepted/input-1" || outbox[0].Message.Topic != "input.accepted" {
		t.Fatalf("outbox=%+v err=%v", outbox, err)
	}
	var persisted InputAcceptance
	if err := json.Unmarshal(outbox[0].Message.Payload, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.SessionVersion != acceptance.SessionVersion || persisted.Cursor != acceptance.Cursor || persisted.Position != acceptance.Position {
		t.Fatalf("persisted acceptance=%+v returned=%+v", persisted, acceptance)
	}
}

func TestInputProcessorRetriesSameCommandAfterLostResponse(t *testing.T) {
	repository := NewMemoryRepository()
	createSessionForProcessor(t, repository)
	processor, err := NewInputProcessor(repository)
	if err != nil {
		t.Fatal(err)
	}
	request := InputSubmitRequest{Key: SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: "command-1", InputID: "input-1", Content: json.RawMessage(`{"text":"hello"}`)}
	first, err := processor.Submit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	second, err := processor.Submit(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || second.InputID != first.InputID || second.TurnID != first.TurnID || second.SessionVersion != first.SessionVersion || second.Cursor != first.Cursor {
		t.Fatalf("retry acceptance=%+v first=%+v", second, first)
	}
	record, err := processor.Get(context.Background(), request.Key)
	if err != nil || len(record.State.Turns) != 1 || len(record.State.Inputs) != 1 {
		t.Fatalf("duplicate created extra state: record=%+v err=%v", record, err)
	}
}

func TestInputProcessorSameInputPayloadWithNewCommandIsDuplicate(t *testing.T) {
	repository := NewMemoryRepository()
	createSessionForProcessor(t, repository)
	processor, err := NewInputProcessor(repository)
	if err != nil {
		t.Fatal(err)
	}
	first, err := processor.Submit(context.Background(), InputSubmitRequest{Key: SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: "command-1", InputID: "input-1", Content: json.RawMessage(`{"text":"hello"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := processor.Submit(context.Background(), InputSubmitRequest{Key: SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: "command-2", InputID: "input-2", Content: json.RawMessage(`{"text":"later"}`)}); err != nil {
		t.Fatal(err)
	}
	second, err := processor.Submit(context.Background(), InputSubmitRequest{Key: SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: "command-3", InputID: "input-1", Content: json.RawMessage(`{"text":"hello"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || second.InputID != first.InputID || second.TurnID != first.TurnID || second.Position != first.Position || second.SessionVersion != first.SessionVersion || second.Cursor != first.Cursor {
		t.Fatalf("same input retry changed acceptance: first=%+v second=%+v", first, second)
	}
}

func TestInputProcessorSameInputRetrySurvivesOutboxRetention(t *testing.T) {
	repository := newMemoryRepository(4)
	createSessionForProcessor(t, repository)
	processor, err := NewInputProcessor(repository)
	if err != nil {
		t.Fatal(err)
	}
	key := SessionKey{TenantID: "tenant", SessionID: "session"}
	first, err := processor.Submit(context.Background(), InputSubmitRequest{Key: key, CommandID: "command-1", InputID: "input-1", Content: json.RawMessage(`{"text":"first"}`)})
	if err != nil {
		t.Fatal(err)
	}
	for index := 2; index <= 4; index++ {
		if _, err := processor.Submit(context.Background(), InputSubmitRequest{
			Key: key, CommandID: fmt.Sprintf("command-%d", index), InputID: fmt.Sprintf("input-%d", index),
			Content: json.RawMessage(fmt.Sprintf(`{"text":"message-%d"}`, index)),
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := repository.ReadOutbox(context.Background(), key, 0, 100); !errors.Is(err, ErrCursorExpired) {
		t.Fatalf("expected original outbox cursor to expire, got %v", err)
	}
	second, err := processor.Submit(context.Background(), InputSubmitRequest{Key: key, CommandID: "command-retry", InputID: "input-1", Content: json.RawMessage(`{"text":"first"}`)})
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || second.InputID != first.InputID || second.TurnID != first.TurnID || second.Position != first.Position || second.SessionVersion != first.SessionVersion || second.Cursor != first.Cursor {
		t.Fatalf("retained retry changed acceptance: first=%+v second=%+v", first, second)
	}
}

func TestInputProcessorRejectsInputConflict(t *testing.T) {
	repository := NewMemoryRepository()
	createSessionForProcessor(t, repository)
	processor, err := NewInputProcessor(repository)
	if err != nil {
		t.Fatal(err)
	}
	base := InputSubmitRequest{Key: SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: "command-1", InputID: "input-1", Content: json.RawMessage(`{"text":"hello"}`)}
	if _, err := processor.Submit(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	_, err = processor.Submit(context.Background(), InputSubmitRequest{Key: base.Key, CommandID: "command-2", InputID: base.InputID, Content: json.RawMessage(`{"text":"different"}`)})
	if !errors.Is(err, ErrInputConflict) {
		t.Fatalf("expected input conflict, got %v", err)
	}
	_, err = processor.Submit(context.Background(), InputSubmitRequest{Key: base.Key, CommandID: base.CommandID, InputID: base.InputID, Content: json.RawMessage(`{"text":"different"}`)})
	if !errors.Is(err, ErrInputConflict) {
		t.Fatalf("expected command/input conflict, got %v", err)
	}
}

func TestInputProcessorConcurrentGatewaysProduceOneTurn(t *testing.T) {
	repository := NewMemoryRepository()
	createSessionForProcessor(t, repository)
	processor, err := NewInputProcessor(repository)
	if err != nil {
		t.Fatal(err)
	}
	request := InputSubmitRequest{Key: SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: "command-1", InputID: "input-1", Content: json.RawMessage(`{"text":"hello"}`)}
	const callers = 16
	results := make(chan InputAcceptance, callers)
	errorsCh := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			acceptance, err := processor.Submit(context.Background(), request)
			if err != nil {
				errorsCh <- err
				return
			}
			results <- acceptance
		}()
	}
	wg.Wait()
	close(results)
	close(errorsCh)
	for err := range errorsCh {
		t.Fatalf("concurrent submit failed: %v", err)
	}
	var first InputAcceptance
	count := 0
	for acceptance := range results {
		if count == 0 {
			first = acceptance
		} else if acceptance.TurnID != first.TurnID || acceptance.Position != first.Position || acceptance.SessionVersion != first.SessionVersion || acceptance.Cursor != first.Cursor {
			t.Fatalf("inconsistent concurrent acceptance: first=%+v current=%+v", first, acceptance)
		}
		count++
	}
	if count != callers {
		t.Fatalf("got %d results, want %d", count, callers)
	}
	record, err := processor.Get(context.Background(), request.Key)
	if err != nil || len(record.State.Turns) != 1 || len(record.State.Inputs) != 1 {
		t.Fatalf("concurrent submit duplicated state: record=%+v err=%v", record, err)
	}
	outbox, err := repository.ReadOutbox(context.Background(), request.Key, 0, 100)
	if err != nil || len(outbox) != 1 {
		t.Fatalf("concurrent submit duplicated acceptance outbox: outbox=%+v err=%v", outbox, err)
	}
}

func TestInputProcessorConcurrentDistinctInputsOutlastCASContention(t *testing.T) {
	base := NewMemoryRepository()
	repository := &conflictRepository{SessionRepository: base, conflictsPerCommand: 32, attempts: make(map[string]int)}
	createSessionForProcessor(t, repository)
	processor, err := NewInputProcessor(repository)
	if err != nil {
		t.Fatal(err)
	}

	const callers = 16
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	errorsCh := make(chan error, callers)
	var wg sync.WaitGroup
	for i := 0; i < callers; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := processor.Submit(ctx, InputSubmitRequest{
				Key:       SessionKey{TenantID: "tenant", SessionID: "session"},
				CommandID: fmt.Sprintf("command-%d", i),
				InputID:   fmt.Sprintf("input-%d", i),
				Content:   json.RawMessage(fmt.Sprintf(`{"text":"message-%d"}`, i)),
			})
			if err != nil {
				errorsCh <- err
			}
		}()
	}
	wg.Wait()
	close(errorsCh)
	for err := range errorsCh {
		t.Fatalf("distinct input submission failed under contention: %v", err)
	}
	record, err := processor.Get(context.Background(), SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	if len(record.State.Inputs) != callers || len(record.State.Turns) != callers {
		t.Fatalf("inputs=%d turns=%d, want %d", len(record.State.Inputs), len(record.State.Turns), callers)
	}
}

func TestInputProcessorExpectedVersion(t *testing.T) {
	repository := NewMemoryRepository()
	createSessionForProcessor(t, repository)
	processor, err := NewInputProcessor(repository)
	if err != nil {
		t.Fatal(err)
	}
	version := uint64(0)
	_, err = processor.Submit(context.Background(), InputSubmitRequest{Key: SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: "command-1", InputID: "input-1", Content: json.RawMessage(`{"text":"hello"}`), ExpectedVersion: &version})
	if !errors.Is(err, ErrVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
}

type conflictRepository struct {
	SessionRepository

	mu                  sync.Mutex
	conflictsPerCommand int
	attempts            map[string]int
}

func (r *conflictRepository) Commit(ctx context.Context, commit Commit) (CommitResult, error) {
	if commit.CommandID != "create" {
		r.mu.Lock()
		attempt := r.attempts[commit.CommandID]
		r.attempts[commit.CommandID] = attempt + 1
		r.mu.Unlock()
		if attempt < r.conflictsPerCommand {
			return CommitResult{}, fmt.Errorf("%w: injected contention", ErrVersionConflict)
		}
	}
	return r.SessionRepository.Commit(ctx, commit)
}

func createSessionForProcessor(t *testing.T, repository SessionRepository) {
	t.Helper()
	state := NewState()
	command := CreateSession{Meta: CommandMeta{ID: "create", Actor: ActorClient}, TenantID: "tenant", SessionID: "session", DriverComponentID: "driver", AgentSpecRevision: "sha256:spec"}
	events, err := Decide(state, command)
	if err != nil {
		t.Fatal(err)
	}
	next, err := ApplyAll(state, events)
	if err != nil {
		t.Fatal(err)
	}
	digest, err := DigestCommand(command)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repository.Commit(context.Background(), Commit{Key: SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: command.Meta.ID, CommandDigest: digest, ExpectedVersion: state.Version, Events: events, Projection: next}); err != nil {
		t.Fatal(err)
	}
}
