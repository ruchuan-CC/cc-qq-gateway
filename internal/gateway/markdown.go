package gateway

import "strings"

// qqMarkdown 将 Codex 常见 Markdown 降级到 QQ 官方 Markdown 更稳的子集。
// QQ 文档明确支持标题、列表、引用、链接等；代码围栏和 GFM 表格在 C2C 里不应直接依赖。
func qqMarkdown(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")

	var out []string
	inFence := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			if !inFence {
				out = append(out, "代码：")
			}
			inFence = !inFence
			continue
		}
		if inFence {
			if trimmed == "" {
				out = append(out, "> ")
			} else {
				out = append(out, "> "+line)
			}
			continue
		}
		if isMarkdownTableSeparator(trimmed) {
			continue
		}
		if looksLikeMarkdownTableRow(trimmed) {
			out = append(out, "- "+strings.Join(tableCells(trimmed), " / "))
			continue
		}
		out = append(out, line)
	}
	return strings.TrimSpace(strings.Join(out, "\n"))
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
