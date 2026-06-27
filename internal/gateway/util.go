package gateway

import (
	"regexp"
	"strings"
)

// mentionRe matches QQ inline markup like <@!123>, <@123>, <#456>, <emoji:1>.
var mentionRe = regexp.MustCompile(`<@!?\d+>|<#\d+>|<emoji:\d+>`)

// cleanContent strips bot mention markup and trims whitespace from inbound text.
func cleanContent(s string) string {
	s = mentionRe.ReplaceAllString(s, "")
	return strings.TrimSpace(s)
}

// splitMessage breaks text into chunks of at most maxRunes runes, preferring to
// split on paragraph and line boundaries.
func splitMessage(text string, maxRunes int) []string {
	if maxRunes <= 0 {
		maxRunes = 1800
	}
	text = strings.TrimSpace(text)
	if runeLen(text) <= maxRunes {
		return []string{text}
	}

	var chunks []string
	var cur strings.Builder
	curLen := 0

	flush := func() {
		if curLen > 0 {
			chunks = append(chunks, strings.TrimRight(cur.String(), "\n"))
			cur.Reset()
			curLen = 0
		}
	}

	for _, line := range strings.Split(text, "\n") {
		lineLen := runeLen(line)
		// A single overlong line: hard-split it.
		if lineLen > maxRunes {
			flush()
			for _, piece := range hardSplit(line, maxRunes) {
				chunks = append(chunks, piece)
			}
			continue
		}
		if curLen+lineLen+1 > maxRunes {
			flush()
		}
		cur.WriteString(line)
		cur.WriteString("\n")
		curLen += lineLen + 1
	}
	flush()
	return chunks
}

func hardSplit(s string, maxRunes int) []string {
	runes := []rune(s)
	var out []string
	for len(runes) > 0 {
		n := maxRunes
		if n > len(runes) {
			n = len(runes)
		}
		out = append(out, string(runes[:n]))
		runes = runes[n:]
	}
	return out
}

func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if maxRunes < 4 {
		return string(runes[:maxRunes])
	}
	return string(runes[:maxRunes-1]) + "…"
}

func runeLen(s string) int { return len([]rune(s)) }

func short(s string) string {
	s = strings.TrimSpace(s)
	if runeLen(s) <= 120 {
		return s
	}
	return string([]rune(s)[:120]) + "…"
}
