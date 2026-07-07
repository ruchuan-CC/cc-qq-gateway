package qq

import (
	"bytes"
	"context"
	"io"
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

func TestDoJSONRefreshesTokenOnExpiredTokenCode(t *testing.T) {
	var apiCalls atomic.Int64
	c := NewClient(Options{
		AppID:        "app",
		ClientSecret: "secret",
		BaseURL:      "https://api.test",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			if r.URL.Host == "bots.qq.com" && r.URL.Path == "/app/getAppAccessToken" {
				return jsonResponse(http.StatusOK, `{"access_token":"fresh-token","expires_in":"7200"}`), nil
			}
			apiCalls.Add(1)
			if got := r.Header.Get("Authorization"); got != "QQBot fresh-token" {
				return jsonResponse(http.StatusInternalServerError, `{"code":11244,"message":"token not exist or expire"}`), nil
			}
			return jsonResponse(http.StatusOK, `{"id":"ok"}`), nil
		})},
	})
	c.tokens.mu.Lock()
	c.tokens.token = "stale-token"
	c.tokens.expiresAt = time.Now().Add(time.Hour)
	c.tokens.mu.Unlock()

	resp, err := c.SendC2CMessage(context.Background(), "openid", &MessageRequest{Content: "hi", MsgSeq: 1})
	if err != nil {
		t.Fatalf("expected expired token refresh to succeed, got %v", err)
	}
	if resp.ID != "ok" {
		t.Fatalf("response id = %q, want ok", resp.ID)
	}
	if got := apiCalls.Load(); got != 2 {
		t.Fatalf("api calls = %d, want stale attempt plus refreshed retry", got)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

func jsonResponse(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}
