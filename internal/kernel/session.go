package kernel

import (
	"sync"
	"time"

	"github.com/bamanoz/tabula/internal/tenant"
)

const maxPendingInputs = 100

type queuedInput struct {
	message *Message
	exclude *Client
}

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
	TenantID        string
	InitContext     string
	State           SessionState
	CreatedAt       time.Time
	LastActiveAt    time.Time
	clients         map[string]bool // client name → true
	inflightTurn    bool
	cancelRequested bool
	pendingInputs   []queuedInput
	pendingSteers   []queuedInput
	activeToolCalls int
}

func (s *Session) BeginToolCall() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.activeToolCalls++
	s.State = SessionActive
	s.touchLocked()
}

func (s *Session) CompleteToolCall() []queuedInput {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeToolCalls > 0 {
		s.activeToolCalls--
	}
	if s.activeToolCalls > 0 || len(s.pendingSteers) == 0 {
		s.touchLocked()
		return nil
	}
	steers := append([]queuedInput(nil), s.pendingSteers...)
	s.pendingSteers = nil
	s.touchLocked()
	return steers
}

func (s *Session) HasActiveToolCalls() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.activeToolCalls > 0
}

func (s *Session) EnqueueSteer(msg *Message, exclude *Client) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == SessionClosing || len(s.pendingSteers) >= maxPendingInputs {
		return false
	}
	s.pendingSteers = append(s.pendingSteers, queuedInput{message: cloneMessage(msg), exclude: exclude})
	s.State = SessionActive
	s.touchLocked()
	return true
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

func (s *Session) CompleteTurn() (queuedInput, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelRequested = false
	if len(s.pendingInputs) > 0 {
		input := s.pendingInputs[0]
		copy(s.pendingInputs, s.pendingInputs[1:])
		s.pendingInputs = s.pendingInputs[:len(s.pendingInputs)-1]
		s.inflightTurn = true
		s.State = SessionActive
		s.touchLocked()
		return input, true
	}
	s.inflightTurn = false
	if s.State != SessionClosing && len(s.clients) == 0 {
		s.State = SessionIdle
	}
	s.touchLocked()
	return queuedInput{}, false
}

func (s *Session) EnqueueInput(msg *Message, exclude *Client) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == SessionClosing || len(s.pendingInputs) >= maxPendingInputs {
		return false
	}
	s.pendingInputs = append(s.pendingInputs, queuedInput{message: cloneMessage(msg), exclude: exclude})
	s.State = SessionActive
	s.touchLocked()
	return true
}

func (s *Session) PendingInputCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.pendingInputs)
}

func (s *Session) RequestCancel() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == SessionClosing || !s.inflightTurn || s.cancelRequested {
		return false
	}
	s.cancelRequested = true
	s.pendingInputs = nil
	s.pendingSteers = nil
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
		s.inflightTurn = false
		s.cancelRequested = false
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
