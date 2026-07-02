package gateway

import (
	"strings"
	"testing"
)

// TestCommandAliasesResolveToKnownHandlers ensures every alias maps to a
// canonical name that handleCommand actually switches on.
func TestCommandAliasesResolveToKnownHandlers(t *testing.T) {
	known := map[string]bool{
		"new": true, "retry": true, "stop": true,
		"model": true, "think": true, "dir": true, "mode": true,
		"agents": true, "mcp": true, "memory": true, "doctor": true,
		"review": true, "diff": true, "explain": true, "web": true, "init": true,
		"usage": true, "cost": true, "status": true, "help": true,
		"whoami": true, "sessions": true, "version": true, "ping": true,
		"compact": true, "export": true, "resume": true, "timeout": true,
	}
	seen := map[string]bool{}
	for alias, canon := range commandAliases {
		if !known[canon] {
			t.Errorf("alias %q maps to unknown canonical %q", alias, canon)
		}
		seen[canon] = true
	}
	for c := range known {
		if !seen[c] {
			t.Errorf("canonical command %q has no alias", c)
		}
	}
}

// TestHelpTextListsCoreCommands guards against documentation drift.
func TestHelpTextListsCoreCommands(t *testing.T) {
	for _, want := range []string{
		"/new", "/retry", "/stop", "/model", "/think", "/dir", "/mode",
		"/agents", "/mcp", "/memory", "/doctor", "/review", "/diff", "/explain", "/web", "/init",
		"/usage", "/cost", "/status", "/help", "/whoami", "/sessions", "/version",
		"/compact", "/export", "/resume", "/timeout",
	} {
		if !strings.Contains(helpText, want) {
			t.Errorf("helpText missing %q", want)
		}
	}
}

// TestEnglishAliasesAreLowercase ensures the lookup (which lowercases the token)
// can match every English alias.
func TestEnglishAliasesAreLowercase(t *testing.T) {
	for alias := range commandAliases {
		if strings.HasPrefix(alias, "/") && alias != strings.ToLower(alias) {
			t.Errorf("alias %q must be lowercase to match", alias)
		}
	}
}
