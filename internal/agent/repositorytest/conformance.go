package repositorytest

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/bamanoz/tabula/internal/agent"
)

type Factory func(t *testing.T) agent.SessionRepository

func Run(t *testing.T, factory Factory) {
	t.Helper()
	t.Run("atomic commit and replay", func(t *testing.T) {
		repository := factory(t)
		ctx := context.Background()
		state := agent.NewState()
		state, create := decide(t, state, agent.CreateSession{Meta: meta("create", agent.ActorClient), TenantID: "tenant", SessionID: "session", DriverComponentID: "driver", AgentSpecRevision: "sha256:spec"})
		result := commit(t, ctx, repository, agent.NewState(), state, create, []agent.OutboxMessage{{ID: "notify-create", Topic: "session.created", Payload: []byte(`{"session_id":"session"}`)}})
		if result.Record.Version != 1 || result.Record.Cursor != 2 || len(result.Outbox) != 1 {
			t.Fatalf("unexpected first commit result: %+v", result)
		}
		events, err := repository.ReadEvents(ctx, result.Record.Key, 0, 10)
		if err != nil || len(events) != 1 || events[0].Cursor != 1 || events[0].ID == "" {
			t.Fatalf("events=%+v err=%v", events, err)
		}
		outbox, err := repository.ReadOutbox(ctx, result.Record.Key, 0, 10)
		if err != nil || len(outbox) != 1 || outbox[0].Cursor != 2 {
			t.Fatalf("outbox=%+v err=%v", outbox, err)
		}
		replayed, err := agent.ApplyAll(agent.NewState(), envelopes(events))
		if err != nil || replayed.Version != result.Record.State.Version || replayed.SessionID != result.Record.State.SessionID {
			t.Fatalf("replayed=%+v err=%v", replayed, err)
		}
	})

	t.Run("command deduplication precedes cas", func(t *testing.T) {
		repository := factory(t)
		ctx := context.Background()
		base := agent.NewState()
		state, events := decide(t, base, agent.CreateSession{Meta: meta("create", agent.ActorClient), TenantID: "tenant", SessionID: "session", DriverComponentID: "driver", AgentSpecRevision: "sha256:spec"})
		firstRequest := makeCommit(base, state, events, nil)
		first, err := repository.Commit(ctx, firstRequest)
		if err != nil {
			t.Fatalf("commit first command: %v", err)
		}

		laterState, laterEvents := decide(t, state, submit("later", "input-later", "turn-later"))
		later := commit(t, ctx, repository, state, laterState, laterEvents, []agent.OutboxMessage{{ID: "later", Topic: "input.accepted"}})

		firstRequest.ExpectedVersion = 999
		duplicate, err := repository.Commit(ctx, firstRequest)
		if err != nil || !duplicate.Duplicate {
			t.Fatalf("duplicate=%+v err=%v", duplicate, err)
		}
		if duplicate.Record.Version != later.Record.Version || duplicate.Record.Cursor != later.Record.Cursor {
			t.Fatalf("duplicate current record=%+v later=%+v", duplicate.Record, later.Record)
		}
		if duplicate.CommandVersion != first.Record.Version || duplicate.CommandCursor != first.Record.Cursor {
			t.Fatalf("duplicate command boundary=(%d,%d) first=(%d,%d)", duplicate.CommandVersion, duplicate.CommandCursor, first.Record.Version, first.Record.Cursor)
		}
		if len(duplicate.Outbox) != 0 {
			t.Fatalf("duplicate returned historical outbox: %+v", duplicate.Outbox)
		}

		firstRequest.CommandDigest = "different"
		_, err = repository.Commit(ctx, firstRequest)
		if !errors.Is(err, agent.ErrCommandConflict) {
			t.Fatalf("expected command conflict, got %v", err)
		}
	})

	t.Run("cas conflict is atomic", func(t *testing.T) {
		repository := factory(t)
		ctx := context.Background()
		state := createSession(t, ctx, repository)
		leftState, leftEvents := decide(t, state, submit("left", "input-left", "turn-left"))
		rightState, rightEvents := decide(t, state, submit("right", "input-right", "turn-right"))
		left := makeCommit(state, leftState, leftEvents, []agent.OutboxMessage{{ID: "left", Topic: "input.accepted"}})
		right := makeCommit(state, rightState, rightEvents, []agent.OutboxMessage{{ID: "right", Topic: "input.accepted"}})

		var wg sync.WaitGroup
		results := make(chan error, 2)
		for _, request := range []agent.Commit{left, right} {
			wg.Add(1)
			go func(request agent.Commit) {
				defer wg.Done()
				_, err := repository.Commit(ctx, request)
				results <- err
			}(request)
		}
		wg.Wait()
		close(results)
		successes := 0
		conflicts := 0
		for err := range results {
			switch {
			case err == nil:
				successes++
			case errors.Is(err, agent.ErrVersionConflict):
				conflicts++
			default:
				t.Fatalf("unexpected commit error: %v", err)
			}
		}
		if successes != 1 || conflicts != 1 {
			t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
		}
		key := agent.SessionKey{TenantID: "tenant", SessionID: "session"}
		events, err := repository.ReadEvents(ctx, key, 0, 10)
		if err != nil || len(events) != 2 {
			t.Fatalf("events=%+v err=%v", events, err)
		}
		outbox, err := repository.ReadOutbox(ctx, key, 0, 10)
		if err != nil || len(outbox) != 1 {
			t.Fatalf("outbox=%+v err=%v", outbox, err)
		}
	})

	t.Run("returned values are defensive copies", func(t *testing.T) {
		repository := factory(t)
		ctx := context.Background()
		base := agent.NewState()
		next, events := decide(t, base, agent.CreateSession{Meta: meta("create", agent.ActorClient), TenantID: "tenant", SessionID: "session", DriverComponentID: "driver", AgentSpecRevision: "sha256:spec"})
		result := commit(t, ctx, repository, base, next, events, []agent.OutboxMessage{{ID: "message", Topic: "session.created", Payload: []byte("original")}})
		result.Record.State.Status = agent.SessionClosed
		result.Outbox[0].Message.Payload[0] = 'X'
		loaded, err := repository.Load(ctx, agent.SessionKey{TenantID: "tenant", SessionID: "session"})
		if err != nil || loaded.State.Status != agent.SessionOpen {
			t.Fatalf("loaded=%+v err=%v", loaded, err)
		}
		outbox, err := repository.ReadOutbox(ctx, loaded.Key, 0, 10)
		if err != nil || string(outbox[0].Message.Payload) != "original" {
			t.Fatalf("outbox=%+v err=%v", outbox, err)
		}
	})

	t.Run("event ordering and pagination", func(t *testing.T) {
		repository := factory(t)
		ctx := context.Background()
		state := createSession(t, ctx, repository)
		for i, id := range []string{"a", "b", "c"} {
			next, events := decide(t, state, submit(id, "input-"+id, "turn-"+id))
			result := commit(t, ctx, repository, state, next, events, nil)
			state = result.Record.State
			if result.Record.Version != uint64(i+2) {
				t.Fatalf("version=%d", result.Record.Version)
			}
		}
		key := agent.SessionKey{TenantID: "tenant", SessionID: "session"}
		first, err := repository.ReadEvents(ctx, key, 1, 2)
		if err != nil || len(first) != 2 || first[0].Version != 2 || first[1].Version != 3 {
			t.Fatalf("first page=%+v err=%v", first, err)
		}
		second, err := repository.ReadEvents(ctx, key, first[1].Cursor, 2)
		if err != nil || len(second) != 1 || second[0].Version != 4 {
			t.Fatalf("second page=%+v err=%v", second, err)
		}
	})

	t.Run("no-op command is durable", func(t *testing.T) {
		repository := factory(t)
		ctx := context.Background()
		state := createSession(t, ctx, repository)
		command := agent.UnarchiveSession{Meta: meta("noop", agent.ActorHuman)}
		next, events := decide(t, state, command)
		if len(events) != 0 {
			t.Fatalf("expected no events, got %d", len(events))
		}
		digest, err := agent.DigestCommand(command)
		if err != nil {
			t.Fatalf("digest command: %v", err)
		}
		request := makeCommit(state, next, events, nil)
		request.CommandID = command.Meta.ID
		request.CommandDigest = digest
		first, err := repository.Commit(ctx, request)
		if err != nil {
			t.Fatalf("commit no-op: %v", err)
		}
		request.ExpectedVersion = 99
		second, err := repository.Commit(ctx, request)
		if err != nil || !second.Duplicate || second.Record.Version != first.Record.Version {
			t.Fatalf("second=%+v err=%v", second, err)
		}
	})

	t.Run("corrupt projection is rejected atomically", func(t *testing.T) {
		repository := factory(t)
		ctx := context.Background()
		state := agent.NewState()
		next, events := decide(t, state, agent.CreateSession{Meta: meta("create", agent.ActorClient), TenantID: "tenant", SessionID: "session", DriverComponentID: "driver", AgentSpecRevision: "sha256:spec"})
		next.Archived = true
		request := makeCommit(state, next, events, []agent.OutboxMessage{{ID: "bad", Topic: "bad"}})
		_, err := repository.Commit(ctx, request)
		if !errors.Is(err, agent.ErrCorruptState) {
			t.Fatalf("expected corrupt state, got %v", err)
		}
		key := agent.SessionKey{TenantID: "tenant", SessionID: "session"}
		_, err = repository.Load(ctx, key)
		if !errors.Is(err, agent.ErrNotFound) {
			t.Fatalf("failed commit leaked state: %v", err)
		}
	})

	t.Run("deleted session rejects later commits", func(t *testing.T) {
		repository := factory(t)
		ctx := context.Background()
		state := createSession(t, ctx, repository)
		deleted, events := decide(t, state, agent.DeleteSession{Meta: meta("delete", agent.ActorHuman)})
		state = commit(t, ctx, repository, state, deleted, events, nil).Record.State
		resurrected, events := decide(t, agent.NewState(), agent.CreateSession{Meta: meta("resurrect", agent.ActorClient), TenantID: "tenant", SessionID: "session", DriverComponentID: "driver", AgentSpecRevision: "sha256:spec"})
		request := makeCommit(agent.NewState(), resurrected, events, nil)
		request.ExpectedVersion = state.Version
		_, err := repository.Commit(ctx, request)
		if !errors.Is(err, agent.ErrSessionClosed) {
			t.Fatalf("expected deleted session, got %v", err)
		}
	})

	t.Run("listing is tenant scoped and stable", func(t *testing.T) {
		repository := factory(t)
		ctx := context.Background()
		for _, sessionID := range []string{"c", "a", "b"} {
			base := agent.NewState()
			next, events := decide(t, base, agent.CreateSession{Meta: meta("create-"+sessionID, agent.ActorClient), TenantID: "tenant", SessionID: sessionID, DriverComponentID: "driver", AgentSpecRevision: "sha256:spec"})
			commit(t, ctx, repository, base, next, events, nil)
		}
		page, err := repository.List(ctx, agent.SessionQuery{TenantID: "tenant", Limit: 2})
		if err != nil || len(page.Records) != 2 || page.Records[0].Key.SessionID != "a" || page.Records[1].Key.SessionID != "b" || page.NextAfterSessionID != "b" {
			t.Fatalf("page=%+v err=%v", page, err)
		}
		next, err := repository.List(ctx, agent.SessionQuery{TenantID: "tenant", AfterSessionID: page.NextAfterSessionID, Limit: 2})
		if err != nil || len(next.Records) != 1 || next.Records[0].Key.SessionID != "c" {
			t.Fatalf("next=%+v err=%v", next, err)
		}
	})

	t.Run("context cancellation and missing session", func(t *testing.T) {
		repository := factory(t)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := repository.Load(ctx, agent.SessionKey{TenantID: "tenant", SessionID: "session"})
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context cancellation, got %v", err)
		}
		_, err = repository.Load(context.Background(), agent.SessionKey{TenantID: "tenant", SessionID: "missing"})
		if !errors.Is(err, agent.ErrNotFound) {
			t.Fatalf("expected not found, got %v", err)
		}
	})
}

func createSession(t *testing.T, ctx context.Context, repository agent.SessionRepository) agent.State {
	t.Helper()
	base := agent.NewState()
	next, events := decide(t, base, agent.CreateSession{Meta: meta("create", agent.ActorClient), TenantID: "tenant", SessionID: "session", DriverComponentID: "driver", AgentSpecRevision: "sha256:spec"})
	return commit(t, ctx, repository, base, next, events, nil).Record.State
}

func submit(commandID, inputID, turnID string) agent.SubmitInput {
	return agent.SubmitInput{Meta: meta(commandID, agent.ActorClient), InputID: inputID, TurnID: turnID, Content: json.RawMessage(`{"text":"hello"}`)}
}

func meta(id string, actor agent.ActorKind) agent.CommandMeta {
	return agent.CommandMeta{ID: id, Actor: actor}
}

func decide(t *testing.T, state agent.State, command agent.Command) (agent.State, []agent.Envelope) {
	t.Helper()
	events, err := agent.Decide(state, command)
	if err != nil {
		t.Fatalf("decide %T: %v", command, err)
	}
	next, err := agent.ApplyAll(state, events)
	if err != nil {
		t.Fatalf("apply %T: %v", command, err)
	}
	return next, events
}

func commit(t *testing.T, ctx context.Context, repository agent.SessionRepository, before, after agent.State, events []agent.Envelope, outbox []agent.OutboxMessage) agent.CommitResult {
	t.Helper()
	result, err := repository.Commit(ctx, makeCommit(before, after, events, outbox))
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	return result
}

func makeCommit(before, after agent.State, events []agent.Envelope, outbox []agent.OutboxMessage) agent.Commit {
	commandID := eventsCommandID(events)
	digest := eventsDigest(events)
	if len(events) == 0 {
		commandID = "noop"
		digest = "noop-digest"
	}
	return agent.Commit{
		Key:             agent.SessionKey{TenantID: after.TenantID, SessionID: after.SessionID},
		CommandID:       commandID,
		CommandDigest:   digest,
		ExpectedVersion: before.Version,
		Events:          events,
		Projection:      after,
		Outbox:          outbox,
	}
}

func eventsCommandID(events []agent.Envelope) string {
	if len(events) == 0 {
		return ""
	}
	return events[0].CommandID
}

func eventsDigest(events []agent.Envelope) string {
	if len(events) == 0 {
		return ""
	}
	return events[0].CommandDigest
}

func envelopes(events []agent.StoredEvent) []agent.Envelope {
	result := make([]agent.Envelope, len(events))
	for i, event := range events {
		result[i] = event.Event
	}
	return result
}
