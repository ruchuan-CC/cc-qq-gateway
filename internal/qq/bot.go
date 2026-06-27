package qq

import (
	"context"
	"net/http"
)

// GetMe returns the current bot user (used at startup to confirm authentication).
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var out User
	if err := c.doJSON(ctx, http.MethodGet, "/users/@me", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ---------------------------------------------------------------------------
// Gateway (WebSocket bootstrap)
// ---------------------------------------------------------------------------

// SessionStartLimit describes WebSocket session creation quotas.
type SessionStartLimit struct {
	Total          int `json:"total"`
	Remaining      int `json:"remaining"`
	ResetAfter     int `json:"reset_after"`
	MaxConcurrency int `json:"max_concurrency"`
}

// GatewayInfo is the result of GET /gateway/bot.
type GatewayInfo struct {
	URL               string            `json:"url"`
	Shards            int               `json:"shards"`
	SessionStartLimit SessionStartLimit `json:"session_start_limit"`
}

// GetGatewayBot returns the WSS gateway info (URL + shard recommendation).
func (c *Client) GetGatewayBot(ctx context.Context) (*GatewayInfo, error) {
	var out GatewayInfo
	if err := c.doJSON(ctx, http.MethodGet, "/gateway/bot", nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
