package qq

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newTestClient wires a Client to a test server, bypassing real token fetches
// by pre-seeding the token cache.
func newTestClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()
	c := NewClient(Options{AppID: "app", ClientSecret: "secret", BaseURL: srv.URL})
	c.tokens.mu.Lock()
	c.tokens.token = "test-token"
	c.tokens.expiresAt = time.Now().Add(time.Hour)
	c.tokens.mu.Unlock()
	return c
}

// A 5xx must be retried once with the same payload; the retry's success is the
// call's success.
func TestDoJSONRetriesOn5xx(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			http.Error(w, `{"code":500,"message":"upstream hiccup"}`, http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"id":"m1"}`))
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	resp, err := c.SendC2CMessage(context.Background(), "openid", &MessageRequest{Content: "hi", MsgSeq: 1})
	if err != nil {
		t.Fatalf("expected retry to succeed, got %v", err)
	}
	if resp.ID != "m1" {
		t.Fatalf("unexpected response id %q", resp.ID)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("expected exactly 2 attempts, got %d", got)
	}
}

// A 4xx is a deterministic rejection and must NOT be retried.
func TestDoJSONNoRetryOn4xx(t *testing.T) {
	var calls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		http.Error(w, `{"code":40054005,"message":"消息被去重"}`, http.StatusBadRequest)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.SendC2CMessage(context.Background(), "openid", &MessageRequest{Content: "hi", MsgSeq: 1})
	if err == nil {
		t.Fatalf("expected an error")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("4xx must not be retried; got %d attempts", got)
	}
}
