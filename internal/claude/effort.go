package claude

import "strings"

// validEfforts maps human-typed effort tokens to the value the Claude Code CLI's
// --effort flag accepts. The empty value means "clear the override / CLI default".
var validEfforts = map[string]string{
	"low":     "low",
	"medium":  "medium",
	"med":     "medium",
	"high":    "high",
	"xhigh":   "xhigh",
	"x-high":  "xhigh",
	"extra":   "xhigh",
	"max":     "max",
	"default": "",
	"":        "",
	"默认":      "",
}

// EffortLevels lists the selectable effort levels, low→high, for help text.
var EffortLevels = []string{"low", "medium", "high", "xhigh", "max"}

// NormalizeEffort maps a human-typed effort level to a value --effort accepts,
// returning ok=false for input that isn't a recognized level. An empty return
// with ok=true means "clear the override" (default / max).
func NormalizeEffort(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	v, ok := validEfforts[s]
	return v, ok
}
