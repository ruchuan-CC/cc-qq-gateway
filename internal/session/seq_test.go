package session

import (
	"sync"
	"testing"
)

// msg_seq must be monotonic and start at 1 (0 is omitted on the wire by
// omitempty, and QQ rejects a reused seq with code 40054005). It must also
// survive a session reset so the sequence never goes backwards while the process
// lives — a backwards/reused seq is exactly what caused the dedup failures.
func TestNextSeqMonotonicAndSurvivesReset(t *testing.T) {
	s := &Session{Key: "c2c:x"}
	if first := s.NextSeq(); first != 1 {
		t.Fatalf("first NextSeq = %d, want 1", first)
	}
	if second := s.NextSeq(); second != 2 {
		t.Fatalf("second NextSeq = %d, want 2", second)
	}
	// A reset (/new or idle) clears the Claude session but must NOT rewind seq.
	s.ClearClaude()
	if next := s.NextSeq(); next != 3 {
		t.Fatalf("NextSeq after ClearClaude = %d, want 3 (must not rewind)", next)
	}
}

// NextSeq is shared by every responder for a conversation (concurrent turns,
// active pushes, the notify endpoint), so it must hand out distinct values under
// concurrency. Run with -race.
func TestNextSeqConcurrentDistinct(t *testing.T) {
	s := &Session{Key: "c2c:x"}
	const n = 500
	var wg sync.WaitGroup
	seen := make([]int, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			seen[idx] = s.NextSeq()
		}(i)
	}
	wg.Wait()

	uniq := make(map[int]bool, n)
	for _, v := range seen {
		if uniq[v] {
			t.Fatalf("duplicate msg_seq %d handed out under concurrency", v)
		}
		uniq[v] = true
	}
	if len(uniq) != n {
		t.Fatalf("got %d distinct seqs, want %d", len(uniq), n)
	}
}
