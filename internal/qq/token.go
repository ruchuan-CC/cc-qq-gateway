package qq

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// tokenEndpoint issues the app access token. It is the same for production and
// sandbox and lives on a different host than the OpenAPI base.
const tokenEndpoint = "https://bots.qq.com/app/getAppAccessToken"

type tokenRequest struct {
	AppID        string `json:"appId"`
	ClientSecret string `json:"clientSecret"`
}

type tokenResponse struct {
	AccessToken string      `json:"access_token"`
	ExpiresIn   json.Number `json:"expires_in"`
}

// TokenManager fetches and caches the app access token, refreshing it before
// expiry. It is safe for concurrent use.
type TokenManager struct {
	appID        string
	clientSecret string
	httpClient   *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

// NewTokenManager creates a token manager for the given credentials.
func NewTokenManager(appID, clientSecret string, httpClient *http.Client) *TokenManager {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	}
	return &TokenManager{
		appID:        appID,
		clientSecret: clientSecret,
		httpClient:   httpClient,
	}
}

// Token returns a valid access token, refreshing if it is within the 60-second
// grace window of expiry.
func (m *TokenManager) Token(ctx context.Context) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.token != "" && time.Until(m.expiresAt) > 60*time.Second {
		return m.token, nil
	}
	if err := m.refreshLocked(ctx); err != nil {
		// If we still have a token that is technically valid, fall back to it
		// rather than failing outright.
		if m.token != "" && time.Now().Before(m.expiresAt) {
			return m.token, nil
		}
		return "", err
	}
	return m.token, nil
}

func (m *TokenManager) refreshLocked(ctx context.Context) error {
	body, _ := json.Marshal(tokenRequest{AppID: m.appID, ClientSecret: m.clientSecret})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEndpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request access token: %w", err)
	}
	defer resp.Body.Close()

	var tr tokenResponse
	if err := json.NewDecoder(resp.Body).Decode(&tr); err != nil {
		return fmt.Errorf("decode access token response: %w", err)
	}
	if resp.StatusCode != http.StatusOK || tr.AccessToken == "" {
		return fmt.Errorf("get access token failed: status=%d body=%+v", resp.StatusCode, tr)
	}

	expires, _ := tr.ExpiresIn.Int64()
	if expires <= 0 {
		expires = 7200
	}
	m.token = tr.AccessToken
	m.expiresAt = time.Now().Add(time.Duration(expires) * time.Second)
	return nil
}
