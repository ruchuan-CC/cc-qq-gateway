// Package app assembles the gateway components from configuration and keeps the
// QQ WebSocket transport running until the context is cancelled.
package app

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"time"

	"github.com/chenhg5/cc-qq-gateway/internal/codex"
	"github.com/chenhg5/cc-qq-gateway/internal/config"
	"github.com/chenhg5/cc-qq-gateway/internal/gateway"
	"github.com/chenhg5/cc-qq-gateway/internal/qq"
	"github.com/chenhg5/cc-qq-gateway/internal/session"
)

// App is a fully wired gateway ready to run.
type App struct {
	cfg    *config.Config
	client *qq.Client
	gw     *gateway.Gateway
	logger *log.Logger
}

// New builds an App from configuration.
func New(cfg *config.Config, logger *log.Logger) *App {
	if logger == nil {
		logger = log.Default()
	}
	client := qq.NewClient(qq.Options{
		AppID:        cfg.QQ.AppID,
		ClientSecret: cfg.QQ.ClientSecret,
		Sandbox:      cfg.QQ.Sandbox,
	})
	bridge := codex.New(codex.Config{
		Binary:                     cfg.Codex.Binary,
		WorkDir:                    cfg.Codex.WorkDir,
		Model:                      cfg.Codex.Model,
		Effort:                     cfg.Codex.Effort,
		PermissionMode:             cfg.Codex.PermissionMode,
		Sandbox:                    cfg.Codex.Sandbox,
		ApprovalPolicy:             cfg.Codex.ApprovalPolicy,
		DangerouslySkipPermissions: cfg.Codex.DangerouslySkipPermissions,
		WebSearch:                  cfg.Codex.WebSearch,
		AppendSystemPrompt:         cfg.Codex.AppendSystemPrompt,
		AddDirs:                    cfg.Codex.AddDirs,
		ExtraArgs:                  cfg.Codex.ExtraArgs,
		Timeout:                    cfg.CodexTimeout(),
	})
	sessions := session.NewManager()
	sessions.SetStatePath(cfg.Gateway.StatePath)
	if err := sessions.LoadState(); err != nil {
		logger.Printf("[app] warning: could not restore session state: %v", err)
	} else if cfg.Gateway.StatePath != "" {
		logger.Printf("[app] session state restored from %s", cfg.Gateway.StatePath)
	}
	gw := gateway.New(client, bridge, sessions, cfg.Gateway, logger)
	return &App{cfg: cfg, client: client, gw: gw, logger: logger}
}

// Run supervises the WebSocket transport. Any error or panic from the transport
// is logged and the transport is restarted after a short, jittered delay.
func (a *App) Run(ctx context.Context) error {
	a.awaitIdentity(ctx)

	defer a.gw.SaveState()
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				a.gw.SaveState()
			}
		}
	}()

	const (
		minDelay = 2 * time.Second
		maxDelay = 30 * time.Second
	)
	delay := minDelay
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		start := time.Now()
		err := a.runTransportSafely(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if time.Since(start) >= time.Minute {
			delay = minDelay
		}
		a.logger.Printf("[app] transport exited after %s: %v; restarting in ~%s",
			time.Since(start).Round(time.Second), err, delay)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jitter(delay)):
		}
		if delay < maxDelay {
			delay *= 2
			if delay > maxDelay {
				delay = maxDelay
			}
		}
	}
}

func (a *App) runTransportSafely(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("transport panic: %v", r)
			a.logger.Printf("[app] recovered from panic: %v", r)
		}
	}()
	return a.runWebSocket(ctx)
}

func (a *App) awaitIdentity(ctx context.Context) {
	for attempt := 1; attempt <= 5; attempt++ {
		if ctx.Err() != nil {
			return
		}
		tokCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		me, err := a.client.GetMe(tokCtx)
		cancel()
		if err == nil {
			a.logger.Printf("[app] authenticated as bot %s (id %s)", me.Username, me.ID)
			return
		}
		a.logger.Printf("[app] identity check attempt %d failed: %v", attempt, err)
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Duration(attempt) * 2 * time.Second):
		}
	}
	a.logger.Printf("[app] warning: could not confirm bot identity; continuing and will retry on connect")
}

func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	delta := float64(d) * 0.2
	return time.Duration(float64(d) - delta + rand.Float64()*2*delta)
}

func (a *App) runWebSocket(ctx context.Context) error {
	intents := qq.IntentsFromNames(a.cfg.QQ.Intents)
	a.logger.Printf("[app] starting WebSocket transport (intents=%d)", int(intents))
	ws := qq.NewWSClient(a.client, intents, a.gw.HandleEvent, a.logger)
	return ws.Run(ctx)
}
