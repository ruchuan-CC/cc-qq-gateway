package session

import "testing"

func TestPendingQueueOrderAndClear(t *testing.T) {
	s := &Session{Key: "c2c:x"}

	if got := s.TakePending(); got != nil {
		t.Fatalf("fresh session pending = %v, want nil", got)
	}

	s.QueuePending("first")
	s.QueuePending("") // ignored
	s.QueuePending("second")

	got := s.TakePending()
	if len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("pending = %v, want [first second]", got)
	}

	// TakePending must have cleared the queue.
	if again := s.TakePending(); again != nil {
		t.Fatalf("pending after take = %v, want nil", again)
	}
}

func TestPendingSurvivesClearClaude(t *testing.T) {
	// A queued result must not be dropped just because the Claude session id is
	// cleared (e.g. an idle reset or /new) — the user still wants that output.
	s := &Session{Key: "c2c:x", ClaudeSessionID: "abc"}
	s.QueuePending("result")
	s.ClearClaude()
	if got := s.TakePending(); len(got) != 1 || got[0] != "result" {
		t.Fatalf("pending after ClearClaude = %v, want [result]", got)
	}
}
