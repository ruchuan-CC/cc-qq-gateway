package claude

import "strings"

// NormalizeModel maps a human-typed model name to a value the Claude Code CLI's
// --model flag accepts, returning ok=false for input that clearly isn't a model.
//
// The CLI wants a short alias (opus / sonnet / haiku / fable / opusplan) or a
// concrete id (e.g. claude-opus-4-8, claude-opus-4-8[1m]). It does NOT accept the
// display names the Claude apps show ("Opus 4.8 (1M context)"): such a value comes
// back as an is_error 404 at exit code 0, which previously got stored on the
// session and wedged every following turn. So we translate the common display
// forms here and reject obviously-invalid free text instead of passing it through.
//
// An empty string (or default/reset) maps to "" = the CLI default.
func NormalizeModel(s string) (string, bool) {
	raw := strings.TrimSpace(s)
	if raw == "" {
		return "", true
	}
	low := collapseSpaces(strings.ToLower(fullwidthToASCII(raw)))

	switch low {
	case "default", "reset", "默认", "重置":
		return "", true
	case "opus", "sonnet", "haiku", "fable", "opusplan":
		return low, true
	}
	// An explicit concrete id — trust it verbatim (preserve case + any [1m] suffix).
	if strings.HasPrefix(low, "claude-") {
		return raw, true
	}

	is1m := strings.Contains(low, "1m") || strings.Contains(low, "百万") || strings.Contains(low, "1000k")
	switch {
	case strings.Contains(low, "opus"):
		if is1m {
			return "claude-opus-4-8[1m]", true
		}
		return "opus", true
	case strings.Contains(low, "sonnet"):
		return "sonnet", true
	case strings.Contains(low, "haiku"):
		return "haiku", true
	case strings.Contains(low, "fable"):
		return "fable", true
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
