package session

import (
	"path/filepath"
	"testing"
	"time"
)

// TestStateRoundTrip verifies that everything durable about a conversation
// survives a save/load cycle — the guarantee that a gateway restart is
// invisible to the user.
func TestStateRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")

	m1 := NewManager(0)
	m1.SetStatePath(path)
	s := m1.Get("c2c:user1")
	s.SetSessionID("sess-abc")
	s.SetModel("opus")
	s.SetWorkDir("/tmp/w")
	s.SetMode("plan")
	s.SetTimeoutMin(45)
	s.IncTurn()
	s.IncTurn()
	s.RecordTurn("上一条消息", 0.12, 3400)
	s.SetSeed("压缩摘要内容")
	s.QueuePending("排队中的回复")
	for i := 0; i < 7; i++ {
		s.NextSeq()
	}
	if err := m1.SaveState(); err != nil {
		t.Fatalf("save: %v", err)
	}

	m2 := NewManager(0)
	m2.SetStatePath(path)
	if err := m2.LoadState(); err != nil {
		t.Fatalf("load: %v", err)
	}
	r := m2.Get("c2c:user1")
	if got := r.GetSessionID(); got != "sess-abc" {
		t.Errorf("session id = %q", got)
	}
	if r.GetModel() != "opus" || r.GetWorkDir() != "/tmp/w" || r.GetMode() != "plan" {
		t.Errorf("overrides = %q %q %q", r.GetModel(), r.GetWorkDir(), r.GetMode())
	}
	if r.GetTimeoutMin() != 45 {
		t.Errorf("timeout = %d", r.GetTimeoutMin())
	}
	if r.TurnCount() != 2 {
		t.Errorf("turns = %d", r.TurnCount())
	}
	if r.LastPrompt() != "上一条消息" {
		t.Errorf("last prompt = %q", r.LastPrompt())
	}
	if r.PeekSeed() != "压缩摘要内容" {
		t.Errorf("seed = %q", r.PeekSeed())
	}
	if p := r.TakePending(); len(p) != 1 || p[0] != "排队中的回复" {
		t.Errorf("pending = %v", p)
	}
	// The msg_seq counter must continue past the persisted value, never reuse.
	if got := r.NextSeq(); got != 8 {
		t.Errorf("next seq = %d, want 8", got)
	}
}

// TestLoadStateMissingFile ensures a first run (no state file) is not an error.
func TestLoadStateMissingFile(t *testing.T) {
	m := NewManager(0)
	m.SetStatePath(filepath.Join(t.TempDir(), "absent.json"))
	if err := m.LoadState(); err != nil {
		t.Fatalf("missing file should be fine: %v", err)
	}
}

// TestIdleRestoredSessionStillExpires ensures a restored-but-stale session goes
// through the normal idle reset (with its user-visible flag) on next contact.
func TestIdleRestoredSessionStillExpires(t *testing.T) {
	path := filepath.Join(t.TempDir(), "state.json")
	m1 := NewManager(0)
	m1.SetStatePath(path)
	s := m1.Get("c2c:u")
	s.SetSessionID("old")
	s.LastActive = time.Now().Add(-2 * time.Hour)
	if err := m1.SaveState(); err != nil {
		t.Fatal(err)
	}

	m2 := NewManager(30 * time.Minute)
	m2.SetStatePath(path)
	if err := m2.LoadState(); err != nil {
		t.Fatal(err)
	}
	r := m2.Get("c2c:u")
	if r.GetSessionID() != "" {
		t.Errorf("stale session should have been reset, got %q", r.GetSessionID())
	}
	if !r.TakeIdleReset() {
		t.Errorf("idle reset should be flagged for the user notice")
	}
}

// TestSeedTakeOnce ensures the compact seed is consumed exactly once.
func TestSeedTakeOnce(t *testing.T) {
	s := &Session{}
	s.SetSeed("x")
	if s.TakeSeed() != "x" || s.TakeSeed() != "" {
		t.Error("seed must be consumed exactly once")
	}
}

// TestAttachSessionBumpsGen ensures /resume prevents an in-flight turn from
// overwriting the attached session id.
func TestAttachSessionBumpsGen(t *testing.T) {
	s := &Session{}
	gen := s.ClaudeGen()
	s.AttachSession("newid")
	if s.SetSessionIDIfGen("stale-write", gen) {
		t.Error("stale generation write must be rejected after AttachSession")
	}
	if s.GetSessionID() != "newid" {
		t.Errorf("session id = %q", s.GetSessionID())
	}
}
