// Session persistence: the manager can snapshot every conversation's durable
// state to a JSON file and restore it at startup, so a gateway restart (upgrade,
// crash, reboot) no longer loses the resumable Claude session, per-conversation
// settings, or queued replies — the conversation continues as if nothing happened.
package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// persistedSession is the durable subset of a Session. Runtime-only state
// (in-flight turn, cancel func, idle-reset flag, resume listing) is not saved.
type persistedSession struct {
	Key        string    `json:"key"`
	SessionID  string    `json:"session_id,omitempty"`
	Model      string    `json:"model,omitempty"`
	WorkDir    string    `json:"work_dir,omitempty"`
	Mode       string    `json:"mode,omitempty"`
	TimeoutMin int       `json:"timeout_min,omitempty"`
	Turns      int       `json:"turns,omitempty"`
	Seq        int64     `json:"seq,omitempty"`
	LastActive time.Time `json:"last_active"`
	LastPrompt string    `json:"last_prompt,omitempty"`
	Seed       string    `json:"seed,omitempty"`
	Pending    []string  `json:"pending,omitempty"`
}

// persistedState is the on-disk file shape.
type persistedState struct {
	Version  int                `json:"version"`
	SavedAt  time.Time          `json:"saved_at"`
	Sessions []persistedSession `json:"sessions"`
}

// exportState snapshots a session's durable fields.
func (s *Session) exportState() persistedSession {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return persistedSession{
		Key:        s.Key,
		SessionID:  s.ClaudeSessionID,
		Model:      s.Model,
		WorkDir:    s.WorkDir,
		Mode:       s.Mode,
		TimeoutMin: s.TimeoutMin,
		Turns:      s.Turns,
		Seq:        s.seqCounter.Load(),
		LastActive: s.LastActive,
		LastPrompt: s.lastPrompt,
		Seed:       s.seed,
		Pending:    append([]string(nil), s.pending...),
	}
}

// importState restores a session's durable fields from a snapshot.
func (s *Session) importState(p persistedSession) {
	s.ctrl.Lock()
	s.ClaudeSessionID = p.SessionID
	s.Model = p.Model
	s.WorkDir = p.WorkDir
	s.Mode = p.Mode
	s.TimeoutMin = p.TimeoutMin
	s.Turns = p.Turns
	s.lastPrompt = p.LastPrompt
	s.seed = p.Seed
	s.pending = append([]string(nil), p.Pending...)
	s.ctrl.Unlock()
	s.seqCounter.Store(p.Seq)
	s.LastActive = p.LastActive
}

// SetStatePath configures where SaveState/LoadState persist the sessions.
// Empty disables persistence (both become no-ops).
func (m *Manager) SetStatePath(path string) {
	m.mu.Lock()
	m.statePath = path
	m.mu.Unlock()
}

// LoadState restores sessions from the state file. A missing file is not an
// error (first run). Sessions idle past the TTL are still restored — the normal
// Get() path applies the idle reset with its user-visible notice.
func (m *Manager) LoadState() error {
	m.mu.Lock()
	path := m.statePath
	m.mu.Unlock()
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read session state %s: %w", path, err)
	}
	var st persistedState
	if err := json.Unmarshal(data, &st); err != nil {
		return fmt.Errorf("decode session state %s: %w", path, err)
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range st.Sessions {
		if p.Key == "" {
			continue
		}
		s, ok := m.sessions[p.Key]
		if !ok {
			s = &Session{Key: p.Key}
			m.sessions[p.Key] = s
		}
		s.importState(p)
		if s.LastActive.IsZero() {
			s.LastActive = time.Now()
		}
	}
	return nil
}

// SaveState atomically writes all sessions to the state file (0600 — it holds
// conversation remnants like the last prompt and queued replies).
func (m *Manager) SaveState() error {
	m.mu.Lock()
	path := m.statePath
	sessions := make([]*Session, 0, len(m.sessions))
	for _, s := range m.sessions {
		sessions = append(sessions, s)
	}
	m.mu.Unlock()
	if path == "" {
		return nil
	}

	st := persistedState{Version: 1, SavedAt: time.Now()}
	for _, s := range sessions {
		st.Sessions = append(st.Sessions, s.exportState())
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
