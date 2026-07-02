package session

import (
	"testing"
	"time"
)

// An idle-TTL reset must be flagged exactly once, and only when a live Claude
// context was actually lost.
func TestIdleResetFlag(t *testing.T) {
	m := NewManager(time.Millisecond)

	s := m.Get("c2c:u1")
	s.SetSessionID("sess-1")

	// Simulate idleness past the TTL.
	m.mu.Lock()
	s.LastActive = time.Now().Add(-time.Hour)
	m.mu.Unlock()

	s2 := m.Get("c2c:u1")
	if s2 != s {
		t.Fatalf("expected the same session object")
	}
	if s2.GetSessionID() != "" {
		t.Fatalf("idle reset should clear the Claude session id")
	}
	if !s2.TakeIdleReset() {
		t.Fatalf("idle reset of a live context must set the flag")
	}
	if s2.TakeIdleReset() {
		t.Fatalf("the flag must clear after being taken")
	}
}

// Resetting an already-fresh session (no Claude context) must not nag.
func TestIdleResetFlagNotSetWithoutContext(t *testing.T) {
	m := NewManager(time.Millisecond)
	s := m.Get("c2c:u2")

	m.mu.Lock()
	s.LastActive = time.Now().Add(-time.Hour)
	m.mu.Unlock()

	if m.Get("c2c:u2").TakeIdleReset() {
		t.Fatalf("no context was lost; the flag must stay clear")
	}
}

// An explicit clear (/new) is announced by its own confirmation and must not
// additionally set the idle flag.
func TestExplicitClearDoesNotFlag(t *testing.T) {
	m := NewManager(0)
	s := m.Get("c2c:u3")
	s.SetSessionID("sess-3")
	s.ClearClaude()
	if s.TakeIdleReset() {
		t.Fatalf("ClearClaude must not set the idle-reset flag")
	}
}
