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
	s.SetModel("gpt-5.5")
	s.SetPermissions("workspace-write")
	s.SetGoal("持续维护 QQ-Codex 网关")
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
	ctrl := r.ControlState()
	if ctrl.Model != "gpt-5.5" {
		t.Fatalf("model = %q, want gpt-5.5", ctrl.Model)
	}
	if ctrl.Permissions != "workspace-write" {
		t.Fatalf("permissions = %q, want workspace-write", ctrl.Permissions)
	}
	if ctrl.Goal != "持续维护 QQ-Codex 网关" {
		t.Fatalf("goal = %q, want saved goal", ctrl.Goal)
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
