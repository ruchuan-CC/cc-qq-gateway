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
	s = strings.TrimSpace(s)
	// Chinese IMEs often emit a full-width slash; fold a leading one so "／help" is
	// still recognized as the "/help" command rather than silently sent to Claude.
	if strings.HasPrefix(s, "／") {
		s = "/" + strings.TrimPrefix(s, "／")
	}
	return s
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
	if maxRunes < 0 {
		maxRunes = 0
	}
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

// kvLines renders [label, value] pairs as a QQ Markdown unordered LIST with bold
// labels, e.g. "- **会话** 已连接". Per the QQ docs the supported syntax is
// headings/bold/italic/strikethrough/links/images/lists/quotes/dividers — NOT
// code blocks, inline code or tables — and a bare "\n" is not a reliable line
// break (needs a blank line, U+200B, or a list). A list gives each item its own
// line reliably and renders cleanly; it is the right primitive for structured data.
func kvLines(pairs [][2]string) string {
	var b strings.Builder
	for i, p := range pairs {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString("- **" + p[0] + "** " + p[1])
	}
	return b.String()
}

func short(s string) string {
	s = strings.TrimSpace(s)
	if runeLen(s) <= 120 {
		return s
	}
	return string([]rune(s)[:120]) + "…"
}
