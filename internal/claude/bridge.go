// Package claude bridges the gateway to a locally-installed Claude Code CLI.
//
// Each turn is run in non-interactive "print" mode with JSON output so we can
// extract the assistant's reply and the session id, which is threaded back into
// the next turn via --resume to maintain conversational context.
package claude

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
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

	args := []string{"--print", "--output-format", "json"}
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
	// Pass the prompt via stdin rather than as a trailing positional arg:
	// variadic flags like --add-dir greedily consume following args, which would
	// otherwise swallow the prompt and make the CLI think no input was given.
	cmd.Stdin = strings.NewReader(req.Prompt)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		// Even on a non-zero exit the CLI may have emitted a JSON error object.
		if r, perr := parseResult(stdout.Bytes()); perr == nil && r.Text != "" {
			return r, nil
		}
		return nil, fmt.Errorf("claude run failed: %w (stderr: %s)", err, truncate(stderr.String(), 500))
	}

	r, err := parseResult(stdout.Bytes())
	if err != nil {
		return nil, fmt.Errorf("%w (stderr: %s)", err, truncate(stderr.String(), 300))
	}
	return r, nil
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
