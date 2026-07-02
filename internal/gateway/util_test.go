package gateway

import (
	"strings"
	"testing"
)

func TestCleanContent(t *testing.T) {
	cases := map[string]string{
		"<@!123456> hello there":      "hello there",
		"  <@987> what is <#42> ?":    "what is  ?",
		"plain message":               "plain message",
		"<emoji:4> hi <@!1> <@2> bye": "hi   bye",
		"／help":                       "/help", // full-width slash folds to a command
		"／model opus":                 "/model opus",
	}
	for in, want := range cases {
		if got := cleanContent(in); got != want {
			t.Errorf("cleanContent(%q) = %q, want %q", in, got, want)
		}
	}
}

// truncateRunes must never panic on a zero/negative budget (a tiny max_reply_chars
// once underflowed the slice index).
func TestTruncateRunesNonPositive(t *testing.T) {
	for _, n := range []int{0, -1, -100} {
		if got := truncateRunes("hello world", n); got != "" {
			t.Errorf("truncateRunes(_, %d) = %q, want empty", n, got)
		}
	}
}

func TestSplitMessageShort(t *testing.T) {
	chunks := splitMessage("hello world", 100)
	if len(chunks) != 1 || chunks[0] != "hello world" {
		t.Fatalf("unexpected chunks: %#v", chunks)
	}
}

func TestSplitMessageByLines(t *testing.T) {
	text := strings.Repeat("line\n", 50) // 250 runes
	chunks := splitMessage(text, 60)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, c := range chunks {
		if runeLen(c) > 60 {
			t.Errorf("chunk exceeds max: %d runes", runeLen(c))
		}
	}
}

func TestSplitMessageHardSplit(t *testing.T) {
	long := strings.Repeat("x", 250) // single overlong line
	chunks := splitMessage(long, 60)
	if len(chunks) != 5 {
		t.Fatalf("expected 5 chunks, got %d", len(chunks))
	}
	rejoined := strings.Join(chunks, "")
	if rejoined != long {
		t.Errorf("hard split lost data")
	}
}

func TestTruncateRunes(t *testing.T) {
	if got := truncateRunes("héllo wörld", 5); runeLen(got) != 5 {
		t.Errorf("truncateRunes len = %d, want 5 (%q)", runeLen(got), got)
	}
	if got := truncateRunes("short", 50); got != "short" {
		t.Errorf("truncateRunes shouldn't change short strings, got %q", got)
	}
}
