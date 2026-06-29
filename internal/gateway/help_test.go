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

// The /help ARK card must contain one list row per real command.
func TestHelpArkListsAllCommands(t *testing.T) {
	ark := buildHelpArk()
	if ark.TemplateID != 23 {
		t.Fatalf("help ARK template = %d, want 23", ark.TemplateID)
	}
	var list []string
	for _, kv := range ark.KV {
		if kv.Key == "#LIST#" {
			for _, row := range kv.Obj {
				if len(row.ObjKV) > 0 {
					list = append(list, row.ObjKV[0].Value)
				}
			}
		}
	}
	total := 0
	for _, g := range helpGroups {
		total += len(g.cmds)
	}
	if len(list) != total {
		t.Fatalf("help ARK has %d rows, want %d", len(list), total)
	}
}
