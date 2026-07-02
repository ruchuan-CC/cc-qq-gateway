package claude

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A turn killed by the per-turn timeout must surface ErrTurnTimeout so the
// gateway can tell the user the task hit its time limit (and can be resumed)
// instead of a raw "signal: killed".
func TestRunReportsTurnTimeout(t *testing.T) {
	dir := t.TempDir()
	slow := filepath.Join(dir, "slow-claude")
	// A stand-in binary that emits a session id event then outlives the timeout.
	script := "#!/bin/sh\necho '{\"type\":\"system\",\"session_id\":\"sess-slow\"}'\nsleep 5\n"
	if err := os.WriteFile(slow, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}

	b := New(Config{Binary: slow, Timeout: 300 * time.Millisecond})
	res, err := b.Run(context.Background(), Request{Prompt: "hi"})
	if err == nil {
		t.Fatalf("expected an error from the timed-out turn")
	}
	if !errors.Is(err, ErrTurnTimeout) {
		t.Fatalf("expected ErrTurnTimeout, got %v", err)
	}
	if res == nil || res.SessionID != "sess-slow" {
		t.Fatalf("the session id seen mid-stream must survive for resume, got %+v", res)
	}
}

// A user cancel (context cancellation) is not a timeout and must not be
// reported as one.
func TestRunCancelIsNotTimeout(t *testing.T) {
	dir := t.TempDir()
	slow := filepath.Join(dir, "slow-claude")
	if err := os.WriteFile(slow, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	b := New(Config{Binary: slow, Timeout: time.Minute})
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()
	_, err := b.Run(ctx, Request{Prompt: "hi"})
	if err == nil {
		t.Fatalf("expected an error from the cancelled turn")
	}
	if errors.Is(err, ErrTurnTimeout) {
		t.Fatalf("a cancel must not masquerade as a timeout: %v", err)
	}
}
