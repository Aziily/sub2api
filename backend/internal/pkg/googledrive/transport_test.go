package googledrive

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTransportCachesAndPersistsRotatedCredential(t *testing.T) {
	refreshes := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/token" {
			refreshes++
			fmt.Fprint(w, `{"access_token":"access","refresh_token":"rotated","expires_in":3600}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer access" {
			t.Error("missing access token")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	flow, err := NewOAuthFlow(OAuthConfig{ClientID: "id", ClientSecret: "secret", RedirectURI: "http://localhost/callback"}, server.Client())
	if err != nil {
		t.Fatal(err)
	}
	flow.tokenURL = server.URL + "/token"
	var saved string
	transport := &TokenTransport{Flow: flow, RefreshToken: "old", Base: server.Client().Transport, SaveRefreshToken: func(_ context.Context, value string) error { saved = value; return nil }}
	client := &http.Client{Transport: transport}
	for i := 0; i < 2; i++ {
		resp, err := client.Get(server.URL + "/file")
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
	}
	if refreshes != 1 || saved != "rotated" {
		t.Fatal("refresh was not cached or persisted")
	}
}
