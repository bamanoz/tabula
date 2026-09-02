package agent

import (
	"context"
	"errors"
	"fmt"
)

var (
	ErrNotFound        = errors.New("not found")
	ErrVersionConflict = errors.New("version conflict")
	ErrSessionClosed   = errors.New("session closed")
	ErrCursorExpired   = errors.New("cursor expired")
	ErrCorruptState    = errors.New("corrupt state")
)

type CursorExpiredError struct {
	After          Cursor
	RetainedAfter  Cursor
	SnapshotCursor Cursor
}

func (e *CursorExpiredError) Error() string {
	return fmt.Sprintf("%s: cursor %d is older than retained boundary %d", ErrCursorExpired, e.After, e.RetainedAfter)
}

func (e *CursorExpiredError) Unwrap() error {
	return ErrCursorExpired
}

type SessionKey struct {
	TenantID  string `json:"tenant_id"`
	SessionID string `json:"session_id"`
}

func (k SessionKey) Validate() error {
	if k.TenantID == "" || k.SessionID == "" {
		return fmt.Errorf("%w: tenant and session ids are required", ErrInvalidArgument)
	}
	return nil
}

type Cursor uint64

type Record struct {
	Key     SessionKey `json:"key"`
	Version uint64     `json:"version"`
	Cursor  Cursor     `json:"cursor"`
	State   State      `json:"state"`
}

type StoredEvent struct {
	ID      string   `json:"id"`
	Cursor  Cursor   `json:"cursor"`
	Version uint64   `json:"version"`
	Event   Envelope `json:"event"`
}

type OutboxMessage struct {
	ID      string `json:"id"`
	Topic   string `json:"topic"`
	Payload []byte `json:"payload"`
}

type StoredOutboxMessage struct {
	Cursor  Cursor        `json:"cursor"`
	Message OutboxMessage `json:"message"`
}

type Commit struct {
	Key             SessionKey      `json:"key"`
	CommandID       string          `json:"command_id"`
	CommandDigest   string          `json:"command_digest"`
	ExpectedVersion uint64          `json:"expected_version"`
	Events          []Envelope      `json:"events"`
	Projection      State           `json:"projection"`
	Outbox          []OutboxMessage `json:"outbox,omitempty"`
}

type CommitResult struct {
	Record         Record                `json:"record"`
	CommandVersion uint64                `json:"command_version"`
	CommandCursor  Cursor                `json:"command_cursor"`
	Duplicate      bool                  `json:"duplicate"`
	Outbox         []StoredOutboxMessage `json:"outbox,omitempty"`
}

type SessionQuery struct {
	TenantID        string          `json:"tenant_id"`
	Statuses        []SessionStatus `json:"statuses,omitempty"`
	IncludeArchived bool            `json:"include_archived,omitempty"`
	AfterSessionID  string          `json:"after_session_id,omitempty"`
	Limit           int             `json:"limit,omitempty"`
}

type SessionPage struct {
	Records            []Record `json:"records"`
	NextAfterSessionID string   `json:"next_after_session_id,omitempty"`
}

// SessionRepository persists decisions made by the aggregate. Implementations
// must not derive or reinterpret domain transitions.
//
// Commit is atomic across command deduplication, event append, projection, and
// outbox. Duplicate command lookup precedes version comparison so callers can
// recover a lost response. A duplicate with the same digest returns the current
// projection, the original command version/cursor, no historical outbox, and
// Duplicate set; a different digest returns ErrCommandConflict.
// Commits without events durably deduplicate a no-op command without advancing
// version or cursor and cannot append outbox messages.
type SessionRepository interface {
	// Load returns one projection and its event/outbox cursor from the same read
	// boundary. It returns ErrNotFound when the key has never been committed.
	Load(ctx context.Context, key SessionKey) (Record, error)

	// Commit returns ErrVersionConflict for a new command with a stale expected
	// version, ErrSessionClosed after a tombstone, ErrCommandConflict when a
	// command ID is reused with another digest, and ErrCorruptState when events
	// do not reproduce the supplied projection.
	Commit(ctx context.Context, commit Commit) (CommitResult, error)

	// ReadEvents and ReadOutbox return records strictly after the opaque cursor,
	// ordered by cursor. Limit must be positive. A missing key returns ErrNotFound.
	// A cursor older than the retained boundary returns ErrCursorExpired and requires
	// the caller to load a fresh projection and resume from its cursor.
	ReadEvents(ctx context.Context, key SessionKey, after Cursor, limit int) ([]StoredEvent, error)
	ReadOutbox(ctx context.Context, key SessionKey, after Cursor, limit int) ([]StoredOutboxMessage, error)

	// List is tenant-scoped and ordered by session ID. NextAfterSessionID is an
	// opaque continuation value for the next query. Limit must be positive.
	List(ctx context.Context, query SessionQuery) (SessionPage, error)
}
