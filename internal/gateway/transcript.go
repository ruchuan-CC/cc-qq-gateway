package gateway

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Codex stores exec sessions as JSONL rollout files under
// ~/.codex/sessions/YYYY/MM/DD. The gateway reads only lightweight metadata for
// /resume: thread id, cwd, first user prompt, and modification time.

// sessionInfo is one past session, for the /resume listing.
type sessionInfo struct {
	ID       string
	Title    string
	Modified time.Time
}

type codexLine struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

type codexSessionMeta struct {
	SessionID string `json:"session_id"`
	ID        string `json:"id"`
	CWD       string `json:"cwd"`
	Timestamp string `json:"timestamp"`
}

type codexEventMsg struct {
	Type    string `json:"type"`
	Message string `json:"message"`
}

func codexSessionsRoot() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/home/codex"
	}
	if ch := strings.TrimSpace(os.Getenv("CODEX_HOME")); ch != "" {
		return filepath.Join(ch, "sessions")
	}
	return filepath.Join(home, ".codex", "sessions")
}

func effectiveWorkDir(workDir string) string {
	if strings.TrimSpace(workDir) != "" {
		return filepath.Clean(workDir)
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		return filepath.Clean(cwd)
	}
	return ""
}

func newCodexScanner(f *os.File) *bufio.Scanner {
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	return sc
}

func readSessionInfo(path, wantWorkDir string) (sessionInfo, bool) {
	fi, err := os.Stat(path)
	if err != nil {
		return sessionInfo{}, false
	}
	info := sessionInfo{ID: idFromRolloutName(path), Modified: fi.ModTime()}

	f, err := os.Open(path)
	if err != nil {
		return sessionInfo{}, false
	}
	defer f.Close()

	sc := newCodexScanner(f)
	var cwd string
	for lines := 0; sc.Scan() && lines < 800; lines++ {
		var line codexLine
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		switch line.Type {
		case "session_meta":
			var meta codexSessionMeta
			if json.Unmarshal(line.Payload, &meta) == nil {
				if meta.SessionID != "" {
					info.ID = meta.SessionID
				} else if meta.ID != "" {
					info.ID = meta.ID
				}
				cwd = filepath.Clean(meta.CWD)
				if t, err := time.Parse(time.RFC3339Nano, meta.Timestamp); err == nil {
					info.Modified = t
				}
			}
		case "event_msg":
			if info.Title != "" {
				continue
			}
			var ev codexEventMsg
			if json.Unmarshal(line.Payload, &ev) == nil && ev.Type == "user_message" {
				info.Title = firstLine(ev.Message)
			}
		}
	}
	if info.ID == "" {
		return sessionInfo{}, false
	}
	if wantWorkDir != "" && cwd != "" && filepath.Clean(cwd) != wantWorkDir {
		return sessionInfo{}, false
	}
	if info.Title == "" {
		info.Title = "(空会话)"
	}
	return info, true
}

func idFromRolloutName(path string) string {
	base := strings.TrimSuffix(filepath.Base(path), ".jsonl")
	if len(base) < 36 {
		return ""
	}
	cand := base[len(base)-36:]
	if strings.Count(cand, "-") == 4 {
		return cand
	}
	return ""
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

// listSessions returns the most recently modified Codex sessions for a working
// directory, newest first.
func listSessions(workDir string, limit int) ([]sessionInfo, error) {
	root := codexSessionsRoot()
	wantWorkDir := effectiveWorkDir(workDir)
	var infos []sessionInfo
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(d.Name(), ".jsonl") {
			return nil
		}
		if info, ok := readSessionInfo(path, wantWorkDir); ok {
			infos = append(infos, info)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].Modified.After(infos[j].Modified) })
	if limit > 0 && len(infos) > limit {
		infos = infos[:limit]
	}
	return infos, nil
}

// findSessionByPrefix resolves a Codex thread-id prefix. Returns the full id, or
// "" when the prefix matches zero or several sessions for the working directory.
func findSessionByPrefix(workDir, prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return ""
	}
	infos, err := listSessions(workDir, 0)
	if err != nil {
		return ""
	}
	var match string
	for _, in := range infos {
		if !strings.HasPrefix(in.ID, prefix) {
			continue
		}
		if match != "" {
			return ""
		}
		match = in.ID
	}
	return match
}

func sessionTitleByID(workDir, id string) string {
	infos, err := listSessions(workDir, 0)
	if err != nil {
		return ""
	}
	for _, in := range infos {
		if in.ID == id {
			return in.Title
		}
	}
	return ""
}
