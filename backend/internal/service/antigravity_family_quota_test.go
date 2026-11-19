//go:build unit

package service

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Antigravity Ultra exposes two shared quota buckets: one for Gemini text models
// and one for Claude/GPT-OSS ("3p"). Exhausting any member must cool the whole
// bucket on that account, while leaving the other bucket schedulable.
func TestAntigravityModelRateLimits_SharedFamilies(t *testing.T) {
	mapping := map[string]any{
		"claude-opus-5-5-high":   "claude-opus-5-5-high",
		"claude-opus-5-5-low":    "claude-opus-5-5-low",
		"claude-sonnet-5-5-high": "claude-sonnet-5-5-high",
		"claude-opus-5-5":        "claude-opus-5-5-high",
		"gpt-oss-120b-medium":    "gpt-oss-120b-medium",
		"gemini-3.8-flash":       "gemini-3.8-flash",
		"gemini-4-argon":         "gemini-4-argon-high",
		"gemini-4-carbon":        "gemini-4-carbon-high",
		"gemini-4-carbon-low":    "gemini-4-carbon-low",
		"gemini-4-neon": "gemini-4-neon",
		"gemini-4-neon-xhigh": "gemini-4-neon-xhigh",
		"gemini-3-pro-image":     "gemini-3-pro-image",
	}
	claude := []string{"claude-opus-5-5-high", "claude-opus-5-5-low", "claude-sonnet-5-5-high", "claude-opus-5-5", "gpt-oss-120b-medium"}
	gemini := []string{"gemini-3.8-flash", "gemini-4-argon", "gemini-4-carbon", "gemini-4-carbon-low", "gemini-4-neon", "gemini-4-neon-xhigh"}
	cases := []struct {
		name      string
		exhausted string
		same      []string
		other     []string
	}{
		{name: "Opus exhausts Claude bucket", exhausted: "claude-opus-5-5-high", same: claude, other: gemini},
		{name: "Neon exhausts Gemini bucket", exhausted: "gemini-4-neon-xhigh", same: gemini, other: claude},
		{name: "Carbon exhausts Gemini bucket", exhausted: "gemini-4-carbon-high", same: gemini, other: claude},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			account := &Account{
				ID: 17, Platform: PlatformAntigravity, Status: StatusActive, Schedulable: true,
				Credentials: map[string]any{"plan_type": "g1-ultra-tier", "model_mapping": mapping},
			}
			repo := &stubAntigravityAccountRepo{}
			svc := &AntigravityGatewayService{}
			require.True(t, svc.setAntigravityModelRateLimits(context.Background(), repo, account,
				tc.exhausted, "[test]", http.StatusTooManyRequests, time.Now().Add(time.Hour), false))

			limits := make(map[string]any)
			for _, call := range repo.modelRateLimitCalls {
				limits[call.modelKey] = map[string]any{"rate_limit_reset_at": call.resetAt.Format(time.RFC3339)}
			}
			account.Extra = map[string]any{modelRateLimitsKey: limits}
			data, err := json.Marshal(account)
			require.NoError(t, err)
			var snapshot Account
			require.NoError(t, json.Unmarshal(data, &snapshot))

			for _, model := range tc.same {
				require.False(t, snapshot.IsSchedulableForModel(model), "same bucket: %s", model)
			}
			for _, model := range tc.other {
				require.True(t, snapshot.IsSchedulableForModel(model), "other bucket: %s", model)
			}
			require.True(t, snapshot.IsSchedulableForModel("gemini-3-pro-image"))
		})
	}
}

func TestAntigravityClaudeCapacityDoesNotCoolSharedFamily(t *testing.T) {
	account := &Account{ID: 18, Platform: PlatformAntigravity, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"plan_type": "g1-ultra-tier"}}
	repo := &stubAntigravityAccountRepo{}
	svc := &AntigravityGatewayService{}
	require.True(t, svc.setAntigravityModelRateLimits(context.Background(), repo, account,
		"claude-opus-5-5-high", "[test]", http.StatusServiceUnavailable, time.Now().Add(time.Hour), false))
	require.Len(t, repo.modelRateLimitCalls, 1)
	require.Equal(t, "claude-opus-5-5-high", repo.modelRateLimitCalls[0].modelKey)
}
