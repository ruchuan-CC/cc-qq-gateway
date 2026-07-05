package codex

import "strings"

// validEfforts maps human-typed effort tokens to Codex's model_reasoning_effort
// config value. The empty value means "clear the override / CLI default".
var validEfforts = map[string]string{
	"minimal": "minimal",
	"min":     "minimal",
	"low":     "low",
	"medium":  "medium",
	"med":     "medium",
	"high":    "high",
	"xhigh":   "xhigh",
	"x-high":  "xhigh",
	"extra":   "xhigh",
	"max":     "xhigh",
	"default": "",
	"":        "",
	"默认":      "",
}

// EffortLevels lists the selectable effort levels, low→high, for help text.
var EffortLevels = []string{"minimal", "low", "medium", "high", "xhigh"}

// NormalizeEffort maps a human-typed effort level to a Codex effort value,
// returning ok=false for input that isn't recognized. An empty return with
// ok=true means "clear the override".
func NormalizeEffort(s string) (string, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	v, ok := validEfforts[s]
	return v, ok
}
