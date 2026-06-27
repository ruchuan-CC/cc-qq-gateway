// Package session tracks per-conversation Claude Code sessions, resetting them
// after inactivity and serializing turns within a single conversation.
package session

import (
	"context"
	"sync"
	"time"
)

// Session holds the Claude session id and bookkeeping for one conversation.
type Session struct {
	// Key uniquely identifies the conversation (e.g. "group:<openid>").
	Key string
	// ClaudeSessionID is the resumable Claude Code session id (empty until the
	// first turn completes).
	ClaudeSessionID string
	// LastActive is the time of the last interaction.
	LastActive time.Time
	// MsgSeq is the per-conversation reply sequence counter for QQ group/C2C
	// passive replies.
	MsgSeq int
	// Model, WorkDir and Mode override the gateway defaults for this conversation
	// only, driven by /model, /dir and /mode. Empty means "use default".
	Model   string
	WorkDir string
	Mode    string // permission mode: default|plan|acceptEdits|bypass
	// Turns counts completed Claude turns in this conversation.
	Turns int

	// mu serializes turns within this conversation so messages are processed in
	// order and never concurrently.
	mu sync.Mutex

	// ctrl guards the in-flight turn's cancel func and running flag, plus the
	// last-turn bookkeeping below. It is a separate lock from mu so /stop can
	// cancel a turn (and commands can read stats) while mu is held by a turn.
	ctrl       sync.Mutex
	cancel     context.CancelFunc
	running    bool
	lastPrompt string  // last user message, for /retry
	lastCost   float64 // last turn cost (USD), for /cost
	lastDurMS  int     // last turn duration (ms), for /cost
	thinkNext  bool    // next turn uses extended thinking, set by /think
}

// RecordTurn stores bookkeeping from a completed turn for /retry and /cost.
func (s *Session) RecordTurn(prompt string, costUSD float64, durMS int) {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	if prompt != "" {
		s.lastPrompt = prompt
	}
	s.lastCost = costUSD
	s.lastDurMS = durMS
}

// LastPrompt returns the last user message (for /retry).
func (s *Session) LastPrompt() string {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return s.lastPrompt
}

// LastStats returns the last turn's cost (USD) and duration (ms).
func (s *Session) LastStats() (float64, int) {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return s.lastCost, s.lastDurMS
}

// SetThinkNext marks the next turn to use extended thinking.
func (s *Session) SetThinkNext() {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	s.thinkNext = true
}

// TakeThinkNext returns whether the next turn should think, clearing the flag.
func (s *Session) TakeThinkNext() bool {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	v := s.thinkNext
	s.thinkNext = false
	return v
}

// Lock serializes a turn for this conversation.
func (s *Session) Lock()   { s.mu.Lock() }
func (s *Session) Unlock() { s.mu.Unlock() }

// BeginTurn records the cancel func for the in-flight turn and marks it running.
func (s *Session) BeginTurn(cancel context.CancelFunc) {
	s.ctrl.Lock()
	s.cancel = cancel
	s.running = true
	s.ctrl.Unlock()
}

// EndTurn clears the in-flight turn state.
func (s *Session) EndTurn() {
	s.ctrl.Lock()
	s.cancel = nil
	s.running = false
	s.ctrl.Unlock()
}

// CancelTurn cancels the in-flight turn if one is running. Returns true if a
// turn was actually cancelled.
func (s *Session) CancelTurn() bool {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	if s.cancel != nil {
		s.cancel()
		return true
	}
	return false
}

// Running reports whether a turn is currently executing.
func (s *Session) Running() bool {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return s.running
}

// The per-conversation override fields (Model/WorkDir/Mode) and the Claude
// session bookkeeping (ClaudeSessionID/Turns/MsgSeq) are mutated by control
// commands (/model, /dir, /mode, /new) and the idle-reset path WHILE a turn is
// concurrently reading them under mu. They are therefore guarded by ctrl (the
// same lock /stop uses), so a command can update them while mu is held by a turn
// without a data race. runTurn must access them only through these accessors.

// GetModel / SetModel get/set the per-conversation model override.
func (s *Session) GetModel() string  { s.ctrl.Lock(); defer s.ctrl.Unlock(); return s.Model }
func (s *Session) SetModel(v string) { s.ctrl.Lock(); s.Model = v; s.ctrl.Unlock() }

// GetWorkDir / SetWorkDir get/set the per-conversation working-directory override.
func (s *Session) GetWorkDir() string  { s.ctrl.Lock(); defer s.ctrl.Unlock(); return s.WorkDir }
func (s *Session) SetWorkDir(v string) { s.ctrl.Lock(); s.WorkDir = v; s.ctrl.Unlock() }

// GetMode / SetMode get/set the per-conversation permission-mode override.
func (s *Session) GetMode() string  { s.ctrl.Lock(); defer s.ctrl.Unlock(); return s.Mode }
func (s *Session) SetMode(v string) { s.ctrl.Lock(); s.Mode = v; s.ctrl.Unlock() }

// GetSessionID / SetSessionID get/set the resumable Claude session id.
func (s *Session) GetSessionID() string {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return s.ClaudeSessionID
}
func (s *Session) SetSessionID(v string) { s.ctrl.Lock(); s.ClaudeSessionID = v; s.ctrl.Unlock() }

// IncTurn increments and returns the completed-turn count.
func (s *Session) IncTurn() int { s.ctrl.Lock(); defer s.ctrl.Unlock(); s.Turns++; return s.Turns }

// TurnCount returns the completed-turn count.
func (s *Session) TurnCount() int { s.ctrl.Lock(); defer s.ctrl.Unlock(); return s.Turns }

// HasSession reports whether a resumable Claude session id is set.
func (s *Session) HasSession() bool {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return s.ClaudeSessionID != ""
}

// ClearClaude clears the resumable session id (and the reply seq), starting a
// fresh Claude conversation on the next turn.
func (s *Session) ClearClaude() {
	s.ctrl.Lock()
	s.ClaudeSessionID = ""
	s.MsgSeq = 0
	s.ctrl.Unlock()
}

// Manager owns the set of live sessions.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	idleTTL  time.Duration
}

// NewManager creates a session manager. idleTTL is how long a session may be
// idle before it is reset; zero disables expiry.
func NewManager(idleTTL time.Duration) *Manager {
	return &Manager{
		sessions: make(map[string]*Session),
		idleTTL:  idleTTL,
	}
}

// Get returns (creating if needed) the session for a conversation key. If the
// existing session has been idle past the TTL it is reset (a fresh Claude
// session will be started on the next turn).
func (m *Manager) Get(key string) *Session {
	m.mu.Lock()
	defer m.mu.Unlock()

	s, ok := m.sessions[key]
	now := time.Now()
	if !ok {
		s = &Session{Key: key, LastActive: now}
		m.sessions[key] = s
		return s
	}
	if m.idleTTL > 0 && now.Sub(s.LastActive) > m.idleTTL {
		s.ClearClaude()
	}
	s.LastActive = now
	return s
}

// Reset clears the Claude session id for a conversation, starting fresh on the
// next turn. Returns false if no session existed.
func (m *Manager) Reset(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, ok := m.sessions[key]
	if !ok {
		return false
	}
	s.ClearClaude()
	return true
}

// Touch updates the last-active time.
func (m *Manager) Touch(key string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if s, ok := m.sessions[key]; ok {
		s.LastActive = time.Now()
	}
}

// Count returns the number of live sessions.
func (m *Manager) Count() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sessions)
}

// Summary is a point-in-time view of a conversation, for status commands.
type Summary struct {
	Key        string
	Active     bool // has a Claude session id
	Running    bool // a turn is in flight
	Turns      int
	LastActive time.Time
}

// Snapshot returns summaries of all live sessions.
func (m *Manager) Snapshot() []Summary {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Summary, 0, len(m.sessions))
	for _, s := range m.sessions {
		out = append(out, Summary{
			Key:        s.Key,
			Active:     s.HasSession(),
			Running:    s.Running(),
			Turns:      s.TurnCount(),
			LastActive: s.LastActive,
		})
	}
	return out
}
