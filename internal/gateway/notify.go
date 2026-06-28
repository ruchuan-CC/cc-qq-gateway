package gateway

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

// NotifyServer is a localhost-only HTTP endpoint that lets a trusted local
// process (the trading bot) push a proactive message to the operator over QQ.
// It is intentionally tiny: one authenticated POST → one active push (with the
// gateway's queue-for-next-inbound fallback). Bind to loopback only.
type NotifyServer struct {
	gw    *Gateway
	addr  string
	token string
}

// NewNotifyServer builds a notify endpoint. Returns nil (disabled) when no
// address or token is configured, or when the address is not a loopback bind —
// an unauthenticated or externally-reachable push channel is never started.
func (g *Gateway) NewNotifyServer() *NotifyServer {
	addr := strings.TrimSpace(g.cfg.NotifyAddr)
	token := strings.TrimSpace(g.cfg.NotifyToken)
	if addr == "" || token == "" {
		if addr != "" && token == "" {
			g.logger.Printf("[notify] disabled: notify_addr set but notify_token empty (refusing unauthenticated push)")
		}
		return nil
	}
	if !isLoopbackAddr(addr) {
		g.logger.Printf("[notify] disabled: notify_addr %q is not a loopback bind (use 127.0.0.1:PORT)", addr)
		return nil
	}
	return &NotifyServer{gw: g, addr: addr, token: token}
}

// isLoopbackAddr reports whether host:port binds only the loopback interface.
func isLoopbackAddr(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	host = strings.TrimSpace(host)
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// maxNotifyBody caps an inbound push body (defensive; messages are short).
const maxNotifyBody = 16 << 10

// Run serves the notify endpoint until ctx is cancelled. It is started once for
// the process lifetime (independent of the transport restart loop).
func (n *NotifyServer) Run(ctx context.Context) {
	mux := http.NewServeMux()
	mux.HandleFunc("/notify", n.handle)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	srv := &http.Server{Addr: n.addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() {
		<-ctx.Done()
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}()
	n.gw.logger.Printf("[notify] listening on %s (POST /notify)", n.addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		n.gw.logger.Printf("[notify] server stopped: %v", err)
	}
}

// handle authenticates a push request and forwards its text to the operator.
func (n *NotifyServer) handle(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	got := r.Header.Get("X-Notify-Token")
	if got == "" {
		got = r.URL.Query().Get("token")
	}
	// Constant-time comparison so the token can't be guessed by timing.
	if subtle.ConstantTimeCompare([]byte(got), []byte(n.token)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, maxNotifyBody))
	if err != nil {
		http.Error(w, "read error", http.StatusBadRequest)
		return
	}
	text := extractNotifyText(body, r.Header.Get("Content-Type"))
	if strings.TrimSpace(text) == "" {
		http.Error(w, "empty text", http.StatusBadRequest)
		return
	}

	// Detach from the request context so returning the HTTP response doesn't cancel
	// the in-flight QQ send.
	if err := n.gw.PushToOperator(context.Background(), text); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok"))
}

// extractNotifyText pulls the message out of a request body: a JSON object with a
// "text" field when the body is JSON, otherwise the raw body as plain text.
func extractNotifyText(body []byte, contentType string) string {
	trimmed := strings.TrimSpace(string(body))
	if strings.Contains(contentType, "application/json") ||
		(strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}")) {
		var payload struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(trimmed), &payload); err == nil && strings.TrimSpace(payload.Text) != "" {
			return payload.Text
		}
	}
	return trimmed
}
