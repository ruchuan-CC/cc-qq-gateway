package gateway

import (
	"strings"
	"testing"
)

// The /help table is hand-aligned by display width (CJK = 2 cells). Every framed
// line of the rendered code block must therefore have identical display width, or
// it will look ragged on QQ. This guards that invariant as commands change.
func TestHelpTableAligned(t *testing.T) {
	var framed []string
	for _, line := range strings.Split(helpText, "\n") {
		// The bordered rows are exactly those drawn with box-drawing characters.
		if strings.HasPrefix(line, "│") || strings.HasPrefix(line, "┌") ||
			strings.HasPrefix(line, "├") || strings.HasPrefix(line, "└") {
			framed = append(framed, line)
		}
	}
	if len(framed) < 3 {
		t.Fatalf("expected a framed table, got %d framed lines", len(framed))
	}
	want := displayWidth(framed[0])
	for _, line := range framed {
		if got := displayWidth(line); got != want {
			t.Errorf("table line width %d != %d:\n%s", got, want, line)
		}
	}
}

// Every command in the help table must be a real, dispatchable alias so /help
// never advertises a command that does nothing.
func TestHelpCommandsAreReal(t *testing.T) {
	for _, c := range helpCommands {
		if _, ok := commandAliases[c.cmd]; !ok {
			t.Errorf("/help lists %q but it is not in commandAliases", c.cmd)
		}
	}
}
