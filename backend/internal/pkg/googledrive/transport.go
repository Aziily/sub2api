package googledrive

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// TokenTransport obtains short-lived access tokens without exposing refresh
// credentials to callers. Each instance belongs to exactly one connection.
type TokenTransport struct {
	Flow         *OAuthFlow
	RefreshToken string
	// SaveRefreshToken must persist a rotated token before it is used.
	SaveRefreshToken func(context.Context, string) error
	Base             http.RoundTripper
	mu               sync.Mutex
	accessToken      string
	expires          time.Time
}

func (t *TokenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	token, err := t.access(req.Context())
	if err != nil {
		return nil, err
	}
	clone := req.Clone(req.Context())
	clone.Header = req.Header.Clone()
	clone.Header.Set("Authorization", "Bearer "+token)
	base := t.Base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(clone)
}

func (t *TokenTransport) access(ctx context.Context) (string, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.accessToken != "" && time.Now().Before(t.expires) {
		return t.accessToken, nil
	}
	if t.Flow == nil || t.RefreshToken == "" {
		return "", errors.New("Drive credential is not configured")
	}
	token, err := t.Flow.Refresh(ctx, t.RefreshToken)
	if err != nil {
		return "", err
	}
	if token.ExpiresIn <= 60 {
		return "", errors.New("invalid Google access token lifetime")
	}
	if token.RefreshToken != "" && token.RefreshToken != t.RefreshToken {
		if t.SaveRefreshToken == nil {
			return "", errors.New("cannot save rotated Drive credential")
		}
		if err := t.SaveRefreshToken(ctx, token.RefreshToken); err != nil {
			return "", errors.New("cannot save rotated Drive credential")
		}
		t.RefreshToken = token.RefreshToken
	}
	t.accessToken = token.AccessToken
	t.expires = time.Now().Add(time.Duration(token.ExpiresIn-60) * time.Second)
	return t.accessToken, nil
}
