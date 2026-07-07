// Package session tracks per-conversation Codex threads and serializes turns
// within a single QQ conversation.
package session

import (
	"sync"
	"sync/atomic"
	"time"
)

// Session holds the durable Codex thread id and runtime bookkeeping for one
// QQ C2C conversation.
type Session struct {
	Key        string
	ThreadID   string
	LastActive time.Time
	Turns      int
	Control    ControlState

	mu sync.Mutex

	ctrl               sync.Mutex
	running            bool
	pending            []string
	pendingAttachments []AttachmentRef

	// QQ rejects reused msg_seq values, so every reply path for a user shares one
	// process-lifetime counter.
	seqCounter atomic.Int64
}

// AttachmentRef is an inbound QQ attachment after the gateway has attempted to
// materialize it locally.
type AttachmentRef struct {
	Kind  string
	Path  string
	URL   string
	Error string
}

// ControlState 是每个 QQ 用户自己的 Codex 交互偏好；空值表示使用 config.toml 默认值。
type ControlState struct {
	Model       string `json:"model,omitempty"`
	Permissions string `json:"permissions,omitempty"`
	Goal        string `json:"goal,omitempty"`
}

// NextSeq returns the next monotonic msg_seq for this conversation.
func (s *Session) NextSeq() int { return int(s.seqCounter.Add(1)) }

// QueuePending stores a reply that could not be delivered now.
func (s *Session) QueuePending(text string) {
	if text == "" {
		return
	}
	s.ctrl.Lock()
	s.pending = append(s.pending, text)
	s.ctrl.Unlock()
}

// TakePending returns and clears queued replies.
func (s *Session) TakePending() []string {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	p := s.pending
	s.pending = nil
	return p
}

// QueuePendingAttachments stores attachment refs that are waiting for the
// user's next text prompt.
func (s *Session) QueuePendingAttachments(refs []AttachmentRef) {
	if len(refs) == 0 {
		return
	}
	s.ctrl.Lock()
	s.pendingAttachments = append(s.pendingAttachments, refs...)
	s.ctrl.Unlock()
}

// TakePendingAttachments returns and clears attachment refs waiting for the next
// text prompt.
func (s *Session) TakePendingAttachments() []AttachmentRef {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	refs := s.pendingAttachments
	s.pendingAttachments = nil
	return refs
}

// ControlState returns a copy of this user's persisted Codex controls.
func (s *Session) ControlState() ControlState {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return s.Control
}

// SetModel stores the per-user model override. Empty means config default.
func (s *Session) SetModel(model string) {
	s.ctrl.Lock()
	s.Control.Model = model
	s.ctrl.Unlock()
}

// SetPermissions stores the per-user permissions override. Empty means config default.
func (s *Session) SetPermissions(permissions string) {
	s.ctrl.Lock()
	s.Control.Permissions = permissions
	s.ctrl.Unlock()
}

// SetGoal stores the QQ session goal that is prepended to future Codex turns.
func (s *Session) SetGoal(goal string) {
	s.ctrl.Lock()
	s.Control.Goal = goal
	s.ctrl.Unlock()
}

// ClearGoal removes the QQ session goal.
func (s *Session) ClearGoal() {
	s.ctrl.Lock()
	s.Control.Goal = ""
	s.ctrl.Unlock()
}

// Lock serializes Codex turns for this conversation.
func (s *Session) Lock()   { s.mu.Lock() }
func (s *Session) Unlock() { s.mu.Unlock() }

// BeginTurn marks a Codex turn as running.
func (s *Session) BeginTurn() {
	s.ctrl.Lock()
	s.running = true
	s.ctrl.Unlock()
}

// EndTurn clears the in-flight marker.
func (s *Session) EndTurn() {
	s.ctrl.Lock()
	s.running = false
	s.ctrl.Unlock()
}

// Running reports whether a turn is currently executing.
func (s *Session) Running() bool {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return s.running
}

// GetSessionID / SetSessionID get/set the resumable Codex thread id.
func (s *Session) GetSessionID() string {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return s.ThreadID
}

func (s *Session) SetSessionID(v string) {
	s.ctrl.Lock()
	s.ThreadID = v
	s.ctrl.Unlock()
}

// ClearThread clears the resumable Codex thread id.
func (s *Session) ClearThread() {
	s.ctrl.Lock()
	s.ThreadID = ""
	s.ctrl.Unlock()
}

// HasSession reports whether a resumable Codex thread id is set.
func (s *Session) HasSession() bool {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return s.ThreadID != ""
}

// IncTurn increments and returns the completed-turn count.
func (s *Session) IncTurn() int {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	s.Turns++
	return s.Turns
}

// TurnCount returns the completed-turn count.
func (s *Session) TurnCount() int {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return s.Turns
}

// Manager owns the set of live sessions.
type Manager struct {
	mu        sync.Mutex
	sessions  map[string]*Session
	statePath string
}

// NewManager creates a session manager.
func NewManager() *Manager {
	return &Manager{sessions: make(map[string]*Session)}
}

// Get returns the session for a conversation key, creating it when needed.
func (m *Manager) Get(key string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	s, ok := m.sessions[key]
	if !ok {
		s = &Session{Key: key, LastActive: now}
		m.sessions[key] = s
		return s
	}
	s.LastActive = now
	return s
}

// Reset clears the Codex thread id for a conversation.
func (m *Manager) Reset(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	if !ok {
		return false
	}
	s.ClearThread()
	return true
}
