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
func (c *Client) doJSON(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request: %w", err)
		}
		reader = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, reader)
	if err != nil {
		return err
	}

	token, err := c.tokens.Token(ctx)
	if err != nil {
		return fmt.Errorf("acquire token: %w", err)
	}
	req.Header.Set("Authorization", "QQBot "+token)
	req.Header.Set("X-Union-Appid", c.appID)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
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
		return apiErr
	}

	if out == nil || len(bytes.TrimSpace(data)) == 0 {
		return nil
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode response: %w (body=%s)", err, truncate(string(data), 256))
	}
	return nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
