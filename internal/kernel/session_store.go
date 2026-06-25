package kernel

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"

	"github.com/bamanoz/tabula/internal/tenant"
)

type SessionStore interface {
	Save(*Session) error
	Load(sessionID, tenantID string) (*sessionFile, error)
	Delete(sessionID, tenantID string) error
}

type DiskSessionStore struct {
	home string
}

type sessionFile struct {
	ID                  string       `json:"id"`
	TenantID            string       `json:"tenant_id"`
	State               SessionState `json:"state"`
	CreatedAt           string       `json:"created_at"`
	LastActiveAt        string       `json:"last_active_at"`
	Busy                bool         `json:"busy"`
	CancelRequested     bool         `json:"cancel_requested"`
	PendingInputs       int          `json:"pending_inputs"`
	ClientCount         int          `json:"client_count"`
	ActiveToolCalls     int          `json:"active_tool_calls"`
	RestartObservations int          `json:"restart_observations"`
	StuckSuspended      bool         `json:"stuck_suspended"`
}

const stuckSessionRestartThreshold = 3

func NewDiskSessionStore(tabulaHome string) *DiskSessionStore {
	return &DiskSessionStore{home: filepath.Clean(tabulaHome)}
}

func (s *DiskSessionStore) Save(sess *Session) error {
	if s == nil || sess == nil {
		return nil
	}
	sess.mu.RLock()
	record := sessionFile{
		ID:                  sess.ID,
		TenantID:            sess.TenantID,
		State:               sess.State,
		CreatedAt:           formatSnapshotTime(sess.CreatedAt),
		LastActiveAt:        formatSnapshotTime(sess.LastActiveAt),
		Busy:                sess.inflightTurn,
		CancelRequested:     sess.cancelRequested,
		PendingInputs:       len(sess.pendingInputs),
		ClientCount:         len(sess.clients),
		ActiveToolCalls:     sess.activeToolCalls,
		RestartObservations: sess.restartObservations,
		StuckSuspended:      sess.stuckSuspended,
	}
	sess.mu.RUnlock()
	if record.TenantID == "" {
		record.TenantID = tenant.DefaultID
	}
	path := s.path(record.TenantID, record.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func (s *DiskSessionStore) Load(sessionID, tenantID string) (*sessionFile, error) {
	if s == nil {
		return nil, nil
	}
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	data, err := os.ReadFile(s.path(tenantID, sessionID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var record sessionFile
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *DiskSessionStore) Delete(sessionID, tenantID string) error {
	if s == nil {
		return nil
	}
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	if err := os.Remove(s.path(tenantID, sessionID)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func (s *DiskSessionStore) path(tenantID, sessionID string) string {
	return filepath.Join(s.home, "tenants", tenantID, "state", "sessions", url.PathEscape(sessionID)+".json")
}

func (h *Hub) SetSessionStore(store SessionStore) {
	h.sessionStore = store
}

func (h *Hub) persistSessionState(tenantID, session string) {
	if h == nil || h.sessionStore == nil || h.sessions == nil || session == "" {
		return
	}
	sess, ok := h.sessions.Get(session, tenantID)
	if !ok {
		return
	}
	if err := h.sessionStore.Save(sess); err != nil {
		h.Logger.Warn("persist session state failed", "session", session, "err", err)
	}
}

func (h *Hub) observePersistedSessionRestart(sess *Session) {
	if h == nil || h.sessionStore == nil || sess == nil {
		return
	}
	record, err := h.sessionStore.Load(sess.ID, sess.TenantID)
	if err != nil {
		h.Logger.Warn("load session state failed", "session", sess.ID, "tenant_id", sess.TenantID, "err", err)
		return
	}
	if record == nil {
		return
	}
	active := record.Busy || record.ActiveToolCalls > 0
	sess.observeRestart(active, record.RestartObservations, stuckSessionRestartThreshold)
}

func (h *Hub) deleteSessionState(session, tenantID string) {
	if h == nil || h.sessionStore == nil || session == "" {
		return
	}
	if err := h.sessionStore.Delete(session, tenantID); err != nil {
		h.Logger.Warn("delete session state failed", "session", session, "tenant_id", tenantID, "err", err)
	}
}
