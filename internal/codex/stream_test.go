package codex

import (
	"strings"
	"testing"
)

func TestConsumeStreamSuccess(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-123"}`,
		`{"type":"item.started","item":{"type":"command_execution","command":"go test ./..."}}`,
		`{"type":"item.started","item":{"type":"mcp_tool_call","name":"docs"}}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"all done"}}`,
		`{"type":"turn.completed","usage":{"input_tokens":10,"cached_input_tokens":4,"output_tokens":5,"reasoning_output_tokens":2,"total_tokens":15}}`,
	}, "\n")

	var tools []string
	res, sid, fallback, err := consumeStream(strings.NewReader(stream), func(t string) { tools = append(tools, t) })
	if err != nil {
		t.Fatalf("unexpected scan error: %v", err)
	}

	if res == nil {
		t.Fatal("expected a terminal result event")
	}
	if res.Text != "all done" {
		t.Errorf("Text = %q, want %q", res.Text, "all done")
	}
	if res.SessionID != "thread-123" || sid != "thread-123" {
		t.Errorf("session id = %q / %q, want thread-123", res.SessionID, sid)
	}
	if res.InputTokens != 10 || res.CachedInputTokens != 4 || res.OutputTokens != 5 ||
		res.ReasoningOutputTokens != 2 || res.TotalTokens != 15 {
		t.Errorf("metadata mismatch: %+v", res)
	}
	if len(fallback) != 0 {
		t.Errorf("unexpected fallback: %q", fallback)
	}
	if strings.Join(tools, ",") != "shell,mcp:docs" {
		t.Errorf("tools = %v, want [shell mcp:docs]", tools)
	}
}

// A turn killed mid-stream emits no result event, but we must still recover the
// session id so the caller can resume instead of losing the conversation.
func TestConsumeStreamKilledKeepsSessionID(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-abc"}`,
		`{"type":"item.started","item":{"type":"command_execution","command":"sleep 5"}}`,
		// process killed here — no turn.completed event
	}, "\n")

	res, sid, _, _ := consumeStream(strings.NewReader(stream), nil)
	if res != nil {
		t.Errorf("expected no result event, got %+v", res)
	}
	if sid != "thread-abc" {
		t.Errorf("session id = %q, want thread-abc", sid)
	}
}

// Non-stream-json lines (e.g. an unexpected plain-JSON or text output) are kept as
// a fallback so the caller can still salvage a reply.
func TestConsumeStreamFallback(t *testing.T) {
	res, _, fallback, _ := consumeStream(strings.NewReader("not json at all\n"), nil)
	if res != nil {
		t.Errorf("expected no result, got %+v", res)
	}
	if strings.TrimSpace(string(fallback)) != "not json at all" {
		t.Errorf("fallback = %q", fallback)
	}
}

func TestConsumeStreamResultError(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"thread.started","thread_id":"s"}`,
		`{"type":"turn.failed","error":{"message":"hit the wall"}}`,
	}, "\n")
	res, _, _, _ := consumeStream(strings.NewReader(stream), nil)
	if res == nil || !res.IsError {
		t.Fatalf("expected an error result, got %+v", res)
	}
	if res.Text != "hit the wall" {
		t.Errorf("Text = %q, want fallback to error field", res.Text)
	}
}
