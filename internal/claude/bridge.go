// Package claude bridges the gateway to a locally-installed Claude Code CLI.
//
// Each turn is run in non-interactive "print" mode with JSON output so we can
// extract the assistant's reply and the session id, which is threaded back into
// the next turn via --resume to maintain conversational context.
package claude

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Config configures how the local Claude Code CLI is invoked.
type Config struct {
	// Binary is the path to the claude executable (default "claude").
	Binary string
	// WorkDir is the working directory Claude runs in (the project root).
	WorkDir string
	// Model overrides the model (e.g. "claude-opus-4-8"); empty = CLI default.
	Model string
	// PermissionMode is passed via --permission-mode (default / acceptEdits /
	// plan / bypassPermissions). Empty leaves the CLI default.
	PermissionMode string
	// DangerouslySkipPermissions passes --dangerously-skip-permissions for fully
	// autonomous tool use. Use with care.
	DangerouslySkipPermissions bool
	// AllowedTools / DisallowedTools restrict tool access.
	AllowedTools    []string
	DisallowedTools []string
	// AppendSystemPrompt is added via --append-system-prompt.
	AppendSystemPrompt string
	// ProtocolPrompt is an additional system-prompt addendum the gateway always
	// injects (e.g. the QQ media I/O protocol). Combined with AppendSystemPrompt.
	ProtocolPrompt string
	// AddDirs are extra directories Claude may access (--add-dir).
	AddDirs []string
	// ExtraArgs are appended verbatim to the command line.
	ExtraArgs []string
	// Timeout bounds a single turn (default 5m).
	Timeout time.Duration
}

// Bridge runs Claude Code turns.
type Bridge struct {
	cfg Config
}

// New creates a Bridge.
func New(cfg Config) *Bridge {
	if cfg.Binary == "" {
		cfg.Binary = "claude"
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Minute
	}
	return &Bridge{cfg: cfg}
}

// DefaultWorkDir reports the bridge's configured working directory.
func (b *Bridge) DefaultWorkDir() string { return b.cfg.WorkDir }

// DefaultModel reports the bridge's configured model ("" means CLI default).
func (b *Bridge) DefaultModel() string { return b.cfg.Model }

// FullAuthority reports whether turns run with permission prompts disabled.
func (b *Bridge) FullAuthority() bool { return b.cfg.DangerouslySkipPermissions }

// Result is the outcome of a single Claude Code turn.
type Result struct {
	Text       string
	SessionID  string
	IsError    bool
	CostUSD    float64
	NumTurns   int
	DurationMS int
}

// cliResult mirrors the JSON object emitted by `claude -p --output-format json`.
type cliResult struct {
	Type       string  `json:"type"`
	Subtype    string  `json:"subtype"`
	IsError    bool    `json:"is_error"`
	Result     string  `json:"result"`
	SessionID  string  `json:"session_id"`
	TotalCost  float64 `json:"total_cost_usd"`
	NumTurns   int     `json:"num_turns"`
	DurationMS int     `json:"duration_ms"`
	Error      string  `json:"error"`
}

// Request is a single Claude Code turn. SessionID, when set, resumes an existing
// conversation. Model and WorkDir override the bridge defaults for this turn
// only (used by per-conversation /model and /cwd commands).
type Request struct {
	SessionID string
	Prompt    string
	Model     string // overrides Config.Model when non-empty
	WorkDir   string // overrides Config.WorkDir when non-empty
	// PermissionMode overrides the configured permission handling for this turn:
	// "default" | "plan" | "acceptEdits" | "bypass". Empty uses the config.
	PermissionMode string
	// OnActivity, when set, is called with a short label each time the turn makes
	// visible progress (a tool starts). Used for server-side progress logging so a
	// long-running turn can be observed instead of looking dead. Called from the
	// goroutine driving Run; keep it cheap and non-blocking.
	OnActivity func(label string)
}

// streamEvent is one newline-delimited object from `--output-format stream-json`.
// It unions the fields we care about across the system/assistant/result event types.
type streamEvent struct {
	Type       string          `json:"type"`
	Subtype    string          `json:"subtype"`
	SessionID  string          `json:"session_id"`
	IsError    bool            `json:"is_error"`
	Result     string          `json:"result"`
	Error      string          `json:"error"`
	TotalCost  float64         `json:"total_cost_usd"`
	NumTurns   int             `json:"num_turns"`
	DurationMS int             `json:"duration_ms"`
	Message    json.RawMessage `json:"message"`
}

// Run executes one turn. If req.SessionID is non-empty the conversation is
// resumed; otherwise a new session is started. The new/continued session id is
// returned in Result.SessionID.
func (b *Bridge) Run(ctx context.Context, req Request) (*Result, error) {
	ctx, cancel := context.WithTimeout(ctx, b.cfg.Timeout)
	defer cancel()

	model := b.cfg.Model
	if req.Model != "" {
		model = req.Model
	}
	workDir := b.cfg.WorkDir
	if req.WorkDir != "" {
		workDir = req.WorkDir
	}

	// Stream the turn as newline-delimited JSON events (--verbose is required by the
	// CLI for stream-json in print mode). Streaming lets us (1) capture the session
	// id as soon as it is reported so a turn killed by the timeout can still be
	// resumed instead of vanishing, and (2) observe tool activity as it happens.
	args := []string{"--print", "--output-format", "stream-json", "--verbose"}
	if req.SessionID != "" {
		args = append(args, "--resume", req.SessionID)
	}
	if model != "" {
		args = append(args, "--model", model)
	}
	switch {
	case req.PermissionMode == "bypass" || req.PermissionMode == "bypassPermissions":
		args = append(args, "--dangerously-skip-permissions")
	case req.PermissionMode != "":
		args = append(args, "--permission-mode", req.PermissionMode)
	case b.cfg.DangerouslySkipPermissions:
		args = append(args, "--dangerously-skip-permissions")
	case b.cfg.PermissionMode != "":
		args = append(args, "--permission-mode", b.cfg.PermissionMode)
	}
	if len(b.cfg.AllowedTools) > 0 {
		args = append(args, "--allowedTools", strings.Join(b.cfg.AllowedTools, ","))
	}
	if len(b.cfg.DisallowedTools) > 0 {
		args = append(args, "--disallowedTools", strings.Join(b.cfg.DisallowedTools, ","))
	}
	if sys := joinSystemPrompts(b.cfg.AppendSystemPrompt, b.cfg.ProtocolPrompt); sys != "" {
		args = append(args, "--append-system-prompt", sys)
	}
	for _, d := range b.cfg.AddDirs {
		args = append(args, "--add-dir", d)
	}
	args = append(args, b.cfg.ExtraArgs...)

	cmd := exec.CommandContext(ctx, b.cfg.Binary, args...)
	if workDir != "" {
		cmd.Dir = workDir
	}
	// Run the turn in its own process group and, on cancel (timeout or /stop), kill
	// the WHOLE group — not just claude, but every tool subprocess it spawned (a long
	// Bash, an MCP server). Otherwise those would be orphaned and keep running.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process != nil {
			_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) // negative pid = the group
		}
		return nil
	}
	// Bound the post-cancel wait: if a lingering child still holds the stdout pipe
	// open after the group is killed, don't let cmd.Wait() (and the turn) hang forever.
	cmd.WaitDelay = 5 * time.Second
	// Pass the prompt via stdin rather than as a trailing positional arg:
	// variadic flags like --add-dir greedily consume following args, which would
	// otherwise swallow the prompt and make the CLI think no input was given.
	cmd.Stdin = strings.NewReader(req.Prompt)
	stdoutPipe, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("claude stdout pipe: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("claude start failed: %w (stderr: %s)", err, truncate(stderr.String(), 300))
	}

	result, sessionID, fallback, scanErr := consumeStream(stdoutPipe, req.OnActivity)
	waitErr := cmd.Wait()

	// A terminal result event is authoritative even if Wait later reports an error.
	if result != nil {
		if result.SessionID == "" {
			result.SessionID = sessionID
		}
		return result, nil
	}

	// No result event: the process was killed (timeout/OOM/cancel) or produced an
	// unexpected shape. Return whatever session id we saw so the caller can resume,
	// alongside the error — callers must treat a non-nil Result on an error as a
	// resumable remnant, not a successful turn.
	if r, perr := parseResult(fallback); perr == nil && r.Text != "" {
		if r.SessionID == "" {
			r.SessionID = sessionID
		}
		return r, nil
	}
	remnant := &Result{SessionID: sessionID}
	if scanErr != nil {
		// The stream was cut short by a read error (e.g. a single event larger than the
		// 8MB scanner cap). Report it plainly instead of letting it masquerade as a
		// silent "no result" / timeout.
		return remnant, fmt.Errorf("claude output stream error: %w (stderr: %s)", scanErr, truncate(stderr.String(), 300))
	}
	if waitErr != nil {
		return remnant, fmt.Errorf("claude run failed: %w (stderr: %s)", waitErr, truncate(stderr.String(), 500))
	}
	return remnant, fmt.Errorf("claude produced no result (stderr: %s)", truncate(stderr.String(), 300))
}

// consumeStream reads newline-delimited stream-json events to completion. It
// returns the terminal "result" event (nil if none arrived, e.g. the process was
// killed mid-turn), the most recent session id seen, and any non-JSON lines as a
// raw fallback. onActivity, when non-nil, is called for each tool a turn starts.
func consumeStream(r io.Reader, onActivity func(string)) (*Result, string, []byte, error) {
	var (
		result    *Result
		sessionID string
		fallback  bytes.Buffer
	)
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024) // tool results can be large
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
		if ev.SessionID != "" {
			sessionID = ev.SessionID
		}
		switch ev.Type {
		case "assistant":
			if onActivity != nil {
				for _, name := range toolNames(ev.Message) {
					onActivity(name)
				}
			}
		case "result":
			text := ev.Result
			if text == "" && ev.Error != "" {
				text = ev.Error
			}
			result = &Result{
				Text:       text,
				SessionID:  ev.SessionID,
				IsError:    ev.IsError,
				CostUSD:    ev.TotalCost,
				NumTurns:   ev.NumTurns,
				DurationMS: ev.DurationMS,
			}
		}
	}
	return result, sessionID, fallback.Bytes(), sc.Err()
}

// toolNames extracts the names of any tool_use blocks in an assistant message's
// content, for progress logging. Unparseable or non-tool messages yield nothing.
func toolNames(raw json.RawMessage) []string {
	if len(raw) == 0 {
		return nil
	}
	var m struct {
		Content []struct {
			Type string `json:"type"`
			Name string `json:"name"`
		} `json:"content"`
	}
	if json.Unmarshal(raw, &m) != nil {
		return nil
	}
	var names []string
	for _, c := range m.Content {
		if c.Type == "tool_use" && c.Name != "" {
			names = append(names, c.Name)
		}
	}
	return names
}

// RunCLI runs a claude management subcommand (e.g. "mcp list", "agents --json")
// and returns its combined output. Used by the /mcp and /agents commands. Output
// is returned even on non-zero exit so error text reaches the user.
func (b *Bridge) RunCLI(ctx context.Context, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, b.cfg.Binary, args...)
	if b.cfg.WorkDir != "" {
		cmd.Dir = b.cfg.WorkDir
	}
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	err := cmd.Run()
	return strings.TrimSpace(out.String()), err
}

func parseResult(out []byte) (*Result, error) {
	out = bytes.TrimSpace(out)
	if len(out) == 0 {
		return nil, fmt.Errorf("empty output from claude")
	}
	var cr cliResult
	if err := json.Unmarshal(out, &cr); err != nil {
		// Fall back to treating the raw stdout as the reply text.
		return &Result{Text: string(out)}, nil
	}
	text := cr.Result
	if text == "" && cr.Error != "" {
		text = cr.Error
	}
	return &Result{
		Text:       text,
		SessionID:  cr.SessionID,
		IsError:    cr.IsError,
		CostUSD:    cr.TotalCost,
		NumTurns:   cr.NumTurns,
		DurationMS: cr.DurationMS,
	}, nil
}

// joinSystemPrompts combines the user-configured and gateway-injected system
// prompt addenda into a single --append-system-prompt value.
func joinSystemPrompts(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			nonEmpty = append(nonEmpty, strings.TrimSpace(p))
		}
	}
	return strings.Join(nonEmpty, "\n\n")
}

func truncate(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
