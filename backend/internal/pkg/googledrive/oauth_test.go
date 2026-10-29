package googledrive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func TestOAuthStatePKCEAndRefresh(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.Form.Get("client_secret") != "secret" {
			t.Error("missing client secret")
		}
		if r.Form.Get("grant_type") == "authorization_code" && (r.Form.Get("code_verifier") == "" || r.Form.Get("code") != "code") {
			t.Error("invalid code exchange")
		}
		if r.Form.Get("grant_type") == "refresh_token" && r.Form.Get("refresh_token") != "refresh" {
			t.Error("invalid refresh")
		}
		_ = json.NewEncoder(w).Encode(Token{AccessToken: "access", RefreshToken: "refresh", ExpiresIn: 3600})
	}))
	defer server.Close()
	flow, err := NewOAuthFlow(OAuthConfig{ClientID: "client", ClientSecret: "secret", RedirectURI: "http://localhost:28080/auth/drive/callback"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	flow.tokenURL = server.URL
	auth, err := flow.Start(12)
	if err != nil {
		t.Fatal(err)
	}
	parsed, _ := url.Parse(auth)
	state := parsed.Query().Get("state")
	if state == "" || parsed.Query().Get("code_challenge_method") != "S256" || parsed.Query().Get("scope") != DriveFileScope {
		t.Fatal("missing OAuth parameters")
	}
	if _, err = flow.Exchange(context.Background(), 13, state, "code"); err == nil {
		t.Fatal("wrong user accepted")
	}
	if _, err = flow.Exchange(context.Background(), 12, state, "code"); err != nil {
		t.Fatal(err)
	}
	if _, err = flow.Exchange(context.Background(), 12, state, "code"); err == nil {
		t.Fatal("reused state accepted")
	}
	if _, err = flow.Refresh(context.Background(), "refresh"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("unexpected token requests: %d", calls)
	}
}
func TestRejectUnsafeRedirect(t *testing.T) {
	for _, uri := range []string{"http://example.com/cb", "https://user@example.com/cb", "https://example.com/cb?x=1"} {
		if _, err := NewOAuthFlow(OAuthConfig{ClientID: "id", ClientSecret: "secret", RedirectURI: uri}, nil); err == nil {
			t.Fatalf("accepted %s", uri)
		}
	}
}
