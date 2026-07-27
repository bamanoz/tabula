package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type SessionLedgerStore interface {
	AppendLedgerEvent(session, tenantID, kind, producer string, payload map[string]any) error
}

type ToolResultSpoolStore interface {
	ToolResultSpoolDir() string
}

func (s *DiskSessionStore) AppendLedgerEvent(session, tenantID, kind, producer string, payload map[string]any) error {
	if s == nil || strings.TrimSpace(s.home) == "" {
		return nil
	}
	return appendSessionLedgerEvent(s.home, session, tenantID, kind, producer, payload)
}

func (s *DiskSessionStore) ToolResultSpoolDir() string {
	if s == nil || strings.TrimSpace(s.home) == "" {
		return ""
	}
	return filepath.Join(s.home, "run", "tool-results")
}

func appendSessionLedgerEvent(home, session, tenantID, kind, producer string, payload map[string]any) error {
	event := map[string]any{
		"type":      "ledger.event",
		"kind":      kind,
		"session":   session,
		"tenant_id": tenantID,
		"producer":  producer,
		"payload":   payload,
		"ts":        float64(time.Now().UnixNano()) / 1e9,
	}
	path := filepath.Join(home, "data", "sessions", session, "ledger.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	_, err = file.Write(append(data, '\n'))
	return err
}
