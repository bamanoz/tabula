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
	inflightTurn        bool
	inflightInput       queuedInput
	hasInflightInput    bool
	cancelRequested     bool
	pendingInputs       []queuedInput
	pendingSteers       []queuedInput
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
	s.inflightTurn = false
	s.cancelRequested = false
	s.pendingInputs = nil
	s.pendingSteers = nil
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
	if len(s.clients) == 0 && !s.inflightTurn && !s.stuckSuspended {
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
	if s.State == SessionClosing || s.stuckSuspended || s.inflightTurn {
		return false
	}
	s.inflightTurn = true
	s.inflightInput = queuedInput{}
	s.hasInflightInput = false
	s.cancelRequested = false
	s.State = SessionActive
	s.touchLocked()
	return true
}

func (s *Session) EndTurn() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.inflightTurn = false
	s.inflightInput = queuedInput{}
	s.hasInflightInput = false
	s.cancelRequested = false
	if s.stuckSuspended {
		s.State = SessionStuck
	} else if s.State != SessionClosing && len(s.clients) == 0 {
		s.State = SessionIdle
	}
	s.touchLocked()
}

func (s *Session) CompleteTurn() (queuedInput, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.cancelRequested = false
	s.inflightInput = queuedInput{}
	s.hasInflightInput = false
	if input, ok := s.shiftQueuedInputLocked(); ok {
		return input, true
	}
	s.inflightTurn = false
	if s.stuckSuspended {
		s.State = SessionStuck
	} else if s.State != SessionClosing && len(s.clients) == 0 {
		s.State = SessionIdle
	}
	s.touchLocked()
	return queuedInput{}, false
}

func (s *Session) SetInflightInput(msg *Message, exclude *Client) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.inflightTurn || msg == nil {
		return
	}
	s.inflightInput = queuedInput{message: cloneMessage(msg), exclude: exclude}
	s.hasInflightInput = true
	s.touchLocked()
}

func (s *Session) InterruptTurn() (queuedInput, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.inflightTurn {
		return queuedInput{}, false
	}
	input, hasInput := s.inflightInput, s.hasInflightInput && s.inflightInput.message != nil
	s.inflightTurn = false
	s.inflightInput = queuedInput{}
	s.hasInflightInput = false
	s.cancelRequested = false
	if hasInput {
		s.pendingInputs = append([]queuedInput{input}, s.pendingInputs...)
	}
	if s.stuckSuspended {
		s.State = SessionStuck
	} else if s.State != SessionClosing && len(s.clients) == 0 {
		s.State = SessionIdle
	}
	s.touchLocked()
	return input, hasInput
}

func (s *Session) BeginQueuedInput() (queuedInput, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.inflightTurn {
		return queuedInput{}, false
	}
	return s.shiftQueuedInputLocked()
}

func (s *Session) shiftQueuedInputLocked() (queuedInput, bool) {
	if len(s.pendingInputs) > 0 {
		input := s.pendingInputs[0]
		copy(s.pendingInputs, s.pendingInputs[1:])
		s.pendingInputs = s.pendingInputs[:len(s.pendingInputs)-1]
		s.inflightTurn = true
		s.inflightInput = input
		s.hasInflightInput = input.message != nil
		s.State = SessionActive
		s.touchLocked()
		return input, true
	}
	return queuedInput{}, false
}

func (s *Session) EnqueueInput(msg *Message, exclude *Client) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.State == SessionClosing || s.stuckSuspended || len(s.pendingInputs) >= maxPendingInputs {
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
	if s.stuckSuspended {
		s.stuckSuspended = false
		s.restartObservations = 0
		s.State = SessionIdle
		s.touchLocked()
		return true
	}
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

func (s *Session) observeRestart(active bool, previousObservations int, threshold int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !active {
		s.restartObservations = 0
		return
	}
	s.restartObservations = previousObservations + 1
	if threshold > 0 && s.restartObservations >= threshold {
		s.stuckSuspended = true
		s.State = SessionStuck
	}
	s.touchLocked()
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
