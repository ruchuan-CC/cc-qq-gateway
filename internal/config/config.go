// Package config loads the gateway's TOML configuration.
package config

import (
	"fmt"
	"os"
	"time"

	"github.com/BurntSushi/toml"
)

// Config is the full gateway configuration.
type Config struct {
	QQ      QQConfig      `toml:"qq"`
	Claude  ClaudeConfig  `toml:"claude"`
	Gateway GatewayConfig `toml:"gateway"`
}

// QQConfig holds QQ Bot credentials and transport settings.
type QQConfig struct {
	AppID        string `toml:"app_id"`
	ClientSecret string `toml:"client_secret"`
	Sandbox      bool   `toml:"sandbox"`

	// Transport is "websocket" or "webhook".
	Transport string `toml:"transport"`

	// Intents are gateway intent names; empty uses a conversational default.
	Intents []string `toml:"intents"`

	// Webhook settings (used when Transport == "webhook").
	WebhookAddr    string `toml:"webhook_addr"` // e.g. ":8443"
	WebhookPath    string `toml:"webhook_path"` // e.g. "/qqbot"
	WebhookTLSCert string `toml:"webhook_tls_cert"`
	WebhookTLSKey  string `toml:"webhook_tls_key"`
}

// ClaudeConfig configures the local Claude Code CLI invocation.
type ClaudeConfig struct {
	Binary                     string   `toml:"binary"`
	WorkDir                    string   `toml:"work_dir"`
	Model                      string   `toml:"model"`
	PermissionMode             string   `toml:"permission_mode"`
	DangerouslySkipPermissions bool     `toml:"dangerously_skip_permissions"`
	AllowedTools               []string `toml:"allowed_tools"`
	DisallowedTools            []string `toml:"disallowed_tools"`
	AppendSystemPrompt         string   `toml:"append_system_prompt"`
	AddDirs                    []string `toml:"add_dirs"`
	ExtraArgs                  []string `toml:"extra_args"`
	TimeoutSeconds             int      `toml:"timeout_seconds"`
}

// GatewayConfig holds behavioral settings.
type GatewayConfig struct {
	// SessionIdleMinutes resets a conversation after this many idle minutes.
	SessionIdleMinutes int `toml:"session_idle_minutes"`
	// MaxReplyChars splits long replies into chunks of at most this many runes.
	MaxReplyChars int `toml:"max_reply_chars"`
	// ReplyAsMarkdown sends replies as markdown (msg_type=2) instead of text.
	ReplyAsMarkdown bool `toml:"reply_as_markdown"`
	// ThinkingMessage is sent immediately on receipt to acknowledge the user.
	ThinkingMessage string `toml:"thinking_message"`
	// AllowedUsers, if set, restricts the bot to these C2C (single-chat) user
	// open_ids. Empty serves any user. (Legacy allowed_groups keys are ignored.)
	AllowedUsers []string `toml:"allowed_users"`
	// MediaDir is where inbound attachments are downloaded and outbound files are
	// staged. Default: <home>/.cc-qq/media.
	MediaDir string `toml:"media_dir"`
	// SendLongRepliesAsFile, when true (default), delivers replies that exceed the
	// passive-reply budget as an uploaded file instead of truncating them.
	SendLongRepliesAsFile *bool `toml:"send_long_replies_as_file"`

	// NotifyAddr, when set, starts a localhost-only HTTP endpoint that lets trusted
	// local processes (e.g. the trading bot) push a proactive message to the operator
	// over QQ. Bind to a loopback address only (e.g. "127.0.0.1:8787"); a non-loopback
	// bind is refused at startup. Empty disables the endpoint.
	NotifyAddr string `toml:"notify_addr"`
	// NotifyToken is the shared secret a caller must present (header X-Notify-Token or
	// ?token=) to push. Required when NotifyAddr is set — an empty token disables the
	// endpoint even if an address is configured (never expose an unauthenticated push).
	NotifyToken string `toml:"notify_token"`
	// NotifyOpenID is the C2C open_id that pushes are delivered to. Empty falls back to
	// the first entry of AllowedUsers (the locked operator).
	NotifyOpenID string `toml:"notify_open_id"`
}

// Load reads and validates a TOML config file.
func Load(path string) (*Config, error) {
	var c Config
	if _, err := toml.DecodeFile(path, &c); err != nil {
		return nil, fmt.Errorf("decode config %s: %w", path, err)
	}
	c.applyDefaults()
	if err := c.validate(); err != nil {
		return nil, err
	}
	return &c, nil
}

func (c *Config) applyDefaults() {
	if c.QQ.Transport == "" {
		c.QQ.Transport = "websocket"
	}
	if c.QQ.WebhookPath == "" {
		c.QQ.WebhookPath = "/qqbot"
	}
	if c.QQ.WebhookAddr == "" {
		c.QQ.WebhookAddr = ":8443"
	}
	if c.Claude.Binary == "" {
		c.Claude.Binary = "claude"
	}
	if c.Claude.TimeoutSeconds == 0 {
		c.Claude.TimeoutSeconds = 300
	}
	if c.Gateway.SessionIdleMinutes == 0 {
		c.Gateway.SessionIdleMinutes = 30
	}
	if c.Gateway.MaxReplyChars == 0 {
		c.Gateway.MaxReplyChars = 1800
	} else if c.Gateway.MaxReplyChars < 200 {
		// Keep a sane floor: the deliver path subtracts a small header budget, so a
		// tiny value would make chunking degenerate (and previously underflowed).
		c.Gateway.MaxReplyChars = 200
	}
	if c.Gateway.MediaDir == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = "/tmp"
		}
		c.Gateway.MediaDir = home + "/.cc-qq/media"
	}
	if c.Gateway.SendLongRepliesAsFile == nil {
		v := true
		c.Gateway.SendLongRepliesAsFile = &v
	}
}

// LongRepliesAsFile reports whether long replies should be sent as a file.
func (g GatewayConfig) LongRepliesAsFile() bool {
	return g.SendLongRepliesAsFile == nil || *g.SendLongRepliesAsFile
}

func (c *Config) validate() error {
	if c.QQ.AppID == "" {
		return fmt.Errorf("qq.app_id is required")
	}
	if c.QQ.ClientSecret == "" {
		return fmt.Errorf("qq.client_secret is required")
	}
	switch c.QQ.Transport {
	case "websocket", "webhook":
	default:
		return fmt.Errorf("qq.transport must be \"websocket\" or \"webhook\", got %q", c.QQ.Transport)
	}
	if c.Claude.WorkDir != "" {
		if _, err := os.Stat(c.Claude.WorkDir); err != nil {
			return fmt.Errorf("claude.work_dir %q: %w", c.Claude.WorkDir, err)
		}
	}
	switch c.Claude.PermissionMode {
	case "", "default", "plan", "acceptEdits", "bypassPermissions", "auto", "dontAsk":
		// valid CLI --permission-mode values (empty = leave the CLI default)
	default:
		return fmt.Errorf("claude.permission_mode %q is invalid (use one of: default, plan, "+
			"acceptEdits, bypassPermissions, auto, dontAsk)", c.Claude.PermissionMode)
	}
	return nil
}

// ClaudeTimeout returns the configured per-turn timeout.
func (c *Config) ClaudeTimeout() time.Duration {
	return time.Duration(c.Claude.TimeoutSeconds) * time.Second
}

// SessionIdleTTL returns the configured idle TTL.
func (c *Config) SessionIdleTTL() time.Duration {
	return time.Duration(c.Gateway.SessionIdleMinutes) * time.Minute
}
