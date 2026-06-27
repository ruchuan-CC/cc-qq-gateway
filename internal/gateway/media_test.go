package gateway

import (
	"strings"
	"testing"

	"github.com/chenhg5/cc-qq-gateway/internal/qq"
)

func TestExtractSendDirectivesNone(t *testing.T) {
	in := "just a normal reply\nwith two lines"
	out, items := extractSendDirectives(in)
	if out != in || items != nil {
		t.Fatalf("expected passthrough, got %q / %#v", out, items)
	}
}

func TestExtractSendDirectives(t *testing.T) {
	in := "Here is your chart:\n@@QQ_IMAGE: /home/claude/out.png\nand a log file\n@@QQ_FILE: /tmp/log.txt\n@@QQ_IMAGE: https://example.com/a.jpg"
	out, items := extractSendDirectives(in)
	if strings.Contains(out, "@@QQ_") {
		t.Errorf("directives not stripped: %q", out)
	}
	if !strings.Contains(out, "Here is your chart") || !strings.Contains(out, "and a log file") {
		t.Errorf("body text lost: %q", out)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d: %#v", len(items), items)
	}
	if items[0].kind != qq.FileTypeImage || items[0].path != "/home/claude/out.png" {
		t.Errorf("item0 wrong: %#v", items[0])
	}
	if items[1].kind != qq.FileTypeFile || items[1].path != "/tmp/log.txt" {
		t.Errorf("item1 wrong: %#v", items[1])
	}
	if items[2].kind != qq.FileTypeImage || items[2].url != "https://example.com/a.jpg" || items[2].path != "" {
		t.Errorf("item2 should be a URL image: %#v", items[2])
	}
}

func TestNormalizeURL(t *testing.T) {
	cases := map[string]string{
		"http://x/y":      "http://x/y",
		"https://x/y":     "https://x/y",
		"gchat.qpic.cn/a": "https://gchat.qpic.cn/a",
		"":                "",
	}
	for in, want := range cases {
		if got := normalizeURL(in); got != want {
			t.Errorf("normalizeURL(%q)=%q want %q", in, got, want)
		}
	}
}

func TestSafeFilename(t *testing.T) {
	if got := safeFilename("../../etc/passwd", "", 0); strings.Contains(got, "/") || strings.Contains(got, "..") {
		t.Errorf("path traversal not neutralized: %q", got)
	}
	if got := safeFilename("", "image/png", 2); got != "attachment_3.png" {
		t.Errorf("png ext from content-type: got %q", got)
	}
	if got := safeFilename("", "image/jpeg", 0); !strings.HasSuffix(got, ".jpg") {
		t.Errorf("jpeg ext: got %q", got)
	}
}
