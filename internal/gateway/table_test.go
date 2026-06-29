package gateway

import (
	"strings"
	"testing"
)

func TestDisplayWidth(t *testing.T) {
	cases := []struct {
		s    string
		want int
	}{
		{"", 0},
		{"abc", 3},
		{"中文", 4},            // 2 CJK glyphs × 2 cells
		{"a中b", 4},           // 1+2+1
		{"/sessions", 9},     // ASCII command
		{"CLAUDE.md", 9},     // ASCII
		{"生成 CLAUDE.md", 14}, // 生成(4) + space(1) + CLAUDE.md(9)
	}
	for _, c := range cases {
		if got := displayWidth(c.s); got != c.want {
			t.Errorf("displayWidth(%q) = %d, want %d", c.s, got, c.want)
		}
	}
}

// Every line of a rendered table (borders and rows alike) must have identical
// display width, including rows with mixed CJK/ASCII content — otherwise the
// monospace box looks ragged on QQ.
func TestRenderTableAligned(t *testing.T) {
	out := renderTable(
		[]string{"项", "值"},
		[][]string{
			{"会话", "已连接 · 4a1b6db7…"},
			{"模型", "claude-opus-4-8[1m]"},
			{"权限", "完全（无需确认）"},
		},
	)
	var widths []int
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "│") || strings.HasPrefix(line, "┌") ||
			strings.HasPrefix(line, "├") || strings.HasPrefix(line, "└") {
			widths = append(widths, displayWidth(line))
		}
	}
	if len(widths) < 5 { // top, header, mid, 3 rows, bottom
		t.Fatalf("expected a full framed table, got %d framed lines", len(widths))
	}
	for i, w := range widths {
		if w != widths[0] {
			t.Errorf("framed line %d width %d != %d", i, w, widths[0])
		}
	}
}

func TestRenderTableEmpty(t *testing.T) {
	if got := renderTable([]string{"a", "b"}, nil); got != "" {
		t.Errorf("renderTable with no rows = %q, want empty", got)
	}
}
