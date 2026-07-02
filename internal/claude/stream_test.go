package claude

import (
	"strings"
	"testing"
)

func TestConsumeStreamSuccess(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"sess-123","tools":["Bash"]}`,
		`{"type":"assistant","session_id":"sess-123","message":{"content":[{"type":"text","text":"working"},{"type":"tool_use","name":"Bash"}]}}`,
		`{"type":"assistant","session_id":"sess-123","message":{"content":[{"type":"tool_use","name":"Read"}]}}`,
		`{"type":"result","subtype":"success","session_id":"sess-123","result":"all done","total_cost_usd":0.04,"num_turns":2,"duration_ms":1500}`,
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
	if res.SessionID != "sess-123" || sid != "sess-123" {
		t.Errorf("session id = %q / %q, want sess-123", res.SessionID, sid)
	}
	if res.CostUSD != 0.04 || res.NumTurns != 2 || res.DurationMS != 1500 {
		t.Errorf("metadata mismatch: %+v", res)
	}
	if len(fallback) != 0 {
		t.Errorf("unexpected fallback: %q", fallback)
	}
	if strings.Join(tools, ",") != "Bash,Read" {
		t.Errorf("tools = %v, want [Bash Read]", tools)
	}
}

// A turn killed mid-stream emits no result event, but we must still recover the
// session id so the caller can resume instead of losing the conversation.
func TestConsumeStreamKilledKeepsSessionID(t *testing.T) {
	stream := strings.Join([]string{
		`{"type":"system","subtype":"init","session_id":"sess-abc"}`,
		`{"type":"assistant","session_id":"sess-abc","message":{"content":[{"type":"tool_use","name":"Bash"}]}}`,
		// process killed here — no result event
	}, "\n")

	res, sid, _, _ := consumeStream(strings.NewReader(stream), nil)
	if res != nil {
		t.Errorf("expected no result event, got %+v", res)
	}
	if sid != "sess-abc" {
		t.Errorf("session id = %q, want sess-abc", sid)
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
	stream := `{"type":"result","subtype":"error_max_turns","is_error":true,"session_id":"s","error":"hit the wall"}`
	res, _, _, _ := consumeStream(strings.NewReader(stream), nil)
	if res == nil || !res.IsError {
		t.Fatalf("expected an error result, got %+v", res)
	}
	if res.Text != "hit the wall" {
		t.Errorf("Text = %q, want fallback to error field", res.Text)
	}
}
