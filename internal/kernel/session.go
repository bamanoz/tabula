package kernel

import (
	"sync"
	"time"
)

// SessionState represents the lifecycle state of a session.
type SessionState string

const (
	SessionActive  SessionState = "active"
	SessionIdle    SessionState = "idle"
	SessionClosing SessionState = "closing"
)

// Session represents an active session with its own lifecycle and metadata.
type Session struct {
	mu              sync.RWMutex
	ID              string
	State           SessionState
	CreatedAt       time.Time
	LastActiveAt    time.Time
	clients         map[string]bool // client name → true
	inflightTurn    bool
	cancelRequested bool
}

func newSession(id string) *Session {
	now := time.Now()
	return &Session{
		ID:           id,
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

func (s *Session) AddClient(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clients[name] = true
	s.State = SessionActive
	s.touchLocked()
}

func (s *Session) RemoveClient(name string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.clients, name)
	if len(s.clients) == 0 && !s.inflightTurn {
		s.State = SessionIdle
	}
	s.touchLocked()
}

func (s *Session) ClientCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.clients)
}

func (s *Session) BeginTurn() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == SessionClosing || s.inflightTurn {
		return false
	}
	s.inflightTurn = true
	s.cancelRequested = false
	s.State = SessionActive
	s.touchLocked()
	return true
}

func (s *Session) EndTurn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflightTurn = false
	s.cancelRequested = false
	if s.State != SessionClosing && len(s.clients) == 0 {
		s.State = SessionIdle
	}
	s.touchLocked()
}

func (s *Session) RequestCancel() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == SessionClosing || !s.inflightTurn || s.cancelRequested {
		return false
	}
	s.cancelRequested = true
	s.touchLocked()
	return true
}

func (s *Session) IsBusy() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.inflightTurn
}

func (s *Session) CancelRequested() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cancelRequested
}

// SessionRegistry manages session lifecycle and metadata.
type SessionRegistry struct {
	mu       sync.RWMutex
	sessions map[string]*Session
}

func NewSessionRegistry() *SessionRegistry {
	return &SessionRegistry{
		sessions: make(map[string]*Session),
	}
}

// GetOrCreate returns an existing session or creates a new one.
func (r *SessionRegistry) GetOrCreate(id string) *Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[id]; ok {
		return s
	}
	s := newSession(id)
	r.sessions[id] = s
	return s
}

// Get returns a session by ID, or nil if not found.
func (r *SessionRegistry) Get(id string) (*Session, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[id]
	return s, ok
}

// Exists checks if a session exists without creating it.
func (r *SessionRegistry) Exists(id string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.sessions[id]
	return ok
}

// Remove marks a session as closing and removes it.
func (r *SessionRegistry) Remove(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if s, ok := r.sessions[id]; ok {
		s.State = SessionClosing
		s.inflightTurn = false
		s.cancelRequested = false
		s.touchLocked()
	}
	delete(r.sessions, id)
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
