package googledrive

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const DriveFileScope = "https://www.googleapis.com/auth/drive.file"
const googleAuthEndpoint = "https://accounts.google.com/o/oauth2/v2/auth"
const googleTokenEndpoint = "https://oauth2.googleapis.com/token"

type OAuthConfig struct{ ClientID, ClientSecret, RedirectURI string }
type oauthSession struct {
	verifier string
	userID   int64
	actorID  int64
	expires  time.Time
}
type OAuthFlow struct {
	config     OAuthConfig
	httpClient *http.Client
	tokenURL   string
	mu         sync.Mutex
	sessions   map[string]oauthSession
}
type Token struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"`
	Scope        string `json:"scope"`
	TokenType    string `json:"token_type"`
}

func NewOAuthFlow(config OAuthConfig, client *http.Client) (*OAuthFlow, error) {
	if config.ClientID == "" || config.ClientSecret == "" {
		return nil, errors.New("Google OAuth client is not configured")
	}
	redirect, err := url.Parse(config.RedirectURI)
	if err != nil || redirect.Host == "" || (redirect.Scheme != "https" && !(redirect.Scheme == "http" && (redirect.Hostname() == "localhost" || redirect.Hostname() == "127.0.0.1"))) || redirect.User != nil || redirect.RawQuery != "" || redirect.Fragment != "" {
		return nil, errors.New("invalid Google OAuth redirect URI")
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}
	return &OAuthFlow{config: config, httpClient: client, tokenURL: googleTokenEndpoint, sessions: make(map[string]oauthSession)}, nil
}
func randomString() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// Start binds the one-time state to the administrator and a target (for the
// storage pool: an existing storage account ID, or 0 for a new account) and
// expires it after ten minutes.
func (o *OAuthFlow) Start(actorID, userID int64) (string, error) {
	if actorID <= 0 || userID < 0 {
		return "", errors.New("invalid user")
	}
	state, err := randomString()
	if err != nil {
		return "", err
	}
	verifier, err := randomString()
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256([]byte(verifier))
	o.mu.Lock()
	for k, v := range o.sessions {
		if time.Now().After(v.expires) {
			delete(o.sessions, k)
		}
	}
	if len(o.sessions) >= 1024 {
		o.mu.Unlock()
		return "", errors.New("too many pending authorization sessions")
	}
	o.sessions[state] = oauthSession{verifier: verifier, userID: userID, actorID: actorID, expires: time.Now().Add(10 * time.Minute)}
	o.mu.Unlock()
	q := url.Values{"client_id": {o.config.ClientID}, "redirect_uri": {o.config.RedirectURI}, "response_type": {"code"}, "scope": {DriveFileScope}, "access_type": {"offline"}, "prompt": {"consent"}, "state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}}
	return googleAuthEndpoint + "?" + q.Encode(), nil
}

func (o *OAuthFlow) Exchange(ctx context.Context, actorID, userID int64, state, code string) (*Token, error) {
	o.mu.Lock()
	session, ok := o.sessions[state]
	if ok && session.userID == userID && session.actorID == actorID {
		delete(o.sessions, state)
	}
	o.mu.Unlock()
	if !ok || session.userID != userID || session.actorID != actorID || time.Now().After(session.expires) || code == "" {
		return nil, errors.New("invalid or expired authorization session")
	}
	return o.token(ctx, url.Values{"code": {code}, "client_id": {o.config.ClientID}, "client_secret": {o.config.ClientSecret}, "redirect_uri": {o.config.RedirectURI}, "code_verifier": {session.verifier}, "grant_type": {"authorization_code"}})
}

// ExchangeByState completes an authorization using only the one-time state.
// The state was issued to an authenticated administrator and is bound to the
// target user, so possession of an unexpired, unused state authorizes the
// exchange. This lets the browser redirect complete server-side without
// relying on the administrator's browser session or origin.
func (o *OAuthFlow) ExchangeByState(ctx context.Context, state, code string) (actorID, userID int64, token *Token, err error) {
	if state == "" || code == "" {
		return 0, 0, nil, errors.New("invalid or expired authorization session")
	}
	o.mu.Lock()
	session, ok := o.sessions[state]
	if ok {
		delete(o.sessions, state)
	}
	o.mu.Unlock()
	if !ok || time.Now().After(session.expires) {
		return 0, 0, nil, errors.New("invalid or expired authorization session")
	}
	token, err = o.token(ctx, url.Values{"code": {code}, "client_id": {o.config.ClientID}, "client_secret": {o.config.ClientSecret}, "redirect_uri": {o.config.RedirectURI}, "code_verifier": {session.verifier}, "grant_type": {"authorization_code"}})
	if err != nil {
		return 0, 0, nil, err
	}
	return session.actorID, session.userID, token, nil
}
func (o *OAuthFlow) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	if refreshToken == "" {
		return nil, errors.New("missing refresh token")
	}
	return o.token(ctx, url.Values{"refresh_token": {refreshToken}, "client_id": {o.config.ClientID}, "client_secret": {o.config.ClientSecret}, "grant_type": {"refresh_token"}})
}
func (o *OAuthFlow) token(ctx context.Context, data url.Values) (*Token, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.tokenURL, strings.NewReader(data.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := o.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("Google OAuth request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: resp.StatusCode}
	}
	var token Token
	if err = json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&token); err != nil {
		return nil, errors.New("invalid Google OAuth response")
	}
	if token.AccessToken == "" {
		return nil, errors.New("Google OAuth response missing access token")
	}
	return &token, nil
}
