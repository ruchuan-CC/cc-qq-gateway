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

// helpText is the single inline message /help sends: it must name every grouped
// command (and its group label) so the rendered menu is complete.
func TestHelpTextListsEveryCommand(t *testing.T) {
	out := helpText
	for _, g := range helpGroups {
		if !strings.Contains(out, g.title) {
			t.Errorf("help text missing group label %q", g.title)
		}
		for _, c := range g.cmds {
			if !strings.Contains(out, c.cmd) {
				t.Errorf("help text missing command %q", c.cmd)
			}
		}
	}
}
