package kernel

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/bamanoz/tabula/internal/sessionrecord"
)

type memorySessionRecordStore struct {
	mu      sync.Mutex
	records []sessionrecord.StoredRecord
}

func (s *memorySessionRecordStore) Append(_ context.Context, record sessionrecord.Record) (sessionrecord.StoredRecord, error) {
	if err := record.Validate(); err != nil {
		return sessionrecord.StoredRecord{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	stored := sessionrecord.StoredRecord{
		ID: int64(len(s.records) + 1), TenantID: record.TenantID, SessionID: record.SessionID,
		Kind: record.Kind, Producer: record.Producer, Payload: append([]byte(nil), record.Payload...), CreatedAt: time.Now().UTC(),
	}
	s.records = append(s.records, stored)
	return stored, nil
}

func (s *memorySessionRecordStore) Read(_ context.Context, query sessionrecord.Query) ([]sessionrecord.StoredRecord, error) {
	if err := query.Validate(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]sessionrecord.StoredRecord, 0, query.Limit)
	for _, record := range s.records {
		if record.TenantID != query.TenantID || record.SessionID != query.SessionID || query.Kind != "" && record.Kind != query.Kind {
			continue
		}
		if query.AfterID != 0 && record.ID <= query.AfterID || query.BeforeID != 0 && record.ID >= query.BeforeID {
			continue
		}
		out = append(out, record)
	}
	if query.AfterID == 0 && len(out) > query.Limit {
		out = out[len(out)-query.Limit:]
	} else if len(out) > query.Limit {
		out = out[:query.Limit]
	}
	return append([]sessionrecord.StoredRecord(nil), out...), nil
}

func (s *memorySessionRecordStore) snapshot(kind string) []sessionrecord.StoredRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]sessionrecord.StoredRecord, 0, len(s.records))
	for _, record := range s.records {
		if kind == "" || record.Kind == kind {
			out = append(out, record)
		}
	}
	return out
}

func sessionRecordText(t *testing.T, store *memorySessionRecordStore, kind string) string {
	t.Helper()
	var text strings.Builder
	for _, record := range store.snapshot(kind) {
		text.Write(record.Payload)
		text.WriteByte('\n')
	}
	return text.String()
}
