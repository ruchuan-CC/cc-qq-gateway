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

// The /help table image must be built from every real command, in a 4-column
// (命令|说明|命令|说明) layout.
func TestHelpTableData(t *testing.T) {
	headers, rows := helpTableData()
	if len(headers) != 4 {
		t.Fatalf("help table has %d columns, want 4", len(headers))
	}
	total := 0
	for _, g := range helpGroups {
		total += len(g.cmds)
	}
	cells := 0
	for _, row := range rows {
		for _, c := range []string{row[0], row[2]} { // the two command columns
			if c == "" {
				continue
			}
			cells++
			if _, ok := commandAliases[c]; !ok {
				t.Errorf("help table lists %q but it is not a real command", c)
			}
		}
	}
	if cells != total {
		t.Errorf("help table has %d commands, want %d", cells, total)
	}
}
