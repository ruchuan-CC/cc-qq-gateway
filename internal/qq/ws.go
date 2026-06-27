package qq

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// EventHandler receives a decoded dispatch payload (op 0). The handler should
// not block for long; offload heavy work to a goroutine/queue.
type EventHandler func(ctx context.Context, p *Payload)

// WSClient maintains a resilient WebSocket connection to the QQ gateway,
// handling identify, heartbeat, resume and automatic reconnect.
type WSClient struct {
	client  *Client
	intents Intent
	handler EventHandler
	logger  *log.Logger

	mu        sync.Mutex
	sessionID string
	lastSeq   int64
}

// NewWSClient builds a WebSocket transport.
func NewWSClient(client *Client, intents Intent, handler EventHandler, logger *log.Logger) *WSClient {
	if logger == nil {
		logger = log.Default()
	}
	return &WSClient{client: client, intents: intents, handler: handler, logger: logger}
}

// Run connects and processes events until ctx is cancelled. It reconnects
// forever with exponential backoff (capped, with jitter) on any failure, so the
// gateway stays online across network blips, gateway restarts and token churn.
// The backoff resets after a connection has stayed up long enough to be
// considered healthy, so a long-lived link that drops once retries immediately.
func (w *WSClient) Run(ctx context.Context) error {
	const (
		minBackoff   = time.Second
		maxBackoff   = 30 * time.Second
		stableUptime = 60 * time.Second // a connection up this long is "healthy"
	)
	backoff := minBackoff
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		start := time.Now()
		err := w.runOnce(ctx)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// A connection that survived a while is healthy; retry promptly.
		if time.Since(start) >= stableUptime {
			backoff = minBackoff
		}
		if err != nil {
			w.logger.Printf("[ws] connection ended after %s: %v (reconnecting in ~%s)", time.Since(start).Round(time.Second), err, backoff)
		} else {
			w.logger.Printf("[ws] connection ended after %s (reconnecting in ~%s)", time.Since(start).Round(time.Second), backoff)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(jitter(backoff)):
		}
		if backoff < maxBackoff {
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
		}
	}
}

// jitter returns d perturbed by ±20% to avoid reconnect thundering herds.
func jitter(d time.Duration) time.Duration {
	if d <= 0 {
		return 0
	}
	delta := float64(d) * 0.2
	return time.Duration(float64(d) - delta + rand.Float64()*2*delta)
}

func (w *WSClient) runOnce(ctx context.Context) error {
	info, err := w.client.GetGatewayBot(ctx)
	if err != nil {
		return fmt.Errorf("get gateway: %w", err)
	}

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, info.URL, nil)
	if err != nil {
		return fmt.Errorf("dial %s: %w", info.URL, err)
	}
	defer conn.Close()

	connCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Unblock a stuck ReadMessage promptly when the context is cancelled (clean
	// shutdown) or a watchdog/heartbeat forces a reconnect.
	go func() {
		<-connCtx.Done()
		_ = conn.Close()
	}()

	// Read the Hello frame.
	_, raw, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read hello: %w", err)
	}
	var hello Payload
	if err := json.Unmarshal(raw, &hello); err != nil {
		return fmt.Errorf("decode hello: %w", err)
	}
	if hello.Op != OpHello {
		return fmt.Errorf("expected hello op, got %d", hello.Op)
	}
	var helloData HelloData
	_ = json.Unmarshal(hello.Data, &helloData)
	interval := time.Duration(helloData.HeartbeatInterval) * time.Millisecond
	if interval <= 0 {
		interval = 45 * time.Second
	}

	token, err := w.client.Token(ctx)
	if err != nil {
		return fmt.Errorf("token: %w", err)
	}

	// Resume if we have a session, otherwise identify.
	w.mu.Lock()
	resuming := w.sessionID != ""
	sessionID, lastSeq := w.sessionID, w.lastSeq
	w.mu.Unlock()

	var writeMu sync.Mutex
	send := func(v any) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return conn.WriteJSON(v)
	}

	// Liveness tracking: any inbound frame proves the link is alive. The
	// heartbeat goroutine reconnects if the gateway goes silent past a grace
	// window, catching half-open ("zombie") TCP connections the OS hasn't
	// noticed yet.
	var liveMu sync.Mutex
	lastAlive := time.Now()
	markAlive := func() {
		liveMu.Lock()
		lastAlive = time.Now()
		liveMu.Unlock()
	}
	silentFor := func() time.Duration {
		liveMu.Lock()
		defer liveMu.Unlock()
		return time.Since(lastAlive)
	}

	if resuming {
		w.logger.Printf("[ws] resuming session %s at seq %d", sessionID, lastSeq)
		if err := send(map[string]any{
			"op": OpResume,
			"d": map[string]any{
				"token":      "QQBot " + token,
				"session_id": sessionID,
				"seq":        lastSeq,
			},
		}); err != nil {
			return fmt.Errorf("send resume: %w", err)
		}
	} else {
		w.logger.Printf("[ws] identifying with intents %d", int(w.intents))
		if err := send(map[string]any{
			"op": OpIdentify,
			"d": map[string]any{
				"token":   "QQBot " + token,
				"intents": int(w.intents),
				"shard":   []int{0, 1},
				"properties": map[string]string{
					"$os":      "linux",
					"$browser": "cc-qq-gateway",
					"$device":  "cc-qq-gateway",
				},
			},
		}); err != nil {
			return fmt.Errorf("send identify: %w", err)
		}
	}

	// Heartbeat loop, with a liveness watchdog. The watchdog fires more often
	// than the heartbeat so a dead link is detected within ~one interval.
	staleAfter := interval*2 + 10*time.Second
	go func() {
		hb := time.NewTicker(interval)
		defer hb.Stop()
		watch := time.NewTicker(interval / 2)
		defer watch.Stop()
		for {
			select {
			case <-connCtx.Done():
				return
			case <-watch.C:
				if silentFor() > staleAfter {
					w.logger.Printf("[ws] no gateway traffic for %s; forcing reconnect", silentFor().Round(time.Second))
					_ = conn.Close()
					cancel()
					return
				}
			case <-hb.C:
				w.mu.Lock()
				seq := w.lastSeq
				w.mu.Unlock()
				var d any
				if seq > 0 {
					d = seq
				}
				if err := send(map[string]any{"op": OpHeartbeat, "d": d}); err != nil {
					w.logger.Printf("[ws] heartbeat failed: %v", err)
					_ = conn.Close()
					cancel()
					return
				}
			}
		}
	}()

	// Read loop.
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		markAlive() // any frame proves the link is alive
		var p Payload
		if err := json.Unmarshal(raw, &p); err != nil {
			w.logger.Printf("[ws] decode frame: %v", err)
			continue
		}

		switch p.Op {
		case OpDispatch:
			if p.Seq > 0 {
				w.mu.Lock()
				w.lastSeq = p.Seq
				w.mu.Unlock()
			}
			if p.Type == EventReady {
				var rd ReadyData
				if err := json.Unmarshal(p.Data, &rd); err == nil {
					w.mu.Lock()
					w.sessionID = rd.SessionID
					w.mu.Unlock()
					w.logger.Printf("[ws] READY as %s (session %s)", rd.User.Username, rd.SessionID)
				}
			}
			if w.handler != nil {
				w.handler(connCtx, &p)
			}
		case OpHeartbeat:
			// Server requested an immediate heartbeat.
			w.mu.Lock()
			seq := w.lastSeq
			w.mu.Unlock()
			_ = send(map[string]any{"op": OpHeartbeat, "d": seq})
		case OpHeartbeatACK:
			// ok
		case OpReconnect:
			w.logger.Printf("[ws] server requested reconnect")
			return nil
		case OpInvalidSession:
			w.logger.Printf("[ws] invalid session; starting fresh")
			w.mu.Lock()
			w.sessionID = ""
			w.lastSeq = 0
			w.mu.Unlock()
			return nil
		default:
			// ignore unknown opcodes
		}
	}
}
