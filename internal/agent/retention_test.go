package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"
)

func TestRetainAttemptOutputsBoundsCountAndBytes(t *testing.T) {
	countBounded := make([]AttemptOutput, 0, MaxRetainedOutputsPerAttempt+1)
	for sequence := 1; sequence <= MaxRetainedOutputsPerAttempt+1; sequence++ {
		countBounded = append(countBounded, AttemptOutput{Sequence: uint64(sequence), Payload: json.RawMessage(`{"x":1}`)})
	}
	countBounded = retainAttemptOutputs(countBounded)
	if len(countBounded) != MaxRetainedOutputsPerAttempt || countBounded[0].Sequence != 2 || countBounded[len(countBounded)-1].Sequence != uint64(MaxRetainedOutputsPerAttempt+1) {
		t.Fatalf("count-bounded outputs = first=%d last=%d count=%d", countBounded[0].Sequence, countBounded[len(countBounded)-1].Sequence, len(countBounded))
	}

	payload := make(json.RawMessage, MaxOutputPayloadBytes)
	byteBounded := make([]AttemptOutput, 0, MaxRetainedOutputBytesPerAttempt/MaxOutputPayloadBytes+1)
	for sequence := 1; sequence <= cap(byteBounded); sequence++ {
		byteBounded = append(byteBounded, AttemptOutput{Sequence: uint64(sequence), Payload: payload})
	}
	byteBounded = retainAttemptOutputs(byteBounded)
	if got := outputPayloadBytes(byteBounded); got > MaxRetainedOutputBytesPerAttempt {
		t.Fatalf("retained attempt output bytes = %d", got)
	}
	if len(byteBounded) != MaxRetainedOutputBytesPerAttempt/MaxOutputPayloadBytes {
		t.Fatalf("byte-bounded output count = %d", len(byteBounded))
	}
}

func TestRetainSessionOutputsEvictsOldestAttempts(t *testing.T) {
	state := NewState()
	for turnPosition := 1; turnPosition <= 5; turnPosition++ {
		outputs := make([]AttemptOutput, 0, MaxRetainedOutputsPerAttempt)
		for sequence := 1; sequence <= MaxRetainedOutputsPerAttempt; sequence++ {
			outputs = append(outputs, AttemptOutput{Sequence: uint64(sequence), Payload: json.RawMessage(`{"x":1}`)})
		}
		turnID := string(rune('a' + turnPosition - 1))
		state.Turns[turnID] = Turn{ID: turnID, Position: uint64(turnPosition), Attempts: []Attempt{{ID: "attempt-" + turnID, Outputs: outputs}}}
	}

	retainSessionOutputs(&state)

	count := 0
	bytes := 0
	for _, turn := range state.Turns {
		for _, attempt := range turn.Attempts {
			count += len(attempt.Outputs)
			bytes += outputPayloadBytes(attempt.Outputs)
		}
	}
	if count != MaxRetainedOutputsPerSession || bytes > MaxRetainedOutputBytesPerSession {
		t.Fatalf("retained session outputs: count=%d bytes=%d", count, bytes)
	}
	if got := len(state.Turns["a"].Attempts[0].Outputs); got != 0 {
		t.Fatalf("oldest attempt retained %d outputs", got)
	}
	if got := len(state.Turns["e"].Attempts[0].Outputs); got != MaxRetainedOutputsPerAttempt {
		t.Fatalf("newest attempt retained %d outputs", got)
	}
}

func TestAppendOutputSequenceContinuesAfterProjectionEviction(t *testing.T) {
	fence := Fence{DriverInstanceID: "driver-1", LeaseID: "lease-1", Generation: 1}
	outputs := make([]AttemptOutput, 0, MaxRetainedOutputsPerAttempt)
	for sequence := 2; sequence <= MaxRetainedOutputsPerAttempt+1; sequence++ {
		outputs = append(outputs, AttemptOutput{Sequence: uint64(sequence), Type: "usage", Payload: json.RawMessage(`{"tokens":1}`), Digest: "digest"})
	}
	attempt := Attempt{ID: "attempt-1", Status: AttemptPermitted, DriverInstanceID: fence.DriverInstanceID, LeaseID: fence.LeaseID, DriverGeneration: fence.Generation, Outputs: outputs}
	turn := Turn{ID: "turn-1", Status: TurnExecuting, ActiveAttemptID: attempt.ID, Attempts: []Attempt{attempt}}
	state := NewState()
	state.TenantID = "tenant"
	state.SessionID = "session"
	state.Status = SessionOpen
	state.ActiveTurnID = turn.ID
	state.Turns[turn.ID] = turn
	state.Driver = Driver{Status: DriverReady, Fence: fence, Generation: fence.Generation}

	events, err := Decide(state, AppendAttemptOutput{
		Meta: CommandMeta{ID: "output-next", Actor: ActorDriver}, TurnID: turn.ID, AttemptID: attempt.ID, Fence: fence,
		Sequence: uint64(MaxRetainedOutputsPerAttempt + 2), Type: "usage", Payload: json.RawMessage(`{"tokens":2}`),
	})
	if err != nil || len(events) != 1 {
		t.Fatalf("append after eviction events=%+v err=%v", events, err)
	}
}

func TestMemoryRepositoryExpiresReplayCursor(t *testing.T) {
	exerciseReplayRetention(t, newMemoryRepository(4))
}

func TestSQLiteRepositoryExpiresReplayCursor(t *testing.T) {
	repository, err := openSQLiteRepository(context.Background(), filepath.Join(t.TempDir(), "sessions.db"), 4)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = repository.Close() })
	exerciseReplayRetention(t, repository)
}

func TestSQLiteMigrationBackfillsReplayRetentionBoundary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sessions.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE schema_migrations (version INTEGER PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`,
		`INSERT INTO schema_migrations(version) VALUES (1)`,
		`CREATE TABLE sessions (tenant_id TEXT NOT NULL, session_id TEXT NOT NULL, version INTEGER NOT NULL, cursor INTEGER NOT NULL, state BLOB NOT NULL, PRIMARY KEY (tenant_id, session_id))`,
		`CREATE TABLE events (tenant_id TEXT NOT NULL, session_id TEXT NOT NULL, cursor INTEGER NOT NULL, version INTEGER NOT NULL, event_id TEXT NOT NULL, command_id TEXT NOT NULL, command_digest TEXT NOT NULL, event_type TEXT NOT NULL, payload BLOB NOT NULL, PRIMARY KEY (tenant_id, session_id, cursor))`,
		`CREATE TABLE outbox (tenant_id TEXT NOT NULL, session_id TEXT NOT NULL, cursor INTEGER NOT NULL, message_id TEXT NOT NULL, topic TEXT NOT NULL, payload BLOB NOT NULL, PRIMARY KEY (tenant_id, session_id, cursor))`,
		`CREATE TABLE commands (tenant_id TEXT NOT NULL, session_id TEXT NOT NULL, command_id TEXT NOT NULL, command_digest TEXT NOT NULL, result BLOB NOT NULL, PRIMARY KEY (tenant_id, session_id, command_id))`,
	}
	for _, statement := range statements {
		if _, err := db.Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	state := NewState()
	state.TenantID, state.SessionID, state.Status = "tenant", "session", SessionOpen
	stateJSON, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO sessions(tenant_id, session_id, version, cursor, state) VALUES (?, ?, ?, ?, ?)`, "tenant", "session", 0, 5000, stateJSON); err != nil {
		t.Fatal(err)
	}
	for _, cursor := range []int{1, 1000} {
		if _, err := db.Exec(`INSERT INTO events(tenant_id, session_id, cursor, version, event_id, command_id, command_digest, event_type, payload) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, "tenant", "session", cursor, 0, cursor, cursor, cursor, "session.created", []byte(`{}`)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.Exec(`INSERT INTO outbox(tenant_id, session_id, cursor, message_id, topic, payload) VALUES (?, ?, ?, ?, ?, ?)`, "tenant", "session", 500, "old", "turn.output", []byte(`{}`)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	repository, err := OpenSQLiteRepository(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer repository.Close()
	var version, floor, eventCount, outboxCount int
	if err := repository.db.QueryRow(`SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := repository.db.QueryRow(`SELECT retained_cursor FROM sessions WHERE tenant_id = 'tenant' AND session_id = 'session'`).Scan(&floor); err != nil {
		t.Fatal(err)
	}
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM events`).Scan(&eventCount); err != nil {
		t.Fatal(err)
	}
	if err := repository.db.QueryRow(`SELECT COUNT(*) FROM outbox`).Scan(&outboxCount); err != nil {
		t.Fatal(err)
	}
	if version != sqliteSchemaVersion || floor != 5000-MaxRetainedReplayRecordsPerSession || eventCount != 1 || outboxCount != 0 {
		t.Fatalf("migration result: version=%d floor=%d events=%d outbox=%d", version, floor, eventCount, outboxCount)
	}
}

func exerciseReplayRetention(t *testing.T, repository SessionRepository) {
	t.Helper()
	ctx := context.Background()
	key := SessionKey{TenantID: "tenant", SessionID: "session"}
	state := NewState()
	state = commitRetentionCommand(t, repository, state, CreateSession{
		Meta: CommandMeta{ID: "create", Actor: ActorClient}, TenantID: key.TenantID, SessionID: key.SessionID,
		DriverComponentID: "driver", AgentSpecRevision: "sha256:spec",
	}, nil)
	for index := 1; index <= 3; index++ {
		command := SubmitInput{
			Meta:    CommandMeta{ID: "submit-" + string(rune('0'+index)), Actor: ActorClient},
			InputID: "input-" + string(rune('0'+index)), TurnID: "turn-" + string(rune('0'+index)), Content: json.RawMessage(`{"text":"hello"}`),
		}
		state = commitRetentionCommand(t, repository, state, command, []OutboxMessage{{ID: "outbox-" + string(rune('0'+index)), Topic: "input.accepted", Payload: []byte("accepted")}})
	}
	record, err := repository.Load(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if record.Cursor != 7 {
		t.Fatalf("snapshot cursor = %d", record.Cursor)
	}
	for _, read := range []func(context.Context, SessionKey, Cursor, int) error{
		func(ctx context.Context, key SessionKey, after Cursor, limit int) error {
			_, err := repository.ReadEvents(ctx, key, after, limit)
			return err
		},
		func(ctx context.Context, key SessionKey, after Cursor, limit int) error {
			_, err := repository.ReadOutbox(ctx, key, after, limit)
			return err
		},
	} {
		err := read(ctx, key, 0, 10)
		var expired *CursorExpiredError
		if !errors.As(err, &expired) || expired.RetainedAfter != 3 || expired.SnapshotCursor != record.Cursor {
			t.Fatalf("expired cursor error = %#v", err)
		}
	}
	events, err := repository.ReadEvents(ctx, key, 3, 10)
	if err != nil || len(events) != 2 || events[0].Cursor != 4 {
		t.Fatalf("retained events=%+v err=%v", events, err)
	}
	events, err = repository.ReadEvents(ctx, key, record.Cursor, 10)
	if err != nil || len(events) != 0 {
		t.Fatalf("snapshot replay=%+v err=%v", events, err)
	}
}

func commitRetentionCommand(t *testing.T, repository SessionRepository, state State, command Command, outbox []OutboxMessage) State {
	t.Helper()
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
	result, err := repository.Commit(context.Background(), Commit{
		Key: SessionKey{TenantID: next.TenantID, SessionID: next.SessionID}, CommandID: command.Metadata().ID, CommandDigest: digest,
		ExpectedVersion: state.Version, Events: events, Projection: next, Outbox: outbox,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result.Record.State
}
