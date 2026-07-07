package session

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type persistedSession struct {
	Key        string    `json:"key"`
	SessionID  string    `json:"session_id,omitempty"`
	Turns      int       `json:"turns,omitempty"`
	Seq        int64     `json:"seq,omitempty"`
	LastActive time.Time `json:"last_active"`
	Pending    []string  `json:"pending,omitempty"`
}

type persistedState struct {
	Version  int                `json:"version"`
	SavedAt  time.Time          `json:"saved_at"`
	Sessions []persistedSession `json:"sessions"`
}

func (s *Session) exportState() persistedSession {
	s.ctrl.Lock()
	defer s.ctrl.Unlock()
	return persistedSession{
		Key:        s.Key,
		SessionID:  s.ThreadID,
		Turns:      s.Turns,
		Seq:        s.seqCounter.Load(),
		LastActive: s.LastActive,
		Pending:    append([]string(nil), s.pending...),
	}
}

func (s *Session) importState(p persistedSession) {
	s.ctrl.Lock()
	s.ThreadID = p.SessionID
	s.Turns = p.Turns
	s.pending = append([]string(nil), p.Pending...)
	s.ctrl.Unlock()
	s.seqCounter.Store(p.Seq)
	s.LastActive = p.LastActive
}

// SetStatePath configures where SaveState/LoadState persist sessions. Empty
// disables persistence.
func (m *Manager) SetStatePath(path string) {
	m.mu.Lock()
	m.statePath = path
	m.mu.Unlock()
}

// LoadState restores sessions from disk. A missing file is not an error.
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

// SaveState atomically writes all sessions to the state file.
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

	st := persistedState{Version: 2, SavedAt: time.Now()}
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
