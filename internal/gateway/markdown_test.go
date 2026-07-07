package gateway

import (
	"strings"
	"testing"
)

func TestQQMarkdownRemovesFencedCodeBlocks(t *testing.T) {
	in := "说明\n```go\nfmt.Println(\"hi\")\n```\n结束"

	got := qqMarkdown(in)

	if strings.Contains(got, "```") {
		t.Fatalf("markdown still contains fenced code marker: %q", got)
	}
	if !strings.Contains(got, "代码：") || !strings.Contains(got, "> fmt.Println") {
		t.Fatalf("markdown did not convert code block to quoted text: %q", got)
	}
}

func TestQQMarkdownConvertsTablesToLists(t *testing.T) {
	in := "| 项 | 值 |\n| --- | --- |\n| 模型 | gpt-5.5 |"

	got := qqMarkdown(in)

	if strings.Contains(got, "| --- |") {
		t.Fatalf("markdown still contains table separator: %q", got)
	}
	if !strings.Contains(got, "- 项 / 值") || !strings.Contains(got, "- 模型 / gpt-5.5") {
		t.Fatalf("markdown did not convert table rows to lists: %q", got)
	}
}
