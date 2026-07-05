package codex

import "strings"

// NormalizeModel maps a human-typed model name to a value the Codex CLI's
// --model flag accepts, returning ok=false for input that clearly isn't a model.
// The accepted model set changes over time, so this only normalizes common
// shorthand and rejects multi-word prose that would wedge later turns.
func NormalizeModel(s string) (string, bool) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return "", true
	}
	low := collapseSpaces(strings.ToLower(fullwidthToASCII(raw)))

	switch low {
	case "default", "reset", "默认", "重置":
		return "", true
	}
	if isRetiredClaudeModel(low) {
		return "", false
	}
	if strings.HasPrefix(low, "gpt ") {
		return "gpt-" + strings.TrimSpace(strings.TrimPrefix(low, "gpt ")), true
	}
	if strings.HasPrefix(low, "gpt") && len(low) > 3 && low[3] >= '0' && low[3] <= '9' {
		return "gpt-" + low[3:], true
	}
	// A single bare token we don't recognize might be a new alias/id the CLI knows —
	// let it try, but only if it LOOKS like a model id (ASCII letters/digits/.-_). A
	// stray word like "列表" (from "model 列表") or multi-word display text is not a
	// model, so reject it with guidance rather than storing it and wedging the next turn.
	if isModelToken(low) {
		return raw, true
	}
	return "", false
}

// isModelToken reports whether s is shaped like a model id/alias: a single token of
// ASCII letters, digits, dot, dash or underscore (no spaces, parens, or non-ASCII).
func isModelToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// fullwidthToASCII folds the full-width punctuation/space that Chinese IMEs emit
// (（ ） and the full-width space) to their ASCII equivalents so "（1M context）"
// matches the same way "(1m context)" does.
func fullwidthToASCII(s string) string {
	r := strings.NewReplacer(
		"（", "(", "）", ")", "　", " ", "［", "[", "］", "]",
	)
	return r.Replace(s)
}

func collapseSpaces(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

func isRetiredClaudeModel(s string) bool {
	for _, marker := range []string{"opus", "sonnet", "haiku", "fable"} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}
