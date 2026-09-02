package sessionrecord

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

const sqliteSchemaVersion = 1

type SQLiteStore struct {
	db    *sql.DB
	close sync.Once
}

func OpenSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: sqlite path is required", ErrInvalidArgument)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite session records: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &SQLiteStore{db: db}
	if err := store.configure(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := store.migrate(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	var err error
	s.close.Do(func() { err = s.db.Close() })
	return err
}

func (s *SQLiteStore) configure(ctx context.Context) error {
	for _, statement := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA synchronous = FULL",
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
	} {
		if _, err := s.db.ExecContext(ctx, statement); err != nil {
			return fmt.Errorf("configure sqlite session records with %q: %w", statement, err)
		}
	}
	return nil
}

func (s *SQLiteStore) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin session record migration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS session_record_schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP
)`); err != nil {
		return fmt.Errorf("create session record migration table: %w", err)
	}
	var version int
	if err := tx.QueryRowContext(ctx, "SELECT COALESCE(MAX(version), 0) FROM session_record_schema_migrations").Scan(&version); err != nil {
		return fmt.Errorf("read session record schema version: %w", err)
	}
	if version > sqliteSchemaVersion {
		return fmt.Errorf("session record schema version %d is newer than supported version %d", version, sqliteSchemaVersion)
	}
	if version < 1 {
		statements := []string{
			`CREATE TABLE session_records (
                id INTEGER PRIMARY KEY AUTOINCREMENT,
                tenant_id TEXT NOT NULL,
                session_id TEXT NOT NULL,
                kind TEXT NOT NULL,
                producer TEXT NOT NULL,
                payload BLOB NOT NULL,
                created_at_ns INTEGER NOT NULL
            )`,
			`CREATE INDEX session_records_by_session_kind_id ON session_records(tenant_id, session_id, kind, id)`,
			`INSERT INTO session_record_schema_migrations(version) VALUES (1)`,
		}
		for _, statement := range statements {
			if _, err := tx.ExecContext(ctx, statement); err != nil {
				return fmt.Errorf("apply session record migration 1: %w", err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit session record migration: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Append(ctx context.Context, record Record) (StoredRecord, error) {
	if s == nil || s.db == nil {
		return StoredRecord{}, errors.New("session record store is unavailable")
	}
	if err := record.Validate(); err != nil {
		return StoredRecord{}, err
	}
	createdAt := time.Now().UTC()
	result, err := s.db.ExecContext(ctx, `
INSERT INTO session_records(tenant_id, session_id, kind, producer, payload, created_at_ns)
VALUES (?, ?, ?, ?, ?, ?)`, record.TenantID, record.SessionID, record.Kind, record.Producer, []byte(record.Payload), createdAt.UnixNano())
	if err != nil {
		return StoredRecord{}, fmt.Errorf("append session record: %w", err)
	}
	id, err := result.LastInsertId()
	if err != nil {
		return StoredRecord{}, fmt.Errorf("read appended session record id: %w", err)
	}
	return StoredRecord{ID: id, TenantID: record.TenantID, SessionID: record.SessionID, Kind: record.Kind, Producer: record.Producer, Payload: append([]byte(nil), record.Payload...), CreatedAt: createdAt}, nil
}

func (s *SQLiteStore) Read(ctx context.Context, query Query) ([]StoredRecord, error) {
	if s == nil || s.db == nil {
		return nil, errors.New("session record store is unavailable")
	}
	if err := query.Validate(); err != nil {
		return nil, err
	}
	var statement strings.Builder
	statement.WriteString("SELECT id, tenant_id, session_id, kind, producer, payload, created_at_ns FROM session_records WHERE tenant_id = ? AND session_id = ?")
	args := []any{query.TenantID, query.SessionID}
	if query.Kind != "" {
		statement.WriteString(" AND kind = ?")
		args = append(args, query.Kind)
	}
	descending := query.AfterID == 0
	if query.AfterID != 0 {
		statement.WriteString(" AND id > ?")
		args = append(args, query.AfterID)
	} else if query.BeforeID != 0 {
		statement.WriteString(" AND id < ?")
		args = append(args, query.BeforeID)
	}
	if descending {
		statement.WriteString(" ORDER BY id DESC")
	} else {
		statement.WriteString(" ORDER BY id ASC")
	}
	statement.WriteString(" LIMIT ?")
	args = append(args, query.Limit)

	rows, err := s.db.QueryContext(ctx, statement.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("read session records: %w", err)
	}
	defer rows.Close()
	records := make([]StoredRecord, 0, query.Limit)
	for rows.Next() {
		var record StoredRecord
		var payload []byte
		var createdAtNS int64
		if err := rows.Scan(&record.ID, &record.TenantID, &record.SessionID, &record.Kind, &record.Producer, &payload, &createdAtNS); err != nil {
			return nil, fmt.Errorf("scan session record: %w", err)
		}
		record.Payload = append([]byte(nil), payload...)
		record.CreatedAt = time.Unix(0, createdAtNS).UTC()
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate session records: %w", err)
	}
	if descending {
		for left, right := 0, len(records)-1; left < right; left, right = left+1, right-1 {
			records[left], records[right] = records[right], records[left]
		}
	}
	return records, nil
}
