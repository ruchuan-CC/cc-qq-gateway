package gateway

import "testing"

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

// The /help button grid must stay within QQ's keyboard limits (≤5 rows, ≤5
// buttons per row) and every button must auto-send a real command.
func TestHelpKeyboardWithinLimits(t *testing.T) {
	kb := buildHelpKeyboard()
	rows := kb.Content.Rows
	if len(rows) > 5 {
		t.Fatalf("keyboard has %d rows, QQ allows at most 5", len(rows))
	}
	total := 0
	for _, g := range helpGroups {
		total += len(g.cmds)
	}
	got := 0
	for _, row := range rows {
		if len(row.Buttons) > 5 {
			t.Errorf("a row has %d buttons, QQ allows at most 5", len(row.Buttons))
		}
		for _, b := range row.Buttons {
			got++
			if _, ok := commandAliases[b.Action.Data]; !ok {
				t.Errorf("button data %q is not a real command", b.Action.Data)
			}
			if b.Action.Type != 2 || !b.Action.Enter {
				t.Errorf("button %q should auto-send (type 2 + enter)", b.Action.Data)
			}
		}
	}
	if got != total {
		t.Errorf("keyboard has %d buttons, want %d (one per command)", got, total)
	}
}
