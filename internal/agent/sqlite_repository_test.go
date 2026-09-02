package agent_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	. "github.com/bamanoz/tabula/internal/agent"
	"github.com/bamanoz/tabula/internal/agent/repositorytest"
	_ "modernc.org/sqlite"
)

func TestSQLiteRepositoryConformance(t *testing.T) {
	repositorytest.Run(t, func(t *testing.T) SessionRepository {
		t.Helper()
		path := filepath.Join(t.TempDir(), "sessions.db")
		repository, err := OpenSQLiteRepository(context.Background(), path)
		if err != nil {
			t.Fatalf("open sqlite repository: %v", err)
		}
		t.Cleanup(func() { _ = repository.Close() })
		return repository
	})
}

func TestSQLiteCommandDeduplicationRowsDoNotSnapshotGrowingProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	ctx := context.Background()
	repository, err := OpenSQLiteRepository(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	state := sqliteCreateSession(t, repository)
	for i := 0; i < 32; i++ {
		id := fmt.Sprintf("submit-%d", i)
		next, events := sqliteDecide(t, state, SubmitInput{
			Meta:    CommandMeta{ID: id, Actor: ActorClient},
			InputID: fmt.Sprintf("input-%d", i),
			TurnID:  fmt.Sprintf("turn-%d", i),
			Content: json.RawMessage(fmt.Sprintf(`{"text":%q}`, strings.Repeat("x", 4096))),
		})
		result, err := repository.Commit(ctx, sqliteCommit(state, next, events, nil))
		if err != nil {
			t.Fatal(err)
		}
		state = result.Record.State
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var maxResultBytes int
	if err := db.QueryRowContext(ctx, `SELECT COALESCE(MAX(length(result)), 0) FROM commands`).Scan(&maxResultBytes); err != nil {
		t.Fatal(err)
	}
	if maxResultBytes > 256 {
		t.Fatalf("largest command result = %d bytes, want compact deduplication metadata", maxResultBytes)
	}
}

func TestSQLiteRepositoryRejectsInvalidStoredCommandBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	ctx := context.Background()
	repository, err := OpenSQLiteRepository(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })

	base := NewState()
	created, createEvents := sqliteDecide(t, base, CreateSession{
		Meta:              CommandMeta{ID: "create", Actor: ActorClient},
		TenantID:          "tenant",
		SessionID:         "session",
		DriverComponentID: "driver",
		AgentSpecRevision: "sha256:spec",
	})
	request := sqliteCommit(base, created, createEvents, nil)
	if _, err := repository.Commit(ctx, request); err != nil {
		t.Fatal(err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `UPDATE commands SET result = ? WHERE tenant_id = ? AND session_id = ? AND command_id = ?`, []byte(`{"record":{}}`), "tenant", "session", "create"); err != nil {
		_ = db.Close()
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	request.ExpectedVersion = 999
	_, err = repository.Commit(ctx, request)
	if !errors.Is(err, ErrCorruptState) {
		t.Fatalf("expected corrupt command boundary, got %v", err)
	}
}

func TestSQLiteInputProcessorSurvivesReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	ctx := context.Background()
	repository, err := OpenSQLiteRepository(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	createSessionForProcessorTest(t, repository)
	processor, err := NewInputProcessor(repository)
	if err != nil {
		t.Fatal(err)
	}
	request := InputSubmitRequest{Key: SessionKey{TenantID: "tenant", SessionID: "session"}, CommandID: "command-1", InputID: "input-1", Content: json.RawMessage(`{"text":"hello"}`)}
	first, err := processor.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenSQLiteRepository(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	reopenedProcessor, err := NewInputProcessor(reopened)
	if err != nil {
		t.Fatal(err)
	}
	second, err := reopenedProcessor.Submit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if !second.Duplicate || second.TurnID != first.TurnID || second.Position != first.Position || second.SessionVersion != first.SessionVersion || second.Cursor != first.Cursor {
		t.Fatalf("reopened retry=%+v first=%+v", second, first)
	}
	third, err := reopenedProcessor.Submit(ctx, InputSubmitRequest{Key: request.Key, CommandID: "command-2", InputID: "input-2", Content: json.RawMessage(`{"text":"second"}`)})
	if err != nil || third.Position != 2 || third.SessionVersion != 3 {
		t.Fatalf("second durable input=%+v err=%v", third, err)
	}
}

func TestSQLiteRepositoryReopenPreservesProjectionEventsAndOutbox(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	ctx := context.Background()
	repository, err := OpenSQLiteRepository(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	state := sqliteCreateSession(t, repository)
	next, events := sqliteDecide(t, state, SubmitInput{Meta: CommandMeta{ID: "submit", Actor: ActorClient}, InputID: "input", TurnID: "turn", Content: json.RawMessage(`{"text":"hello"}`)})
	request := sqliteCommit(state, next, events, []OutboxMessage{{ID: "accepted", Topic: "input.accepted", Payload: []byte("payload")}})
	result, err := repository.Commit(ctx, request)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := OpenSQLiteRepository(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	loaded, err := reopened.Load(ctx, result.Record.Key)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Version != result.Record.Version || loaded.Cursor != result.Record.Cursor || loaded.State.Turns["turn"].Status != TurnQueued {
		t.Fatalf("loaded=%+v want version=%d cursor=%d", loaded, result.Record.Version, result.Record.Cursor)
	}
	eventsAfter, err := reopened.ReadEvents(ctx, result.Record.Key, 0, 10)
	if err != nil || len(eventsAfter) != 2 || eventsAfter[1].ID == "" {
		t.Fatalf("events=%+v err=%v", eventsAfter, err)
	}
	outbox, err := reopened.ReadOutbox(ctx, result.Record.Key, 0, 10)
	if err != nil || len(outbox) != 1 || string(outbox[0].Message.Payload) != "payload" {
		t.Fatalf("outbox=%+v err=%v", outbox, err)
	}
}

func TestSQLiteRepositoryRejectsUnsupportedFutureMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP); INSERT INTO schema_migrations(version) VALUES (99);`)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = OpenSQLiteRepository(context.Background(), path)
	if !errors.Is(err, ErrCorruptState) {
		t.Fatalf("expected corrupt schema error, got %v", err)
	}
}

func TestSQLiteRepositoryReportsCorruptProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	repository, err := OpenSQLiteRepository(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	key := SessionKey{TenantID: "tenant", SessionID: "session"}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions(tenant_id, session_id, version, cursor, state) VALUES (?, ?, ?, ?, ?)`, key.TenantID, key.SessionID, 1, 1, []byte("not-json")); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = repository.Load(context.Background(), key)
	if !errors.Is(err, ErrCorruptState) {
		t.Fatalf("expected corrupt projection, got %v", err)
	}
	if err := repository.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSQLiteRepositoryRollsBackCorruptCommit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	repository, err := OpenSQLiteRepository(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	state := sqliteCreateSession(t, repository)
	next, events := sqliteDecide(t, state, SubmitInput{Meta: CommandMeta{ID: "submit", Actor: ActorClient}, InputID: "input", TurnID: "turn", Content: json.RawMessage(`{"text":"hello"}`)})
	next.Archived = true
	request := sqliteCommit(state, next, events, []OutboxMessage{{ID: "must-not-commit", Topic: "input.accepted"}})
	_, err = repository.Commit(context.Background(), request)
	if !errors.Is(err, ErrCorruptState) {
		t.Fatalf("expected corrupt state, got %v", err)
	}
	loaded, err := repository.Load(context.Background(), stateKey(state))
	if err != nil || loaded.Version != state.Version || loaded.State.Archived {
		t.Fatalf("rollback leaked state: loaded=%+v err=%v", loaded, err)
	}
	storedEvents, err := repository.ReadEvents(context.Background(), stateKey(state), 0, 10)
	if err != nil || len(storedEvents) != 1 {
		t.Fatalf("rollback leaked events: %+v err=%v", storedEvents, err)
	}
	storedOutbox, err := repository.ReadOutbox(context.Background(), stateKey(state), 0, 10)
	if err != nil || len(storedOutbox) != 0 {
		t.Fatalf("rollback leaked outbox: %+v err=%v", storedOutbox, err)
	}
}

func TestSQLiteRepositorySubprocessReopen(t *testing.T) {
	if os.Getenv("TABULA_SQLITE_CHILD") == "1" {
		path := os.Getenv("TABULA_SQLITE_PATH")
		repository, err := OpenSQLiteRepository(context.Background(), path)
		if err != nil {
			os.Exit(10)
		}
		state := sqliteCreateSessionForProcess(repository)
		command := SubmitInput{Meta: CommandMeta{ID: "child-submit", Actor: ActorClient}, InputID: "child-input", TurnID: "child-turn", Content: json.RawMessage(`{"text":"child"}`)}
		events, err := Decide(state, command)
		if err != nil {
			os.Exit(11)
		}
		next, err := ApplyAll(state, events)
		if err != nil {
			os.Exit(11)
		}
		if _, err := repository.Commit(context.Background(), sqliteCommit(state, next, events, nil)); err != nil {
			os.Exit(12)
		}
		if err := repository.Close(); err != nil {
			os.Exit(13)
		}
		os.Exit(0)
	}

	path := filepath.Join(t.TempDir(), "sessions.db")
	command := exec.Command(os.Args[0], "-test.run", "^TestSQLiteRepositorySubprocessReopen$")
	command.Env = append(os.Environ(), "TABULA_SQLITE_CHILD=1", "TABULA_SQLITE_PATH="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("child failed: %v output=%s", err, output)
	}
	repository, err := OpenSQLiteRepository(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	record, err := repository.Load(context.Background(), SessionKey{TenantID: "tenant", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	if record.Version != 2 || record.State.Turns["child-turn"].Status != TurnQueued {
		t.Fatalf("child commit not durable: %+v", record)
	}
}

func createSessionForProcessorTest(t *testing.T, repository SessionRepository) {
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

func sqliteCreateSession(t *testing.T, repository SessionRepository) State {
	t.Helper()
	return sqliteCreateSessionForProcess(repository)
}

func sqliteCreateSessionForProcess(repository SessionRepository) State {
	base := NewState()
	command := CreateSession{Meta: CommandMeta{ID: "create", Actor: ActorClient}, TenantID: "tenant", SessionID: "session", DriverComponentID: "driver", AgentSpecRevision: "sha256:spec"}
	events, err := Decide(base, command)
	if err != nil {
		panic(err)
	}
	next, err := ApplyAll(base, events)
	if err != nil {
		panic(err)
	}
	if _, err := repository.Commit(context.Background(), Commit{Key: stateKey(next), CommandID: command.Meta.ID, CommandDigest: mustDigest(command), ExpectedVersion: base.Version, Events: events, Projection: next}); err != nil {
		panic(err)
	}
	return next
}

func sqliteDecide(t *testing.T, state State, command Command) (State, []Envelope) {
	t.Helper()
	events, err := Decide(state, command)
	if err != nil {
		t.Fatal(err)
	}
	next, err := ApplyAll(state, events)
	if err != nil {
		t.Fatal(err)
	}
	return next, events
}

func sqliteCommit(before, after State, events []Envelope, outbox []OutboxMessage) Commit {
	commandID := "noop"
	digest := "noop"
	if len(events) > 0 {
		commandID = events[0].CommandID
		digest = events[0].CommandDigest
	}
	return Commit{Key: stateKey(after), CommandID: commandID, CommandDigest: digest, ExpectedVersion: before.Version, Events: events, Projection: after, Outbox: outbox}
}

func stateKey(state State) SessionKey {
	return SessionKey{TenantID: state.TenantID, SessionID: state.SessionID}
}

func mustDigest(command Command) string {
	digest, err := DigestCommand(command)
	if err != nil {
		panic(err)
	}
	return digest
}
