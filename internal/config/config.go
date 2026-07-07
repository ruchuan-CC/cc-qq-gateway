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
	Codex   CodexConfig   `toml:"codex"`
	Gateway GatewayConfig `toml:"gateway"`
}

// QQConfig holds QQ Bot credentials and WebSocket intent settings.
type QQConfig struct {
	AppID        string   `toml:"app_id"`
	ClientSecret string   `toml:"client_secret"`
	Sandbox      bool     `toml:"sandbox"`
	Intents      []string `toml:"intents"`
}

// CodexConfig configures the local Codex CLI invocation.
type CodexConfig struct {
	Binary                     string   `toml:"binary"`
	WorkDir                    string   `toml:"work_dir"`
	Model                      string   `toml:"model"`
	Effort                     string   `toml:"effort"`
	PermissionMode             string   `toml:"permission_mode"`
	Sandbox                    string   `toml:"sandbox"`
	ApprovalPolicy             string   `toml:"approval_policy"`
	DangerouslySkipPermissions bool     `toml:"dangerously_skip_permissions"`
	WebSearch                  bool     `toml:"web_search"`
	AppendSystemPrompt         string   `toml:"append_system_prompt"`
	AddDirs                    []string `toml:"add_dirs"`
	ExtraArgs                  []string `toml:"extra_args"`
	TimeoutSeconds             int      `toml:"timeout_seconds"`
}

// GatewayConfig holds transport-side behavior. These settings do not rewrite
// inbound prompts.
type GatewayConfig struct {
	MaxReplyChars      int      `toml:"max_reply_chars"`
	ReplyAsMarkdown    bool     `toml:"reply_as_markdown"`
	AllowedUsers       []string `toml:"allowed_users"`
	AdminUsers         []string `toml:"admin_users"`
	StatePath          string   `toml:"state_path"`
	AttachmentDir      string   `toml:"attachment_dir"`
	AttachmentMaxBytes int64    `toml:"attachment_max_bytes"`
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
	if c.Codex.Binary == "" {
		c.Codex.Binary = "codex"
	}
	if c.Codex.TimeoutSeconds == 0 {
		c.Codex.TimeoutSeconds = 300
	}
	if c.Codex.Sandbox == "" {
		c.Codex.Sandbox = "read-only"
	}
	if c.Codex.ApprovalPolicy == "" {
		c.Codex.ApprovalPolicy = "never"
	}
	if c.Gateway.MaxReplyChars == 0 {
		c.Gateway.MaxReplyChars = 1800
	} else if c.Gateway.MaxReplyChars < 200 {
		c.Gateway.MaxReplyChars = 200
	}
	if c.Gateway.AttachmentMaxBytes == 0 {
		c.Gateway.AttachmentMaxBytes = 512 * 1024 * 1024
	}
	if c.Gateway.AttachmentDir == "" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = "/tmp"
		}
		c.Gateway.AttachmentDir = home + "/.cc-qq/attachments"
	}
	switch c.Gateway.StatePath {
	case "":
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			home = "/tmp"
		}
		c.Gateway.StatePath = home + "/.cc-qq/state.json"
	case "none", "off", "disabled":
		c.Gateway.StatePath = ""
	}
}

func (c *Config) validate() error {
	if c.QQ.AppID == "" {
		return fmt.Errorf("qq.app_id is required")
	}
	if c.QQ.ClientSecret == "" {
		return fmt.Errorf("qq.client_secret is required")
	}
	if c.Codex.WorkDir != "" {
		if _, err := os.Stat(c.Codex.WorkDir); err != nil {
			return fmt.Errorf("codex work_dir %q: %w", c.Codex.WorkDir, err)
		}
	}
	switch c.Codex.PermissionMode {
	case "", "default", "plan", "acceptEdits", "bypassPermissions", "bypass", "auto", "dontAsk":
	default:
		return fmt.Errorf("permission_mode %q is invalid (use one of: default, plan, acceptEdits, bypassPermissions)", c.Codex.PermissionMode)
	}
	switch c.Codex.Sandbox {
	case "", "read-only", "workspace-write", "danger-full-access":
	default:
		return fmt.Errorf("sandbox %q is invalid (use one of: read-only, workspace-write, danger-full-access)", c.Codex.Sandbox)
	}
	switch c.Codex.ApprovalPolicy {
	case "", "never", "on-request", "on-failure", "untrusted":
	default:
		return fmt.Errorf("approval_policy %q is invalid (use one of: never, on-request, on-failure, untrusted)", c.Codex.ApprovalPolicy)
	}
	return nil
}

// CodexTimeout returns the configured per-turn timeout.
func (c *Config) CodexTimeout() time.Duration {
	return time.Duration(c.Codex.TimeoutSeconds) * time.Second
}
