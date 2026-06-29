package claude

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// Subscription usage endpoints used by Claude Code's OAuth (subscription) auth.
const (
	usageURL   = "https://api.anthropic.com/api/oauth/usage"
	profileURL = "https://api.anthropic.com/api/oauth/profile"
	oauthBeta  = "oauth-2025-04-20"
)

// Window is one rate-limit window (5-hour, 7-day, per-model).
type Window struct {
	Has         bool
	Utilization float64   // percent 0..100
	ResetsAt    time.Time // zero if unknown
}

// Usage is a snapshot of the subscription's usage/limits.
type Usage struct {
	Plan     string // e.g. "default_claude_max_20x"
	FiveHour Window
	SevenDay Window
	Opus     Window
	Sonnet   Window
}

type apiWindow struct {
	Utilization float64 `json:"utilization"`
	ResetsAt    string  `json:"resets_at"`
}

func (w *apiWindow) to() Window {
	if w == nil {
		return Window{}
	}
	out := Window{Has: true, Utilization: w.Utilization}
	if w.ResetsAt != "" {
		if t, err := time.Parse(time.RFC3339, w.ResetsAt); err == nil {
			out.ResetsAt = t
		}
	}
	return out
}

// credPath returns the Claude OAuth credentials file path.
func credPath() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/home/claude"
	}
	return filepath.Join(home, ".claude", ".credentials.json")
}

// FetchUsage reads the OAuth access token from the Claude credentials file and
// queries the subscription usage + profile. The token is refreshed by the Claude
// CLI on every turn, so reading it fresh here keeps it valid.
func FetchUsage(ctx context.Context) (*Usage, error) {
	data, err := os.ReadFile(credPath())
	if err != nil {
		return nil, fmt.Errorf("read credentials: %w", err)
	}
	var creds struct {
		OAuth struct {
			AccessToken string `json:"accessToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &creds); err != nil {
		return nil, fmt.Errorf("parse credentials: %w", err)
	}
	tok := creds.OAuth.AccessToken
	if tok == "" {
		return nil, fmt.Errorf("no OAuth token (API-key install has no subscription usage)")
	}

	var raw struct {
		FiveHour       *apiWindow `json:"five_hour"`
		SevenDay       *apiWindow `json:"seven_day"`
		SevenDayOpus   *apiWindow `json:"seven_day_opus"`
		SevenDaySonnet *apiWindow `json:"seven_day_sonnet"`
	}
	if err := getJSON(ctx, usageURL, tok, &raw); err != nil {
		return nil, err
	}

	u := &Usage{
		FiveHour: raw.FiveHour.to(),
		SevenDay: raw.SevenDay.to(),
		Opus:     raw.SevenDayOpus.to(),
		Sonnet:   raw.SevenDaySonnet.to(),
	}

	// Profile is best-effort (plan / rate-limit tier).
	var prof struct {
		Organization struct {
			RateLimitTier string `json:"rate_limit_tier"`
		} `json:"organization"`
	}
	if getJSON(ctx, profileURL, tok, &prof) == nil {
		u.Plan = prof.Organization.RateLimitTier
	}
	return u, nil
}

func getJSON(ctx context.Context, url, tok string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("anthropic-beta", oauthBeta)
	req.Header.Set("anthropic-version", "2023-06-01")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "cc-qq-gateway")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("usage api: status %d", resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(out)
}
