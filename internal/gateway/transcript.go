package gateway

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// The Claude Code CLI stores every session as a JSONL transcript under
// ~/.claude/projects/<slug>/<session-id>.jsonl, where <slug> is the working
// directory with every non-alphanumeric character replaced by "-". This file
// reads those transcripts to power /resume (list & re-attach past sessions)
// and /export (deliver a readable Markdown transcript over QQ).

// projectSlug converts a working directory to the CLI's project-folder slug.
func projectSlug(workDir string) string {
	var b strings.Builder
	for _, r := range workDir {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}

// projectDir returns the CLI project directory for a working directory.
func projectDir(workDir string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/home/claude"
	}
	return filepath.Join(home, ".claude", "projects", projectSlug(workDir))
}

// sessionInfo is one past session, for the /resume listing.
type sessionInfo struct {
	ID       string
	Title    string // ai-title, else the first user prompt, else "(空会话)"
	Modified time.Time
}

// transcriptLine unions the JSONL fields we care about across line types.
type transcriptLine struct {
	Type        string          `json:"type"`
	AITitle     string          `json:"aiTitle"`
	IsSidechain bool            `json:"isSidechain"`
	Timestamp   string          `json:"timestamp"`
	Message     json.RawMessage `json:"message"`
}

// transcriptMessage is the message payload of a user/assistant line. Content is
// either a plain string or a list of typed blocks.
type transcriptMessage struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// contentBlock is one element of a block-style content array.
type contentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Name string `json:"name"` // tool_use
}

// newScanner returns a line scanner sized for transcript lines (tool results
// can be megabytes).
func newTranscriptScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	return sc
}

// messageText extracts the human-readable text of a user/assistant message:
// plain-string content verbatim; block content joins the text blocks. Tool
// results/uses yield "" here (tools are summarized separately).
func messageText(raw json.RawMessage) string {
	var m transcriptMessage
	if json.Unmarshal(raw, &m) != nil {
		return ""
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return strings.TrimSpace(s)
	}
	var blocks []contentBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		return ""
	}
	var parts []string
	for _, b := range blocks {
		if b.Type == "text" && strings.TrimSpace(b.Text) != "" {
			parts = append(parts, strings.TrimSpace(b.Text))
		}
	}
	return strings.Join(parts, "\n\n")
}

// toolUses lists the tool names invoked by an assistant message's content.
func toolUses(raw json.RawMessage) []string {
	var m transcriptMessage
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	var blocks []contentBlock
	if json.Unmarshal(m.Content, &blocks) != nil {
		return nil
	}
	var names []string
	for _, b := range blocks {
		if b.Type == "tool_use" && b.Name != "" {
			names = append(names, b.Name)
		}
	}
	return names
}

// sessionTitle scans a transcript for its best title: the last aiTitle line, or
// the first (non-sidechain) user prompt. Scanning is capped so a huge transcript
// can't stall the listing.
func sessionTitle(path string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := newTranscriptScanner(f)
	var title, firstUser string
	for lines := 0; sc.Scan() && lines < 4000; lines++ {
		var tl transcriptLine
		if json.Unmarshal(sc.Bytes(), &tl) != nil {
			continue
		}
		switch {
		case tl.Type == "ai-title" && tl.AITitle != "":
			title = tl.AITitle
		case tl.Type == "user" && firstUser == "" && !tl.IsSidechain:
			if t := messageText(tl.Message); t != "" {
				firstUser = firstLine(t)
			}
		}
	}
	if title != "" {
		return title
	}
	return firstUser
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// listSessions returns the most recently modified sessions of a project
// directory, newest first, with titles.
func listSessions(workDir string, limit int) ([]sessionInfo, error) {
	dir := projectDir(workDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var infos []sessionInfo
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		fi, err := e.Info()
		if err != nil {
			continue
		}
		infos = append(infos, sessionInfo{
			ID:       strings.TrimSuffix(e.Name(), ".jsonl"),
			Modified: fi.ModTime(),
		})
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Modified.After(infos[j].Modified) })
	if limit > 0 && len(infos) > limit {
		infos = infos[:limit]
	}
	for i := range infos {
		title := sessionTitle(filepath.Join(dir, infos[i].ID+".jsonl"))
		if title == "" {
			title = "(空会话)"
		}
		infos[i].Title = title
	}
	return infos, nil
}

// findSessionByPrefix resolves a session-id prefix in a project directory.
// Returns the full id, or "" when the prefix matches zero or several sessions.
func findSessionByPrefix(workDir, prefix string) string {
	entries, err := os.ReadDir(projectDir(workDir))
	if err != nil {
		return ""
	}
	var match string
	for _, e := range entries {
		name := strings.TrimSuffix(e.Name(), ".jsonl")
		if !strings.HasSuffix(e.Name(), ".jsonl") || !strings.HasPrefix(name, prefix) {
			continue
		}
		if match != "" {
			return "" // ambiguous
		}
		match = name
	}
	return match
}

// renderTranscript reads a session JSONL and renders a readable Markdown
// transcript (for /export). Returns the markdown and the number of exchanges
// (user + assistant messages) rendered. Sidechain (subagent) traffic is skipped
// — it is internal work, not the conversation.
func renderTranscript(path, sessionID string) (string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	var b strings.Builder
	fmt.Fprintf(&b, "# 对话导出 · %s\n\n- 导出时间：%s\n\n---\n", sessionID, time.Now().Format("2006-01-02 15:04:05"))

	count := 0
	var pendingTools []string
	flushTools := func() {
		if len(pendingTools) == 0 {
			return
		}
		fmt.Fprintf(&b, "\n> 🔧 %s\n", summarizeTools(pendingTools))
		pendingTools = nil
	}

	sc := newTranscriptScanner(f)
	for sc.Scan() {
		var tl transcriptLine
		if json.Unmarshal(sc.Bytes(), &tl) != nil {
			continue
		}
		if tl.IsSidechain || (tl.Type != "user" && tl.Type != "assistant") {
			continue
		}
		ts := ""
		if t, err := time.Parse(time.RFC3339Nano, tl.Timestamp); err == nil {
			ts = " · " + t.Local().Format("01-02 15:04")
		}
		switch tl.Type {
		case "user":
			text := messageText(tl.Message)
			if text == "" {
				continue // tool_result-only lines
			}
			flushTools()
			fmt.Fprintf(&b, "\n### 👤 操作者%s\n\n%s\n", ts, text)
			count++
		case "assistant":
			pendingTools = append(pendingTools, toolUses(tl.Message)...)
			text := messageText(tl.Message)
			if text == "" {
				continue
			}
			flushTools()
			fmt.Fprintf(&b, "\n### 🤖 Claude%s\n\n%s\n", ts, text)
			count++
		}
	}
	flushTools()
	if err := sc.Err(); err != nil {
		return "", 0, fmt.Errorf("scan transcript: %w", err)
	}
	return b.String(), count, nil
}

// summarizeTools renders a tool-name list as "Bash ×3, Read ×2" preserving
// first-use order.
func summarizeTools(names []string) string {
	counts := map[string]int{}
	var order []string
	for _, n := range names {
		if counts[n] == 0 {
			order = append(order, n)
		}
		counts[n]++
	}
	parts := make([]string, 0, len(order))
	for _, n := range order {
		if counts[n] > 1 {
			parts = append(parts, fmt.Sprintf("%s ×%d", n, counts[n]))
		} else {
			parts = append(parts, n)
		}
	}
	return strings.Join(parts, ", ")
}
