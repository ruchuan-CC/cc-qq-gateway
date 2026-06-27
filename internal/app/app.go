// Package app assembles the gateway components from configuration and runs the
// selected transport until the context is cancelled.
package app

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"time"

	"github.com/chenhg5/cc-qq-gateway/internal/claude"
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
	bridge := claude.New(claude.Config{
		Binary:                     cfg.Claude.Binary,
		WorkDir:                    cfg.Claude.WorkDir,
		Model:                      cfg.Claude.Model,
		PermissionMode:             cfg.Claude.PermissionMode,
		DangerouslySkipPermissions: cfg.Claude.DangerouslySkipPermissions,
		AllowedTools:               cfg.Claude.AllowedTools,
		DisallowedTools:            cfg.Claude.DisallowedTools,
		AppendSystemPrompt:         cfg.Claude.AppendSystemPrompt,
		ProtocolPrompt:             gateway.ProtocolPrompt,
		AddDirs:                    cfg.Claude.AddDirs,
		ExtraArgs:                  cfg.Claude.ExtraArgs,
		Timeout:                    cfg.ClaudeTimeout(),
	})
	sessions := session.NewManager(cfg.SessionIdleTTL())
	gw := gateway.New(client, bridge, sessions, cfg.Gateway, logger)

	return &App{cfg: cfg, client: client, gw: gw, logger: logger}
}

// Run supervises the configured transport and only returns when ctx is
// cancelled. Any error or panic from the transport is logged and the transport
// is restarted after a short, jittered delay, so the gateway is self-healing and
// effectively never stays offline while the process is alive.
func (a *App) Run(ctx context.Context) error {
	// Verify credentials early (best-effort, retried) so startup is observable.
	a.awaitIdentity(ctx)

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
		// A transport that ran a while recovered cleanly; restart promptly.
		if time.Since(start) >= time.Minute {
			delay = minDelay
		}
		a.logger.Printf("[app] transport %q exited after %s: %v; restarting in ~%s",
			a.cfg.QQ.Transport, time.Since(start).Round(time.Second), err, delay)
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

// runTransportSafely runs the selected transport, converting a panic into an
// error so the supervisor can restart it instead of crashing the process.
func (a *App) runTransportSafely(ctx context.Context) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("transport panic: %v", r)
			a.logger.Printf("[app] recovered from panic: %v", r)
		}
	}()
	switch a.cfg.QQ.Transport {
	case "websocket":
		return a.runWebSocket(ctx)
	case "webhook":
		return a.runWebhook(ctx)
	default:
		return fmt.Errorf("unknown transport %q", a.cfg.QQ.Transport)
	}
}

// awaitIdentity logs the bot identity once credentials work, retrying briefly so
// a transient network failure at boot doesn't look like a hard error.
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

// jitter returns d perturbed by ±20% to avoid synchronized restarts.
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

func (a *App) runWebhook(ctx context.Context) error {
	srv := qq.NewWebhookServer(a.cfg.QQ.ClientSecret, a.cfg.QQ.WebhookPath, a.gw.HandleEvent, a.logger)
	mux := http.NewServeMux()
	mux.Handle(srv.Path(), srv.Handler())
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	httpSrv := &http.Server{
		Addr:              a.cfg.QQ.WebhookAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		a.logger.Printf("[app] starting Webhook transport on %s%s", a.cfg.QQ.WebhookAddr, srv.Path())
		if a.cfg.QQ.WebhookTLSCert != "" && a.cfg.QQ.WebhookTLSKey != "" {
			errCh <- httpSrv.ListenAndServeTLS(a.cfg.QQ.WebhookTLSCert, a.cfg.QQ.WebhookTLSKey)
		} else {
			a.logger.Printf("[app] WARNING: serving webhook without TLS; terminate TLS at a reverse proxy")
			errCh <- httpSrv.ListenAndServe()
		}
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = httpSrv.Shutdown(shutCtx)
		return ctx.Err()
	case err := <-errCh:
		return err
	}
}
