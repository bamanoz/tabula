package kernel

import (
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/tenant"
)

// SessionState represents the lifecycle state of a session.
type SessionState string

const (
	SessionActive  SessionState = "active"
	SessionIdle    SessionState = "idle"
	SessionClosing SessionState = "closing"
	SessionStuck   SessionState = "suspended_stuck"
)

// Session represents an active session with its own lifecycle and metadata.
type Session struct {
	mu                  sync.RWMutex
	ID                  string
	TenantID            string
	InitContext         string
	PreferredRuntimeID  string
	State               SessionState
	CreatedAt           time.Time
	LastActiveAt        time.Time
	ArchivedAt          time.Time
	DeletedAt           time.Time
	clients             map[string]bool // client name → true
	activeToolCalls     int
	restartObservations int
	stuckSuspended      bool
}

func (s *Session) BeginToolCall() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeToolCalls++
	s.State = SessionActive
	s.touchLocked()
}

func (s *Session) CompleteToolCall() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeToolCalls > 0 {
		s.activeToolCalls--
	}
	s.touchLocked()
}

func (s *Session) SetInitContext(context string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.InitContext = context
	s.touchLocked()
}

func (s *Session) GetInitContext() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.InitContext
}

func (s *Session) BindPreferredRuntime(runtimeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.PreferredRuntimeID != "" || runtimeID == "" {
		return false
	}
	s.PreferredRuntimeID = runtimeID
	s.touchLocked()
	return true
}

func (s *Session) RestorePreferredRuntime(runtimeID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.PreferredRuntimeID != "" || runtimeID == "" {
		return
	}
	s.PreferredRuntimeID = runtimeID
}

func (s *Session) PreferredRuntime() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.PreferredRuntimeID
}

func (s *Session) SetPreferredRuntime(runtimeID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if runtimeID == "" || s.PreferredRuntimeID == runtimeID {
		return false
	}
	s.PreferredRuntimeID = runtimeID
	s.touchLocked()
	return true
}

func newSession(id, tenantID string) *Session {
	now := time.Now()
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	return &Session{
		ID:           id,
		TenantID:     tenantID,
		State:        SessionIdle,
		CreatedAt:    now,
		LastActiveAt: now,
		clients:      make(map[string]bool),
	}
}

func (s *Session) touchLocked() {
	s.LastActiveAt = time.Now()
}

func (s *Session) Touch() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.touchLocked()
}

func (s *Session) Archive() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ArchivedAt.IsZero() {
		s.ArchivedAt = time.Now()
	}
	s.touchLocked()
	return s.ArchivedAt
}

func (s *Session) MarkDeleted() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.DeletedAt.IsZero() {
		s.DeletedAt = time.Now()
	}
	s.State = SessionClosing
	s.touchLocked()
	return s.DeletedAt
}

func (s *Session) IsDeleted() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.DeletedAt.IsZero()
}

func (s *Session) AddClient(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[name] = true
	if s.stuckSuspended {
		s.State = SessionStuck
	} else {
		s.State = SessionActive
	}
	s.touchLocked()
}

func (s *Session) RemoveClient(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, name)
	if len(s.clients) == 0 && !s.stuckSuspended {
		s.State = SessionIdle
	}
	s.touchLocked()
}

func (s *Session) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

func (s *Session) IsStuckSuspended() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.stuckSuspended || s.State == SessionStuck
}

func (s *Session) RestartObservations() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.restartObservations
}

// SessionRegistry manages session lifecycle and metadata.
type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[sessionKey]*Session
}

type sessionKey struct {
	tenantID string
	id       string
}

func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{
		sessions: make(map[sessionKey]*Session),
	}
}

func sessionRegistryKey(id, tenantID string) sessionKey {
	if tenantID == "" {
		tenantID = tenant.DefaultID
	}
	return sessionKey{tenantID: tenantID, id: id}
}

// GetOrCreate returns an existing session or creates one bound to tenantID.
func (r *SessionRegistry) GetOrCreate(id, tenantID string) *Session {
	s, _ := r.GetOrCreateStatus(id, tenantID)
	return s
}

// GetOrCreateStatus returns an existing session or creates one bound to tenantID,
// along with a flag reporting whether it was newly created.
func (r *SessionRegistry) GetOrCreateStatus(id, tenantID string) (*Session, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := sessionRegistryKey(id, tenantID)
	if s, ok := r.sessions[key]; ok {
		return s, false
	}
	s := newSession(id, key.tenantID)
	r.sessions[key] = s
	return s, true
}

// Get returns a session by ID, or nil if not found.
func (r *SessionRegistry) Get(id, tenantID string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[sessionRegistryKey(id, tenantID)]
	return s, ok
}

// Exists checks if a session exists without creating it.
func (r *SessionRegistry) Exists(id, tenantID string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.sessions[sessionRegistryKey(id, tenantID)]
	return ok
}

// Remove marks a session as closing and removes it.
func (r *SessionRegistry) Remove(id, tenantID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := sessionRegistryKey(id, tenantID)
	if s, ok := r.sessions[key]; ok {
		s.mu.Lock()
		s.State = SessionClosing
		s.touchLocked()
		s.mu.Unlock()
	}
	delete(r.sessions, key)
}

// All returns all sessions.
func (r *SessionRegistry) All() []*Session {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]*Session, 0, len(r.sessions))
	for _, s := range r.sessions {
		out = append(out, s)
	}
	return out
}

func (r *SessionRegistry) TenantID(id string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	found := ""
	for key, sess := range r.sessions {
		if key.id != id {
			continue
		}
		if found != "" && found != sess.TenantID {
			return tenant.DefaultID
		}
		found = sess.TenantID
	}
	if found == "" {
		return tenant.DefaultID
	}
	return found
}
