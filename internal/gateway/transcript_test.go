package gateway

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const codexFixture = `{"timestamp":"2026-07-05T17:28:34.403Z","type":"session_meta","payload":{"session_id":"019f3353-22e9-7940-9fa9-199fe918d6d2","cwd":"/tmp/project","timestamp":"2026-07-05T17:28:34.294Z"}}
{"timestamp":"2026-07-05T17:28:35.138Z","type":"event_msg","payload":{"type":"user_message","message":"帮我看下磁盘\n第二行"}}
{"timestamp":"2026-07-05T17:28:37.016Z","type":"event_msg","payload":{"type":"agent_message","message":"ok"}}
`

func writeCodexFixture(t *testing.T, root string) string {
	t.Helper()
	dir := filepath.Join(root, "2026", "07", "05")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "rollout-2026-07-05T17-28-34-019f3353-22e9-7940-9fa9-199fe918d6d2.jsonl")
	if err := os.WriteFile(path, []byte(codexFixture), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadSessionInfo(t *testing.T) {
	path := writeCodexFixture(t, t.TempDir())
	info, ok := readSessionInfo(path, "/tmp/project")
	if !ok {
		t.Fatal("expected session info")
	}
	if info.ID != "019f3353-22e9-7940-9fa9-199fe918d6d2" {
		t.Errorf("id = %q", info.ID)
	}
	if info.Title != "帮我看下磁盘" {
		t.Errorf("title = %q", info.Title)
	}
	if info.Modified.IsZero() {
		t.Error("modified time should be set")
	}
}

func TestReadSessionInfoFiltersCWD(t *testing.T) {
	path := writeCodexFixture(t, t.TempDir())
	if _, ok := readSessionInfo(path, "/tmp/other"); ok {
		t.Fatal("session from another cwd should be filtered")
	}
}

func TestIDFromRolloutName(t *testing.T) {
	got := idFromRolloutName("rollout-2026-07-05T17-28-34-019f3353-22e9-7940-9fa9-199fe918d6d2.jsonl")
	if got != "019f3353-22e9-7940-9fa9-199fe918d6d2" {
		t.Errorf("id = %q", got)
	}
}

func TestFirstLine(t *testing.T) {
	if got := firstLine(" a\nb "); got != "a" {
		t.Errorf("firstLine = %q", got)
	}
	if strings.TrimSpace(firstLine("")) != "" {
		t.Error("empty firstLine should stay empty")
	}
}
