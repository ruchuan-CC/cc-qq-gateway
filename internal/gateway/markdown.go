package gateway

import "strings"

// qqMarkdown 将 Codex 常见 Markdown 降级到 QQ 官方 Markdown 更稳的子集。
// QQ 文档明确支持标题、列表、引用、链接等；代码围栏和 GFM 表格在 C2C 里不应直接依赖。
func qqMarkdown(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")

	var out []markdownLine
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if !inFence {
				out = append(out, markdownLine{text: "代码：", kind: markdownParagraph})
			}
			inFence = !inFence
			continue
		}
		if inFence {
			if trimmed == "" {
				out = append(out, markdownLine{text: "> ", kind: markdownQuote})
			} else {
				out = append(out, markdownLine{text: "> " + line, kind: markdownQuote})
			}
			continue
		}
		if isMarkdownTableSeparator(trimmed) {
			continue
		}
		if looksLikeMarkdownTableRow(trimmed) {
			out = append(out, markdownLine{text: "- " + strings.Join(tableCells(trimmed), " / "), kind: markdownList})
			continue
		}
		out = append(out, markdownLine{text: line, kind: lineKind(trimmed)})
	}
	return renderMarkdownLines(out)
}

type markdownLineKind int

const (
	markdownBlank markdownLineKind = iota
	markdownParagraph
	markdownHeading
	markdownList
	markdownQuote
	markdownRule
)

type markdownLine struct {
	text string
	kind markdownLineKind
}

func lineKind(trimmed string) markdownLineKind {
	switch {
	case trimmed == "":
		return markdownBlank
	case strings.HasPrefix(trimmed, "#"):
		return markdownHeading
	case strings.HasPrefix(trimmed, "- "), strings.HasPrefix(trimmed, "* "), isOrderedList(trimmed):
		return markdownList
	case strings.HasPrefix(trimmed, ">"):
		return markdownQuote
	case trimmed == "***", trimmed == "---":
		return markdownRule
	default:
		return markdownParagraph
	}
}

func renderMarkdownLines(lines []markdownLine) string {
	var b strings.Builder
	var prev markdownLineKind
	wrote := false
	for _, line := range lines {
		if line.kind == markdownBlank {
			if wrote && !strings.HasSuffix(b.String(), "\n\n") {
				b.WriteString("\n\n")
			}
			prev = markdownBlank
			continue
		}
		if wrote {
			if needsQQBlankLine(prev, line.kind) {
				b.WriteString("\n\n")
			} else {
				b.WriteByte('\n')
			}
		}
		b.WriteString(line.text)
		wrote = true
		prev = line.kind
	}
	return strings.TrimSpace(b.String())
}

func needsQQBlankLine(prev, cur markdownLineKind) bool {
	if prev == markdownBlank {
		return false
	}
	if prev == markdownList && cur == markdownList {
		return false
	}
	if prev == markdownQuote && cur == markdownQuote {
		return false
	}
	return true
}

func isOrderedList(line string) bool {
	dot := strings.IndexByte(line, '.')
	if dot <= 0 || dot+1 >= len(line) || line[dot+1] != ' ' {
		return false
	}
	for _, r := range line[:dot] {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func looksLikeMarkdownTableRow(line string) bool {
	if !strings.Contains(line, "|") {
		return false
	}
	if strings.Contains(line, "://") {
		return false
	}
	return len(tableCells(line)) > 1
}

func isMarkdownTableSeparator(line string) bool {
	if !strings.Contains(line, "|") {
		return false
	}
	cells := tableCells(line)
	if len(cells) == 0 {
		return false
	}
	for _, cell := range cells {
		cell = strings.Trim(cell, ":- ")
		if cell != "" {
			return false
		}
	}
	return true
}

func tableCells(line string) []string {
	parts := strings.Split(strings.Trim(line, "|"), "|")
	cells := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			cells = append(cells, p)
		}
	}
	return cells
}
