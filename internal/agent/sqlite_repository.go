package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"
)

const sqliteSchemaVersion = 2

type storedCommandResult struct {
	Version uint64 `json:"version"`
	Cursor  Cursor `json:"cursor"`
}

type SQLiteRepository struct {
	db              *sql.DB
	close           sync.Once
	replayRetention Cursor
}

func OpenSQLiteRepository(ctx context.Context, path string) (*SQLiteRepository, error) {
	return openSQLiteRepository(ctx, path, MaxRetainedReplayRecordsPerSession)
}

func openSQLiteRepository(ctx context.Context, path string, replayRetention Cursor) (*SQLiteRepository, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: sqlite path is required", ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite repository: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	repository := &SQLiteRepository{db: db, replayRetention: replayRetention}
	if err := repository.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repository.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repository, nil
}

func (r *SQLiteRepository) Close() error {
	var err error
	r.close.Do(func() { err = r.db.Close() })
	return err
}

func (r *SQLiteRepository) configure(ctx context.Context) error {
	statements := []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	}
	for _, statement := range statements {
		if _, err := r.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite with %q: %w", statement, err)
		}
	}
	return nil
}

func (r *SQLiteRepository) migrate(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin sqlite migration: %w", err)
	}
	defer rollback(tx)
	if _, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		return fmt.Errorf("create migration table: %w", err)
	}
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if version > sqliteSchemaVersion {
		return fmt.Errorf("%w: sqlite schema version %d is newer than supported version %d", ErrCorruptState, version, sqliteSchemaVersion)
	}
	if version < 1 {
		statements := []string{
			`CREATE TABLE sessions (
                tenant_id TEXT NOT NULL,
                session_id TEXT NOT NULL,
                version INTEGER NOT NULL,
                cursor INTEGER NOT NULL,
                state BLOB NOT NULL,
                PRIMARY KEY (tenant_id, session_id)
            )`,
			`CREATE TABLE events (
                tenant_id TEXT NOT NULL,
                session_id TEXT NOT NULL,
                cursor INTEGER NOT NULL,
                version INTEGER NOT NULL,
                event_id TEXT NOT NULL,
                command_id TEXT NOT NULL,
                command_digest TEXT NOT NULL,
                event_type TEXT NOT NULL,
                payload BLOB NOT NULL,
                PRIMARY KEY (tenant_id, session_id, cursor),
                UNIQUE (event_id),
                FOREIGN KEY (tenant_id, session_id) REFERENCES sessions(tenant_id, session_id)
            )`,
			`CREATE TABLE outbox (
                tenant_id TEXT NOT NULL,
                session_id TEXT NOT NULL,
                cursor INTEGER NOT NULL,
                message_id TEXT NOT NULL,
                topic TEXT NOT NULL,
                payload BLOB NOT NULL,
                PRIMARY KEY (tenant_id, session_id, cursor),
                UNIQUE (message_id),
                FOREIGN KEY (tenant_id, session_id) REFERENCES sessions(tenant_id, session_id)
            )`,
			`CREATE TABLE commands (
                tenant_id TEXT NOT NULL,
                session_id TEXT NOT NULL,
                command_id TEXT NOT NULL,
                command_digest TEXT NOT NULL,
                result BLOB NOT NULL,
                PRIMARY KEY (tenant_id, session_id, command_id),
                FOREIGN KEY (tenant_id, session_id) REFERENCES sessions(tenant_id, session_id)
            )`,
			`CREATE INDEX events_by_session_cursor ON events(tenant_id, session_id, cursor)`,
			`CREATE INDEX outbox_by_session_cursor ON outbox(tenant_id, session_id, cursor)`,
			`INSERT INTO schema_migrations(version) VALUES (1)`,
		}
		for _, statement := range statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply sqlite migration 1: %w", err)
			}
		}
	}
	if version < 2 {
		statements := []string{
			`ALTER TABLE sessions ADD COLUMN retained_cursor INTEGER NOT NULL DEFAULT 0`,
			fmt.Sprintf(`UPDATE sessions SET retained_cursor = CASE WHEN cursor > %d THEN cursor - %d ELSE 0 END`, MaxRetainedReplayRecordsPerSession, MaxRetainedReplayRecordsPerSession),
			`DELETE FROM events WHERE cursor <= (SELECT retained_cursor FROM sessions WHERE sessions.tenant_id = events.tenant_id AND sessions.session_id = events.session_id)`,
			`DELETE FROM outbox WHERE cursor <= (SELECT retained_cursor FROM sessions WHERE sessions.tenant_id = outbox.tenant_id AND sessions.session_id = outbox.session_id)`,
			`INSERT INTO schema_migrations(version) VALUES (2)`,
		}
		for _, statement := range statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply sqlite migration 2: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit sqlite migration: %w", err)
	}
	return nil
}

func (r *SQLiteRepository) Load(ctx context.Context, key SessionKey) (Record, error) {
	if err := key.Validate(); err != nil {
		return Record{}, err
	}
	var version, cursor uint64
	var stateJSON []byte
	err := r.db.QueryRowContext(ctx, `SELECT version, cursor, state FROM sessions WHERE tenant_id = ? AND session_id = ?`, key.TenantID, key.SessionID).Scan(&version, &cursor, &stateJSON)
	if errors.Is(err, sql.ErrNoRows) {
		return Record{}, fmt.Errorf("%w: session %s/%s", ErrNotFound, key.TenantID, key.SessionID)
	}
	if err != nil {
		return Record{}, fmt.Errorf("load sqlite session: %w", err)
	}
	state, err := decodeState(stateJSON)
	if err != nil {
		return Record{}, fmt.Errorf("%w: decode session %s/%s: %v", ErrCorruptState, key.TenantID, key.SessionID, err)
	}
	if state.Version != version {
		return Record{}, fmt.Errorf("%w: session version %d does not match projection version %d", ErrCorruptState, version, state.Version)
	}
	return Record{Key: key, Version: version, Cursor: Cursor(cursor), State: state}, nil
}

func (r *SQLiteRepository) Commit(ctx context.Context, commit Commit) (CommitResult, error) {
	if err := validateCommitIdentity(commit); err != nil {
		return CommitResult{}, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return CommitResult{}, fmt.Errorf("begin sqlite commit: %w", err)
	}
	defer rollback(tx)

	var existingDigest string
	var existingResult []byte
	err = tx.QueryRowContext(ctx, `SELECT command_digest, result FROM commands WHERE tenant_id = ? AND session_id = ? AND command_id = ?`, commit.Key.TenantID, commit.Key.SessionID, commit.CommandID).Scan(&existingDigest, &existingResult)
	if err == nil {
		if existingDigest != commit.CommandDigest {
			return CommitResult{}, fmt.Errorf("%w: command id %q was already used", ErrCommandConflict, commit.CommandID)
		}
		stored, decodeErr := decodeStoredCommandResult(existingResult)
		if decodeErr != nil {
			return CommitResult{}, fmt.Errorf("%w: decode stored command result: %v", ErrCorruptState, decodeErr)
		}
		var version, cursor uint64
		var stateJSON []byte
		if err := tx.QueryRowContext(ctx, `SELECT version, cursor, state FROM sessions WHERE tenant_id = ? AND session_id = ?`, commit.Key.TenantID, commit.Key.SessionID).Scan(&version, &cursor, &stateJSON); err != nil {
			return CommitResult{}, fmt.Errorf("load sqlite duplicate projection: %w", err)
		}
		state, decodeErr := decodeState(stateJSON)
		if decodeErr != nil {
			return CommitResult{}, fmt.Errorf("%w: decode duplicate projection: %v", ErrCorruptState, decodeErr)
		}
		return CommitResult{
			Record:         Record{Key: commit.Key, Version: version, Cursor: Cursor(cursor), State: state},
			CommandVersion: stored.Version,
			CommandCursor:  stored.Cursor,
			Duplicate:      true,
		}, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return CommitResult{}, fmt.Errorf("look up sqlite command: %w", err)
	}
	if err := validateCommitBody(commit); err != nil {
		return CommitResult{}, err
	}

	var currentVersion, currentCursor, currentRetainedCursor uint64
	var currentStateJSON []byte
	err = tx.QueryRowContext(ctx, `SELECT version, cursor, retained_cursor, state FROM sessions WHERE tenant_id = ? AND session_id = ?`, commit.Key.TenantID, commit.Key.SessionID).Scan(&currentVersion, &currentCursor, &currentRetainedCursor, &currentStateJSON)
	exists := err == nil
	if !exists && !errors.Is(err, sql.ErrNoRows) {
		return CommitResult{}, fmt.Errorf("load sqlite commit projection: %w", err)
	}
	if exists {
		currentState, decodeErr := decodeState(currentStateJSON)
		if decodeErr != nil {
			return CommitResult{}, fmt.Errorf("%w: decode current projection: %v", ErrCorruptState, decodeErr)
		}
		if currentState.Version != currentVersion {
			return CommitResult{}, fmt.Errorf("%w: current projection version mismatch", ErrCorruptState)
		}
		if currentState.Status == SessionClosed {
			return CommitResult{}, fmt.Errorf("%w: session %s/%s", ErrSessionClosed, commit.Key.TenantID, commit.Key.SessionID)
		}
	}
	if currentVersion != commit.ExpectedVersion {
		return CommitResult{}, fmt.Errorf("%w: expected %d, current %d", ErrVersionConflict, commit.ExpectedVersion, currentVersion)
	}
	if commit.Projection.TenantID != commit.Key.TenantID || commit.Projection.SessionID != commit.Key.SessionID {
		return CommitResult{}, fmt.Errorf("%w: projection identity does not match key", ErrInvalidArgument)
	}
	if commit.Projection.Version != currentVersion+uint64(len(commit.Events)) {
		return CommitResult{}, fmt.Errorf("%w: projection version %d does not match event count", ErrInvalidArgument, commit.Projection.Version)
	}
	if err := Validate(commit.Projection); err != nil {
		return CommitResult{}, fmt.Errorf("%w: projection: %v", ErrCorruptState, err)
	}
	base := NewState()
	if exists {
		base, err = decodeState(currentStateJSON)
		if err != nil {
			return CommitResult{}, fmt.Errorf("%w: decode replay base: %v", ErrCorruptState, err)
		}
	}
	replayed, err := ApplyAll(base, commit.Events)
	if err != nil || !statesEqual(replayed, commit.Projection) {
		return CommitResult{}, fmt.Errorf("%w: projection does not match event replay", ErrCorruptState)
	}

	cursor := Cursor(currentCursor)
	stateJSON, err := json.Marshal(commit.Projection)
	if err != nil {
		return CommitResult{}, fmt.Errorf("encode sqlite projection: %w", err)
	}
	if !exists {
		baseJSON, marshalErr := json.Marshal(NewState())
		if marshalErr != nil {
			return CommitResult{}, fmt.Errorf("encode sqlite projection placeholder: %w", marshalErr)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(tenant_id, session_id, version, cursor, state) VALUES (?, ?, ?, ?, ?)`, commit.Key.TenantID, commit.Key.SessionID, 0, currentCursor, baseJSON); err != nil {
			return CommitResult{}, fmt.Errorf("insert sqlite projection placeholder: %w", err)
		}
		exists = true
	}
	storedOutbox := make([]StoredOutboxMessage, 0, len(commit.Outbox))
	for index, event := range commit.Events {
		cursor++
		encoded, eventType, encodeErr := encodeEvent(event.Body)
		if encodeErr != nil {
			return CommitResult{}, fmt.Errorf("encode sqlite event: %w", encodeErr)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO events(tenant_id, session_id, cursor, version, event_id, command_id, command_digest, event_type, payload) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`, commit.Key.TenantID, commit.Key.SessionID, cursor, currentVersion+uint64(index)+1, eventID(commit.Key, cursor), commit.CommandID, commit.CommandDigest, eventType, encoded); err != nil {
			return CommitResult{}, fmt.Errorf("append sqlite event: %w", err)
		}
	}
	for _, message := range commit.Outbox {
		cursor++
		stored := StoredOutboxMessage{Cursor: cursor, Message: cloneOutbox(message)}
		storedOutbox = append(storedOutbox, stored)
		payload := message.Payload
		if payload == nil {
			payload = []byte{}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO outbox(tenant_id, session_id, cursor, message_id, topic, payload) VALUES (?, ?, ?, ?, ?, ?)`, commit.Key.TenantID, commit.Key.SessionID, cursor, message.ID, message.Topic, payload); err != nil {
			return CommitResult{}, fmt.Errorf("append sqlite outbox: %w", err)
		}
	}
	floor := retainedCursor(cursor, r.replayRetention)
	if floor < Cursor(currentRetainedCursor) {
		floor = Cursor(currentRetainedCursor)
	}
	if floor > 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM events WHERE tenant_id = ? AND session_id = ? AND cursor <= ?`, commit.Key.TenantID, commit.Key.SessionID, floor); err != nil {
			return CommitResult{}, fmt.Errorf("retain sqlite events: %w", err)
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM outbox WHERE tenant_id = ? AND session_id = ? AND cursor <= ?`, commit.Key.TenantID, commit.Key.SessionID, floor); err != nil {
			return CommitResult{}, fmt.Errorf("retain sqlite outbox: %w", err)
		}
	}
	if exists {
		if _, err := tx.ExecContext(ctx, `UPDATE sessions SET version = ?, cursor = ?, retained_cursor = ?, state = ? WHERE tenant_id = ? AND session_id = ?`, commit.Projection.Version, cursor, floor, stateJSON, commit.Key.TenantID, commit.Key.SessionID); err != nil {
			return CommitResult{}, fmt.Errorf("update sqlite projection: %w", err)
		}
	} else {
		if _, err := tx.ExecContext(ctx, `INSERT INTO sessions(tenant_id, session_id, version, cursor, state) VALUES (?, ?, ?, ?, ?)`, commit.Key.TenantID, commit.Key.SessionID, commit.Projection.Version, cursor, stateJSON); err != nil {
			return CommitResult{}, fmt.Errorf("insert sqlite projection: %w", err)
		}
	}
	result := CommitResult{
		Record:         Record{Key: commit.Key, Version: commit.Projection.Version, Cursor: cursor, State: cloneState(commit.Projection)},
		CommandVersion: commit.Projection.Version,
		CommandCursor:  cursor,
		Outbox:         storedOutbox,
	}
	resultJSON, err := json.Marshal(storedCommandResult{Version: result.CommandVersion, Cursor: result.CommandCursor})
	if err != nil {
		return CommitResult{}, fmt.Errorf("encode sqlite command result: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO commands(tenant_id, session_id, command_id, command_digest, result) VALUES (?, ?, ?, ?, ?)`, commit.Key.TenantID, commit.Key.SessionID, commit.CommandID, commit.CommandDigest, resultJSON); err != nil {
		return CommitResult{}, fmt.Errorf("store sqlite command: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return CommitResult{}, fmt.Errorf("commit sqlite transaction: %w", err)
	}
	return result, nil
}

func (r *SQLiteRepository) ReadEvents(ctx context.Context, key SessionKey, after Cursor, limit int) ([]StoredEvent, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, fmt.Errorf("%w: limit must be positive", ErrInvalidArgument)
	}
	snapshotCursor, floor, err := r.replayBoundary(ctx, key)
	if err != nil {
		return nil, err
	}
	if after < floor {
		return nil, &CursorExpiredError{After: after, RetainedAfter: floor, SnapshotCursor: snapshotCursor}
	}
	rows, err := r.db.QueryContext(ctx, `SELECT cursor, version, event_id, command_id, command_digest, event_type, payload FROM events WHERE tenant_id = ? AND session_id = ? AND cursor > ? ORDER BY cursor LIMIT ?`, key.TenantID, key.SessionID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("read sqlite events: %w", err)
	}
	defer rows.Close()
	result := make([]StoredEvent, 0, limit)
	for rows.Next() {
		var cursor, version uint64
		var id, commandID, digest, eventType string
		var payload []byte
		if err := rows.Scan(&cursor, &version, &id, &commandID, &digest, &eventType, &payload); err != nil {
			return nil, fmt.Errorf("scan sqlite event: %w", err)
		}
		event, err := decodeEvent(eventType, payload)
		if err != nil {
			return nil, fmt.Errorf("%w: decode event %s: %v", ErrCorruptState, id, err)
		}
		result = append(result, StoredEvent{ID: id, Cursor: Cursor(cursor), Version: version, Event: Envelope{CommandID: commandID, CommandDigest: digest, Body: event}})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite events: %w", err)
	}
	return result, nil
}

func (r *SQLiteRepository) ReadOutbox(ctx context.Context, key SessionKey, after Cursor, limit int) ([]StoredOutboxMessage, error) {
	if err := key.Validate(); err != nil {
		return nil, err
	}
	if limit <= 0 {
		return nil, fmt.Errorf("%w: limit must be positive", ErrInvalidArgument)
	}
	snapshotCursor, floor, err := r.replayBoundary(ctx, key)
	if err != nil {
		return nil, err
	}
	if after < floor {
		return nil, &CursorExpiredError{After: after, RetainedAfter: floor, SnapshotCursor: snapshotCursor}
	}
	rows, err := r.db.QueryContext(ctx, `SELECT cursor, message_id, topic, payload FROM outbox WHERE tenant_id = ? AND session_id = ? AND cursor > ? ORDER BY cursor LIMIT ?`, key.TenantID, key.SessionID, after, limit)
	if err != nil {
		return nil, fmt.Errorf("read sqlite outbox: %w", err)
	}
	defer rows.Close()
	result := make([]StoredOutboxMessage, 0, limit)
	for rows.Next() {
		var cursor uint64
		var messageID, topic string
		var payload []byte
		if err := rows.Scan(&cursor, &messageID, &topic, &payload); err != nil {
			return nil, fmt.Errorf("scan sqlite outbox: %w", err)
		}
		result = append(result, StoredOutboxMessage{Cursor: Cursor(cursor), Message: OutboxMessage{ID: messageID, Topic: topic, Payload: append([]byte(nil), payload...)}})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate sqlite outbox: %w", err)
	}
	return result, nil
}

func (r *SQLiteRepository) List(ctx context.Context, query SessionQuery) (SessionPage, error) {
	if query.TenantID == "" {
		return SessionPage{}, fmt.Errorf("%w: tenant id is required", ErrInvalidArgument)
	}
	if query.Limit <= 0 {
		return SessionPage{}, fmt.Errorf("%w: limit must be positive", ErrInvalidArgument)
	}
	rows, err := r.db.QueryContext(ctx, `SELECT session_id, version, cursor, state FROM sessions WHERE tenant_id = ? AND session_id > ? ORDER BY session_id`, query.TenantID, query.AfterSessionID)
	if err != nil {
		return SessionPage{}, fmt.Errorf("list sqlite sessions: %w", err)
	}
	defer rows.Close()
	result := make([]Record, 0, query.Limit)
	for rows.Next() {
		var sessionID string
		var version, cursor uint64
		var stateJSON []byte
		if err := rows.Scan(&sessionID, &version, &cursor, &stateJSON); err != nil {
			return SessionPage{}, fmt.Errorf("scan sqlite session list: %w", err)
		}
		state, err := decodeState(stateJSON)
		if err != nil {
			return SessionPage{}, fmt.Errorf("%w: decode listed session %s: %v", ErrCorruptState, sessionID, err)
		}
		if !query.IncludeArchived && state.Archived {
			continue
		}
		if len(query.Statuses) > 0 && !containsStatus(query.Statuses, state.Status) {
			continue
		}
		result = append(result, Record{Key: SessionKey{TenantID: query.TenantID, SessionID: sessionID}, Version: version, Cursor: Cursor(cursor), State: state})
		if len(result) == query.Limit {
			break
		}
	}
	if err := rows.Err(); err != nil {
		return SessionPage{}, fmt.Errorf("iterate sqlite session list: %w", err)
	}
	page := SessionPage{Records: result}
	if len(result) == query.Limit {
		page.NextAfterSessionID = result[len(result)-1].Key.SessionID
	}
	return page, nil
}

func (r *SQLiteRepository) replayBoundary(ctx context.Context, key SessionKey) (Cursor, Cursor, error) {
	var cursor, floor uint64
	err := r.db.QueryRowContext(ctx, `SELECT cursor, retained_cursor FROM sessions WHERE tenant_id = ? AND session_id = ?`, key.TenantID, key.SessionID).Scan(&cursor, &floor)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, fmt.Errorf("%w: session %s/%s", ErrNotFound, key.TenantID, key.SessionID)
	}
	if err != nil {
		return 0, 0, fmt.Errorf("read sqlite replay boundary: %w", err)
	}
	return Cursor(cursor), Cursor(floor), nil
}

func rollback(tx *sql.Tx) {
	_ = tx.Rollback()
}

func containsStatus(statuses []SessionStatus, wanted SessionStatus) bool {
	for _, status := range statuses {
		if status == wanted {
			return true
		}
	}
	return false
}

func statesEqual(left, right State) bool {
	return jsonEqual(left, right)
}

func jsonEqual(left, right any) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && string(leftJSON) == string(rightJSON)
}

func encodeEvent(event Event) ([]byte, string, error) {
	var eventType string
	switch event.(type) {
	case SessionCreated:
		eventType = "session.created"
	case SessionArchived:
		eventType = "session.archived"
	case SessionUnarchived:
		eventType = "session.unarchived"
	case SessionSuspendedEvent:
		eventType = "session.suspended"
	case SessionResumedEvent:
		eventType = "session.resumed"
	case SessionDeletedEvent:
		eventType = "session.deleted"
	case InputSubmitted:
		eventType = "input.submitted"
	case DriverLeaseGranted:
		eventType = "driver.lease_granted"
	case DriverBecameReady:
		eventType = "driver.ready"
	case DriverLeaseRenewed:
		eventType = "driver.lease_renewed"
	case DriverBecameSuspect:
		eventType = "driver.suspect"
	case DriverLeaseReleased:
		eventType = "driver.lease_released"
	case DriverLeaseExpired:
		eventType = "driver.lease_expired"
	case AttemptAssignedEvent:
		eventType = "attempt.assigned"
	case AttemptPreparedContextSet:
		eventType = "attempt.prepared_context_set"
	case AttemptPreparedEvent:
		eventType = "attempt.prepared"
	case AttemptPermittedEvent:
		eventType = "attempt.permitted"
	case AttemptOutputAppended:
		eventType = "attempt.output_appended"
	case AttemptCompletedEvent:
		eventType = "attempt.completed"
	case AttemptFailedEvent:
		eventType = "attempt.failed"
	case AttemptAbandonedEvent:
		eventType = "attempt.abandoned"
	case AttemptUncertainEvent:
		eventType = "attempt.uncertain"
	case TurnCancellationRequested:
		eventType = "turn.cancellation_requested"
	case AttemptCancelledEvent:
		eventType = "attempt.cancelled"
	case AttemptResumedEvent:
		eventType = "attempt.resumed"
	case AttemptSupersededEvent:
		eventType = "attempt.superseded"
	case TurnDiscardedEvent:
		eventType = "turn.discarded"
	default:
		return nil, "", fmt.Errorf("%w: unsupported event %T", ErrInvalidArgument, event)
	}
	payload, err := json.Marshal(event)
	return payload, eventType, err
}

func decodeEvent(eventType string, payload []byte) (Event, error) {
	var event Event
	switch eventType {
	case "session.created":
		event = &SessionCreated{}
	case "session.archived":
		event = &SessionArchived{}
	case "session.unarchived":
		event = &SessionUnarchived{}
	case "session.suspended":
		event = &SessionSuspendedEvent{}
	case "session.resumed":
		event = &SessionResumedEvent{}
	case "session.deleted":
		event = &SessionDeletedEvent{}
	case "input.submitted":
		event = &InputSubmitted{}
	case "driver.lease_granted":
		event = &DriverLeaseGranted{}
	case "driver.ready":
		event = &DriverBecameReady{}
	case "driver.lease_renewed":
		event = &DriverLeaseRenewed{}
	case "driver.suspect":
		event = &DriverBecameSuspect{}
	case "driver.lease_released":
		event = &DriverLeaseReleased{}
	case "driver.lease_expired":
		event = &DriverLeaseExpired{}
	case "attempt.assigned":
		event = &AttemptAssignedEvent{}
	case "attempt.prepared_context_set":
		event = &AttemptPreparedContextSet{}
	case "attempt.prepared":
		event = &AttemptPreparedEvent{}
	case "attempt.permitted":
		event = &AttemptPermittedEvent{}
	case "attempt.output_appended":
		event = &AttemptOutputAppended{}
	case "attempt.completed":
		event = &AttemptCompletedEvent{}
	case "attempt.failed":
		event = &AttemptFailedEvent{}
	case "attempt.abandoned":
		event = &AttemptAbandonedEvent{}
	case "attempt.uncertain":
		event = &AttemptUncertainEvent{}
	case "turn.cancellation_requested":
		event = &TurnCancellationRequested{}
	case "attempt.cancelled":
		event = &AttemptCancelledEvent{}
	case "attempt.resumed":
		event = &AttemptResumedEvent{}
	case "attempt.superseded":
		event = &AttemptSupersededEvent{}
	case "turn.discarded":
		event = &TurnDiscardedEvent{}
	default:
		return nil, fmt.Errorf("unknown event type %q", eventType)
	}
	if err := json.Unmarshal(payload, event); err != nil {
		return nil, err
	}
	return dereferenceEvent(event), nil
}

func dereferenceEvent(event Event) Event {
	switch value := event.(type) {
	case *SessionCreated:
		return *value
	case *SessionArchived:
		return *value
	case *SessionUnarchived:
		return *value
	case *SessionSuspendedEvent:
		return *value
	case *SessionResumedEvent:
		return *value
	case *SessionDeletedEvent:
		return *value
	case *InputSubmitted:
		return *value
	case *DriverLeaseGranted:
		return *value
	case *DriverBecameReady:
		return *value
	case *DriverLeaseRenewed:
		return *value
	case *DriverBecameSuspect:
		return *value
	case *DriverLeaseReleased:
		return *value
	case *DriverLeaseExpired:
		return *value
	case *AttemptAssignedEvent:
		return *value
	case *AttemptPreparedContextSet:
		return *value
	case *AttemptPreparedEvent:
		return *value
	case *AttemptPermittedEvent:
		return *value
	case *AttemptOutputAppended:
		return *value
	case *AttemptCompletedEvent:
		return *value
	case *AttemptFailedEvent:
		return *value
	case *AttemptAbandonedEvent:
		return *value
	case *AttemptUncertainEvent:
		return *value
	case *TurnCancellationRequested:
		return *value
	case *AttemptCancelledEvent:
		return *value
	case *AttemptResumedEvent:
		return *value
	case *AttemptSupersededEvent:
		return *value
	case *TurnDiscardedEvent:
		return *value
	default:
		return event
	}
}

func decodeState(data []byte) (State, error) {
	state := NewState()
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, err
	}
	if state.Inputs == nil {
		state.Inputs = make(map[string]Input)
	}
	if state.Turns == nil {
		state.Turns = make(map[string]Turn)
	}
	if state.AppliedCommands == nil {
		state.AppliedCommands = make(map[string]string)
	}
	return state, Validate(state)
}

func decodeStoredCommandResult(data []byte) (storedCommandResult, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return storedCommandResult{}, err
	}
	if len(fields) != 2 || fields["version"] == nil || fields["cursor"] == nil {
		return storedCommandResult{}, fmt.Errorf("stored command result has invalid shape")
	}
	var stored storedCommandResult
	if err := json.Unmarshal(data, &stored); err != nil {
		return storedCommandResult{}, err
	}
	if stored.Version == 0 || stored.Cursor == 0 {
		return storedCommandResult{}, fmt.Errorf("stored command result has invalid boundary")
	}
	return stored, nil
}

var _ SessionRepository = (*SQLiteRepository)(nil)
