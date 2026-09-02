package sessionrecord

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

var (
	ErrInvalidArgument = errors.New("invalid argument")
	ErrNotFound        = errors.New("not found")
)

const MaxPayloadBytes = 1 << 20

type Record struct {
	TenantID  string          `json:"tenant_id"`
	SessionID string          `json:"session_id"`
	Kind      string          `json:"kind"`
	Producer  string          `json:"producer"`
	Payload   json.RawMessage `json:"payload"`
}

type StoredRecord struct {
	ID        int64           `json:"id"`
	TenantID  string          `json:"tenant_id"`
	SessionID string          `json:"session_id"`
	Kind      string          `json:"kind"`
	Producer  string          `json:"producer"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type Query struct {
	TenantID  string
	SessionID string
	Kind      string
	AfterID   int64
	BeforeID  int64
	Limit     int
}

type Store interface {
	Append(context.Context, Record) (StoredRecord, error)
	Read(context.Context, Query) ([]StoredRecord, error)
}

func (r Record) Validate() error {
	if r.TenantID == "" || r.SessionID == "" || r.Kind == "" || r.Producer == "" {
		return fmt.Errorf("%w: tenant_id, session_id, kind, and producer are required", ErrInvalidArgument)
	}
	if len(r.Kind) > 128 || len(r.Producer) > 256 {
		return fmt.Errorf("%w: kind or producer is too long", ErrInvalidArgument)
	}
	if len(r.Payload) == 0 || len(r.Payload) > MaxPayloadBytes || !json.Valid(r.Payload) {
		return fmt.Errorf("%w: payload must be valid JSON no larger than %d bytes", ErrInvalidArgument, MaxPayloadBytes)
	}
	return nil
}

func (q Query) Validate() error {
	if q.TenantID == "" || q.SessionID == "" {
		return fmt.Errorf("%w: tenant_id and session_id are required", ErrInvalidArgument)
	}
	if q.AfterID < 0 || q.BeforeID < 0 || (q.AfterID != 0 && q.BeforeID != 0) {
		return fmt.Errorf("%w: after_id and before_id must be non-negative and mutually exclusive", ErrInvalidArgument)
	}
	if q.Limit < 1 || q.Limit > 256 {
		return fmt.Errorf("%w: limit must be between 1 and 256", ErrInvalidArgument)
	}
	return nil
}
