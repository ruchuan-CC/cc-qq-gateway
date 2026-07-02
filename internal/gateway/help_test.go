package gateway

import (
	"strings"
	"testing"
)

// Every command listed in /help must be a real, dispatchable alias so /help never
// advertises a command that does nothing.
func TestHelpCommandsAreReal(t *testing.T) {
	for _, g := range helpGroups {
		for _, c := range g.cmds {
			if _, ok := commandAliases[c.cmd]; !ok {
				t.Errorf("/help lists %q (group %q) but it is not in commandAliases", c.cmd, g.title)
			}
		}
	}
}

// Both /help views must name every grouped command (and its group label) so
// neither menu silently drops a command.
func TestHelpTextListsEveryCommand(t *testing.T) {
	for name, out := range map[string]string{"compact": helpText, "full": helpFullText} {
		for _, g := range helpGroups {
			if !strings.Contains(out, g.title) {
				t.Errorf("%s help missing group label %q", name, g.title)
			}
			for _, c := range g.cmds {
				if !strings.Contains(out, c.cmd) {
					t.Errorf("%s help missing command %q", name, c.cmd)
				}
			}
		}
	}
}

// The compact view is the default for a reason: it must stay one-line-per-group
// short (a QQ screen), and each view must advertise how to reach the other.
func TestHelpCompactStaysCompact(t *testing.T) {
	if n := strings.Count(helpText, "\n- "); n != len(helpGroups) {
		t.Errorf("compact help has %d list lines, want exactly %d (one per group)", n, len(helpGroups))
	}
	if !strings.Contains(helpText, "/help all") {
		t.Error("compact help must point to /help all")
	}
	if !strings.Contains(helpFullText, "/help") {
		t.Error("full help must point back to /help")
	}
}
