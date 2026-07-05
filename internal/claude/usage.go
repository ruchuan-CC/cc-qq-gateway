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

// Limit is one rate-limit window reported by the usage API. The API's `limits`
// array is the canonical, complete list of every quota on the account (the
// rolling session window, the weekly-all window, and any per-model weekly
// windows such as Opus or Fable), so rendering it shows *all* quotas.
type Limit struct {
	Kind     string    // "session" | "weekly_all" | "weekly_scoped" | ...
	Group    string    // "session" | "weekly"
	Scope    string    // model display name for scoped limits, else ""
	Percent  float64   // 0..100
	Severity string    // "normal" | "warning" | "critical" | ...
	ResetsAt time.Time // zero if unknown
	IsActive bool
}

// Usage is a snapshot of the subscription's usage/limits.
type Usage struct {
	Plan   string  // e.g. "default_claude_max_20x"
	Limits []Limit // every quota window the API reports

	// Extra usage (pay-as-you-go credits beyond the plan), shown when enabled.
	ExtraEnabled bool
	ExtraPercent float64 // 0..100; 0 if unknown
}

func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
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

	type apiWindow struct {
		Utilization float64 `json:"utilization"`
		ResetsAt    string  `json:"resets_at"`
	}
	var raw struct {
		FiveHour *apiWindow `json:"five_hour"`
		SevenDay *apiWindow `json:"seven_day"`
		Limits   []struct {
			Kind     string  `json:"kind"`
			Group    string  `json:"group"`
			Percent  float64 `json:"percent"`
			Severity string  `json:"severity"`
			ResetsAt string  `json:"resets_at"`
			IsActive bool    `json:"is_active"`
			Scope    *struct {
				Model *struct {
					DisplayName string `json:"display_name"`
				} `json:"model"`
			} `json:"scope"`
		} `json:"limits"`
		ExtraUsage struct {
			IsEnabled   bool     `json:"is_enabled"`
			Utilization *float64 `json:"utilization"`
		} `json:"extra_usage"`
	}
	if err := getJSON(ctx, usageURL, tok, &raw); err != nil {
		return nil, err
	}

	u := &Usage{}
	// Prefer the rich `limits` array (complete + includes per-model scoped windows).
	for _, l := range raw.Limits {
		lim := Limit{
			Kind:     l.Kind,
			Group:    l.Group,
			Percent:  l.Percent,
			Severity: l.Severity,
			ResetsAt: parseTime(l.ResetsAt),
			IsActive: l.IsActive,
		}
		if l.Scope != nil && l.Scope.Model != nil {
			lim.Scope = l.Scope.Model.DisplayName
		}
		u.Limits = append(u.Limits, lim)
	}
	// Fallback for older API shapes without `limits`: synthesize from the two
	// top-level windows so /usage still shows something.
	if len(u.Limits) == 0 {
		if raw.FiveHour != nil {
			u.Limits = append(u.Limits, Limit{Kind: "session", Group: "session", Percent: raw.FiveHour.Utilization, ResetsAt: parseTime(raw.FiveHour.ResetsAt), IsActive: true})
		}
		if raw.SevenDay != nil {
			u.Limits = append(u.Limits, Limit{Kind: "weekly_all", Group: "weekly", Percent: raw.SevenDay.Utilization, ResetsAt: parseTime(raw.SevenDay.ResetsAt)})
		}
	}
	u.ExtraEnabled = raw.ExtraUsage.IsEnabled
	if raw.ExtraUsage.Utilization != nil {
		u.ExtraPercent = *raw.ExtraUsage.Utilization
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
