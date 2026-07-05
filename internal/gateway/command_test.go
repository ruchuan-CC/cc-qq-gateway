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
		"model": true, "effort": true, "think": true, "dir": true, "mode": true,
		"mcp": true, "doctor": true,
		"review": true, "diff": true, "explain": true, "web": true, "init": true,
		"status": true, "help": true,
		"whoami": true, "sessions": true, "version": true, "ping": true,
		"compact": true, "resume": true, "timeout": true, "usage": true,
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
		"/mcp", "/doctor", "/review", "/diff", "/explain", "/web", "/init",
		"/status", "/help", "/whoami", "/sessions", "/version",
		"/compact", "/resume", "/timeout", "/usage",
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
