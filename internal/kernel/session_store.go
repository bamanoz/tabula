package kernel

import (
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/bamanoz/tabula/internal/tenant"
)

type SessionStore interface {
	Save(*Session) error
	Load(sessionID, tenantID string) (*sessionFile, error)
	Delete(sessionID, tenantID string) error
}

type ToolResultSpoolStore interface {
	ToolResultSpoolDir() string
}

type sessionListStore interface {
	LoadAll() ([]sessionFile, error)
}

type DiskSessionStore struct {
	home string
}

type sessionFile struct {
	ID                  string       `json:"id"`
	TenantID            string       `json:"tenant_id"`
	PreferredRuntimeID  string       `json:"preferred_runtime_id,omitempty"`
	State               SessionState `json:"state"`
	CreatedAt           string       `json:"created_at"`
	LastActiveAt        string       `json:"last_active_at"`
	ArchivedAt          string       `json:"archived_at,omitempty"`
	DeletedAt           string       `json:"deleted_at,omitempty"`
	ClientCount         int          `json:"client_count"`
	ActiveToolCalls     int          `json:"active_tool_calls"`
	RestartObservations int          `json:"restart_observations"`
	StuckSuspended      bool         `json:"stuck_suspended"`
}

func NewDiskSessionStore(tabulaHome string) *DiskSessionStore {
	store := &DiskSessionStore{home: filepath.Clean(tabulaHome)}
	cleanupInvokeResultSpools(store.ToolResultSpoolDir())
	return store
}

func (s *DiskSessionStore) ToolResultSpoolDir() string {
	if s == nil || strings.TrimSpace(s.home) == "" {
		return ""
	}
	return filepath.Join(s.home, "run", "tool-results")
}

func (s *DiskSessionStore) Save(sess *Session) error {
	if s == nil || sess == nil {
		return nil
	}
	sess.mu.RLock()
	record := sessionFile{
		ID:                  sess.ID,
		TenantID:            sess.TenantID,
		PreferredRuntimeID:  sess.PreferredRuntimeID,
		State:               sess.State,
		CreatedAt:           formatSnapshotTime(sess.CreatedAt),
		LastActiveAt:        formatSnapshotTime(sess.LastActiveAt),
		ArchivedAt:          formatSnapshotTime(sess.ArchivedAt),
		DeletedAt:           formatSnapshotTime(sess.DeletedAt),
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

func (s *DiskSessionStore) LoadAll() ([]sessionFile, error) {
	if s == nil {
		return nil, nil
	}
	root := filepath.Join(s.home, "tenants")
	var records []sessionFile
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || filepath.Ext(path) != ".json" || !strings.Contains(path, string(filepath.Separator)+"state"+string(filepath.Separator)+"sessions"+string(filepath.Separator)) {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var record sessionFile
		if err := json.Unmarshal(data, &record); err != nil {
			return err
		}
		if record.ID == "" {
			return nil
		}
		if record.TenantID == "" {
			record.TenantID = tenant.DefaultID
		}
		records = append(records, record)
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	return records, err
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
	h.hydratePersistedSessions(store)
}

func (h *Hub) hydratePersistedSessions(store SessionStore) {
	listStore, ok := store.(sessionListStore)
	if h == nil || h.sessions == nil || !ok {
		return
	}
	records, err := listStore.LoadAll()
	if err != nil {
		h.Logger.Warn("load persisted sessions failed", "err", err)
		return
	}
	for _, record := range records {
		if record.DeletedAt != "" {
			continue
		}
		sess, created := h.sessions.GetOrCreateStatus(record.ID, record.TenantID)
		if !created {
			continue
		}
		sess.restorePersisted(record)
	}
	if len(records) > 0 {
		h.Logger.Info("hydrated persisted sessions", "count", len(records))
	}
}

func (s *Session) restorePersisted(record sessionFile) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if record.PreferredRuntimeID != "" {
		s.PreferredRuntimeID = record.PreferredRuntimeID
	}
	if record.State != "" {
		s.State = record.State
	}
	if createdAt := parseSessionTime(record.CreatedAt); !createdAt.IsZero() {
		s.CreatedAt = createdAt
	}
	if lastActiveAt := parseSessionTime(record.LastActiveAt); !lastActiveAt.IsZero() {
		s.LastActiveAt = lastActiveAt
	}
	if archivedAt := parseSessionTime(record.ArchivedAt); !archivedAt.IsZero() {
		s.ArchivedAt = archivedAt
	}
	if deletedAt := parseSessionTime(record.DeletedAt); !deletedAt.IsZero() {
		s.DeletedAt = deletedAt
		s.State = SessionClosing
	}
	s.restartObservations = record.RestartObservations
	s.stuckSuspended = record.StuckSuspended
	// Live-only state is intentionally not restored. Clients and running tool calls
	// are re-established through joins and runtime lifecycle after kernel restart.
	s.activeToolCalls = 0
}

func parseSessionTime(value string) time.Time {
	if value == "" {
		return time.Time{}
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
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

func (h *Hub) deleteSessionState(session, tenantID string) {
	if h == nil || h.sessionStore == nil || session == "" {
		return
	}
	if err := h.sessionStore.Delete(session, tenantID); err != nil {
		h.Logger.Warn("delete session state failed", "session", session, "tenant_id", tenantID, "err", err)
	}
}
