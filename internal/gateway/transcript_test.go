package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSlug(t *testing.T) {
	for in, want := range map[string]string{
		"/home/claude":                   "-home-claude",
		"/home/claude/anything/OkxForAI": "-home-claude-anything-OkxForAI",
		"/tmp/a.b_c":                     "-tmp-a-b-c",
	} {
		if got := projectSlug(in); got != want {
			t.Errorf("projectSlug(%q) = %q, want %q", in, got, want)
		}
	}
}

// fixture is a minimal CLI transcript: a user prompt, an assistant tool_use, a
// tool_result-only user line, a sidechain line, an ai-title, and the reply.
const fixture = `{"type":"user","isSidechain":false,"timestamp":"2026-07-02T03:00:00.000Z","message":{"role":"user","content":"帮我看下磁盘"}}
{"type":"assistant","isSidechain":false,"timestamp":"2026-07-02T03:00:05.000Z","message":{"role":"assistant","content":[{"type":"tool_use","name":"Bash","input":{}},{"type":"tool_use","name":"Bash","input":{}}]}}
{"type":"user","isSidechain":false,"timestamp":"2026-07-02T03:00:06.000Z","message":{"role":"user","content":[{"type":"tool_result","content":"..."}]}}
{"type":"assistant","isSidechain":true,"timestamp":"2026-07-02T03:00:07.000Z","message":{"role":"assistant","content":[{"type":"text","text":"sidechain noise"}]}}
{"type":"ai-title","aiTitle":"磁盘检查"}
{"type":"assistant","isSidechain":false,"timestamp":"2026-07-02T03:00:09.000Z","message":{"role":"assistant","content":[{"type":"text","text":"磁盘使用 26%，很健康。"}]}}
`

func writeFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "abcd1234-0000.jsonl")
	if err := os.WriteFile(path, []byte(fixture), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestRenderTranscript(t *testing.T) {
	md, count, err := renderTranscript(writeFixture(t), "abcd1234-0000")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Errorf("count = %d, want 2 (user + assistant)", count)
	}
	for _, want := range []string{"帮我看下磁盘", "磁盘使用 26%", "Bash ×2", "👤", "🤖"} {
		if !strings.Contains(md, want) {
			t.Errorf("transcript missing %q", want)
		}
	}
	if strings.Contains(md, "sidechain noise") {
		t.Error("sidechain content must be excluded")
	}
}

func TestSessionTitlePrefersAITitle(t *testing.T) {
	if got := sessionTitle(writeFixture(t)); got != "磁盘检查" {
		t.Errorf("title = %q, want 磁盘检查", got)
	}
}

func TestSummarizeTools(t *testing.T) {
	got := summarizeTools([]string{"Bash", "Read", "Bash", "Bash"})
	if got != "Bash ×3, Read" {
		t.Errorf("summarizeTools = %q", got)
	}
}
