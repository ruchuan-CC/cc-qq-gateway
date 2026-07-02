package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Production and sandbox OpenAPI bases.
const (
	BaseProd    = "https://api.sgroup.qq.com"
	BaseSandbox = "https://sandbox.api.sgroup.qq.com"
)

// Client is a QQ Bot OpenAPI v2 client. It handles authentication, request
// signing and JSON (de)serialization for every documented endpoint.
type Client struct {
	base       string
	appID      string
	tokens     *TokenManager
	httpClient *http.Client
}

// Options configures a Client.
type Options struct {
	AppID        string
	ClientSecret string
	Sandbox      bool
	HTTPClient   *http.Client
	// BaseURL overrides the OpenAPI base (tests). Empty picks prod/sandbox.
	BaseURL string
}

// NewClient builds a Client from credentials.
func NewClient(opts Options) *Client {
	httpClient := opts.HTTPClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	base := BaseProd
	if opts.Sandbox {
		base = BaseSandbox
	}
	if opts.BaseURL != "" {
		base = opts.BaseURL
	}
	return &Client{
		base:       base,
		appID:      opts.AppID,
		tokens:     NewTokenManager(opts.AppID, opts.ClientSecret, httpClient),
		httpClient: httpClient,
	}
}

// Base returns the OpenAPI base URL in use.
func (c *Client) Base() string { return c.base }

// AppID returns the bot's app id.
func (c *Client) AppID() string { return c.appID }

// Token returns a valid access token (used by the WebSocket transport).
func (c *Client) Token(ctx context.Context) (string, error) {
	return c.tokens.Token(ctx)
}

// APIError is a structured OpenAPI error response.
type APIError struct {
	HTTPStatus int    `json:"-"`
	Code       int    `json:"code"`
	Message    string `json:"message"`
	TraceID    string `json:"-"`
}

func (e *APIError) Error() string {
	return fmt.Sprintf("qq api error: http=%d code=%d message=%q trace=%s",
		e.HTTPStatus, e.Code, e.Message, e.TraceID)
}

// doJSON performs an authenticated request. body (if non-nil) is JSON-encoded;
// out (if non-nil) receives the decoded JSON response. 204/empty bodies are
// handled gracefully.
//
// A network error or a 5xx is retried once after a short pause: for message
// sends this is safe against duplicates because the retried request carries the
// SAME msg_seq — if the first attempt actually landed, QQ dedupes the second
// (40054005). Without this, one transient blip on a passive reply escalated
// straight to an active push, burning the scarce monthly active-push quota.
// 4xx responses are never retried (they are deterministic rejections).
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var raw []byte
	if body != nil {
		var err error
		if raw, err = json.Marshal(body); err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
	}

	const attempts = 2
	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return lastErr
			case <-time.After(500 * time.Millisecond):
			}
		}
		retry, err := c.doJSONOnce(ctx, method, path, raw, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if !retry {
			return err
		}
	}
	return lastErr
}

// doJSONOnce performs a single authenticated request. retry reports whether the
// failure is transient (worth one same-payload retry).
func (c *Client) doJSONOnce(ctx context.Context, method, path string, raw []byte, out any) (retry bool, err error) {
	var reader io.Reader
	if raw != nil {
		reader = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return false, err
	}

	token, err := c.tokens.Token(ctx)
	if err != nil {
		return false, fmt.Errorf("acquire token: %w", err)
	}
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("X-Union-Appid", c.appID)
	if raw != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Transport-level failure (unless the caller's context ended).
		return ctx.Err() == nil, err
	}
	defer resp.Body.Close()

	data, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		apiErr := &APIError{HTTPStatus: resp.StatusCode, TraceID: resp.Header.Get("X-Tps-trace-ID")}
		// Best-effort decode of the structured error body.
		_ = json.Unmarshal(data, apiErr)
		if apiErr.Message == "" {
			apiErr.Message = strings.TrimSpace(string(data))
		}
		return resp.StatusCode >= 500, apiErr
	}

	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return false, nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return false, fmt.Errorf("decode response: %w (body=%s)", err, truncate(string(data), 256))
	}
	return false, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
