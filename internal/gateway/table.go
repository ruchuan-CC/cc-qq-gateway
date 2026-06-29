package gateway

import "strings"

// QQ does not render Markdown pipe tables, but it does render fenced code blocks
// in a monospace font. So every "table" the bot sends is a box-drawn ASCII table
// inside a code block: it actually looks like a table on QQ and degrades to
// still-readable framed text if markdown falls back to plain. Columns are padded
// by display width (CJK = 2 cells, see padDisplay) so Chinese and ASCII align.
// Keep emoji OUT of table cells — their rendered width is renderer-dependent and
// would misalign columns; put emoji in the bold header/intro outside the fence.

// colWidths returns the display width of each column, sized to the widest cell
// (header or body) in that column. The column count is the widest of the header
// and any row, so it works with nil headers (headerless KV tables).
func colWidths(headers []string, rows [][]string) []int {
	n := len(headers)
	for _, row := range rows {
		if len(row) > n {
			n = len(row)
		}
	}
	w := make([]int, n)
	for i, h := range headers {
		w[i] = displayWidth(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if cw := displayWidth(cell); cw > w[i] {
				w[i] = cw
			}
		}
	}
	return w
}

// tableRule draws a horizontal border (e.g. "┌──┬──┐") for the given widths.
func tableRule(w []int, left, mid, right string) string {
	var b strings.Builder
	b.WriteString(left)
	for i, cw := range w {
		if i > 0 {
			b.WriteString(mid)
		}
		b.WriteString(strings.Repeat("─", cw+2))
	}
	b.WriteString(right)
	return b.String()
}

// tableRow draws one "│ a │ b │" data row, padding each cell to its column width.
func tableRow(cells []string, w []int) string {
	var b strings.Builder
	b.WriteString("│")
	for i, cw := range w {
		cell := ""
		if i < len(cells) {
			cell = cells[i]
		}
		b.WriteString(" " + padDisplay(cell, cw) + " │")
	}
	return b.String()
}

// renderKV renders rows as a headerless box table (a clean key→value list) inside
// a code fence. Used by the "show current value" command views. Returns "" if
// there are no rows.
func renderKV(rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	w := colWidths(nil, rows)
	var b strings.Builder
	b.WriteString("```\n")
	b.WriteString(tableRule(w, "┌", "┬", "┐") + "\n")
	for _, row := range rows {
		b.WriteString(tableRow(row, w) + "\n")
	}
	b.WriteString(tableRule(w, "└", "┴", "┘") + "\n")
	b.WriteString("```")
	return b.String()
}

// renderTable renders headers + rows as a monospace box table wrapped in a code
// fence, ready to send to QQ. Returns "" if there are no rows.
func renderTable(headers []string, rows [][]string) string {
	if len(rows) == 0 {
		return ""
	}
	w := colWidths(headers, rows)
	var b strings.Builder
	b.WriteString("```\n")
	b.WriteString(tableRule(w, "┌", "┬", "┐") + "\n")
	b.WriteString(tableRow(headers, w) + "\n")
	b.WriteString(tableRule(w, "├", "┼", "┤") + "\n")
	for _, row := range rows {
		b.WriteString(tableRow(row, w) + "\n")
	}
	b.WriteString(tableRule(w, "└", "┴", "┘") + "\n")
	b.WriteString("```")
	return b.String()
}
