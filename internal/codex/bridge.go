// Package codex bridges the gateway to a locally installed Codex CLI.
// Every turn is executed through `codex exec --json`.
package codex

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// ErrTurnTimeout marks a turn that was killed by the per-turn timeout
// (Config.Timeout), as opposed to a crash or a user cancel. Callers detect it
// with errors.Is to tell the user the task hit its time limit instead of showing
// a raw "signal: killed".
var ErrTurnTimeout = errors.New("codex turn timed out")

// Config configures how the local Codex CLI is invoked.
type Config struct {
	// Binary is the path to the codex executable (default "codex").
	Binary string
	// WorkDir is the working directory Codex runs in.
	WorkDir string
	// Model overrides the model; empty = CLI/config default.
	Model string
	// Effort sets model_reasoning_effort (minimal/low/medium/high/xhigh).
	Effort string
	// PermissionMode is the legacy gateway mode:
	// default | plan | acceptEdits | bypassPermissions.
	PermissionMode string
	// Sandbox is the Codex sandbox mode (read-only/workspace-write/danger-full-access).
	Sandbox string
	// ApprovalPolicy is the Codex approval policy (never/on-request/on-failure/untrusted).
	ApprovalPolicy string
	// DangerouslySkipPermissions maps to --dangerously-bypass-approvals-and-sandbox.
	DangerouslySkipPermissions bool
	// WebSearch enables Codex's native web_search tool.
	WebSearch bool
	// AppendSystemPrompt is prepended to each prompt because Codex exec has no
	// append-system-prompt flag.
	AppendSystemPrompt string
	// AddDirs are extra directories Codex may access/write (--add-dir).
	AddDirs []string
	// ExtraArgs are appended before the exec subcommand.
	ExtraArgs []string
	// Timeout bounds a single turn (default 5m).
	Timeout time.Duration
}

// Bridge runs Codex CLI turns.
type Bridge struct {
	cfg Config
}

// New creates a Bridge.
func New(cfg Config) *Bridge {
	if cfg.Binary == "" {
		cfg.Binary = "codex"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Minute
	}
	if cfg.ApprovalPolicy == "" {
		cfg.ApprovalPolicy = "never"
	}
	if cfg.Sandbox == "" {
		cfg.Sandbox = "read-only"
	}
	return &Bridge{cfg: cfg}
}

// Result is the outcome of a single Codex turn.
type Result struct {
	Text                  string
	SessionID             string
	IsError               bool
	NumTurns              int
	DurationMS            int
	InputTokens           int
	CachedInputTokens     int
	OutputTokens          int
	ReasoningOutputTokens int
	TotalTokens           int
}

// Request is a single Codex turn. SessionID, when set, resumes an existing
// thread.
type Request struct {
	SessionID string
	Prompt    string
	// Model overrides Config.Model for this single turn.
	Model string
	// Permissions is the QQ command vocabulary:
	// read-only | workspace-write | full-access. Empty = Config defaults.
	Permissions string
	// Goal is a QQ-side session goal prepended to this turn's prompt.
	Goal string
	// PlanOnly forces a read-only planning turn without changing stored permissions.
	PlanOnly bool
	// OnActivity is called with a short label each time the Codex JSON stream
	// reports visible tool progress.
	OnActivity func(label string)
}

type streamEvent struct {
	Type     string          `json:"type"`
	ThreadID string          `json:"thread_id"`
	Item     *streamItem     `json:"item"`
	Usage    *tokenUsage     `json:"usage"`
	Message  string          `json:"message"`
	Error    json.RawMessage `json:"error"`
}

type streamItem struct {
	Type    string `json:"type"`
	Text    string `json:"text"`
	Name    string `json:"name"`
	Command string `json:"command"`
}

type tokenUsage struct {
	InputTokens           int `json:"input_tokens"`
	CachedInputTokens     int `json:"cached_input_tokens"`
	OutputTokens          int `json:"output_tokens"`
	ReasoningOutputTokens int `json:"reasoning_output_tokens"`
	TotalTokens           int `json:"total_tokens"`
}

// Run executes one turn. If req.SessionID is non-empty the thread is resumed;
// otherwise a new thread is started. The new/continued thread id is returned in
// Result.SessionID.
func (b *Bridge) Run(ctx context.Context, req Request) (*Result, error) {
	timeout := b.cfg.Timeout
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	workDir := b.cfg.WorkDir

	args := b.execArgs(req)
	cmd := exec.CommandContext(ctx, b.cfg.Binary, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	cmd.WaitDelay = 5 * time.Second
	cmd.Stdin = strings.NewReader(b.prompt(req))

	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("codex stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	started := time.Now()
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("codex start failed: %w (stderr: %s)", err, truncate(stderr.String(), 300))
	}

	result, sessionID, fallback, scanErr := consumeStream(stdoutPipe, req.OnActivity)
	waitErr := cmd.Wait()
	if result != nil {
		if result.SessionID == "" {
			result.SessionID = sessionID
		}
		if result.DurationMS == 0 {
			result.DurationMS = int(time.Since(started) / time.Millisecond)
		}
		return result, nil
	}

	if r, perr := parseResult(fallback); perr == nil && r.Text != "" {
		if r.SessionID == "" {
			r.SessionID = sessionID
		}
		if r.DurationMS == 0 {
			r.DurationMS = int(time.Since(started) / time.Millisecond)
		}
		return r, nil
	}
	remnant := &Result{SessionID: sessionID, DurationMS: int(time.Since(started) / time.Millisecond)}
	if ctx.Err() == context.DeadlineExceeded {
		return remnant, fmt.Errorf("%w after %s", ErrTurnTimeout, timeout)
	}
	if scanErr != nil {
		return remnant, fmt.Errorf("codex output stream error: %w (stderr: %s)", scanErr, truncate(stderr.String(), 300))
	}
	if waitErr != nil {
		return remnant, fmt.Errorf("codex run failed: %w (stderr: %s)", waitErr, truncate(stderr.String(), 500))
	}
	return remnant, fmt.Errorf("codex produced no result (stderr: %s)", truncate(stderr.String(), 300))
}

func (b *Bridge) execArgs(req Request) []string {
	args := []string{}
	if b.cfg.WebSearch {
		args = append(args, "--search")
	}
	bypass, sandbox, approval := b.permissionArgs(req)
	if bypass {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
	} else {
		if sandbox != "" {
			args = append(args, "--sandbox", sandbox)
		}
		if approval != "" {
			args = append(args, "--ask-for-approval", approval)
		}
	}
	if b.cfg.WorkDir != "" {
		args = append(args, "--cd", b.cfg.WorkDir)
	}
	for _, d := range b.cfg.AddDirs {
		args = append(args, "--add-dir", d)
	}
	model := strings.TrimSpace(req.Model)
	if model == "" {
		model = b.cfg.Model
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	if b.cfg.Effort != "" {
		args = append(args, "-c", fmt.Sprintf("model_reasoning_effort=%q", b.cfg.Effort))
	}
	args = append(args, b.cfg.ExtraArgs...)
	if req.SessionID != "" {
		return append(args, "exec", "resume", "--json", "--skip-git-repo-check", req.SessionID, "-")
	}
	return append(args, "exec", "--json", "--skip-git-repo-check", "-")
}

func (b *Bridge) permissionArgs(req Request) (bypass bool, sandbox, approval string) {
	if req.PlanOnly {
		return false, "read-only", "never"
	}
	switch strings.ToLower(strings.TrimSpace(req.Permissions)) {
	case "read-only":
		return false, "read-only", "never"
	case "workspace-write":
		return false, "workspace-write", "never"
	case "full-access":
		return true, "", ""
	}
	switch strings.ToLower(b.cfg.PermissionMode) {
	case "bypass", "bypasspermissions":
		return true, "", ""
	case "plan":
		return false, "read-only", "never"
	case "acceptedits", "accept-edits", "auto", "dontask":
		return false, "workspace-write", "never"
	}
	if b.cfg.DangerouslySkipPermissions {
		return true, "", ""
	}
	return false, b.cfg.Sandbox, b.cfg.ApprovalPolicy
}

func isBypassMode(mode string) bool {
	switch strings.ToLower(mode) {
	case "bypass", "bypasspermissions":
		return true
	default:
		return false
	}
}

func (b *Bridge) prompt(req Request) string {
	parts := make([]string, 0, 4)
	if strings.TrimSpace(b.cfg.AppendSystemPrompt) != "" {
		parts = append(parts, strings.TrimSpace(b.cfg.AppendSystemPrompt))
	}
	if strings.TrimSpace(req.Goal) != "" {
		parts = append(parts, "当前 QQ 会话目标：\n"+strings.TrimSpace(req.Goal))
	}
	if req.PlanOnly {
		parts = append(parts, "请只制定方案，不要修改文件或执行会改变项目状态的操作。")
	}
	parts = append(parts, req.Prompt)
	return strings.Join(parts, "\n\n---\n\n")
}

// consumeStream reads Codex JSONL events to completion. It returns a completed
// turn result (nil if none arrived), the most recent thread id seen, and any
// non-JSON lines as a raw fallback.
func consumeStream(r io.Reader, onActivity func(string)) (*Result, string, []byte, error) {
	var (
		sessionID string
		fallback  bytes.Buffer
		texts     []string
		usage     tokenUsage
		done      bool
		failed    bool
		errText   string
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var ev streamEvent
		if json.Unmarshal(line, &ev) != nil {
			fallback.Write(line)
			fallback.WriteByte('\n')
			continue
		}
		if ev.ThreadID != "" {
			sessionID = ev.ThreadID
		}
		switch ev.Type {
		case "item.started":
			if onActivity != nil {
				if label := activityLabel(ev.Item); label != "" {
					onActivity(label)
				}
			}
		case "item.completed":
			if ev.Item != nil && ev.Item.Type == "agent_message" && strings.TrimSpace(ev.Item.Text) != "" {
				texts = append(texts, strings.TrimSpace(ev.Item.Text))
			}
		case "turn.completed":
			done = true
			if ev.Usage != nil {
				usage = *ev.Usage
			}
		case "turn.failed", "error":
			failed = true
			errText = eventError(ev)
		}
	}
	if done || failed || len(texts) > 0 {
		text := strings.TrimSpace(strings.Join(texts, "\n\n"))
		if text == "" && errText != "" {
			text = errText
		}
		return &Result{
			Text:                  text,
			SessionID:             sessionID,
			IsError:               failed && !done,
			NumTurns:              1,
			InputTokens:           usage.InputTokens,
			CachedInputTokens:     usage.CachedInputTokens,
			OutputTokens:          usage.OutputTokens,
			ReasoningOutputTokens: usage.ReasoningOutputTokens,
			TotalTokens:           usage.TotalTokens,
		}, sessionID, fallback.Bytes(), sc.Err()
	}
	return nil, sessionID, fallback.Bytes(), sc.Err()
}

func activityLabel(item *streamItem) string {
	if item == nil {
		return ""
	}
	switch item.Type {
	case "", "agent_message":
		return ""
	case "command_execution":
		if item.Command != "" {
			return "shell"
		}
	case "mcp_tool_call":
		if item.Name != "" {
			return "mcp:" + item.Name
		}
	case "web_search_call", "web_search":
		return "web_search"
	}
	return strings.ReplaceAll(item.Type, "_", " ")
}

func eventError(ev streamEvent) string {
	if ev.Message != "" {
		return ev.Message
	}
	if len(ev.Error) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(ev.Error, &s) == nil {
		return s
	}
	var obj struct {
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(ev.Error, &obj) == nil {
		if obj.Message != "" {
			return obj.Message
		}
		return obj.Error
	}
	return string(ev.Error)
}

func parseResult(out []byte) (*Result, error) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("empty output from codex")
	}
	return &Result{Text: string(out)}, nil
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
