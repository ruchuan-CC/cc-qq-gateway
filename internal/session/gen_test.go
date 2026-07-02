package session

import "testing"

// A turn that finishes right as /new (ClearClaude) resets the conversation must not
// resurrect the cleared context: SetSessionIDIfGen with the pre-turn generation is
// rejected once the generation has moved on.
func TestSetSessionIDIfGenGuardsAgainstReset(t *testing.T) {
	s := &Session{Key: "c2c:x"}

	// Normal case: generation unchanged, the write lands.
	gen := s.ClaudeGen()
	if !s.SetSessionIDIfGen("sess-1", gen) {
		t.Fatal("expected the write to land when generation is unchanged")
	}
	if s.GetSessionID() != "sess-1" {
		t.Fatalf("session id = %q, want sess-1", s.GetSessionID())
	}

	// A turn captures the generation, then /new clears the session mid-turn.
	gen = s.ClaudeGen()
	s.ClearClaude() // /new
	if s.GetSessionID() != "" {
		t.Fatalf("ClearClaude should empty the id, got %q", s.GetSessionID())
	}
	// The late write from the finishing turn must be rejected, leaving it cleared.
	if s.SetSessionIDIfGen("sess-2", gen) {
		t.Error("expected the stale-generation write to be rejected")
	}
	if s.GetSessionID() != "" {
		t.Errorf("cleared context was resurrected: id = %q", s.GetSessionID())
	}
}
