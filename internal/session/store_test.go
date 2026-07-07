package session

import (
	"path/filepath"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	m := NewManager()
	m.SetStatePath(path)

	s := m.Get("c2c:user")
	s.SetSessionID("thread-123")
	s.IncTurn()
	s.QueuePending("pending reply")
	seq := s.NextSeq()
	if err := m.SaveState(); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	restored := NewManager()
	restored.SetStatePath(path)
	if err := restored.LoadState(); err != nil {
		t.Fatalf("LoadState: %v", err)
	}
	r := restored.Get("c2c:user")
	if got := r.GetSessionID(); got != "thread-123" {
		t.Fatalf("session id = %q, want thread-123", got)
	}
	if got := r.TurnCount(); got != 1 {
		t.Fatalf("turn count = %d, want 1", got)
	}
	if got := r.TakePending(); len(got) != 1 || got[0] != "pending reply" {
		t.Fatalf("pending = %#v", got)
	}
	if got := r.NextSeq(); got != seq+1 {
		t.Fatalf("next seq = %d, want %d", got, seq+1)
	}
}

func TestLoadStateMissingFile(t *testing.T) {
	m := NewManager()
	m.SetStatePath(filepath.Join(t.TempDir(), "missing.json"))
	if err := m.LoadState(); err != nil {
		t.Fatalf("LoadState missing file returned error: %v", err)
	}
}
